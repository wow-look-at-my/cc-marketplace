package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func readManifest(t *testing.T, dir string) pluginReleaseManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m pluginReleaseManifest
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

// The manifest is what update-marketplace mirrors into marketplace.json, so
// the cooked plugin.json and .mcp.json have to travel inside it -- otherwise
// that job needs a second copy of the tree.
func TestWriteReleaseManifestCarriesTheCookedManifests(t *testing.T) {
	cooked := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cooked, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cooked, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"glob","version":"7","description":"fast glob"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cooked, ".mcp.json"),
		[]byte(`{"mcpServers":{"glob":{"command":"./build/glob"}}}`), 0o644))

	require.NoError(t, writeReleaseManifest(cooked, "glob", "7", "glob#7"))

	m := readManifest(t, cooked)
	require.Equal(t, "glob", m.Name)
	require.Equal(t, "7", m.Version)
	require.Equal(t, "glob#7", m.Tag)
	require.Equal(t, "fast glob", m.PluginJSON["description"])

	servers, ok := m.MCPJSON["mcpServers"].(map[string]interface{})
	require.True(t, ok)
	require.Contains(t, servers, "glob")
}

// A skills-only plugin has neither file. It still gets a manifest -- the tag is
// the artifact either way, and a missing manifest is fatal downstream.
func TestWriteReleaseManifestWithoutOptionalManifests(t *testing.T) {
	cooked := t.TempDir()
	require.NoError(t, writeReleaseManifest(cooked, "css-cascade", "3", "css-cascade#3"))

	m := readManifest(t, cooked)
	require.Equal(t, "css-cascade", m.Name)
	require.Equal(t, "css-cascade#3", m.Tag)
	require.Nil(t, m.PluginJSON)
	require.Nil(t, m.MCPJSON)
}

// An unparseable plugin.json must not take the release facts down with it: the
// name/version/tag are what readPackagedPlugins insists on, and they are known
// here without reading anything.
func TestWriteReleaseManifestSurvivesUnparseableInputs(t *testing.T) {
	cooked := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cooked, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cooked, ".claude-plugin", "plugin.json"),
		[]byte(`{not json`), 0o644))

	require.NoError(t, writeReleaseManifest(cooked, "glob", "7", "glob#7"))

	m := readManifest(t, cooked)
	require.Equal(t, "glob#7", m.Tag)
	require.Nil(t, m.PluginJSON)
}

func TestWriteReleaseManifestReportsAnUnwritableDir(t *testing.T) {
	require.Error(t, writeReleaseManifest(filepath.Join(t.TempDir(), "nope"), "glob", "7", "glob#7"))
}

// The round trip is the contract between the two jobs: release-plugin writes
// it, update-marketplace reads it back out of the uploaded directory.
func TestManifestRoundTripsThroughReadPackagedPlugins(t *testing.T) {
	input := t.TempDir()
	for _, name := range []string{"glob", "css-cascade"} {
		dir := filepath.Join(input, name)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, writeReleaseManifest(dir, name, "7", name+"#7"))
	}

	plugins, err := readPackagedPlugins(input)
	require.NoError(t, err)
	require.Len(t, plugins, 2)
	require.Equal(t, "css-cascade", plugins[0].name, "sorted, so marketplace.json is stable")
	require.Equal(t, "glob#7", plugins[1].manifest.Tag)
}
