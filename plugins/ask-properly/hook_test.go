package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTranscript builds a JSONL transcript from raw record lines.
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

func userPrompt(text string) string {
	return `{"type":"user","message":{"role":"user","content":[{"type":"text","text":` + quote(text) + `}]}}`
}

func assistantText(text string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + quote(text) + `}]}}`
}

func assistantAsk() string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"AskUserQuestion","input":{}}]}}`
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func payload(t *testing.T, path string, repeat bool) string {
	t.Helper()
	b, err := json.Marshal(HookInput{
		HookEventName:  "Stop",
		TranscriptPath: path,
		StopHookActive: repeat,
	})
	require.NoError(t, err)
	return string(b)
}

func TestStopIsRefusedOnAProseQuestion(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("resolve the open questions"),
		assistantText("Four are still open. Which one should win?"),
	)
	res := run(strings.NewReader(payload(t, path, false)))
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "AskUserQuestion")
	assert.Contains(t, res.stderr, "Which one should win?")
}

// The escape hatch: a turn that used the tool asked properly, so prose
// alongside the rendered card is commentary and not an offloaded decision.
func TestStopIsAllowedWhenTheTurnUsedAskUserQuestion(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("resolve the open questions"),
		assistantAsk(),
		assistantText("Putting the four decisions to you. Which do you prefer?"),
	)
	res := run(strings.NewReader(payload(t, path, false)))
	assert.Equal(t, 0, res.code)
	assert.Empty(t, res.stderr)
}

// An AskUserQuestion call in an EARLIER turn does not license a prose
// question now.
func TestAnEarlierTurnsAskDoesNotCount(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("first ask"),
		assistantAsk(),
		assistantText("Thanks."),
		userPrompt("now finish it"),
		assistantText("Done. Which rule should win?"),
	)
	res := run(strings.NewReader(payload(t, path, false)))
	assert.Equal(t, 2, res.code)
}

func TestStopIsAllowedOnAPlainReport(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("land it"),
		assistantText("Landed the rule in docs/json.md and pinned it in tests/json.dats. CI is green."),
	)
	res := run(strings.NewReader(payload(t, path, false)))
	assert.Equal(t, 0, res.code)
}

func TestTheRefusalEscalatesOnRepeat(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("go"),
		assistantText("Your call."),
	)
	res := run(strings.NewReader(payload(t, path, true)))
	require.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "second time")
}

func TestEveryFailurePathAllowsTheStop(t *testing.T) {
	assert.Equal(t, 0, run(strings.NewReader("not json")).code)
	assert.Equal(t, 0, run(strings.NewReader(`{"hook_event_name":"PreToolUse"}`)).code)
	assert.Equal(t, 0, run(strings.NewReader(`{"hook_event_name":"Stop"}`)).code)
	assert.Equal(t, 0, run(strings.NewReader(
		`{"hook_event_name":"Stop","transcript_path":"/nope/missing.jsonl"}`)).code)
	assert.Equal(t, 0, run(strings.NewReader("")).code)
}

func TestTheRefusalNamesBothWaysOut(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("go"),
		assistantText("Should I use exit 64 here?"),
	)
	res := run(strings.NewReader(payload(t, path, false)))
	require.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "ANSWER IT YOURSELF")
	assert.Contains(t, res.stderr, "ASK IT WITH AskUserQuestion")
	assert.Contains(t, res.stderr, "recommendation first")
}
