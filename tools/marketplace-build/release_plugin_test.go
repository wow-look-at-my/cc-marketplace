package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestContainsTemplate(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"template json", "plugin.template.json", true},
		{"template md", "SKILL.template.md", true},
		{"mcp template", ".mcp.template.json", true},
		{"README template", "README.template.md", true},
		{"normal json", "plugin.json", false},
		{"normal md", "README.md", false},
		{"empty", "", false},
		{"just template", ".template.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, containsTemplate(tt.filename))
		})
	}
}

func TestIsGoSource(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"go file", "hook.go", true},
		{"go mod", "go.mod", true},
		{"go sum", "go.sum", true},
		{"yaml", "config.yaml", false},
		{"json", "plugin.json", false},
		{"md", "README.md", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isGoSource(tt.filename))
		})
	}
}

func TestCookJSONForRelease_PluginJSON(t *testing.T) {
	input := `{
		"$schema": "../../schema.json",
		"name": "my-plugin",
		"description": "test",
		"mh": {
			"include_in_marketplace": true
		}
	}`

	result, err := cookJSONForRelease([]byte(input), 42, ".claude-plugin/plugin.json")
	require.NoError(t, err)

	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &obj))

	_, hasSchema := obj["$schema"]
	require.False(t, hasSchema)
	_, hasMH := obj["mh"]
	require.False(t, hasMH)
	require.Equal(t, "42", obj["version"])
	require.Equal(t, "my-plugin", obj["name"])
	require.Equal(t, "test", obj["description"])
}

func TestCookJSONForRelease_NonPluginJSON(t *testing.T) {
	input := `{
		"$schema": "schema.json",
		"mcpServers": {"server": {}}
	}`

	result, err := cookJSONForRelease([]byte(input), 5, ".mcp.json")
	require.NoError(t, err)

	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &obj))

	_, hasSchema := obj["$schema"]
	require.False(t, hasSchema)
	_, hasVersion := obj["version"]
	require.False(t, hasVersion)
	_, hasMCP := obj["mcpServers"]
	require.True(t, hasMCP)
}

func TestCookJSONForRelease_InvalidJSON(t *testing.T) {
	input := []byte("{bad json")
	result, err := cookJSONForRelease(input, 1, "plugin.json")
	require.NotNil(t, err)
	require.Equal(t, input, result)
}

func TestCookPluginForRelease(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "commands"), 0755))

	pluginJSON := `{"$schema": "../../schema.json", "name": "test", "mh": {"include_in_marketplace": true}}`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(pluginJSON), 0644))

	// Template file — should be skipped
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "commands", "cmd.template.md"), []byte("template"), 0644))

	// Normal files — should be copied
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "commands", "real-cmd.md"), []byte("real command"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("readme"), 0644))

	// Non-JSON file in .claude-plugin
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "config.yaml"), []byte("key: val"), 0644))

	// Go source files — should be skipped
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "cmd"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "cmd", "main.go"), []byte("package main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "go.sum"), []byte(""), 0644))

	meta := releaseMetadata{
		SourceCommit: "abc123",
		SourceURL:    "https://github.com/test/repo/tree/abc123/plugins/test",
		BuiltAt:      "2026-01-01T00:00:00Z",
	}

	err := cookPluginForRelease(srcDir, dstDir, 7, meta)
	require.NoError(t, err)

	// mh.plugin.json written
	metaData, err := os.ReadFile(filepath.Join(dstDir, "mh.plugin.json"))
	require.NoError(t, err)
	require.Contains(t, string(metaData), "abc123")

	// plugin.json cooked
	pj, err := os.ReadFile(filepath.Join(dstDir, ".claude-plugin", "plugin.json"))
	require.NoError(t, err)
	var pjObj map[string]interface{}
	require.NoError(t, json.Unmarshal(pj, &pjObj))
	require.Equal(t, "7", pjObj["version"])
	_, hasSchema := pjObj["$schema"]
	require.False(t, hasSchema)
	_, hasMH := pjObj["mh"]
	require.False(t, hasMH)

	// Template file skipped
	_, err = os.Stat(filepath.Join(dstDir, "commands", "cmd.template.md"))
	require.True(t, os.IsNotExist(err))

	// Normal files copied
	data, err := os.ReadFile(filepath.Join(dstDir, "commands", "real-cmd.md"))
	require.NoError(t, err)
	require.Equal(t, "real command", string(data))

	data, err = os.ReadFile(filepath.Join(dstDir, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "readme", string(data))

	// Non-JSON copied as-is
	data, err = os.ReadFile(filepath.Join(dstDir, ".claude-plugin", "config.yaml"))
	require.NoError(t, err)
	require.Equal(t, "key: val", string(data))

	// Go source files skipped
	_, err = os.Stat(filepath.Join(dstDir, "cmd", "main.go"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dstDir, "go.mod"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dstDir, "go.sum"))
	require.True(t, os.IsNotExist(err))
}

func TestCookPluginForRelease_PreservesHooks(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "build"), 0755))

	pluginJSON := `{
		"$schema": "../../schema.json",
		"name": "cleanup-bash-cmds",
		"hooks": {
			"PreToolUse": [{
				"matcher": "Bash",
				"hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/build/hook"}]
			}]
		},
		"mh": {"include_in_marketplace": true}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(pluginJSON), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "build", "hook"), []byte("#!/bin/sh"), 0755))

	meta := releaseMetadata{SourceCommit: "abc123", SourceURL: "https://example.com", BuiltAt: "2026-01-01T00:00:00Z"}
	require.NoError(t, cookPluginForRelease(srcDir, dstDir, 42, meta))

	pj, err := os.ReadFile(filepath.Join(dstDir, ".claude-plugin", "plugin.json"))
	require.NoError(t, err)

	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(pj, &obj))

	hooks, ok := obj["hooks"]
	require.True(t, ok, "cooked plugin.json must preserve hooks")

	hooksMap, ok := hooks.(map[string]interface{})
	require.True(t, ok)
	_, hasPreToolUse := hooksMap["PreToolUse"]
	require.True(t, hasPreToolUse, "cooked plugin.json must preserve PreToolUse hooks")
}

func TestValidateHooksPreserved_SourceHasHooks_CookedPreserves(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dstDir, ".claude-plugin"), 0755))

	hooks := `{
		"name": "test",
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo"}]}]
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(hooks), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, ".claude-plugin", "plugin.json"), []byte(hooks), 0644))

	require.NoError(t, validateHooksPreserved(srcDir, dstDir))
}

func TestValidateHooksPreserved_SourceHasHooks_CookedMissing(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dstDir, ".claude-plugin"), 0755))

	srcJSON := `{
		"name": "test",
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo"}]}]
		}
	}`
	dstJSON := `{"name": "test"}`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(srcJSON), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, ".claude-plugin", "plugin.json"), []byte(dstJSON), 0644))

	err := validateHooksPreserved(srcDir, dstDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "hooks will not register")
}

func TestValidateHooksPreserved_SourceHasHooks_CookedMissingEvent(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dstDir, ".claude-plugin"), 0755))

	srcJSON := `{
		"name": "test",
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo"}]}],
			"PostToolUse": [{"hooks": [{"type": "command", "command": "echo"}]}]
		}
	}`
	dstJSON := `{
		"name": "test",
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo"}]}]
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(srcJSON), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, ".claude-plugin", "plugin.json"), []byte(dstJSON), 0644))

	err := validateHooksPreserved(srcDir, dstDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "PostToolUse")
}

func TestValidateHooksPreserved_NoHooksInSource(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dstDir, ".claude-plugin"), 0755))

	noHooks := `{"name": "test"}`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(noHooks), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, ".claude-plugin", "plugin.json"), []byte(noHooks), 0644))

	require.NoError(t, validateHooksPreserved(srcDir, dstDir))
}

func TestValidateHooksPreserved_NoPluginJSONInSource(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, validateHooksPreserved(srcDir, dstDir))
}

func TestValidateHooksPreserved_CookedPluginJSONMissing(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))

	srcJSON := `{
		"name": "test",
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo"}]}]
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(srcJSON), 0644))

	err := validateHooksPreserved(srcDir, dstDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cooked plugin.json is missing")
}

func TestWriteNPMPackageJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0755))

	pluginJSON := `{"name":"my-plugin","description":"Does stuff","license":"MIT"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(pluginJSON), 0644))

	err := writeNPMPackageJSON(dir, "owner-my-plugin", "42.0.0")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	require.NoError(t, err)

	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &pkg))
	require.Equal(t, "owner-my-plugin", pkg["name"])
	require.Equal(t, "42.0.0", pkg["version"])
	require.Equal(t, "Does stuff", pkg["description"])
	require.Equal(t, "MIT", pkg["license"])
	_, hasPublishConfig := pkg["publishConfig"]
	require.False(t, hasPublishConfig)
}

func TestWriteNPMPackageJSON_NoPluginJSON(t *testing.T) {
	dir := t.TempDir()

	err := writeNPMPackageJSON(dir, "owner-empty-plugin", "1.0.0")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	require.NoError(t, err)

	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &pkg))
	require.Equal(t, "owner-empty-plugin", pkg["name"])
	require.Equal(t, "1.0.0", pkg["version"])
	_, hasDesc := pkg["description"]
	require.False(t, hasDesc)
	_, hasLicense := pkg["license"]
	require.False(t, hasLicense)
}

func TestWriteNPMPackageJSON_BadPluginJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte("{bad json}"), 0644))

	err := writeNPMPackageJSON(dir, "owner-plugin", "5.0.0")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	require.NoError(t, err)
	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &pkg))
	require.Equal(t, "owner-plugin", pkg["name"])
	_, hasDesc := pkg["description"]
	require.False(t, hasDesc)
}
