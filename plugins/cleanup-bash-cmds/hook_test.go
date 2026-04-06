package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

// --- cleanCommand unit tests ---

func TestCleanTrailing2Redirect(t *testing.T) {
	assert.Equal(t, "ls -la", cleanCommand("ls -la 2>&1"))
}

func TestCleanTrailingDevNull(t *testing.T) {
	assert.Equal(t, "cat file.txt", cleanCommand("cat file.txt 2>/dev/null"))
}

func TestCleanTrailingOrTrue(t *testing.T) {
	assert.Equal(t, "rm -f foo", cleanCommand("rm -f foo || true"))
}

func TestCleanSetEPrefix(t *testing.T) {
	assert.Equal(t, "npm test", cleanCommand("set -e; npm test"))
}

func TestCleanSetEWithAmpersand(t *testing.T) {
	assert.Equal(t, "npm test", cleanCommand("set -e && npm test"))
}

func TestCleanWhitespace(t *testing.T) {
	assert.Equal(t, "ls -la", cleanCommand("  ls -la  "))
}

func TestCleanTrailingHead(t *testing.T) {
	assert.Equal(t, "cat file.txt", cleanCommand("cat file.txt | head"))
}

func TestCleanTrailingHeadWithN(t *testing.T) {
	assert.Equal(t, "cat file.txt", cleanCommand("cat file.txt | head -5"))
}

func TestCleanTrailingHeadWithDashN(t *testing.T) {
	assert.Equal(t, "cat file.txt", cleanCommand("cat file.txt | head -n 20"))
}

func TestCleanTrailingTail(t *testing.T) {
	assert.Equal(t, "cat file.txt", cleanCommand("cat file.txt | tail"))
}

func TestCleanTrailingTailWithN(t *testing.T) {
	assert.Equal(t, "cat file.txt", cleanCommand("cat file.txt | tail -10"))
}

func TestCleanTrailingTailWithPlusN(t *testing.T) {
	assert.Equal(t, "cat file.txt", cleanCommand("cat file.txt | tail -n +5"))
}

func TestCleanMultiplePatterns(t *testing.T) {
	assert.Equal(t, "ls -la", cleanCommand("set -e; ls -la 2>&1"))
}

func TestCleanChainedSuffixes(t *testing.T) {
	assert.Equal(t, "cmd", cleanCommand("cmd 2>&1 || true"))
}

func TestCleanAllPatterns(t *testing.T) {
	assert.Equal(t, "npm install", cleanCommand("set -e; npm install 2>&1 || true"))
}

func TestCleanHeadAfterRedirect(t *testing.T) {
	assert.Equal(t, "ls -la", cleanCommand("ls -la 2>&1 | head -20"))
}

// --- Edge cases: patterns should NOT be removed ---

func TestPreserve2RedirectInMiddle(t *testing.T) {
	assert.Equal(t, "cmd 2>&1 | grep foo", cleanCommand("cmd 2>&1 | grep foo"))
}

func TestPreserveOrTrueInMiddle(t *testing.T) {
	assert.Equal(t, "cmd || true && echo done", cleanCommand("cmd || true && echo done"))
}

func TestPreserveDevNullNotTrailing(t *testing.T) {
	assert.Equal(t, "cmd 2>/dev/null | wc", cleanCommand("cmd 2>/dev/null | wc"))
}

func TestPreserveHeadNotTrailing(t *testing.T) {
	assert.Equal(t, "cmd | head -5 | grep foo", cleanCommand("cmd | head -5 | grep foo"))
}

func TestPreserveTailNotTrailing(t *testing.T) {
	assert.Equal(t, "cmd | tail -10 | grep foo", cleanCommand("cmd | tail -10 | grep foo"))
}

func TestEmptyCommand(t *testing.T) {
	assert.Equal(t, "", cleanCommand(""))
}

func TestNoChangePassthrough(t *testing.T) {
	assert.Equal(t, "git status", cleanCommand("git status"))
}

// --- Integration tests (full evaluate path) ---

func runEvaluate(t *testing.T, input HookInput) (int, string, string) {
	t.Helper()
	data, err := json.Marshal(input)
	require.Nil(t, err)
	return evaluate(data)
}

func TestRewriteOutputsJSON(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "ls -la 2>&1"},
	}

	code, stderr, stdout := runEvaluate(t, input)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.NotEmpty(t, stdout)

	var out HookOutput
	err := json.Unmarshal([]byte(stdout), &out)
	require.Nil(t, err)
	assert.Equal(t, "PreToolUse", out.HookSpecificOutput.HookEventName)
	assert.Equal(t, "allow", out.HookSpecificOutput.PermissionDecision)
	assert.Equal(t, "ls -la", out.HookSpecificOutput.UpdatedInput.Command)
}

func TestPassthroughNoOutput(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "git status"},
	}

	code, stderr, stdout := runEvaluate(t, input)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Empty(t, stdout)
}

func TestPreservesTimeout(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "npm install 2>&1", Timeout: 120000},
	}

	code, _, stdout := runEvaluate(t, input)
	assert.Equal(t, 0, code)

	var out HookOutput
	err := json.Unmarshal([]byte(stdout), &out)
	require.Nil(t, err)
	assert.Equal(t, "npm install", out.HookSpecificOutput.UpdatedInput.Command)
	assert.Equal(t, 120000, out.HookSpecificOutput.UpdatedInput.Timeout)
}

func TestPreservesDescription(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "npm install 2>&1", Description: "Install deps"},
	}

	code, _, stdout := runEvaluate(t, input)
	assert.Equal(t, 0, code)

	var out HookOutput
	err := json.Unmarshal([]byte(stdout), &out)
	require.Nil(t, err)
	assert.Equal(t, "Install deps", out.HookSpecificOutput.UpdatedInput.Description)
}

func TestNonBashToolPassthrough(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Read",
		ToolInput:     ToolInput{Command: "ls 2>&1"},
	}

	code, _, stdout := runEvaluate(t, input)
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
}

func TestEmptyCommandPassthrough(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{},
	}

	code, _, stdout := runEvaluate(t, input)
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
}

func TestInvalidJSONPassthrough(t *testing.T) {
	code, _, stdout := evaluate([]byte("not json"))
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
}

func TestRunFromReader(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "ls -la 2>&1 || true"},
	}
	data, err := json.Marshal(input)
	require.Nil(t, err)

	code, _, stdout := run(strings.NewReader(string(data)))
	assert.Equal(t, 0, code)
	assert.NotEmpty(t, stdout)

	var out HookOutput
	err = json.Unmarshal([]byte(stdout), &out)
	require.Nil(t, err)
	assert.Equal(t, "ls -la", out.HookSpecificOutput.UpdatedInput.Command)
}
