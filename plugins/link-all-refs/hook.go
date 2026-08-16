// Command link-all-refs is a Claude Code Stop hook that refuses to let a turn
// end while the closing message names something the reader cannot click.
//
// A pull request number, a commit SHA, a branch, a GitHub URL: each is a
// markdown link or the turn does not end. `[text](url)` is the one spelling
// that works on both surfaces -- a link on the web client, and a real OSC 8
// terminal hyperlink in the CLI, which renders markdown links through the
// terminal's hyperlink escape when the terminal advertises support.
//
// The check is one step: remove every markdown link from the message and look
// at what is left. A link given earlier in the message, or in an earlier turn,
// earns nothing -- the reader is looking at the text in front of them.
//
// Every failure path allows the stop. A guard that blocks because it could not
// read a file is worse than no guard.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// HookInput is the subset of the Stop payload this plugin reads.
type HookInput struct {
	HookEventName  string `json:"hook_event_name"`
	TranscriptPath string `json:"transcript_path"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// result is what one invocation emits: a stop is refused with exit code 2 and
// a reason on stderr, which is how a Stop hook hands the model its objection.
type result struct {
	stderr string
	code   int
}

// allow lets the turn end.
func allow() result { return result{} }

func main() {
	res := run(os.Stdin)
	if res.stderr != "" {
		fmt.Fprint(os.Stderr, res.stderr)
	}
	os.Exit(res.code)
}

// run reads a hook payload from r and decides whether the turn may end.
func run(r io.Reader) result {
	data, _ := io.ReadAll(r)
	var in HookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return allow()
	}
	if in.HookEventName != "" && in.HookEventName != "Stop" {
		return allow()
	}
	refs := FindUnlinked(FinalAssistantText(in.TranscriptPath))
	if len(refs) == 0 {
		return allow()
	}
	return result{code: 2, stderr: reason(refs, in.StopHookActive)}
}

// reason is what the model is told. It names each offending token with the
// line it sits on, states the rule, and shows the shape of the fix, because a
// refusal that does not say what to write costs a round trip to find out.
func reason(refs []Ref, repeat bool) string {
	var b strings.Builder
	b.WriteString("Do not stop here. This message names things the user cannot click:\n\n")
	for _, ref := range refs[:min(len(refs), 6)] {
		fmt.Fprintf(&b, "  %s -- %s\n      %s\n", ref.Text, ref.Kind, ref.Line)
	}
	b.WriteString("\nA pull request, a commit, a branch, and a URL are markdown links. Every mention, in\n")
	b.WriteString("every message: linking one earlier earns nothing, because the user is reading THIS\n")
	b.WriteString("message. `[text](url)` is right on both surfaces -- a link on the web, and a real\n")
	b.WriteString("clickable hyperlink in the terminal, which Claude Code renders through the\n")
	b.WriteString("terminal's own hyperlink escape.\n\n")
	b.WriteString("  [owner/repo#42](https://github.com/owner/repo/pull/42)\n")
	b.WriteString("  [6884dd2](https://github.com/owner/repo/commit/6884dd2)\n")
	b.WriteString("  [claude/fix-thing](https://github.com/owner/repo/compare/master...claude/fix-thing?expand=1)\n\n")
	b.WriteString("Rewrite your message with every one of them linked, then stop.")
	if repeat {
		b.WriteString("\n\nThis is the second time. Do not argue with the hook and do not delete the\n")
		b.WriteString("reference to get past it -- write the link.")
	}
	return b.String()
}
