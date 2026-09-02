package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// run executes the hook as a real process against a payload, with a private
// temp directory so the once-per-session markers do not leak between tests.
func run(t *testing.T, binary, tempDir, stdin string) string {
	t.Helper()

	cmd := exec.Command(binary)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "TMPDIR="+tempDir)

	out, err := cmd.Output()
	require.NoError(t, err, "the hook must always exit 0")
	return string(out)
}

func buildHook(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "docs-nudge")
	build := exec.Command("go", "build", "-o", binary, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "build failed: %s", out)
	return binary
}

func payloadJSON(t *testing.T, session, tool string, input toolInput) string {
	t.Helper()

	body, err := json.Marshal(payload{
		HookEventName: "PreToolUse",
		SessionID:     session,
		ToolName:      tool,
		ToolInput:     input,
	})
	require.NoError(t, err)
	return string(body)
}

func TestTheHookNamesTheSkillForADockerfileEdit(t *testing.T) {
	binary, temp := buildHook(t), t.TempDir()

	out := run(t, binary, temp, payloadJSON(t, "s1", "Write", toolInput{FilePath: "Dockerfile"}))

	var got output
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "PreToolUse", got.HookSpecificOutput.HookEventName)
	assert.Contains(t, got.HookSpecificOutput.AdditionalContext, "/docs:dockerfile")
	assert.Contains(t, got.HookSpecificOutput.AdditionalContext, "reference/dockerfile.md")
	assert.Contains(t, got.HookSpecificOutput.AdditionalContext, "Dockerfile")
}

// The whole point is that it is a reminder, not a gate. A denial here would
// block ordinary work over documentation.
func TestTheHookNeverDenies(t *testing.T) {
	binary, temp := buildHook(t), t.TempDir()

	out := run(t, binary, temp, payloadJSON(t, "s1", "Write", toolInput{FilePath: "compose.yaml"}))

	assert.NotContains(t, out, "permissionDecision")
	assert.NotContains(t, out, "deny")
}

// A reminder repeated on every edit is nagging, and gets skimmed past.
func TestTheSameSkillIsNamedOncePerSession(t *testing.T) {
	binary, temp := buildHook(t), t.TempDir()
	call := payloadJSON(t, "s1", "Write", toolInput{FilePath: "Dockerfile"})

	first := run(t, binary, temp, call)
	second := run(t, binary, temp, call)

	assert.Contains(t, first, "/docs:dockerfile")
	assert.Empty(t, strings.TrimSpace(second), "the second call says nothing")
}

// Two skills are tracked apart: naming one must not silence the other.
func TestNamingOneSkillDoesNotSilenceTheOther(t *testing.T) {
	binary, temp := buildHook(t), t.TempDir()

	run(t, binary, temp, payloadJSON(t, "s1", "Write", toolInput{FilePath: "Dockerfile"}))
	out := run(t, binary, temp, payloadJSON(t, "s1", "Write", toolInput{FilePath: "compose.yaml"}))

	assert.Contains(t, out, "/docs:docker-compose")
}

// Parallel sessions must not silence each other.
func TestSessionsAreTrackedApart(t *testing.T) {
	binary, temp := buildHook(t), t.TempDir()
	file := toolInput{FilePath: "Dockerfile"}

	run(t, binary, temp, payloadJSON(t, "s1", "Write", file))
	out := run(t, binary, temp, payloadJSON(t, "s2", "Write", file))

	assert.Contains(t, out, "/docs:dockerfile")
}

func TestAnUnrelatedCallSaysNothing(t *testing.T) {
	binary, temp := buildHook(t), t.TempDir()

	out := run(t, binary, temp, payloadJSON(t, "s1", "Write", toolInput{FilePath: "main.go"}))

	assert.Empty(t, strings.TrimSpace(out))
}

// A guard that fires on the wrong event, or on input it cannot parse, is worse
// than no guard.
func TestEveryFailurePathIsSilent(t *testing.T) {
	binary, temp := buildHook(t), t.TempDir()

	for name, stdin := range map[string]string{
		"unparseable":   "{not json",
		"empty":         "",
		"another event": `{"hook_event_name":"Stop","tool_name":"Write","tool_input":{"file_path":"Dockerfile"}}`,
		"no tool input": `{"hook_event_name":"PreToolUse","tool_name":"Write"}`,
	} {
		out := run(t, binary, temp, stdin)
		assert.Empty(t, strings.TrimSpace(out), name)
	}
}
