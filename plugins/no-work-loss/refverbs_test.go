package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Commands that destroy refs, commits or the reflog. These are refused on a
// clean tree too, because no working-tree state could make them recoverable.
func TestDeniesRefDestroyingCommandsRegardlessOfState(t *testing.T) {
	dir := newRepo(t)
	for _, tc := range []struct{ cmd, wants string }{
		{"git branch -D feature", "-d"},
		{"git branch --delete --force feature", "-d"},
		{"git branch -M master", "-m"},
		{"git push --delete origin feature", "--delete"},
		{"git push --mirror origin", "--force-with-lease"},
		{"git reflog expire --expire=now --all", "reflog"},
		{"git reflog delete refs/heads/master@{0}", "reflog"},
		{"git update-ref -d refs/heads/feature", "tag"},
		{"git filter-branch --tree-filter true HEAD", "branch"},
		{"git worktree remove --force ../wt", "worktree remove"},
	} {
		r := denied(t, dir, tc.cmd)
		assert.Contains(t, r, tc.wants, "denial for %q should point somewhere useful", tc.cmd)
	}
}

// The safe spellings of the same verbs stay usable, which is what keeps the
// guard from being switched off.
func TestAllowsTheSafeSpellingsOfThoseVerbs(t *testing.T) {
	dir := newRepo(t)
	for _, c := range []string{
		"git branch -d merged-feature",
		"git branch -m old new",
		"git branch feature",
		"git push origin master",
		"git reflog",
		"git reflog show HEAD",
		"git update-ref refs/heads/x HEAD",
		"git worktree remove ../wt",
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

// An unknown git verb that is not an alias is a git error, not a hazard.
func TestAllowsUnknownGitVerbs(t *testing.T) {
	dir := newRepo(t)
	allowed(t, dir, "git frobnicate --wildly")
	allowed(t, dir, "git lfs pull")
}

// An alias chain still resolves, and a self-referential one terminates rather
// than looping.
func TestAliasChainsResolveAndTerminate(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	git(t, dir, "config", "alias.one", "two")
	git(t, dir, "config", "alias.two", "reset --hard")
	denied(t, dir, "git one")

	git(t, dir, "config", "alias.loop", "loop")
	require.Empty(t, ask(t, dir, "git loop"), "a self-referential alias must terminate, not hang")
}

// An alias that is harmless stays harmless -- resolving aliases must not turn
// every custom verb into a denial.
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
