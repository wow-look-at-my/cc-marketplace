package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHookBinaries_MissingBinary(t *testing.T) {
	// Create a temp plugin directory with a plugin.json that references a missing binary
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}

	pluginJSON := `{
		"name": "test-plugin",
		"hooks": {
			"PermissionRequest": [
				{
					"matcher": "Bash",
					"hooks": [
						{
							"type": "command",
							"command": "${CLAUDE_PLUGIN_ROOT}/run"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		t.Fatal(err)
	}

	err := validateHookBinaries(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing hook binary, got nil")
	}
	if !strings.Contains(err.Error(), "hook binary validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "run") {
		t.Fatalf("error should mention the missing binary 'run': %v", err)
	}
}

func TestValidateHookBinaries_BinaryExists(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}

	pluginJSON := `{
		"name": "test-plugin",
		"hooks": {
			"PermissionRequest": [
				{
					"matcher": "Bash",
					"hooks": [
						{
							"type": "command",
							"command": "${CLAUDE_PLUGIN_ROOT}/run"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the binary
	if err := os.WriteFile(filepath.Join(tmpDir, "run"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	err := validateHookBinaries(tmpDir)
	if err != nil {
		t.Fatalf("expected no error when binary exists, got: %v", err)
	}
}

func TestValidateHookBinaries_SubdirBinary(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}

	pluginJSON := `{
		"name": "test-plugin",
		"hooks": {
			"PermissionRequest": [
				{
					"matcher": "Bash",
					"hooks": [
						{
							"type": "command",
							"command": "${CLAUDE_PLUGIN_ROOT}/build/hook"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Binary missing — should fail
	err := validateHookBinaries(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing build/hook binary")
	}
	if !strings.Contains(err.Error(), "build/hook") {
		t.Fatalf("error should mention 'build/hook': %v", err)
	}

	// Create the binary — should pass
	if err := os.MkdirAll(filepath.Join(tmpDir, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "build", "hook"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	err = validateHookBinaries(tmpDir)
	if err != nil {
		t.Fatalf("expected no error when build/hook exists, got: %v", err)
	}
}

func TestValidateHookBinaries_NoHooks(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}

	pluginJSON := `{"name": "test-plugin"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		t.Fatal(err)
	}

	err := validateHookBinaries(tmpDir)
	if err != nil {
		t.Fatalf("expected no error for plugin without hooks, got: %v", err)
	}
}

func TestValidateHookBinaries_InlineCommand(t *testing.T) {
	// Commands that don't use ${CLAUDE_PLUGIN_ROOT} should be skipped
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}

	pluginJSON := `{
		"name": "test-plugin",
		"hooks": {
			"PermissionRequest": [
				{
					"matcher": "mcp__*",
					"hooks": [
						{
							"type": "command",
							"command": "printf '{\"decision\":{\"behavior\":\"allow\"}}'"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644); err != nil {
		t.Fatal(err)
	}

	err := validateHookBinaries(tmpDir)
	if err != nil {
		t.Fatalf("expected no error for inline commands, got: %v", err)
	}
}
