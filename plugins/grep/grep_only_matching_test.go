package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnlyMatchingContent(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "x foo1 y foo22\nplain\nfoo333\n"})
	g := testTool(t, root)
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": `foo\d+`, "-o": true}))
	wantText(t, got, "a.txt:1:foo1\na.txt:1:foo22\na.txt:3:foo333")
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": `foo\d+`, "-o": true, "-n": false}))
	wantText(t, got, "a.txt:foo1\na.txt:foo22\na.txt:foo333")
}

func TestOnlyMatchingCountCountsMatches(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "foo foo foo\nfoo\n"})
	g := testTool(t, root)
	wantText(t, grepOK(t, g, countArgs(map[string]any{"pattern": "foo"})),
		"a.txt:2\n\nFound 2 total occurrences across 1 file.")
	wantText(t, grepOK(t, g, countArgs(map[string]any{"pattern": "foo", "-o": true})),
		"a.txt:4\n\nFound 4 total occurrences across 1 file.")
}

func TestOnlyMatchingFwm(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "x foo1 y foo22\nplain\nfoo333\n"})
	g := testTool(t, root)
	got := grepOK(t, g, map[string]any{"pattern": `foo\d+`, "-o": true})
	wantText(t, got, "Found 1 file\na.txt:\n  1:foo1\n  1:foo22\n  3:foo333")
	got = grepOK(t, g, map[string]any{"pattern": `foo\d+`, "-o": true, "-n": false})
	wantText(t, got, "Found 1 file\na.txt:\n  foo1\n  foo22\n  foo333")
}

// Context lines stay whole, as rg -o -C prints them. Matches on one line do not count as a gap.
func TestOnlyMatchingFwmWithContext(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "a\nfoo bar foo\nb\nc\nzz foo\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "foo", "-o": true, "-C": 1})
	wantText(t, got, "Found 1 file\na.txt:\n  1-a\n  2:foo\n  2:foo\n  3-b\n  4-c\n  5:foo")
}

// A multiline match splits into numbered lines, as rg -o -U prints it.
func TestOnlyMatchingFwmMultiline(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "a\nfoo\nbar\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": `oo\nba`, "-o": true, "multiline": true})
	wantText(t, got, "Found 1 file\na.txt:\n  2:oo\n  3:ba")
}

// head_limit counts each match as one line.
func TestOnlyMatchingFwmPagination(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "foo foo foo\n"})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "foo", "-o": true, "head_limit": 2})
	wantText(t, got, "Found 1 file limit: 2\na.txt:\n  1:foo\n  1:foo")
}

func TestParseFwmEventsOnlyMatch(t *testing.T) {
	groups := parseFwmEvents([]string{
		`{"type":"match","data":{"path":{"text":"/r/a"},"lines":{"text":"ab\ncd foo\n"},"line_number":7,"submatches":[{"start":1,"end":4},{"start":6,"end":9}]}}`,
		`{"type":"match","data":{"path":{"text":"/r/a"},"lines":{"text":"x\n"},"line_number":9,"submatches":[{"start":0,"end":99}]}}`, // out of range: dropped
	}, true)
	require.Len(t, groups, 1)
	assert.Equal(t, []fwmLine{
		{num: 7, text: "b", match: true, matchCol: -1},
		{num: 8, text: "c", match: true, matchCol: -1},
		{num: 8, text: "foo", match: true, matchCol: -1},
	}, groups[0].lines)
}
