package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every case in this suite is a pair: a command that must be denied because it
// writes inside the working tree, and a control that must be allowed because the
// same command writes somewhere else. The pair is what makes a case load-bearing
// -- a rule that denied on the command's shape alone would pass the first half
// and fail the second -- and each denial is additionally required to NAME the
// path or route it stopped, so a deny arriving from an unrelated rule cannot
// satisfy the assertion.

// newTree builds a working tree: a repository root with source files, a build
// directory the rules deliberately allow, and no relationship to the outside
// directory the controls write to.
func newTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".git", "src", "build", "node_modules", ".claude"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	for _, f := range []string{"src.txt", "notes.md", "src/app.go", "build/app.js", "node_modules/dep.js"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, f), []byte("original\n"), 0o644))
	}
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	return root
}

// outsideTree is a directory the hook does not protect. It is a sibling of the
// tree, never a child, so a write into it is genuinely outside.
func outsideTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src.txt"), []byte("original\n"), 0o644))
	return dir
}

// ask drives the real entry point rather than a rewritten copy of it, so the
// payload shape is part of what the suite covers.
func ask(t *testing.T, cwd, command string) string {
	t.Helper()
	return askTool(t, "Bash", cwd, map[string]any{"command": command})
}

func askTool(t *testing.T, tool, cwd string, input map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       tool,
		"cwd":             cwd,
		"tool_input":      input,
	})
	require.NoError(t, err)
	return decide(raw)
}

// fill substitutes the two directories into a case's command. Named tokens
// rather than printf verbs, because a shell command is full of % and & and a
// format string mangles the ones it does not understand.
func fill(cmd, root, out string) string {
	return strings.NewReplacer("{{tree}}", root, "{{out}}", out).Replace(cmd)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
}

// lossOnly asks the destruction half by itself. A handful of cases below are
// about hazard classes -- which content a verb spares -- and the provenance half
// answers a different question about the same command, so those tests name the
// half they mean instead of asserting on the merged verdict.
func lossOnly(t *testing.T, cwd, command string) string {
	t.Helper()
	return evaluateLoss(command, cwd)
}

func decideWithEvent(t *testing.T, event, tool, cwd, command string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": event,
		"tool_name":       tool,
		"cwd":             cwd,
		"tool_input":      map[string]any{"command": command},
	})
	require.NoError(t, err)
	return decide(raw)
}

// captureStdout and the payload assertion live in hookio_test.go.
