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

// record builds one JSONL line for a message with the given role and blocks.
func record(t *testing.T, role string, blocks ...map[string]any) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"type":    role,
		"message": map[string]any{"role": role, "content": blocks},
	})
	require.NoError(t, err)
	return string(line)
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

// transcriptOf writes lines to a file and returns its path.
func transcriptOf(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return path
}

func TestFinalAssistantTextTakesTheLastAssistantMessage(t *testing.T) {
	path := transcriptOf(t,
		record(t, "assistant", text("first")),
		record(t, "user", text("a question")),
		record(t, "assistant", text("last")),
	)
	assert.Equal(t, "last", FinalAssistantText(path))
}

func TestFinalAssistantTextJoinsEveryTextBlock(t *testing.T) {
	path := transcriptOf(t, record(t, "assistant", text("one"), text("two")))
	assert.Equal(t, "one\ntwo", FinalAssistantText(path))
}

func TestFinalAssistantTextSkipsNonTextBlocks(t *testing.T) {
	path := transcriptOf(t,
		record(t, "assistant", text("the reply")),
		record(t, "assistant",
			map[string]any{"type": "thinking", "thinking": "not my problem"},
			map[string]any{"type": "tool_use", "name": "Bash"},
		),
	)
	assert.Equal(t, "the reply", FinalAssistantText(path), "a message with no text falls through to the one before it")
}

func TestFinalAssistantTextIgnoresUserRecords(t *testing.T) {
	path := transcriptOf(t, record(t, "user", text("that's pre-existing")))
	assert.Empty(t, FinalAssistantText(path))
}

func TestFinalAssistantTextSkipsUnparseableLines(t *testing.T) {
	path := transcriptOf(t, record(t, "assistant", text("kept")), "{not json", "")
	assert.Equal(t, "kept", FinalAssistantText(path))
}

func TestFinalAssistantTextOnMissingOrEmptyPath(t *testing.T) {
	assert.Empty(t, FinalAssistantText(""))
	assert.Empty(t, FinalAssistantText(filepath.Join(t.TempDir(), "absent.jsonl")))
}

func TestFinalAssistantTextOnAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	assert.Empty(t, FinalAssistantText(path))
}

func TestReadTailDropsThePartialFirstLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	pad := strings.Repeat("x", transcriptTailBytes)
	require.NoError(t, os.WriteFile(path, []byte(pad+"\nsecond\nthird\n"), 0o644))

	lines, err := readTail(path)
	require.NoError(t, err)
	require.NotEmpty(t, lines)
	assert.NotContains(t, string(lines[0]), "x", "the truncated head line is dropped")
}

func TestFinalAssistantTextReadsPastAHugeHead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	pad := record(t, "assistant", text(strings.Repeat("old ", transcriptTailBytes/4)))
	body := pad + "\n" + record(t, "assistant", text("the newest reply")) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	assert.Equal(t, "the newest reply", FinalAssistantText(path))
}
