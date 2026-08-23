// Command no-blame-language is a Claude Code Stop hook that refuses to let a
// turn end while its closing message deflects a defect instead of owning it.
//
// "Pre-existing", "not my problem", "out of scope", "flagging this for you",
// "that predates this session": each of these reports a finding and stops
// there, or shifts blame for code in this org's own repos onto some other
// author or an earlier point in time. This org's own written convention bans
// that shape of sentence -- found it, fix it, or say precisely why you are
// not the one to fix it, never park a finding and walk away from it.
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
	hits := FindBannedPhrases(FinalAssistantText(in.TranscriptPath))
	if len(hits) == 0 {
		return allow()
	}
	return result{code: 2, stderr: reason(hits, in.StopHookActive)}
}

// reason is what the model is told. It names each banned phrase with the line
// it sits on, states the rule, and says what to write instead, because a
// refusal that does not say what to write costs a round trip to find out.
func reason(hits []Hit, repeat bool) string {
	var b strings.Builder
	b.WriteString("Do not stop here. This message reports a defect instead of owning it:\n\n")
	for _, hit := range hits[:min(len(hits), 6)] {
		fmt.Fprintf(&b, "  %q\n      %s\n", hit.Phrase, hit.Line)
	}
	b.WriteString("\nThis org bans deflecting, blame-shifting language in a closing message: a\n")
	b.WriteString("finding you report and leave is a defect you caused, and a correction never\n")
	b.WriteString("authorizes naming who wrote the broken line first. Fix the root cause and say\n")
	b.WriteString("so, or state precisely what you found, why it is not yours to fix, and what\n")
	b.WriteString("you did instead -- never park a finding and walk away from it.\n\n")
	b.WriteString("Rewrite your message without the phrase above, then stop.")
	if repeat {
		b.WriteString("\n\nThis is the second time. Do not argue with the hook and do not delete the\n")
		b.WriteString("phrase to get past it -- either fix the thing, or write the honest, specific\n")
		b.WriteString("account of why you are not the one to fix it.")
	}
	return b.String()
}
