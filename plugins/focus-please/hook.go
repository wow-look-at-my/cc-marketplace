// Command focus-please is a Claude Code hook that enforces one blunt rule:
// when the user's prompt contains a question mark, every tool call is
// blocked until the assistant answers the user in plain text. It is a
// mechanical "answer the human first" guard -- the big-guns response to an
// assistant that runs tools for ten minutes while a question goes ignored.
//
// A single binary serves three hook events, dispatched on hook_event_name:
//
//   - UserPromptSubmit: if the submitted prompt contains "?", drop a
//     per-session marker file and inject a context note telling the model
//     every tool is blocked until it replies. A prompt with no "?" removes
//     any stale marker so tools flow normally.
//   - PreToolUse: while the marker exists, deny the tool with a reason that
//     tells the model to answer the user first. Otherwise allow.
//   - Stop: the assistant has finished its reply and is ending the turn, so
//     remove the marker; the next turn's tools are unblocked.
//
// The marker lives at <tempdir>/focus-please/<session>.pending, keyed by a
// hash of the session id so concurrent sessions never block one another and
// an odd id can never escape the marker directory. Every error path fails
// OPEN (no marker written, no denial emitted) so the plugin can never wedge
// a session shut.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HookInput is the subset of the hook payload this plugin reads; the CLI
// sends more fields and the extras are ignored.
type HookInput struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	Prompt        string `json:"prompt"`
}

// upsOutput injects context back to the model on UserPromptSubmit.
type upsOutput struct {
	HookSpecificOutput upsSpecific `json:"hookSpecificOutput"`
}

type upsSpecific struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// denyOutput blocks a tool on PreToolUse via a permission decision.
type denyOutput struct {
	HookSpecificOutput denySpecific `json:"hookSpecificOutput"`
}

type denySpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// contextNote is added to the prompt so the model learns, before it acts,
// that focus-please has engaged for this turn.
const contextNote = "Your most recent message from the user contains a question mark, so the focus-please plugin has engaged: every tool call is blocked until you answer them. Respond to the user's question directly, in plain text, before doing anything else -- do not attempt any tool use. Tools become available again once you have replied and your turn ends."

// denyReason is shown to the model when it tries to call a tool while the
// block is active.
const denyReason = "Blocked by focus-please: the user's last message contained a question and you have not answered it yet. Do not call tools right now. Reply to the user in plain text first -- the block clears automatically once you end your turn with a response."

func main() {
	fmt.Print(run(os.Stdin))
}

// run reads a hook payload from r and returns the JSON to print on stdout.
// Unparseable input or an unrecognized event yields "{}", a no-op that
// leaves the tool/turn untouched.
func run(r io.Reader) string {
	data, _ := io.ReadAll(r)
	var in HookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return "{}"
	}
	switch in.HookEventName {
	case "UserPromptSubmit":
		return onUserPromptSubmit(in)
	case "PreToolUse":
		return onPreToolUse(in)
	case "Stop":
		return onStop(in)
	default:
		return "{}"
	}
}

// onUserPromptSubmit arms the block when the prompt asks a question and
// disarms it otherwise (clearing any marker a prior turn left behind).
func onUserPromptSubmit(in HookInput) string {
	if strings.Contains(in.Prompt, "?") {
		setMarker(in.SessionID)
		out, _ := json.Marshal(upsOutput{upsSpecific{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: contextNote,
		}})
		return string(out)
	}
	clearMarker(in.SessionID)
	return "{}"
}

// onPreToolUse denies the tool while the block is armed for this session.
func onPreToolUse(in HookInput) string {
	if !markerExists(in.SessionID) {
		return "{}"
	}
	out, _ := json.Marshal(denyOutput{denySpecific{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: denyReason,
	}})
	return string(out)
}

// onStop disarms the block: the assistant has produced its reply, so the
// next turn is free to use tools.
func onStop(in HookInput) string {
	clearMarker(in.SessionID)
	return "{}"
}

// markerDir is the per-user directory holding session marker files.
func markerDir() string {
	return filepath.Join(os.TempDir(), "focus-please")
}

// markerPath maps a session id to its marker file. The id is hashed so the
// filename is always a safe, flat token; an empty id hashes to a stable
// fallback so the plugin still functions (all such sessions share it).
func markerPath(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(markerDir(), hex.EncodeToString(sum[:16])+".pending")
}

// setMarker arms the block for a session. Failure to create the directory
// or file is swallowed: a missing marker simply means tools stay allowed.
func setMarker(sessionID string) {
	if err := os.MkdirAll(markerDir(), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(markerPath(sessionID), []byte("1"), 0o644)
}

// clearMarker disarms the block for a session; a missing marker is fine.
func clearMarker(sessionID string) {
	_ = os.Remove(markerPath(sessionID))
}

// markerExists reports whether the block is armed for a session.
func markerExists(sessionID string) bool {
	_, err := os.Stat(markerPath(sessionID))
	return err == nil
}
