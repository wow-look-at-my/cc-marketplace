package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	fn()
	require.NoError(t, w.Close())
	os.Stdout = old
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// The denial payload is a contract with the CLI, which rejects a body whose
// hookEventName is not the event it dispatched. Asserting on the raw keys is
// the point: unmarshalling into the producing struct would agree with itself
// no matter what the field names were.
func TestDenialPayloadMatchesTheDocumentedSchema(t *testing.T) {
	out := captureStdout(t, func() { emitDeny("blocked: something\nrun: git status") })

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &raw))

	inner, ok := raw["hookSpecificOutput"].(map[string]any)
	require.True(t, ok, "payload must nest under hookSpecificOutput: %s", out)
	assert.Equal(t, "PreToolUse", inner["hookEventName"])
	assert.Equal(t, "deny", inner["permissionDecision"])
	assert.Equal(t, "blocked: something\nrun: git status", inner["permissionDecisionReason"])
	assert.Len(t, inner, 3, "no stray keys: %s", out)
}

// Nothing is written for an allowed command, which is what leaves the normal
// permission flow untouched.
func TestAllowWritesNothingAtAll(t *testing.T) {
	dir := newRepo(t)
	out := captureStdout(t, func() {
		if r := ask(t, dir, "git status"); r != "" {
			emitDeny(r)
		}
	})
	assert.Empty(t, out)
}

func TestInternalErrorDeniesAndNamesTheVerb(t *testing.T) {
	r := internalErrorReason("git reset --hard")
	assert.Contains(t, r, "blocked:")
	assert.Contains(t, r, "git reset --hard")
	assert.Contains(t, r, "run: ")
}

// The recover in evaluate is the last line of defence: a panic while checking
// a destructive command must deny, and a panic elsewhere must not.
func TestPanicPathDeniesOnlyForDestructiveCommands(t *testing.T) {
	label, destructive := destructiveKeyword("git reset --hard origin/master")
	require.True(t, destructive)
	assert.Equal(t, "git reset --hard", label)

	_, destructive = destructiveKeyword("npm run build")
	assert.False(t, destructive)
}

func TestEmptyCommandIsIgnored(t *testing.T) {
	in := hookInput{HookEventName: "PreToolUse", ToolName: "Bash", Cwd: t.TempDir()}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Empty(t, decide(raw))
}
