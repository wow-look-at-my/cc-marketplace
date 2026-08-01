package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// End to end, against a real git repository: scope, extract, analyze, report.
// The unit tests can all pass while the pipeline finds nothing, because the
// pipeline is mostly about what it chooses to LOOK at.

func repoFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
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
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if !hasKind(found, ClaimQuantity) {
		t.Fatalf("the wrong figure was not reported; got %v", found)
	}
	if !strings.Contains(render(found), "docs/perf.md") {
		t.Errorf("the report should name the document it checked against:\n%s", render(found))
	}
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
	if found := findingsFor(t, dir); hasKind(found, ClaimQuantity) {
		t.Errorf("a correct rounding was reported: %v", found)
	}
}

// The other real defect: a comment left behind naming a test that was deleted
// in the same change.
func TestCatchesNameThatNoLongerExists(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", `package p

// TestTimelineFrameBudget gates on the average, and Existing covers the rest.
func Encode() {}
`)
	found := findingsFor(t, dir)
	if !hasKind(found, ClaimReference) {
		t.Fatalf("the dangling test name was not reported; got %v", found)
	}
	if strings.Contains(render(found), "\"Existing\"") {
		t.Errorf("a name that DOES exist was reported:\n%s", render(found))
	}
}

func TestCatchesHedge(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n// This probably keeps the lock held.\nfunc Encode() {}\n")
	if found := findingsFor(t, dir); !hasKind(found, ClaimHedge) {
		t.Fatalf("hedge not reported; got %v", found)
	}
}

// SCOPE is the efficiency claim, so it gets a test: a pre-existing bad comment
// that this session did not touch must cost nothing and report nothing.
func TestUntouchedCommentsAreNotChecked(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "old.go", "package p\n\n// This probably leaks, and TestGone covers it.\nfunc Old() {}\n")
	commit(t, dir, "pre-existing")

	// A new change elsewhere, with nothing wrong in it.
	write(t, dir, "new.go", "package p\n\n// Encode writes the header first.\nfunc Encode() {}\n")

	if found := findingsFor(t, dir); len(found) != 0 {
		t.Errorf("checked comments the session never wrote: %v", found)
	}
}

// A comment inside a string literal is not a comment.
func TestStringLiteralsAreNotComments(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\nvar s = \"// this probably fails and TestGone proves it\"\n")
	if found := findingsFor(t, dir); len(found) != 0 {
		t.Errorf("read a string literal as a comment: %v", found)
	}
}

// Tool directives are instructions, not claims.
func TestDirectivesAreSkipped(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n//go:generate echo probably\nfunc Encode() {}\n")
	if found := findingsFor(t, dir); len(found) != 0 {
		t.Errorf("reported a directive: %v", found)
	}
}

// A session that wrote no comments must do no work and block nothing.
func TestCleanSessionIsSilent(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\nfunc Encode() { _ = 1 }\n")
	if found := findingsFor(t, dir); len(found) != 0 {
		t.Errorf("blocked a session with nothing to report: %v", found)
	}
}

// The hook contract: exit 2 blocks, exit 0 allows, and an already-blocking stop
// never blocks again (that would be an infinite loop).
func TestHookContract(t *testing.T) {
	dir := repoFixture(t)
	write(t, dir, "wire.go", "package p\n\n// This probably keeps the lock held.\nfunc Encode() {}\n")

	var stderr bytes.Buffer
	code := run(strings.NewReader(`{"hook_event_name":"Stop","cwd":"`+dir+`"}`), &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2 (block)", code)
	}
	if !strings.Contains(stderr.String(), "STOP:") {
		t.Errorf("no findings on stderr: %q", stderr.String())
	}

	stderr.Reset()
	code = run(strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":true,"cwd":"`+dir+`"}`), &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Errorf("re-blocked while already blocking: exit=%d stderr=%q", code, stderr.String())
	}

	stderr.Reset()
	if code := run(strings.NewReader("not json"), &stderr); code != 0 {
		t.Errorf("garbage stdin: exit = %d, want 0", code)
	}
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
	if !strings.Contains(render(found), "were NOT checked") {
		t.Fatalf("an unreachable reviewer was silent; got:\n%s", render(found))
	}
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
	if !hasKind(found, ClaimReference) {
		t.Errorf("the mechanical floor did not apply without a key: %v", found)
	}
	if strings.Contains(render(found), "were NOT checked") {
		t.Errorf("absent config should be a clean no-op, not an error:\n%s", render(found))
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", msg}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
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
