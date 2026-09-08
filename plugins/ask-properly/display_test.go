package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTranscript builds a JSONL transcript from raw record lines.
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

// record builds one JSONL transcript line for a message with the given role
// and content blocks.
func record(role string, blocks ...map[string]any) string {
	line, _ := json.Marshal(map[string]any{
		"type":    role,
		"message": map[string]any{"role": role, "content": blocks},
	})
	return string(line)
}

func userPrompt(text string) string {
	return record("user", map[string]any{"type": "text", "text": text})
}

func assistantText(text string) string {
	return record("assistant", map[string]any{"type": "text", "text": text})
}

func assistantAsk() string {
	return record("assistant", map[string]any{"type": "tool_use", "name": askTool, "input": map[string]any{}})
}

// flush drives one MessageDisplay flush.
func flush(t *testing.T, in HookInput) string {
	t.Helper()
	in.HookEventName = "MessageDisplay"
	data, err := json.Marshal(in)
	require.NoError(t, err)
	return run(strings.NewReader(string(data)))
}

// displayed is the text the CLI shows for a flush: the hook's replacement when
// it emitted one, and the original delta otherwise.
func displayed(t *testing.T, out, delta string) string {
	t.Helper()
	if out == "" {
		return delta
	}
	var got Output
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "MessageDisplay", got.HookSpecificOutput.HookEventName)
	return got.HookSpecificOutput.DisplayContent
}

func TestAProseQuestionIsAnnotated(t *testing.T) {
	delta := "Four are still open. Which one should win?"
	out := flush(t, HookInput{MessageID: "m1", Index: 0, Final: true, Delta: delta})
	got := displayed(t, out, delta)

	assert.True(t, strings.HasPrefix(got, delta), "the message itself is displayed unchanged: %q", got)
	assert.Contains(t, got, "ask-properly")
	assert.Contains(t, got, `"Which one should win?"`)
	// The repair is to DECIDE. Pointing at a card is a slower stall: the work
	// stops either way, and the owner answers a question he did not want.
	assert.Contains(t, got, "Make it yourself")
	assert.NotContains(t, got, "ask it with AskUserQuestion")
}

func TestADeferralIsAnnotated(t *testing.T) {
	delta := "Four questions remain. Your call."
	got := displayed(t, flush(t, HookInput{MessageID: "m1", Final: true, Delta: delta}), delta)
	assert.Contains(t, got, `"your call"`)
}

// The escape hatch survives the move: a turn that used the tool asked properly,
// so prose alongside the rendered card is commentary, not an offloaded decision.
func TestAMessageBesideAnAskUserQuestionCardIsNotAnnotated(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("resolve the open questions"),
		assistantAsk(),
	)
	delta := "Putting the four decisions to you. Which do you prefer?"
	assert.Empty(t, flush(t, HookInput{TranscriptPath: path, MessageID: "m1", Final: true, Delta: delta}))
}

// An AskUserQuestion call in an EARLIER turn does not license a prose question
// now.
func TestAnEarlierTurnsAskDoesNotCount(t *testing.T) {
	path := writeTranscript(t,
		userPrompt("first ask"),
		assistantAsk(),
		assistantText("Thanks."),
		userPrompt("now finish it"),
	)
	delta := "Done. Which rule should win?"
	got := displayed(t, flush(t, HookInput{TranscriptPath: path, MessageID: "m1", Final: true, Delta: delta}), delta)
	assert.Contains(t, got, "ask-properly")
}

func TestAPlainReportIsNotAnnotated(t *testing.T) {
	delta := "Landed the rule in docs/json.md and pinned it in tests/json.dats. CI is green."
	assert.Empty(t, flush(t, HookInput{MessageID: "m1", Final: true, Delta: delta}))
}

// A "?" is not enough on its own, and the move to MessageDisplay does not
// loosen that: a nullable type and a compare URL are not questions.
func TestProseThatMustNotBeAnnotated(t *testing.T) {
	for _, delta := range []string{
		"find and index_of now return UInt? rather than Int?.",
		"Pushed. The compare page is https://github.com/o/r/compare/master...claude/x?expand=1 and CI is green.",
	} {
		assert.Empty(t, flush(t, HookInput{MessageID: "m1", Final: true, Delta: delta}), "for %q", delta)
	}
}

// The line sits under the message, so it stays one line however many findings
// there are.
func TestTheAnnotationNamesAtMostThreeFindings(t *testing.T) {
	got := Annotate("Your call. Let me know. Up to you. Shall I? Want me to?")
	require.NotEmpty(t, got)
	assert.Equal(t, 3, strings.Count(got, `"`)/2, "three findings quoted in %q", got)
	assert.Contains(t, got, "and more")
	assert.Equal(t, 1, strings.Count(strings.TrimPrefix(got, "\n\n"), "\n")+1, "one line: %q", got)
}

// A deferral phrase often sits inside a question already quoted, and naming
// both says the same thing twice on the reader's screen.
func TestADeferralInsideAQuotedQuestionIsNotNamedTwice(t *testing.T) {
	got := Annotate("Want me to fix it?")
	require.NotEmpty(t, got)
	assert.Contains(t, got, `"Want me to fix it?"`)
	assert.Equal(t, 1, strings.Count(got, `"`)/2, "one finding quoted in %q", got)
}

// A question hit carries the sentence the detector cut at the "?", so the mark
// goes back on, and a long sentence is trimmed rather than quoted whole.
func TestAQuotedQuestionIsBoundedAndKeepsItsMark(t *testing.T) {
	long := "Should I " + strings.Repeat("really ", 40) + "land it?"
	got := Annotate(long)
	require.NotEmpty(t, got)
	assert.Contains(t, got, `land it?"`)
	assert.Contains(t, got, `"...`)
}

// A question can span a line wrap, and one flush carries only the lines that
// completed since the last one. So the message is accumulated and judged whole
// on its final flush.
func TestAQuestionSplitAcrossFlushesIsStillFound(t *testing.T) {
	assert.Empty(t, flush(t, HookInput{MessageID: "wrap", Index: 0, Delta: "Landed the fix. Which rule\n"}),
		"a non-final flush is never annotated")

	out := flush(t, HookInput{MessageID: "wrap", Index: 1, Final: true, Delta: "should win?"})
	assert.Contains(t, displayed(t, out, ""), "ask-properly")
}

// The final flush's delta is empty when the message ends on a newline. It is
// still the end-of-message signal, and the annotation still lands.
func TestAnEmptyFinalFlushStillCarriesTheAnnotation(t *testing.T) {
	require.Empty(t, flush(t, HookInput{MessageID: "empty-final", Index: 0, Delta: "Your call.\n"}))

	got := displayed(t, flush(t, HookInput{MessageID: "empty-final", Index: 1, Final: true}), "")
	assert.Contains(t, got, `"your call"`)
	assert.NotContains(t, got, "Your call.\n", "the earlier flush is not redisplayed")
}

// The annotation is never repeated: each message is judged once, on its last
// flush, and the accumulated text is dropped there.
func TestAMessageIsAnnotatedOnceAndItsStateIsDropped(t *testing.T) {
	require.NotEmpty(t, flush(t, HookInput{MessageID: "once", Final: true, Delta: "Your call."}))
	assert.Empty(t, priorText("once"), "the accumulated text is dropped on the final flush")
	assert.Empty(t, flush(t, HookInput{MessageID: "once", Final: true, Delta: "All green."}))
}

// Printing nothing leaves the CLI showing the original delta, which is the only
// acceptable failure for a hook in the render path.
func TestEverySurpriseRendersTheOriginal(t *testing.T) {
	cases := map[string]string{
		"not json":        "not json",
		"empty":           "",
		"another event":   `{"hook_event_name":"Stop","final":true,"delta":"Your call."}`,
		"no event name":   `{"final":true,"delta":"Your call."}`,
		"nothing to say":  `{"hook_event_name":"MessageDisplay","final":true,"delta":"the suite is green."}`,
		"empty delta":     `{"hook_event_name":"MessageDisplay","final":true,"delta":""}`,
		"missing tscript": `{"hook_event_name":"MessageDisplay","transcript_path":"/nope/missing.jsonl","final":true,"delta":"the suite is green."}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, run(strings.NewReader(in)))
		})
	}
}

// An unreadable transcript costs the escape hatch and nothing else: the message
// is judged on its own text and still only earns an advisory line.
func TestAnUnreadableTranscriptStillAnnotates(t *testing.T) {
	delta := "Your call."
	got := displayed(t, flush(t, HookInput{TranscriptPath: "/nope/missing.jsonl", Final: true, Delta: delta}), delta)
	assert.Contains(t, got, "ask-properly")
}

// Nothing is sent back to the model and nothing is refused: the process always
// exits 0, and the only output it can produce is a displayContent envelope.
func TestTheOnlyOutputIsADisplayContentEnvelope(t *testing.T) {
	out := flush(t, HookInput{MessageID: "shape", Final: true, Delta: "Your call."})
	require.NotEmpty(t, out)

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	require.Len(t, raw, 1, "no decision, no reason, no systemMessage: %v", raw)

	inner, ok := raw["hookSpecificOutput"].(map[string]any)
	require.True(t, ok)
	keys := make([]string, 0, len(inner))
	for k := range inner {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	assert.Equal(t, []string{"displayContent", "hookEventName"}, keys)
}

func TestTheHookCanBeTurnedOff(t *testing.T) {
	t.Setenv("CC_ASK_PROPERLY", "0")
	assert.Empty(t, flush(t, HookInput{MessageID: "off", Final: true, Delta: "Your call."}))
}
