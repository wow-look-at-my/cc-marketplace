package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The non-Go reader has no parser to lean on, so the cases it must get right --
// a marker inside a string, a span comment, coalescing -- are pinned here.

func TestLineBlocksAcrossLanguages(t *testing.T) {
	tests := []struct {
		name, file, src string
		wantText        []string
		wantNot         []string
	}{
		{
			name: "consecutive line comments coalesce into one block",
			file: "a.ts",
			src:  "// first line\n// second line\nconst x = 1;\n",
			// One block, both lines, so a claim spanning them is seen whole.
			wantText: []string{"first line\nsecond line"},
		},
		{
			name:     "a marker inside a string is not a comment",
			file:     "a.ts",
			src:      "const url = \"https://x/y//z\";\nconst re = '# not a comment';\n",
			wantNot:  []string{"z", "not a comment"},
			wantText: nil,
		},
		{
			name:     "a span comment is taken whole",
			file:     "a.ts",
			src:      "/* one\n   two */\nconst x = 1;\n",
			wantText: []string{"one"},
		},
		{
			name:     "hash comments",
			file:     "a.sh",
			src:      "# probably fine\necho hi\n",
			wantText: []string{"probably fine"},
		},
		{
			name:     "blank lines separate blocks",
			file:     "a.ts",
			src:      "// alpha\n\n// beta\nconst x = 1;\n",
			wantText: []string{"alpha", "beta"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			touched := allLines(tc.src)
			blocks := blocksIn(tc.file, []byte(tc.src), touched)
			var texts []string
			for _, b := range blocks {
				texts = append(texts, b.Text)
			}
			joined := strings.Join(texts, "|")
			for _, want := range tc.wantText {
				assert.Contains(t, joined, want)
			}
			for _, not := range tc.wantNot {
				assert.NotContains(t, joined, not)
			}
		})
	}
}

func TestCommentStartSeesThroughQuotes(t *testing.T) {
	assert.Equal(t, -1, commentStart(`const u = "http://x//y";`))
	assert.Equal(t, -1, commentStart("const s = `a // b`;"))
	assert.Equal(t, -1, commentStart(`const e = "a \" // b";`))
	assert.Equal(t, 11, commentStart(`const x=1; // real`))
	assert.Equal(t, 0, commentStart(`# shell`))
}

// A comment is paired with the code below it, which is the evidence for "is
// this comment even about the thing it sits on".
func TestBlockCarriesTheCodeItAnnotates(t *testing.T) {
	src := "package p\n\n// Encode writes the header first.\nfunc Encode() {}\n"
	blocks := blocksIn("a.go", []byte(src), allLines(src))
	require.Len(t, blocks, 1)
	assert.Contains(t, blocks[0].Code, "func Encode()")
}

// A file mid-edit does not parse, and a checker that goes quiet exactly when
// the code is in flux is worth very little.
func TestUnparseableGoFallsBackRatherThanSkipping(t *testing.T) {
	src := "package p\n\n// This probably breaks.\nfunc Encode( {\n"
	blocks := blocksIn("a.go", []byte(src), allLines(src))
	require.NotEmpty(t, blocks)
	assert.Contains(t, blocks[0].Text, "probably")
}

func allLines(src string) map[int]bool {
	out := map[int]bool{}
	for i := range strings.Split(src, "\n") {
		out[i+1] = true
	}
	return out
}

// --- the judgment stage ------------------------------------------------------

func TestParseVerdict(t *testing.T) {
	blocks := []Block{
		{File: "a.go", Line: 3, Text: "first"},
		{File: "b.go", Line: 9, Text: "second"},
	}

	got, err := parseVerdict(`{"findings":[{"index":1,"problem":"unsupported","evidence":"the code allows it"}]}`, blocks)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.go", got[0].File)
	assert.Equal(t, 9, got[0].Line)
	assert.Equal(t, "unsupported", got[0].Problem)

	// The expected answer is nothing.
	got, err = parseVerdict(`{"findings":[]}`, blocks)
	require.NoError(t, err)
	assert.Empty(t, got)

	// Models wrap JSON in prose; that is not a reason to lose the verdict.
	got, err = parseVerdict("Here you go:\n{\"findings\":[{\"index\":0,\"problem\":\"p\"}]}\nhope that helps", blocks)
	require.NoError(t, err)
	require.Len(t, got, 1)

	// An index outside the batch is dropped rather than mapped to the wrong
	// comment -- reporting a finding against innocent code is worse than
	// losing it.
	got, err = parseVerdict(`{"findings":[{"index":99,"problem":"p"},{"index":-1,"problem":"q"}]}`, blocks)
	require.NoError(t, err)
	assert.Empty(t, got)

	// Unparseable is an ERROR, never a silent pass.
	_, err = parseVerdict("I could not do that", blocks)
	require.Error(t, err)
}

// The whole judgment stage against a stub endpoint: the request carries the
// comment AND the evidence, and the verdict maps back onto the right comment.
func TestReviewerRoundTrip(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n// Decodes 5x faster because nothing parses per record.\nfunc Encode() {}\n")
	repo, ok := openRepo(dir)
	require.True(t, ok)
	blocks := repo.changedBlocks()
	require.NotEmpty(t, blocks)

	var sawPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer k", r.Header.Get("Authorization"))
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Len(t, body.Messages, 2)
		sawPrompt = body.Messages[1].Content
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"findings\":[{\"index\":0,\"problem\":\"unmeasured\",\"evidence\":\"no benchmark\"}]}"}}]}`))
	}))
	defer srv.Close()

	rv := &Reviewer{URL: srv.URL, Key: "k", Model: "m", Client: srv.Client()}
	got, err := rv.Review(context.Background(), repo, blocks)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "unmeasured", got[0].Problem)

	// The model is handed the evidence, not sent looking for it.
	assert.Contains(t, sawPrompt, "=== COMMENT 0 ===")
	assert.Contains(t, sawPrompt, "THE CODE THIS COMMENT IS ATTACHED TO")
	assert.Contains(t, sawPrompt, "func Encode()")
}

func TestReviewerReportsHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	rv := &Reviewer{URL: srv.URL, Key: "k", Model: "m", Client: srv.Client()}
	_, err := rv.Review(context.Background(), &Repo{Root: t.TempDir()}, []Block{{Text: "x"}})
	require.Error(t, err)
}

func TestNewReviewerDefaults(t *testing.T) {
	t.Setenv(envKey, "")
	_, ok := newReviewer()
	assert.False(t, ok, "no key must be a clean no-op")

	t.Setenv(envKey, "k")
	t.Setenv(envURL, "")
	t.Setenv(envModel, "")
	rv, ok := newReviewer()
	require.True(t, ok)
	assert.NotEmpty(t, rv.URL)
	assert.NotEmpty(t, rv.Model)
}

// Evidence assembly pulls the lines of a cited document that carry the
// comment's figures, rather than shipping the whole document.
func TestEvidencePullsCitedDocLines(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n// The payload is 23.8 B/event, see docs/perf.md\nfunc Encode() {}\n")
	repo, ok := openRepo(dir)
	require.True(t, ok)
	blocks := repo.changedBlocks()
	require.NotEmpty(t, blocks)

	ev := repo.evidenceFor(blocks[0])
	assert.Contains(t, ev, "docs/perf.md")
	assert.Contains(t, ev, "23.8")
}

// A cross-repo citation is normal in a multi-repo session and must not be
// reported as missing.
func TestSiblingCheckoutResolvesACitation(t *testing.T) {
	parent := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(parent, "lib"), 0o755))
	lib := filepath.Join(parent, "lib")
	initRepo(t, lib)
	write(t, lib, "wire-format.ts", "export const x = 1;\n")
	commit(t, lib, "lib")

	app := filepath.Join(parent, "app")
	require.NoError(t, os.Mkdir(app, 0o755))
	initRepo(t, app)
	write(t, app, "seed.go", "package p\n")
	commit(t, app, "base")
	write(t, app, "use.go", "package p\n\n// The decoder is wire-format.ts next door.\nfunc Use() {}\n")

	assert.Empty(t, check(app), "a file in the sibling checkout was reported missing")
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "-q", "-b", "master")
}
