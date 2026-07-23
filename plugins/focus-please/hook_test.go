package main

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

// withTemp isolates the marker directory to this test's own tempdir so
// concurrent test runs never collide over marker files.
func withTemp(t *testing.T) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
}

func TestQuestionArmsBlockAndAddsContext(t *testing.T) {
	withTemp(t)
	out := run(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt":"why is this broken?"}`))
	require.Contains(t, out, "additionalContext")

	require.Contains(t, out, "UserPromptSubmit")

	require.True(t, markerExists("s1"))

}

func TestNoQuestionDisarmsBlock(t *testing.T) {
	withTemp(t)
	setMarker("s1") // stale marker left by a prior turn
	out := run(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt":"go ahead and do it."}`))
	require.Equal(t, "{}", out)

	require.False(t, markerExists("s1"))

}

func TestPreToolUseDeniesWhileArmed(t *testing.T) {
	withTemp(t)
	setMarker("s1")
	out := run(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"Bash"}`))
	require.Contains(t, out, `"permissionDecision":"deny"`)

	require.Contains(t, out, "focus-please")

}

func TestPreToolUseAllowsWhenDisarmed(t *testing.T) {
	withTemp(t)
	out := run(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"Bash"}`))
	require.Equal(t, "{}", out)

}

func TestStopDisarmsBlock(t *testing.T) {
	withTemp(t)
	setMarker("s1")
	out := run(strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1","stop_hook_active":false}`))
	require.Equal(t, "{}", out)

	require.False(t, markerExists("s1"))

}

// TestFullTurnCycle walks a question turn end to end: armed on submit,
// tools denied mid-turn, cleared on stop, tools allowed on the next turn.
func TestFullTurnCycle(t *testing.T) {
	withTemp(t)
	run(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt":"can you check?"}`))
	require.True(t, markerExists("s1"))

	deny := run(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"Read"}`))
	require.Contains(t, deny, "deny")

	run(strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1"}`))
	allow := run(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"Read"}`))
	require.Equal(t, "{}", allow)

}

// TestSessionIsolation confirms one session's block never affects another.
func TestSessionIsolation(t *testing.T) {
	withTemp(t)
	run(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"a","prompt":"huh?"}`))
	out := run(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"b","tool_name":"Bash"}`))
	require.Equal(t, "{}", out)

}

func TestUnknownEventIsNoOp(t *testing.T) {
	withTemp(t)
	out := run(strings.NewReader(`{"hook_event_name":"PostToolUse","session_id":"s1"}`))
	require.Equal(t, "{}", out)

}

func TestBadJSONIsNoOp(t *testing.T) {
	withTemp(t)
	out := run(strings.NewReader(`not json`))
	require.Equal(t, "{}", out)

}

// TestQuestionMarkAnywhere: a "?" anywhere in the message arms the block,
// even when the question is buried in a longer prompt.
func TestQuestionMarkAnywhere(t *testing.T) {
	withTemp(t)
	run(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt":"Fix the bug. Also, what does foo do? Thanks."}`))
	require.True(t, markerExists("s1"))

}
