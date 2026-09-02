package main

import "testing"

// The verbs that integrate committed history. Everything they write is
// reachable from a ref, so the only thing they can lose is a change that is in
// no object yet -- which makes a committed tree the whole condition.

func TestRebaseFamilyBlockedDirtyButRecoveryVerbsAllowed(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	denied(t, dir, "git rebase master")
	denied(t, dir, "git merge feature")
	denied(t, dir, "git pull origin master")
	denied(t, dir, "git pull --rebase")
	denied(t, dir, "git cherry-pick abc123")
	allowed(t, dir, "git rebase --abort")
	allowed(t, dir, "git merge --abort")
	allowed(t, dir, "git cherry-pick --continue")
}

// A staged change counts as outstanding: the index is not a commit.
func TestMergeAndPullAreAllowedOnlyOnACommittedTree(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "git merge feature")
	allowed(t, dir, "git pull origin master")

	modify(t, dir)
	denied(t, dir, "git merge feature")
	denied(t, dir, "git pull origin master")

	stage(t, dir)
	denied(t, dir, "git merge feature")
	denied(t, dir, "git pull origin master")
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
