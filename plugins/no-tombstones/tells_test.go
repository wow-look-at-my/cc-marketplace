package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// comment wraps prose as a Go comment block, which is how the detector sees it.
func comment(lines ...string) []Block {
	var out []string
	for _, l := range lines {
		out = append(out, "// "+l)
	}
	return commentBlocks(strings.Join(out, "\n"), cLike)
}

// tellFor runs the detector and returns the tell that fired, or "" for none.
// Naming the tell is what makes each case load-bearing: a deny arriving from an
// unrelated rule must not satisfy an assertion about this one.
func tellFor(t *testing.T, lines ...string) string {
	t.Helper()
	hits := FindTombstones(comment(lines...), 0)
	if len(hits) == 0 {
		return ""
	}
	return hits[0].Tell
}

// The rows below are the worked example this plugin was designed against: a
// block of release prose, split into the sentences that must be refused and the
// sentences that must survive. Every keeper is present tense with a referent
// the reader can go and look at.
func TestTheSampleBlockSplitsIntoTombstonesAndKeepers(t *testing.T) {
	tombstones := []struct {
		want string
		text string
	}{
		{"a date", "2026-09-02: the macOS metadata wave"},
		{"a change reference", "as commit 4815162 established"},
		{"a former state", "section 6 listed the syscalls that used to fall through"},
		{"a former state", "os.Chmod no longer fails on macOS"},
		{"a former state", "the old dispatch spine is replaced"},
		{"a former state", "we removed the arm64 build tag from the table"},
		{"a then-and-now contrast", "the table lives here rather than the old arm64 file"},
		{"a then-and-now contrast", "all of them are emulated now"},
		{"an address to the reviewer", "what is worth keeping here is the nosplit budget"},
		{"an address to the reviewer", "this change is not tidying, and here is why"},
		{"an address to the reviewer", "worth noting that the ubuntu leg runs these"},
		{"a defence of the change", "an arm64-tagged table would have been dead weight"},
		{"a quoted instruction", "the owner ruled that every table moves"},
		{"a report of an experiment", "negative control: breaking one flag mapping turned the test red"},
		{"a report of an experiment", "i ran this against a linux host to confirm"},
	}
	for _, row := range tombstones {
		t.Run(row.text, func(t *testing.T) {
			assert.Equal(t, row.want, tellFor(t, row.text), "should be refused as %s", row.want)
		})
	}

	keepers := []string{
		"every handler on this spine is nosplit; a stack split here is fatal",
		"Apple's struct statfs is 2168 bytes, so it cannot be built in this frame",
		"the emulation refuses a buffer smaller than the Apple struct and returns EINVAL",
		"getpriority returns a value where -1 is legal, so errno decides failure",
		"errno must be read before the second lseek, or the fixup clobbers it",
		"sendfile reverses Apple's first two arguments against Linux's",
		"the size guard is what makes the struct pin load-bearing",
		"a Linux caller passing no offset needs an lseek before and after",
		// A demonstrative in front of a word that is also a participle. This
		// is the false positive the first run against real prose exposed.
		"that split has an obvious way to go wrong",
		"this merge of the two paths is what the size guard protects",
		"the buffer is dropped when the caller passes no offset",
	}
	for _, text := range keepers {
		t.Run(text, func(t *testing.T) {
			assert.Empty(t, tellFor(t, text), "current-state prose must not be refused")
		})
	}
}

func TestAVolumeCapCatchesAnEssayWhoseEverySentenceIsCurrent(t *testing.T) {
	var lines []string
	for range 20 {
		lines = append(lines, "this handler runs after entersyscall and must not split the stack")
	}
	hits := FindTombstones(comment(lines...), 14)
	require.NotEmpty(t, hits)
	assert.Contains(t, hits[0].Tell, "comment block of 20 lines")

	assert.Empty(t, FindTombstones(comment(lines...), 0), "a zero cap turns the rule off")
	assert.Empty(t, FindTombstones(comment(lines[:5]...), 14), "a short block passes")
}

func TestACodeStringIsNotAComment(t *testing.T) {
	src := `msg := "this PR previously removed the flag"` + "\n" + `x := 1 // holds the count`
	assert.Empty(t, FindTombstones(commentBlocks(src, cLike), 0),
		"a tell inside a string literal is data, not the file's own prose")
}

func TestBlockCommentsAreRead(t *testing.T) {
	src := "/* the old parser was replaced here */\nfunc f() {}"
	hits := FindTombstones(commentBlocks(src, cLike), 0)
	require.NotEmpty(t, hits)
	assert.Equal(t, "a former state", hits[0].Tell)
}

func TestAdjacentLineCommentsMergeIntoOneBlock(t *testing.T) {
	src := "// one\n// two\n// three\ncode()\n// separate"
	blocks := commentBlocks(src, cLike)
	require.Len(t, blocks, 2)
	assert.Equal(t, 3, blocks[0].Lines)
	assert.Equal(t, 1, blocks[1].Lines)
}

func TestHashAndDashLanguages(t *testing.T) {
	assert.NotEmpty(t, FindTombstones(commentBlocks("# this used to run twice", hashish), 0))
	assert.NotEmpty(t, FindTombstones(commentBlocks("-- formerly a view", dashish), 0))
	assert.Empty(t, FindTombstones(commentBlocks(`echo "used to"`, hashish), 0))
}

func TestDocumentProseSkipsFencesAndInlineCode(t *testing.T) {
	doc := "```\nthis used to be a flag\n```\n\nthe `used to` marker is quoted here\n\nthe old spelling is gone"
	hits := FindTombstones(paragraphs(doc), 0)
	require.Len(t, hits, 1, "only the unfenced, unquoted sentence counts")
	assert.Equal(t, "a former state", hits[0].Tell)
}

func TestOnlyKnownLanguagesAreJudged(t *testing.T) {
	assert.Empty(t, AddedBlocks("weird.xyzzy", "// this used to work"),
		"an unknown syntax is never guessed at")
	assert.NotEmpty(t, AddedBlocks("main.go", "// this used to work"))
	assert.NotEmpty(t, AddedBlocks("Makefile", "# this used to work"))
	assert.NotEmpty(t, AddedBlocks("notes.md", "this used to work"))
}
