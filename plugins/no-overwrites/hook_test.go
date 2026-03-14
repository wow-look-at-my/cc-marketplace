package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Build the hook binary for testing
	if err := exec.Command("go", "build", "-o", "hook", ".").Run(); err != nil {
		panic("failed to build hook: " + err.Error())
	}
	code := m.Run()
	os.Remove("hook")
	os.Exit(code)
}

func runHook(t *testing.T, input HookInput) (int, string) {
	t.Helper()

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	cmd := exec.Command("./hook")
	cmd.Stdin = bytes(data)
	stderr, err := cmd.CombinedOutput()

	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	return exitCode, string(stderr)
}

func bytes(data []byte) *os.File {
	r, w, _ := os.Pipe()
	go func() {
		w.Write(data)
		w.Close()
	}()
	return r
}

func TestAllowNewFile(t *testing.T) {
	// Write to a file that doesn't exist - should be allowed
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Write",
		ToolInput: ToolInput{
			FilePath: filepath.Join(t.TempDir(), "does-not-exist.txt"),
		},
	}

	code, _ := runHook(t, input)
	if code != 0 {
		t.Errorf("expected exit code 0 for new file, got %d", code)
	}
}

func TestBlockExistingFile(t *testing.T) {
	// Create a file first
	tmp := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(tmp, []byte("existing content"), 0644); err != nil {
		t.Fatal(err)
	}

	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Write",
		ToolInput: ToolInput{
			FilePath: tmp,
		},
	}

	code, stderr := runHook(t, input)
	if code != 2 {
		t.Errorf("expected exit code 2 for existing file, got %d", code)
	}
	if stderr == "" {
		t.Error("expected stderr message when blocking")
	}
}

func TestAllowNonWriteTool(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Read",
		ToolInput: ToolInput{
			FilePath: "/etc/hosts",
		},
	}

	code, _ := runHook(t, input)
	if code != 0 {
		t.Errorf("expected exit code 0 for non-Write tool, got %d", code)
	}
}

func TestAllowEmptyFilePath(t *testing.T) {
	input := HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Write",
		ToolInput:     ToolInput{},
	}

	code, _ := runHook(t, input)
	if code != 0 {
		t.Errorf("expected exit code 0 for empty file path, got %d", code)
	}
}

func TestAllowInvalidJSON(t *testing.T) {
	cmd := exec.Command("./hook")
	r, w, _ := os.Pipe()
	w.Write([]byte("not json"))
	w.Close()
	cmd.Stdin = r
	err := cmd.Run()
	if err != nil {
		t.Errorf("expected exit code 0 for invalid JSON, got error: %v", err)
	}
}
