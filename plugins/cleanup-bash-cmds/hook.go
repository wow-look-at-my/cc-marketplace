package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

type HookInput struct {
	HookEventName string    `json:"hook_event_name"`
	ToolName      string    `json:"tool_name"`
	ToolInput     ToolInput `json:"tool_input"`
}

type ToolInput struct {
	Command     string `json:"command"`
	Timeout     int    `json:"timeout,omitempty"`
	Description string `json:"description,omitempty"`
}

type HookOutput struct {
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput"`
}

type HookSpecificOutput struct {
	HookEventName      string    `json:"hookEventName"`
	PermissionDecision string    `json:"permissionDecision"`
	UpdatedInput       ToolInput `json:"updatedInput"`
}

var (
	trailingRedirectOut = regexp.MustCompile(`\s+2>&1\s*$`)
	trailingDevNull     = regexp.MustCompile(`\s+2>/dev/null\s*$`)
	trailingOrTrue      = regexp.MustCompile(`\s*\|\|\s*true\s*$`)
	leadingSetE         = regexp.MustCompile(`^set\s+-e\s*;\s*`)
	leadingSetEAnd      = regexp.MustCompile(`^set\s+-e\s*&&\s*`)
	trailingHead        = regexp.MustCompile(`\s*\|\s*head(\s+[^\s|]+)*\s*$`)
	trailingTail        = regexp.MustCompile(`\s*\|\s*tail(\s+[^\s|]+)*\s*$`)
)

// cleanCommand applies cleanup rules to a bash command, removing unnecessary
// patterns that the model tends to add. Rules are applied in a loop until
// the command stabilizes.
func cleanCommand(cmd string) string {
	for range 10 {
		prev := cmd
		cmd = strings.TrimSpace(cmd)
		cmd = leadingSetE.ReplaceAllString(cmd, "")
		cmd = leadingSetEAnd.ReplaceAllString(cmd, "")
		cmd = trailingOrTrue.ReplaceAllString(cmd, "")
		cmd = trailingRedirectOut.ReplaceAllString(cmd, "")
		cmd = trailingDevNull.ReplaceAllString(cmd, "")
		cmd = trailingHead.ReplaceAllString(cmd, "")
		cmd = trailingTail.ReplaceAllString(cmd, "")
		cmd = strings.TrimSpace(cmd)
		if cmd == prev {
			break
		}
	}
	return cmd
}

// evaluate processes a PreToolUse hook input and returns the exit code,
// stderr message, and stdout JSON (if the command was rewritten).
func evaluate(input []byte) (int, string, string) {
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		return 0, "", ""
	}

	if hi.ToolName != "Bash" {
		return 0, "", ""
	}

	cmd := hi.ToolInput.Command
	if cmd == "" {
		return 0, "", ""
	}

	cleaned := cleanCommand(cmd)
	if cleaned == cmd {
		return 0, "", ""
	}

	out := HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
			UpdatedInput: ToolInput{
				Command:     cleaned,
				Timeout:     hi.ToolInput.Timeout,
				Description: hi.ToolInput.Description,
			},
		},
	}

	data, err := json.Marshal(out)
	if err != nil {
		return 0, "", ""
	}

	return 0, "", string(data)
}

// run reads stdin and returns the exit code, stderr message, and stdout output.
func run(r io.Reader) (int, string, string) {
	input, _ := io.ReadAll(r)
	return evaluate(input)
}

func main() {
	code, stderr, stdout := run(os.Stdin)
	if stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	os.Exit(code)
}
