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

func TestReadTurnReturnsTheLastAssistantText(t *testing.T) {
	path := write(t,
		userPrompt("go"),
		assistantText("first"),
		assistantText("second"),
	)
	assert.Equal(t, "second", ReadTurn(path).FinalText)
}

func TestReadTurnSeesTheAskToolInThisTurn(t *testing.T) {
	path := write(t,
		userPrompt("go"),
		assistantAsk(),
		assistantText("asked"),
	)
	turn := ReadTurn(path)
	assert.True(t, turn.UsedAskTool)
	assert.Equal(t, "asked", turn.FinalText)
}

// A user record carrying only tool_result blocks answers a call from earlier
// in the SAME turn, so it must not split the turn.
func TestAToolResultDoesNotStartANewTurn(t *testing.T) {
	toolResult := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","text":"ok"}]}}`
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
	turn := ReadTurn(path)
	assert.False(t, turn.UsedAskTool)
	assert.Equal(t, "done", turn.FinalText)
}

func TestUnreadableTranscriptIsAZeroTurn(t *testing.T) {
	assert.Equal(t, Turn{}, ReadTurn(""))
	assert.Equal(t, Turn{}, ReadTurn("/nope/missing.jsonl"))
}

func TestGarbageLinesAreSkipped(t *testing.T) {
	path := write(t,
		"{not json",
		userPrompt("go"),
		assistantText("fine"),
	)
	assert.Equal(t, "fine", ReadTurn(path).FinalText)
}
