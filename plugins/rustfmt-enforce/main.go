// Command rustfmt-enforce makes rustfmt impossible to skip.
//
// PostToolUse on Write, Edit and MultiEdit formats the .rs file the tool wrote.
// PreToolUse on Bash formats every staged or modified .rs file before a git
// commit, and denies the commit. This happens when that changed a file or
// rustfmt failed.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// HookInput is the subset of the hook payload this plugin reads.
type HookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Cwd           string          `json:"cwd"`
}

// result is one invocation's output. Exit 2 on PostToolUse puts stderr in
// front of the model.
type result struct {
	stdout string
	stderr string
	code   int
}

func main() {
	res := run(os.Stdin)
	fmt.Fprint(os.Stdout, res.stdout)
	fmt.Fprint(os.Stderr, res.stderr)
	os.Exit(res.code)
}

func run(stdin io.Reader) result {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return result{stderr: "rustfmt-enforce: read hook payload: " + err.Error() + "\n", code: 1}
	}
	var in HookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return result{stderr: "rustfmt-enforce: parse hook payload: " + err.Error() + "\n", code: 1}
	}
	switch in.HookEventName {
	case "PostToolUse":
		return postToolUse(in)
	case "PreToolUse":
		return preToolUse(in)
	}
	return result{}
}

func postToolUse(in HookInput) result {
	switch in.ToolName {
	case "Write", "Edit", "MultiEdit":
	default:
		return result{}
	}
	var ti struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(in.ToolInput, &ti) != nil || !isRust(ti.FilePath) {
		return result{}
	}
	changed, err := formatFile(ti.FilePath)
	if err != nil {
		return result{stderr: fmt.Sprintf("rustfmt failed on %s. Fix the file so that rustfmt accepts it:\n%v\n", ti.FilePath, err), code: 2}
	}
	if !changed {
		return result{}
	}
	return contextResult("PostToolUse", fmt.Sprintf("rustfmt reformatted %s after this edit. Read the file again before the next Edit to it.", ti.FilePath))
}

func preToolUse(in HookInput) result {
	if in.ToolName != "Bash" {
		return result{}
	}
	var ti struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(in.ToolInput, &ti) != nil {
		return result{}
	}
	dirs := commitDirs(ti.Command, in.Cwd)
	if len(dirs) == 0 {
		return result{}
	}
	var reasons []string
	for _, dir := range dirs {
		reasons = append(reasons, checkCommit(dir)...)
	}
	if len(reasons) == 0 {
		return result{}
	}
	return denyResult(reasons)
}

func contextResult(event, text string) result {
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": text,
		},
	})
	return result{stdout: string(out) + "\n"}
}

func denyResult(reasons []string) result {
	text := "Commit refused: rustfmt is not clean.\n"
	for _, r := range reasons {
		text += "  - " + r + "\n"
	}
	text += "Stage the rustfmt output with git add, then commit again."
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": text,
		},
	})
	return result{stdout: string(out) + "\n"}
}
