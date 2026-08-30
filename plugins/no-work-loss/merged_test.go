package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Where the two halves disagree. Each case names both verdicts, because the
// merged behaviour is not obvious from either rule alone and reading it back
// later from one of them would be reading half the answer.

// The destruction half allows a command it cannot parse when nothing in it
// deletes; the provenance half cannot, because an unparsed command's writes are
// unknown and unknown fails closed. The merged verdict is the stricter one.
func TestDeniesUnparseableCommandEvenWithNothingDestructive(t *testing.T) {
	dir := newRepo(t)
	assert.Empty(t, lossOnly(t, dir, "echo `"))
	assert.Contains(t, denied(t, dir, "echo `"), "does not parse as shell")
}

func TestRedirectOntoANewFileIsStillAuthoring(t *testing.T) {
	dir := newRepo(t)
	// Nothing is lost, so the destruction half is content.
	assert.Empty(t, lossOnly(t, dir, "echo x > brand-new.txt"))
	// But a new file with content in it is exactly what Write is for.
	assert.Contains(t, denied(t, dir, "echo x > brand-new.txt"), "brand-new.txt")

	// A build directory is writable by whatever writes it, and a device is not a
	// file in the tree at all.
	allowed(t, dir, "echo x > build/out.log")
	allowed(t, dir, "git status 2>/dev/null")
	allowed(t, dir, "git status > /dev/null 2>&1")
}

// The destruction half's message wins where both object, because losing unsaved
// edits is the more urgent fact and its message names the stash that saves them.
func TestTheDestructionMessageWinsWhenBothHalvesObject(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	reason := denied(t, dir, "echo x > tracked.go")
	assert.Contains(t, reason, "would lose")

	// And the alternative it names must itself survive the other half: `>> file`
	// spares the content but is still a write outside the edit tools.
	assert.NotContains(t, reason, ">> tracked.go")
	assert.Contains(t, reason, "Edit")
}
