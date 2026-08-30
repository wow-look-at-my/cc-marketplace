package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phrases pulls just the matched text, which is what the assertions are about.
func phrases(doc string) []string {
	var out []string
	for _, h := range FindCounts(doc) {
		out = append(out, h.Phrase)
	}
	return out
}

func TestADigitQuantifyingAPluralNounIsACount(t *testing.T) {
	assert.Equal(t, []string{"15 plugins"}, phrases("The payload embeds this repo's 15 plugins."))
}

func TestASpelledCardinalIsACount(t *testing.T) {
	assert.Equal(t, []string{"four rules"}, phrases("Read the four rules below before editing."))
}

func TestAdjectivesBetweenTheNumberAndTheNounDoNotHideIt(t *testing.T) {
	assert.Equal(t, []string{"15 published marketplace plugins"},
		phrases("It serves 15 published marketplace plugins."))
}

func TestEveryCountOnAPageIsReported(t *testing.T) {
	assert.Equal(t, []string{"three sections", "two events"},
		phrases("It has three sections.\nIt answers two events."))
}

func TestTheReportedLineIsTheWholeLine(t *testing.T) {
	hits := FindCounts("The payload embeds this repo's 15 plugins.")
	require.Len(t, hits, 1)
	assert.Equal(t, "The payload embeds this repo's 15 plugins.", hits[0].Line)
}

// A measurement stays true after somebody adds a plugin, so it is not a count.
func TestAMeasurementIsNotACount(t *testing.T) {
	for _, doc := range []string{
		"The read times out after 20 seconds.",
		"Warn at 500 lines.",
		"The budget is 40000 characters per file.",
		"It retries at most 3 times.",
	} {
		assert.Empty(t, phrases(doc), "%q measures rather than counts", doc)
	}
}

func TestOrdinaryProseWithoutACountIsLeftAlone(t *testing.T) {
	for _, doc := range []string{
		"Every plugin this repo installs rides in the payload.",
		"The sections are evaluated deny > ask > allow.",
		"See docs/hooks.md for the full list.",
		"Version 2 of the format drops the header.",
	} {
		assert.Empty(t, phrases(doc), "%q states no count", doc)
	}
}

func TestFencedCodeIsExempt(t *testing.T) {
	assert.Empty(t, phrases("Prose here.\n```\necho 'this repo has 15 plugins'\n```\nMore prose."))
}

func TestATildeFenceIsExemptToo(t *testing.T) {
	assert.Empty(t, phrases("Prose.\n~~~\nthree sections\n~~~\n"))
}

func TestIndentedCodeIsExempt(t *testing.T) {
	assert.Empty(t, phrases("Prose here.\n\n    grep -c 'three sections' file\n"))
}

func TestInlineBackticksAreExempt(t *testing.T) {
	assert.Empty(t, phrases("Run `head -20 lines` to see it."))
}

func TestAnHTMLCommentIsExempt(t *testing.T) {
	assert.Empty(t, phrases("Prose.\n<!-- there are three sections -->\nMore prose."))
}

func TestYAMLFrontmatterIsExempt(t *testing.T) {
	assert.Empty(t, phrases("---\ndescription: covers three sections\n---\n\nProse with no count.\n"))
}

// Prose after a closed fence is judged again; a fence must not swallow the rest
// of the document.
func TestProseAfterAFenceIsStillJudged(t *testing.T) {
	assert.Equal(t, []string{"three sections"},
		phrases("Prose.\n```\ncode\n```\nIt has three sections."))
}

// "One" is deliberately unmatched: in prose it is almost always a pronoun.
func TestOneIsNotMatched(t *testing.T) {
	assert.Empty(t, phrases("That is the wrong one for this job."))
}

func TestMarkdownPathsAreRecognized(t *testing.T) {
	assert.True(t, IsMarkdown("CLAUDE.md"))
	assert.True(t, IsMarkdown("/repo/docs/Notes.MD"))
	assert.True(t, IsMarkdown("readme.markdown"))
	assert.False(t, IsMarkdown("main.go"))
	assert.False(t, IsMarkdown("settings.json"))
	assert.False(t, IsMarkdown("mdfile"))
}
