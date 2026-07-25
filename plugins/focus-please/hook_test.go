package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// withTemp isolates the marker directory to this test's own tempdir so
// concurrent test runs never collide over marker files.
func withTemp(t *testing.T) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
}

func fire(t *testing.T, payload string) result {
	t.Helper()
	return run(strings.NewReader(payload))
}

func payload(fields map[string]any) string {
	b, _ := json.Marshal(fields)
	return string(b)
}

func ups(session, prompt string) string {
	return payload(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      session,
		"prompt":          prompt,
	})
}

func pre(session, tool string) string {
	return payload(map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      session,
		"tool_name":       tool,
	})
}

func stopEvent(session string, hookActive bool) string {
	return payload(map[string]any{
		"hook_event_name":  "Stop",
		"session_id":       session,
		"stop_hook_active": hookActive,
	})
}

func TestQuestionArmsBlockAndAddsContext(t *testing.T) {
	withTemp(t)
	res := fire(t, ups("s1", "why is this broken?"))
	require.Contains(t, res.stdout, "additionalContext")
	require.Contains(t, res.stdout, "UserPromptSubmit")
	require.Equal(t, 0, res.code)
	require.True(t, markerExists("s1", markerPending))
	// A first prompt is not an interjection, so no resume is recorded.
	require.False(t, markerExists("s1", markerResume))
}

func TestNoQuestionDisarmsBlock(t *testing.T) {
	withTemp(t)
	setMarker("s1", markerPending) // stale marker left by a prior turn
	res := fire(t, ups("s1", "go ahead and do it."))
	require.Equal(t, "{}", res.stdout)
	require.False(t, markerExists("s1", markerPending))
}

func TestPreToolUseDeniesWhileArmed(t *testing.T) {
	withTemp(t)
	setMarker("s1", markerPending)
	res := fire(t, pre("s1", "Bash"))
	require.Contains(t, res.stdout, `"permissionDecision":"deny"`)
	require.Contains(t, res.stdout, "focus-please")
}

// TestPreToolUseAllowsLookupsWhileArmed: the model may keep reading and
// searching for an answer even while the block is armed.
func TestPreToolUseAllowsLookupsWhileArmed(t *testing.T) {
	withTemp(t)
	setMarker("s1", markerPending)
	for _, tool := range []string{
		"Read", "Grep", "Glob",
		// This marketplace's own plugins restore the disabled builtins as
		// MCP tools; they must count as lookups too.
		"mcp__plugin_grep_grep__Grep",
		"mcp__plugin_glob_glob__Glob",
	} {
		res := fire(t, pre("s1", tool))
		require.Equal(t, "{}", res.stdout, "%s must be allowed while armed", tool)
	}
}

// TestPreToolUseDeniesActingToolsWhileArmed: everything that is not a
// read-only lookup stays blocked.
func TestPreToolUseDeniesActingToolsWhileArmed(t *testing.T) {
	withTemp(t)
	setMarker("s1", markerPending)
	for _, tool := range []string{
		"Bash", "Write", "Edit", "Task", "WebFetch", "TodoWrite",
		"mcp__github__create_issue",
	} {
		res := fire(t, pre("s1", tool))
		require.Contains(t, res.stdout, `"permissionDecision":"deny"`,
			"%s must be denied while armed", tool)
	}
}

func TestPreToolUseAllowsWhenDisarmed(t *testing.T) {
	withTemp(t)
	res := fire(t, pre("s1", "Bash"))
	require.Equal(t, "{}", res.stdout)
}

func TestStopDisarmsBlock(t *testing.T) {
	withTemp(t)
	setMarker("s1", markerPending)
	res := fire(t, stopEvent("s1", false))
	require.Equal(t, "{}", res.stdout)
	require.Equal(t, 0, res.code)
	require.False(t, markerExists("s1", markerPending))
}

// TestFullTurnCycle walks a question turn end to end: armed on submit,
// acting tools denied mid-turn, cleared on stop, tools allowed next turn.
func TestFullTurnCycle(t *testing.T) {
	withTemp(t)
	fire(t, ups("s1", "can you check?"))
	require.True(t, markerExists("s1", markerPending))

	deny := fire(t, pre("s1", "Bash"))
	require.Contains(t, deny.stdout, "deny")

	stop := fire(t, stopEvent("s1", false))
	require.Equal(t, 0, stop.code, "an uninterrupted turn may stop")

	allow := fire(t, pre("s1", "Bash"))
	require.Equal(t, "{}", allow.stdout)
}

// TestMidTurnInterjectionRefusesStopOnce is the core of the resume rule: a
// user message that lands while the assistant is still working must not end
// the turn once answered -- the interrupted work has to continue.
func TestMidTurnInterjectionRefusesStopOnce(t *testing.T) {
	withTemp(t)
	// Turn 1 begins; the assistant starts working (no Stop yet).
	fire(t, ups("s1", "please refactor the parser"))
	require.False(t, markerExists("s1", markerResume))

	// The user interjects mid-turn with a question.
	res := fire(t, ups("s1", "wait, why is it slow?"))
	require.Contains(t, res.stdout, "additionalContext")
	require.Contains(t, res.stdout, "resume the work it interrupted",
		"the injected note must tell the model its reply does not end the turn")
	require.True(t, markerExists("s1", markerResume))

	// The assistant answers and tries to end the turn: refused, once.
	stop := fire(t, stopEvent("s1", false))
	require.Equal(t, 2, stop.code)
	require.Contains(t, stop.stderr, "Do not stop here")
	require.False(t, markerExists("s1", markerResume), "the flag is consumed")

	// The continuation has full tool access again.
	require.False(t, markerExists("s1", markerPending))
	require.Equal(t, "{}", fire(t, pre("s1", "Bash")).stdout)

	// The next stop goes through -- never an infinite loop.
	stop2 := fire(t, stopEvent("s1", false))
	require.Equal(t, 0, stop2.code)
	require.Empty(t, stop2.stderr)
}

// TestInterjectionWithoutQuestionStillResumes: the resume rule keys off the
// interruption, not the question mark.
func TestInterjectionWithoutQuestionStillResumes(t *testing.T) {
	withTemp(t)
	fire(t, ups("s1", "start the migration"))
	res := fire(t, ups("s1", "also rename the table."))
	require.Equal(t, "{}", res.stdout, "no question mark: no block, no context note")
	require.False(t, markerExists("s1", markerPending))
	require.True(t, markerExists("s1", markerResume))

	stop := fire(t, stopEvent("s1", false))
	require.Equal(t, 2, stop.code)
}

// TestUninterruptedTurnStopsCleanly: a turn nobody interrupted must never be
// refused.
func TestUninterruptedTurnStopsCleanly(t *testing.T) {
	withTemp(t)
	fire(t, ups("s1", "what does foo do?"))
	stop := fire(t, stopEvent("s1", false))
	require.Equal(t, 0, stop.code)
	require.Empty(t, stop.stderr)

	// And the turn after it, too (the active marker was cleared).
	fire(t, ups("s1", "thanks, now fix it"))
	require.False(t, markerExists("s1", markerResume))
	require.Equal(t, 0, fire(t, stopEvent("s1", false)).code)
}

// TestStopHookActiveAlwaysAllows: the loop guard wins over a pending resume
// flag, so a session can never be wedged shut.
func TestStopHookActiveAlwaysAllows(t *testing.T) {
	withTemp(t)
	setMarker("s1", markerResume)
	setMarker("s1", markerActive)
	stop := fire(t, stopEvent("s1", true))
	require.Equal(t, 0, stop.code)
	require.False(t, markerExists("s1", markerResume))
	require.False(t, markerExists("s1", markerActive))
}

// TestSessionIsolation confirms one session's block never affects another.
func TestSessionIsolation(t *testing.T) {
	withTemp(t)
	fire(t, ups("a", "huh?"))
	res := fire(t, pre("b", "Bash"))
	require.Equal(t, "{}", res.stdout)
}

// TestResumeIsSessionScoped: an interjection in one session must not refuse
// another session's stop.
func TestResumeIsSessionScoped(t *testing.T) {
	withTemp(t)
	fire(t, ups("a", "do the thing"))
	fire(t, ups("a", "wait, what?")) // a is interrupted
	fire(t, ups("b", "unrelated question?"))
	require.Equal(t, 0, fire(t, stopEvent("b", false)).code)
	require.Equal(t, 2, fire(t, stopEvent("a", false)).code)
}

func TestUnknownEventIsNoOp(t *testing.T) {
	withTemp(t)
	res := fire(t, payload(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s1"}))
	require.Equal(t, "{}", res.stdout)
	require.Equal(t, 0, res.code)
}

func TestBadJSONIsNoOp(t *testing.T) {
	withTemp(t)
	res := fire(t, `not json`)
	require.Equal(t, "{}", res.stdout)
	require.Equal(t, 0, res.code)
}

// TestQuestionMarkAnywhere: a "?" anywhere in the message arms the block,
// even when the question is buried in a longer prompt.
func TestQuestionMarkAnywhere(t *testing.T) {
	withTemp(t)
	fire(t, ups("s1", "Fix the bug. Also, what does foo do? Thanks."))
	require.True(t, markerExists("s1", markerPending))
}

func TestIsLookupTool(t *testing.T) {
	for _, tool := range []string{
		"Read", "Grep", "Glob",
		"mcp__plugin_grep_grep__Grep",
		"mcp__plugin_glob_glob__Glob",
		"mcp__any_server__Read",
	} {
		require.True(t, isLookupTool(tool), tool)
	}
	for _, tool := range []string{
		"Bash", "Write", "Edit", "MultiEdit", "Task", "WebFetch", "WebSearch",
		"TodoWrite", "NotebookEdit", "",
		"mcp__github__get_file_contents", // read-only, but not a lookup tool name
		"ReadFile", "Grepper", "myGlob",
	} {
		require.False(t, isLookupTool(tool), tool)
	}
}
