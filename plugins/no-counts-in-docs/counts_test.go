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

// A possessive says the things belong here, so the number is an inventory.
func TestAPossessiveFrameMakesItACount(t *testing.T) {
	assert.Equal(t, []string{"15 plugins"}, phrases("The payload embeds this repo's 15 plugins."))
	assert.Equal(t, []string{"four steps"}, phrases("The payload's four steps run in order."))
}

// A having verb asserts what is there, which is the same claim.
func TestAHavingFrameMakesItACount(t *testing.T) {
	assert.Equal(t, []string{"three sections"}, phrases("It has three sections."))
	assert.Equal(t, []string{"two events"}, phrases("The binary answers two events."))
	assert.Equal(t, []string{"five rules"}, phrases("There are five rules in the deny section."))
}

// A deictic points inside the page, so editing the page breaks the number.
func TestADeicticFrameMakesItACount(t *testing.T) {
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
		"The read has 20 seconds before it gives up.",
		"It covers 500 lines.",
		"The budget holds 40000 characters.",
		"It has 3 attempts.",
	} {
		assert.Empty(t, phrases(doc), "%q measures rather than counts", doc)
	}
}

// A quantity with no frame is ordinary technical prose. Refusing it would fire
// on every page, which is how a guard earns the reputation that uninstalls it.
func TestAQuantityWithNoFrameIsLeftAlone(t *testing.T) {
	for _, doc := range []string{
		"The rule fired under five selectors before anybody noticed.",
		"Pre-2.1.205 clients skip the server entirely.",
		"Ten diagnostics per file, thirty overall.",
		"Version 2 of the format drops the header.",
	} {
		assert.Empty(t, phrases(doc), "%q has no inventory frame", doc)
	}
}

func TestOrdinaryProseWithoutACountIsLeftAlone(t *testing.T) {
	for _, doc := range []string{
		"Every plugin this repo installs rides in the payload.",
		"The sections are evaluated deny > ask > allow.",
		"See docs/hooks.md for the full list.",
	} {
		assert.Empty(t, phrases(doc), "%q states no count", doc)
	}
}

// A function word between the number and the noun means the noun is not what
// the number counts.
func TestAFunctionWordBreaksTheQuantity(t *testing.T) {
	assert.Empty(t, phrases("It has 2 of the format drops."))
}

func TestFencedCodeIsExempt(t *testing.T) {
	assert.Empty(t, phrases("Prose here.\n```\necho \"this repo has 15 plugins\"\n```\nMore prose."))
}

func TestATildeFenceIsExemptToo(t *testing.T) {
	assert.Empty(t, phrases("Prose.\n~~~\nit has three sections\n~~~\n"))
}

func TestIndentedCodeIsExempt(t *testing.T) {
	assert.Empty(t, phrases("Prose here.\n\n    grep -c 'it has three sections' file\n"))
}

// Backticks are how this plugin's own documentation quotes the shape it
// refuses, so the same text must pass inside them and fail outside them.
func TestInlineBackticksAreExempt(t *testing.T) {
	assert.Empty(t, phrases("Never write `this repo's 15 plugins`."))
	assert.Equal(t, []string{"15 plugins"}, phrases("Never write this repo's 15 plugins."))
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
