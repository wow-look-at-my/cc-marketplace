package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Resolving a variable this hook can prove holds one literal value.
// newRepo, modify, denied and allowed live in guard_test.go.
// ---------------------------------------------------------------------------

// The reported incident: two literal assignments feed a redirect target, and
// the target lands outside any repository. Once the path resolves, the
// existing repo-scoping already allows it -- no new carve-out was needed.
func TestAllowsRedirectBuiltFromSequentialLiteralAssignments(t *testing.T) {
	dir := newRepo(t)
	// A dirty tree proves the point: before this fix, the unresolved "$OUT"
	// denied regardless of where it pointed, so a clean tree would pass this
	// test for the wrong reason.
	modify(t, dir)
	scratch := t.TempDir()
	allowed(t, dir, `V=2.1.263
OUT="`+scratch+`/cli-$V.js"
echo x > "$OUT"`)
}

// The same resolution must still let a genuinely destructive command through
// when the resolved path lands on real uncommitted work.
func TestDeniesRedirectBuiltFromSequentialLiteralAssignmentsOntoTrackedFile(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	r := denied(t, dir, `NAME=tracked.go
echo x > "$NAME"`)
	assert.Contains(t, r, "tracked.go")
}

// A name assigned twice must never be trusted, even when both assignments
// are in the safe sequential flow this hook otherwise resolves.
func TestReassignedVariableStaysUnresolvable(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "V=a\nV=b\nrm $V")
	assert.Contains(t, r, "cannot resolve")
}

// A name assigned inside a conditional can end up not assigned at all, so a
// later use must not trust it.
func TestVariableAssignedInsideAConditionalStaysUnresolvable(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "if true; then V=x; fi\nrm $V")
	assert.Contains(t, r, "cannot resolve")
}

// A name assigned inside a loop takes a different value on every iteration,
// so no single value survives the loop.
func TestForLoopVariableStaysUnresolvable(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "for V in a b c; do :; done\nrm $V")
	assert.Contains(t, r, "cannot resolve")
}

// read can set a variable this scan cannot see, which is exactly the case
// the mutating-command abort exists for: resolution turns off for the whole
// command rather than trusting a value read never touched.
func TestReadDisablesResolutionForTheWholeCommand(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "read V\nrm $V")
	assert.Contains(t, r, "cannot resolve")
}

// A prefix assignment is scoped to the one command it rides on and must
// never leak into a later statement.
func TestPrefixAssignmentDoesNotPersist(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "V=a echo hi\nrm $V")
	assert.Contains(t, r, "cannot resolve")
}

// A modifier on the expansion -- a default, an index, a slice -- means the
// value depends on something this scan does not evaluate, even when the
// bare name behind it is otherwise resolvable.
func TestParamExpansionWithAModifierStaysUnresolvable(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "V=x\nrm ${V:-fallback}")
	assert.Contains(t, r, "cannot resolve")
}

// The remedy in an unresolved-path denial must match what the finding itself
// would have recommended, not a single message reused for every command.
func TestUnresolvedPathDenialCarriesTheFindingsOwnRemedy(t *testing.T) {
	dir := newRepo(t)
	rmDenial := denied(t, dir, "rm $TARGET")
	assert.Contains(t, rmDenial, "git stash push -u")
	assert.NotContains(t, rmDenial, "commit -m wip")

	truncateDenial := denied(t, dir, `echo x > "$TARGET"`)
	assert.Contains(t, truncateDenial, "commit the file first")
	assert.NotContains(t, truncateDenial, "commit -m wip")
}

// A word that resolves to no literal text at all must not be quoted as an
// empty string, which reads as a real, empty path rather than as what it
// actually is: an expansion this hook never evaluated.
func TestUnresolvedPathDenialNamesAnExpansionRatherThanAnEmptyString(t *testing.T) {
	dir := newRepo(t)
	r := denied(t, dir, "rm $TARGET")
	assert.Contains(t, r, "an unresolved expansion")
	assert.NotContains(t, r, `("")`)
}
