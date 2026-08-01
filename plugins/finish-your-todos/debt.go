// Shared state + classification for the entry-side halves of this plugin.
//
// The problem this exists for: the model is told, by system reminder, on most
// turns, to keep a task list. It reads the reminder and does not do it -- a
// whole session went by with five separate assignments given and zero tasks
// filed, because a reminder is text and text is skippable. So the debt is
// recorded in a file and collected by a PreToolUse gate: no other tool runs
// until the task exists.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Debt is the per-session marker: an assignment that has no task yet.
type Debt struct {
	// Prompt is the message that assigned work, trimmed, for the block message.
	Prompt string `json:"prompt"`
	// Refusals counts tool calls refused while this debt stands.
	Refusals int `json:"refusals"`
}

// debtPath is keyed by a hash of the session id so a session id that is a path
// fragment, or merely long, cannot escape or overflow the temp directory.
func debtPath(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(os.TempDir(), "force-todos", hex.EncodeToString(sum[:])[:16]+".json")
}

// readDebt returns nil when there is no outstanding debt, including when the
// marker is unreadable or corrupt: a broken guard must fail open.
func readDebt(sessionID string) *Debt {
	data, err := os.ReadFile(debtPath(sessionID))
	if err != nil {
		return nil
	}
	var d Debt
	if err := json.Unmarshal(data, &d); err != nil {
		return nil
	}
	return &d
}

// writeDebt records the marker. Losing it costs enforcement, not correctness,
// so every failure is swallowed.
func writeDebt(sessionID string, d Debt) {
	path := debtPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(d)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func clearDebt(sessionID string) {
	_ = os.Remove(debtPath(sessionID))
}

// taskTools must stay callable while a debt is outstanding, so the settling
// call can check for duplicates first.
var taskTools = map[string]bool{
	"TaskCreate": true, "TaskUpdate": true, "TaskList": true, "TaskGet": true,
}

// settlingTools pay the debt: filing new work, or claiming/retargeting work
// that already exists.
var settlingTools = map[string]bool{"TaskCreate": true, "TaskUpdate": true}

// A wh-word opens a question whether or not the "?" was typed.
var whOpeners = regexp.MustCompile(`(?i)^\s*(what|why|how|when|where|which|who|whose)\b`)

// An auxiliary opens a question only WITH a "?" present. Without one it is
// nearly always an instruction -- "do the thing", "can you add a test",
// "should we block python" -- and reading those as questions is precisely how
// an assignment goes unfiled, which is the failure this plugin exists to stop.
var auxOpeners = regexp.MustCompile(`(?i)^\s*(is|are|was|were|do|does|did|can|could|should|would|will|has|have|had|am)\b`)

var ackOnly = regexp.MustCompile(`(?i)^\s*(ok(ay)?|k|yes|yep|yeah|no|nope|sure|thanks|thank you|ty|nice|cool|great|perfect|good|lgtm|ship it|go ahead|continue|proceed|please do|sounds good|👍|\+1)[\s.!?]*$`)

// A slash command is the CLI's own control surface -- but its ARGUMENTS are
// not. The hook never sees a command's expansion, only the raw `/name args`
// the user typed, so `/goal fix the flaky test` arrives as exactly that string.
// Skipping the whole line on the leading "/" is how assignments handed over
// that way went unfiled for a whole session. Strip the command word and judge
// what is left; a bare `/review` leaves nothing and passes through as before.
var slashCommand = regexp.MustCompile(`^\s*/\S*\s*`)

// stripSlashCommand removes a leading `/command` token and returns the
// argument text. Input with no leading slash is returned unchanged.
func stripSlashCommand(text string) string {
	if prefix := slashCommand.FindString(text); prefix != "" {
		return strings.TrimSpace(text[len(prefix):])
	}
	return text
}

// commandArgs splits a slash command into its argument text.
func commandArgs(text string) (args string, isCommand bool) {
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return "", false
	}
	return stripSlashCommand(strings.TrimSpace(text)), true
}

// argWordFloor is where command arguments stop looking like a parameter and
// start looking like prose. Settings and knobs are short -- "high", "5m /foo";
// an assignment typed at a command is a sentence.
const argWordFloor = 5

// assignsWorkAsCommand judges the ARGUMENTS of a slash command.
//
// The permissive rule used for prose cannot be reused here. Prose that is not a
// question is almost always an instruction, but command arguments are just as
// often parameters -- "/effort high", "/loop 5m /foo" -- and treating those as
// assignments would arm the gate on routine control input. So an imperative
// carries it on its own, and otherwise the arguments have to read like a
// sentence rather than a setting.
func assignsWorkAsCommand(args string) bool {
	if args == "" {
		return false
	}
	if whOpeners.MatchString(args) || (auxOpeners.MatchString(args) && strings.Contains(args, "?")) {
		// A question typed at a command is still a question -- unless it also
		// carries an instruction, which is the part that gets forgotten.
		return hasImperative(args)
	}
	return hasImperative(args) || len(strings.Fields(args)) >= argWordFloor
}

// Verbs that mean "do something to the codebase". Matched at the start of any
// line or clause, which is where an instruction actually sits.
var imperatives = []string{
	"add", "block", "build", "change", "check", "clean", "commit", "create", "delete", "deny",
	"deploy", "disable", "document", "drop", "enable", "enforce", "extract", "fix", "handle",
	"implement", "install", "make", "merge", "move", "open", "port", "publish", "push", "refactor",
	"remove", "rename", "replace", "revert", "rewrite", "run", "set", "split", "stop", "switch",
	"test", "update", "upgrade", "use", "verify", "wire", "write", "wrap",
}

var imperativePattern = regexp.MustCompile(
	`(?i)(^|[.!?;\n]\s*|\b(?:please|also|then|and|now|just|can you|could you|make sure (?:you|to)|i want you to|i need you to)\s+)(` +
		strings.Join(imperatives, "|") + `)\b`)

func hasImperative(text string) bool { return imperativePattern.MatchString(text) }

// assignsWork reports whether a prompt hands the session something to do.
//
// Deliberately biased toward YES. A false positive costs one TaskCreate call;
// a false negative is the exact failure this plugin exists to stop. The only
// things that get a pass are pure questions, bare acknowledgements, and bare
// slash commands -- and a question that also contains an imperative ("why is
// X? fix it") still arms, because the imperative is the part that gets
// forgotten.
func assignsWork(prompt string) bool {
	text := strings.TrimSpace(prompt)
	if args, isCommand := commandArgs(text); isCommand {
		return assignsWorkAsCommand(args)
	}
	if text == "" || ackOnly.MatchString(text) {
		return false
	}
	isQuestion := whOpeners.MatchString(text) || (auxOpeners.MatchString(text) && strings.Contains(text, "?"))
	return !isQuestion || hasImperative(text)
}

// hookPayload is the subset of the hook JSON the entry-side halves read.
type hookPayload struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
	ToolName  string `json:"tool_name"`
	EventName string `json:"hook_event_name"`
	// TranscriptPath is how the PreToolUse gate sees assignments that never
	// reached UserPromptSubmit at all -- see interject.go.
	TranscriptPath string `json:"transcript_path"`
}

// parsePayload fails open: garbage stdin yields an empty session id, which
// every caller treats as "do not enforce".
func parsePayload(raw []byte) hookPayload {
	var p hookPayload
	_ = json.Unmarshal(raw, &p)
	return p
}

var whitespaceRun = regexp.MustCompile(`\s+`)

// summarize collapses a prompt to one line short enough to quote in a refusal.
func summarize(prompt string) string {
	s := whitespaceRun.ReplaceAllString(strings.TrimSpace(prompt), " ")
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}
