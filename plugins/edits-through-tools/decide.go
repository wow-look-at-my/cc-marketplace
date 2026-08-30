package main

import (
	"encoding/json"
	"strings"
)

type toolInput struct {
	Command        string          `json:"command"`
	FilePath       string          `json:"file_path"`
	NotebookPath   string          `json:"notebook_path"`
	Path           string          `json:"path"`
	Skill          string          `json:"skill"`
	PermissionMode string          `json:"permissionMode"`
	PermissionMod2 string          `json:"permission_mode"`
	Tools          json.RawMessage `json:"tools"`
	AllowedTools   json.RawMessage `json:"allowedTools"`
	ExtraAllowed   json.RawMessage `json:"extra_allowed_tools"`
}

const useTheTools = "Use Edit to change an existing file, or Write to create one."

// decide returns the denial reason, or "" to stay out of the way.
func decide(raw []byte) string {
	var in hookInput
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	if in.HookEventName != "PreToolUse" {
		return ""
	}
	var ti toolInput
	if len(in.ToolInput) > 0 {
		_ = json.Unmarshal(in.ToolInput, &ti)
	}

	switch {
	case in.ToolName == "Bash":
		return evaluate(ti.Command, in.Cwd)
	case isEditTool(in.ToolName):
		return editToolReason(in.ToolName, ti, in.Cwd)
	case isAgentTool(in.ToolName):
		return agentReason(in.ToolName, ti)
	case in.ToolName == "Skill":
		return skillReason(ti.Skill)
	default:
		return mcpReason(in.ToolName)
	}
}

// evaluate runs the shell analysis under a recover. This hook fails CLOSED, so a
// panic denies rather than waving the command through: a bug here must be loud
// and visible, not a silent hole in the rule.
func evaluate(command, cwd string) (reason string) {
	if strings.TrimSpace(command) == "" {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			reason = "blocked: edits-through-tools could not analyse this command, and an unanalysed command is treated as one that writes. " + useTheTools
		}
	}()
	return analyse(command, cwd)
}

func analyse(command, cwd string) string {
	roots := guardedRoots(cwd)
	segs, blockers, ok := parseSegments(command, cwd)
	if !ok {
		return "blocked: this command does not parse as shell, so the files it would write cannot be resolved. " + useTheTools
	}
	for _, b := range blockers {
		return "blocked: the command runs " + b + ". " + useTheTools
	}
	for _, seg := range segs {
		for _, w := range classify(seg) {
			if reason := judge(w, roots); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// judge turns one write into a verdict. Every branch that cannot resolve a
// target denies: a path this hook cannot name is a path it cannot clear.
func judge(w write, roots []string) string {
	if w.opaque != "" {
		return "blocked: " + w.route + " runs " + w.opaque + ". " + useTheTools
	}
	if w.whole {
		if w.dir == unknownDirText {
			return "blocked: " + w.route + " writes into a directory that is not statically known, so this hook cannot tell whether it lands in the working tree. " + useTheTools
		}
		if root, hit := coversGuarded(roots, w.dir); hit {
			return "blocked: " + w.route + " writes into " + display(root, w.dir) + ", which is inside the working tree. " + useTheTools
		}
		return ""
	}
	for _, p := range w.paths {
		if !p.static {
			return "blocked: " + w.route + " writes a path built from an expansion (" + p.text + "), so this hook cannot tell whether it lands in the working tree. " + useTheTools
		}
		abs := abs(w.dir, p.text)
		if abs == "" {
			return "blocked: " + w.route + " writes " + p.text + ", which resolves against a directory that is not statically known. " + useTheTools
		}
		if isProtectedConfig(abs) {
			return settingsReason(w.route + " writes " + abs)
		}
		if root, hit := insideGuarded(roots, abs); hit {
			return "blocked: " + w.route + " writes " + display(root, abs) + ", which is inside the working tree. " + useTheTools
		}
	}
	return ""
}

func settingsReason(what string) string {
	return "blocked: " + what + ", the live Claude Code settings. A session must not re-grant what a guard denies; ask the user to make the change."
}

func isEditTool(name string) bool {
	switch name {
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}

// The edit tools are the sanctioned route and are left alone -- except where
// their target is the live settings, which is how a session would re-grant what
// every rule above denies.
func editToolReason(tool string, ti toolInput, cwd string) string {
	for _, p := range []string{ti.FilePath, ti.NotebookPath, ti.Path} {
		if p == "" {
			continue
		}
		if a := abs(cwd, p); a != "" && isProtectedConfig(a) {
			return settingsReason(tool + " targets " + a)
		}
	}
	return ""
}

func isAgentTool(name string) bool {
	return name == "Agent" || name == "Task" ||
		strings.HasSuffix(name, "__create_session")
}

// A subagent inherits the session's hooks, so its own Bash calls arrive here
// like any other. What does not arrive here is a grant the spawn hands the
// child: an explicit tool list or a permissive mode makes the child able to do
// what the parent was refused, in one call. A spawn that asks for neither is
// ordinary delegation and passes.
func agentReason(tool string, ti toolInput) string {
	mode := ti.PermissionMode
	if mode == "" {
		mode = ti.PermissionMod2
	}
	switch mode {
	case "bypassPermissions", "acceptEdits", "dontAsk":
		return "blocked: " + tool + " asks for permissionMode " + mode + ", which would let the child edit files without the checks this session runs under. Spawn it with the inherited mode."
	}
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"tools", ti.Tools}, {"allowedTools", ti.AllowedTools}, {"extra_allowed_tools", ti.ExtraAllowed},
	} {
		if grantsShell(field.raw) {
			return "blocked: " + tool + " hands the child a " + field.name + " grant including Bash. A child must not be given what the parent does not have; delegate without the grant."
		}
	}
	return ""
}

func grantsShell(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false
		}
		list = strings.Split(s, ",")
	}
	for _, t := range list {
		switch strings.TrimSpace(t) {
		case "Bash", "*", "Write", "Edit", "NotebookEdit":
			return true
		}
	}
	return false
}

// configSkills rewrite settings.json as their whole purpose, which is the same
// act as editing the file by hand.
var configSkills = map[string]bool{
	"update-config": true, "fewer-permission-prompts": true,
}

func skillReason(skill string) string {
	name := skill
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	if configSkills[name] {
		return settingsReason("the " + name + " skill rewrites")
	}
	return ""
}

// githubContentWrites are the MCP tools that commit a file through the API. They
// are the same server-side write as `gh api PUT .../contents/...`, reached
// without a shell.
var githubContentWrites = map[string]bool{
	"create_or_update_file": true, "push_files": true, "delete_file": true,
	"create_file": true, "update_file": true, "upload_file": true,
	"create_commit_on_branch": true, "create_or_update_file_contents": true,
}

func mcpReason(tool string) string {
	if !strings.HasPrefix(tool, "mcp__") {
		return ""
	}
	parts := strings.SplitN(strings.TrimPrefix(tool, "mcp__"), "__", 2)
	if len(parts) != 2 {
		return ""
	}
	if !githubContentWrites[parts[1]] {
		return ""
	}
	return "blocked: " + tool + " commits file content through the API, where it never exists as a file and no edit tool ever sees it. " + useTheTools
}
