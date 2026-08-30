// edits-through-tools: every change to file content under the working tree goes
// through Write, Edit or NotebookEdit. Bash runs things -- git, builds, tests,
// validation, search -- and does not author files.
//
// The hook parses each Bash command, resolves every path it would write, and
// denies when one lands inside the tree. It also closes the routes that are not
// Bash at all: a subagent handed a wider grant, a server-side commit through the
// GitHub API, and a session editing the live settings that gate it.
// see docs/decision-model.md
package main

import (
	"encoding/json"
	"io"
	"os"
)

type hookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	Cwd           string          `json:"cwd"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// The CLI rejects a payload whose hookEventName is not the event it dispatched,
// so the shape follows the event rather than the verdict.
type preToolUseResponse struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		// Reading the payload failed, so nothing is known about the call. This
		// is the one place the hook cannot fail closed: with no payload there is
		// no decision to emit and no reason to attach to it.
		return
	}
	if reason := decide(raw); reason != "" {
		emitDeny(reason)
	}
}

func emitDeny(reason string) {
	var resp preToolUseResponse
	resp.HookSpecificOutput.HookEventName = "PreToolUse"
	resp.HookSpecificOutput.PermissionDecision = "deny"
	resp.HookSpecificOutput.PermissionDecisionReason = reason
	out, err := json.Marshal(resp)
	if err != nil {
		return
	}
	os.Stdout.Write(out)
}
