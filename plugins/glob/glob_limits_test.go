package main

import (
	"fmt"
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
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := testTool(t, root)
	got, isErr := runGlob(t, g, "*.txt")
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.HasPrefix(got, persistedOutputOpen+"\n") || !strings.HasSuffix(got, persistedOutputClose) {
		t.Fatalf("expected a persisted-output block, got:\n%.400s", got)
	}
	m := persistedPathRe.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no persisted path in:\n%.400s", got)
	}
	saved, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(saved), "\n")
	if len(lines) != globMaxResults+1 {
		t.Fatalf("persisted %d lines, want %d paths + truncation line", len(lines), globMaxResults+1)
	}
	if lines[len(lines)-1] != globTruncationLine {
		t.Errorf("last line = %q, want the verbatim truncation line", lines[len(lines)-1])
	}
	seen := make(map[string]bool, globMaxResults)
	for _, l := range lines[:globMaxResults] {
		if !strings.HasPrefix(l, "f") || !strings.HasSuffix(l, ".txt") {
			t.Fatalf("unexpected path line %q", l)
		}
		if seen[l] {
			t.Fatalf("duplicate path %q", l)
		}
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
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := testTool(t, root)
	got, isErr := runGlob(t, g, "*.txt")
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.HasPrefix(got, persistedOutputOpen) {
		t.Fatalf("expected persistence above 50000 chars, got %d chars inline", utf16Len(got))
	}
	if strings.Contains(got, globTruncationLine) {
		t.Error("preview must not contain the truncation line: only 3100 files")
	}
	m := persistedPathRe.FindStringSubmatch(got)
	if m == nil {
		t.Fatal("no persisted path")
	}
	saved, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(string(saved), "\n")); n != total {
		t.Errorf("saved %d lines, want %d (no truncation)", n, total)
	}
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
	if !isErr {
		t.Fatalf("want timeout error, got: %s", got)
	}
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
	if isErr {
		t.Fatalf("partial timeout must resolve: %s", got)
	}
	wantText(t, got, "kept.txt")
}

func TestEAGAINRetryThroughTool(t *testing.T) {
	root := t.TempDir()
	fake := writeFakeRg(t, fmt.Sprintf(`if [ "$1" = "-j" ]; then printf '%s/ok.txt\n'; else echo 'rg: Resource temporarily unavailable (os error 11)' >&2; exit 2; fi`, root))
	g := testTool(t, root)
	g.resolveRg = fixedRg(fake)
	got, isErr := runGlob(t, g, "*")
	if isErr {
		t.Fatalf("retry must succeed: %s", got)
	}
	wantText(t, got, "ok.txt")
}

func TestRipgrepMissingThroughTool(t *testing.T) {
	g := testTool(t, t.TempDir())
	g.resolveRg = resolveRipgrep
	t.Setenv("PATH", t.TempDir())
	got, isErr := runGlob(t, g, "*")
	if !isErr {
		t.Fatalf("want rg-not-found error, got: %s", got)
	}
	wantText(t, got, ripgrepNotFoundMsg)
}

func TestRelativizePathQuirks(t *testing.T) {
	if got := relativizePath("/root/sub/f.txt", "/root"); got != "sub/f.txt" {
		t.Errorf("under root: %q", got)
	}
	if got := relativizePath("/other/f.txt", "/root"); got != "/other/f.txt" {
		t.Errorf("outside root must stay absolute: %q", got)
	}
	// Faithful quirk: a sibling name beginning with ".." falls back to
	// the absolute path even though it is under the root.
	if got := relativizePath("/root/..foo", "/root"); got != "/root/..foo" {
		t.Errorf("..-prefixed relative form must fall back to absolute: %q", got)
	}
}

func TestResolveAgainst(t *testing.T) {
	if got := resolveAgainst("/abs/dir", "/root"); got != "/abs/dir" {
		t.Errorf("absolute: %q", got)
	}
	if got := resolveAgainst("rel/dir", "/root"); got != "/root/rel/dir" {
		t.Errorf("relative: %q", got)
	}
	if got := resolveAgainst("../x", "/root/sub"); got != "/root/x" {
		t.Errorf("dot-dot: %q", got)
	}
}

func TestNewGlobToolDefaults(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "/tmp/some-project")
	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "")
	g := newGlobTool(discardLogf)
	if g.root != "/tmp/some-project" {
		t.Errorf("root = %q, want CLAUDE_PROJECT_DIR", g.root)
	}
	if g.maxResults != 25000 || g.persistThreshold != 50000 || g.maxOutput != 20000000 {
		t.Errorf("limits = %d/%d/%d, want 25000/50000/20000000", g.maxResults, g.persistThreshold, g.maxOutput)
	}

	t.Setenv("CLAUDE_PROJECT_DIR", "")
	g = newGlobTool(discardLogf)
	wd, _ := os.Getwd()
	if g.root != wd {
		t.Errorf("root = %q, want process cwd %q", g.root, wd)
	}
}

func TestUNCishResolvedPathSkipsValidation(t *testing.T) {
	// The builtin skips stat validation when the RESOLVED path starts
	// with \\ or // (Windows UNC shapes). On POSIX, resolution collapses
	// a leading // (both Node and Go), so the branch is only reachable
	// with a backslash form — test it at the validateDir layer.
	g := testTool(t, t.TempDir())
	if msg, ok := g.validateDir(`\\host\share`, `\\host\share`); !ok {
		t.Errorf("UNC path must skip validation, got %q", msg)
	}
	if msg, ok := g.validateDir("//host/share", "//host/share"); !ok {
		t.Errorf("//-prefixed resolved path must skip validation, got %q", msg)
	}
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
		if got := envTruthyDefault(k, "true"); got != tc.want {
			t.Errorf("envTruthyDefault(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}
