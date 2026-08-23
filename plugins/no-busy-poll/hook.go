// Command no-busy-poll is a Claude Code Stop hook that refuses to end a
// turn which is the latest in a run of several turns making the exact same
// tool call, closely spaced in time, with nothing else different in
// between.
//
// This is the shape a manual polling loop takes: re-running the same
// status check every turn instead of waiting for a real event or arming an
// actual scheduled wakeup. It burns the user's tokens for zero new
// information every single time, because the answer cannot have changed
// between two calls seconds apart.
//
// A properly spaced watch loop -- the same check re-run after a real gap,
// because a scheduled trigger fired or an event arrived -- is not this
// pattern and is not refused; see detect.go for the spacing rule that
// tells the two apart.
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

// result is what one invocation emits: a stop is refused with exit code 2
// and a reason on stderr, which is how a Stop hook hands the model its
// objection.
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

	n, calls := streak(parseTurns(in.TranscriptPath))
	if n < threshold() {
		return allow()
	}
	return result{code: 2, stderr: reason(n, calls, in.StopHookActive)}
}

// reason is what the model is told. It names the repeated call, states the
// count, and gives the two ways out, because a refusal that does not say
// what to do instead just gets repeated with a different excuse.
func reason(n int, calls []call, repeat bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Stop. The last %d turns in a row made the exact same call, with nothing else\n", n)
	b.WriteString("different in between and no real wait between them -- that is a busy-poll loop,\n")
	b.WriteString("and it burns the user's tokens for zero new signal, because the answer cannot\n")
	b.WriteString("have changed in the seconds since the last check:\n\n")
	for _, c := range calls {
		fmt.Fprintf(&b, "  %s\n", c.disp)
	}
	b.WriteString("\nDo not run any of the calls above again on a hunch. Either:\n\n")
	b.WriteString("  - Reply with NO tool call at all and wait for a real signal -- a queued\n")
	b.WriteString("    notification, a scheduled trigger firing, an actual event arriving -- or\n")
	b.WriteString("  - Arm a real wakeup (ScheduleWakeup / send_later / a Monitor watch) with a\n")
	b.WriteString("    genuine delay, then stop. Never re-check by hand in the meantime.\n\n")
	b.WriteString("Rewrite this turn so it makes none of the calls listed above, then stop.")
	if repeat {
		b.WriteString("\n\nThis is not the first refusal. Making the same call again will refuse again --\n")
		b.WriteString("the fix is to stop calling it, not to call it one more time.")
	}
	return b.String()
}
