package main

import "testing"

// A session is told to merge the base branch into its PR head, and pr-minder
// merges the base on its own schedule, so integrating that merge is the only
// way the next push fast-forwards. Refusing it left the branch a session is
// required to work on unpushable, with no human present to run the merge.
func TestIntegratingCommittedWorkIsAllowedOnACleanTree(t *testing.T) {
	dir := newRepo(t)

	allowed(t, dir, "git merge origin/master")
	allowed(t, dir, "git merge --no-edit FETCH_HEAD")
	allowed(t, dir, "git pull origin master")
	allowed(t, dir, "git pull --no-rebase origin master")
}

// The other hazard is real and belongs to the destruction half: a merge into a
// tree with uncommitted edits can clobber bytes that exist in no commit.
func TestIntegratingIsStillRefusedWithUncommittedWork(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	denied(t, dir, "git merge origin/master")
	denied(t, dir, "git pull origin master")
}

// Applying a patch authors content that is in no commit, so it stays refused
// even on a clean tree. This is the line the change above must not cross.
func TestApplyingAPatchIsStillRefused(t *testing.T) {
	dir := newRepo(t)

	denied(t, dir, "git apply /tmp/change.patch")
	denied(t, dir, "git am /tmp/change.patch")
}

// A staged change counts as outstanding too: the index is not a commit.
func TestMergeAndPullDenyOverAStagedChange(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	stage(t, dir)

	denied(t, dir, "git merge feature")
	denied(t, dir, "git pull origin master")
	denied(t, dir, "git pull --rebase")
}

// An untracked scratch file is not something a merge can overwrite: git refuses
// the merge instead. Denying over it would refuse the ordinary case where a
// build output or a note sits beside a clean tree.
func TestMergeAndPullIgnoreUntrackedFiles(t *testing.T) {
	dir := newRepo(t)
	untrack(t, dir, "scratch.txt")

	allowed(t, dir, "git merge feature")
	allowed(t, dir, "git pull origin master")
}
