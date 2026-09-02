package main

import "testing"

// The clean-tree and dirty-tree halves of merge and pull live in guard_test.go.
// These two pin the edges of what a committed tree means.

// A staged change counts as outstanding: the index is not a commit.
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
