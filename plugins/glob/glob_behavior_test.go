package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestEnvNoIgnoreOverride(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, ".git/config", "ignored.txt", "kept.txt")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0o644))

	t.Setenv("CLAUDE_CODE_GLOB_NO_IGNORE", "0")
	got, _ := runGlob(t, testTool(t, root), "*.txt")
	assert.False(t, containsLine(got, "ignored.txt"))

	assert.True(t, containsLine(got, "kept.txt"))

}

func TestEnvHiddenOverride(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, ".hidden.txt", "visible.txt")
	t.Setenv("CLAUDE_CODE_GLOB_HIDDEN", "false")
	got, _ := runGlob(t, testTool(t, root), "*.txt")
	wantText(t, got, "visible.txt")
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

func TestInvalidGlobResolvesAsNoFilesFound(t *testing.T) {
	// rg exits 2 on an unparseable glob; the builtin resolves whatever
	// stdout produced (nothing) instead of erroring (spec quirk #3).
	root := t.TempDir()
	mkFiles(t, root, "a.txt")
	got, isErr := runGlob(t, testTool(t, root), "{unclosed")
	require.False(t, isErr)

	wantText(t, got, "No files found")
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

func TestEmptyPathTreatedAsOmitted(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "a.txt")
	got, isErr := runGlob(t, testTool(t, root), "*.txt", "")
	require.False(t, isErr)

	wantText(t, got, "a.txt")
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
