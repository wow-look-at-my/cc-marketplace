// Command link-all-refs is a Claude Code MessageDisplay hook. It renders every
// reference the reader might open -- a pull request number, a commit, a branch,
// a bare GitHub URL -- as a markdown link, while the message streams.
//
// `[text](url)` is the one spelling that works on both surfaces: a link on the
// web client, and a real OSC 8 terminal hyperlink in the CLI, which renders a
// markdown link through the terminal's own hyperlink escape.
//
// It does NOT refuse a stop, and it sends nothing back to the model. That was
// the previous design and it was wrong twice over. The user read the bare
// reference and then a near-identical retype of the same message. And the
// retype names the reference again while explaining itself, so the guard fired
// again, and the only reply that escapes that loop is one carrying nothing but
// links -- which is what the user was left reading. A missing link is not work
// only the model can do: the token plus the checkout determine the URL, so the
// hook writes it, for no round trip and with nothing to loop on.
//
// displayContent is display-only, verified against the shipped bundle: the
// transcript and the model's next request are both fed from an array populated
// BEFORE this hook runs and never updated from its result. So this changes what
// the reader sees and nothing else, which is the surface the rule is about.
//
// Every failure path prints nothing, which leaves the CLI showing the original
// text. This runs in the render path, so a guard that can eat output is worse
// than no guard at all.
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
	if out := run(os.Stdin, &GitResolver{Dir: "."}); out != "" {
		fmt.Println(out)
	}
}

// run decides what this flush renders as. An empty return means print nothing,
// which shows the original.
func run(r io.Reader, res Resolver) string {
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

	// Carry the fence state across flushes: this batch cannot see the ``` that
	// opened in an earlier one.
	prior := ""
	if in.MessageID != "" && in.Index != 0 {
		prior = priorText(in.MessageID)
	}
	rewritten, changed := RewriteDelta(in.Delta, EndsInsideFence(prior), res)

	if in.MessageID != "" {
		if in.Final {
			forgetText(in.MessageID)
			sweep()
		} else {
			rememberText(in.MessageID, prior+in.Delta)
		}
	}
	if !changed {
		return ""
	}

	var out Output
	out.HookSpecificOutput.HookEventName = "MessageDisplay"
	out.HookSpecificOutput.DisplayContent = rewritten
	encoded, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func disabled() bool {
	switch os.Getenv("CC_LINK_ALL_REFS") {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

var stateDir = filepath.Join(os.TempDir(), "cc-link-all-refs")

// unsafeName strips everything that is not plainly a filename character. A
// message id is a UUID, but an id is never trusted straight into a path.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func stateFile(messageID string) string {
	return filepath.Join(stateDir, unsafeName.ReplaceAllString(messageID, "")+".txt")
}

// priorText is this message's text before the current flush. Only the fence
// state is read from it, so an unreadable file means "assume not fenced".
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
	// Losing this costs fence tracking on the next flush, nothing more.
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
