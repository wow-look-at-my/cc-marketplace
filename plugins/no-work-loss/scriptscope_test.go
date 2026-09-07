package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A script file the walk runs as a NEW shell is a program. Its variables, its
// working directory and the text it hands another shell are all its own, and
// this hook does not sandbox the programs it starts. varenv_test.go covers the
// operand half of that rule. These cover the two halves it was missing: where
// the program stands, and what it could not read at all. Every case pairs the
// script form that must run with the same text typed at top level, which must
// still be refused.

// writeScript puts an executable shell script in dir and returns the command
// that runs it as a fresh shell from dir.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\n"+body), 0o755))
	return "cd " + dir + " && ./" + name
}

// The Go toolchain's own src/bootstrap.bash: it takes its destination from
// $1, cds there, and removes paths under it. Every operand is perfectly
// static; what nothing here can know is the directory they land in. The
// destruction half denied that outright while the provenance half had already
// exempted it, which is how one build step stayed unrunnable after the rule
// was written down.
func TestAScriptsUnresolvableWorkingDirectoryIsTheProgramsOwnBusiness(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	untrack(t, dir, "scratch.txt")

	cmd := writeScript(t, dir, "bootstrap.bash", `targ="$1"
cd "$targ"
rm -f .gitignore
rm -rf pkg/bootstrap pkg/obj
`)
	allowed(t, dir, cmd)
}

// The control: the same two statements typed into the command line resolve
// against nothing either, and there they are exactly the ambiguity this
// plugin exists to refuse rather than guess at.
func TestAnUnresolvableWorkingDirectoryStillDeniesInTheCommandText(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	r := denied(t, dir, `cd "$TARG" && rm -f .gitignore`)
	assert.Contains(t, r, "not statically known")
}

// The other control, and the one that matters more: a script standing
// somewhere it named STATICALLY is judged exactly as before, so following a
// script still sees what it deletes.
func TestAScriptsKnownWorkingDirectoryIsStillJudged(t *testing.T) {
	dir := newRepo(t)
	writeAt(t, dir, "sub/keep.txt", "scratch\n")

	cmd := writeScript(t, dir, "clean.bash", "cd sub\nrm -f keep.txt\n")
	notice := preserved(t, dir, cmd)
	assert.Contains(t, notice, "sub/keep.txt")
}

// A script that hands another shell a command built at run time. The text of
// that command is in no file this hook can read, so the walk records a
// blocker -- and a blocker had no origin at all, so one such line refused the
// whole script before any write was judged. That is what made
// `bash tests/run-tests.sh` unrunnable in a sibling plugin.
func TestAScriptsUnreadableInnerShellIsTheProgramsOwnBusiness(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	cmd := writeScript(t, dir, "run-tests.sh", `cmd="echo hello"
sh -c "$cmd"
`)
	allowed(t, dir, cmd)
}

// The control: the same `sh -c` in the command text still denies, because
// there the unreadable program IS what the session asked to run.
func TestAnUnreadableInnerShellStillDeniesInTheCommandText(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)

	r := denied(t, dir, `cmd="echo hello"; sh -c "$cmd"`)
	assert.Contains(t, r, "assembled from an expansion")
}

// A script whose blockers are exempt must still be judged on everything the
// walk COULD read. Without this, exempting the blocker would quietly exempt
// the whole file.
func TestAScriptWithABlockerIsStillJudgedOnWhatItNamesStatically(t *testing.T) {
	dir := newRepo(t)

	cmd := writeScript(t, dir, "mixed.sh", `cmd="echo hello"
sh -c "$cmd"
echo generated > tracked.go
`)
	r := denied(t, dir, cmd)
	assert.Contains(t, r, "tracked.go")
}

// `set -e` and `set +o pipefail` set shell options. Neither binds a name, and
// treating them as a hazard turned off variable resolution for every script
// that opens with one -- which is most scripts. The redirect below resolves
// to a path outside the tree and must be allowed.
func TestShellOptionSetDoesNotDisableVariableResolution(t *testing.T) {
	dir := newRepo(t)
	modify(t, dir)
	scratch := t.TempDir()

	allowed(t, dir, `set -e
set +o pipefail
OUT=`+scratch+`/out.log
echo x > "$OUT"`)
}

// The narrowing's own control: the spellings of `set` that really do rebind
// parameters still turn resolution off, so the same redirect denies.
func TestParameterSettingSetStillDisablesVariableResolution(t *testing.T) {
	scratch := t.TempDir()
	for _, form := range []string{"set -- a b", "set a b", "set"} {
		t.Run(form, func(t *testing.T) {
			dir := newRepo(t)
			modify(t, dir)
			r := denied(t, dir, form+`
OUT=`+scratch+`/out.log
echo x > "$OUT"`)
			assert.Contains(t, r, "cannot resolve")
		})
	}
}
