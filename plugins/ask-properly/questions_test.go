package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kinds(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Kind)
	}
	return out
}

func TestAProseQuestionIsFound(t *testing.T) {
	for _, msg := range []string{
		"Which one do you want?",
		"Should I land these now?",
		"What do you want to do with these two repos?",
		"The layout is unpinned. Is that a contract, or an illustration?",
		"Do you want me to pick the strict rule here?",
	} {
		hits := FindQuestions(msg)
		require.NotEmpty(t, hits, "expected a finding in %q", msg)
	}
}

func TestADeferralWithNoQuestionMarkIsFound(t *testing.T) {
	for _, msg := range []string{
		"Four questions remain. Your call.",
		"I won't touch them until you say.",
		"Let me know and I'll land the rest.",
		"Tell me which way you want it and I'll write it up.",
		"Answer them here, or say \"your call\" and I'll pick.",
	} {
		hits := FindQuestions(msg)
		require.NotEmpty(t, hits, "expected a finding in %q", msg)
	}
}

// The incident this plugin exists for: a closing message that lists open
// decisions and invites the user to answer in prose.
func TestTheIncidentMessageIsRefused(t *testing.T) {
	msg := "Eight decisions landed, PR green, and four questions still open.\n\n" +
		"1. Enum inside a collection - one is wrong.\n" +
		"2. Unknown CLI option - exit 64, or into raw_args?\n\n" +
		"Answer them here, or say \"your call\" and I'll pick, land them, and empty the file."
	hits := FindQuestions(msg)
	require.NotEmpty(t, hits)
	assert.Contains(t, kinds(hits), "question")
	assert.Contains(t, kinds(hits), "deferral")
}

// A "?" is not enough on its own. This org's specs are full of nullable types
// and every compare URL carries a query string; matching a bare "?" reports a
// question in a message that asked nothing.
func TestProseThatMustNotTrip(t *testing.T) {
	for _, msg := range []string{
		"find and index_of now return UInt? rather than Int?.",
		"A List<Int?> sorts with null compared as the type's default value.",
		"Pushed. The compare page is https://github.com/o/r/compare/master...claude/x?expand=1 and CI is green.",
		"See [the compare page](https://github.com/o/r/compare/a...b?expand=1) for the diff.",
		"Landed the rule in docs/json.md and pinned it in tests/json.dats. CI is green.",
		"I reverted the edit. Nothing was pushed.",
		"The declaration is `String? find(String s)` and the field is `Int? n`.",
	} {
		assert.Empty(t, FindQuestions(msg), "expected no finding in %q", msg)
	}
}

func TestFencedAndQuotedTextIsExempt(t *testing.T) {
	fenced := "Here is the rule I wrote:\n\n```\nShould I land this?\n```\n\nIt is committed."
	assert.Empty(t, FindQuestions(fenced))

	quoted := "The snippet says:\n\n> Do you want me to fix it?\n\nSo I fixed it."
	assert.Empty(t, FindQuestions(quoted))

	indented := "The doc reads:\n\n    Which one should win?\n\nI picked the first."
	assert.Empty(t, FindQuestions(indented))
}

// Matching the siblings: a question in inline backticks is still a question.
func TestInlineBackticksAreNotExempt(t *testing.T) {
	assert.NotEmpty(t, FindQuestions("So: `which one do you want?`"))
}

func TestEmptyMessageIsAllowed(t *testing.T) {
	assert.Empty(t, FindQuestions(""))
	assert.Empty(t, FindQuestions("   \n\t "))
}

func TestAHitCarriesItsLine(t *testing.T) {
	hits := FindQuestions("Landed the fix.\n\nWhich rule should win?\n\nCI is green.")
	require.Len(t, hits, 1)
	assert.Equal(t, "question", hits[0].Kind)
	assert.Equal(t, "Which rule should win?", hits[0].Line)
}

// A question mark that closes a sentence with no interrogative cue is not a
// question this plugin reports.
func TestAQuestionMarkWithNoCueIsIgnored(t *testing.T) {
	assert.Empty(t, FindQuestions("The field is Int? and the token is a Float."))
}

func TestFindingsAreDedupedPerLine(t *testing.T) {
	hits := FindQuestions("Which one? Which one? Which one?")
	assert.Len(t, hits, 1)
}
