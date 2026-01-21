package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	Name             interface{}       `json:"name"` // string or []string
	Description      string            `json:"description,omitempty"`
	AllowedFlags     interface{}       `json:"allowedFlags,omitempty"` // "*" or []string
	RequiredFlags    []string          `json:"requiredFlags,omitempty"`
	RequireFlagValue *RequireFlagRule  `json:"requireFlagValue,omitempty"`
	DenyWithMessage  string            `json:"denyWithMessage,omitempty"`
	Subcommands      []CommandNode     `json:"subcommands,omitempty"`
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

// Parsed command from shfmt
type ShfmtFile struct {
	Type  string      `json:"Type"`
	Stmts []ShfmtStmt `json:"Stmts"`
}

type ShfmtStmt struct {
	Cmd ShfmtCmd `json:"Cmd"`
}

type ShfmtCmd struct {
	Type string      `json:"Type"`
	Args []ShfmtWord `json:"Args"`
}

type ShfmtWord struct {
	Parts []ShfmtPart `json:"Parts"`
}

type ShfmtPart struct {
	Type  string `json:"Type"`
	Value string `json:"Value"`
}

var rules Rules

func main() {
	input, _ := io.ReadAll(os.Stdin)
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		os.Exit(0)
	}

	if hi.HookEventName != "PermissionRequest" || hi.ToolName != "Bash" {
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
	args := parseCommand(command)
	if len(args) == 0 {
		return "", ""
	}

	// Find matching command node and evaluate recursively
	return evaluateArgs(args, rules.Commands)
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

func parseCommand(command string) []string {
	cmd := exec.Command("shfmt", "--to-json")
	cmd.Stdin = strings.NewReader(command)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var ast ShfmtFile
	if err := json.Unmarshal(output, &ast); err != nil {
		return nil
	}

	if len(ast.Stmts) != 1 {
		return nil
	}

	stmt := ast.Stmts[0]
	if stmt.Cmd.Type != "CallExpr" {
		return nil
	}

	// Check for dangerous constructs
	if hasDangerousConstruct(output) {
		return nil
	}

	var args []string
	for _, word := range stmt.Cmd.Args {
		arg := extractWord(word)
		if arg == "" {
			return nil
		}
		args = append(args, arg)
	}

	return args
}

func extractWord(word ShfmtWord) string {
	var parts []string
	for _, part := range word.Parts {
		switch part.Type {
		case "Lit", "SglQuoted":
			parts = append(parts, part.Value)
		default:
			return ""
		}
	}
	return strings.Join(parts, "")
}

func hasDangerousConstruct(data []byte) bool {
	dangerous := []string{`"Type":"CmdSubst"`, `"Type":"ParamExp"`, `"Type":"ArithExp"`, `"Type":"ProcSubst"`}
	s := string(data)
	for _, d := range dangerous {
		if strings.Contains(s, d) {
			return true
		}
	}
	return false
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
