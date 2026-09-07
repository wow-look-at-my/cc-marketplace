package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A redirect is only this hook's business when it can empty a file holding
// content no git object has. Two shapes never can: a device target swallows
// what it is given, and a descriptor other than stdout carries a stream rather
// than the command's output. Each case below pairs the shape that must pass
// with the one that must still be refused.

// The reported incident, verbatim in shape. Nothing here puts a file at risk,
// and the working directory the redirect would resolve against is unknowable,
// so the old verdict named an ambiguity about a path that needs no directory
// to resolve at all.
func TestStderrDiscardInsideAnUnresolvableSubshellIsAllowed(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	untrack(t, dir, "scratch.txt")

	allowed(t, dir, `for d in a b; do (cd "$d" && git status -sb 2>/dev/null); done`)
}

// The control on the same shape: send STDOUT to a real file in a directory
// nothing can resolve and the ambiguity is genuine again, because that file
// may be one the tree holds.
func TestStdoutIntoAnUnresolvableDirectoryStillDenies(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	r := denied(t, dir, `for d in a b; do (cd "$d" && git status -sb > status.log); done`)
	assert.Contains(t, r, "status.log")
}

// Every device spelling, at stdout and at stderr, over a tree dirty enough
// that a real truncation would be refused.
func TestDeviceTargetsAreNeverLosable(t *testing.T) {
	for _, cmd := range []string{
		"echo x > /dev/null",
		"echo x 2>/dev/null",
		"echo x > /dev/stdout",
		"echo x 2> /dev/stderr",
		"echo x > /dev/tty",
		"echo x > /dev/fd/3",
		"echo x &> /dev/null",
		"git status 2>&1",
	} {
		t.Run(cmd, func(t *testing.T) {
			dir := newRepo(t)
			modify(t, dir)
			untrack(t, dir, "scratch.txt")
			allowed(t, dir, cmd)
		})
	}
}

// The control every device case above rests on: the same redirect aimed at a
// tracked file with unsaved edits is refused, and the refusal names the file.
func TestStdoutIntoATrackedFileStillDenies(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	r := denied(t, dir, "echo x > tracked.go")
	assert.Contains(t, r, "tracked.go")
}

// A stderr capture into a real file inside the tree is still refused -- but by
// the half whose question it actually answers. Content reaching a file the tree
// holds is authored content whichever stream filled it, so the provenance half
// names the file and points at the edit tools. The message quotes the
// descriptor the reader wrote rather than reporting it as a plain `>`.
func TestStderrIntoATrackedFileIsRefusedAsAWriteRatherThanALoss(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	r := denied(t, dir, "echo x 2> tracked.go")
	assert.Contains(t, r, "tracked.go")
	assert.Contains(t, r, "2> tracked.go")
	assert.Contains(t, r, useTheTools)
}

// The two halves, asked separately, on the two descriptors. This is the pair
// the whole change turns on: stdout into a dirty tracked file is content this
// hook must save first, and stderr into the same file is not.
func TestOnlyAStdoutRedirectIsAWorkLossFinding(t *testing.T) {
	stdoutRepo := newRepo(t)
	modify(t, stdoutRepo)
	reason, notices := lossOnlyNotices(t, stdoutRepo, "echo x > tracked.go")
	require.Empty(t, reason)
	require.NotEmpty(t, notices, "a stdout truncation of a dirty tracked file must be preserved first")
	assert.Contains(t, notices[0], "tracked.go")

	stderrRepo := newRepo(t)
	modify(t, stderrRepo)
	reason, notices = lossOnlyNotices(t, stderrRepo, "echo x 2> tracked.go")
	assert.Empty(t, reason)
	assert.Empty(t, notices, "stderr puts no file the tree holds at risk, so nothing is preserved for it")
}
