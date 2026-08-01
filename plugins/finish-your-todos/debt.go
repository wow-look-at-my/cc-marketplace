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

// A slash command is the CLI's own control surface, not an assignment.
var slashCommand = regexp.MustCompile(`^\s*/`)

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
// things that get a pass are pure questions, bare acknowledgements, and slash
// commands -- and a question that also contains an imperative ("why is X? fix
// it") still arms, because the imperative is the part that gets forgotten.
func assignsWork(prompt string) bool {
	text := strings.TrimSpace(prompt)
	if text == "" || slashCommand.MatchString(text) || ackOnly.MatchString(text) {
		return false
	}
	isQuestion := whOpeners.MatchString(text) || (auxOpeners.MatchString(text) && strings.Contains(text, "?"))
	return !isQuestion || hasImperative(text)
}

// hookPayload is the subset of the hook JSON both entry-side halves read.
type hookPayload struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
	ToolName  string `json:"tool_name"`
	EventName string `json:"hook_event_name"`
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
