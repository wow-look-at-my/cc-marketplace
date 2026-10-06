// Command focus-please is a Claude Code hook that enforces one blunt rule.
// When the user's prompt contains a question mark, the assistant must answer
// them before it does anything else. It is a mechanical "answer the human
// first" guard -- the big-guns response to an assistant that runs tools for
// several minutes while a question goes ignored.
//
// A single binary serves hook events, dispatched on hook_event_name:
//
//   - UserPromptSubmit: if the submitted prompt contains "?", arm the block
//     (a per-session marker file) and inject a context note telling the model
//     what is blocked. A prompt with no "?" disarms it. Either way, a prompt
//     that arrives while a turn is still in flight is recorded as a mid-turn
//     interjection (see Stop below).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/wow-look-at-my/go-containers/set"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HookInput is the subset of the hook payload this plugin reads; the CLI
// sends more fields and the extras are ignored.
type HookInput struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Prompt         string `json:"prompt"`
	ToolName       string `json:"tool_name"`
	StopHookActive bool   `json:"stop_hook_active"`
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

// result is what one hook invocation emits: JSON on stdout for the events
// that speak JSON.
type result struct {
	stdout string
	stderr string
	code   int
}

// noop is the do-nothing result: allow the tool, allow the stop, add no context.
func noop() result { return result{stdout: "{}"} }

// contextNote is added to the prompt so the model learns, before it acts, that focus-please has engaged for this turn. It states the rule the way the hook enforces it: the block is lifted by TEXT, not by ending the turn. Saying otherwise taught models to hand control back to the user instead of replying and carrying on.
const contextNote = "Your most recent message from the user contains a question mark, so the focus-please plugin has engaged: until you write a reply, tool calls are blocked -- except the read-only lookups Read, Grep and Glob, which stay available so you can check a file or search the code before answering. Write your reply FIRST, as ordinary text, in THIS turn. The moment that text exists every tool unblocks, in this same turn -- so if answering needs a command, say what you are about to check (one line is enough), run it, then give the real answer. Do NOT end your turn to escape the block, and do not guess at an answer you could verify: reply, then verify, then answer."

// interjectionNote is appended to contextNote when the message arrived while a turn was still running.
const interjectionNote = "You were also mid-task when this message arrived: answer it first, then resume the work it interrupted -- replying does not finish your turn."

// denyReason is shown to the model when it tries to act before writing anything. It must make the escape hatch unmistakable -- emit text and retry IN THIS TURN. This is because the wording ("...once you end your turn") convinced models the only way through was to stop and wait for the user. This is the opposite of the point.
const denyReason = "Blocked by focus-please: the user's last message contained a question and you have not written any reply yet. Emit your reply as plain text right now, in THIS turn -- one sentence is enough (\"checking X now\") -- and then call this tool again; it will go through, because this block is lifted by your text, not by the end of your turn. Do NOT end your turn to get past this, and do NOT substitute a guess for the check you were about to run. Read, Grep and Glob work meanwhile."

// resumeReason is handed to the model when it tries to end a turn that was interrupted by a user message. It fires at most once per interjection.
const resumeReason = "Do not stop here. A user message arrived while you were mid-task, so this turn is an interruption that you have now answered -- but the work you were doing when it arrived is still unfinished. Pick it back up now: re-read your plan or todo list and continue where you left off. If the user's message redirected you, follow that new direction instead. If the interrupted work is genuinely complete, say so and end your turn again -- focus-please refuses a stop only once per interruption, so your next stop always goes through."

func main() {
	res := run(os.Stdin)
	if res.stdout != "" {
		fmt.Print(res.stdout)
	}
	if res.stderr != "" {
		fmt.Fprint(os.Stderr, res.stderr)
	}
	os.Exit(res.code)
}

// run reads a hook payload from r and returns what to emit. Unparseable
// input or an unrecognized event yields a no-op that leaves the tool/turn
// untouched.
func run(r io.Reader) result {
	data, _ := io.ReadAll(r)
	var in HookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return noop()
	}
	switch in.HookEventName {
	case "UserPromptSubmit":
		return onUserPromptSubmit(in)
	case "PreToolUse":
		return onPreToolUse(in)
	case "Stop":
		return onStop(in)
	default:
		return noop()
	}
}

// onUserPromptSubmit arms the block when the prompt asks a question and
// disarms it otherwise (clearing any marker a prior turn left behind). It
// also records whether this prompt interrupted a turn that had not stopped
// yet. This is what onStop uses to push the assistant back to work.
func onUserPromptSubmit(in HookInput) result {
	// The active marker outlives a turn only until that turn's Stop.
	interjection := markerExists(in.SessionID, markerActive)
	if interjection {
		setMarker(in.SessionID, markerResume)
	}
	setMarker(in.SessionID, markerActive)

	if !strings.Contains(in.Prompt, "?") {
		clearMarker(in.SessionID, markerPending)
		return noop()
	}

	setMarker(in.SessionID, markerPending)
	note := contextNote
	if interjection {
		note += " " + interjectionNote
	}
	out, _ := json.Marshal(upsOutput{upsSpecific{
		HookEventName:     "UserPromptSubmit",
		AdditionalContext: note,
	}})
	return result{stdout: string(out)}
}

// onPreToolUse denies acting tools while the block is armed for this session.
// It lets read-only lookups through. It lifts the block as soon as the
// assistant replies, so a reply and the tools after it fit in ONE turn.
func onPreToolUse(in HookInput) result {
	if !markerExists(in.SessionID, markerPending) {
		return noop()
	}
	if isLookupTool(in.ToolName) {
		return noop()
	}
	if hasRepliedSince(in.TranscriptPath) {
		// The reply is out, so the guard has done its job.
		clearMarker(in.SessionID, markerPending)
		return noop()
	}
	out, _ := json.Marshal(denyOutput{denySpecific{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: denyReason,
	}})
	return result{stdout: string(out)}
}

// onStop lifts the question block, because the reply has happened. A user
// message can interrupt the turn. Then onStop refuses the stop once, so the
// assistant resumes the interrupted work.
func onStop(in HookInput) result {
	// However this stop resolves, the assistant has replied, so the block is over.
	clearMarker(in.SessionID, markerPending)

	// Loop guard: this stop is already the retry after a refusal of ours, so
	// let it through unconditionally. A session can never be wedged shut.
	if in.StopHookActive {
		clearMarker(in.SessionID, markerResume)
		clearMarker(in.SessionID, markerActive)
		return noop()
	}

	if markerExists(in.SessionID, markerResume) {
		// One refusal per interjection: clear the flag before refusing so the next stop goes through even.
		clearMarker(in.SessionID, markerResume)
		return result{code: 2, stderr: resumeReason}
	}

	clearMarker(in.SessionID, markerActive)
	return noop()
}

// lookupTools are the read-only tools that stay available while the block is
// armed.
var lookupTools = set.Of[string]("Read",
	"Grep",
	"Glob")

// isLookupTool reports whether a tool is one of the permitted read-only
// lookups.
func isLookupTool(name string) bool {
	if lookupTools.Contains(name) {
		return true
	}
	if strings.HasPrefix(name, "mcp__") {
		if i := strings.LastIndex(name, "__"); i > 0 {
			return lookupTools.Contains(name[i+2:])
		}
	}
	return false
}

// Marker kinds. Each is its own file under markerDir so the pieces of
// per-session state never collide.
const (
	// markerPending: a question is unanswered, so acting tools are blocked.
	markerPending = "pending"
	// markerActive: a turn is in flight (no Stop seen since its prompt).
	markerActive = "active"
	// markerResume: a user message interrupted the turn, so the next stop is refused once.
	markerResume = "resume"
)

// markerDir is the per-user directory holding session marker files.
func markerDir() string {
	return filepath.Join(os.TempDir(), "focus-please")
}

// markerPath maps a session id and marker kind to a file.
func markerPath(sessionID, kind string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(markerDir(), hex.EncodeToString(sum[:16])+"."+kind)
}

// setMarker records a marker. Failure to create the directory or file is
// swallowed: a missing marker means the guard stays open.
func setMarker(sessionID, kind string) {
	if err := os.MkdirAll(markerDir(), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(markerPath(sessionID, kind), []byte("1"), 0o644)
}

// clearMarker removes a marker; a missing marker is fine.
func clearMarker(sessionID, kind string) {
	_ = os.Remove(markerPath(sessionID, kind))
}

// markerExists reports whether a marker is set for a session.
func markerExists(sessionID, kind string) bool {
	_, err := os.Stat(markerPath(sessionID, kind))
	return err == nil
}
