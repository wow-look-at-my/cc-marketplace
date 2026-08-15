// no-work-loss: a PreToolUse hook that refuses Bash commands which would
// destroy content that exists only in the working tree.
//
// The invariant: a modified-but-uncommitted or untracked file is in no git
// object, so losing it is unrecoverable. Committed work is reachable from the
// reflog and is deliberately NOT protected here.
// see docs/decision-model.md
package main

import (
	"encoding/json"
	"io"
	"os"
)

type hookInput struct {
	HookEventName string    `json:"hook_event_name"`
	ToolName      string    `json:"tool_name"`
	Cwd           string    `json:"cwd"`
	ToolInput     toolInput `json:"tool_input"`
}

type toolInput struct {
	Command string `json:"command"`
}

// The CLI rejects a payload whose hookEventName is not the event it
// dispatched, so the shape follows the event rather than the verdict.
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
		return
	}
	if reason := decide(raw); reason != "" {
		emitDeny(reason)
	}
}

// decide returns the denial reason, or "" to stay out of the way. Split from
// main so the tests drive the real entry path rather than a rewritten copy.
func decide(raw []byte) string {
	var in hookInput
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	if in.HookEventName != "PreToolUse" || in.ToolName != "Bash" {
		return ""
	}
	command := in.ToolInput.Command
	if command == "" {
		return ""
	}
	// Cheap byte scan first: the overwhelming majority of Bash calls name no
	// verb that can delete anything, and those must not pay for a parse or a
	// subprocess.
	if !mayDestroy(command) {
		return ""
	}
	return evaluate(command, in.Cwd)
}

// evaluate runs the real analysis under a recover, because this hook's whole
// purpose is defeated if a panic inside it lets a destructive command through.
// A crash on a command that names a destructive verb denies; anything else
// stays open so a bug here cannot brick a session.
func evaluate(command, cwd string) (reason string) {
	defer func() {
		if r := recover(); r != nil {
			reason = ""
			if verb, ok := destructiveKeyword(command); ok {
				reason = internalErrorReason(verb)
			}
		}
	}()
	return analyze(command, cwd)
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
