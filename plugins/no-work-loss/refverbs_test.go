package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A ref-destroying command is only a hazard when the commits exist nowhere
// else. Already pushed, already merged, or sitting on another branch all mean
// nothing is lost, and the command is ordinary work.
//
// remoteRepo builds a repo with a real bare remote so remote-tracking refs are
// genuine rather than simulated.
func remoteRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	base := t.TempDir()
	base, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)
	bare := filepath.Join(base, "remote.git")
	dir := filepath.Join(base, "work")

	git(t, base, "init", "-q", "--bare", bare)
	git(t, base, "clone", "-q", bare, dir)
	git(t, dir, "config", "user.email", "guard@example.com")
	git(t, dir, "config", "user.name", "Guard")
	write(t, dir, "app.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "initial")
	git(t, dir, "push", "-q", "origin", "HEAD:master")
	git(t, dir, "branch", "-q", "--set-upstream-to=origin/master")
	return dir
}

// ---------------------------------------------------------------------------
// branch -D
// ---------------------------------------------------------------------------

func TestAllowsDeletingABranchWhoseCommitsSurviveElsewhere(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "checkout", "-q", "-b", "merged-feature")
	write(t, dir, "feature.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feature")
	git(t, dir, "checkout", "-q", "master")
	git(t, dir, "merge", "-q", "--ff-only", "merged-feature")

	allowed(t, dir, "git branch -D merged-feature") // master holds every commit
}

func TestDeniesDeletingABranchWithCommitsOfItsOwn(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "checkout", "-q", "-b", "orphan-feature")
	write(t, dir, "only-here.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "unique work")
	git(t, dir, "checkout", "-q", "master")

	r := denied(t, dir, "git branch -D orphan-feature")
	assert.Contains(t, r, "exist nowhere else")
}

func TestAllowsDeletingABranchHeldOnlyByATag(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "checkout", "-q", "-b", "tagged")
	write(t, dir, "t.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "tagged work")
	git(t, dir, "tag", "keepsake")
	git(t, dir, "checkout", "-q", "master")

	allowed(t, dir, "git branch -D tagged") // the tag keeps it findable
}

func TestAllowsDeletingANonexistentBranch(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "git branch -D never-existed") // git will error on its own
}

// ---------------------------------------------------------------------------
// push --force
// ---------------------------------------------------------------------------

func TestAllowsForcePushThatIsAFastForward(t *testing.T) {
	dir := remoteRepo(t)
	write(t, dir, "next.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "next")

	allowed(t, dir, "git push --force origin master") // nothing on the remote is rewritten
}

func TestAllowsForcePushWhenTheOldTipSurvivesOnAnotherBranch(t *testing.T) {
	dir := remoteRepo(t)
	write(t, dir, "b.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "second")
	git(t, dir, "push", "-q", "origin", "HEAD:master")

	git(t, dir, "branch", "-q", "rescue") // keeps the commit reachable
	git(t, dir, "reset", "-q", "--hard", "HEAD~1")

	allowed(t, dir, "git push --force origin master")
}

func TestDeniesForcePushThatStrandsTheOnlyCopy(t *testing.T) {
	dir := remoteRepo(t)
	write(t, dir, "b.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "second")
	git(t, dir, "push", "-q", "origin", "HEAD:master")
	git(t, dir, "reset", "-q", "--hard", "HEAD~1") // no other ref holds it now

	r := denied(t, dir, "git push --force origin master")
	assert.Contains(t, r, "exist nowhere else")
	assert.Contains(t, r, "--force-with-lease")
}

// Without a remote-tracking ref there is no local record of what the remote
// holds, so the blast radius is unknown -- which denies.
func TestDeniesForcePushWithNoRemoteTrackingRef(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "git push --force origin master")
	assert.Contains(t, r, "git fetch")
}

func TestAllowsForceWithLeaseAndPlainPush(t *testing.T) {
	dir := remoteRepo(t)
	allowed(t, dir, "git push --force-with-lease origin master")
	allowed(t, dir, "git push origin master")
}

func TestMirrorPushIsRefusedOutright(t *testing.T) {
	dir := remoteRepo(t)
	r := denied(t, dir, "git push --mirror origin")
	assert.Contains(t, r, "cannot be enumerated")
}

func TestForceRefspecIsTreatedAsAForcePush(t *testing.T) {
	dir := remoteRepo(t)
	write(t, dir, "b.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "second")
	git(t, dir, "push", "-q", "origin", "HEAD:master")
	git(t, dir, "reset", "-q", "--hard", "HEAD~1")

	denied(t, dir, "git push origin +master:master")
}

// ---------------------------------------------------------------------------
// push --delete
// ---------------------------------------------------------------------------

func TestAllowsDeletingARemoteBranchAlreadyMerged(t *testing.T) {
	dir := remoteRepo(t)
	git(t, dir, "push", "-q", "origin", "master:refs/heads/copy") // same commits as master
	git(t, dir, "fetch", "-q", "origin")

	allowed(t, dir, "git push --delete origin copy")
}

func TestDeniesDeletingARemoteBranchHoldingTheOnlyCopy(t *testing.T) {
	dir := remoteRepo(t)
	git(t, dir, "checkout", "-q", "-b", "solo")
	write(t, dir, "solo.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "solo work")
	git(t, dir, "push", "-q", "origin", "HEAD:refs/heads/solo")
	git(t, dir, "fetch", "-q", "origin")
	git(t, dir, "checkout", "-q", "master")
	git(t, dir, "branch", "-q", "-D", "solo") // now only the remote has it

	denied(t, dir, "git push --delete origin solo")
}

// ---------------------------------------------------------------------------
// reflog, filter-branch, worktree, update-ref
// ---------------------------------------------------------------------------

func TestAllowsReflogExpireWhenNothingDependsOnIt(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "git reflog expire --expire=now --all")
}

func TestDeniesReflogExpireWhenItHoldsTheOnlyCopy(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "app.go", "package a\n// work\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "committed work")
	git(t, dir, "reset", "-q", "--hard", "HEAD~1") // reachable only via the reflog

	denied(t, dir, "git reflog expire --expire=now --all")
}

func TestFilterBranchAllowedOnlyWhenHistoryIsPushed(t *testing.T) {
	dir := remoteRepo(t)
	allowed(t, dir, "git filter-branch --tree-filter true HEAD")

	write(t, dir, "unpushed.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "not pushed yet")
	denied(t, dir, "git filter-branch --tree-filter true HEAD")
}

func TestUpdateRefDeleteFollowsReachability(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "branch", "-q", "keeper") // same commits as master
	allowed(t, dir, "git update-ref -d refs/heads/keeper")

	git(t, dir, "checkout", "-q", "-b", "solo")
	write(t, dir, "solo.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "solo")
	git(t, dir, "checkout", "-q", "master")
	denied(t, dir, "git update-ref -d refs/heads/solo")
}

func TestWorktreeRemoveForceChecksThatWorktree(t *testing.T) {
	dir := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt")
	git(t, dir, "worktree", "add", "-q", wt, "-b", "wtbranch")

	allowed(t, dir, "git worktree remove --force "+wt)

	write(t, wt, "scratch.txt", "uncommitted\n")
	denied(t, dir, "git worktree remove --force "+wt)
}

// ---------------------------------------------------------------------------
// The safe spellings stay usable.
// ---------------------------------------------------------------------------

func TestAllowsTheSafeSpellingsOfThoseVerbs(t *testing.T) {
	dir := newRepo(t)
	for _, c := range []string{
		"git branch -d merged-feature",
		"git branch -m old new",
		"git branch feature",
		"git reflog",
		"git reflog show HEAD",
		"git update-ref refs/heads/x HEAD",
		"git worktree list",
		"git worktree prune",
	} {
		allowed(t, dir, c)
	}
}

func TestSubmoduleAndCheckoutIndexForceForms(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "git submodule deinit -f vendor/lib")
	denied(t, dir, "git submodule update --force")
	denied(t, dir, "git checkout-index -f -a")

	allowed(t, dir, "git submodule update --init")
	allowed(t, dir, "git submodule status")
	allowed(t, dir, "git checkout-index -a")
}

func TestAllowsUnknownGitVerbs(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "git frobnicate --wildly")
	allowed(t, dir, "git lfs pull")
}

func TestAliasChainsResolveAndTerminate(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "config", "alias.one", "two")
	git(t, dir, "config", "alias.two", "reset --hard")
	denied(t, dir, "git one")

	git(t, dir, "config", "alias.loop", "loop")
	require.Empty(t, ask(t, dir, "git loop"), "a self-referential alias must terminate, not hang")
}

func TestHarmlessAliasesStayAllowed(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "config", "alias.st", "status -sb")
	git(t, dir, "config", "alias.lg", "log --oneline --graph")
	git(t, dir, "config", "alias.save", "!git add -A && git commit -m wip")
	allowed(t, dir, "git st")
	allowed(t, dir, "git lg")
	allowed(t, dir, "git save")
}
