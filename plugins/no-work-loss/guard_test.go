package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRepo builds a real repository with one committed file. The tests drive
// git for state rather than faking it, because every interesting case in this
// plugin is a question about what `git status` actually reports.
func newRepo(t *testing.T) string {
	t.Helper()
	// A hermetic config: a stray alias in the developer's own ~/.gitconfig
	// must not decide whether these tests pass.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	dir := t.TempDir()
	// git reports the physical path, so resolve before comparing against it.
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "guard@example.com")
	git(t, dir, "config", "user.name", "Guard")
	writeAt(t, dir, "tracked.go", "package a\n")
	writeAt(t, dir, ".gitignore", "build/\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "initial")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
}

// writeAt puts a file in a fixture repository. Named for its shape rather than
// the verb, because `write` is the type the provenance half is built on.
func writeAt(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// modify dirties a tracked file; untrack adds a file git has never seen. The
// two are deliberately separate everywhere in these tests.
func modify(t *testing.T, dir string) { writeAt(t, dir, "tracked.go", "package a\n// edited\n") }
func untrack(t *testing.T, dir string, name string) {
	writeAt(t, dir, name, "scratch\n")
}

// ask lives in harness_test.go: one entry-point driver for both halves.

func denied(t *testing.T, cwd, command string) string {
	t.Helper()
	r := ask(t, cwd, command)
	require.NotEmpty(t, r, "expected DENY for %q", command)
	// Both halves owe the reader a way forward: the destruction half names a
	// command to run instead, the provenance half names the tool to use.
	assert.True(t, strings.Contains(r, "run: ") || strings.Contains(r, "Use Edit") || strings.Contains(r, "ask the user"),
		"a denial must name the safe alternative: %s", r)
	return r
}

func allowed(t *testing.T, cwd, command string) {
	t.Helper()
	assert.Empty(t, ask(t, cwd, command), "expected ALLOW for %q", command)
}

// ---------------------------------------------------------------------------
// The cases the plugin exists to get right.
// ---------------------------------------------------------------------------

func TestDeniesResetHardOnDirtyTree(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	r := denied(t, dir, "git reset --hard origin/master")
	assert.Contains(t, r, "1 modified")
	assert.Contains(t, r, "git stash push -u")
}

func TestDeniesCheckoutOnDirtyTree(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "git checkout master")
}

// The motivating incident: the dangerous command is the second one, and the
// first is what made it dangerous.
func TestDeniesTheTwoCommandIncidentShape(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "git checkout master && git reset --hard origin/master")
}

func TestDeniesResetHardReachedByCd(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	elsewhere := t.TempDir()
	denied(t, elsewhere, "cd "+dir+" && git reset --hard")
}

func TestDeniesCleanWithUntrackedFilesViaDashC(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "scratch.txt")
	elsewhere := t.TempDir()
	r := denied(t, elsewhere, "git -C "+dir+" clean -fdx")
	assert.Contains(t, r, "untracked")
}

func TestDeniesStashClearWithEntries(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "stash", "push", "-m", "wip")
	r := denied(t, dir, "git stash clear")
	assert.Contains(t, r, "1 stash entry")
}

func TestDeniesRmOfUntrackedFile(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "internal/config/env.go")
	r := denied(t, dir, "rm internal/config/env.go")
	assert.Contains(t, r, "internal/config/env.go")
}

func TestDeniesCheckoutDashDashDot(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "git checkout -- .")
}

// A force push is judged on whether the remote's commits survive elsewhere,
// not on the working tree. With no remote-tracking ref there is nothing local
// saying what the remote holds, so it cannot be verified and is refused.
// The recoverable/unrecoverable split is covered in refverbs_test.go against a
// real remote.
func TestDeniesForcePushThatCannotBeVerified(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "git push --force origin master")
	assert.Contains(t, r, "git fetch")
}

func TestDeniesAliasHidingResetHard(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "config", "alias.nuke", "reset --hard")
	denied(t, dir, "git nuke")
}

func TestDeniesShellAliasHidingResetHard(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "config", "alias.boom", "!git reset --hard")
	denied(t, dir, "git boom")
}

// ---------------------------------------------------------------------------
// The allow list. A guard that blocks these gets switched off, and then it
// protects nothing.
// ---------------------------------------------------------------------------

func TestAllowsResetHardOnCleanTree(t *testing.T) {
	dir := newRepo(t)
	assert.Empty(t, lossOnly(t, dir, "git reset --hard origin/master"),
		"a clean tree has no uncommitted work to lose")

	// The merged hook still refuses it, for the other reason: a hard reset
	// replaces the files on disk with a commit's version, and that is a content
	// change no edit tool made.
	assert.Contains(t, denied(t, dir, "git reset --hard origin/master"), "git reset")
}

func TestAllowsBranchCreationEvenWhenDirty(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	untrack(t, dir, "scratch.txt")
	allowed(t, dir, "git checkout -b new-branch")
	allowed(t, dir, "git switch -c new-branch")
}

func TestAllowsReadOnlyCommandsInAnyState(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	untrack(t, dir, "scratch.txt")
	for _, c := range []string{
		"git status", "git log --oneline", "git diff", "git show HEAD",
		"git branch -a", "git stash list", "git remote -v",
	} {
		allowed(t, dir, c)
	}
}

func TestAllowsCommandsThatCreateRecovery(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	untrack(t, dir, "scratch.txt")
	allowed(t, dir, "git stash push -u")
	allowed(t, dir, `git commit -am "save"`)
	allowed(t, dir, "git add -A")
}

func TestAllowsRmOfGitignoredArtifact(t *testing.T) {
	dir := newRepo(t)
	writeAt(t, dir, "build/go-proxy", "binary\n")
	allowed(t, dir, "rm build/go-proxy")
}

func TestAllowsRmOfCleanTrackedFile(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "rm tracked.go") // the content is in HEAD, so it is recoverable
}

// ---------------------------------------------------------------------------
// Untracked and tracked are different states, and the verbs treat them
// differently. Collapsing them is the bug this pair of tests pins.
// ---------------------------------------------------------------------------

func TestResetHardSparesUntrackedFiles(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "scratch.txt")
	assert.Empty(t, lossOnly(t, dir, "git reset --hard"), "reset never touches untracked files")
}

func TestCleanSparesTrackedModifications(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	allowed(t, dir, "git clean -fd") // clean never touches tracked files
}

func TestCleanWithoutXSparesIgnoredFiles(t *testing.T) {
	dir := newRepo(t)
	writeAt(t, dir, "build/go-proxy", "binary\n")
	allowed(t, dir, "git clean -fd")
	denied(t, dir, "git clean -fdx")
}

// ---------------------------------------------------------------------------
// Parsing: chains, wrappers, subshells, and the flag spellings.
// ---------------------------------------------------------------------------

func TestDeniesThroughEveryChainOperator(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	for _, c := range []string{
		"echo hi && git reset --hard",
		"echo hi; git reset --hard",
		"false || git reset --hard",
		"echo hi\ngit reset --hard",
		"(cd " + dir + " && git reset --hard)",
		"echo $(git reset --hard)",
		"true | git reset --hard",
	} {
		require.NotEmpty(t, ask(t, dir, c), "expected DENY for %q", c)
	}
}

func TestDeniesThroughWrappers(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	for _, c := range []string{
		"env git reset --hard",
		"sudo git reset --hard",
		"sudo -E git reset --hard",
		"command git reset --hard",
		`\git reset --hard`,
		"/usr/bin/git reset --hard",
		"timeout 5 git reset --hard",
		"echo x | xargs git reset --hard",
		"env FOO=bar git reset --hard",
	} {
		require.NotEmpty(t, ask(t, dir, c), "expected DENY for %q", c)
	}
}

// GIT_DIR moves the repository somewhere the path words no longer describe, so
// the target becomes unknowable -- and unknowable denies.
func TestDeniesWhenGitEnvRelocatesTheRepo(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "GIT_DIR=/somewhere/else git reset --hard")
	assert.Contains(t, r, "cannot tell")
}

func TestDeniesWhenCdTargetIsNotKnowable(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "cd $TARGET && git reset --hard")
}

func TestDeniesRmWithUnresolvablePath(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "rm $TARGET")
	assert.Contains(t, r, "cannot resolve")
}

func TestDeniesUnparseableCommandNamingADestructiveVerb(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "git reset --hard `") // unterminated backquote
	assert.Contains(t, r, "could not be parsed")
}

func TestFlagVariantsAllReachTheSameVerdict(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "scratch.txt")
	for _, c := range []string{
		"git clean -f -d -x", "git clean -fdx", "git clean -xdf",
		"git clean --force --recurse-directories",
	} {
		require.NotEmpty(t, ask(t, dir, c), "expected DENY for %q", c)
	}
}

func TestAllowsDryRunAndInteractiveClean(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "scratch.txt")
	allowed(t, dir, "git clean -nd")
	allowed(t, dir, "git clean --dry-run")
}

// ---------------------------------------------------------------------------
// Verb-level distinctions that are easy to get backwards.
// ---------------------------------------------------------------------------

func TestAllowsSoftAndMixedReset(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	allowed(t, dir, "git reset --soft HEAD~1")
	allowed(t, dir, "git reset HEAD~1")
	allowed(t, dir, "git reset -- tracked.go")
}

func TestRestoreStagedOnlyLeavesTheFileOnDisk(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	allowed(t, dir, "git restore --staged tracked.go")
	denied(t, dir, "git restore tracked.go")
}

func TestAllowsStashSubcommandsThatPreserveContent(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "stash", "push", "-m", "wip")
	allowed(t, dir, "git stash show")
	allowed(t, dir, "git stash branch recovered")

	// pop and apply destroy nothing -- that is what separates them from drop --
	// but they put a stash's content back into the worktree, which is a change
	// no edit tool made.
	for _, c := range []string{"git stash apply", "git stash pop"} {
		assert.Empty(t, lossOnly(t, dir, c), "%s discards nothing", c)
		denied(t, dir, c)
	}
}

func TestAllowsStashDropWhenStashIsEmpty(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "git stash drop")
}

func TestAllowsForceWithLease(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "git push --force-with-lease origin master")
	allowed(t, dir, "git push origin master")
}

func TestDeniesForceRefspec(t *testing.T) {
	dir := newRepo(t)
	denied(t, dir, "git push origin +master:master")
}

func TestRebaseFamilyBlockedDirtyButRecoveryVerbsAllowed(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "git rebase master")
	denied(t, dir, "git merge feature")
	denied(t, dir, "git cherry-pick abc123")
	allowed(t, dir, "git rebase --abort")
	allowed(t, dir, "git merge --abort")
	allowed(t, dir, "git cherry-pick --continue")
}

func TestGitRmCachedLeavesTheFileAlone(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "scratch.txt")
	allowed(t, dir, "git rm --cached scratch.txt")
	denied(t, dir, "git rm -f scratch.txt")
}

// ---------------------------------------------------------------------------
// Non-git destruction.
// ---------------------------------------------------------------------------

func TestDeniesTruncatingRedirectOntoDirtyFile(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "echo x > tracked.go")
	// An append loses nothing, so the destruction half allows it. The provenance
	// half still refuses: appended text is authored content, and Edit is how
	// authored content reaches a tracked file.
	assert.Empty(t, lossOnly(t, dir, "echo x >> tracked.go"))
	assert.Contains(t, denied(t, dir, "echo x >> tracked.go"), "tracked.go")
}

func TestDeniesMvOverDirtyDestination(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	untrack(t, dir, "src.txt")
	denied(t, dir, "mv src.txt tracked.go")
	allowed(t, dir, "mv src.txt renamed.txt") // destination does not exist
}

func TestDeniesRmDirectoryContainingUncommittedWork(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "internal/config/env.go")
	denied(t, dir, "rm -rf internal")
	denied(t, dir, "rm -rf .")
}

func TestDeniesTeeAndTruncateOntoDirtyFile(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "echo x | tee tracked.go")
	assert.Empty(t, lossOnly(t, dir, "echo x | tee -a tracked.go"), "appending loses nothing")
	denied(t, dir, "echo x | tee -a tracked.go") // but it is still authoring
	denied(t, dir, "truncate -s 0 tracked.go")
}

// ---------------------------------------------------------------------------
// Staying out of the way.
// ---------------------------------------------------------------------------

func TestIgnoresNonBashToolsAndOtherEvents(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	assert.Empty(t, decideWithEvent(t, "PreToolUse", "Read", dir, "git reset --hard"))
	assert.Empty(t, decideWithEvent(t, "PostToolUse", "Bash", dir, "git reset --hard"))
	assert.Empty(t, decide([]byte("not json at all")))
}

func TestAllowsDestructiveCommandsOutsideAnyRepository(t *testing.T) {
	plain := t.TempDir()
	writeAt(t, plain, "notes.txt", "hi\n")
	allowed(t, plain, "rm notes.txt")
}

// The prefilter must never shell out for a command that names nothing
// destructive -- that is what keeps the common case cheap.
func TestPrefilterRejectsOrdinaryCommands(t *testing.T) {
	for _, c := range []string{
		"ls -la", "echo hello", "npm test", "go build ./...", "cat file.txt",
	} {
		assert.False(t, mayDestroy(c), "prefilter should skip %q", c)
	}
	for _, c := range []string{
		"git reset --hard", "rm x", "mv a b", "echo x > f", "truncate -s 0 f",
	} {
		assert.True(t, mayDestroy(c), "prefilter must not skip %q", c)
	}
}

func BenchmarkPrefilterMiss(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mayDestroy("npm run build && node dist/index.js --verbose")
	}
}
