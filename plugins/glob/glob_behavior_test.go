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

func TestWeirdFilenames(t *testing.T) {
	root := t.TempDir()
	// Oldest-first creation order = expected output order.
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
		"日本語ファイル.txt",
		"emoji-😀.txt",
		"combining-e\u0301.txt", // NFD: e + U+0301 combining acute accent
		"a/b/c/d/e/f/deep.txt",
		".hidden.txt",
		".dotdir/inside.txt",
	}
	mkFiles(t, root, names...)
	got, isErr := runGlob(t, testTool(t, root), "**/*.txt")
	require.False(t, isErr)

	wantText(t, got, strings.Join(names, "\n"))
}

func TestMtimeAscendingOrder(t *testing.T) {
	root := t.TempDir()
	// Creation order c, a, b — mtimes assigned in that order, so the
	// result must be c, a, b (oldest first), not alphabetical.
	mkFiles(t, root, "c.go", "a.go", "b.go")
	got, _ := runGlob(t, testTool(t, root), "*.go")
	wantText(t, got, "c.go\na.go\nb.go")
}

func TestGitignoreNotRespectedAndGitIncluded(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, ".git/config", ".gitignore", "ignored.txt", "kept.txt")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0o644))

	got, _ := runGlob(t, testTool(t, root), "**/*")
	for _, want := range []string{".git/config", ".gitignore", "ignored.txt", "kept.txt"} {
		assert.True(t, containsLine(got, want))

	}
}

// The CLAUDE_CODE_GLOB_NO_IGNORE / _HIDDEN overrides act on DIRECTORY
// traversal only: ripgrep treats a positive --glob as a whitelist that
// overrides hidden/ignore filtering for directly-matching FILES (verified
// empirically on rg 14.1.0), so a top-level ignored or hidden file that
// matches the pattern is returned regardless. The builtin passes the
// identical argv and shares the quirk.
func TestEnvNoIgnoreOverride(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, ".git/config", "ignoreddir/within.txt", "ignored.txt", "kept.txt")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignoreddir/\nignored.txt\n"), 0o644))

	g := testTool(t, root)
	got, _ := runGlob(t, g, "*.txt") // default: gitignore NOT respected
	assert.True(t, containsLine(got, "ignoreddir/within.txt"), "default must ignore .gitignore: %s", got)

	t.Setenv("CLAUDE_CODE_GLOB_NO_IGNORE", "0")
	got, _ = runGlob(t, g, "*.txt")
	assert.False(t, containsLine(got, "ignoreddir/within.txt"), "ignored DIRECTORY must be pruned: %s", got)

	assert.True(t, containsLine(got, "kept.txt"), got)

	assert.True(t, containsLine(got, "ignored.txt"), "whitelist-glob quirk: directly-matching ignored FILE still returned: %s", got)

}

func TestEnvHiddenOverride(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, ".dotdir/inside.txt", ".hidden.txt", "visible.txt")
	g := testTool(t, root)
	got, _ := runGlob(t, g, "*.txt") // default: hidden included
	assert.True(t, containsLine(got, ".dotdir/inside.txt"), "default must include hidden dirs: %s", got)

	t.Setenv("CLAUDE_CODE_GLOB_HIDDEN", "false")
	got, _ = runGlob(t, g, "*.txt")
	assert.False(t, containsLine(got, ".dotdir/inside.txt"), "hidden DIRECTORY must be pruned: %s", got)

	assert.True(t, containsLine(got, "visible.txt"), got)

	assert.True(t, containsLine(got, ".hidden.txt"), "whitelist-glob quirk: directly-matching hidden FILE still returned: %s", got)

}

func TestSymlinksNotFollowedOrListed(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "real/target.txt")
	must := func(err error) {
		t.Helper()
		require.Nil(t, err)

	}
	must(os.Symlink(filepath.Join(root, "real/target.txt"), filepath.Join(root, "link-to-file.txt")))
	must(os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link-to-dir")))
	must(os.Symlink(filepath.Join(root, "nope"), filepath.Join(root, "broken-link.txt")))
	got, _ := runGlob(t, testTool(t, root), "**/*")
	// rg's walker (no -L) reports only the real file: symlinks to files,
	// dirs, and broken targets are all absent — builtin parity.
	wantText(t, got, "real/target.txt")
}

func TestBinaryFileListed(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "blob.bin"), []byte{0, 1, 2, 0xff, 0, 60, 61}, 0o644))

	got, _ := runGlob(t, testTool(t, root), "*.bin")
	wantText(t, got, "blob.bin")
}

func TestNoMatchesIsNoFilesFound(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "a.txt")
	got, isErr := runGlob(t, testTool(t, root), "*.zzz")
	require.False(t, isErr)

	wantText(t, got, "No files found")
}

func TestInvalidGlobSurfacesRgStderr(t *testing.T) {
	// rg exits 2 with nothing on stdout for an unparseable glob. The
	// builtin silently resolved "No files found"; this plugin surfaces
	// rg's stderr as a tool error instead (deliberate deviation shared
	// with the grep sibling — see rg.go). The assertion is substring-
	// based because rg 13 omits the "rg: " message prefix that 14+ add.
	root := t.TempDir()
	mkFiles(t, root, "a.txt")
	got, isErr := runGlob(t, testTool(t, root), "{unclosed")
	require.True(t, isErr)

	assert.Contains(t, got, "error parsing glob")

	assert.Contains(t, got, "{unclosed")

}

func TestEmptyPatternMatchesEverything(t *testing.T) {
	// Empirical rg 14.x behavior: --glob '' is inert, so every file
	// matches. The builtin passed the empty string through identically.
	root := t.TempDir()
	mkFiles(t, root, "a.txt", "b/c.txt")
	got, _ := runGlob(t, testTool(t, root), "")
	wantText(t, got, "a.txt\nb/c.txt")
}

func TestBracePattern(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "a.go", "b.ts", "c.txt")
	got, _ := runGlob(t, testTool(t, root), "*.{go,ts}")
	wantText(t, got, "a.go\nb.ts")
}

func TestDoubleStarScopedToSubdir(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "src/x.ts", "src/a/b/y.ts", "other/z.ts")
	got, _ := runGlob(t, testTool(t, root), "src/**/*.ts")
	wantText(t, got, "src/x.ts\nsrc/a/b/y.ts")
}

func TestGlobsAreCaseSensitive(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "Upper.TXT", "lower.txt")
	got, _ := runGlob(t, testTool(t, root), "*.txt")
	wantText(t, got, "lower.txt")
	got, _ = runGlob(t, testTool(t, root), "*.TXT")
	wantText(t, got, "Upper.TXT")
}

func TestPathArgAbsoluteAndRelative(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "top.txt", "sub/inner.txt", "sub/deep/most.txt")
	g := testTool(t, root)

	got, _ := runGlob(t, g, "**/*.txt", filepath.Join(root, "sub"))
	wantText(t, got, "sub/inner.txt\nsub/deep/most.txt")

	got, _ = runGlob(t, g, "**/*.txt", "sub")
	wantText(t, got, "sub/inner.txt\nsub/deep/most.txt")
}

func TestPathOutsideRootReturnsAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mkFiles(t, other, "elsewhere.txt")
	got, _ := runGlob(t, testTool(t, root), "*.txt", other)
	wantText(t, got, filepath.Join(other, "elsewhere.txt"))
}

func TestPathDoesNotExist(t *testing.T) {
	root := t.TempDir()
	got, isErr := runGlob(t, testTool(t, root), "*", "no-such-dir")
	require.True(t, isErr)

	wantText(t, got, fmt.Sprintf("Directory does not exist: no-such-dir. Note: your current working directory is %s.", root))
}

func TestPathIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "plain.txt")
	got, isErr := runGlob(t, testTool(t, root), "*", "plain.txt")
	require.True(t, isErr)

	wantText(t, got, "Path is not a directory: plain.txt")
}

func TestPathDidYouMeanSuggestion(t *testing.T) {
	// EvalSymlinks so the suggester's realpath step cannot diverge on
	// hosts whose temp dir sits behind a symlink (e.g. macOS /var).
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.Nil(t, err)

	root := filepath.Join(base, "proj")
	mkFiles(t, root, "sub/file.txt")
	// "../sub" resolves to base/sub (missing); re-rooted under root it
	// exists, so the suggester proposes the absolute re-rooted path.
	got, isErr := runGlob(t, testTool(t, root), "*", "../sub")
	require.True(t, isErr)

	want := fmt.Sprintf("Directory does not exist: ../sub. Note: your current working directory is %s. Did you mean %s?",
		root, filepath.Join(root, "sub"))
	wantText(t, got, want)
}

func TestAbsolutePatternOverridesPath(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	mkFiles(t, root, "decoy.txt")
	mkFiles(t, elsewhere, "hit/one.txt", "hit/two.txt")
	// Absolute pattern wins over the path argument; results outside the
	// root come back absolute.
	got, _ := runGlob(t, testTool(t, root), elsewhere+"/**/*.txt", root)
	wantText(t, got, filepath.Join(elsewhere, "hit/one.txt")+"\n"+filepath.Join(elsewhere, "hit/two.txt"))
}

func TestAbsolutePatternWithoutMetachar(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "sub/one.txt", "sub/other.txt")
	// No metachar: split into dirname + basename. The basename becomes a
	// bare glob, which (gitignore semantics) matches at any depth below
	// the base dir.
	got, _ := runGlob(t, testTool(t, root), filepath.Join(root, "sub", "one.txt"))
	wantText(t, got, "sub/one.txt")
}

func TestAbsolutePatternMetacharInFirstComponent(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "x.txt")
	base, rel := splitAbsolutePattern("/st*ar")
	assert.False(t, base != "/" || rel != "st*ar")

	base, rel = splitAbsolutePattern("/a/b/pa*/x")
	assert.False(t, base != "/a/b" || rel != "pa*/x")

	base, rel = splitAbsolutePattern("/a/b?.txt")
	assert.False(t, base != "/a" || rel != "b?.txt")

}

func TestSplitAbsolutePatternNodeDirnameParity(t *testing.T) {
	// Node dirname/basename ignore trailing separators; Go's
	// filepath.Dir("/foo/bar/") would keep "/foo/bar" as the base dir.
	base, rel := splitAbsolutePattern("/foo/bar/")
	assert.Equal(t, "/foo", base)

	assert.Equal(t, "bar", rel)

	base, rel = splitAbsolutePattern("/foo/bar///")
	assert.Equal(t, "/foo", base)

	assert.Equal(t, "bar", rel)

	// Node basename("/") is "": the empty glob is inert in rg, so the
	// whole tree under "/" matches (faithful to the builtin's a81).
	base, rel = splitAbsolutePattern("/")
	assert.Equal(t, "/", base)

	assert.Equal(t, "", rel)

}

func TestAbsolutePatternTrailingSlash(t *testing.T) {
	// "<root>/sub/" must search <root> for glob "sub" (Node dirname
	// semantics), not <root>/sub for glob "sub". The fixture makes the
	// two behaviors observably different: a FILE named "sub" lives under
	// a/, while sub/ is a directory (a bare-name glob never matches a
	// directory's contents, verified rg 13/14/15).
	root := t.TempDir()
	mkFiles(t, root, "a/sub", "sub/inner.txt")
	got, isErr := runGlob(t, testTool(t, root), root+"/sub/")
	require.False(t, isErr)

	wantText(t, got, "a/sub")
}

func TestMtimeTieOrdersByLocaleCollation(t *testing.T) {
	// With --sort=modified gone (the sort now happens in Go), equal
	// mtimes order by the localeCompare-parity collator: primary
	// strength is case-insensitive, so a.txt sorts before B.txt (byte
	// order would put B.txt first).
	root := t.TempDir()
	mkFiles(t, root, "B.txt", "a.txt", "sub/C.txt")
	tie := time.Now().Add(-time.Hour)
	for _, n := range []string{"B.txt", "a.txt", "sub/C.txt"} {
		p := filepath.Join(root, filepath.FromSlash(n))
		require.NoError(t, os.Chtimes(p, tie, tie))
	}
	got, _ := runGlob(t, testTool(t, root), "**/*.txt")
	wantText(t, got, "a.txt\nB.txt\nsub/C.txt")
}

func TestPathTildeExpansion(t *testing.T) {
	// Vq parity: "~" and "~/sub" expand to the home directory. Results
	// outside the root come back absolute.
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkFiles(t, home, "inhome.txt", "nested/deep.txt")
	g := testTool(t, root)

	got, isErr := runGlob(t, g, "**/*.txt", "~")
	require.False(t, isErr)

	wantText(t, got, home+"/inhome.txt\n"+home+"/nested/deep.txt")

	got, isErr = runGlob(t, g, "*.txt", "~/nested")
	require.False(t, isErr)

	wantText(t, got, home+"/nested/deep.txt")
}

func TestPathTildeUserNotExpanded(t *testing.T) {
	// The builtin's Vq only expanded "~" and "~/..."; "~user" resolves
	// as a literal name against the root.
	root := t.TempDir()
	got, isErr := runGlob(t, testTool(t, root), "*", "~nobody")
	require.True(t, isErr)

	wantText(t, got, fmt.Sprintf("Directory does not exist: ~nobody. %s %s.", cwdNote, root))
}

func TestPathWhitespaceTrimmedBeforeResolve(t *testing.T) {
	// Vq trim() parity: "  sub  " only names a real directory after
	// trimming, and a whitespace-only path resolves to the root.
	root := t.TempDir()
	mkFiles(t, root, "sub/inner.txt")
	g := testTool(t, root)
	got, isErr := runGlob(t, g, "*.txt", "  sub  ")
	require.False(t, isErr)

	wantText(t, got, "sub/inner.txt")

	got, isErr = runGlob(t, g, "**/*.txt", "   ")
	require.False(t, isErr)

	wantText(t, got, "sub/inner.txt")
}

func TestPathNullByteRejected(t *testing.T) {
	root := t.TempDir()
	got, isErr := runGlob(t, testTool(t, root), "*", "bad\x00path")
	require.True(t, isErr)

	wantText(t, got, "Path contains null bytes")
}

func TestEmptyPathTreatedAsOmitted(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "a.txt")
	got, isErr := runGlob(t, testTool(t, root), "*.txt", "")
	require.False(t, isErr)

	wantText(t, got, "a.txt")
}

func TestUndefinedAndNullPathTreatedAsOmitted(t *testing.T) {
	// Models emit the literal strings "undefined"/"null" for "no path";
	// resolveAgainst maps them to the root instead of erroring on a
	// nonexistent directory of that name.
	root := t.TempDir()
	mkFiles(t, root, "a.txt")
	g := testTool(t, root)
	for _, p := range []string{"undefined", "null", "  undefined  "} {
		got, isErr := runGlob(t, g, "*.txt", p)
		require.False(t, isErr, "path %q", p)

		wantText(t, got, "a.txt")
	}
}

func TestTruncationAtInjectedCap(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "f1.txt", "f2.txt", "f3.txt", "f4.txt", "f5.txt")
	g := testTool(t, root)
	g.maxResults = 3
	got, isErr := runGlob(t, g, "*.txt")
	require.False(t, isErr)

	wantText(t, got, "f1.txt\nf2.txt\nf3.txt\n"+globTruncationLine)
}

func TestExactlyAtCapIsNotTruncated(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "f1.txt", "f2.txt", "f3.txt")
	g := testTool(t, root)
	g.maxResults = 3
	got, _ := runGlob(t, g, "*.txt")
	wantText(t, got, "f1.txt\nf2.txt\nf3.txt")
}

func containsLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l == line {
			return true
		}
	}
	return false
}
