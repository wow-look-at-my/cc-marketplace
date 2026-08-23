// detect.go finds a busy-poll: a trailing run of turns that each made the
// exact same tool call, spaced close enough together that no real event or
// scheduled wakeup could plausibly explain the repeat.
//
// Spacing is what separates this from a legitimate watch loop. A session
// that re-checks a PR every 20 minutes via a scheduled trigger repeats the
// same command too, but each repeat follows a genuine gap; a busy-poll
// repeats it turn after turn with the gap measured in seconds. Only the
// second shape wastes tokens for nothing, so only the second shape refuses.
package main

import (
	"os"
	"strconv"
	"time"
)

// defaultThreshold is how many closely-spaced identical turns in a row it
// takes to refuse. Three quick re-checks can be a person iterating by
// hand; four in a row with no real gap is a loop.
const defaultThreshold = 4

// defaultMaxGap bounds "closely spaced". A properly-armed wakeup in this
// environment is never shorter than a few minutes (ScheduleWakeup clamps to
// 60s minimum, and every documented convention here recommends much
// longer), so a gap under this counts as no real wait happened.
const defaultMaxGap = 5 * time.Minute

func threshold() int {
	if v := os.Getenv("NO_BUSY_POLL_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 1 {
			return n
		}
	}
	return defaultThreshold
}

func maxGap() time.Duration {
	if v := os.Getenv("NO_BUSY_POLL_MAX_GAP_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return defaultMaxGap
}

// streak returns the length of the trailing run of turns sharing the last
// turn's signature, each following the one before it within maxGap, plus
// that last turn's calls for display. It returns (0, nil) when the last
// turn made no tool call at all -- there is nothing repeated to refuse.
func streak(turns []turn) (int, []call) {
	if len(turns) == 0 {
		return 0, nil
	}
	last := turns[len(turns)-1]
	if last.sig == "" {
		return 0, nil
	}
	n := 1
	for i := len(turns) - 2; i >= 0; i-- {
		if turns[i].sig != last.sig {
			break
		}
		gap := turns[i+1].startedAt.Sub(turns[i].endedAt)
		if gap < 0 {
			gap = 0
		}
		if gap > maxGap() {
			break
		}
		n++
	}
	return n, last.calls
}
