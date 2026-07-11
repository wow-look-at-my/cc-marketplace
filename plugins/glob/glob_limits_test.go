package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealCapTruncationAndPersistence exercises the production constants
// end-to-end: 25010 files overflow the real 25000-file cap, and the
// resulting ~350KB text overflows the real 50000-char persistence
// threshold, so the tool must return a <persisted-output> block whose
// saved file ends with the verbatim truncation line after exactly 25000
// paths.
func TestRealCapTruncationAndPersistence(t *testing.T) {
	root := t.TempDir()
	const total = globMaxResults + 10
	for i := 0; i < total; i++ {
		name := filepath.Join(root, fmt.Sprintf("f%05d.txt", i))
		require.NoError(t, os.WriteFile(name, nil, 0o644))

	}
	g := testTool(t, root)
	got, isErr := runGlob(t, g, "*.txt")
	require.False(t, isErr)

	require.False(t, !strings.HasPrefix(got, persistedOutputOpen+"\n") || !strings.HasSuffix(got, persistedOutputClose))

	m := persistedPathRe.FindStringSubmatch(got)
	require.NotNil(t, m)

	saved, err := os.ReadFile(m[1])
	require.Nil(t, err)

	lines := strings.Split(string(saved), "\n")
	require.Equal(t, globMaxResults+1, len(lines))

	assert.Equal(t, globTruncationLine, lines[len(lines)-1])

	seen := make(map[string]bool, globMaxResults)
	for _, l := range lines[:globMaxResults] {
		require.False(t, !strings.HasPrefix(l, "f") || !strings.HasSuffix(l, ".txt"))

		require.False(t, seen[l])

		seen[l] = true
	}
}

// TestPersistenceThroughToolAtRealThreshold crosses the real 50000-char
// ceiling with ~3100 files while staying under the file cap: truncation
// must NOT fire, persistence must.
func TestPersistenceThroughToolAtRealThreshold(t *testing.T) {
	root := t.TempDir()
	const total = 3100 // 3100 * (24-char name + newline) ~= 77.5K chars > 50000
	for i := 0; i < total; i++ {
		name := filepath.Join(root, fmt.Sprintf("padded-filename-%04d.txt", i))
		require.NoError(t, os.WriteFile(name, nil, 0o644))

	}
	g := testTool(t, root)
	got, isErr := runGlob(t, g, "*.txt")
	require.False(t, isErr)

	require.True(t, strings.HasPrefix(got, persistedOutputOpen))

	assert.NotContains(t, got, globTruncationLine)

	m := persistedPathRe.FindStringSubmatch(got)
	require.NotNil(t, m)

	saved, err := os.ReadFile(m[1])
	require.Nil(t, err)

	n := len(strings.Split(string(saved), "\n"))
	assert.Equal(t, total, n)

}

func TestInlineJustUnderPersistThreshold(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "a.txt", "b.txt")
	g := testTool(t, root)
	g.persistThreshold = utf16Len("a.txt\nb.txt") // exactly at threshold: inline
	got, _ := runGlob(t, g, "*.txt")
	wantText(t, got, "a.txt\nb.txt")
}

func TestTimeoutThroughTool(t *testing.T) {
	root := t.TempDir()
	fake := writeFakeRg(t, "sleep 5")
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)
	g.timeout = 150 * time.Millisecond
	got, isErr := runGlob(t, g, "*")
	require.True(t, isErr)

	wantText(t, got, "Ripgrep search timed out after 20 seconds. The search may have matched files but did not complete in time. Try searching a more specific path or pattern.")
}

func TestTimeoutPartialThroughTool(t *testing.T) {
	root := t.TempDir()
	// Fake rg emits two absolute paths then hangs: the tool must resolve
	// the first (last line dropped) relativized against the root.
	fake := writeFakeRg(t, fmt.Sprintf("printf '%s/kept.txt\\n%s/dropped.txt\\n'; sleep 5", root, root))
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)
	g.timeout = 300 * time.Millisecond
	got, isErr := runGlob(t, g, "*")
	require.False(t, isErr)

	wantText(t, got, "kept.txt")
}

func TestEAGAINRetryThroughTool(t *testing.T) {
	root := t.TempDir()
	fake := writeFakeRg(t, fmt.Sprintf(`if [ "$1" = "-j" ]; then printf '%s/ok.txt\n'; else echo 'rg: Resource temporarily unavailable (os error 11)' >&2; exit 2; fi`, root))
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)
	got, isErr := runGlob(t, g, "*")
	require.False(t, isErr)

	wantText(t, got, "ok.txt")
}

func TestRipgrepMissingThroughTool(t *testing.T) {
	g := testTool(t, t.TempDir())
	g.resolveRg = resolveRipgrep
	t.Setenv("PATH", t.TempDir())
	got, isErr := runGlob(t, g, "*")
	require.True(t, isErr)

	wantText(t, got, ripgrepNotFoundMsg)
}

func TestRelativizePathQuirks(t *testing.T) {
	got := relativizePath("/root/sub/f.txt", "/root")
	assert.Equal(t, "sub/f.txt", got)

	got = relativizePath("/other/f.txt", "/root")
	assert.Equal(t, "/other/f.txt", got)

	// Faithful quirk: a sibling name beginning with ".." falls back to
	// the absolute path even though it is under the root.
	got = relativizePath("/root/..foo", "/root")
	assert.Equal(t, "/root/..foo", got)

}

func TestResolveAgainst(t *testing.T) {
	got := resolveAgainst("/abs/dir", "/root")
	assert.Equal(t, "/abs/dir", got)

	got = resolveAgainst("rel/dir", "/root")
	assert.Equal(t, "/root/rel/dir", got)

	got = resolveAgainst("../x", "/root/sub")
	assert.Equal(t, "/root/x", got)

}

func TestNewGlobToolDefaults(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "/tmp/some-project")
	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "")
	g := newGlobTool(discardLogf)
	assert.Equal(t, "/tmp/some-project", g.root)

	assert.False(t, g.maxResults != 25000 || g.persistThreshold != 50000 || g.maxOutput != 20000000)

	t.Setenv("CLAUDE_PROJECT_DIR", "")
	g = newGlobTool(discardLogf)
	wd, _ := os.Getwd()
	assert.Equal(t, wd, g.root)

}

func TestUNCishResolvedPathSkipsValidation(t *testing.T) {
	// The builtin skips stat validation when the RESOLVED path starts
	// with \\ or // (Windows UNC shapes). On POSIX, resolution collapses
	// a leading // (both Node and Go), so the branch is only reachable
	// with a backslash form — test it at the validateDir layer.
	g := testTool(t, t.TempDir())
	msg, ok := g.validateDir(`\\host\share`, `\\host\share`)
	assert.True(t, ok, msg)

	msg, ok = g.validateDir("//host/share", "//host/share")
	assert.True(t, ok, msg)

}

func TestEnvTruthyDefault(t *testing.T) {
	const k = "GLOB_TEST_TRUTHY"
	cases := []struct {
		val  string
		want bool
	}{
		{"", true}, // unset -> fallback "true"
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"on", true},
		{" on ", true},
		{"0", false},
		{"false", false},
		{"off", false},
		{"banana", false}, // faithful: anything non-truthy disables
	}
	for _, tc := range cases {
		t.Setenv(k, tc.val)
		got := envTruthyDefault(k, "true")
		assert.Equal(t, tc.want, got)

	}
}
