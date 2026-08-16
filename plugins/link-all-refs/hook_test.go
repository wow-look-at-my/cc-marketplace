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

func TestStopIsRefusedForAnUnlinkedReference(t *testing.T) {
	res := stop(t, writeTranscript(t, "re-pushed as `claude/thing` (`6884dd2`)."), false)
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "6884dd2")
	assert.Contains(t, res.stderr, "claude/thing")
	assert.Contains(t, res.stderr, "[text](url)")
}

func TestStopIsAllowedWhenEveryReferenceIsLinked(t *testing.T) {
	text := "[o/r#376](https://github.com/o/r/pull/376) is merged, at [6884dd2](https://github.com/o/r/commit/6884dd2)."
	res := stop(t, writeTranscript(t, text), false)
	assert.Equal(t, 0, res.code)
	assert.Empty(t, res.stderr)
}

func TestStopIsAllowedForAMessageWithNoReferences(t *testing.T) {
	res := stop(t, writeTranscript(t, "The suite passes and vet is clean."), false)
	assert.Equal(t, 0, res.code)
}

func TestRefusalRepeatsWhileTheReferenceStaysUnlinked(t *testing.T) {
	path := writeTranscript(t, "PR #376 is green.")
	first := stop(t, path, false)
	second := stop(t, path, true)
	assert.Equal(t, 2, first.code, "the first stop is refused")
	assert.Equal(t, 2, second.code, "and so is the retry, because the message still has no link")
	assert.Contains(t, second.stderr, "second time")
	assert.NotContains(t, first.stderr, "second time")
}

func TestOnlySixReferencesAreQuoted(t *testing.T) {
	res := stop(t, writeTranscript(t, "#1\n#2\n#3\n#4\n#5\n#6\n#7\n#8"), false)
	assert.Equal(t, 2, res.code)
	assert.Equal(t, 6, strings.Count(res.stderr, "-- an issue or pull request number"))
	assert.Contains(t, res.stderr, "#6")
	assert.NotContains(t, res.stderr, "#7")
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
		TranscriptPath: writeTranscript(t, "PR #376 is green."),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, run(strings.NewReader(string(in))).code)
}

func TestAPayloadWithNoEventNameIsTreatedAsStop(t *testing.T) {
	in, err := json.Marshal(map[string]string{"transcript_path": writeTranscript(t, "PR #376 is green.")})
	require.NoError(t, err)
	assert.Equal(t, 2, run(strings.NewReader(string(in))).code)
}

func TestAllowIsSilent(t *testing.T) {
	res := allow()
	assert.Equal(t, 0, res.code)
	assert.Empty(t, res.stderr)
}
