package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func contentArgs(kv map[string]any) map[string]any {
	args := map[string]any{"output_mode": "content"}
	for k, v := range kv {
		args[k] = v
	}
	return args
}

func TestContentBasic(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "alpha needle\nplain\nneedle two\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle"}))
	wantText(t, got, "a.txt:1:alpha needle\na.txt:3:needle two")
}

func TestContentNoLineNumbers(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "alpha needle\nplain\nneedle two\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "-n": false}))
	wantText(t, got, "a.txt:alpha needle\na.txt:needle two")
}

func TestContentCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "NeEdLe here\nnothing\n"})
	g := testTool(t, root)
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "-i": true}))
	wantText(t, got, "a.txt:1:NeEdLe here")
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle"}))
	wantText(t, got, "No matches found")
}

// Context lines print as path-N-text: when neither the path nor the text
// contains a colon, the builtin's first-colon mapping leaves the line
// UNTOUCHED, so context lines stay absolute while match lines are
// relativized. Faithful wart, locked here.
func TestContentContextLinesKeepAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"c.txt", "l1\nl2 needle\nl3\nl4\nl5 needle\nl6\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "-C": 1}))
	want := fmt.Sprintf("%s/c.txt-1-l1\nc.txt:2:l2 needle\n%s/c.txt-3-l3\n%s/c.txt-4-l4\nc.txt:5:l5 needle\n%s/c.txt-6-l6",
		root, root, root, root)
	wantText(t, got, want)
}

// A context line whose TEXT contains a colon gets its prefix relativized
// (first-colon split lands mid-line) — the same faithful wart.
func TestContentContextLineWithColonInText(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"c.txt", "see: this\nl2 needle\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "-B": 1}))
	wantText(t, got, "c.txt-1-see: this\nc.txt:2:l2 needle")
}

func TestContentSeparatorBetweenChunks(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"g.txt", "m needle\nx\ny\nz\nneedle end\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "-C": 1}))
	want := fmt.Sprintf("g.txt:1:m needle\n%s/g.txt-2-x\n--\n%s/g.txt-4-z\ng.txt:5:needle end", root, root)
	wantText(t, got, want)
}

func TestContentContextPrecedence(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"p.txt", "a\nb\nc needle\nd\ne\n"})
	g := testTool(t, root)
	abs := func(n int, text string) string { return fmt.Sprintf("%s/p.txt-%d-%s", root, n, text) }

	// context wins over -C.
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "context": 2, "-C": 0}))
	wantText(t, got, abs(1, "a")+"\n"+abs(2, "b")+"\np.txt:3:c needle\n"+abs(4, "d")+"\n"+abs(5, "e"))

	// -C wins over -B/-A.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "-C": 1, "-B": 2, "-A": 2}))
	wantText(t, got, abs(2, "b")+"\np.txt:3:c needle\n"+abs(4, "d"))

	// -B alone.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "-B": 1}))
	wantText(t, got, abs(2, "b")+"\np.txt:3:c needle")

	// -A alone.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "-A": 1}))
	wantText(t, got, "p.txt:3:c needle\n"+abs(4, "d"))

	// -B and -A together.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "-B": 1, "-A": 1}))
	wantText(t, got, abs(2, "b")+"\np.txt:3:c needle\n"+abs(4, "d"))
}

// Context flags are ignored outside content/filenames_with_matches.
func TestContextIgnoredInFilenamesAndCountModes(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "x needle\ny\n"})
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": "needle", "output_mode": "filenames", "-C": 2})
	wantText(t, got, "Found 1 file\na.txt")
	got = grepOK(t, g, map[string]any{"pattern": "needle", "output_mode": "count", "context": 2})
	wantText(t, got, "a.txt:1\n\nFound 1 total occurrence across 1 file.")
}

// Searching a single FILE path drops the filename prefix from rg's
// output (no -H in content mode): lines arrive as N:text and the mapping
// leaves them alone. Faithful to the builtin.
func TestContentSingleFilePathHasNoFilenamePrefix(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"solo.txt", "one needle\ntwo\nneedle three\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "path": "solo.txt"}))
	wantText(t, got, "1:one needle\n3:needle three")
}

func TestContentColonAndDigitContent(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"w.txt", "12:34 err needle\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle"}))
	wantText(t, got, "w.txt:1:12:34 err needle")
}

func TestContentWeirdFilenameWithColon(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"we:ird.txt", "a needle\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle"}))
	// The first-colon split lands inside the filename, but relativizing
	// the shorter prefix strips the same root prefix, so the line still
	// comes out right.
	wantText(t, got, "we:ird.txt:1:a needle")
}

func TestContentUnicodeMatch(t *testing.T) {
	root := t.TempDir()
	// Explicit escapes: \u00e9 = precomposed e-acute, e+\u0301 = NFD
	// (combining acute); never paste bare combining sequences.
	mkTree(t, root,
		tf{"u.txt", "pr\u00e9fix needle \u65e5\u672c\u8a9e\n"},
		tf{"nfd-e\u0301.txt", "needle in NFD-named file\n"})
	g := testTool(t, root)
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle \u65e5\u672c\u8a9e"}))
	wantText(t, got, "u.txt:1:pr\u00e9fix needle \u65e5\u672c\u8a9e")
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "NFD-named"}))
	wantText(t, got, "nfd-e\u0301.txt:1:needle in NFD-named file")
}

func TestContentLongLineOmitted(t *testing.T) {
	root := t.TempDir()
	// 500 content chars + newline = 501 bytes > 500: omitted by rg.
	mkTree(t, root, tf{"long.txt", strings.Repeat("y", 496) + "LONG\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "LONG"}))
	wantText(t, got, "long.txt:1:[Omitted long matching line]")
}

func TestContentLongLineBoundaryNotOmitted(t *testing.T) {
	root := t.TempDir()
	// 499 content chars + newline = 500 bytes: printed in full.
	line := strings.Repeat("y", 495) + "LONG"
	mkTree(t, root, tf{"edge.txt", line + "\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "LONG"}))
	wantText(t, got, "edge.txt:1:"+line)
}

func TestContentMultilineMode(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"ml.txt", "one A\ntwo B\nthree\n"})
	g := testTool(t, root)
	// Off by default: the dot cannot cross the newline.
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "A.two"}))
	wantText(t, got, "No matches found")
	// On: every line of the span prints as its own match line.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "A.two", "multiline": true}))
	wantText(t, got, "ml.txt:1:one A\nml.txt:2:two B")
}

func TestContentLeadingDashPattern(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"d.txt", "has -needle flag\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "-needle"}))
	wantText(t, got, "d.txt:1:has -needle flag")
}

func TestContentEmptyResult(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "nothing here\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle"}))
	wantText(t, got, "No matches found")
}

func TestGitignoreRespected(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{".git/config", "needle inside git\n"},
		tf{".gitignore", "secret.txt\n"},
		tf{"secret.txt", "needle secret\n"},
		tf{"kept.txt", "needle kept\n"},
		tf{".hidden.txt", "needle hidden\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle"}))
	// Gitignored files and the .git dir are excluded; hidden files are
	// searched (--hidden). Opposite of the sibling glob plugin's default.
	assert.NotContains(t, got, "secret")
	assert.NotContains(t, got, ".git/config")
	assert.True(t, containsLine(got, "kept.txt:1:needle kept"), got)
	assert.True(t, containsLine(got, ".hidden.txt:1:needle hidden"), got)
}

// A positive glob parameter acts as a ripgrep whitelist: gitignored
// files that directly match it are searched anyway (builtin parity —
// identical argv; verified empirically on rg 14.1.0).
func TestGlobParamWhitelistsGitignoredFiles(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{".git/keep", "x\n"},
		tf{".gitignore", "*.log\n"},
		tf{"app.log", "needle logged\n"},
		tf{"other.txt", "needle other\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "glob": "*.log"}))
	wantText(t, got, "app.log:1:needle logged")
}

func TestGlobFilter(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"t.js", "var needle = 1\n"},
		tf{"t.py", "needle = 2\n"},
		tf{"u.js", "let needle = 3\n"})
	g := testTool(t, root)

	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "glob": "*.js"}))
	assert.True(t, containsLine(got, "t.js:1:var needle = 1"), got)
	assert.True(t, containsLine(got, "u.js:1:let needle = 3"), got)
	assert.NotContains(t, got, "t.py")

	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "glob": "t.*"}))
	assert.True(t, containsLine(got, "t.js:1:var needle = 1"), got)
	assert.True(t, containsLine(got, "t.py:1:needle = 2"), got)
	assert.NotContains(t, got, "u.js")
}

// TestSlashGlobThroughSymlinkedRoot pins the symlink-resolution fix: rg
// roots its --glob matcher at the child's RESOLVED cwd but builds
// candidates from the search-path argv, so an unresolved (symlinked)
// argv made every slash-containing glob match nothing (macOS /var ->
// /private/var broke every t.TempDir() root this way). The tool must
// hand rg resolved paths and still display root-relative results.
func TestSlashGlobThroughSymlinkedRoot(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real")
	mkTree(t, real,
		tf{"src/x.ts", "needle here\n"},
		tf{"src/a/b/y.ts", "needle deep\n"},
		tf{"other/z.ts", "needle other\n"})
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(real, link))
	g := testTool(t, link)

	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "glob": "src/**"}))
	assert.True(t, containsLine(got, "src/x.ts:1:needle here"), got)
	assert.True(t, containsLine(got, "src/a/b/y.ts:1:needle deep"), got)
	assert.NotContains(t, got, "other/z.ts")
}

func TestGlobParamTokenization(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"a.go", "needle go\n"},
		tf{"b.ts", "needle ts\n"},
		tf{"c.txt", "needle txt\n"},
		tf{"d.rs", "needle rs\n"})
	g := testTool(t, root)

	// Comma-separated tokens split into individual globs.
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "glob": "*.go,*.ts"}))
	assert.True(t, containsLine(got, "a.go:1:needle go"), got)
	assert.True(t, containsLine(got, "b.ts:1:needle ts"), got)
	assert.NotContains(t, got, "c.txt")

	// Whitespace-separated tokens too.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "glob": "*.go *.rs"}))
	assert.True(t, containsLine(got, "a.go:1:needle go"), got)
	assert.True(t, containsLine(got, "d.rs:1:needle rs"), got)
	assert.NotContains(t, got, "b.ts")

	// Brace tokens stay whole (the comma inside braces is not a split).
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "glob": "*.{go,ts}"}))
	assert.True(t, containsLine(got, "a.go:1:needle go"), got)
	assert.True(t, containsLine(got, "b.ts:1:needle ts"), got)
	assert.NotContains(t, got, "d.rs")
}

func TestTokenizeGlobParamUnits(t *testing.T) {
	assert.Nil(t, tokenizeGlobParam(""))
	assert.Equal(t, []string{"*.go"}, tokenizeGlobParam("*.go"))
	assert.Equal(t, []string{"*.go", "*.ts"}, tokenizeGlobParam("*.go,*.ts"))
	assert.Equal(t, []string{"*.go", "*.ts"}, tokenizeGlobParam(" *.go \t *.ts "))
	assert.Equal(t, []string{"*.{go,ts}"}, tokenizeGlobParam("*.{go,ts}"))
	assert.Equal(t, []string{"*.{go,ts}", "*.rs"}, tokenizeGlobParam("*.{go,ts} *.rs"))
	assert.Equal(t, []string{"a", "b"}, tokenizeGlobParam("a,,b,"))
}

func TestPathAsDirectoryRelativeAndAbsolute(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"top.txt", "needle top\n"}, tf{"sub/inner.txt", "needle inner\n"})
	g := testTool(t, root)
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "path": "sub"}))
	wantText(t, got, "sub/inner.txt:1:needle inner")
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "path": root + "/sub"}))
	wantText(t, got, "sub/inner.txt:1:needle inner")
}

func TestPathOutsideRootStaysAbsolute(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mkTree(t, other, tf{"far.txt", "needle far\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "path": other}))
	wantText(t, got, other+"/far.txt:1:needle far")
}

func TestEmptyPathTreatedAsOmitted(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "needle\n"})
	got := grepOK(t, testTool(t, root), contentArgs(map[string]any{"pattern": "needle", "path": ""}))
	wantText(t, got, "a.txt:1:needle")
}

func TestUndefinedAndNullPathTreatedAsOmitted(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "needle\n"})
	g := testTool(t, root)
	for _, p := range []string{"undefined", "null"} {
		got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "path": p}))
		wantText(t, got, "a.txt:1:needle")
	}
}

func TestPathDoesNotExist(t *testing.T) {
	root := t.TempDir()
	got, isErr := runGrep(t, testTool(t, root), map[string]any{"pattern": "x", "path": "no-such"})
	require.True(t, isErr)
	wantText(t, got, fmt.Sprintf("Path does not exist: no-such. Note: your current working directory is %s.", root))
}

func TestPathDidYouMeanSuggestion(t *testing.T) {
	// EvalSymlinks so the suggester's realpath step cannot diverge on
	// hosts whose temp dir sits behind a symlink (e.g. macOS /var).
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	root := base + "/proj"
	mkTree(t, root, tf{"sub/file.txt", "x\n"})
	got, isErr := runGrep(t, testTool(t, root), map[string]any{"pattern": "x", "path": "../sub"})
	require.True(t, isErr)
	want := fmt.Sprintf("Path does not exist: ../sub. Note: your current working directory is %s. Did you mean %s?",
		root, root+"/sub")
	wantText(t, got, want)
}

func TestUNCishResolvedPathSkipsValidation(t *testing.T) {
	g := testTool(t, t.TempDir())
	msg, ok := g.validatePath(`\\host\share`, `\\host\share`)
	assert.True(t, ok, msg)
	msg, ok = g.validatePath("//host/share", "//host/share")
	assert.True(t, ok, msg)
}

func TestPathTildeExpansion(t *testing.T) {
	// Vq parity: "~" and "~/sub" expand to the home directory. Results
	// outside the root come back absolute.
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkTree(t, home, tf{"inhome.txt", "needle\n"}, tf{"nested/deep.txt", "needle\n"})
	g := testTool(t, root)

	got := grepOK(t, g, map[string]any{"pattern": "needle", "path": "~", "output_mode": "filenames"})
	wantText(t, got, "Found 2 files\n"+home+"/nested/deep.txt\n"+home+"/inhome.txt")

	got = grepOK(t, g, map[string]any{"pattern": "needle", "path": "~/nested", "output_mode": "filenames"})
	wantText(t, got, "Found 1 file\n"+home+"/nested/deep.txt")
}

func TestPathTildeUserNotExpanded(t *testing.T) {
	// The builtin's Vq only expanded "~" and "~/..."; "~user" resolves
	// as a literal name against the root.
	root := t.TempDir()
	got, isErr := runGrep(t, testTool(t, root), map[string]any{"pattern": "x", "path": "~nobody"})
	require.True(t, isErr)
	wantText(t, got, fmt.Sprintf("Path does not exist: ~nobody. Note: your current working directory is %s.", root))
}

func TestPathWhitespaceTrimmedBeforeResolve(t *testing.T) {
	// Vq trim() parity: "  sub  " only names a real directory after
	// trimming, and a whitespace-only path resolves to the root.
	root := t.TempDir()
	mkTree(t, root, tf{"sub/inner.txt", "needle\n"})
	g := testTool(t, root)
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "path": "  sub  "}))
	wantText(t, got, "sub/inner.txt:1:needle")
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "path": "   "}))
	wantText(t, got, "sub/inner.txt:1:needle")
}

func TestPathNullByteRejected(t *testing.T) {
	root := t.TempDir()
	got, isErr := runGrep(t, testTool(t, root), map[string]any{"pattern": "x", "path": "bad\x00path"})
	require.True(t, isErr)
	wantText(t, got, "Path contains null bytes")
}
