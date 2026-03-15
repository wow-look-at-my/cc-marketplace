package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
)

type HookInput struct {
	HookEventName string    `json:"hook_event_name"`
	ToolName      string    `json:"tool_name"`
	ToolInput     ToolInput `json:"tool_input"`
}

type ToolInput struct {
	Command string `json:"command"`
}

var goBuildPattern = regexp.MustCompile(`\bgo\s+(build|test)\b`)

// evaluate checks whether a Bash command contains a direct go build/test invocation.
// Returns exit code (0 = allow, 2 = block) and a message for stderr when blocking.
func evaluate(input []byte) (int, string) {
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		return 0, ""
	}

	if hi.ToolName != "Bash" {
		return 0, ""
	}

	cmd := hi.ToolInput.Command
	if cmd == "" {
		return 0, ""
	}

	if !goBuildPattern.MatchString(cmd) {
		return 0, ""
	}

	return 2, fmt.Sprintf("BLOCKED: Direct `go build` and `go test` are not allowed. Use `go-toolchain` instead — it builds, tests, vets, and checks coverage in one command.")
}

// run reads stdin and returns the exit code and stderr message.
func run(r io.Reader) (int, string) {
	input, _ := io.ReadAll(r)
	return evaluate(input)
}

func main() {
	code, msg := run(os.Stdin)
	if msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
	os.Exit(code)
}
