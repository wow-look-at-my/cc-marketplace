package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// Rules configuration - array-based recursive structure
type Rules struct {
	Commands []CommandNode `json:"commands"`
}

type CommandNode struct {
	Name             interface{}      `json:"name"` // string or []string
	Description      string           `json:"description,omitempty"`
	AllowedFlags     interface{}      `json:"allowedFlags,omitempty"` // "*" or []string
	DeniedFlags      []string         `json:"deniedFlags,omitempty"`
	ExecFlags        []string         `json:"execFlags,omitempty"`
	RequiredFlags    []string         `json:"requiredFlags,omitempty"`
	RequireFlagValue *RequireFlagRule `json:"requireFlagValue,omitempty"`
	DenyWithMessage  string           `json:"denyWithMessage,omitempty"`
	Subcommands      []CommandNode    `json:"subcommands,omitempty"`
}

type RequireFlagRule struct {
	Flags   []string `json:"flags"`
	Default string   `json:"default"`
	Allowed []string `json:"allowed"`
}

// Permission response
type PermissionResponse struct {
	HookSpecificOutput struct {
		HookEventName string `json:"hookEventName"`
		Decision      struct {
			Behavior string `json:"behavior"`
			Message  string `json:"message,omitempty"`
		} `json:"decision"`
	} `json:"hookSpecificOutput"`
}

var rules Rules

func main() {
	input, _ := io.ReadAll(os.Stdin)
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		os.Exit(0)
	}

	if hi.HookEventName != "PermissionRequest" {
		os.Exit(0)
	}

	// Allow all read-only tools
	if hi.ToolName == "Read" || hi.ToolName == "Glob" || hi.ToolName == "Grep" {
		outputDecision("allow", "")
		return
	}

	if hi.ToolName != "Bash" {
		os.Exit(0)
	}

	// Load rules from adjacent file
	rulesPath := filepath.Join(filepath.Dir(os.Args[0]), "..", "rules.json")
	rulesData, err := os.ReadFile(rulesPath)
	if err != nil {
		os.Exit(0)
	}
	if err := json.Unmarshal(rulesData, &rules); err != nil {
		os.Exit(0)
	}

	decision, message := evaluateCommand(hi.ToolInput.Command)
	if decision != "" {
		outputDecision(decision, message)
	}
}

func evaluateCommand(command string) (string, string) {
	commands := parseAllCommands(command)
	if len(commands) == 0 {
		return "", ""
	}

	// All commands must be allowed (or passthrough)
	// If any is denied, deny. If all allowed, allow. Otherwise passthrough.
	allAllowed := true
	for _, args := range commands {
		decision, msg := evaluateArgs(args, rules.Commands)
		if decision == "deny" {
			return "deny", msg
		}
		if decision != "allow" {
			allAllowed = false
		}
	}

	if allAllowed {
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

	for _, node := range nodes {
		if !matchesName(node.Name, current) {
			continue
		}

		// Check deny with message first
		if node.DenyWithMessage != "" {
			return "deny", node.DenyWithMessage
		}

		// Check required flags
		if len(node.RequiredFlags) > 0 {
			if hasAnyFlag(args, node.RequiredFlags) {
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

		// If there are subcommands, recurse
		if len(node.Subcommands) > 0 && len(remaining) > 0 {
			decision, msg := evaluateArgs(remaining, node.Subcommands)
			if decision != "" {
				return decision, msg
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
				decision, msg := evaluateArgs(subCmd, rules.Commands)
				if decision == "deny" {
					return "deny", msg
				}
				if decision != "allow" {
					return "", ""
				}
			}
		}

		// Check allowed flags
		if node.AllowedFlags != nil {
			if checkAllowedFlags(remaining, node.AllowedFlags) {
				return "allow", ""
			}
		}

		return "", ""
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
		allowed := make(map[string]bool)
		for _, f := range v {
			if s, ok := f.(string); ok {
				allowed[s] = true
			}
		}
		// Check all flags are allowed
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") && !allowed[arg] {
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
	flagSet := make(map[string]bool, len(execFlags))
	for _, f := range execFlags {
		flagSet[f] = true
	}

	var result [][]string
	for i := 0; i < len(args); i++ {
		if !flagSet[args[i]] {
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
	flagSet := make(map[string]bool)
	for _, f := range flags {
		flagSet[f] = true
	}
	for _, arg := range args {
		if flagSet[arg] {
			return true
		}
	}
	return false
}

func getFlagValue(args []string, flags []string) string {
	flagSet := make(map[string]bool)
	for _, f := range flags {
		flagSet[f] = true
	}
	for i, arg := range args {
		if flagSet[arg] && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func outputDecision(behavior, message string) {
	resp := PermissionResponse{}
	resp.HookSpecificOutput.HookEventName = "PermissionRequest"
	resp.HookSpecificOutput.Decision.Behavior = behavior
	if message != "" {
		resp.HookSpecificOutput.Decision.Message = message
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}
