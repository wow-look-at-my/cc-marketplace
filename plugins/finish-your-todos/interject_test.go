package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeUserTranscript builds a JSONL transcript of queued_command attachments,
// which is the shape a mid-turn message really lands in -- verified against a
// live transcript, where the text sits in the attachment's `prompt` field and
// `commandMode` separates a typed message from a harness-injected one.
func writeUserTranscript(t *testing.T, msgs []userText) string {
	t.Helper()
	return writeAttachments(t, msgs, "prompt")
}

func writeAttachments(t *testing.T, msgs []userText, mode string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, m := range msgs {
		require.NoError(t, enc.Encode(map[string]any{
			"type": "attachment",
			"uuid": m.UUID,
			"attachment": map[string]any{
				"type":        "queued_command",
				"commandMode": mode,
				"prompt":      m.Text,
			},
		}))
	}
	return path
}

// wrapped renders the FALLBACK shape: a user message carrying the CLI's
// rendered wrapper. Kept covered because the structured attachment is the
// primary path and this plugin has already been bitten once by scanning for a
// single shape that later moved.
func wrapped(body string) string {
	return "The user sent a new message while you were working:\n" + body +
		"\n\nThis is how Claude Code surfaces messages the user sends mid-turn."
}

func writeRenderedTranscript(t *testing.T, msgs []userText) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rendered.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, m := range msgs {
		require.NoError(t, enc.Encode(map[string]any{
			"type":    "user",
			"uuid":    m.UUID,
			"message": map[string]any{"role": "user", "content": m.Text},
		}))
	}
	return path
}

// The failure this closes: a mid-turn message fires no UserPromptSubmit at all,
// so the entry gate never armed and the assignment was silently dropped. On a
// web surface every message queues while the session is busy, so this was most
// of them.
func TestMidTurnInterjectionArmsTheGate(t *testing.T) {
	session := "sess-interject-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	path := writeUserTranscript(t, []userText{
		{UUID: "u1", Text: "thanks"},
		{UUID: "u2", Text: "also auto-allow the add repo tool"},
	})

	require.Nil(t, readDebt(session), "precondition: no debt yet")
	out := todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path})

	require.NotEmpty(t, out, "the gate must refuse the tool call after a mid-turn assignment")
	var resp struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &resp))
	assert.Equal(t, "deny", resp.HookSpecificOutput.PermissionDecision)
	assert.Contains(t, resp.HookSpecificOutput.PermissionDecisionReason, "auto-allow the add repo tool",
		"the refusal quotes the user's own words, not the wrapper")
}

// The high-water mark must advance, or one interjection re-arms on every tool
// call for the rest of the session and the gate becomes unusable.
func TestInterjectionArmsAtMostOnce(t *testing.T) {
	session := "sess-once-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	path := writeUserTranscript(t, []userText{{UUID: "u1", Text: "fix the flaky test"}})

	require.NotEmpty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}))
	// Settle it the way the model would.
	todoGate(hookPayload{SessionID: session, ToolName: "TaskCreate", TranscriptPath: path})
	require.Nil(t, readDebt(session), "TaskCreate settles the debt")

	assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}),
		"an interjection already accounted for must not arm again")
}

// A message that is merely context, not an assignment, must not arm.
func TestMidTurnNonAssignmentDoesNotArm(t *testing.T) {
	session := "sess-ctx-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	path := writeUserTranscript(t, []userText{{UUID: "u1", Text: "thanks"}})
	assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}))
	assert.Nil(t, readDebt(session))
}

// An ordinary submitted user message is not an interjection: promptArm already
// covers it, and arming here too would double-count every normal turn.
func TestPlainUserMessageIsNotAnInterjection(t *testing.T) {
	session := "sess-plain-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	path := writeRenderedTranscript(t, []userText{{UUID: "u1", Text: "fix the flaky test"}})
	assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}))
	assert.Nil(t, readDebt(session))
}

// The rendered wrapper is the fallback path, kept alive in case the structured
// attachment shape moves under us again.
func TestRenderedWrapperStillArms(t *testing.T) {
	session := "sess-rendered-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	path := writeRenderedTranscript(t, []userText{{UUID: "u1", Text: wrapped("fix the flaky test")}})
	assert.NotEmpty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}))
	debt := readDebt(session)
	require.NotNil(t, debt)
	assert.NotContains(t, debt.Prompt, "This is how Claude Code surfaces",
		"the CLI's trailing explanation is not part of what the user asked for")
}

// Webhooks, background-task completions and reminders ride the SAME queue as a
// typed interjection. Arming on those would refuse every tool call over a PR
// notification nobody asked for -- the fastest way to get the whole plugin
// switched off.
func TestSystemEnvelopesDoNotArm(t *testing.T) {
	for _, envelope := range []string{
		"<github-webhook-activity>\nThe PR has been merged. Update the docs and push.",
		"<task-notification>\n<task-id>abc</task-id> agent finished; now fix the thing",
		"<system-reminder>\nremember to add tests and commit them",
	} {
		t.Run(envelope[:20], func(t *testing.T) {
			session := "sess-env-" + envelope[:20]
			t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

			path := writeUserTranscript(t, []userText{{UUID: "u1", Text: envelope}})
			assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}),
				"a harness-injected message must never arm the gate")
			assert.Nil(t, readDebt(session))
		})
	}
}

// commandMode marks what the queue entry actually was. Only a typed prompt is
// a candidate assignment.
func TestNonPromptCommandModeDoesNotArm(t *testing.T) {
	session := "sess-mode-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	path := writeAttachments(t, []userText{{UUID: "u1", Text: "fix the flaky test"}}, "task-notification")
	assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}))
	assert.Nil(t, readDebt(session))
}

// No transcript, unreadable transcript, garbage lines: the guard fails open
// rather than blocking every tool call in the session.
func TestInterjectionFailsOpen(t *testing.T) {
	session := "sess-open-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: ""}))
	assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: "/nonexistent/x.jsonl"}))

	junk := filepath.Join(t.TempDir(), "junk.jsonl")
	require.NoError(t, os.WriteFile(junk, []byte("not json\n{\"type\":\"user\"}\n"), 0o600))
	assert.Empty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: junk}))
}

// The block-array content form must work too -- the same message can arrive
// either way depending on surface.
func TestInterjectionInBlockArrayContent(t *testing.T) {
	session := "sess-blocks-" + t.Name()
	t.Cleanup(func() { clearDebt(session); os.Remove(seenPath(session)) })

	path := filepath.Join(t.TempDir(), "t.jsonl")
	rec, err := json.Marshal(map[string]any{
		"type": "user",
		"uuid": "u1",
		"message": map[string]any{
			"role":    "user",
			"content": []any{map[string]any{"type": "text", "text": wrapped("add a regression test")}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(rec, '\n'), 0o600))

	assert.NotEmpty(t, todoGate(hookPayload{SessionID: session, ToolName: "Bash", TranscriptPath: path}),
		"an interjection carried as content blocks must arm just like the string form")
}

// Slash commands: the hook sees the RAW `/name args`, never the expansion, so
// the arguments are where the assignment lives. `/goal <work>` reaches the hook
// this way; skipping on the leading "/" is what dropped it.
func TestSlashCommandArgumentsAssignWork(t *testing.T) {
	for _, tt := range []struct {
		prompt string
		want   bool
		why    string
	}{
		{"/goal fix the flaky test and push it", true, "the arguments carry a plain instruction"},
		{"/goal add the missing plugins", true, "same, different verb"},
		{"/goal next up, figure out what is wrong with the todo plugin", true,
			"no verb from the imperative list, but it reads as a sentence rather than a setting"},
		{"/review", false, "a bare command assigns nothing"},
		{"/effort high", false, "a setting, not work -- the whole reason command args need a stricter rule than prose"},
		{"/clear", false, "bare control command"},
		{"/loop 5m /babysit-prs", false, "parameters, not an assignment"},
		{"/goal why is CI red?", false, "a question stays a question through the strip"},
		{"/goal why is CI red? fix it", true, "an imperative riding a question still arms"},
	} {
		assert.Equal(t, tt.want, assignsWork(tt.prompt), "%q: %s", tt.prompt, tt.why)
	}
}

func TestStripSlashCommand(t *testing.T) {
	assert.Equal(t, "fix the tests", stripSlashCommand("/goal fix the tests"))
	assert.Equal(t, "", stripSlashCommand("/review"))
	assert.Equal(t, "fix the tests", stripSlashCommand("fix the tests"), "non-slash input is untouched")
	assert.Equal(t, "b", stripSlashCommand("  /a   b"))
}
