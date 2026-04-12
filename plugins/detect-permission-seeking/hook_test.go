package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func runEvaluate(t *testing.T, input HookInput) (int, string) {
	t.Helper()
	data, err := json.Marshal(input)
	require.Nil(t, err)
	return evaluate(data)
}

func TestBlockWantMeTo(t *testing.T) {
	code, msg := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "I've analyzed the code. Want me to refactor it?",
	})
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "permission-seeking")
	assert.Contains(t, msg, "autonomy")
}

func TestBlockWouldYouLike(t *testing.T) {
	code, msg := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "Here's what I found. Would you like me to fix these issues?",
	})
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "permission-seeking")
}

func TestBlockShallI(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "The tests are passing. Shall I deploy?",
	})
	assert.Equal(t, 2, code)
}

func TestBlockShouldI(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "I noticed some issues. Should I fix them?",
	})
	assert.Equal(t, 2, code)
}

func TestBlockDoYouWant(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "Do you want me to add error handling?",
	})
	assert.Equal(t, 2, code)
}

func TestBlockLetMeKnow(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "I've set up the basics. Let me know if you'd like me to continue.",
	})
	assert.Equal(t, 2, code)
}

func TestBlockICanIfYoudLike(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "I can add more tests if you'd like.",
	})
	assert.Equal(t, 2, code)
}

func TestAllowCleanMessage(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "I've completed the implementation. All tests pass.",
	})
	assert.Equal(t, 0, code)
}

func TestAllowEmptyMessage(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName: "Stop",
	})
	assert.Equal(t, 0, code)
}

func TestAllowInvalidJSON(t *testing.T) {
	code, _ := evaluate([]byte("not json"))
	assert.Equal(t, 0, code)
}

func TestAllowWrongEvent(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:        "PreToolUse",
		LastAssistantMessage: "Want me to do this?",
	})
	assert.Equal(t, 0, code)
}

func TestAllowMissingFields(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName: "Stop",
	})
	assert.Equal(t, 0, code)
}

func TestTranscriptFallback(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	lines := `{"role":"user","message":"Fix the bug"}
{"role":"assistant","message":"I found the bug. Want me to fix it?"}
`
	require.NoError(t, os.WriteFile(transcript, []byte(lines), 0644))

	code, msg := runEvaluate(t, HookInput{
		HookEventName:  "Stop",
		TranscriptPath: transcript,
	})
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "permission-seeking")
}

func TestTranscriptFallbackContentField(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	lines := `{"role":"assistant","content":"Should I proceed with the refactoring?"}
`
	require.NoError(t, os.WriteFile(transcript, []byte(lines), 0644))

	code, _ := runEvaluate(t, HookInput{
		HookEventName:  "Stop",
		TranscriptPath: transcript,
	})
	assert.Equal(t, 2, code)
}

func TestTranscriptFallbackUsesLastEntry(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	lines := `{"role":"assistant","message":"Want me to do this?"}
{"role":"assistant","message":"Done. All tests pass."}
`
	require.NoError(t, os.WriteFile(transcript, []byte(lines), 0644))

	code, _ := runEvaluate(t, HookInput{
		HookEventName:  "Stop",
		TranscriptPath: transcript,
	})
	assert.Equal(t, 0, code)
}

func TestTranscriptFallbackMissingFile(t *testing.T) {
	code, _ := runEvaluate(t, HookInput{
		HookEventName:  "Stop",
		TranscriptPath: "/nonexistent/transcript.jsonl",
	})
	assert.Equal(t, 0, code)
}

func TestRunFromReader(t *testing.T) {
	input := HookInput{
		HookEventName:        "Stop",
		LastAssistantMessage: "Should I continue with the refactoring?",
	}
	data, err := json.Marshal(input)
	require.Nil(t, err)

	code, msg := run(strings.NewReader(string(data)))
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "permission-seeking")
}
