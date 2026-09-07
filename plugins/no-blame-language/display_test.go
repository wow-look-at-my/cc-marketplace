package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestADeflectingMessageIsAnnotated(t *testing.T) {
	delta := "That bug is pre-existing, so it stays left as-is."
	out := flush(t, HookInput{MessageID: "m1", Index: 0, Final: true, Delta: delta})
	got := displayed(t, out, delta)

	assert.True(t, strings.HasPrefix(got, delta), "the message itself is displayed unchanged: %q", got)
	assert.Contains(t, got, "no-blame-language")
	assert.Contains(t, got, `"pre-existing"`)
	assert.Contains(t, got, `"left as-is"`)
	assert.Contains(t, got, "Fix the root cause")
}

// The annotation is one line under the message. A finding-per-phrase list would
// be longer than the message it comments on.
func TestTheAnnotationNamesAtMostThreePhrases(t *testing.T) {
	var b strings.Builder
	for _, p := range bannedPhrases[:6] {
		b.WriteString(p)
		b.WriteString(". ")
	}
	got := Annotate(b.String())
	require.NotEmpty(t, got)
	assert.Equal(t, 3, strings.Count(got, `"`)/2, "three phrases quoted in %q", got)
	assert.Contains(t, got, "and more")
	assert.Equal(t, 1, strings.Count(strings.TrimPrefix(got, "\n\n"), "\n")+1, "one line: %q", got)
}

func TestAFixedAndOwnedFindingIsNotAnnotated(t *testing.T) {
	delta := "Found the race in the retry loop and fixed it here. The suite is green."
	assert.Empty(t, flush(t, HookInput{MessageID: "m1", Index: 0, Final: true, Delta: delta}))
}

// Naming a blocker plainly is the org's own documented escape hatch.
func TestAnHonestDeferralIsNotAnnotated(t *testing.T) {
	delta := "This needs your call on A vs B, so I pushed the branch with A and left the test red."
	assert.Empty(t, flush(t, HookInput{MessageID: "m1", Index: 0, Final: true, Delta: delta}))
}

// A banned phrase can span a line wrap, and one flush carries only the lines
// that completed since the last one. So the message is accumulated and judged
// whole on its final flush.
func TestAPhraseSplitAcrossFlushesIsStillFound(t *testing.T) {
	assert.Empty(t, flush(t, HookInput{MessageID: "wrap", Index: 0, Delta: "This is worth your\n"}),
		"a non-final flush is never annotated")

	out := flush(t, HookInput{MessageID: "wrap", Index: 1, Final: true, Delta: "attention before we ship."})
	assert.Contains(t, displayed(t, out, ""), `"worth your attention"`)
}

// The final flush's delta is empty when the message ends on a newline. It is
// still the end-of-message signal, and the annotation still lands.
func TestAnEmptyFinalFlushStillCarriesTheAnnotation(t *testing.T) {
	require.Empty(t, flush(t, HookInput{MessageID: "empty-final", Index: 0, Delta: "That is out of scope.\n"}))

	out := flush(t, HookInput{MessageID: "empty-final", Index: 1, Final: true})
	got := displayed(t, out, "")
	assert.Contains(t, got, `"out of scope"`)
	assert.NotContains(t, got, "That is out of scope", "the earlier flush is not redisplayed")
}

// The non-streaming path calls the hook once, with index 0, final true, and the
// whole message as the delta.
func TestTheWholeMessageInOneFlushIsAnnotated(t *testing.T) {
	delta := "Not my problem, this was existing code."
	out := flush(t, HookInput{MessageID: "one-shot", Index: 0, Final: true, Delta: delta})
	assert.Contains(t, displayed(t, out, delta), "no-blame-language")
}

// The annotation is never repeated: each message is judged once, on its last
// flush, and the accumulated text is dropped there.
func TestAMessageIsAnnotatedOnceAndItsStateIsDropped(t *testing.T) {
	require.NotEmpty(t, flush(t, HookInput{MessageID: "once", Index: 0, Final: true, Delta: "It is pre-existing."}))
	assert.Empty(t, priorText("once"), "the accumulated text is dropped on the final flush")
	assert.Empty(t, flush(t, HookInput{MessageID: "once", Index: 0, Final: true, Delta: "All green."}))
}

// Printing nothing leaves the CLI showing the original delta, which is the only
// acceptable failure for a hook in the render path.
func TestEverySurpriseRendersTheOriginal(t *testing.T) {
	cases := map[string]string{
		"not json":       "not json",
		"empty":          "",
		"another event":  `{"hook_event_name":"Stop","final":true,"delta":"It is pre-existing."}`,
		"no event name":  `{"final":true,"delta":"It is pre-existing."}`,
		"nothing to say": `{"hook_event_name":"MessageDisplay","final":true,"delta":"the suite is green."}`,
		"empty delta":    `{"hook_event_name":"MessageDisplay","final":true,"delta":""}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, run(strings.NewReader(in)))
		})
	}
}

// Nothing is sent back to the model and nothing is refused: the process always
// exits 0, and the only output it can produce is a displayContent envelope.
func TestTheOnlyOutputIsADisplayContentEnvelope(t *testing.T) {
	out := flush(t, HookInput{MessageID: "shape", Final: true, Delta: "It is pre-existing."})
	require.NotEmpty(t, out)

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))
	require.Len(t, raw, 1, "no decision, no reason, no systemMessage: %v", raw)

	inner, ok := raw["hookSpecificOutput"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []string{"displayContent", "hookEventName"}, sortedKeys(inner))
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestTheHookCanBeTurnedOff(t *testing.T) {
	t.Setenv("CC_NO_BLAME_LANGUAGE", "0")
	assert.Empty(t, flush(t, HookInput{MessageID: "off", Final: true, Delta: "It is pre-existing."}))
}
