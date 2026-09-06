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

// The rule itself is slopfmt's, and slopfmt tests it. What is left here is the
// plumbing: which writes are worth a subprocess, how the answer is spliced back
// into the payload, and that every failure lets the write through untouched.
//
// So the tests drive a stub in place of the binary. A real slopfmt on PATH
// would make the suite depend on which version happens to be installed.

// stub puts a script in place of slopfmt that answers with body on stdout.
func stub(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "slopfmt-stub")
	quoted := "'" + strings.ReplaceAll(body, "'", `'\''`) + "'"
	script := "#!/bin/sh\ncat > /dev/null\nprintf '%s' " + quoted + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	swap(t, path)
}

// swap points the hook at path for one test.
func swap(t *testing.T, path string) {
	t.Helper()
	previous := slopfmtBinary
	slopfmtBinary = path
	t.Cleanup(func() { slopfmtBinary = previous })
}

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

const strippedOne = `{"text":"This repo's plugins ride in the payload.","changed":true,` +
	`"removed":["15 plugins"],"lines":["This repo's 15 plugins ride in the payload."]}`

const unchanged = `{"text":"","changed":false}`

func TestWritingACountIntoMarkdownIsStripped(t *testing.T) {
	stub(t, strippedOne)
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`)
	input, notice := repaired(t, out)
	assert.Equal(t, "This repo's plugins ride in the payload.", input["content"])
	assert.Contains(t, notice, "15 plugins")
	assert.Contains(t, notice, "/repo/CLAUDE.md")
}

func TestEditingACountIntoMarkdownIsStripped(t *testing.T) {
	stub(t, `{"text":"the rules below","changed":true,"removed":["four rules"],"lines":["the four rules below"]}`)
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Edit",
		"tool_input":{"file_path":"/repo/README.md","old_string":"a","new_string":"the four rules below"}}`)
	input, _ := repaired(t, out)
	assert.Equal(t, "the rules below", input["new_string"])
	// The keys this hook does not touch have to survive the rewrite, or the
	// edit it hands back is a different edit.
	assert.Equal(t, "a", input["old_string"])
	assert.Equal(t, "/repo/README.md", input["file_path"])
}

func TestEveryEditOfAMultiEditGoesThroughTheTool(t *testing.T) {
	stub(t, `{"text":"it has sections","changed":true,"removed":["three sections"],"lines":["it has three sections"]}`)
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"MultiEdit",
		"tool_input":{"file_path":"/repo/README.md","edits":[
			{"old_string":"b","new_string":"it has three sections"}]}}`)
	input, _ := repaired(t, out)
	edits, ok := input["edits"].([]any)
	require.True(t, ok)
	require.Len(t, edits, 1)
	assert.Equal(t, "it has sections", edits[0].(map[string]any)["new_string"])
}

// The negative control for every case above: the same text in a file this
// plugin does not govern never reaches the tool at all.
func TestTheSameCountInANonMarkdownFileIsAllowed(t *testing.T) {
	stub(t, strippedOne)
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/main.go","content":"// This repo's 15 plugins ride in the payload."}}`))
}

func TestATextTheToolLeavesAloneIsAllowed(t *testing.T) {
	stub(t, unchanged)
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"Every plugin this repo installs rides in the payload."}}`))
}

// A guard that mangles a write because its tool is absent is worse than none.
func TestAMissingToolLetsTheWriteThrough(t *testing.T) {
	swap(t, filepath.Join(t.TempDir(), "not-installed"))
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`))
}

func TestAnUnreadableAnswerLetsTheWriteThrough(t *testing.T) {
	stub(t, "not json at all")
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`))
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
	stub(t, strippedOne)
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/CLAUDE.md","content":"This repo's 15 plugins ride in the payload."}}`)
	_, notice := repaired(t, out)
	assert.Contains(t, notice, "let the")
	assert.Contains(t, notice, "reader count")
	assert.Contains(t, notice, "already gone")
}
