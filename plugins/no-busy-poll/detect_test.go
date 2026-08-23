package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func mkTurn(sig string, start, end time.Time) turn {
	return turn{sig: sig, calls: []call{{canon: sig, disp: "disp:" + sig}}, startedAt: start, endedAt: end}
}

func TestStreakCountsCloselySpacedIdenticalTurns(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	turns := []turn{
		mkTurn("A", base, base.Add(time.Second)),
		mkTurn("A", base.Add(10*time.Second), base.Add(11*time.Second)),
		mkTurn("A", base.Add(20*time.Second), base.Add(21*time.Second)),
		mkTurn("A", base.Add(30*time.Second), base.Add(31*time.Second)),
	}
	n, calls := streak(turns)
	assert.Equal(t, 4, n)
	assert.Len(t, calls, 1)
}

func TestStreakBreaksOnADifferentSignature(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	turns := []turn{
		mkTurn("B", base, base.Add(time.Second)),
		mkTurn("A", base.Add(10*time.Second), base.Add(11*time.Second)),
		mkTurn("A", base.Add(20*time.Second), base.Add(21*time.Second)),
	}
	n, _ := streak(turns)
	assert.Equal(t, 2, n, "the streak stops at the older, different turn")
}

func TestStreakBreaksOnALongGap(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	turns := []turn{
		mkTurn("A", base, base.Add(time.Second)),
		mkTurn("A", base.Add(1*time.Hour), base.Add(1*time.Hour+time.Second)), // a real gap: a scheduled check-in, not a loop
		mkTurn("A", base.Add(1*time.Hour+10*time.Second), base.Add(1*time.Hour+11*time.Second)),
	}
	n, _ := streak(turns)
	assert.Equal(t, 2, n, "the hour-long gap breaks the streak, leaving only the last two close turns")
}

func TestStreakIsZeroWhenTheLastTurnMadeNoCall(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	turns := []turn{
		mkTurn("A", base, base.Add(time.Second)),
		mkTurn("A", base.Add(10*time.Second), base.Add(11*time.Second)),
		{sig: "", startedAt: base.Add(20 * time.Second), endedAt: base.Add(21 * time.Second)},
	}
	n, calls := streak(turns)
	assert.Equal(t, 0, n, "the current turn broke the pattern by making no call at all")
	assert.Nil(t, calls)
}

func TestStreakOnEmptyTurnsIsZero(t *testing.T) {
	n, calls := streak(nil)
	assert.Equal(t, 0, n)
	assert.Nil(t, calls)
}

func TestThresholdDefaultsToFour(t *testing.T) {
	os.Unsetenv("NO_BUSY_POLL_THRESHOLD")
	assert.Equal(t, 4, threshold())
}

func TestThresholdEnvOverride(t *testing.T) {
	t.Setenv("NO_BUSY_POLL_THRESHOLD", "7")
	assert.Equal(t, 7, threshold())
}

func TestThresholdIgnoresGarbageOrTooSmallEnv(t *testing.T) {
	t.Setenv("NO_BUSY_POLL_THRESHOLD", "not a number")
	assert.Equal(t, defaultThreshold, threshold())
	t.Setenv("NO_BUSY_POLL_THRESHOLD", "1")
	assert.Equal(t, defaultThreshold, threshold(), "a threshold of 1 would refuse a turn that never repeated anything")
}

func TestMaxGapDefaultIsFiveMinutes(t *testing.T) {
	os.Unsetenv("NO_BUSY_POLL_MAX_GAP_SECONDS")
	assert.Equal(t, defaultMaxGap, maxGap())
}

func TestMaxGapEnvOverride(t *testing.T) {
	t.Setenv("NO_BUSY_POLL_MAX_GAP_SECONDS", "30")
	assert.Equal(t, 30*time.Second, maxGap())
}
