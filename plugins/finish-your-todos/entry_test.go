package main

import (
	"encoding/json"
	"strings"
	"testing"
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
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("gate emitted invalid JSON: %v (%q)", err, out)
	}
	return parsed.HookSpecificOutput.PermissionDecision
}

func TestAssignmentArmsTheGate(t *testing.T) {
	isolate(t)
	arm("add deny support to the enhanced-auto-allow plugin", "s")
	if got := decision(t, gate("Bash", "s")); got != "deny" {
		t.Fatalf("an assignment must arm the gate, got %q", got)
	}
}

func TestQuestionsDoNotArm(t *testing.T) {
	isolate(t)
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
			if got := decision(t, gate("Bash", "s")); got != "" {
				t.Fatalf("a question must not arm the gate: %q -> %q", q, got)
			}
		})
	}
}

// The counterpart, and the one that matters: an auxiliary opener with no "?"
// is an instruction, and reading it as a question is how an assignment goes
// unfiled.
func TestInstructionsArm(t *testing.T) {
	isolate(t)
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
			if got := decision(t, gate("Bash", "s")); got != "deny" {
				t.Fatalf("an instruction must arm the gate: %q -> %q", p, got)
			}
		})
	}
}

func TestQuestionWithImperativeArms(t *testing.T) {
	isolate(t)
	arm("why is the build red? fix it", "s")
	if got := decision(t, gate("Bash", "s")); got != "deny" {
		t.Fatalf("a question carrying an imperative must arm, got %q", got)
	}
}

func TestAcksAndSlashCommandsDoNotArm(t *testing.T) {
	for _, p := range []string{"ok", "thanks!", "lgtm", "go ahead", "  ", "/compact", "/goal do the thing"} {
		t.Run(p, func(t *testing.T) {
			isolate(t)
			arm(p, "s")
			if got := decision(t, gate("Bash", "s")); got != "" {
				t.Fatalf("an ack/command must not arm the gate: %q -> %q", p, got)
			}
		})
	}
}

func TestEveryNonTaskToolIsRefused(t *testing.T) {
	for _, tool := range []string{"Bash", "Read", "Edit", "Write", "Grep", "WebFetch", "Task"} {
		t.Run(tool, func(t *testing.T) {
			isolate(t)
			arm("port the hooks to go", "s")
			if got := decision(t, gate(tool, "s")); got != "deny" {
				t.Fatalf("%s must be refused while a task is owed, got %q", tool, got)
			}
		})
	}
}

// Reading the list is allowed so the settling call can check for duplicates --
// but reading is not filing, and must not settle the debt.
func TestListingDoesNotSettle(t *testing.T) {
	isolate(t)
	arm("port the hooks to go", "s")

	if got := decision(t, gate("TaskList", "s")); got != "" {
		t.Fatalf("TaskList must not be refused, got %q", got)
	}
	if got := decision(t, gate("TaskGet", "s")); got != "" {
		t.Fatalf("TaskGet must not be refused, got %q", got)
	}
	if got := decision(t, gate("Bash", "s")); got != "deny" {
		t.Fatalf("reading the list must not settle the debt, got %q", got)
	}
}

func TestSettlingTools(t *testing.T) {
	for _, tool := range []string{"TaskCreate", "TaskUpdate"} {
		t.Run(tool, func(t *testing.T) {
			isolate(t)
			arm("port the hooks to go", "s")

			if got := decision(t, gate(tool, "s")); got != "" {
				t.Fatalf("%s must not be refused, got %q", tool, got)
			}
			if got := decision(t, gate("Bash", "s")); got != "" {
				t.Fatalf("%s must settle the debt, got %q", tool, got)
			}
		})
	}
}

func TestRefusalQuotesTheAssignmentAndTheWayOut(t *testing.T) {
	isolate(t)
	arm("add deny support to the enhanced-auto-allow plugin", "s")
	out := gate("Bash", "s")

	if !strings.Contains(out, "enhanced-auto-allow") {
		t.Fatalf("the refusal must quote the assignment: %q", out)
	}
	if !strings.Contains(out, "TaskCreate") {
		t.Fatalf("the refusal must name the way out: %q", out)
	}
}

// Repeated refusals escalate, so an ignored block does not read the same as a
// first one.
func TestRefusalsAreCounted(t *testing.T) {
	isolate(t)
	arm("port the hooks to go", "s")

	if out := gate("Bash", "s"); strings.Contains(out, "This is refusal") {
		t.Fatalf("the first refusal must not claim to be a repeat: %q", out)
	}
	if out := gate("Bash", "s"); !strings.Contains(out, "This is refusal 2") {
		t.Fatalf("the second refusal must say so: %q", out)
	}
}

func TestFirstAssignmentSurvivesAFollowUp(t *testing.T) {
	isolate(t)
	arm("add deny support to the plugin", "s")
	arm("also wrap it in a bow", "s")

	if out := gate("Bash", "s"); !strings.Contains(out, "deny support") {
		t.Fatalf("the first unfiled assignment must survive a follow-up: %q", out)
	}
}

func TestDebtIsPerSession(t *testing.T) {
	isolate(t)
	arm("port the hooks to go", "session-a")

	if got := decision(t, gate("Bash", "session-b")); got != "" {
		t.Fatalf("another session must not inherit the debt, got %q", got)
	}
	if got := decision(t, gate("Bash", "session-a")); got != "deny" {
		t.Fatalf("the arming session must still be gated, got %q", got)
	}
}

// A broken guard must never wedge a session: no session id, garbage stdin and
// an unknown event all pass through silently.
func TestFailsOpen(t *testing.T) {
	isolate(t)

	if out := arm("do the thing", ""); out != "" {
		t.Fatalf("no session id must not arm: %q", out)
	}
	if out := gate("Bash", ""); out != "" {
		t.Fatalf("no session id must not gate: %q", out)
	}

	for _, raw := range []string{"", "not json", "[]", "null"} {
		p := parsePayload([]byte(raw))
		if p.SessionID != "" {
			t.Fatalf("garbage stdin must yield no session: %q -> %q", raw, p.SessionID)
		}
		if out := promptArm(p) + todoGate(p); out != "" {
			t.Fatalf("garbage stdin must produce no decision: %q -> %q", raw, out)
		}
	}
}

// The dispatch itself: one binary, three events, and an unrecognized event must
// not block.
func TestRunDispatchesByEvent(t *testing.T) {
	isolate(t)

	code, stderr, stdout := run(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"s","prompt":"fix the build"}`))
	if code != 0 || stderr != "" || !strings.Contains(stdout, "additionalContext") {
		t.Fatalf("UserPromptSubmit: code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}

	code, stderr, stdout = run(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s","tool_name":"Bash"}`))
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"deny"`) {
		t.Fatalf("PreToolUse: code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}

	code, stderr, stdout = run(strings.NewReader(`{"hook_event_name":"SomethingElse","session_id":"s"}`))
	if code != 0 || stdout != "" {
		t.Fatalf("an unknown event must not block: code=%d stdout=%q", code, stdout)
	}
}
