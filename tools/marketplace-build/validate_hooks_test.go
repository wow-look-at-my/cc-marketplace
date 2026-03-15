package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestValidateHookBinaries_MissingBinary(t *testing.T) {
	// Create a temp plugin directory with a plugin.json that references a missing binary
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))

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
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644))

	err := validateHookBinaries(tmpDir)
	require.NotNil(t, err)

	require.Contains(t, err.Error(), "hook binary validation failed")

	require.Contains(t, err.Error(), "run")

}

func TestValidateHookBinaries_BinaryExists(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))

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
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644))

	// Create the binary
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "run"), []byte("#!/bin/sh\n"), 0755))

	err := validateHookBinaries(tmpDir)
	require.Nil(t, err)

}

func TestValidateHookBinaries_SubdirBinary(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))

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
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644))

	// Binary missing — should fail
	err := validateHookBinaries(tmpDir)
	require.NotNil(t, err)

	require.Contains(t, err.Error(), "build/hook")

	// Create the binary — should pass
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "build"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "build", "hook"), []byte("#!/bin/sh\n"), 0755))

	err = validateHookBinaries(tmpDir)
	require.Nil(t, err)

}

func TestValidateHookBinaries_NoHooks(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))

	pluginJSON := `{"name": "test-plugin"}`
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644))

	err := validateHookBinaries(tmpDir)
	require.Nil(t, err)

}

func TestValidateHookBinaries_InlineCommand(t *testing.T) {
	// Commands that don't use ${CLAUDE_PLUGIN_ROOT} should be skipped
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))

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
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0644))

	err := validateHookBinaries(tmpDir)
	require.Nil(t, err)

}
