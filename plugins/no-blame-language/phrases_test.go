package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phraseTexts is the reported phrase list, which is what the refusal quotes.
func phraseTexts(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Phrase)
	}
	return out
}

func TestEveryBannedPhraseIsFound(t *testing.T) {
	for _, phrase := range bannedPhrases {
		t.Run(phrase, func(t *testing.T) {
			text := "I looked into it: " + phrase + ", so I left it there."
			hits := FindBannedPhrases(text)
			require.NotEmpty(t, hits, "expected a hit for %q", phrase)
			assert.Contains(t, phraseTexts(hits), phrase)
		})
	}
}

func TestMatchingIsCaseInsensitive(t *testing.T) {
	hits := FindBannedPhrases("That's PRE-EXISTING, not something I touched.")
	assert.Contains(t, phraseTexts(hits), "pre-existing")
}

func TestALineWrapDoesNotHideAPhrase(t *testing.T) {
	// A markdown line-wrap replaces the space between "your" and "attention"
	// with a newline. The phrase must still be found.
	hits := FindBannedPhrases("This is worth your\nattention before we ship.")
	assert.Contains(t, phraseTexts(hits), "worth your attention")
}

func TestARunOfWhitespaceCollapsesToOneSpace(t *testing.T) {
	hits := FindBannedPhrases("out   of\n\n  scope for this change.")
	assert.Contains(t, phraseTexts(hits), "out of scope")
}

func TestOrdinaryProseIsNotAHit(t *testing.T) {
	cases := []string{
		"I found the bug in auth.go and fixed it; the suite is green.",
		"The scope of this change is the parser, not the lexer.",
		"Someone on the team asked for this feature last quarter.",
		"This file predates the rewrite of the storage layer.",
	}
	for _, text := range cases {
		assert.Empty(t, FindBannedPhrases(text), "expected no hit for %q", text)
	}
}

func TestAFixedAndOwnedFindingIsNotAHit(t *testing.T) {
	text := "Found a bug in the retry loop and fixed it in this same change. " +
		"Tests pass and the suite is green."
	assert.Empty(t, FindBannedPhrases(text))
}

func TestFencedIndentedAndBlockquotedTextIsExempt(t *testing.T) {
	text := "Here is the rule:\n\n```\nnever write pre-existing or flagging this for you\n```\n\n" +
		"> the linter used to say: out of scope\n\n    indented: not my problem\n\nEverything above is fine."
	assert.Empty(t, FindBannedPhrases(text))
}

func TestInlineBackticksAreNotExempt(t *testing.T) {
	hits := FindBannedPhrases("This is `pre-existing` behavior, so I left it.")
	assert.Contains(t, phraseTexts(hits), "pre-existing")
}

func TestEachPhraseIsReportedOnce(t *testing.T) {
	hits := FindBannedPhrases("out of scope, definitely out of scope, still out of scope.")
	assert.Equal(t, []string{"out of scope"}, phraseTexts(hits))
}

func TestReportCarriesPhraseAndLine(t *testing.T) {
	hits := FindBannedPhrases("first line\nthat bug is pre-existing and not mine.\nlast line")
	require.Len(t, hits, 1)
	assert.Equal(t, "pre-existing", hits[0].Phrase)
	assert.Equal(t, "that bug is pre-existing and not mine.", hits[0].Line)
}

func TestLineIsBounded(t *testing.T) {
	long := ""
	for len(long) < 400 {
		long += "padding words "
	}
	hits := FindBannedPhrases(long + "out of scope")
	require.Len(t, hits, 1)
	assert.Len(t, hits[0].Line, 160)
}

func TestFenceMarker(t *testing.T) {
	assert.Equal(t, "`", fenceMarker("```go"))
	assert.Equal(t, "~", fenceMarker("  ~~~"))
	assert.Equal(t, "", fenceMarker("plain text"))
}

func TestUnclosedFenceSwallowsTheRest(t *testing.T) {
	assert.Empty(t, FindBannedPhrases("intro\n```\nout of scope\n"))
}

func TestNormalizeWhitespaceCollapsesRuns(t *testing.T) {
	norm, offsets := normalizeWhitespace("a  b\n\tc")
	assert.Equal(t, "a b c", norm)
	require.Len(t, offsets, len(norm))
}

func TestEmptyInput(t *testing.T) {
	assert.Empty(t, FindBannedPhrases(""))
}
