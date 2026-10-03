package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Length is a defect only against what the comment annotates. Both directions
// matter: a manual on a constant is the failure, and trimming a long comment
// that earns its place is the same failure in reverse.
func TestBloatIsProportionNotLength(t *testing.T) {
	long := "This explains a great deal about the surrounding system, at length,\n" +
		"across many lines, with detail that would take a reader some time to\n" +
		"reconstruct from the code itself, and it keeps going well past the\n" +
		"point where a short note would have done, elaborating on the history\n" +
		"and the alternatives and the measurements and the reasoning behind\n" +
		"each of them in turn, at a level of detail that belongs in a document\n" +
		"rather than beside a single line of source, which is the whole point\n" +
		"of the check this test is exercising right now in this test file here."

	t.Run("on a constant it is reported", func(t *testing.T) {
		_, over := bloated(Block{Text: long, Code: `const GSMBaseURL = "https://x.example"`})
		assert.True(t, over)
	})
	t.Run("on a struct field it is reported", func(t *testing.T) {
		_, over := bloated(Block{Text: long, Code: "\tRoot string"})
		assert.True(t, over)
	})
	t.Run("on a function it is NOT", func(t *testing.T) {
		_, over := bloated(Block{Text: long, Code: "func (r *Repo) checkMechanically(blocks []Block) (findings []Finding) {"})
		assert.False(t, over, "a long comment on something substantial is correct")
	})
	t.Run("on a type it is NOT", func(t *testing.T) {
		_, over := bloated(Block{Text: long, Code: "type Reviewer struct {"})
		assert.False(t, over)
	})
	t.Run("a short comment on a constant is fine", func(t *testing.T) {
		_, over := bloated(Block{Text: "The mirror every container's traffic rides.", Code: `const Base = "x"`})
		assert.False(t, over)
	})
	t.Run("a package doc is not measured against the next line", func(t *testing.T) {
		_, over := bloated(Block{Text: long, Code: "package main"})
		assert.False(t, over)
	})
}

// Only the markers that are never legitimate: a date, a PR number.
func TestTombstoneMarkers(t *testing.T) {
	for _, text := range []string{
		"Changed 2026-07-15 after the incident.",
		"See #193 for why this exists.",
		"buildhost#193 made this part of publishing.",
	} {
		c := Analyze(Block{Text: text})
		assert.NotEmpty(t, c.Tombstones, "%q should read as a changelog", text)
	}

	// The past tense counts too: each of these has a subject that is absent
	// from the file, which is what makes it a changelog rather than an
	// explanation.
	for _, text := range []string{
		"The former WEBHOOK_RUNNER_GSM_URL knob was removed.",
		"this used to re-apply the caller deadline",
		"the retry loop is no longer here",
		"formerly a per-actor gate",
		"the fallback has been deleted",
	} {
		c := Analyze(Block{Text: text})
		assert.NotEmpty(t, c.Tombstones, "%q narrates the change", text)
	}

	// Naming one of these phrases as an EXAMPLE is not using it. Without this
	// the rule reports any comment that documents the rule -- including this
	// tool's own source, which is the one file whose subject matter IS these
	// phrases. That is a property of writing about tombstones, not a reason to
	// stop detecting them.
	for _, text := range []string{
		`the past tense ("used to", "no longer", "was removed")`,
		`flags a comment saying "formerly" or "previously"`,
		`matches a date like "2026-07-15" in prose`,
	} {
		c := Analyze(Block{Text: text})
		assert.Empty(t, c.Tombstones, "%q shows the phrase, it does not use it", text)
	}
}

// Naming the SHAPE of a thing is not a claim that a thing with that literal
// name exists.
func TestPlaceholdersAreNotCitations(t *testing.T) {
	for _, text := range []string{
		"cuts the next `timelinewire/vX.Y.Z` tag",
		"tags matching `plugin/*/v*` are cleaned up",
		"see `docs/<area>/<thing>.md` for the depth",
	} {
		c := Analyze(Block{Text: text})
		assert.Empty(t, c.References, "%q names a pattern, not a file", text)
	}
	// A real citation alongside a pattern still resolves as one.
	c := Analyze(Block{Text: "cuts `timelinewire/vX.Y.Z` from `deploy.yml`"})
	require.Len(t, c.References, 1)
	assert.Equal(t, "deploy.yml", c.References[0])
}
