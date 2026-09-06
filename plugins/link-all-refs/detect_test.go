package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokens is the matched token list, in the order they sit on the line.
func tokens(refs []Located) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Text)
	}
	return out
}

func TestEachKindIsMatched(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
		kind string
	}{
		{"pr number", "PR #376 is merged and green.", "#376", "an issue or pull request number"},
		{"owner repo number", "wow-look-at-my/go-toolchain#376 landed.", "wow-look-at-my/go-toolchain#376", "an issue or pull request number"},
		{"short sha", "re-pushed as (`6884dd2`).", "6884dd2", "a commit SHA"},
		{"long sha", "master is at e3665a4689bb now.", "e3665a4689bb", "a commit SHA"},
		{"branch", "pushed `claude/binary-name-collision-drop` to origin.", "claude/binary-name-collision-drop", "a branch"},
		{"bare url", "here: https://github.com/wow-look-at-my/agentic-loop/pull/30", "https://github.com/wow-look-at-my/agentic-loop/pull/30", "a bare GitHub URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refs := FindUnlinkedInLine(tc.text)
			require.NotEmpty(t, refs, "expected a match in %q", tc.text)
			assert.Contains(t, tokens(refs), tc.want)
			for _, r := range refs {
				if r.Text == tc.want {
					assert.Equal(t, tc.kind, r.Kind)
					assert.Equal(t, tc.want, tc.text[r.Start:r.End], "offsets must select the token")
				}
			}
		})
	}
}

// A '.' joins a filename to its extension, and it also ends a sentence. Counting
// every trailing '.' as part of the token made every reference that ENDS a
// sentence invisible to this guard.
func TestAReferenceAtTheEndOfASentenceIsMatched(t *testing.T) {
	cases := map[string]string{
		"re-pushed as 6884dd2.":       "6884dd2",
		"pushed claude/thing.":        "claude/thing",
		"merged in #376.":             "#376",
		"landed as 6884dd2, finally.": "6884dd2",
	}
	for text, want := range cases {
		refs := FindUnlinkedInLine(text)
		require.NotEmpty(t, refs, "expected a match in %q", text)
		assert.Contains(t, tokens(refs), want, "for %q", text)
	}
}

// ... and the case the dot rule exists for still holds.
func TestADottedNameIsNotAReference(t *testing.T) {
	cases := []string{
		"A file named 6884dd2abc.log is not a commit.",
		"Read plugins/example-plugin/README.md for the template.",
		"The moved files live in go/core and go/client.",
		"It ran 1234567 iterations in 20260816013119 nanoseconds.",
		"The word defaced is spelled entirely in hex letters.",
		"`&#0;` is how NUL travels, and `&#x1F;` is a restricted character.",
		"The build passes at 89.4% coverage and vet is clean.",
		"## Rules",
	}
	for _, text := range cases {
		assert.Empty(t, FindUnlinkedInLine(text), "expected no match in %q", text)
	}
}

// Text that is already a link is blanked before matching, so the rewrite cannot
// nest one link inside another.
func TestAlreadyLinkedTextIsNotMatched(t *testing.T) {
	cases := []string{
		"[wow-look-at-my/go-toolchain#376](https://github.com/wow-look-at-my/go-toolchain/pull/376) is merged.",
		"[6884dd2](https://github.com/o/r/commit/6884dd2) fixes it.",
		"[claude/fix](https://github.com/o/r/compare/master...claude/fix?expand=1) is pushed.",
		"see <https://github.com/o/r/pull/1>",
		"[the PR](https://github.com/o/r/pull/12 \"title\") is green.",
	}
	for _, text := range cases {
		assert.Empty(t, FindUnlinkedInLine(text), "expected no match in %q", text)
	}
}

// Blanking preserves length, so an offset taken from the blanked line still
// selects the same bytes in the original. Stripping would move every offset
// after the link and splice the next one into the middle of a word.
func TestOffsetsSurviveALinkEarlierOnTheLine(t *testing.T) {
	line := "[o/r#1](https://github.com/o/r/pull/1) is merged, and #2 is not."
	refs := FindUnlinkedInLine(line)
	require.Len(t, refs, 1)
	assert.Equal(t, "#2", refs[0].Text)
	assert.Equal(t, "#2", line[refs[0].Start:refs[0].End])
}

// A URL contains a slug the branch matcher also matches, and rewriting both
// would nest a link inside a link.
func TestAnOverlappingMatchIsDropped(t *testing.T) {
	refs := FindUnlinkedInLine("https://github.com/o/r/tree/claude/thing")
	require.Len(t, refs, 1)
	assert.Equal(t, "a bare GitHub URL", refs[0].Kind)
}

// Every occurrence is reported, because each one is rewritten independently.
func TestEveryOccurrenceIsReported(t *testing.T) {
	assert.Equal(t, []string{"#376", "#376"}, tokens(FindUnlinkedInLine("#376 blocked, then #376 merged.")))
}

func TestMatchesAreOrderedByPosition(t *testing.T) {
	refs := FindUnlinkedInLine("first #1 then 6884dd2 then claude/x here")
	assert.Equal(t, []string{"#1", "6884dd2", "claude/x"}, tokens(refs))
}

func TestValidBranchRequiresANameAfterThePrefix(t *testing.T) {
	assert.False(t, validBranch("fix/"))
	assert.True(t, validBranch("fix/a"))
}

// A SHA needs both a digit and a hex letter, which is what separates a commit
// from a decimal number and from a word spelled in a-f.
func TestValidSHA(t *testing.T) {
	assert.True(t, validSHA("6884dd2"))
	assert.False(t, validSHA("1234567"))
	assert.False(t, validSHA("defaced"))
}

func TestFenceMarker(t *testing.T) {
	assert.Equal(t, "`", fenceMarker("```go"))
	assert.Equal(t, "~", fenceMarker("  ~~~"))
	assert.Equal(t, "", fenceMarker("plain text"))
}

func TestBlankLinksPreservesLength(t *testing.T) {
	line := "[a](https://github.com/o/r/pull/1) and &#0; and <https://github.com/o/r>"
	assert.Len(t, blankLinks(line), len(line))
}

func TestEmptyInput(t *testing.T) {
	assert.Empty(t, FindUnlinkedInLine(""))
}
