package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestIsMarketplacePlugin_True(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))

	pj := map[string]interface{}{
		"name": "test",
		"mh":   map[string]interface{}{"include_in_marketplace": true},
	}
	data, err := json.Marshal(pj)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0644))

	require.True(t, isMarketplacePlugin(dir))
}

func TestIsMarketplacePlugin_False(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))

	pj := map[string]interface{}{
		"name": "test",
		"mh":   map[string]interface{}{"include_in_marketplace": false},
	}
	data, err := json.Marshal(pj)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0644))

	require.False(t, isMarketplacePlugin(dir))
}

func TestIsMarketplacePlugin_NoPluginJSON(t *testing.T) {
	require.False(t, isMarketplacePlugin(t.TempDir()))
}

func TestIsMarketplacePlugin_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte("{bad"), 0644))
	require.False(t, isMarketplacePlugin(dir))
}

func TestRunTestPlugin_GoPlugin(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "plugins", "go-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte("package main\n"), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runTestPlugin(testPluginCmd, []string{"go-plugin"})
	require.NoError(t, err)
}

func TestRunTestPlugin_NonGoPlugin(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "plugins", "js-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte("//js\n"), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runTestPlugin(testPluginCmd, []string{"js-plugin"})
	require.NoError(t, err)
}

func TestRunTestPlugin_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "plugins"), 0755))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runTestPlugin(testPluginCmd, []string{"nonexistent"})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "plugin not found")
}

func TestRunTestPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	pluginsDir := filepath.Join(tmpDir, "plugins")

	// Create a marketplace plugin
	dir := filepath.Join(pluginsDir, "my-plugin", ".claude-plugin")
	require.NoError(t, os.MkdirAll(dir, 0755))
	pj := map[string]interface{}{
		"name": "my-plugin",
		"mh":   map[string]interface{}{"include_in_marketplace": true},
	}
	data, err := json.Marshal(pj)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644))

	// Create a non-marketplace dir
	require.NoError(t, os.MkdirAll(filepath.Join(pluginsDir, "example-plugin"), 0755))

	// Create a file (not a dir)
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "README.md"), []byte("hi"), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err = runTestPlugins(testPluginsCmd, nil)
	require.NoError(t, err)
}
