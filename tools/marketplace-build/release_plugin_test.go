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
}
