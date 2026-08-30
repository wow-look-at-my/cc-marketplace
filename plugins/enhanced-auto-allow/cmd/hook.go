package main

import (
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// Hook input from Claude Code
type HookInput struct {
	HookEventName string    `json:"hook_event_name"`
	ToolName      string    `json:"tool_name"`
	ToolInput     ToolInput `json:"tool_input"`
}

type ToolInput struct {
	Command string `json:"command"`
}

// Sections evaluated deny > ask > allow. A rule matches either argv as
// written or the resolved process name; see CommandNode.
type Rules struct {
	Allow []CommandNode `json:"allow"`
	Ask   []CommandNode `json:"ask"`
	Deny  []CommandNode `json:"deny"`

	AllowProcesses []ProcessRule `json:"allowProcesses"`
	AskProcesses   []ProcessRule `json:"askProcesses"`
	DenyProcesses  []ProcessRule `json:"denyProcesses"`

	MCPServers map[string][]string `json:"mcpServers"`
}

type CommandNode struct {
	Name               interface{}      `json:"name"` // string or []string
	Description        string           `json:"description,omitempty"`
	AllowedFlags       interface{}      `json:"allowedFlags,omitempty"` // "*" or []string
	DeniedFlags        []string         `json:"deniedFlags,omitempty"`
	ExecFlags          []string         `json:"execFlags,omitempty"`
	RequiredFlags      []string         `json:"requiredFlags,omitempty"`
	RequireFlagValue   *RequireFlagRule `json:"requireFlagValue,omitempty"`
	DenyWithMessage    string           `json:"denyWithMessage,omitempty"`
	FlagsWithValue     []string         `json:"flagsWithValue,omitempty"`
	HelpAlwaysAllowed  bool             `json:"helpAlwaysAllowed,omitempty"`
	BareOnly           bool             `json:"bareOnly,omitempty"`
	DenyArgSubstrings  []string         `json:"denyArgSubstrings,omitempty"`
	AllowedArgPrefixes []string         `json:"allowedArgPrefixes,omitempty"`
	Subcommands        []CommandNode    `json:"subcommands,omitempty"`
}

type RequireFlagRule struct {
	Flags   []string `json:"flags"`
	Default string   `json:"default"`
	Allowed []string `json:"allowed"`
}

// Not interchangeable: PermissionRequest runs only after the engine lands on
// "ask", so a deny there alone is silent under any mode that resolves earlier.
// The deny half rides PreToolUse. See docs/two-event-registration.md.
const (
	eventPermissionRequest = "PermissionRequest"
	eventPreToolUse        = "PreToolUse"
)

// Permission response (PermissionRequest event).
type PermissionResponse struct {
	HookSpecificOutput struct {
		HookEventName string `json:"hookEventName"`
		Decision      struct {
			Behavior string `json:"behavior"`
			Message  string `json:"message,omitempty"`
		} `json:"decision"`
	} `json:"hookSpecificOutput"`
}

// PreToolUse response. A flat permissionDecision rather than the nested
// decision object, and the CLI rejects a payload whose hookEventName is not
// the event it dispatched -- so the shape must follow the event, not the
// verdict.
type PreToolUseResponse struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	} `json:"hookSpecificOutput"`
}

var rules Rules

func main() {
	input, _ := io.ReadAll(os.Stdin)
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		os.Exit(0)
	}

	if hi.HookEventName != eventPermissionRequest && hi.HookEventName != eventPreToolUse {
		os.Exit(0)
	}

	// PreToolUse denies only. An allow there settles the call before the
	// engine runs, so the user's own deny rules never get a vote.
	denyOnly := hi.HookEventName == eventPreToolUse

	// Allow all read-only tools
	if hi.ToolName == "Read" || hi.ToolName == "Glob" || hi.ToolName == "Grep" {
		if !denyOnly {
			outputDecision(hi.HookEventName, "allow", "")
		}
		return
	}

	// Load rules from adjacent file
	rulesPath := filepath.Join(filepath.Dir(os.Args[0]), "..", "rules.xml")
	rulesData, err := os.ReadFile(rulesPath)
	if err != nil {
		os.Exit(0)
	}
	var xmlErr error
	rules, xmlErr = loadXMLRules(rulesData)
	if xmlErr != nil {
		os.Exit(0)
	}

	// Allow read-only MCP tools by server + tool pattern matching
	if server, tool := parseMCPTool(hi.ToolName); tool != "" {
		if !denyOnly && matchMCPServer(rules.MCPServers, server, tool) {
			outputDecision(hi.HookEventName, "allow", "")
			return
		}
		os.Exit(0)
	}

	if hi.ToolName != "Bash" {
		os.Exit(0)
	}

	decision, message := evaluateCommand(hi.ToolInput.Command)
	if decision == "" || (denyOnly && decision != "deny") {
		return
	}
	outputDecision(hi.HookEventName, decision, message)
}

func evaluateCommand(command string) (string, string) {
	// Process rules go ahead of the rest. They walk the parse tree, so they
	// still see a command the allow path below refuses to read.
	for _, section := range []struct {
		rules    []ProcessRule
		behavior string
	}{
		{rules.DenyProcesses, "deny"},
		{rules.AskProcesses, "ask"},
	} {
		if name, msg := matchProcessRule(command, section.rules); name != "" {
			if msg == "" {
				msg = name + " may not be run here."
			}
			return section.behavior, msg
		}
	}

	commands := parseAllCommands(command)
	if len(commands) == 0 {
		return "", ""
	}

	// Command rules, same precedence. Every command in a compound must clear
	// the bar: if any is denied, deny; if all are allowed, allow; otherwise
	// passthrough and let the normal permission flow decide.
	for _, section := range []struct {
		nodes    []CommandNode
		behavior string
	}{
		{rules.Deny, "deny"},
		{rules.Ask, "ask"},
	} {
		for _, args := range commands {
			if decision, msg := evaluateArgs(args, section.nodes); decision == "allow" || decision == "deny" {
				// A match in a deny/ask section means that section's behavior,
				// whatever the rule's internal verdict was.
				return section.behavior, msg
			}
		}
	}

	allAllowed := true
	for _, args := range commands {
		decision, msg := evaluateArgs(args, rules.Allow)
		if decision == "deny" {
			return "deny", msg
		}
		if decision != "allow" {
			allAllowed = false
		}
	}

	if allAllowed {
		if name, _ := matchProcessRule(command, rules.AllowProcesses); name != "" {
			return "allow", ""
		}
		return "allow", ""
	}
	return "", ""
}

func evaluateArgs(args []string, nodes []CommandNode) (string, string) {
	if len(args) == 0 || len(nodes) == 0 {
		return "", ""
	}

	current := args[0]
	remaining := args[1:]

	// Deny wins over allow; allow wins over passthrough.
	anyAllowed := false
	for _, node := range nodes {
		if !matchesName(node.Name, current) {
			continue
		}

		decision, msg := evaluateOneNode(node, args, remaining)
		if decision == "deny" {
			return "deny", msg
		}
		if decision == "allow" {
			anyAllowed = true
		}
	}

	if anyAllowed {
		return "allow", ""
	}
	return "", ""
}

func evaluateOneNode(node CommandNode, args []string, remaining []string) (string, string) {
	// If helpAlwaysAllowed, any subcommand chain ending in --help/-h is allowed
	if node.HelpAlwaysAllowed && hasAnyFlag(remaining, []string{"--help", "-h"}) {
		return "allow", ""
	}

	// A deny carrying a message wins outright.
	if node.DenyWithMessage != "" {
		return "deny", node.DenyWithMessage
	}

	// A denied substring in any argument un-matches the node: awk and sed take
	// a script, and its dangerous features are substrings of that body.
	if len(node.DenyArgSubstrings) > 0 {
		for _, arg := range args {
			for _, substr := range node.DenyArgSubstrings {
				if strings.Contains(arg, substr) {
					return "", ""
				}
			}
		}
	}

	// Check required flags
	if len(node.RequiredFlags) > 0 {
		if hasAnyFlag(args, node.RequiredFlags) {
			return "allow", ""
		}
		return "", ""
	}

	// If bareOnly, only allow when there are no remaining arguments
	if node.BareOnly {
		if len(remaining) == 0 {
			return "allow", ""
		}
		return "", ""
	}

	// Check requireFlagValue
	if node.RequireFlagValue != nil {
		value := getFlagValue(args, node.RequireFlagValue.Flags)
		if value == "" {
			value = node.RequireFlagValue.Default
		}
		for _, allowed := range node.RequireFlagValue.Allowed {
			if value == allowed {
				return "allow", ""
			}
		}
		return "", ""
	}

	// Strip own flags (that take values) before subcommand matching
	subcommandArgs := remaining
	if len(node.FlagsWithValue) > 0 {
		subcommandArgs = stripFlagsWithValue(remaining, node.FlagsWithValue)
	}

	// If there are subcommands, recurse
	if len(node.Subcommands) > 0 && len(subcommandArgs) > 0 {
		decision, msg := evaluateArgs(subcommandArgs, node.Subcommands)
		if decision != "" {
			return decision, msg
		}
		// A leading remaining arg that looks like a subcommand but matched
		// none of them is unknown or mutating: never fall through to
		// allowedFlags.
		if !strings.HasPrefix(subcommandArgs[0], "-") {
			return "", ""
		}
	}

	// Check denied flags
	if len(node.DeniedFlags) > 0 && hasAnyFlag(args, node.DeniedFlags) {
		return "", ""
	}

	// Check exec flags: extract sub-commands and evaluate them
	if len(node.ExecFlags) > 0 {
		subCmds := extractExecSubCommands(remaining, node.ExecFlags)
		for _, subCmd := range subCmds {
			decision, msg := evaluateArgs(subCmd, rules.Allow)
			if decision == "deny" {
				return "deny", msg
			}
			if decision != "allow" {
				return "", ""
			}
		}
	}

	// Check allowed flags (and optionally constrain positional args by prefix)
	if node.AllowedFlags != nil {
		effectiveRemaining := remaining
		if len(node.FlagsWithValue) > 0 {
			effectiveRemaining = stripFlagsWithValue(remaining, node.FlagsWithValue)
		}

		if len(node.AllowedArgPrefixes) > 0 {
			flags, positionals := splitFlagsAndArgs(effectiveRemaining)
			if len(positionals) == 0 {
				return "", ""
			}
			if !allArgsMatchPrefix(positionals, node.AllowedArgPrefixes) {
				return "", ""
			}
			if checkAllowedFlags(flags, node.AllowedFlags) {
				return "allow", ""
			}
		} else if checkAllowedFlags(remaining, node.AllowedFlags) {
			return "allow", ""
		}
	}

	return "", ""
}

func matchesName(nameField interface{}, target string) bool {
	switch v := nameField.(type) {
	case string:
		return v == target
	case []interface{}:
		for _, name := range v {
			if s, ok := name.(string); ok && s == target {
				return true
			}
		}
	}
	return false
}

func checkAllowedFlags(args []string, allowedFlags interface{}) bool {
	switch v := allowedFlags.(type) {
	case string:
		return v == "*"
	case []interface{}:
		allowed := set.New[string]()
		for _, f := range v {
			if s, ok := f.(string); ok {
				allowed.Add(s)
			}
		}
		// Check all flags are allowed
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") && !allowed.Contains(arg) {
				return false
			}
		}
		return true
	}
	return false
}

func parseAllCommands(command string) [][]string {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}

	// Reject any command with output redirections (>, >>, etc.)
	if hasOutputRedirect(file) {
		return nil
	}

	var allCommands [][]string
	for _, stmt := range file.Stmts {
		commands := extractCommands(stmt.Cmd)
		if commands == nil {
			return nil
		}
		allCommands = append(allCommands, commands...)
	}

	return allCommands
}

func extractCommands(cmd syntax.Command) [][]string {
	if cmd == nil {
		return nil
	}

	// Check for dangerous constructs
	if hasDangerousConstruct(cmd) {
		return nil
	}

	switch c := cmd.(type) {
	case *syntax.CallExpr:
		args := extractCallArgs(c)
		if args == nil {
			return nil
		}
		return [][]string{args}

	case *syntax.BinaryCmd:
		// Handle &&, ||, |
		left := extractCommands(c.X.Cmd)
		if left == nil {
			return nil
		}
		right := extractCommands(c.Y.Cmd)
		if right == nil {
			return nil
		}
		return append(left, right...)

	default:
		return nil
	}
}

func extractCallArgs(call *syntax.CallExpr) []string {
	var args []string
	for _, word := range call.Args {
		arg := extractWord(word)
		if arg == "" {
			return nil
		}
		args = append(args, arg)
	}
	return args
}

func extractWord(word *syntax.Word) string {
	var parts []string
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			parts = append(parts, p.Value)
		case *syntax.SglQuoted:
			parts = append(parts, p.Value)
		case *syntax.DblQuoted:
			// Double quotes are OK if they only contain literals
			for _, qpart := range p.Parts {
				if lit, ok := qpart.(*syntax.Lit); ok {
					parts = append(parts, lit.Value)
				} else {
					return "" // Contains variable expansion or similar
				}
			}
		default:
			return "" // Unknown part type
		}
	}
	return strings.Join(parts, "")
}

// extractExecSubCommands extracts sub-commands from exec-style flags.
// e.g., for args ["-name", "*.h", "-exec", "grep", "-l", "pattern", "{}", ";"]
// with execFlags ["-exec"], returns [["grep", "-l", "pattern"]].
func extractExecSubCommands(args []string, execFlags []string) [][]string {
	flagSet := set.New[string]()
	for _, f := range execFlags {
		flagSet.Add(f)
	}

	var result [][]string
	for i := 0; i < len(args); i++ {
		if !flagSet.Contains(args[i]) {
			continue
		}
		// Collect args until ";" or "+"
		var subCmd []string
		i++
		for i < len(args) {
			a := args[i]
			if a == ";" || a == "+" {
				break
			}
			if a != "{}" {
				subCmd = append(subCmd, a)
			}
			i++
		}
		if len(subCmd) > 0 {
			result = append(result, subCmd)
		}
	}
	return result
}

func hasOutputRedirect(node syntax.Node) bool {
	found := false
	syntax.Walk(node, func(n syntax.Node) bool {
		if stmt, ok := n.(*syntax.Stmt); ok {
			for _, r := range stmt.Redirs {
				switch r.Op {
				case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll,
					syntax.DplOut, syntax.ClbOut, syntax.RdrInOut:
					if isAllowedRedirect(r) {
						continue
					}
					found = true
					return false
				}
			}
		}
		return !found
	})
	return found
}

// isAllowedRedirect reports whether a redirect operation is safe to auto-allow.
// Permitted: merging stderr into stdout, and stdout/stderr writes to /dev/null
// or under /tmp/.
func isAllowedRedirect(r *syntax.Redirect) bool {
	fd := "1"
	if r.N != nil {
		fd = r.N.Value
	}
	target := redirectTarget(r)

	// stderr merged into stdout.
	if r.Op == syntax.DplOut {
		return fd == "2" && target == "1"
	}

	switch r.Op {
	case syntax.RdrOut, syntax.AppOut:
		if fd != "1" && fd != "2" {
			return false
		}
	case syntax.RdrAll, syntax.AppAll:
		// &> and &>> redirect both stdout and stderr
	default:
		return false
	}

	return isSafeRedirectPath(target)
}

// isSafeRedirectPath reports /dev/null or a path under /tmp/. path.Clean
// defeats traversal.
func isSafeRedirectPath(target string) bool {
	if target == "/dev/null" {
		return true
	}
	return strings.HasPrefix(path.Clean(target), "/tmp/")
}

func redirectTarget(r *syntax.Redirect) string {
	if r.Word == nil {
		return ""
	}
	var parts []string
	for _, p := range r.Word.Parts {
		lit, ok := p.(*syntax.Lit)
		if !ok {
			return ""
		}
		parts = append(parts, lit.Value)
	}
	return strings.Join(parts, "")
}

func hasDangerousConstruct(node syntax.Node) bool {
	dangerous := false
	syntax.Walk(node, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.CmdSubst, *syntax.ParamExp, *syntax.ArithmExp, *syntax.ProcSubst:
			dangerous = true
			return false
		}
		return true
	})
	return dangerous
}

func hasAnyFlag(args []string, flags []string) bool {
	flagSet := set.New[string]()
	for _, f := range flags {
		flagSet.Add(f)
	}
	for _, arg := range args {
		if flagSet.Contains(arg) {
			return true
		}
		// Also match --flag=value form.
		if idx := strings.Index(arg, "="); idx > 0 && flagSet.Contains(arg[:idx]) {
			return true
		}
	}
	return false
}

func stripFlagsWithValue(args []string, flags []string) []string {
	flagSet := set.New[string]()
	for _, f := range flags {
		flagSet.Add(f)
	}
	var result []string
	for i := 0; i < len(args); i++ {
		if flagSet.Contains(args[i]) && i+1 < len(args) {
			i++ // skip the value too
			continue
		}
		result = append(result, args[i])
	}
	return result
}

func getFlagValue(args []string, flags []string) string {
	flagSet := set.New[string]()
	for _, f := range flags {
		flagSet.Add(f)
	}
	for i, arg := range args {
		if flagSet.Contains(arg) && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func splitFlagsAndArgs(args []string) (flags, positionals []string) {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		} else {
			positionals = append(positionals, arg)
		}
	}
	return
}

func allArgsMatchPrefix(args []string, prefixes []string) bool {
	for _, arg := range args {
		matched := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(arg, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func outputDecision(event, behavior, message string) {
	if event == eventPreToolUse {
		resp := PreToolUseResponse{}
		resp.HookSpecificOutput.HookEventName = eventPreToolUse
		resp.HookSpecificOutput.PermissionDecision = behavior
		resp.HookSpecificOutput.PermissionDecisionReason = message
		json.NewEncoder(os.Stdout).Encode(resp)
		return
	}

	resp := PermissionResponse{}
	resp.HookSpecificOutput.HookEventName = eventPermissionRequest
	resp.HookSpecificOutput.Decision.Behavior = behavior
	if message != "" {
		resp.HookSpecificOutput.Decision.Message = message
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}
