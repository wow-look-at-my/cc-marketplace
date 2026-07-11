package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFwmIsTheDefaultMode(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"old.txt", "a needle\n"}, tf{"new.txt", "b needle\n"})
	g := testTool(t, root)
	want := "Found 2 files\nnew.txt:\n  1:b needle\nold.txt:\n  1:a needle"
	got := grepOK(t, g, map[string]any{"pattern": "needle"})
	wantText(t, got, want)
	got = grepOK(t, g, map[string]any{"pattern": "needle", "output_mode": "filenames_with_matches"})
	wantText(t, got, want)
}

func TestFwmNewestFirstLinesAscending(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"oldest.txt", "1st needle\nx\n3rd needle\n"},
		tf{"middle.txt", "mid needle\n"},
		tf{"newest.txt", "new needle\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle"})
	want := "Found 3 files\n" +
		"newest.txt:\n  1:new needle\n" +
		"middle.txt:\n  1:mid needle\n" +
		"oldest.txt:\n  1:1st needle\n  3:3rd needle"
	wantText(t, got, want)
}

// Without context flags ripgrep prints no chunk separators, and neither
// does this mode: the 1/3 gap in oldest.txt above renders without "--".
// With a nonzero context width the separators appear.
func TestFwmContextAndSeparators(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"c.txt", "h1\nm needle\nh3\nz1\nz2\nm2 needle\n"})
	g := testTool(t, root)

	got := grepOK(t, g, map[string]any{"pattern": "needle", "-C": 1})
	want := "Found 1 file\n" +
		"c.txt:\n" +
		"  1-h1\n" +
		"  2:m needle\n" +
		"  3-h3\n" +
		"  --\n" +
		"  5-z2\n" +
		"  6:m2 needle"
	wantText(t, got, want)

	// -C 0 keeps the printer out of context mode: no separators.
	got = grepOK(t, g, map[string]any{"pattern": "needle", "-C": 0})
	wantText(t, got, "Found 1 file\nc.txt:\n  2:m needle\n  6:m2 needle")
}

func TestFwmContextPrecedence(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"p.txt", "a\nb\nc needle\nd\ne\n"})
	g := testTool(t, root)

	// context wins over -C: width 1, not 0.
	got := grepOK(t, g, map[string]any{"pattern": "needle", "context": 1, "-C": 0})
	wantText(t, got, "Found 1 file\np.txt:\n  2-b\n  3:c needle\n  4-d")

	// -C wins over -B/-A.
	got = grepOK(t, g, map[string]any{"pattern": "needle", "-C": 0, "-B": 2, "-A": 2})
	wantText(t, got, "Found 1 file\np.txt:\n  3:c needle")

	// -B/-A apply when alone.
	got = grepOK(t, g, map[string]any{"pattern": "needle", "-B": 1})
	wantText(t, got, "Found 1 file\np.txt:\n  2-b\n  3:c needle")
	got = grepOK(t, g, map[string]any{"pattern": "needle", "-A": 1})
	wantText(t, got, "Found 1 file\np.txt:\n  3:c needle\n  4-d")
}

func TestFwmNoLineNumbers(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"c.txt", "h1\nm needle\nh3\nz1\nz2\nm2 needle\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle", "-C": 1, "-n": false})
	want := "Found 1 file\n" +
		"c.txt:\n" +
		"  h1\n" +
		"  m needle\n" +
		"  h3\n" +
		"  --\n" +
		"  z2\n" +
		"  m2 needle"
	wantText(t, got, want)
}

func TestFwmCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "NEEDLE loud\n"})
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": "needle", "-i": true})
	wantText(t, got, "Found 1 file\na.txt:\n  1:NEEDLE loud")
	got = grepOK(t, g, map[string]any{"pattern": "needle"})
	wantText(t, got, "No files found")
}

func TestFwmWeirdFilenames(t *testing.T) {
	root := t.TempDir()
	// Creation order oldest-first; expected output order is the reverse.
	names := []string{
		"plain.txt",
		"with space.txt",
		"sq'uote.txt",
		`dq"uote.txt`,
		`back\slash.txt`,
		"-leading-dash.txt",
		"brack[et].txt",
		"brace{x}.txt",
		"star*name.txt",
		"we:ird.txt",
		"nfd-e\u0301.txt", // NFD: e + U+0301 combining acute accent
		"a/b/c/d/deep.txt",
		".hidden.txt",
	}
	files := make([]tf, len(names))
	for i, n := range names {
		files[i] = tf{n, "needle x\n"}
	}
	mkTree(t, root, files...)
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle"})
	parts := []string{fmt.Sprintf("Found %d files", len(names))}
	for i := len(names) - 1; i >= 0; i-- {
		parts = append(parts, names[i]+":", "  1:needle x")
	}
	wantText(t, got, strings.Join(parts, "\n"))
}

// The two-space indent keeps headers parseable even when a filename
// contains ":" and the content contains ":" and leading digits.
func TestFwmColonsEverywhereStayUnambiguous(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"12:34.txt", "5:6 needle here\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle"})
	wantText(t, got, "Found 1 file\n12:34.txt:\n  1:5:6 needle here")
}

func TestFwmMtimeTieBreaksByPathAscending(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"zz.txt", "needle\n"},
		tf{"aa.txt", "needle\n"},
		tf{"mm.txt", "needle\n"})
	// Same mtime for all three: order must fall back to ascending path.
	tie := time.Now().Add(-time.Hour)
	for _, n := range []string{"zz.txt", "aa.txt", "mm.txt"} {
		require.NoError(t, os.Chtimes(filepath.Join(root, n), tie, tie))
	}
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": "needle"})
	wantText(t, got, "Found 3 files\naa.txt:\n  1:needle\nmm.txt:\n  1:needle\nzz.txt:\n  1:needle")
	got = grepOK(t, g, map[string]any{"pattern": "needle", "output_mode": "filenames"})
	wantText(t, got, "Found 3 files\naa.txt\nmm.txt\nzz.txt")
}

func TestFwmMultilinePattern(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"ml.txt", "one A\ntwo B\nthree\n"})
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": "A.two"})
	wantText(t, got, "No files found")
	// The spanning match expands to consecutively numbered match lines
	// (no separator: the numbers are contiguous).
	got = grepOK(t, g, map[string]any{"pattern": "A.two", "multiline": true})
	wantText(t, got, "Found 1 file\nml.txt:\n  1:one A\n  2:two B")
}

func TestFwmLongLinesOmitted(t *testing.T) {
	root := t.TempDir()
	longCtx := strings.Repeat("y", 500) // +\n = 501 bytes: omitted
	mkTree(t, root,
		tf{"match.txt", strings.Repeat("x", 495) + "needle\n"}, // 501 chars +\n
		tf{"ctx.txt", longCtx + "\nz needle\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle", "-B": 1})
	want := "Found 2 files\n" +
		"ctx.txt:\n" +
		"  1-[Omitted long context line]\n" +
		"  2:z needle\n" +
		"match.txt:\n" +
		"  1:[Omitted long matching line]"
	wantText(t, got, want)
}

func TestFwmLongLineBoundaryKept(t *testing.T) {
	root := t.TempDir()
	line := strings.Repeat("y", 493) + "needle" // 499 chars +\n = 500: kept
	mkTree(t, root, tf{"edge.txt", line + "\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle"})
	wantText(t, got, "Found 1 file\nedge.txt:\n  1:"+line)
}

func TestFwmSingleFilePathKeepsHeader(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"solo.txt", "one needle\ntwo\nneedle three\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle", "path": "solo.txt"})
	// Unlike content mode (which drops the prefix for single files), the
	// JSON events always carry the path, so the header is present.
	wantText(t, got, "Found 1 file\nsolo.txt:\n  1:one needle\n  3:needle three")
}

func TestFwmExplicitBinaryFileRendersRawLines(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "blob.bin")
	require.NoError(t, os.WriteFile(p, []byte("bin\x00ary needle here\n"), 0o644))
	// Directory search: ripgrep skips binary files entirely.
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": "needle"})
	wantText(t, got, "No files found")
	// Explicit file target: the JSON events carry the real matched line
	// (content mode would print rg's "binary file matches" note instead;
	// documented divergence).
	got = grepOK(t, g, map[string]any{"pattern": "needle", "path": "blob.bin"})
	wantText(t, got, "Found 1 file\nblob.bin:\n  1:bin\x00ary needle here")
}

func TestFwmGitignoreRespected(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{".git/config", "needle git\n"},
		tf{".gitignore", "secret.txt\n"},
		tf{"secret.txt", "needle secret\n"},
		tf{"kept.txt", "needle kept\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle"})
	wantText(t, got, "Found 1 file\nkept.txt:\n  1:needle kept")
}

func TestFwmGlobAndTypeFilters(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"a.py", "needle py\n"},
		tf{"b.js", "needle js\n"})
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": "needle", "type": "js"})
	wantText(t, got, "Found 1 file\nb.js:\n  1:needle js")
	got = grepOK(t, g, map[string]any{"pattern": "needle", "glob": "*.py"})
	wantText(t, got, "Found 1 file\na.py:\n  1:needle py")
}

func TestFwmEmptyResult(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "nothing\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle"})
	wantText(t, got, "No files found")
}

// ---- filenames mode (the builtin's files_with_matches, verbatim) ----

func TestFilenamesBasicNewestFirst(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"oldest.txt", "needle\n"},
		tf{"middle.txt", "needle\n"},
		tf{"newest.txt", "needle\n"},
		tf{"nope.txt", "nothing\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle", "output_mode": "filenames"})
	wantText(t, got, "Found 3 files\nnewest.txt\nmiddle.txt\noldest.txt")
}

func TestFilenamesSingularHeader(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"only.txt", "needle\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle", "output_mode": "filenames"})
	wantText(t, got, "Found 1 file\nonly.txt")
}

func TestFilenamesEmpty(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "x\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle", "output_mode": "filenames"})
	wantText(t, got, "No files found")
}

func TestFilenamesWeirdNamesAndOutsideRootAbsolute(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mkTree(t, root, tf{"we:ird.txt", "needle\n"}, tf{"with space.txt", "needle\n"})
	mkTree(t, other, tf{"far.txt", "needle\n"})
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": "needle", "output_mode": "filenames"})
	wantText(t, got, "Found 2 files\nwith space.txt\nwe:ird.txt")
	got = grepOK(t, g, map[string]any{"pattern": "needle", "output_mode": "filenames", "path": other})
	wantText(t, got, "Found 1 file\n"+other+"/far.txt")
}

func TestFilenamesHiddenIncludedGitExcluded(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{".git/config", "needle\n"},
		tf{".hidden.txt", "needle\n"},
		tf{"seen.txt", "needle\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle", "output_mode": "filenames"})
	wantText(t, got, "Found 2 files\nseen.txt\n.hidden.txt")
}

// Files with equal mtimes and a failed-stat entry: rg lists them, the
// sorter scores unstattable paths as mtime 0 (sorting last).
func TestSortPathsByMtimeDescUnits(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"old.txt", "x\n"}, tf{"new.txt", "x\n"})
	gone := filepath.Join(root, "gone.txt")
	sorted := sortPathsByMtimeDesc([]string{
		gone,
		filepath.Join(root, "old.txt"),
		filepath.Join(root, "new.txt"),
	})
	assert.Equal(t, []string{
		filepath.Join(root, "new.txt"),
		filepath.Join(root, "old.txt"),
		gone,
	}, sorted)
}

// ---- fwm parsing units ----

func TestExpandEventLinesUnits(t *testing.T) {
	// Multi-line text (multiline match): consecutive numbers, all match.
	lines := expandEventLines("one\ntwo\n", 5, true)
	require.Len(t, lines, 2)
	assert.Equal(t, fwmLine{num: 5, text: "one", match: true}, lines[0])
	assert.Equal(t, fwmLine{num: 6, text: "two", match: true}, lines[1])

	// No trailing newline (EOF).
	lines = expandEventLines("tail", 9, false)
	require.Len(t, lines, 1)
	assert.Equal(t, fwmLine{num: 9, text: "tail", match: false}, lines[0])

	// CRLF terminators are stripped from the display text but count
	// toward the omission size.
	lines = expandEventLines("win\r\n", 1, true)
	require.Len(t, lines, 1)
	assert.Equal(t, "win", lines[0].text)

	// A 500-byte line + newline is over the limit; 499 + newline is not.
	lines = expandEventLines(strings.Repeat("a", 500)+"\n", 1, true)
	assert.True(t, lines[0].overLong)
	lines = expandEventLines(strings.Repeat("a", 499)+"\n", 1, true)
	assert.False(t, lines[0].overLong)
	// Without a terminator the raw length alone decides.
	lines = expandEventLines(strings.Repeat("a", 501), 1, true)
	assert.True(t, lines[0].overLong)
	lines = expandEventLines(strings.Repeat("a", 500), 1, true)
	assert.False(t, lines[0].overLong)

	assert.Empty(t, expandEventLines("", 1, true))
}

func TestParseFwmEventsUnits(t *testing.T) {
	groups := parseFwmEvents([]string{
		`{"type":"begin","data":{"path":{"text":"/r/a"}}}`,
		`{"type":"match","data":{"path":{"text":"/r/a"},"lines":{"text":"hit\n"},"line_number":3}}`,
		`{"type":"context","data":{"path":{"text":"/r/a"},"lines":{"text":"ctx\n"},"line_number":4}}`,
		`{"type":"end","data":{"path":{"text":"/r/a"},"binary_offset":null}}`,
		`{"type":"match","data":{"path":{"text":"/r/b"},"lines":{"text":"other\n"},"line_number":1}}`,
		`{"type":"summary","data":{}}`,
		`{"type":"match","data":{"path":{"text":"/r/nope"},"lines":{"text":"x\n"}}}`, // no line_number: skipped
		`{truncated garbage`, // unparseable: skipped
	})
	require.Len(t, groups, 2)
	assert.Equal(t, "/r/a", groups[0].path)
	require.Len(t, groups[0].lines, 2)
	assert.Equal(t, fwmLine{num: 3, text: "hit", match: true}, groups[0].lines[0])
	assert.Equal(t, fwmLine{num: 4, text: "ctx", match: false}, groups[0].lines[1])
	assert.Equal(t, "/r/b", groups[1].path)
}

func TestRgJSONTextBytesFallback(t *testing.T) {
	txt := "hello"
	b64 := "aGVsbG8=" // base64("hello")
	assert.Equal(t, "hello", rgJSONText{Text: &txt}.value())
	assert.Equal(t, "hello", rgJSONText{Bytes: &b64}.value())
	bad := "!!!not-base64"
	assert.Equal(t, "", rgJSONText{Bytes: &bad}.value())
	assert.Equal(t, "", rgJSONText{}.value())
}

func TestContextSeparatorsEnabledUnits(t *testing.T) {
	num := func(f float64) *float64 { return &f }
	assert.False(t, contextSeparatorsEnabled(&grepArgs{}))
	assert.True(t, contextSeparatorsEnabled(&grepArgs{context: num(1)}))
	assert.False(t, contextSeparatorsEnabled(&grepArgs{context: num(0), dashC: num(5)}))
	assert.True(t, contextSeparatorsEnabled(&grepArgs{dashC: num(2)}))
	assert.False(t, contextSeparatorsEnabled(&grepArgs{dashC: num(0), before: num(5)}))
	assert.True(t, contextSeparatorsEnabled(&grepArgs{before: num(1)}))
	assert.True(t, contextSeparatorsEnabled(&grepArgs{after: num(1)}))
	assert.False(t, contextSeparatorsEnabled(&grepArgs{before: num(0), after: num(0)}))
}
