// Command ask-properly is a Claude Code Stop hook that refuses to let a turn
// end on a question put to the user in prose.
//
// A question typed into a closing message is a decision handed back. The user
// has to read it, work out which options were meant, and type an answer --
// and a model that can do this can offload any hard call it was asked to
// make. AskUserQuestion exists for exactly this: it renders the choices, so
// the user picks instead of composing a reply, and the model has to have
// thought the options through well enough to write them down.
//
// So: ask through AskUserQuestion, or answer the question yourself. Do not
// end a turn by inviting the user to decide in prose.
//
// Every failure path allows the stop. A guard that blocks because it could
// not read a file is worse than no guard.
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

	turn := ReadTurn(in.TranscriptPath)

	// A turn that used the tool asked properly. Prose alongside a rendered
	// card is commentary, not an offloaded decision.
	if turn.UsedAskTool {
		return allow()
	}

	hits := FindQuestions(turn.FinalText)
	if len(hits) == 0 {
		return allow()
	}
	return result{code: 2, stderr: reason(hits, in.StopHookActive)}
}

// reason is what the model is told. It quotes each finding with its line,
// states the rule, and says what to do instead, because a refusal that does
// not say what to write costs a round trip to find out.
func reason(hits []Hit, repeat bool) string {
	var b strings.Builder
	b.WriteString("Do not stop here. This message ends by handing the user a decision in prose:\n\n")
	for _, hit := range hits[:min(len(hits), 6)] {
		label := "question"
		if hit.Kind == "deferral" {
			label = "deferral"
		}
		fmt.Fprintf(&b, "  [%s] %q\n      %s\n", label, hit.Text, hit.Line)
	}
	b.WriteString(waysOut)
	if repeat {
		b.WriteString(repeatNote)
	}
	return b.String()
}

// waysOut is the body of the refusal: the rule, and the only two things that
// satisfy it.
const waysOut = `
A question typed into a closing message is work pushed back onto the user:
they have to reconstruct the options and compose a reply. Two ways out, and
only two:

  1. ANSWER IT YOURSELF. If you can reach a defensible answer from the code,
     the docs, or a sensible default, do that and say what you assumed. Most
     questions worth asking a user are questions you can settle by reading.

  2. ASK IT WITH AskUserQuestion. If the answer is genuinely the user's to
     give, call the tool: put your recommendation first and label it, and
     give every option a description saying what it COSTS and what it BUYS.
     Batch every open question into one call; never ask them one at a time.

A dismissed or unanswered card is not a ban on asking. Ask again, better.

Rewrite the message without the prose question, then stop.`

// repeatNote is appended once the model has already been refused this turn.
const repeatNote = `

This is the second time. Do not argue with the hook, and do not delete the
question to get past it while still leaving the decision unmade -- either
settle it yourself and say so, or call AskUserQuestion.`
