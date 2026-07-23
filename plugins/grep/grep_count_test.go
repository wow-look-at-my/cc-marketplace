package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func countArgs(kv map[string]any) map[string]any {
	args := map[string]any{"output_mode": "count"}
	for k, v := range kv {
		args[k] = v
	}
	return args
}

func TestCountSingleFileKeepsFilenamePrefix(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "needle one\nplain\nneedle two\n"})
	// The -H fix: without it a single-file target produced a bare "2"
	// the parser scored as 0 files / 0 matches (the 2.1.116 bug).
	got := grepOK(t, testTool(t, root), countArgs(map[string]any{"pattern": "needle", "path": "a.txt"}))
	wantText(t, got, "a.txt:2\n\nFound 2 total occurrences across 1 file.")
}

func TestCountDirectorySingleMatch(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "needle\n"})
	got := grepOK(t, testTool(t, root), countArgs(map[string]any{"pattern": "needle"}))
	wantText(t, got, "a.txt:1\n\nFound 1 total occurrence across 1 file.")
}

func TestCountMultipleFiles(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"a.txt", "needle\nneedle\n"},
		tf{"b.txt", "needle\n"},
		tf{"c.txt", "none\n"})
	got := grepOK(t, testTool(t, root), countArgs(map[string]any{"pattern": "needle"}))
	// rg's cross-file output order is nondeterministic: assert the lines
	// and the exact trailer instead of a full golden.
	assert.True(t, containsLine(got, "a.txt:2"), got)
	assert.True(t, containsLine(got, "b.txt:1"), got)
	assert.NotContains(t, got, "c.txt")
	assert.True(t, strings.HasSuffix(got, "\n\nFound 3 total occurrences across 2 files."), got)
}

func TestCountEmpty(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "nothing\n"})
	got := grepOK(t, testTool(t, root), countArgs(map[string]any{"pattern": "needle"}))
	wantText(t, got, "No matches found\n\nFound 0 total occurrences across 0 files.")
}

func TestCountWeirdFilenameWithColon(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"we:ird.txt", "needle\nneedle\n"})
	// The LAST-colon split keeps the whole filename (colons included).
	got := grepOK(t, testTool(t, root), countArgs(map[string]any{"pattern": "needle"}))
	wantText(t, got, "we:ird.txt:2\n\nFound 2 total occurrences across 1 file.")
}

func TestCountCaseInsensitiveAndFilters(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.js", "NEEDLE\n"}, tf{"b.py", "needle\n"})
	g := testTool(t, root)
	got := grepOK(t, g, countArgs(map[string]any{"pattern": "needle", "-i": true, "glob": "*.js"}))
	wantText(t, got, "a.js:1\n\nFound 1 total occurrence across 1 file.")
	got = grepOK(t, g, countArgs(map[string]any{"pattern": "needle", "glob": "*.py"}))
	wantText(t, got, "b.py:1\n\nFound 1 total occurrence across 1 file.")
}

func TestCountMultilineSpan(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"ml.txt", "one A\ntwo B\n"})
	got := grepOK(t, testTool(t, root), countArgs(map[string]any{"pattern": "A.two", "multiline": true}))
	wantText(t, got, "ml.txt:1\n\nFound 1 total occurrence across 1 file.")
}

// Pagination goldens use a fake rg so the count-line order is fixed.
func TestCountPagination(t *testing.T) {
	root := t.TempDir()
	fake := writeFakeRg(t, `printf 'a:1\nb:2\nc:3\n'`)
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)

	got := grepOK(t, g, countArgs(map[string]any{"pattern": "x", "head_limit": 2}))
	wantText(t, got, "a:1\nb:2\n\nFound 3 total occurrences across 2 files. with pagination = limit: 2")

	// Q46 quirk: the limit is only "applied" when items existed beyond
	// the window (3 - 1 is not > 2), so only the offset is reported.
	got = grepOK(t, g, countArgs(map[string]any{"pattern": "x", "head_limit": 2, "offset": 1}))
	wantText(t, got, "b:2\nc:3\n\nFound 5 total occurrences across 2 files. with pagination = offset: 1")

	// Exactly at the limit: no pagination note.
	got = grepOK(t, g, countArgs(map[string]any{"pattern": "x", "head_limit": 3}))
	wantText(t, got, "a:1\nb:2\nc:3\n\nFound 6 total occurrences across 3 files.")

	// 0 = unlimited.
	got = grepOK(t, g, countArgs(map[string]any{"pattern": "x", "head_limit": 0}))
	wantText(t, got, "a:1\nb:2\nc:3\n\nFound 6 total occurrences across 3 files.")

	// Offset alone, no truncation: offset note only.
	got = grepOK(t, g, countArgs(map[string]any{"pattern": "x", "offset": 2}))
	wantText(t, got, "c:3\n\nFound 3 total occurrences across 1 file. with pagination = offset: 2")

	// Offset past the end: empty body, zero totals, offset note kept.
	got = grepOK(t, g, countArgs(map[string]any{"pattern": "x", "offset": 9}))
	wantText(t, got, "No matches found\n\nFound 0 total occurrences across 0 files. with pagination = offset: 9")
}

// Unparseable count lines stay in the body but do not contribute to the
// totals (the builtin's parseInt-based counting).
func TestCountUnparseableLinesSkipped(t *testing.T) {
	root := t.TempDir()
	fake := writeFakeRg(t, `printf 'plainline\nx:notanum\na:2\n'`)
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)
	got := grepOK(t, g, countArgs(map[string]any{"pattern": "x"}))
	wantText(t, got, "plainline\nx:notanum\na:2\n\nFound 2 total occurrences across 1 file.")
}

func TestJSParseIntUnits(t *testing.T) {
	cases := []struct {
		in string
		n  int
		ok bool
	}{
		{"3", 3, true},
		{"  42", 42, true},
		{"7trailing", 7, true},
		{"-5", -5, true},
		{"+9", 9, true},
		{"", 0, false},
		{"abc", 0, false},
		{"-", 0, false},
	}
	for _, tc := range cases {
		n, ok := jsParseInt(tc.in)
		assert.Equal(t, tc.ok, ok, tc.in)
		if tc.ok {
			assert.Equal(t, tc.n, n, tc.in)
		}
	}
}
