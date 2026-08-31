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

// denial parses a refusal and returns its reason, failing the test when the
// call was allowed.
func denial(t *testing.T, out string) string {
	t.Helper()
	require.NotEmpty(t, out, "the call was allowed")
	var res response
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "PreToolUse", res.HookSpecificOutput.HookEventName)
	assert.Equal(t, "deny", res.HookSpecificOutput.PermissionDecision)
	return res.HookSpecificOutput.PermissionDecisionReason
}

func TestWritingACountIntoMarkdownIsRefused(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`)
	reason := denial(t, out)
	assert.Contains(t, reason, "15 plugins")
	assert.Contains(t, reason, "/repo/CLAUDE.md")
}

func TestEditingACountIntoMarkdownIsRefused(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Edit",
		"tool_input":{"file_path":"/repo/README.md","old_string":"a","new_string":"the four rules below"}}`)
	assert.Contains(t, denial(t, out), "four rules")
}

func TestAMultiEditIsJudgedEditByEdit(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"MultiEdit",
		"tool_input":{"file_path":"/repo/README.md","edits":[
			{"old_string":"a","new_string":"nothing to see"},
			{"old_string":"b","new_string":"it has three sections"}]}}`)
	assert.Contains(t, denial(t, out), "three sections")
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

// The refusal has to say what to write instead, or it costs a round trip.
func TestTheRefusalNamesTheRemedy(t *testing.T) {
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`)
	reason := denial(t, out)
	assert.Contains(t, reason, "let the")
	assert.Contains(t, reason, "reader count")
}
