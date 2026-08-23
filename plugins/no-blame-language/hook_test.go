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

// writeTranscript builds a transcript whose last assistant message carries
// text, and returns its path.
func writeTranscript(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	rec := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	}
	line, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(line, '\n'), 0o644))
	return path
}

// stop runs the hook against a Stop payload for path.
func stop(t *testing.T, path string, active bool) result {
	t.Helper()
	in, err := json.Marshal(HookInput{
		HookEventName:  "Stop",
		TranscriptPath: path,
		StopHookActive: active,
	})
	require.NoError(t, err)
	return run(strings.NewReader(string(in)))
}

func TestStopIsRefusedForABannedPhrase(t *testing.T) {
	res := stop(t, writeTranscript(t, "That bug is pre-existing, so it stays left as-is."), false)
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "pre-existing")
	assert.Contains(t, res.stderr, "left as-is")
	assert.Contains(t, res.stderr, "Do not stop here")
}

func TestStopIsAllowedForAFixedAndOwnedFinding(t *testing.T) {
	text := "Found the race in the retry loop and fixed it here. The suite is green."
	res := stop(t, writeTranscript(t, text), false)
	assert.Equal(t, 0, res.code)
	assert.Empty(t, res.stderr)
}

func TestStopIsAllowedForAnHonestDeferral(t *testing.T) {
	// Naming a blocker plainly, without a banned phrase, is the org's own
	// documented escape hatch and must not be refused.
	text := "This needs your call on A vs B, so I pushed the branch with A and left the test red."
	res := stop(t, writeTranscript(t, text), false)
	assert.Equal(t, 0, res.code)
}

func TestRefusalRepeatsWhileThePhraseStaysUnfixed(t *testing.T) {
	path := writeTranscript(t, "Not my problem, this was existing code.")
	first := stop(t, path, false)
	second := stop(t, path, true)
	assert.Equal(t, 2, first.code, "the first stop is refused")
	assert.Equal(t, 2, second.code, "and so is the retry, because the message still deflects")
	assert.Contains(t, second.stderr, "second time")
	assert.NotContains(t, first.stderr, "second time")
}

func TestOnlySixPhrasesAreQuoted(t *testing.T) {
	var b strings.Builder
	for _, p := range bannedPhrases[:8] {
		b.WriteString(p)
		b.WriteString(". ")
	}
	res := stop(t, writeTranscript(t, b.String()), false)
	assert.Equal(t, 2, res.code)
	assert.Equal(t, 6, strings.Count(res.stderr, "\n      "))
}

func TestUnreadableInputAllowsTheStop(t *testing.T) {
	assert.Equal(t, 0, run(strings.NewReader("not json")).code)
	assert.Equal(t, 0, run(strings.NewReader("")).code)
}

func TestAMissingTranscriptAllowsTheStop(t *testing.T) {
	assert.Equal(t, 0, stop(t, filepath.Join(t.TempDir(), "absent.jsonl"), false).code)
	assert.Equal(t, 0, stop(t, "", false).code)
}

func TestAnotherEventIsIgnored(t *testing.T) {
	in, err := json.Marshal(HookInput{
		HookEventName:  "PreToolUse",
		TranscriptPath: writeTranscript(t, "Not my problem."),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, run(strings.NewReader(string(in))).code)
}

func TestAPayloadWithNoEventNameIsTreatedAsStop(t *testing.T) {
	in, err := json.Marshal(map[string]string{"transcript_path": writeTranscript(t, "Not my problem.")})
	require.NoError(t, err)
	assert.Equal(t, 2, run(strings.NewReader(string(in))).code)
}

func TestAllowIsSilent(t *testing.T) {
	res := allow()
	assert.Equal(t, 0, res.code)
	assert.Empty(t, res.stderr)
}
