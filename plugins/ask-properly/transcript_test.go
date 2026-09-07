package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

func TestReadTurnSeesTheAskToolInThisTurn(t *testing.T) {
	path := write(t,
		userPrompt("go"),
		assistantAsk(),
		assistantText("asked"),
	)
	assert.True(t, ReadTurn(path).UsedAskTool)
}

// A user record carrying only tool_result blocks answers a call from earlier
// in the SAME turn, so it must not split the turn.
func TestAToolResultDoesNotStartANewTurn(t *testing.T) {
	toolResult := record("user", map[string]any{"type": "tool_result", "text": "ok"})
	path := write(t,
		userPrompt("go"),
		assistantAsk(),
		toolResult,
		assistantText("done"),
	)
	assert.True(t, ReadTurn(path).UsedAskTool)
}

func TestAnEarlierTurnsAskToolIsNotSeen(t *testing.T) {
	path := write(t,
		userPrompt("one"),
		assistantAsk(),
		userPrompt("two"),
		assistantText("done"),
	)
	assert.False(t, ReadTurn(path).UsedAskTool)
}

func TestUnreadableTranscriptIsAZeroTurn(t *testing.T) {
	assert.Equal(t, Turn{}, ReadTurn(""))
	assert.Equal(t, Turn{}, ReadTurn("/nope/missing.jsonl"))
}

func TestGarbageLinesAreSkipped(t *testing.T) {
	path := write(t,
		"{not json",
		userPrompt("go"),
		assistantAsk(),
	)
	assert.True(t, ReadTurn(path).UsedAskTool)
}
