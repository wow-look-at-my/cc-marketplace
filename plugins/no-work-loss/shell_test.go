package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every compound shell form gets walked. A destructive command hidden in a
// loop body or a case arm is still a destructive command.
func TestDeniesInsideEveryCompoundForm(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	for _, c := range []string{
		"if true; then git reset --hard; fi",
		"if false; then echo no; else git reset --hard; fi",
		"while true; do git reset --hard; done",
		"for f in a b; do git reset --hard; done",
		"case x in x) git reset --hard ;; esac",
		"nuke() { git reset --hard; }",
		"time git reset --hard",
		"{ git reset --hard; }",
	} {
		require.NotEmpty(t, ask(t, dir, c), "expected DENY for %q", c)
	}
}

// A syntax check never runs the script, so the writes named inside it do not
// happen. Running the same script without -n still denies, which is what makes
// the allow a property of -n rather than of the fixture.
func TestSyntaxCheckDoesNotRunTheScript(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	writeAt(t, dir, "deploy.sh", "#!/bin/bash\necho hi > src.txt\n")

	for _, c := range []string{
		"bash -n deploy.sh",
		"bash --noexec deploy.sh",
		"bash -nx deploy.sh",
		"sh -n deploy.sh",
	} {
		assert.Empty(t, ask(t, dir, c), "expected ALLOW for %q", c)
	}
	require.NotEmpty(t, ask(t, dir, "bash deploy.sh"), "the same script without -n must still deny")
}

func TestDeniesThroughMoreWrappers(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	for _, c := range []string{
		"nohup git reset --hard",
		"setsid git reset --hard",
		"nice git reset --hard",
		// The flag values below must not be mistaken for the program.
		"nice -n 10 git reset --hard",
		"ionice -c 3 git reset --hard",
		"env -u FOO git reset --hard",
		"sudo -u root git reset --hard",
		"echo x | xargs -n 1 git checkout",
	} {
		require.NotEmpty(t, ask(t, dir, c), "expected DENY for %q", c)
	}
}

// A cd only carries where the shell itself carries it. These pin the
// difference rather than leaving it to be rediscovered.
func TestCdScopeFollowsTheShell(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	clean := newRepoAt(t)

	// A cd inside a subshell does not move the commands after it. Asked of the
	// destruction half, because the provenance half refuses a hard reset in
	// either repository and would not tell the two cases apart.
	assert.Empty(t, lossOnly(t, clean, "(cd "+dir+" && git status) && git reset --hard"),
		"the cd was contained in the subshell, so the reset ran in the clean repo")

	// A cd in a sequence does.
	assert.NotEmpty(t, lossOnly(t, clean, "cd "+dir+" && git reset --hard"))
}

func TestRelativeAndAbsoluteCdBothResolve(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	parent := filepath.Dir(dir)
	base := filepath.Base(dir)

	assert.NotEmpty(t, ask(t, parent, "cd "+base+" && git reset --hard"), "relative cd")
	assert.NotEmpty(t, ask(t, parent, "cd "+dir+" && git reset --hard"), "absolute cd")
}

// `cd -` goes wherever the shell was last, which this hook cannot know, so a
// destructive command after one is refused rather than guessed at.
func TestCdDashIsUnknowable(t *testing.T) {
	dir := newRepo(t)
	r := ask(t, dir, "cd - && git reset --hard")
	require.NotEmpty(t, r)
	assert.Contains(t, r, "cannot tell")
}

// newRepoAt builds a second, clean repository without re-pointing HOME, so a
// test can hold two repositories at once.
func newRepoAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "guard@example.com")
	git(t, dir, "config", "user.name", "Guard")
	writeAt(t, dir, "tracked.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "initial")
	return dir
}
