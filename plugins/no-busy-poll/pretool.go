// pretool.go is the deny half: a status read is refused BEFORE it runs, so
// the wasted call costs nothing rather than costing a round trip and then
// earning a note about it at Stop.
//
// Two rules, and they answer the same question -- can this call learn
// anything? A subject that has already reached a terminal state cannot teach
// it anything ever again. A subject read earlier with no event and no change
// of the world since cannot teach it anything YET.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// worldChangers are the calls after which re-reading a status is legitimate,
// because the thing being watched really may have moved. A push is the
// obvious one: CI on the new head is a new question, not a repeat.
var worldChangers = []string{
	"git push",
	"git commit",
	"git merge",
	"gh wait-ci dispatch",
	"gh wait-ci rerun",
}

// denyReason is the refusal, or empty when the call may run.
type verdict struct {
	deny   bool
	reason string
}

// judgeCall decides one PreToolUse call against the transcript behind it.
func judgeCall(c toolCall, recs []record) verdict {
	if !isStatusRead(c) {
		return verdict{}
	}
	subs := subjectsIn(strings.ToLower(callText(c)))
	if len(subs) == 0 {
		return verdict{}
	}

	terminal := terminalSubjects(recs)
	for _, s := range subs {
		if terminal[s] {
			return verdict{deny: true, reason: terminalReason(s)}
		}
	}

	already := readSinceLastSignal(recs)
	for _, s := range subs {
		if already[s] {
			return verdict{deny: true, reason: repeatReason(s)}
		}
	}
	return verdict{}
}

// readSinceLastSignal returns the subjects already read since the last thing
// that could have changed an answer: a real user prompt, a wake or
// notification envelope, or a call that changed the world. Everything before
// that point is irrelevant, because something genuinely happened after it.
func readSinceLastSignal(recs []record) map[string]bool {
	start := 0
	for i, r := range recs {
		if r.newPrompt || r.wake {
			start = i
			continue
		}
		for _, c := range r.calls {
			if c.name == "Bash" && containsAny(strings.ToLower(commandOf(c.input)), worldChangers) {
				start = i
			}
		}
	}

	out := map[string]bool{}
	for _, r := range recs[start:] {
		for _, c := range r.calls {
			if !isStatusRead(c) {
				continue
			}
			for _, s := range subjectsIn(strings.ToLower(callText(c))) {
				out[s] = true
			}
		}
	}
	return out
}

// terminalText is what the model is told about a settled subject. It is one
// document with one hole in it, not a run of writes, so the wording reads as
// the reader will see it.
const terminalText = `Blocked: %s already reached a state it cannot leave, and this session
watched it get there -- the answer is in your transcript. A merged pull request
does not un-merge, and a commit that went green does not go red (a later push
makes a NEW commit, which is a different question).

Nothing you can call will return a different answer, so this call is pure cost.
Report the state you already have and move on to work that is not this.`

// repeatText names both ways out, because a refusal that only says "do not"
// costs a round trip while the model guesses at what would satisfy it.
const repeatText = `Blocked: you already read the state of %s, and nothing has happened
since -- no message from the user, no notification or wake event, and no push or
commit of your own. The answer cannot have changed, so this call spends the
user's tokens to re-read what you have.

Re-spelling the question does not make it a new one: a different tool asking
after the same pull request or commit is the same call.

Either reply with no tool call and let the event wake you, or go do something
else. This unblocks by itself the moment anything real happens.`

func terminalReason(subject string) string {
	return fmt.Sprintf(terminalText, describe(subject))
}

func repeatReason(subject string) string {
	return fmt.Sprintf(repeatText, describe(subject))
}

// describe renders a subject key back into something a reader recognises.
func describe(subject string) string {
	switch {
	case strings.HasPrefix(subject, "pr:#"):
		return "pull request #" + strings.TrimPrefix(subject, "pr:#")
	case strings.HasPrefix(subject, "pr:"):
		return "pull request " + strings.TrimPrefix(subject, "pr:")
	case strings.HasPrefix(subject, "sha:"):
		return "commit " + strings.TrimPrefix(subject, "sha:")
	}
	return subject
}

// preToolOutput is the deny payload. PreToolUse carries its decision in
// hookSpecificOutput rather than in an exit code, and a call this hook does
// not refuse must emit nothing at all, so the normal permission flow is
// left exactly as it was.
type preToolOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func denyPayload(reason string) string {
	var out preToolOutput
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	out.HookSpecificOutput.PermissionDecision = "deny"
	out.HookSpecificOutput.PermissionDecisionReason = reason
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}
