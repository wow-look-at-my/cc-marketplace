// no-work-loss: a PreToolUse hook that refuses the two ways a session loses
// authorship of the working tree.
//
//   - Destruction: a command that would destroy content existing only in the
//     working tree. A modified-but-uncommitted or untracked file is in no git
//     object, so losing it is unrecoverable; committed work is reachable from
//     the reflog and is deliberately NOT protected.
//   - Provenance: a change to file content that does not go through Write, Edit
//     or NotebookEdit. Bash runs things -- git, builds, tests, validation,
//     search -- and does not author files.
//
// Both questions are asked of the same parsed command, which is why they live in
// one plugin: the shell walk, the wrapper stripping and the path resolution are
// the same machinery, and two copies of it would drift.
// see docs/decision-model.md and docs/write-routes.md
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
		// Reading the payload failed, so nothing is known about the call. This is
		// the one place neither half can fail closed: with no payload there is no
		// decision to emit and no reason to attach to it.
		return
	}
	if reason := decide(raw); reason != "" {
		emitDeny(reason)
	}
}

// evaluateLoss runs the destruction analysis under a recover. This half fails
// OPEN on a panic unless the command names a destructive verb, because a bug
// while checking `ls` must not wedge a session -- the opposite posture from
// evaluateWrites, and deliberately so: one half refuses what it cannot verify,
// the other only refuses what it can see is dangerous.
func evaluateLoss(command, cwd string) (reason string) {
	// Cheap byte scan first: the overwhelming majority of Bash calls name no verb
	// that can delete anything, and those must not pay for a parse or a
	// subprocess.
	if command == "" || !mayDestroy(command) {
		return ""
	}
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
