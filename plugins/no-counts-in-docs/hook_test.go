package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ask runs the hook over a payload and returns its stdout.
func ask(t *testing.T, payload string) string {
	t.Helper()
	return run(strings.NewReader(payload))
}

// repaired parses a mutation and returns the updated input and the notice,
// failing the test when the write went through untouched.
func repaired(t *testing.T, out string) (map[string]any, string) {
	t.Helper()
	require.NotEmpty(t, out, "the write was left alone")
	var res response
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "PreToolUse", res.HookSpecificOutput.HookEventName)
	// A permissionDecision would take the verdict away from the permission
	// flow. This hook only rewrites.
	assert.NotContains(t, out, "permissionDecision")
	return res.HookSpecificOutput.UpdatedInput, res.HookSpecificOutput.AdditionalContext
}

func TestWritingACountIntoMarkdownIsStripped(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`)
	input, notice := repaired(t, out)
	assert.Equal(t, "This repo's plugins ride in the payload.", input["content"])
	assert.Contains(t, notice, "15 plugins")
	assert.Contains(t, notice, "/repo/CLAUDE.md")
}

func TestEditingACountIntoMarkdownIsStripped(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Edit",
		"tool_input":{"file_path":"/repo/README.md","old_string":"a","new_string":"the four rules below"}}`)
	input, _ := repaired(t, out)
	assert.Equal(t, "the rules below", input["new_string"])
	// The keys this hook does not touch have to survive the rewrite, or the
	// edit it hands back is a different edit.
	assert.Equal(t, "a", input["old_string"])
	assert.Equal(t, "/repo/README.md", input["file_path"])
}

func TestAMultiEditIsRepairedEditByEdit(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"MultiEdit",
		"tool_input":{"file_path":"/repo/README.md","edits":[
			{"old_string":"a","new_string":"nothing to see"},
			{"old_string":"b","new_string":"it has three sections"}]}}`)
	input, _ := repaired(t, out)
	edits, ok := input["edits"].([]any)
	require.True(t, ok)
	require.Len(t, edits, 2)
	assert.Equal(t, "nothing to see", edits[0].(map[string]any)["new_string"])
	assert.Equal(t, "it has sections", edits[1].(map[string]any)["new_string"])
}

// A count inside a fenced block is a literal, and the block comes back whole.
func TestACodeBlockIsNeverTouched(t *testing.T) {
	content := "Prose here.\n\n```go\n// it has three sections\nconst n = 3\n```\n"
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": "/repo/CLAUDE.md", "content": content},
	})
	require.NoError(t, err)
	assert.Empty(t, ask(t, string(payload)))
}

// The negative control for every case above: the same text in a file this
// plugin does not govern must pass.
func TestTheSameCountInANonMarkdownFileIsAllowed(t *testing.T) {
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/main.go","content":"// This repo's 15 plugins ride in the payload."}}`))
}

func TestMarkdownWithoutACountIsAllowed(t *testing.T) {
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"Every plugin this repo installs rides in the payload."}}`))
}

func TestAToolThatDoesNotWriteFilesIsAllowed(t *testing.T) {
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash",
		"tool_input":{"command":"echo this repo has 15 plugins > /repo/CLAUDE.md"}}`))
}

func TestAnotherEventIsAllowed(t *testing.T) {
	assert.Empty(t, ask(t, `{"hook_event_name":"PostToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"this repo's 15 plugins"}}`))
}

func TestAnUnparseablePayloadIsAllowed(t *testing.T) {
	assert.Empty(t, ask(t, "not json at all"))
}

func TestAnUnparseableToolInputIsAllowed(t *testing.T) {
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":"a string"}`))
}

func TestAnEmptyPayloadIsAllowed(t *testing.T) {
	assert.Empty(t, ask(t, ""))
}

// The notice has to say what was done and what to write instead, or the model
// cannot tell why its own text came back different.
func TestTheNoticeNamesTheRemedy(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`)
	_, notice := repaired(t, out)
	assert.Contains(t, notice, "let the")
	assert.Contains(t, notice, "reader count")
	assert.Contains(t, notice, "already gone")
}
