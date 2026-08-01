package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The debt marker lives under os.TempDir(); t.Setenv("TMPDIR") gives each test
// its own so they neither collide nor leak into a real session's state.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
}

func arm(prompt, session string) string {
	return promptArm(hookPayload{SessionID: session, Prompt: prompt, EventName: "UserPromptSubmit"})
}

func gate(tool, session string) string {
	return todoGate(hookPayload{SessionID: session, ToolName: tool, EventName: "PreToolUse"})
}

// decision extracts permissionDecision from a gate response, "" when the gate
// stayed out of the way.
func decision(t *testing.T, out string) string {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return ""
	}
	var parsed struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	require.NoErrorf(t, json.Unmarshal([]byte(out), &parsed), "gate emitted invalid JSON: %q", out)
	return parsed.HookSpecificOutput.PermissionDecision
}

func TestAssignmentArmsTheGate(t *testing.T) {
	isolate(t)
	arm("add deny support to the enhanced-auto-allow plugin", "s")
	require.Equal(t, "deny", decision(t, gate("Bash", "s")), "an assignment must arm the gate")
}

func TestQuestionsDoNotArm(t *testing.T) {
	for _, q := range []string{
		"what does this plugin do?",
		"why is the hook failing",
		"how do I run the tests?",
		"is the build green?",
		"can you see the logs?",
		"should we care about this?",
	} {
		t.Run(q, func(t *testing.T) {
			isolate(t)
			arm(q, "s")
			require.Emptyf(t, decision(t, gate("Bash", "s")), "a question must not arm the gate: %q", q)
		})
	}
}

// The counterpart, and the one that matters: an auxiliary opener with no "?"
// is an instruction, and reading it as a question is how an assignment goes
// unfiled.
func TestInstructionsArm(t *testing.T) {
	for _, p := range []string{
		"do the thing",
		"can you add a test for this",
		"should we block python",
		"fix the failing check",
		"make sure you push it",
		"now update the docs",
	} {
		t.Run(p, func(t *testing.T) {
			isolate(t)
			arm(p, "s")
			require.Equalf(t, "deny", decision(t, gate("Bash", "s")), "an instruction must arm the gate: %q", p)
		})
	}
}

func TestQuestionWithImperativeArms(t *testing.T) {
	isolate(t)
	arm("why is the build red? fix it", "s")
	require.Equal(t, "deny", decision(t, gate("Bash", "s")), "a question carrying an imperative must arm")
}

// Bare commands and settings-shaped arguments stay out. `/goal do the thing`
// sits on the settings side of that line rather than the assignment side: four
// words with no imperative reads like a parameter, and the argument rule errs
// toward silence for short command input.
func TestAcksAndSettingsCommandsDoNotArm(t *testing.T) {
	for _, p := range []string{"ok", "thanks!", "lgtm", "go ahead", "  ", "/compact", "/goal do the thing", "/effort high"} {
		t.Run(p, func(t *testing.T) {
			isolate(t)
			arm(p, "s")
			require.Emptyf(t, decision(t, gate("Bash", "s")), "an ack/command must not arm the gate: %q", p)
		})
	}
}

// ...but a command carrying real work DOES arm now. It used to be skipped on
// the leading "/" alone, and since the hook only ever sees the raw `/name args`
// and never the expansion, that skipped every assignment handed over as
// `/goal <work>` -- a whole session of them, unfiled and forgotten.
func TestSlashCommandCarryingWorkArms(t *testing.T) {
	for _, p := range []string{
		"/goal fix the flaky test",
		"/goal add the missing plugins and fix the hook",
		"/goal next up, figure out what is wrong with the todo plugin",
	} {
		t.Run(p, func(t *testing.T) {
			isolate(t)
			arm(p, "s")
			require.Equalf(t, "deny", decision(t, gate("Bash", "s")), "a command carrying work must arm the gate: %q", p)
		})
	}
}

func TestEveryNonTaskToolIsRefused(t *testing.T) {
	for _, tool := range []string{"Bash", "Read", "Edit", "Write", "Grep", "WebFetch", "Task"} {
		t.Run(tool, func(t *testing.T) {
			isolate(t)
			arm("port the hooks to go", "s")
			require.Equalf(t, "deny", decision(t, gate(tool, "s")), "%s must be refused while a task is owed", tool)
		})
	}
}

// Reading the list is allowed so the settling call can check for duplicates --
// but reading is not filing, and must not settle the debt.
func TestListingDoesNotSettle(t *testing.T) {
	isolate(t)
	arm("port the hooks to go", "s")

	require.Empty(t, decision(t, gate("TaskList", "s")), "TaskList must not be refused")
	require.Empty(t, decision(t, gate("TaskGet", "s")), "TaskGet must not be refused")
	require.Equal(t, "deny", decision(t, gate("Bash", "s")), "reading the list must not settle the debt")
}

func TestSettlingTools(t *testing.T) {
	for _, tool := range []string{"TaskCreate", "TaskUpdate"} {
		t.Run(tool, func(t *testing.T) {
			isolate(t)
			arm("port the hooks to go", "s")

			require.Emptyf(t, decision(t, gate(tool, "s")), "%s must not be refused", tool)
			require.Emptyf(t, decision(t, gate("Bash", "s")), "%s must settle the debt", tool)
		})
	}
}

func TestRefusalQuotesTheAssignmentAndTheWayOut(t *testing.T) {
	isolate(t)
	arm("add deny support to the enhanced-auto-allow plugin", "s")
	out := gate("Bash", "s")

	require.Contains(t, out, "enhanced-auto-allow", "the refusal must quote the assignment")
	require.Contains(t, out, "TaskCreate", "the refusal must name the way out")
}

// Repeated refusals escalate, so an ignored block does not read the same as a
// first one.
func TestRefusalsAreCounted(t *testing.T) {
	isolate(t)
	arm("port the hooks to go", "s")

	require.NotContains(t, gate("Bash", "s"), "This is refusal", "the first refusal must not claim to be a repeat")
	require.Contains(t, gate("Bash", "s"), "This is refusal 2", "the second refusal must say so")
}

func TestFirstAssignmentSurvivesAFollowUp(t *testing.T) {
	isolate(t)
	arm("add deny support to the plugin", "s")
	arm("also wrap it in a bow", "s")

	require.Contains(t, gate("Bash", "s"), "deny support", "the first unfiled assignment must survive a follow-up")
}

func TestDebtIsPerSession(t *testing.T) {
	isolate(t)
	arm("port the hooks to go", "session-a")

	require.Empty(t, decision(t, gate("Bash", "session-b")), "another session must not inherit the debt")
	require.Equal(t, "deny", decision(t, gate("Bash", "session-a")), "the arming session must still be gated")
}

// A broken guard must never wedge a session: no session id, garbage stdin and
// an unknown event all pass through silently.
func TestFailsOpen(t *testing.T) {
	isolate(t)

	require.Empty(t, arm("do the thing", ""), "no session id must not arm")
	require.Empty(t, gate("Bash", ""), "no session id must not gate")

	for _, raw := range []string{"", "not json", "[]", "null"} {
		p := parsePayload([]byte(raw))
		require.Emptyf(t, p.SessionID, "garbage stdin must yield no session: %q", raw)
		require.Emptyf(t, promptArm(p)+todoGate(p), "garbage stdin must produce no decision: %q", raw)
	}
}

// The dispatch itself: one binary, three events, and an unrecognized event must
// not block.
func TestRunDispatchesByEvent(t *testing.T) {
	isolate(t)

	code, stderr, stdout := run(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s","prompt":"fix the build"}`))
	require.Equal(t, 0, code)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "additionalContext")

	code, stderr, stdout = run(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s","tool_name":"Bash"}`))
	require.Equal(t, 0, code)
	require.Empty(t, stderr)
	require.Contains(t, stdout, `"deny"`)

	code, _, stdout = run(strings.NewReader(`{"hook_event_name":"SomethingElse","session_id":"s"}`))
	require.Equal(t, 0, code, "an unknown event must not block")
	require.Empty(t, stdout)
}
