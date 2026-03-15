package main

import (
	"encoding/json"
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

func TestBlockGoBuild(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "go build ./..."},
	}

	code, msg := runEvaluate(t, input)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "BLOCKED")
	assert.Contains(t, msg, "go-toolchain")
}

func TestBlockGoTest(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "go test -v ./..."},
	}

	code, msg := runEvaluate(t, input)
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "BLOCKED")
}

func TestAllowGoToolchain(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "go-toolchain"},
	}

	code, _ := runEvaluate(t, input)
	assert.Equal(t, 0, code)
}

func TestAllowNonBashTool(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Read",
		ToolInput:     ToolInput{Command: "go build"},
	}

	code, _ := runEvaluate(t, input)
	assert.Equal(t, 0, code)
}

func TestAllowEmptyCommand(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{},
	}

	code, _ := runEvaluate(t, input)
	assert.Equal(t, 0, code)
}

func TestAllowInvalidJSON(t *testing.T) {
	code, _ := evaluate([]byte("not json"))
	assert.Equal(t, 0, code)
}

func TestAllowUnrelatedCommand(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "npm install"},
	}

	code, _ := runEvaluate(t, input)
	assert.Equal(t, 0, code)
}

// TestRunFromReader exercises the run() function which reads from an io.Reader
func TestRunFromReader(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "go build -o myapp"},
	}
	data, err := json.Marshal(input)
	require.Nil(t, err)

	code, msg := run(strings.NewReader(string(data)))
	assert.Equal(t, 2, code)
	assert.Contains(t, msg, "BLOCKED")
}
