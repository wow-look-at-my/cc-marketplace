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

// stageTranscript writes lines as a JSONL transcript and returns its path.
func stageTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

// bashCall is an assistant record running one Bash command.
func bashCall(command string) string {
	input, _ := json.Marshal(map[string]string{"command": command})
	return assistantCall("Bash", string(input))
}

// assistantCall is an assistant record making one tool call.
func assistantCall(name, input string) string {
	return `{"type":"assistant","timestamp":"2026-09-05T01:00:00Z","message":{"role":"assistant","content":` +
		`[{"type":"tool_use","name":"` + name + `","input":` + input + `}]}}`
}

// jsonString quotes text the way the harness does. Claude Code writes the
// transcript from JavaScript, whose JSON.stringify leaves `<` alone, while
// Go's json.Marshal escapes it to < -- so marshaling here would build a
// fixture no real transcript ever looks like, and a test passing on it would
// prove nothing about the file the hook actually reads.
func jsonString(text string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(text)
	return strings.TrimRight(b.String(), "\n")
}

// toolResult is the record a call's answer arrives in. It never starts a new
// turn, and it is where a verdict about a subject is reported.
func toolResult(text string) string {
	return `{"type":"user","timestamp":"2026-09-05T01:00:01Z","message":{"role":"user","content":` +
		`[{"type":"tool_result","content":` + jsonString(text) + `}]}}`
}

// userPrompt is a genuine new prompt, which re-opens every subject.
func userPrompt(text string) string {
	body, _ := json.Marshal(text)
	return `{"type":"user","timestamp":"2026-09-05T01:00:02Z","message":{"role":"user","content":` + string(body) + `}}`
}

// preToolPayload is the payload the harness sends before a call runs.
func preToolPayload(t *testing.T, transcript, tool, input string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"transcript_path": transcript,
		"tool_name":       tool,
		"tool_input":      json.RawMessage(input),
	})
	require.NoError(t, err)
	return string(b)
}

// bashInput is the tool_input a Bash call carries.
func bashInput(command string) string {
	b, _ := json.Marshal(map[string]string{"command": command})
	return string(b)
}

// denyReasonOf runs the hook and returns the refusal reason, or "" on allow.
func denyReasonOf(t *testing.T, payload string) string {
	t.Helper()
	res := run(strings.NewReader(payload))
	require.Equal(t, 0, res.code, "PreToolUse carries its verdict in stdout, never in an exit code")
	if res.stdout == "" {
		return ""
	}
	var out preToolOutput
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &out))
	require.Equal(t, "deny", out.HookSpecificOutput.PermissionDecision)
	require.Equal(t, "PreToolUse", out.HookSpecificOutput.HookEventName)
	return out.HookSpecificOutput.PermissionDecisionReason
}

func TestAMergedPullRequestIsNeverReadAgain(t *testing.T) {
	tr := stageTranscript(t,
		bashCall("gh wait-ci checks --repo wow-look-at-my/grok-build"),
		toolResult(`{"outcome":"merged","pr":"wow-look-at-my/grok-build#87"}`),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh pr view 87 --repo wow-look-at-my/grok-build")))

	require.NotEmpty(t, reason, "a merged pull request cannot answer differently")
	assert.Contains(t, reason, "wow-look-at-my/grok-build#87",
		"the refusal must name the subject it settled, not just say no")
	assert.Contains(t, reason, "cannot leave")
}

func TestADifferentPullRequestIsStillReadable(t *testing.T) {
	tr := stageTranscript(t,
		toolResult(`{"outcome":"merged","pr":"wow-look-at-my/grok-build#87"}`),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh pr view 88 --repo wow-look-at-my/grok-build")))

	assert.Empty(t, reason, "one pull request merging says nothing about another")
}

func TestAGreenCommitIsNeverReadAgain(t *testing.T) {
	tr := stageTranscript(t,
		bashCall("gh wait-ci --sha c274ad3c1a9c7bc156d706dc6062b2ab298417c0"),
		toolResult("Progress: 8/8\nCI PASSED\nCommit: c274ad3c1a9c7bc156d706dc6062b2ab298417c0"),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh wait-ci checks --sha c274ad3c1a9c7bc156d706dc6062b2ab298417c0")))

	require.NotEmpty(t, reason, "a commit that went green cannot go red")
	assert.Contains(t, reason, "c274ad3")
}

func TestAnotherCommitIsStillReadableAfterOneGoesGreen(t *testing.T) {
	tr := stageTranscript(t,
		toolResult("CI PASSED for c274ad3c1a9c7bc156d706dc6062b2ab298417c0"),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh wait-ci --sha d15b136aa1b2c3d4e5f60718293a4b5c6d7e8f90")))

	assert.Empty(t, reason, "a push makes a new commit, which is a new question")
}

func TestRereadingTheSameSubjectWithNothingInBetweenIsRefused(t *testing.T) {
	tr := stageTranscript(t,
		bashCall("gh pr view 87 --repo wow-look-at-my/grok-build"),
		toolResult(`{"pr":"wow-look-at-my/grok-build#87","state":"open"}`),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh pr checks 87 --repo wow-look-at-my/grok-build")))

	require.NotEmpty(t, reason, "nothing happened between the two reads")
	assert.Contains(t, reason, "already read the state of")
}

func TestRespellingTheQuestionWithAnotherToolIsStillARepeat(t *testing.T) {
	tr := stageTranscript(t,
		bashCall("gh pr checks 87 --repo wow-look-at-my/grok-build"),
		toolResult("still running"),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "mcp__github__pull_request_read",
		`{"owner":"wow-look-at-my","repo":"grok-build","pullNumber":87}`))

	require.NotEmpty(t, reason,
		"the subject is the same pull request, so a different tool is the same call")
	assert.Contains(t, reason, "same pull request or commit")
}

func TestAWakeEnvelopeIsRecognisedInAToolResult(t *testing.T) {
	tr := stageTranscript(t,
		toolResult(`<wake reason="external-event"><event source="github"/></wake>`),
	)
	recs := parseRecords(tr)
	require.Len(t, recs, 1)
	assert.True(t, recs[0].wake, "the envelope arrives escaped inside the result's content")

	// The same record written by an encoder that escapes HTML must read the
	// same, or the guard is blind on half the transcripts it may be handed.
	escaped, err := json.Marshal(`<wake reason="external-event"><event source="github"/></wake>`)
	require.NoError(t, err)
	require.Contains(t, string(escaped), "\\u003c", "this fixture must be the escaped spelling")
	recs = parseRecords(stageTranscript(t,
		`{"type":"user","message":{"role":"user","content":`+
			`[{"type":"tool_result","content":`+string(escaped)+`}]}}`))
	require.Len(t, recs, 1)
	assert.True(t, recs[0].wake, "the escaped spelling of the envelope counts too")
}

func TestAWakeEventReopensTheSubject(t *testing.T) {
	tr := stageTranscript(t,
		bashCall("gh pr view 87 --repo wow-look-at-my/grok-build"),
		toolResult(`{"pr":"wow-look-at-my/grok-build#87","state":"open"}`),
		toolResult(`<wake reason="external-event"><event source="github" kind="check_suite.completed"/></wake>`),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh pr checks 87 --repo wow-look-at-my/grok-build")))

	assert.Empty(t, reason, "an event arriving is exactly the signal worth re-reading on")
}

func TestAUserPromptReopensTheSubject(t *testing.T) {
	tr := stageTranscript(t,
		bashCall("gh pr view 87 --repo wow-look-at-my/grok-build"),
		toolResult(`{"pr":"wow-look-at-my/grok-build#87","state":"open"}`),
		userPrompt("is it green yet?"),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh pr checks 87 --repo wow-look-at-my/grok-build")))

	assert.Empty(t, reason, "the user asking is always a reason to look")
}

func TestAPushReopensTheSubject(t *testing.T) {
	tr := stageTranscript(t,
		bashCall("gh pr view 87 --repo wow-look-at-my/grok-build"),
		toolResult(`{"pr":"wow-look-at-my/grok-build#87","state":"open"}`),
		bashCall("git push -u origin claude/fix"),
		toolResult("pushed"),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh pr checks 87 --repo wow-look-at-my/grok-build")))

	assert.Empty(t, reason, "CI on a new head is a new question, not a repeat")
}

func TestAListingNamingSeveralPullRequestsSettlesNone(t *testing.T) {
	tr := stageTranscript(t,
		toolResult(`[{"pr":"wow-look-at-my/grok-build#87","state":"merged"},`+
			`{"pr":"wow-look-at-my/grok-build#88","state":"open"}]`),
	)
	reason := denyReasonOf(t, preToolPayload(t, tr, "Bash",
		bashInput("gh pr view 88 --repo wow-look-at-my/grok-build")))

	assert.Empty(t, reason,
		"a verdict that cannot be attributed to one pull request must settle neither")
}

func TestACallThatIsNotAStatusReadIsNeverRefused(t *testing.T) {
	tr := stageTranscript(t,
		toolResult(`{"outcome":"merged","pr":"wow-look-at-my/grok-build#87"}`),
	)
	for _, c := range []struct{ tool, input string }{
		{"Read", `{"file_path":"/repo/wow-look-at-my/grok-build#87.md"}`},
		{"Bash", bashInput("git log --oneline wow-look-at-my/grok-build#87")},
		{"Bash", bashInput("grep -r 'gh pr view' docs/")},
	} {
		assert.Empty(t, denyReasonOf(t, preToolPayload(t, tr, c.tool, c.input)),
			"%s must run: it is not asking after a status", c.tool)
	}
}

func TestACommandThatOnlyMentionsAStatusReadIsNotOne(t *testing.T) {
	assert.False(t, namesAStatusCommand("grep -rn 'gh wait-ci' claude_snippets/"))
	assert.False(t, namesAStatusCommand("git commit -m 'document gh pr view usage'"))
	assert.True(t, namesAStatusCommand("gh wait-ci --sha abc1234"))
	assert.True(t, namesAStatusCommand("cd /repo && gh pr checks 12"))
	assert.True(t, namesAStatusCommand("echo hi; gh run list"))
}

func TestSubjectsAreSpelledTheSameAcrossTools(t *testing.T) {
	fromBash := subjectsIn(strings.ToLower(`gh pr view 87 --repo wow-look-at-my/grok-build`))
	fromMCP := subjectsIn(strings.ToLower(`{"owner":"wow-look-at-my","repo":"grok-build","pullNumber":87}`))
	fromSlug := subjectsIn(strings.ToLower(`wow-look-at-my/grok-build#87`))

	assert.Contains(t, fromBash, "pr:wow-look-at-my/grok-build#87")
	assert.Contains(t, fromMCP, "pr:wow-look-at-my/grok-build#87")
	assert.Contains(t, fromSlug, "pr:wow-look-at-my/grok-build#87")
}

func TestAHexWordIsNotACommitSubject(t *testing.T) {
	assert.NotContains(t, subjectsIn("the defaced banner"), "sha:defaced")
	assert.Contains(t, subjectsIn("commit c274ad3"), "sha:c274ad3")
}

func TestEveryFailurePathAllowsTheCall(t *testing.T) {
	tr := stageTranscript(t, toolResult(`{"outcome":"merged","pr":"wow-look-at-my/grok-build#87"}`))

	t.Run("unparseable payload", func(t *testing.T) {
		assert.Equal(t, allow(), run(strings.NewReader("{not json")))
	})
	t.Run("another event", func(t *testing.T) {
		assert.Equal(t, allow(), run(strings.NewReader(
			`{"hook_event_name":"PostToolUse","tool_name":"Bash"}`)))
	})
	t.Run("no tool name", func(t *testing.T) {
		assert.Equal(t, allow(), run(strings.NewReader(
			`{"hook_event_name":"PreToolUse","transcript_path":"`+tr+`"}`)))
	})
	t.Run("missing transcript", func(t *testing.T) {
		assert.Empty(t, denyReasonOf(t, preToolPayload(t, "/nonexistent/transcript.jsonl",
			"Bash", bashInput("gh pr view 87 --repo wow-look-at-my/grok-build"))))
	})
	t.Run("a status read naming no subject", func(t *testing.T) {
		assert.Empty(t, denyReasonOf(t, preToolPayload(t, tr, "Bash", bashInput("gh run list"))))
	})
}

func TestTheStopHalfStillWorks(t *testing.T) {
	assert.Equal(t, allow(), run(strings.NewReader(
		`{"hook_event_name":"Stop","transcript_path":"/nonexistent"}`)),
		"adding a second event must not change the Stop half's fail-open path")
}
