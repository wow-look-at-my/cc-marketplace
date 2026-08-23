package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closePoll writes n turns, each a Bash call with the same command,
// spaced apart seconds, starting at base -- the exact shape of the
// incident this plugin exists to catch.
func closePoll(t *testing.T, n int, base time.Time, cmd string) string {
	t.Helper()
	var lines []string
	for i := 0; i < n; i++ {
		start := base.Add(time.Duration(i) * 20 * time.Second)
		lines = append(lines,
			rec(t, "user", "user", textBlock("Stop hook feedback: still waiting"), start),
			rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": cmd}), start.Add(time.Second)),
			rec(t, "user", "user", toolResultBlock(), start.Add(2*time.Second)),
			rec(t, "assistant", "assistant", textBlock("still open, holding"), start.Add(3*time.Second)),
		)
	}
	return writeLines(t, lines)
}

func stopPayload(t *testing.T, path string, active bool) string {
	t.Helper()
	in, err := json.Marshal(HookInput{
		HookEventName:  "Stop",
		TranscriptPath: path,
		StopHookActive: active,
	})
	require.NoError(t, err)
	return string(in)
}

func TestStopIsRefusedAfterFourCloselySpacedIdenticalTurns(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := closePoll(t, 4, base, "gh pr view 186 --json state,mergedAt")
	res := run(strings.NewReader(stopPayload(t, path, false)))
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "gh pr view 186")
	assert.Contains(t, res.stderr, "busy-poll")
	assert.Contains(t, res.stderr, "ScheduleWakeup")
}

func TestStopIsAllowedWithFewerThanTheThreshold(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := closePoll(t, 3, base, "gh pr view 186 --json state,mergedAt")
	res := run(strings.NewReader(stopPayload(t, path, false)))
	assert.Equal(t, 0, res.code, "three quick re-checks can be a person iterating by hand")
}

func TestStopIsAllowedWhenTurnsAreProperlyPaced(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 5; i++ {
		start := base.Add(time.Duration(i) * 20 * time.Minute) // a real scheduled cadence
		lines = append(lines,
			rec(t, "user", "user", textBlock("check-in fired"), start),
			rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "gh pr checks 42"}), start.Add(time.Second)),
			rec(t, "user", "user", toolResultBlock(), start.Add(2*time.Second)),
			rec(t, "assistant", "assistant", textBlock("still pending, rearming"), start.Add(3*time.Second)),
		)
	}
	res := run(strings.NewReader(stopPayload(t, writeLines(t, lines), false)))
	assert.Equal(t, 0, res.code, "a properly spaced watch loop is not the pattern this hook refuses")
}

func TestStopIsAllowedWhenTheCurrentTurnBreaksThePattern(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := closePoll(t, 4, base, "gh pr view 186 --json state,mergedAt")

	// The model heeded an earlier refusal: this turn replies with no tool
	// call at all.
	lines := []string{}
	data, err := readTail(path, transcriptTailBytes)
	require.NoError(t, err)
	for _, l := range data {
		if len(l) > 0 {
			lines = append(lines, string(l))
		}
	}
	lastStart := base.Add(4 * 20 * time.Second)
	lines = append(lines,
		rec(t, "user", "user", textBlock("Stop hook feedback: still waiting"), lastStart),
		rec(t, "assistant", "assistant", textBlock("Holding, no new check performed."), lastStart.Add(time.Second)),
	)
	res := run(strings.NewReader(stopPayload(t, writeLines(t, lines), false)))
	assert.Equal(t, 0, res.code, "the current turn made no tool call, so nothing is being repeated right now")
}

func TestStopIsAllowedWhenTheCallsDifferEachTime(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 5; i++ {
		start := base.Add(time.Duration(i) * 20 * time.Second)
		lines = append(lines,
			rec(t, "user", "user", textBlock("fix and retest"), start),
			rec(t, "assistant", "assistant", toolUseBlock("Edit", map[string]any{"file_path": "x.go", "new_string": "v" + string(rune('0'+i))}), start.Add(time.Second)),
			rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "go test ./..."}), start.Add(2*time.Second)),
			rec(t, "user", "user", toolResultBlock(), start.Add(3*time.Second)),
		)
	}
	res := run(strings.NewReader(stopPayload(t, writeLines(t, lines), false)))
	assert.Equal(t, 0, res.code, "each turn's edit differs, so the whole turn is never byte-identical to the last")
}

func TestRefusalEscalatesOnRepeat(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := closePoll(t, 4, base, "gh pr view 186 --json state,mergedAt")
	first := run(strings.NewReader(stopPayload(t, path, false)))
	second := run(strings.NewReader(stopPayload(t, path, true)))
	assert.Equal(t, 2, first.code)
	assert.Equal(t, 2, second.code)
	assert.NotContains(t, first.stderr, "not the first refusal")
	assert.Contains(t, second.stderr, "not the first refusal")
}

func TestUnreadableInputAllowsTheStop(t *testing.T) {
	assert.Equal(t, 0, run(strings.NewReader("not json")).code)
	assert.Equal(t, 0, run(strings.NewReader("")).code)
}

func TestAMissingTranscriptAllowsTheStop(t *testing.T) {
	assert.Equal(t, 0, run(strings.NewReader(stopPayload(t, filepath.Join(t.TempDir(), "absent.jsonl"), false))).code)
	assert.Equal(t, 0, run(strings.NewReader(stopPayload(t, "", false))).code)
}

func TestAnotherEventIsIgnored(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := closePoll(t, 4, base, "gh pr view 186 --json state,mergedAt")
	in, err := json.Marshal(HookInput{HookEventName: "PreToolUse", TranscriptPath: path})
	require.NoError(t, err)
	assert.Equal(t, 0, run(strings.NewReader(string(in))).code)
}

func TestAPayloadWithNoEventNameIsTreatedAsStop(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := closePoll(t, 4, base, "gh pr view 186 --json state,mergedAt")
	in, err := json.Marshal(map[string]string{"transcript_path": path})
	require.NoError(t, err)
	assert.Equal(t, 2, run(strings.NewReader(string(in))).code)
}

func TestAllowIsSilent(t *testing.T) {
	res := allow()
	assert.Equal(t, 0, res.code)
	assert.Empty(t, res.stderr)
}
