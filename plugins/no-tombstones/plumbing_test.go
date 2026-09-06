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

// swap points the hook at path for the duration of a test.
func swap(t *testing.T, path string) {
	t.Helper()
	previous := slopfmtBinary
	slopfmtBinary = path
	t.Cleanup(func() { slopfmtBinary = previous })
}

func ask(t *testing.T, payload string) string {
	t.Helper()
	return run(strings.NewReader(payload))
}

// stripped is the answer for a write whose tombstone came out cleanly.
const stripped = `{"text":"func f() {}\n","changed":true,` +
	`"removed":["// This used to read the flag."]}`

// keptOne is the answer for a finding no deletion resolves.
const keptOne = `{"text":"call() // previously the other\n","changed":false,` +
	`"kept":[{"tell":"a former state","phrase":"previously",` +
	`"line":"call() // previously the other"}]}`

// clean is the answer for text the rule leaves alone.
const clean = `{"text":"func f() {}\n","changed":false}`

func mutation(t *testing.T, out string) (map[string]any, string) {
	t.Helper()
	require.NotEmpty(t, out, "the write was left alone")
	var res response
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "PreToolUse", res.HookSpecificOutput.HookEventName)
	assert.NotContains(t, out, "permissionDecision")

	var updated map[string]any
	require.NoError(t, json.Unmarshal(res.HookSpecificOutput.UpdatedInput, &updated))
	return updated, res.HookSpecificOutput.AdditionalContext
}

func TestAWriteIsRepairedRatherThanRefused(t *testing.T) {
	stub(t, stripped)
	updated, notice := mutation(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/a.go","content":"// This used to read the flag.\nfunc f() {}\n"}}`))

	assert.Equal(t, "func f() {}\n", updated["content"])
	assert.Contains(t, notice, "used to read the flag")
}

func TestAnEditCarriesItsOtherKeysThrough(t *testing.T) {
	stub(t, stripped)
	updated, _ := mutation(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Edit",
		"tool_input":{"file_path":"/repo/a.go","old_string":"x","new_string":"// This used to read the flag.\nfunc f() {}\n","replace_all":true}}`))

	assert.Equal(t, "func f() {}\n", updated["new_string"])
	assert.Equal(t, "x", updated["old_string"])
	assert.Equal(t, true, updated["replace_all"])
}

func TestEveryEditOfAMultiEditIsPutToTheTool(t *testing.T) {
	stub(t, stripped)
	updated, _ := mutation(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"MultiEdit",
		"tool_input":{"file_path":"/repo/a.go","edits":[
			{"old_string":"a","new_string":"// This used to read the flag.\nfunc f() {}\n"},
			{"old_string":"b","new_string":"// This used to read the flag.\nfunc f() {}\n"}]}}`))

	edits, ok := updated["edits"].([]any)
	require.True(t, ok)
	for _, edit := range edits {
		assert.Equal(t, "func f() {}\n", edit.(map[string]any)["new_string"])
	}
}

func TestAFindingNoDeletionResolvesRefusesTheWrite(t *testing.T) {
	stub(t, keptOne)
	out := ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/a.go","content":"call() // previously the other\n"}}`)

	require.NotEmpty(t, out)
	var res response
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, "deny", res.HookSpecificOutput.PermissionDecision)
	assert.Contains(t, res.HookSpecificOutput.PermissionDecisionReason, "a former state")
}

func TestACleanWriteIsLeftAlone(t *testing.T) {
	stub(t, clean)
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/a.go","content":"func f() {}\n"}}`))
}

func TestAMissingToolLetsTheWriteThrough(t *testing.T) {
	swap(t, filepath.Join(t.TempDir(), "absent"))
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/a.go","content":"// This used to read the flag.\n"}}`))
}

func TestAnUnreadableAnswerLetsTheWriteThrough(t *testing.T) {
	stub(t, "not json at all")
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/a.go","content":"// This used to read the flag.\n"}}`))
}

func TestAToolThatWritesNoFileIsNotJudged(t *testing.T) {
	stub(t, stripped)
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash",
		"tool_input":{"command":"echo hi"}}`))
}

func TestAnotherEventIsNotJudged(t *testing.T) {
	stub(t, stripped)
	assert.Empty(t, ask(t, `{"hook_event_name":"PostToolUse","tool_name":"Write",
		"tool_input":{"file_path":"/repo/a.go","content":"// This used to read the flag.\n"}}`))
}

func TestAnUnparseablePayloadIsNotJudged(t *testing.T) {
	stub(t, stripped)
	assert.Empty(t, ask(t, "{"))
}

func TestAnUnparseableToolInputIsNotJudged(t *testing.T) {
	stub(t, stripped)
	assert.Empty(t, ask(t, `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":"nope"}`))
}

func TestTheCommentCapComesFromTheEnvironment(t *testing.T) {
	t.Setenv("NO_TOMBSTONES_MAX_COMMENT_LINES", "3")
	assert.Equal(t, 3, maxCommentLines())

	t.Setenv("NO_TOMBSTONES_MAX_COMMENT_LINES", "not a number")
	assert.Equal(t, defaultMaxCommentLines, maxCommentLines())
}

func TestTheRefusalSaysWhatItDidNotPrint(t *testing.T) {
	var hits []Hit
	for range 9 {
		hits = append(hits, Hit{Tell: "a date", Phrase: "2026-01-01", Line: "// 2026-01-01"})
	}
	assert.Contains(t, reason("/repo/a.go", hits), "and 3 more")
}
