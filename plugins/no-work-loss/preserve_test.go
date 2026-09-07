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

// gitOutput runs git and returns its trimmed stdout, for the tests below that
// must read back what preservation actually committed.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err, "git %s", strings.Join(args, " "))
	return strings.TrimSpace(string(out))
}

// listPreservationRefs names the preservation commit this hook made in dir, or
// nothing when it made none. Preservation commits to the CURRENT BRANCH, so
// there is one place to look and the caller can use the name as an ordinary
// rev. The subject line is what identifies it: a commit the hook wrote is the
// tip, and any other tip means no preservation happened.
func listPreservationRefs(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").Output()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(out)), "no-work-loss:") {
		return nil
	}
	return []string{"HEAD"}
}

// makeStrandedPreservationRef writes a ref under the retired prefix by hand.
// Nothing creates one any more, and the protection against deleting one has to
// keep working for a repository that still carries one from an older build.
func makeStrandedPreservationRef(t *testing.T, dir string) string {
	t.Helper()
	ref := protectedRefPrefix + "20260101T000000.000000000.1"
	git(t, dir, "update-ref", ref, gitOutput(t, dir, "rev-parse", "HEAD"))
	return ref
}

// ---------------------------------------------------------------------------
// Preserve, then allow: the primary case.
// ---------------------------------------------------------------------------

// The owner's own example: rm of an untracked file preserves it into a ref
// and allows the deletion, rather than refusing rm outright.
func TestPreservesUntrackedFileContentBeforeRm(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "scratch.txt") // writes "scratch\n"
	require.Empty(t, listPreservationRefs(t, dir))

	notice := preserved(t, dir, "rm scratch.txt")
	assert.Contains(t, notice, "scratch.txt")
	assert.Contains(t, notice, "committed to master")

	refs := listPreservationRefs(t, dir)
	require.Len(t, refs, 1)

	// The hook only analyses the command; it never runs it. The file is
	// still on disk, and the committed content must equal it exactly.
	content := gitOutput(t, dir, "show", refs[0]+":scratch.txt")
	assert.Equal(t, "scratch", content)
}

// A tracked edit is preserved on top of HEAD, so the commit carries HEAD's
// content for everything else and the CURRENT content for the at-risk path --
// and it never writes the user's working tree to get there.
func TestPreservesModifiedTrackedFileOnTopOfHead(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir) // rewrites tracked.go to "package a\n// edited\n"
	head := gitOutput(t, dir, "rev-parse", "HEAD")

	notice := preserved(t, dir, "git checkout master")
	assert.Contains(t, notice, "git checkout")
	assert.Contains(t, notice, "1 modified")

	refs := listPreservationRefs(t, dir)
	require.Len(t, refs, 1)

	parent := gitOutput(t, dir, "rev-parse", refs[0]+"^")
	assert.Equal(t, head, parent)
	content := gitOutput(t, dir, "show", refs[0]+":tracked.go")
	assert.Equal(t, "package a\n// edited", content)

	// The working tree still holds the edit byte for byte -- the hook analyses
	// the command and never writes a file.
	onDisk, err := os.ReadFile(filepath.Join(dir, "tracked.go"))
	require.NoError(t, err)
	assert.Equal(t, "package a\n// edited\n", string(onDisk))

	// The edit is committed now, so the tree reads clean rather than showing a
	// staged revert of the content that was just preserved.
	assert.Empty(t, gitOutput(t, dir, "status", "--porcelain"))
	assert.Empty(t, gitOutput(t, dir, "diff", "--cached", "--name-only"))
}

// A repository with no commits yet has no HEAD to seed the preservation
// commit from, or to parent it on.
func TestPreservesInARepositoryWithNoCommitsYet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "guard@example.com")
	git(t, dir, "config", "user.name", "Guard")
	writeAt(t, dir, "new.go", "package a\n")

	notice := preserved(t, dir, "rm new.go")
	assert.Contains(t, notice, "new.go")

	refs := listPreservationRefs(t, dir)
	require.Len(t, refs, 1)

	parents := gitOutput(t, dir, "log", "--pretty=%P", "-1", refs[0])
	assert.Empty(t, parents, "a repository with no HEAD must produce a parentless preservation commit")
	content := gitOutput(t, dir, "show", refs[0]+":new.go")
	assert.Equal(t, "package a", content)
}

// A real bare remote: the preservation ref must actually land there, not
// merely be claimed to.
func TestPreservesAndPushesToOrigin(t *testing.T) {
	dir := remoteRepo(t)
	untrack(t, dir, "scratch.txt")

	notice := preserved(t, dir, "rm scratch.txt")
	assert.Contains(t, notice, "and pushed")

	refs := listPreservationRefs(t, dir)
	require.Len(t, refs, 1)

	remoteURL := gitOutput(t, dir, "config", "--get", "remote.origin.url")
	onRemote := gitOutput(t, remoteURL, "rev-parse", refs[0])
	local := gitOutput(t, dir, "rev-parse", refs[0])
	assert.Equal(t, local, onRemote, "the commit pushed to the bare remote must match the local ref")
}

// No remote at all: the push has nowhere to go, but the local ref still
// holds the content, so the command is still allowed and the notice says
// exactly where the content is and that the push did not happen.
func TestPreservesLocallyWhenPushFails(t *testing.T) {
	dir := newRepo(t) // no "origin" remote configured
	untrack(t, dir, "scratch.txt")

	notice := preserved(t, dir, "rm scratch.txt")
	assert.Contains(t, notice, "The push failed")
	assert.Contains(t, notice, "committed to master")

	refs := listPreservationRefs(t, dir)
	require.Len(t, refs, 1, "the commit must survive on the branch even though the push failed")
}

// ---------------------------------------------------------------------------
// What the preservation commit contains, and what it does to the index.
// ---------------------------------------------------------------------------

// dirtyThreeWays builds the tree every question below is asked about: one
// tracked file staged, one tracked file modified and left unstaged, and one
// file git has never seen.
func dirtyThreeWays(t *testing.T, dir string) {
	t.Helper()
	writeAt(t, dir, "staged.go", "package a\n")
	writeAt(t, dir, "unstaged.go", "package b\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "second")

	writeAt(t, dir, "staged.go", "package a\n// staged\n")
	git(t, dir, "add", "staged.go")
	writeAt(t, dir, "unstaged.go", "package b\n// unstaged\n")
	untrack(t, dir, "new.txt")
}

// The commit must carry the at-risk paths on top of HEAD and nothing else. A
// staged file the command never touches keeps HEAD's content, so preservation
// never smuggles unrelated staged work into a commit nobody wrote.
func TestPreservationCommitsOnlyTheAtRiskPaths(t *testing.T) {
	dir := newRepo(t)
	dirtyThreeWays(t, dir)
	head := gitOutput(t, dir, "rev-parse", "HEAD")

	notice := preserved(t, dir, "rm unstaged.go new.txt")
	assert.Contains(t, notice, "unstaged.go")

	refs := listPreservationRefs(t, dir)
	require.Len(t, refs, 1)
	assert.Equal(t, head, gitOutput(t, dir, "rev-parse", refs[0]+"^"))

	// The two at-risk paths carry their working-tree content.
	assert.Equal(t, "package b\n// unstaged", gitOutput(t, dir, "show", refs[0]+":unstaged.go"))
	assert.Equal(t, "scratch", gitOutput(t, dir, "show", refs[0]+":new.txt"))
	// staged.go was never at risk, so the commit holds HEAD's version of it
	// rather than the one sitting in the index.
	assert.Equal(t, "package a", gitOutput(t, dir, "show", refs[0]+":staged.go"))
	assert.Equal(t, []string{"new.txt", "unstaged.go"},
		splitLines(gitOutput(t, dir, "diff", "--name-only", head, refs[0])))
}

// The staged version of a file is a third state, distinct from HEAD and from
// the working tree, and it lives only in the index. Preserving the working
// tree while overwriting the index with it destroys that state, so both go
// into the commit chain: the index content first, the working tree on top.
func TestPreservationKeepsAStagedVersionDistinctFromTheWorkingTree(t *testing.T) {
	dir := newRepo(t)
	writeAt(t, dir, "app.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "second")

	writeAt(t, dir, "app.go", "package a\n// staged\n")
	git(t, dir, "add", "app.go")
	writeAt(t, dir, "app.go", "package a\n// staged\n// working\n")

	preserved(t, dir, "rm app.go")

	// The tip carries the working tree; its parent carries what was staged.
	assert.Equal(t, "package a\n// staged\n// working", gitOutput(t, dir, "show", "HEAD:app.go"))
	assert.Equal(t, "package a\n// staged", gitOutput(t, dir, "show", "HEAD^:app.go"))
}

// Nothing is staged that was not already, so a tree whose at-risk paths are
// all unstaged produces exactly one commit rather than an empty one plus the
// real one.
func TestPreservationMakesNoEmptyIndexCommit(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	head := gitOutput(t, dir, "rev-parse", "HEAD")

	preserved(t, dir, "rm tracked.go")
	assert.Equal(t, head, gitOutput(t, dir, "rev-parse", "HEAD^"))
}

// The working tree is never written, and every at-risk path is still on disk
// byte for byte: this hook analyses a command and never runs one.
func TestPreservationNeverWritesTheWorkingTree(t *testing.T) {
	dir := newRepo(t)
	dirtyThreeWays(t, dir)
	before := readTree(t, dir)

	preserved(t, dir, "rm staged.go unstaged.go new.txt")
	assert.Equal(t, before, readTree(t, dir))
}

// Preservation commits the at-risk content to the CURRENT BRANCH, so content
// that was uncommitted before the hook ran is committed after it. That is the
// whole point, and it is also the one thing a session sees change: `git
// status` and `git diff --cached` stop reporting what was just preserved.
// This pins the shape of that change rather than leaving it to be
// rediscovered -- and pins that it is confined to the at-risk paths.
func TestPreservationClearsOnlyTheAtRiskPathsFromStatus(t *testing.T) {
	dir := newRepo(t)
	dirtyThreeWays(t, dir)

	require.ElementsMatch(t, []string{"M  staged.go", " M unstaged.go", "?? new.txt"},
		splitLines(gitOutput(t, dir, "status", "--porcelain")))

	preserved(t, dir, "rm unstaged.go new.txt")

	// The two preserved paths are committed now, so they read clean. The
	// staged file nothing threatened is still staged, and still names the
	// same content it did before.
	assert.Equal(t, []string{"M  staged.go"},
		splitLines(gitOutput(t, dir, "status", "--porcelain")))
	assert.Equal(t, []string{"staged.go"},
		splitLines(gitOutput(t, dir, "diff", "--cached", "--name-only")))
	assert.Equal(t, "package a\n// staged", gitOutput(t, dir, "show", ":staged.go"))
}

// splitLines turns git's line output into a slice, with no entry for empty
// output -- an assertion against nil is what "git reported nothing" means.
func splitLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// readTree reads every file in the working tree, ignoring .git, so a test can
// assert the hook wrote none of them.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, p)
		require.NoError(t, relErr)
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		b, readErr := os.ReadFile(p)
		require.NoError(t, readErr)
		files[rel] = string(b)
		return nil
	}))
	return files
}

// ---------------------------------------------------------------------------
// What still denies -- preservation must never widen what this hook allows.
// ---------------------------------------------------------------------------

// push --mirror rewrites every ref at once, so there is no bounded set of
// paths to preserve and no bounded set of commits to verify. It stays an
// unconditional denial regardless of a dirty tree.
func TestPreserveNeverOverridesAnUnconditionalDenial(t *testing.T) {
	dir := remoteRepo(t)
	modify(t, dir)
	r := denied(t, dir, "git push --mirror origin")
	assert.Contains(t, r, "cannot be enumerated")
	assert.Empty(t, listPreservationRefs(t, dir), "an unconditional denial must never preserve first")
}

// A path this hook cannot resolve cannot be named to `git add` either, so it
// is never a candidate for preservation.
func TestPreserveNeverAttemptedForAnUnresolvablePath(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "rm $TARGET")
	assert.Contains(t, r, "cannot resolve")
	assert.Empty(t, listPreservationRefs(t, dir))
}

// A stash entry is deliberately not preserved (see docs/decision-model.md),
// so dropping one still denies exactly as before.
func TestPreserveNeverAttemptedForAStashEntry(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "stash", "push", "-m", "wip")
	r := denied(t, dir, "git stash clear")
	assert.Contains(t, r, "1 stash entry")
	assert.Empty(t, listPreservationRefs(t, dir))
}

// A ref-destroying command (branch -D, a force push, reflog expire, ...) is
// deliberately not preserved either -- it asks a different question, whether
// the commits survive elsewhere -- so it still denies on its own terms.
func TestPreserveNeverAttemptedForARefDestroyingCommand(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "checkout", "-q", "-b", "orphan-feature")
	writeAt(t, dir, "only-here.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "unique work")
	git(t, dir, "checkout", "-q", "master")

	r := denied(t, dir, "git branch -D orphan-feature")
	assert.Contains(t, r, "exist nowhere else")
	assert.Empty(t, listPreservationRefs(t, dir))
}

// Close the loop: a preservation ref is the only copy of what it holds, so
// deleting it must never pass the "does it exist somewhere else" test every
// other ref-destroying command gets judged on.
func TestDeniesDeletingAPreservationRef(t *testing.T) {
	dir := newRepo(t)
	ref := makeStrandedPreservationRef(t, dir)

	r := denied(t, dir, "git update-ref -d "+ref)
	assert.Contains(t, r, "the only copy")

	// Still there: the denial actually stopped it.
	assert.NotEmpty(t, gitOutput(t, dir, "rev-parse", ref))
}

// Same protection against a force push that deletes or overwrites the ref on
// the remote -- the two other write shapes push can take.
func TestDeniesForcePushDeletingOrOverwritingAPreservationRef(t *testing.T) {
	dir := remoteRepo(t)
	ref := makeStrandedPreservationRef(t, dir)

	r := denied(t, dir, "git push --delete origin "+ref)
	assert.Contains(t, r, "the only copy")

	r2 := denied(t, dir, "git push --force origin HEAD:"+ref)
	assert.Contains(t, r2, "the only copy")
}

// git branch -D can never actually name a preservation ref -- a branch name
// always resolves under refs/heads/, never under refs/no-work-loss/ -- so the
// ordinary reachability check on branch deletion is untouched by this
// protection. This pins that boundary rather than assuming it.
func TestBranchDeleteCannotNameAPreservationRef(t *testing.T) {
	dir := newRepo(t)
	ref := makeStrandedPreservationRef(t, dir)

	// git itself refuses this as an invalid branch name, but this hook must
	// not be the thing standing in the way -- it should reach the ordinary
	// reachability path (a branch that never existed is allowed) rather than
	// treating it as naming the protected ref.
	allowed(t, dir, "git branch -D "+ref)
}
