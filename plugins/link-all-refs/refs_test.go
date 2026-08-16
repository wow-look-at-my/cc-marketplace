package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokens is the reported token list, which is what the refusal quotes.
func tokens(refs []Ref) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Text)
	}
	return out
}

func TestUnlinkedReferencesAreReported(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"pr number", "PR #376 is merged and green.", "#376"},
		{"owner repo number", "wow-look-at-my/go-toolchain#376 landed.", "wow-look-at-my/go-toolchain#376"},
		{"short sha", "re-pushed as (`6884dd2`).", "6884dd2"},
		{"long sha", "master is at e3665a4689bb now.", "e3665a4689bb"},
		{"branch", "pushed `claude/binary-name-collision-drop` to origin.", "claude/binary-name-collision-drop"},
		{"bare url", "here: https://github.com/wow-look-at-my/agentic-loop/pull/30", "https://github.com/wow-look-at-my/agentic-loop/pull/30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refs := FindUnlinked(tc.text)
			require.NotEmpty(t, refs, "expected a report for %q", tc.text)
			assert.Contains(t, tokens(refs), tc.want)
		})
	}
}

func TestLinkedReferencesPass(t *testing.T) {
	cases := []string{
		"[wow-look-at-my/go-toolchain#376](https://github.com/wow-look-at-my/go-toolchain/pull/376) is merged.",
		"[6884dd2](https://github.com/o/r/commit/6884dd2) fixes it.",
		"[claude/fix](https://github.com/o/r/compare/master...claude/fix?expand=1) is pushed.",
		"see <https://github.com/o/r/pull/1>",
		"[the PR](https://github.com/o/r/pull/12 \"title\") is green.",
	}
	for _, text := range cases {
		assert.Empty(t, FindUnlinked(text), "expected no report for %q", text)
	}
}

func TestOrdinaryProseIsNotAReference(t *testing.T) {
	cases := []string{
		"The build passes at 89.4% coverage and vet is clean.",
		"## Rules",
		"`&#0;` is how NUL travels, and `&#x1F;` is a restricted character.",
		"The moved files live in go/core and go/client.",
		"Read plugins/example-plugin/README.md for the template.",
		"It ran 1234567 iterations in 20260816013119 nanoseconds.",
		"The word defaced is spelled entirely in hex letters.",
		"A file named 6884dd2abc.log is not a commit.",
	}
	for _, text := range cases {
		assert.Empty(t, FindUnlinked(text), "expected no report for %q", text)
	}
}

func TestQuotedAndFencedTextIsExempt(t *testing.T) {
	text := "Here is the rule:\n\n```\nPR #376 at 6884dd2 on claude/thing\n```\n\n> the user wrote: PR #999\n\n    indented #123\n\nAll linked above."
	assert.Empty(t, FindUnlinked(text))
}

func TestInlineBackticksAreNotExempt(t *testing.T) {
	refs := FindUnlinked("re-pushed as `claude/binary-name-collision-drop` (`6884dd2`).")
	assert.ElementsMatch(t, []string{"6884dd2", "claude/binary-name-collision-drop"}, tokens(refs))
}

func TestALinkEarlierEarnsNoCreditForALaterMention(t *testing.T) {
	text := "[o/r#1](https://github.com/o/r/pull/1) is merged. #2 is not."
	assert.Equal(t, []string{"#2"}, tokens(FindUnlinked(text)))
}

func TestEachTokenIsReportedOnce(t *testing.T) {
	refs := FindUnlinked("#376 blocked, then #376 unblocked, and #376 merged.")
	assert.Equal(t, []string{"#376"}, tokens(refs))
}

func TestReportCarriesKindAndLine(t *testing.T) {
	refs := FindUnlinked("first line\nPR #376 is merged and green.\nlast line")
	require.Len(t, refs, 1)
	assert.Equal(t, "an issue or pull request number", refs[0].Kind)
	assert.Equal(t, "PR #376 is merged and green.", refs[0].Line)
}

func TestLineIsBounded(t *testing.T) {
	long := ""
	for len(long) < 400 {
		long += "padding words "
	}
	refs := FindUnlinked(long + "#376")
	require.Len(t, refs, 1)
	assert.Len(t, refs[0].Line, 160)
}

func TestValidBranchRequiresANameAfterThePrefix(t *testing.T) {
	assert.False(t, validBranch("fix/"))
	assert.True(t, validBranch("fix/a"))
}

func TestFenceMarker(t *testing.T) {
	assert.Equal(t, "`", fenceMarker("```go"))
	assert.Equal(t, "~", fenceMarker("  ~~~"))
	assert.Equal(t, "", fenceMarker("plain text"))
}

func TestUnclosedFenceSwallowsTheRest(t *testing.T) {
	assert.Empty(t, FindUnlinked("intro\n```\nPR #376\n"))
}

func TestEmptyInput(t *testing.T) {
	assert.Empty(t, FindUnlinked(""))
}
