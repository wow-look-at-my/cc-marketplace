package main

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// End to end, against a real git repository: scope, extract, analyze, report.
// The unit tests can all pass while the pipeline finds nothing, because the
// pipeline is mostly about what it chooses to LOOK at.

// gitIn runs one git command in a fixture repository.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func repoFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) { t.Helper(); gitIn(t, dir, args...) }
	run("init", "-q", "-b", "master")
	write(t, dir, "docs/perf.md", "Measured: 23.8 B/event raw, 7.5 B/event gzipped, 63.2 ms to decode.\n")
	write(t, dir, "existing.go", "package p\n\nfunc Existing() {}\n")
	run("add", "-A")
	run("commit", "-qm", "base")
	return dir
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))

	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))

}

func findingsFor(t *testing.T, dir string) []Finding {
	t.Helper()
	return check(dir)
}

// The defect this hook exists for: a figure recalled from a document in the
// same repo, wrong, in a comment that cites that very document.
func TestCatchesFigureThatContradictsItsOwnCitedDoc(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", `package p

// The payload is ~10 B/event gzipped, and decodes fast.
// see docs/perf.md
func Encode() {}
`)
	found := findingsFor(t, dir)
	require.True(t, hasKind(found, ClaimQuantity))

	assert.Contains(t, render(found), "docs/perf.md")

}

// The same shape, but the figure is a legitimate rounding. Reporting this
// would make the check unusable.
func TestAcceptsAnHonestRounding(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", `package p

// The payload is ~24 B/event raw and takes ~63 ms to decode.
// see docs/perf.md
func Encode() {}
`)
	found := findingsFor(t, dir)
	assert.False(t, hasKind(found, ClaimQuantity))

}

// The other real defect: a comment left behind naming a test the same change
// removes.
func TestCatchesNameThatNoLongerExists(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", `package p

// TestTimelineFrameBudget gates on the average, and Existing covers the rest.
func Encode() {}
`)
	found := findingsFor(t, dir)
	require.True(t, hasKind(found, ClaimReference))

	assert.NotContains(t, render(found), "\"Existing\"")

}

func TestCatchesHedge(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n// This probably keeps the lock held.\nfunc Encode() {}\n")
	found := findingsFor(t, dir)
	require.True(t, hasKind(found, ClaimHedge))

}

// SCOPE is the efficiency claim, so it gets a test: a pre-existing bad comment
// that this session did not touch must cost nothing and report nothing.
func TestUntouchedCommentsAreNotChecked(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "old.go", "package p\n\n// This probably leaks, and TestGone covers it.\nfunc Old() {}\n")
	commit(t, dir, "pre-existing")

	// A new change elsewhere, with nothing wrong in it.
	write(t, dir, "new.go", "package p\n\n// Encode writes the header first.\nfunc Encode() {}\n")

	found := findingsFor(t, dir)
	assert.Equal(t, 0, len(found))

}

// A comment inside a string literal is not a comment.
func TestStringLiteralsAreNotComments(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\nvar s = \"// this probably fails and TestGone proves it\"\n")
	found := findingsFor(t, dir)
	assert.Equal(t, 0, len(found))

}

// Tool directives are instructions, not claims.
func TestDirectivesAreSkipped(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n//go:generate echo probably\nfunc Encode() {}\n")
	found := findingsFor(t, dir)
	assert.Equal(t, 0, len(found))

}

// A session that wrote no comments must do no work and block nothing.
func TestCleanSessionIsSilent(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\nfunc Encode() { _ = 1 }\n")
	found := findingsFor(t, dir)
	assert.Equal(t, 0, len(found))

}

// The hook contract: exit 2 blocks, exit 0 allows, and an already-blocking stop
// never blocks again (that would be an infinite loop).
func TestHookContract(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n// This probably keeps the lock held.\nfunc Encode() {}\n")

	var stderr bytes.Buffer
	code := run(strings.NewReader(`{"hook_event_name":"Stop","cwd":"`+dir+`"}`), &stderr)
	assert.Equal(t, 2, code)

	assert.Contains(t, stderr.String(), "STOP:")

	stderr.Reset()
	code = run(strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":true,"cwd":"`+dir+`"}`), &stderr)
	assert.False(t, code != 0 || stderr.Len() != 0)

	stderr.Reset()
	code = run(strings.NewReader("not json"), &stderr)
	assert.Equal(t, 0, code)

}

// A backend that cannot be reached must be LOUD. Reporting nothing would claim
// the comments were checked when they were not.
func TestUnreachableReviewerIsReported(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", `package p

// Decodes 5x faster because nothing parses per record.
func Encode() {}
`)
	t.Setenv(envKey, "test-key")
	t.Setenv(envURL, "http://127.0.0.1:1/never")

	found := findingsFor(t, dir)
	require.Contains(t, render(found), "were NOT checked")

}

// With no key the judgment stage is skipped, and the mechanical floor still
// applies.
func TestNoKeyStillEnforcesTheMechanicalFloor(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", `package p

// Decodes 5x faster, and TestGone proves it.
func Encode() {}
`)
	t.Setenv(envKey, "")
	found := findingsFor(t, dir)
	assert.True(t, hasKind(found, ClaimReference))

	assert.NotContains(t, render(found), "were NOT checked")

}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", msg)
}

func hasKind(findings []Finding, kind ClaimKind) bool {
	for _, f := range findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func render(findings []Finding) string {
	var sb strings.Builder
	for _, f := range findings {
		sb.WriteString(f.String())
		sb.WriteString("\n")
	}
	return sb.String()
}
