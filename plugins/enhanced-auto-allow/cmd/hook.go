package main

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path"
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
	Name              interface{}      `json:"name"` // string or []string
	Description       string           `json:"description,omitempty"`
	AllowedFlags      interface{}      `json:"allowedFlags,omitempty"` // "*" or []string
	DeniedFlags       []string         `json:"deniedFlags,omitempty"`
	ExecFlags         []string         `json:"execFlags,omitempty"`
	RequiredFlags     []string         `json:"requiredFlags,omitempty"`
	RequireFlagValue  *RequireFlagRule `json:"requireFlagValue,omitempty"`
	DenyWithMessage   string           `json:"denyWithMessage,omitempty"`
	FlagsWithValue    []string         `json:"flagsWithValue,omitempty"`
	HelpAlwaysAllowed bool             `json:"helpAlwaysAllowed,omitempty"`
	BareOnly          bool             `json:"bareOnly,omitempty"`
	DenyArgSubstrings []string         `json:"denyArgSubstrings,omitempty"`
	Subcommands       []CommandNode    `json:"subcommands,omitempty"`
}

type RequireFlagRule struct {
	Flags   []string `json:"flags"`
	Default string   `json:"default"`
	Allowed []string `json:"allowed"`
}

// XML parsing types

type xmlTest struct {
	Command  string `xml:"cmd,attr"`
	Expected string `xml:"expect,attr"`
}

type xmlRules struct {
	XMLName  xml.Name     `xml:"rules"`
	Tests    []xmlTest    `xml:"test"`
	Commands []xmlCommand `xml:"cmd"`
}

type xmlCommand struct {
	Name              string          `xml:"name,attr"`
	Description       string          `xml:"description,attr,omitempty"`
	AllowedFlagsAttr  string          `xml:"allowedFlags,attr,omitempty"`
	DenyWithMessage   string          `xml:"denyWithMessage,attr,omitempty"`
	HelpAlwaysAllowed bool            `xml:"helpAlwaysAllowed,attr,omitempty"`
	BareOnly          bool            `xml:"bareOnly,attr,omitempty"`
	Tests             []xmlTest       `xml:"test"`
	AllowedFlags      *xmlFlagList    `xml:"allowedFlags"`
	DeniedFlags       *xmlFlagList    `xml:"deniedFlags"`
	ExecFlags         *xmlFlagList    `xml:"execFlags"`
	RequiredFlags     *xmlFlagList    `xml:"requiredFlags"`
	FlagsWithValue    *xmlFlagList    `xml:"flagsWithValue"`
	DenyArgSubstrings *xmlStringList  `xml:"denyArgSubstrings"`
	RequireFlagValue  *xmlRequireFlag `xml:"requireFlagValue"`
	Subcommands       []xmlCommand    `xml:"subcmd"`
}

type xmlFlagList struct {
	Flags []xmlFlag `xml:"flag"`
}

type xmlFlag struct {
	Name string `xml:"name,attr"`
}

type xmlStringList struct {
	Values []string `xml:"value"`
}

type xmlRequireFlag struct {
	Default string    `xml:"default,attr"`
	Flags   []xmlFlag `xml:"flag"`
	Allowed []string  `xml:"allowed"`
}

func loadXMLRules(data []byte) (Rules, error) {
	var xr xmlRules
	if err := xml.Unmarshal(data, &xr); err != nil {
		return Rules{}, err
	}
	var r Rules
	for _, xc := range xr.Commands {
		r.Commands = append(r.Commands, convertXMLCommand(xc))
	}
	return r, nil
}

func convertXMLCommand(xc xmlCommand) CommandNode {
	node := CommandNode{
		Description:       xc.Description,
		DenyWithMessage:   xc.DenyWithMessage,
		HelpAlwaysAllowed: xc.HelpAlwaysAllowed,
		BareOnly:          xc.BareOnly,
	}

	names := strings.Split(xc.Name, ",")
	if len(names) == 1 {
		node.Name = names[0]
	} else {
		ifaces := make([]interface{}, len(names))
		for i, n := range names {
			ifaces[i] = strings.TrimSpace(n)
		}
		node.Name = ifaces
	}

	if xc.AllowedFlagsAttr == "*" {
		node.AllowedFlags = "*"
	} else if xc.AllowedFlags != nil {
		flags := make([]interface{}, len(xc.AllowedFlags.Flags))
		for i, f := range xc.AllowedFlags.Flags {
			flags[i] = f.Name
		}
		node.AllowedFlags = flags
	}

	node.DeniedFlags = xmlFlagNames(xc.DeniedFlags)
	node.ExecFlags = xmlFlagNames(xc.ExecFlags)
	node.RequiredFlags = xmlFlagNames(xc.RequiredFlags)
	node.FlagsWithValue = xmlFlagNames(xc.FlagsWithValue)

	if xc.DenyArgSubstrings != nil {
		node.DenyArgSubstrings = xc.DenyArgSubstrings.Values
	}

	if xc.RequireFlagValue != nil {
		rfv := &RequireFlagRule{
			Default: xc.RequireFlagValue.Default,
			Allowed: xc.RequireFlagValue.Allowed,
		}
		for _, f := range xc.RequireFlagValue.Flags {
			rfv.Flags = append(rfv.Flags, f.Name)
		}
		node.RequireFlagValue = rfv
	}

	for _, xs := range xc.Subcommands {
		node.Subcommands = append(node.Subcommands, convertXMLCommand(xs))
	}

	return node
}

func xmlFlagNames(fl *xmlFlagList) []string {
	if fl == nil {
		return nil
	}
	names := make([]string, len(fl.Flags))
	for i, f := range fl.Flags {
		names[i] = f.Name
	}
	return names
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

	// Try all matching nodes and merge results.
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

	// Check deny with message first
	if node.DenyWithMessage != "" {
		return "deny", node.DenyWithMessage
	}

	// If any argument contains a denied substring, this node does not match.
	// Used for tools that accept script arguments (awk, sed, etc.) where
	// dangerous features (system(), getline, I/O redirection) appear as
	// substrings of the script body.
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
		// If the first remaining arg looks like a subcommand (not a flag)
		// but didn't match any known subcommand, don't fall through to
		// allowedFlags - it's an unknown/mutating subcommand.
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
// Permitted: 2>&1, and stdout/stderr writes to /dev/null or under /tmp/.
func isAllowedRedirect(r *syntax.Redirect) bool {
	fd := "1"
	if r.N != nil {
		fd = r.N.Value
	}
	target := redirectTarget(r)

	// 2>&1
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

// isSafeRedirectPath reports whether target is /dev/null or a path under /tmp/.
// path.Clean defeats traversal attempts like /tmp/../etc/passwd.
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
	flagSet := make(map[string]bool)
	for _, f := range flags {
		flagSet[f] = true
	}
	for _, arg := range args {
		if flagSet[arg] {
			return true
		}
		// Also match --flag=value form.
		if idx := strings.Index(arg, "="); idx > 0 && flagSet[arg[:idx]] {
			return true
		}
	}
	return false
}

func stripFlagsWithValue(args []string, flags []string) []string {
	flagSet := make(map[string]bool, len(flags))
	for _, f := range flags {
		flagSet[f] = true
	}
	var result []string
	for i := 0; i < len(args); i++ {
		if flagSet[args[i]] && i+1 < len(args) {
			i++ // skip the value too
			continue
		}
		result = append(result, args[i])
	}
	return result
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
