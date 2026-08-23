package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rec is one JSONL line, built directly rather than through the plugin's
// own types, so a test failure cannot be masked by a shared bug.
func rec(t *testing.T, typ, role string, content any, ts time.Time) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"type":      typ,
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"role":    role,
			"content": content,
		},
	})
	require.NoError(t, err)
	return string(line)
}

func textBlock(text string) []map[string]any {
	return []map[string]any{{"type": "text", "text": text}}
}

func toolUseBlock(name string, input map[string]any) []map[string]any {
	return []map[string]any{{"type": "tool_use", "name": name, "input": input}}
}

func toolResultBlock() []map[string]any {
	return []map[string]any{{"type": "tool_result", "tool_use_id": "t1", "content": "ok"}}
}

func writeLines(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(joinLines(lines)), 0o644))
	return path
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

func TestANewPromptStartsATurnAndAToolResultDoesNot(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeLines(t, []string{
		rec(t, "user", "user", textBlock("do the thing"), base),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "echo hi"}), base.Add(1*time.Second)),
		rec(t, "user", "user", toolResultBlock(), base.Add(2*time.Second)),
		rec(t, "assistant", "assistant", textBlock("done"), base.Add(3*time.Second)),
	})
	turns := parseTurns(path)
	require.Len(t, turns, 1, "the tool_result must not start a second turn")
	assert.Len(t, turns[0].calls, 1)
	assert.Equal(t, "Bash: echo hi", turns[0].calls[0].disp)
}

func TestTwoRealPromptsProduceTwoTurns(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeLines(t, []string{
		rec(t, "user", "user", textBlock("first"), base),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "one"}), base.Add(1*time.Second)),
		rec(t, "user", "user", textBlock("second"), base.Add(1*time.Minute)),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "two"}), base.Add(1*time.Minute+time.Second)),
	})
	turns := parseTurns(path)
	require.Len(t, turns, 2)
	assert.Equal(t, "Bash: one", turns[0].calls[0].disp)
	assert.Equal(t, "Bash: two", turns[1].calls[0].disp)
}

func TestCanonicalJSONIgnoresKeyOrder(t *testing.T) {
	a := canonicalJSON(json.RawMessage(`{"b":2,"a":1}`))
	b := canonicalJSON(json.RawMessage(`{"a":1,"b":2}`))
	assert.Equal(t, a, b)
}

func TestTwoTurnsWithTheSameCommandGetTheSameSignature(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeLines(t, []string{
		rec(t, "user", "user", textBlock("check"), base),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "gh pr view 1"}), base.Add(time.Second)),
		rec(t, "user", "user", textBlock("check again"), base.Add(10*time.Second)),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "gh pr view 1"}), base.Add(11*time.Second)),
	})
	turns := parseTurns(path)
	require.Len(t, turns, 2)
	assert.Equal(t, turns[0].sig, turns[1].sig)
	assert.NotEmpty(t, turns[0].sig)
}

func TestADifferentCommandGetsADifferentSignature(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeLines(t, []string{
		rec(t, "user", "user", textBlock("a"), base),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "gh pr view 1"}), base.Add(time.Second)),
		rec(t, "user", "user", textBlock("b"), base.Add(10*time.Second)),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "gh pr view 2"}), base.Add(11*time.Second)),
	})
	turns := parseTurns(path)
	require.Len(t, turns, 2)
	assert.NotEqual(t, turns[0].sig, turns[1].sig)
}

func TestATurnWithNoToolCallHasAnEmptySignature(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeLines(t, []string{
		rec(t, "user", "user", textBlock("just talk"), base),
		rec(t, "assistant", "assistant", textBlock("okay"), base.Add(time.Second)),
	})
	turns := parseTurns(path)
	require.Len(t, turns, 1)
	assert.Empty(t, turns[0].sig)
}

func TestRenderCallShowsCompactJSONForNonBash(t *testing.T) {
	got := renderCall("Read", json.RawMessage(`{"file_path":"/a/b.go"}`))
	assert.Equal(t, `Read: {"file_path":"/a/b.go"}`, got)
}

func TestOneLineCollapsesWhitespaceAndTruncates(t *testing.T) {
	assert.Equal(t, "a b c", oneLine("a\n b\t c", 100))
	assert.Equal(t, "abc...", oneLine("abcdef", 3))
}

func TestAnUnreadableTranscriptReturnsNoTurns(t *testing.T) {
	assert.Nil(t, parseTurns(filepath.Join(t.TempDir(), "absent.jsonl")))
	assert.Nil(t, parseTurns(""))
}

func TestGarbageLinesAreSkippedNotFatal(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lines := []string{
		"not json at all",
		rec(t, "user", "user", textBlock("real"), base),
		rec(t, "assistant", "assistant", toolUseBlock("Bash", map[string]any{"command": "x"}), base.Add(time.Second)),
	}
	turns := parseTurns(writeLines(t, lines))
	require.Len(t, turns, 1)
	assert.Equal(t, "Bash: x", turns[0].calls[0].disp)
}
