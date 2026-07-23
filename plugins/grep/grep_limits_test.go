package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func numberedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "needle %d\n", i)
	}
	return b.String()
}

func TestContentPagination(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"n.txt", numberedLines(4)})
	g := testTool(t, root)

	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "head_limit": 2}))
	wantText(t, got, "n.txt:1:needle 1\nn.txt:2:needle 2\n\n[Showing results with pagination = limit: 2]")

	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "head_limit": 2, "offset": 1}))
	wantText(t, got, "n.txt:2:needle 2\nn.txt:3:needle 3\n\n[Showing results with pagination = limit: 2, offset: 1]")

	// Exactly at the limit: no note.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "head_limit": 4}))
	wantText(t, got, "n.txt:1:needle 1\nn.txt:2:needle 2\nn.txt:3:needle 3\nn.txt:4:needle 4")

	// 0 = unlimited; with an offset only the offset is reported.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "head_limit": 0}))
	wantText(t, got, "n.txt:1:needle 1\nn.txt:2:needle 2\nn.txt:3:needle 3\nn.txt:4:needle 4")
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "head_limit": 0, "offset": 3}))
	wantText(t, got, "n.txt:4:needle 4\n\n[Showing results with pagination = offset: 3]")

	// Offset past the end: the empty body still carries the note.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "offset": 9}))
	wantText(t, got, "No matches found\n\n[Showing results with pagination = offset: 9]")

	// Fractional head_limit: JS slice truncation, verbatim in the note.
	got = grepOK(t, g, contentArgs(map[string]any{"pattern": "needle", "head_limit": 2.5}))
	wantText(t, got, "n.txt:1:needle 1\nn.txt:2:needle 2\n\n[Showing results with pagination = limit: 2.5]")
}

func TestContentDefaultHeadLimit250(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"big.txt", numberedLines(251)})
	g := testTool(t, root)
	g.persistThreshold = 1 << 20 // keep the pagination text inline
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle"}))
	lines := strings.Split(got, "\n")
	require.Equal(t, 250+2, len(lines)) // 250 results + blank + note
	assert.Equal(t, "big.txt:1:needle 1", lines[0])
	assert.Equal(t, "big.txt:250:needle 250", lines[249])
	assert.Equal(t, "[Showing results with pagination = limit: 250]", lines[251])
	assert.NotContains(t, got, "needle 251")
}

func TestFwmPagination(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"b.txt", "needle b1\nneedle b2\n"},
		tf{"a.txt", "needle a1\nneedle a2\nneedle a3\n"}) // newest
	g := testTool(t, root)

	// Limit cuts mid-file: file b keeps only its first line.
	got := grepOK(t, g, map[string]any{"pattern": "needle", "head_limit": 4})
	wantText(t, got, "Found 2 files limit: 4\na.txt:\n  1:needle a1\n  2:needle a2\n  3:needle a3\nb.txt:\n  1:needle b1")

	// A file whose lines are entirely cut is omitted.
	got = grepOK(t, g, map[string]any{"pattern": "needle", "head_limit": 3})
	wantText(t, got, "Found 1 file limit: 3\na.txt:\n  1:needle a1\n  2:needle a2\n  3:needle a3")

	// Offset skips into the first file; headers are not counted.
	got = grepOK(t, g, map[string]any{"pattern": "needle", "offset": 1})
	wantText(t, got, "Found 2 files offset: 1\na.txt:\n  2:needle a2\n  3:needle a3\nb.txt:\n  1:needle b1\n  2:needle b2")

	got = grepOK(t, g, map[string]any{"pattern": "needle", "head_limit": 2, "offset": 1})
	wantText(t, got, "Found 1 file limit: 2, offset: 1\na.txt:\n  2:needle a2\n  3:needle a3")

	// Exactly all lines: no note. 0 = unlimited.
	want := "Found 2 files\na.txt:\n  1:needle a1\n  2:needle a2\n  3:needle a3\nb.txt:\n  1:needle b1\n  2:needle b2"
	got = grepOK(t, g, map[string]any{"pattern": "needle", "head_limit": 5})
	wantText(t, got, want)
	got = grepOK(t, g, map[string]any{"pattern": "needle", "head_limit": 0})
	wantText(t, got, want)

	// Offset past the end.
	got = grepOK(t, g, map[string]any{"pattern": "needle", "offset": 99})
	wantText(t, got, "No files found")
}

func TestFwmDefaultHeadLimit250(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"big.txt", numberedLines(251)})
	g := testTool(t, root)
	g.persistThreshold = 1 << 20
	got := grepOK(t, g, map[string]any{"pattern": "needle"})
	lines := strings.Split(got, "\n")
	require.Equal(t, 1+1+250, len(lines)) // Found + header + 250 lines
	assert.Equal(t, "Found 1 file limit: 250", lines[0])
	assert.Equal(t, "big.txt:", lines[1])
	assert.Equal(t, "  250:needle 250", lines[251])
	assert.NotContains(t, got, "needle 251")
}

func TestFilenamesPagination(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root,
		tf{"f1.txt", "needle\n"},
		tf{"f2.txt", "needle\n"},
		tf{"f3.txt", "needle\n"}) // newest
	g := testTool(t, root)
	fn := func(kv map[string]any) map[string]any {
		kv["output_mode"] = "filenames"
		return kv
	}

	got := grepOK(t, g, fn(map[string]any{"pattern": "needle", "head_limit": 2}))
	wantText(t, got, "Found 2 files limit: 2\nf3.txt\nf2.txt")

	// Q46 quirk: 3 - 1 is not > 2, so the limit is not reported as
	// applied; only the offset appears.
	got = grepOK(t, g, fn(map[string]any{"pattern": "needle", "head_limit": 2, "offset": 1}))
	wantText(t, got, "Found 2 files offset: 1\nf2.txt\nf1.txt")

	got = grepOK(t, g, fn(map[string]any{"pattern": "needle", "head_limit": 3}))
	wantText(t, got, "Found 3 files\nf3.txt\nf2.txt\nf1.txt")

	got = grepOK(t, g, fn(map[string]any{"pattern": "needle", "head_limit": 0, "offset": 2}))
	wantText(t, got, "Found 1 file offset: 2\nf1.txt")

	// Offset past the end: bare "No files found", no note (parity with
	// the builtin's numFiles===0 early return).
	got = grepOK(t, g, fn(map[string]any{"pattern": "needle", "offset": 9}))
	wantText(t, got, "No files found")
}

// persistedPathRe lives in persist_test.go (shared with its unit tests).

// TestPersistEndToEnd crosses the real 20000-char threshold in content
// mode: the tool must return a <persisted-output> block whose saved file
// holds the full formatted text (250 default-limited lines + pagination
// note) and whose preview matches the split rules.
func TestPersistEndToEnd(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("z", 90)
	var b strings.Builder
	for i := 1; i <= 251; i++ {
		fmt.Fprintf(&b, "needle %03d %s\n", i, long)
	}
	mkTree(t, root, tf{"huge.txt", b.String()})
	g := testTool(t, root)
	got := grepOK(t, g, contentArgs(map[string]any{"pattern": "needle"}))

	require.True(t, strings.HasPrefix(got, persistedOutputOpen+"\n"), got[:80])
	require.True(t, strings.HasSuffix(got, persistedOutputClose))

	m := persistedPathRe.FindStringSubmatch(got)
	require.NotNil(t, m)
	saved, err := os.ReadFile(m[1])
	require.NoError(t, err)
	text := string(saved)
	assert.True(t, strings.HasPrefix(text, "huge.txt:1:needle 001 "+long))
	assert.True(t, strings.HasSuffix(text, "[Showing results with pagination = limit: 250]"))
	assert.Equal(t, 250+2, len(strings.Split(text, "\n")))

	preview, hasMore := splitPreview(text, persistPreviewChars)
	require.True(t, hasMore)
	want := persistedOutputOpen + "\n" +
		fmt.Sprintf("Output too large (%s). Full output saved to: %s\n\n", humanSize(utf16Len(text)), m[1]) +
		"Preview (first 2KB):\n" +
		preview + "\n...\n" +
		persistedOutputClose
	assert.Equal(t, want, got)
}

func TestPersistFwmEndToEnd(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 220; i++ {
		fmt.Fprintf(&b, "needle %03d %s\n", i, strings.Repeat("q", 100))
	}
	mkTree(t, root, tf{"big.txt", b.String()})
	got := grepOK(t, testTool(t, root), map[string]any{"pattern": "needle"})
	require.True(t, strings.HasPrefix(got, persistedOutputOpen))
	m := persistedPathRe.FindStringSubmatch(got)
	require.NotNil(t, m)
	saved, err := os.ReadFile(m[1])
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(saved), "Found 1 file\nbig.txt:\n  1:needle 001"))
}

func TestInlineJustUnderPersistThreshold(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "needle\n"})
	g := testTool(t, root)
	g.persistThreshold = utf16Len("Found 1 file\na.txt:\n  1:needle") // exactly at threshold
	got := grepOK(t, g, map[string]any{"pattern": "needle"})
	wantText(t, got, "Found 1 file\na.txt:\n  1:needle")
}

func TestInvalidRegexIsErrorWithRgStderr(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "x\n"})
	for _, mode := range []string{"content", "filenames_with_matches", "filenames", "count"} {
		got, isErr := runGrep(t, testTool(t, root), map[string]any{"pattern": "a(", "output_mode": mode})
		require.True(t, isErr, "mode %s: %s", mode, got)
		assert.Contains(t, got, "regex parse error", mode)
		assert.Contains(t, got, "unclosed group", mode)
	}
}

func TestInvalidGlobIsError(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"a.txt", "x\n"})
	g := testTool(t, root)
	got, isErr := runGrep(t, g, map[string]any{"pattern": "x", "glob": "{unclosed"})
	require.True(t, isErr)
	assert.Contains(t, got, "glob")
}

func TestTimeoutThroughTool(t *testing.T) {
	fake := writeFakeRg(t, "exec sleep 5")
	g := testTool(t, t.TempDir())
	g.resolveRg = fixedRg(fake)
	g.timeout = 150 * time.Millisecond
	got, isErr := runGrep(t, g, map[string]any{"pattern": "x"})
	require.True(t, isErr)
	wantText(t, got, "Ripgrep search timed out after 20 seconds. The search may have matched files but did not complete in time. Try searching a more specific path or pattern.")
}

func TestTimeoutPartialThroughTool(t *testing.T) {
	root := t.TempDir()
	// Fake rg emits two content lines then hangs: the tool must resolve
	// the first (last line dropped) through content formatting.
	fake := writeFakeRg(t, fmt.Sprintf("printf '%s/kept.txt:1:hit\\n%s/dropped.txt:9:gone\\n'; exec sleep 5", root, root))
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)
	g.timeout = 300 * time.Millisecond
	got, isErr := runGrep(t, g, contentArgs(map[string]any{"pattern": "hit"}))
	require.False(t, isErr)
	wantText(t, got, "kept.txt:1:hit")
}

func TestEAGAINRetryThroughTool(t *testing.T) {
	root := t.TempDir()
	fake := writeFakeRg(t, fmt.Sprintf(`if [ "$1" = "-j" ]; then printf '%s/ok.txt:1:hit\n'; else echo 'rg: Resource temporarily unavailable (os error 11)' >&2; exit 2; fi`, root))
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)
	got, isErr := runGrep(t, g, contentArgs(map[string]any{"pattern": "hit"}))
	require.False(t, isErr)
	wantText(t, got, "ok.txt:1:hit")
}

func TestRipgrepMissingThroughTool(t *testing.T) {
	g := testTool(t, t.TempDir())
	g.resolveRg = resolveRipgrep
	t.Setenv("PATH", t.TempDir())
	got, isErr := runGrep(t, g, map[string]any{"pattern": "x"})
	require.True(t, isErr)
	wantText(t, got, ripgrepNotFoundMsg)
}

func TestNewGrepToolDefaults(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "/tmp/some-project")
	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "")
	g := newGrepTool(discardLogf)
	assert.Equal(t, "/tmp/some-project", g.root)
	assert.Equal(t, 20000, g.persistThreshold)
	assert.Equal(t, 20000000, g.maxOutput)
	assert.NotNil(t, g.resolveRg)

	t.Setenv("CLAUDE_PROJECT_DIR", "")
	g = newGrepTool(discardLogf)
	wd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, wd, g.root)
}

func TestRelativizePathQuirks(t *testing.T) {
	assert.Equal(t, "sub/f.txt", relativizePath("/root/sub/f.txt", "/root"))
	assert.Equal(t, "/other/f.txt", relativizePath("/other/f.txt", "/root"))
	// Faithful quirk: a sibling name beginning with ".." falls back to
	// the absolute path even though it is under the root.
	assert.Equal(t, "/root/..foo", relativizePath("/root/..foo", "/root"))
	// Non-path content (separator lines, bare line numbers) unchanged.
	assert.Equal(t, "--", relativizePath("--", "/root"))
	assert.Equal(t, "12", relativizePath("12", "/root"))
}

func TestResolveAgainst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct{ in, want string }{
		{"/abs/dir", "/abs/dir"},
		{"rel/dir", "/root/rel/dir"},
		{"  rel/dir  ", "/root/rel/dir"}, // Vq trims before resolving
		{"   ", "/root"},                 // whitespace-only -> root
		{"undefined", "/root"},           // model no-path literal -> root
		{"null", "/root"},                // model no-path literal -> root
		{"~", home},
		{"~/", home},
		{"~/sub/x", home + "/sub/x"},
		{"~user", "/root/~user"}, // "~user" is NOT expanded (Vq parity)
	}
	for _, tc := range cases {
		got, err := resolveAgainst(tc.in, "/root")
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "input %q", tc.in)
	}

	got, err := resolveAgainst("../x", "/root/sub")
	require.NoError(t, err)
	assert.Equal(t, "/root/x", got)

	_, err = resolveAgainst("bad\x00path", "/root")
	require.EqualError(t, err, "Path contains null bytes")

	// Without a resolvable home, "~" stays literal (documented
	// divergence: the builtin's os.homedir() cannot fail on POSIX).
	t.Setenv("HOME", "")
	got, err = resolveAgainst("~", "/root")
	require.NoError(t, err)
	assert.Equal(t, "/root/~", got)
}
