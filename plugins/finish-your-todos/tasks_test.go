package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taskCreateLine builds an assistant line containing a TaskCreate call.
func taskCreateLine(t *testing.T, toolUseID, subject string) string {
	t.Helper()
	return marshalLine(t, "assistant", []any{
		map[string]any{
			"type":  "tool_use",
			"id":    toolUseID,
			"name":  "TaskCreate",
			"input": map[string]any{"subject": subject, "description": subject + " description"},
		},
	})
}

// taskResultLine builds the user line carrying the tool_result that answers a
// TaskCreate -- the only place the new task's id appears.
func taskResultLine(t *testing.T, toolUseID, text string) string {
	t.Helper()
	return marshalLine(t, "user", []any{
		map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": text},
	})
}

// taskResultBlocksLine is the same, with content as an array of text blocks
// rather than a bare string.
func taskResultBlocksLine(t *testing.T, toolUseID, text string) string {
	t.Helper()
	return marshalLine(t, "user", []any{
		map[string]any{
			"type":        "tool_result",
			"tool_use_id": toolUseID,
			"content":     []any{map[string]any{"type": "text", "text": text}},
		},
	})
}

// taskUpdateLine builds an assistant line containing a TaskUpdate call.
func taskUpdateLine(t *testing.T, taskID string, input map[string]any) string {
	t.Helper()
	in := map[string]any{"taskId": taskID}
	for k, v := range input {
		in[k] = v
	}
	return marshalLine(t, "assistant", []any{
		map[string]any{"type": "tool_use", "id": "tu_" + taskID, "name": "TaskUpdate", "input": in},
	})
}

func marshalLine(t *testing.T, role string, content []any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type":    role,
		"message": map[string]any{"role": role, "content": content},
	})
	require.NoError(t, err)
	return string(data)
}

// A filed task nobody finished must block the stop. This is the case the gate
// was blind to: environments with the task tools have no TodoWrite at all, so
// scanning only for TodoWrite allowed every stop.
func TestTaskToolsBlockWhenPending(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Wire up the deny rules"),
		taskResultLine(t, "tu_1", "Task #1 created successfully: Wire up the deny rules"),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Wire up the deny rules")
	assert.Contains(t, msg, "Not started:")
}

func TestTaskToolsAllowWhenCompleted(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Wire up the deny rules"),
		taskResultLine(t, "tu_1", "Task #1 created successfully: Wire up the deny rules"),
		taskUpdateLine(t, "1", map[string]any{"status": "completed"}),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 0, code)
	assert.Empty(t, msg)
}

func TestTaskToolsInProgressIsUnfinished(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Refactor the schema"),
		taskResultLine(t, "tu_1", "Task #1 created successfully: Refactor the schema"),
		taskUpdateLine(t, "1", map[string]any{"status": "in_progress"}),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "In progress:")
	assert.Contains(t, msg, "Refactor the schema")
}

func TestTaskToolsDeletedCountsAsFinished(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Obsolete work"),
		taskResultLine(t, "tu_1", "Task #1 created successfully: Obsolete work"),
		taskUpdateLine(t, "1", map[string]any{"status": "deleted"}),
	)
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}

// Only the LAST status for a task counts, and a renamed subject is reported
// under its new name.
func TestTaskToolsLastStatusAndRenameWin(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Old subject"),
		taskResultLine(t, "tu_1", "Task #1 created successfully: Old subject"),
		taskUpdateLine(t, "1", map[string]any{"status": "completed"}),
		taskUpdateLine(t, "1", map[string]any{"status": "pending", "subject": "New subject"}),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "New subject")
	assert.NotContains(t, msg, "Old subject")
}

// Several tasks, mixed states: only the unfinished ones are named.
func TestTaskToolsReportsOnlyUnfinished(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Done thing"),
		taskResultLine(t, "tu_1", "Task #1 created successfully: Done thing"),
		taskCreateLine(t, "tu_2", "Undone thing"),
		taskResultLine(t, "tu_2", "Task #2 created successfully: Undone thing"),
		taskUpdateLine(t, "1", map[string]any{"status": "completed"}),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Undone thing")
	assert.NotContains(t, msg, "Done thing")
}

// A tool_result whose content is an array of text blocks parses the same way.
func TestTaskToolsResultAsBlocks(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Blocky result"),
		taskResultBlocksLine(t, "tu_1", "Task #7 created successfully: Blocky result"),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Blocky result")
}

// A create whose result never arrived (an interrupted turn) has no id to track,
// so it is not counted rather than being counted under a wrong id.
func TestTaskToolsCreateWithoutResultIsIgnored(t *testing.T) {
	path := writeTranscript(t, taskCreateLine(t, "tu_1", "Never confirmed"))
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}

// An update naming a task that was never created cannot resurrect one.
func TestTaskToolsUpdateForUnknownIDIgnored(t *testing.T) {
	path := writeTranscript(t, taskUpdateLine(t, "42", map[string]any{"status": "pending"}))
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}

// A result that does not announce an id (an error string, say) is skipped.
func TestTaskToolsUnparseableResultIgnored(t *testing.T) {
	path := writeTranscript(t,
		taskCreateLine(t, "tu_1", "Failed create"),
		taskResultLine(t, "tu_1", "Error: could not create task"),
	)
	code, _ := evalStop(t, path, false)
	assert.Equal(t, 0, code)
}

// Both sources are read in one pass, so a session that used TodoWrite AND the
// task tools reports every unfinished item from both.
func TestTodoWriteAndTaskToolsCombine(t *testing.T) {
	path := writeTranscript(t,
		todoWriteLine(t, todo("Legacy todo", "pending")),
		taskCreateLine(t, "tu_1", "Modern task"),
		taskResultLine(t, "tu_1", "Task #1 created successfully: Modern task"),
	)
	code, msg := evalStop(t, path, false)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "Legacy todo")
	assert.Contains(t, msg, "Modern task")
}

func TestResultTextShapes(t *testing.T) {
	assert.Equal(t, "", resultText(nil))
	assert.Equal(t, "plain", resultText(json.RawMessage(`"plain"`)))
	assert.Equal(t, "a\nb\n", resultText(json.RawMessage(`[{"text":"a"},{"text":"b"}]`)))
	assert.Equal(t, "", resultText(json.RawMessage(`{"unexpected":true}`)))
}

func TestIncompleteTasksClassification(t *testing.T) {
	inProgress, pending := incompleteTasks([]taskState{
		{ID: "1", Subject: "a", Status: "in_progress"},
		{ID: "2", Subject: "b", Status: "pending"},
		{ID: "3", Subject: "c", Status: "completed"},
		{ID: "4", Subject: "d", Status: "something-new"},
	})
	require.Len(t, inProgress, 1)
	require.Len(t, pending, 1)
	assert.Equal(t, "a", inProgress[0].Content)
	assert.Equal(t, "b", pending[0].Content)
}
