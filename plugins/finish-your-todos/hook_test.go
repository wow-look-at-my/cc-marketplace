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

// todo is a brief constructor for a TodoItem in tests.
func todo(content, status string) TodoItem {
	return TodoItem{Content: content, Status: status, ActiveForm: content + " (active)"}
}

// todoWriteLine builds one assistant transcript line containing a TodoWrite call.
func todoWriteLine(t *testing.T, todos ...TodoItem) string {
	t.Helper()
	line := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"name":  "TodoWrite",
					"input": map[string]any{"todos": todos},
				},
			},
		},
	}
	data, err := json.Marshal(line)
	require.NoError(t, err)
	return string(data)
}

// writeTranscript writes JSONL lines to a temp file and returns its path.
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644))
	return path
}

// evalStop marshals a Stop HookInput and runs evaluate.
func evalStop(t *testing.T, transcriptPath string, stopHookActive bool) (int, string) {
	t.Helper()
	data, err := json.Marshal(HookInput{
		HookEventName:  "Stop",
		TranscriptPath: transcriptPath,
		StopHookActive: stopHookActive,
	})
	require.NoError(t, err)
	return evaluate(data)
}

func TestBlockPending(t *testing.T) {
	path := writeTranscript(t, todoWriteLine(t, todo("Write the parser", "pending")))
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Write the parser")
	assert.Contains(t, msg, "1 incomplete item")
}

func TestBlockInProgress(t *testing.T) {
	path := writeTranscript(t, todoWriteLine(t, todo("Refactor the handler", "in_progress")))
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Refactor the handler")
	assert.Contains(t, msg, "In progress:")
}

func TestBlockListsBothSections(t *testing.T) {
	path := writeTranscript(t, todoWriteLine(t,
		todo("Build the feature", "in_progress"),
		todo("Add tests", "pending"),
		todo("Update docs", "pending"),
	))
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "3 incomplete items")
	assert.Contains(t, msg, "In progress:")
	assert.Contains(t, msg, "Build the feature")
	assert.Contains(t, msg, "Not started:")
	assert.Contains(t, msg, "Add tests")
	assert.Contains(t, msg, "Update docs")
	// The escape hatch must be explained so Claude knows how to legitimately stop.
	assert.Contains(t, msg, "TodoWrite")
}

func TestAllowAllCompleted(t *testing.T) {
	path := writeTranscript(t, todoWriteLine(t,
		todo("Build the feature", "completed"),
		todo("Add tests", "completed"),
	))
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 0, code)
	assert.Empty(t, msg)
}

func TestAllowNoTodoWriteEver(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"All done."}]}}`
	path := writeTranscript(t, line)
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}

func TestAllowEmptyTodoList(t *testing.T) {
	path := writeTranscript(t, todoWriteLine(t))
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}

func TestLatestTodoWriteWins_NowComplete(t *testing.T) {
	// An early list had work pending; the latest list shows it all completed.
	path := writeTranscript(t,
		todoWriteLine(t, todo("Build the feature", "in_progress")),
		todoWriteLine(t, todo("Build the feature", "completed")),
	)
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}

func TestLatestTodoWriteWins_NowIncomplete(t *testing.T) {
	// An early list was all done; a later list reopened/added work.
	path := writeTranscript(t,
		todoWriteLine(t, todo("Build the feature", "completed")),
		todoWriteLine(t, todo("Handle the edge case", "pending")),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Handle the edge case")
}

func TestInterveningToolCallsDoNotClobberTodos(t *testing.T) {
	bashLine := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`
	path := writeTranscript(t,
		todoWriteLine(t, todo("Ship the fix", "pending")),
		bashLine,
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Ship the fix")
}

func TestStopHookActiveLoopGuard(t *testing.T) {
	// Even with incomplete todos, an already-active stop hook must allow the stop
	// so a stuck session cannot hang forever.
	path := writeTranscript(t, todoWriteLine(t, todo("Never-ending task", "in_progress")))
	code, msg := evalStop(t, path, true)
	assert.Equal(t, 0, code)
	assert.Empty(t, msg)
}

func TestAllowWrongEvent(t *testing.T) {
	path := writeTranscript(t, todoWriteLine(t, todo("Pending work", "pending")))
	data, err := json.Marshal(HookInput{
		HookEventName:  "PreToolUse",
		TranscriptPath: path,
	})
	require.NoError(t, err)
	code, _ := evaluate(data)
	assert.Equal(t, 0, code)
}

func TestAllowInvalidJSON(t *testing.T) {
	code, _ := evaluate([]byte("not json"))
	assert.Equal(t, 0, code)
}

func TestAllowMissingTranscript(t *testing.T) {
	code, _ := evalStop(t, "/nonexistent/transcript.jsonl", false)
	assert.Equal(t, 0, code)
}

func TestAllowEmptyTranscriptPath(t *testing.T) {
	code, _ := evalStop(t, "", false)
	assert.Equal(t, 0, code)
}

func TestStringContentLinesAreSkipped(t *testing.T) {
	// User turns often store content as a plain string, not an array of blocks.
	userLine := `{"type":"user","message":{"role":"user","content":"please continue"}}`
	path := writeTranscript(t,
		userLine,
		todoWriteLine(t, todo("Finish onboarding", "pending")),
		userLine,
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Finish onboarding")
}

func TestMalformedLinesAreSkipped(t *testing.T) {
	path := writeTranscript(t,
		"this is not json",
		`{"type":"attachment"}`,
		todoWriteLine(t, todo("Real task", "in_progress")),
		"{ broken json",
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Real task")
}

func TestRunFromReader(t *testing.T) {
	path := writeTranscript(t, todoWriteLine(t, todo("Wire up the endpoint", "pending")))
	data, err := json.Marshal(HookInput{
		HookEventName:  "Stop",
		TranscriptPath: path,
	})
	require.NoError(t, err)
	code, msg := run(strings.NewReader(string(data)))
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Wire up the endpoint")
}

func TestUnknownStatusFailsOpen(t *testing.T) {
	// A status outside the known enum must not block (fail open).
	path := writeTranscript(t, todoWriteLine(t, todo("Mystery task", "cancelled")))
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}
