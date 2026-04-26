package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func runEvaluate(t *testing.T, input HookInput) (int, string) {
	t.Helper()
	data, err := json.Marshal(input)
	require.Nil(t, err)
	return evaluate(data)
}

func TestAllowNewFile(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Write",
		ToolInput: ToolInput{
			FilePath: filepath.Join(t.TempDir(), "does-not-exist.txt"),
		},
	}

	code, _ := runEvaluate(t, input)
	assert.Equal(t, 0, code)
}

func TestBlockExistingFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "existing.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("existing content"), 0644))

	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Write",
		ToolInput: ToolInput{
			FilePath: tmp,
		},
	}

	code, msg := runEvaluate(t, input)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "BLOCKED")
	assert.Contains(t, msg, "Edit tool")
}

func TestAllowNonWriteTool(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Read",
		ToolInput: ToolInput{
			FilePath: "/etc/hosts",
		},
	}

	code, _ := runEvaluate(t, input)
	assert.Equal(t, 0, code)
}

func TestAllowEmptyFilePath(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Write",
		ToolInput:     ToolInput{},
	}

	code, _ := runEvaluate(t, input)
	assert.Equal(t, 0, code)
}

func TestAllowInvalidJSON(t *testing.T) {
	code, _ := evaluate([]byte("not json"))
	assert.Equal(t, 0, code)
}

// TestRunFromReader exercises the run() function which reads from an io.Reader
func TestRunFromReader(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "via-reader.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("content"), 0644))

	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Write",
		ToolInput:     ToolInput{FilePath: tmp},
	}
	data, err := json.Marshal(input)
	require.Nil(t, err)

	code, msg := run(strings.NewReader(string(data)))
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "BLOCKED")
}
