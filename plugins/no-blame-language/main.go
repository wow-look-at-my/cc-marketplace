// Command no-blame-language is a Claude Code MessageDisplay hook. It marks a
// closing message that deflects a defect instead of owning it, by appending one
// line to what the reader sees. It sends NOTHING back to the model.
//
// "Pre-existing", "not my problem", "out of scope", "flagging this for you",
// "that predates this session": each of these reports a finding and stops
// there, or shifts blame for code in this org's own repos onto some other
// author or an earlier point in time. This org's own written convention bans
// that shape of sentence -- found it, fix it, or say precisely why you are not
// the one to fix it, never park a finding and walk away from it.
//
// It was a Stop hook, and that was wrong the same way the sibling
// link-all-refs plugin's Stop hook was wrong. A Stop hook runs AFTER the
// message has streamed, so refusing cannot unsend anything: the user reads the
// deflection, then reads a near-identical retype of the same message. The
// retype explains itself, which names the banned phrase again, so the guard
// fires again. That loop has no bound, and one goal was refused nine times
// over. The reader is the surface this rule is about, so the annotation goes
// there and the model is left alone.
//
// displayContent is display-only, read out of the shipped bundle rather than
// assumed: it "replaces the delta on screen without changing the stored
// message", and it is the only field the event's output schema carries.
//
// Every failure path prints nothing, which leaves the CLI showing the original
// text. This runs in the render path, and a guard that can eat output is worse
// than no guard.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// HookInput is the subset of the MessageDisplay payload this plugin reads.
// `delta` is whole lines except on the final flush, and `final` is the
// end-of-message signal even when its delta is empty.
type HookInput struct {
	HookEventName string `json:"hook_event_name"`
	MessageID     string `json:"message_id"`
	Index         int    `json:"index"`
	Final         bool   `json:"final"`
	Delta         string `json:"delta"`
}

// Output is what the CLI reads back. displayContent replaces this flush on
// screen, and is the only field the event's output schema carries.
type Output struct {
	HookSpecificOutput struct {
		HookEventName  string `json:"hookEventName"`
		DisplayContent string `json:"displayContent"`
	} `json:"hookSpecificOutput"`
}

func main() {
	if out := run(os.Stdin); out != "" {
		fmt.Println(out)
	}
}

// run decides what this flush renders as. An empty return means print nothing,
// which shows the original.
//
// The message is judged whole, on its last flush. A banned phrase can span a
// line wrap, and one flush carries only the lines that completed since the
// last one, so judging a flush on its own would miss the phrases that straddle
// the boundary and would mark the same message several times over.
func run(r io.Reader) string {
	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return ""
	}
	var in HookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return ""
	}
	if in.HookEventName != "MessageDisplay" || disabled() {
		return ""
	}

	prior := ""
	if in.MessageID != "" {
		prior = priorText(in.MessageID)
	}
	if !in.Final {
		if in.MessageID != "" {
			rememberText(in.MessageID, prior+in.Delta)
		}
		return ""
	}
	if in.MessageID != "" {
		forgetText(in.MessageID)
		sweep()
	}

	note := Annotate(prior + in.Delta)
	if note == "" {
		return ""
	}

	var out Output
	out.HookSpecificOutput.HookEventName = "MessageDisplay"
	out.HookSpecificOutput.DisplayContent = in.Delta + note
	encoded, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func disabled() bool {
	switch os.Getenv("CC_NO_BLAME_LANGUAGE") {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

var stateDir = filepath.Join(os.TempDir(), "cc-no-blame-language")

// unsafeName strips everything that is not plainly a filename character. A
// message id is a UUID, but an id is never trusted straight into a path.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func stateFile(messageID string) string {
	return filepath.Join(stateDir, unsafeName.ReplaceAllString(messageID, "")+".txt")
}

// priorText is this message's text before the current flush. Losing it costs
// the phrases that sit in the earlier flushes, never a wrong annotation.
func priorText(messageID string) string {
	data, err := os.ReadFile(stateFile(messageID))
	if err != nil {
		return ""
	}
	return string(data)
}

func rememberText(messageID, text string) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(stateFile(messageID), []byte(text), 0o600)
}

func forgetText(messageID string) { _ = os.Remove(stateFile(messageID)) }

// sweep collects what a session that died mid-message left behind, so the
// directory cannot grow without bound.
func sweep() {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > time.Hour {
			_ = os.Remove(filepath.Join(stateDir, entry.Name()))
		}
	}
}
