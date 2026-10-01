package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Record shapes copied from a real Claude Code transcript: each content
// block is its own record, and a user prompt carries string content while a
// tool result carries an array of tool_result blocks.
const (
	recPrompt     = `{"type":"user","message":{"role":"user","content":"is this all committed?"}}`
	recPromptArr  = `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"check please?"}]}}`
	recThinking   = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"}]}}`
	recText       = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Checking now."}]}}`
	recBlankText  = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"   "}]}}`
	recToolUse    = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`
	recToolResult = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`
	recAttachment = `{"type":"attachment","attachment":{"type":"file"}}`
)

func writeTranscript(t *testing.T, records ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	// strings.Builder, not += : the huge-transcript case appends thousands of
	// records and quadratic string copying makes the test time out.
	var body strings.Builder
	for _, r := range records {
		body.WriteString(r)
		body.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(path, []byte(body.String()), 0o644))
	return path
}

func TestHasRepliedSince(t *testing.T) {
	cases := []struct {
		name    string
		records []string
		want    bool
	}{
		{
			name:    "prompt only: nothing said yet",
			records: []string{recPrompt},
			want:    false,
		},
		{
			name:    "thinking is not a reply",
			records: []string{recPrompt, recThinking},
			want:    false,
		},
		{
			name:    "tool call without text is not a reply",
			records: []string{recPrompt, recThinking, recToolUse},
			want:    false,
		},
		{
			name:    "text after the prompt is a reply",
			records: []string{recPrompt, recThinking, recText},
			want:    true,
		},
		{
			name:    "whitespace-only text does not count",
			records: []string{recPrompt, recBlankText},
			want:    false,
		},
		{
			name:    "array-content prompt is still a prompt boundary",
			records: []string{recText, recPromptArr},
			want:    false,
		},
		{
			name:    "a reply mid-turn survives later tool traffic",
			records: []string{recPrompt, recText, recToolUse, recToolResult, recToolUse, recToolResult},
			want:    true,
		},
		{
			name: "the PREVIOUS turn's reply does not count for this turn",
			records: []string{
				recPrompt, recText, recToolUse, recToolResult, // turn 1, answered
				recPrompt, recThinking, // turn 2, nothing said yet
			},
			want: false,
		},
		{
			name:    "attachments are skipped",
			records: []string{recPrompt, recAttachment, recThinking},
			want:    false,
		},
		{
			name:    "unparseable lines are skipped, not fatal",
			records: []string{recPrompt, `{truncated`, recText},
			want:    true,
		},
		{
			name:    "no prompt at all and no text",
			records: []string{recToolUse, recToolResult},
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, hasRepliedSince(writeTranscript(t, tc.records...)))
		})
	}
}

// A missing or empty path keeps the block armed (the safe direction: it
// degrades to the old turn-scoped behavior rather than disabling the guard).
func TestHasRepliedSinceUnreadable(t *testing.T) {
	require.False(t, hasRepliedSince(""))
	require.False(t, hasRepliedSince(filepath.Join(t.TempDir(), "nope.jsonl")))
	require.False(t, hasRepliedSince(writeTranscript(t)))
}

// A transcript larger than the tail window must still resolve the current
// turn, which always lives at the end of the file.
func TestHasRepliedSinceHugeTranscript(t *testing.T) {
	// One filler record padded past the window, then a real turn.
	filler := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` +
		strings.Repeat("x", 1024) + `"}]}}`
	records := []string{}
	for len(records)*1030 < transcriptTailBytes+(1<<20) {
		records = append(records, filler)
	}
	// The tail holds: fresh prompt, thinking, no reply yet.
	unanswered := append(append([]string{}, records...), recPrompt, recThinking)
	require.False(t, hasRepliedSince(writeTranscript(t, unanswered...)),
		"the current turn is unanswered even though older turns had text")

	answered := append(append([]string{}, records...), recPrompt, recText)
	require.True(t, hasRepliedSince(writeTranscript(t, answered...)))
}

// The integration the bug report demanded: a question needing a command is
// answerable in ONE turn -- deny before any text, allow right after it.
func TestPreToolUseUnblocksAfterReplyInSameTurn(t *testing.T) {
	withTemp(t)
	unanswered := writeTranscript(t, recPrompt, recThinking)
	answered := writeTranscript(t, recPrompt, recThinking, recText)

	fire(t, ups("s1", "is this all committed?"))

	// Acting before saying anything: denied.
	denied := fire(t, preWithTranscript("s1", "Bash", unanswered))
	require.Contains(t, denied.stdout, `"permissionDecision":"deny"`)
	require.Contains(t, denied.stdout, "THIS turn",
		"the reason must point at replying in-turn, not at ending the turn")

	// Same turn, after the reply text exists: allowed.
	allowed := fire(t, preWithTranscript("s1", "Bash", answered))
	require.Equal(t, "{}", allowed.stdout)

	// And the block is cleared, so the rest of the turn is unimpeded.
	require.False(t, markerExists("s1", markerPending))
	require.Equal(t, "{}", fire(t, preWithTranscript("s1", "Write", unanswered)).stdout,
		"once lifted the block stays lifted for the turn")
}

// Lookups do not depend on the transcript at all.
func TestLookupsAllowedWithNoTranscript(t *testing.T) {
	withTemp(t)
	setMarker("s1", markerPending)
	require.Equal(t, "{}", fire(t, preWithTranscript("s1", "Read", "")).stdout)
	require.Contains(t, fire(t, preWithTranscript("s1", "Bash", "")).stdout, "deny")
}
