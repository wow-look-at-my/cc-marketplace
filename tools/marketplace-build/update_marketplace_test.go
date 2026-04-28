package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

// makeCookedPlugin creates a fake cooked plugin directory under root.
// pluginJSON is the contents of .claude-plugin/plugin.json (must be valid JSON
// or empty for none); mcpJSON is the .mcp.json contents (empty for none).
func makeCookedPlugin(t *testing.T, root, name, version, pluginJSON, mcpJSON string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0755))

	pkg := map[string]string{
		"name":    "test-owner-" + name,
		"version": version,
	}
	pkgData, _ := json.Marshal(pkg)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), pkgData, 0644))

	if pluginJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(pluginJSON), 0644))
	}
	if mcpJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0644))
	}
	return dir
}

func TestReadCookedPlugins(t *testing.T) {
	dir := t.TempDir()
	makeCookedPlugin(t, dir, "alpha", "5.0.0", `{"name":"alpha"}`, "")
	makeCookedPlugin(t, dir, "beta", "1.0.0", `{"name":"beta"}`, "")
	// Stray file should be skipped.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("nope"), 0644))

	plugins, err := readCookedPlugins(dir)
	require.NoError(t, err)
	require.Len(t, plugins, 2)
	require.Equal(t, "alpha", plugins[0].name)
	require.Equal(t, "5.0.0", plugins[0].version)
	require.Equal(t, "beta", plugins[1].name)
	require.Equal(t, "1.0.0", plugins[1].version)
}

func TestReadCookedPlugins_NoPackageJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "broken"), 0755))

	plugins, err := readCookedPlugins(dir)
	require.NoError(t, err)
	require.Empty(t, plugins)
}

func TestReadCookedPlugins_BadPackageJSON(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "broken")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "package.json"), []byte("{not json"), 0644))

	plugins, err := readCookedPlugins(dir)
	require.NoError(t, err)
	require.Empty(t, plugins)
}

func TestReadCookedPlugins_MissingDir(t *testing.T) {
	_, err := readCookedPlugins("/nonexistent/dir/xyz")
	require.NotNil(t, err)
}

func TestWriteSummary(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "summary.md")

	plugins := []cookedPlugin{
		{name: "my-plugin", version: "3.0.0"},
	}

	writeSummary(summaryPath, plugins, "owner", "repo", "master")

	data, err := os.ReadFile(summaryPath)
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "## Marketplace Updated")
	require.Contains(t, content, "master")
	require.Contains(t, content, "my-plugin")
	require.Contains(t, content, "owner-my-plugin")
	require.Contains(t, content, "3.0.0")
}

func TestWriteSummary_BadPath(t *testing.T) {
	writeSummary("/nonexistent/dir/summary.md", nil, "o", "r", "b")
}

func TestBuildPluginsArray(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", nil
	})

	dir := t.TempDir()
	makeCookedPlugin(t, dir, "alpha", "3.0.0",
		`{"name":"alpha","description":"Alpha plugin","version":"3","keywords":["test"],"author":{"name":"Dev"}}`, "")

	plugins, err := readCookedPlugins(dir)
	require.NoError(t, err)
	require.Len(t, plugins, 1)

	existing := map[string]interface{}{
		"plugins": []interface{}{
			map[string]interface{}{
				"name":     "alpha",
				"category": "development",
			},
		},
	}

	result := buildPluginsArray(plugins, existing)
	require.Len(t, result, 1)

	p := result[0].(map[string]interface{})
	require.Equal(t, "alpha", p["name"])
	require.Equal(t, "Alpha plugin", p["description"])
	require.Equal(t, "3", p["version"])
	require.Equal(t, "development", p["category"])

	src := p["source"].(map[string]interface{})
	require.Equal(t, "npm", src["source"])
	require.Equal(t, "test-owner-alpha", src["package"])
	require.Equal(t, "3.0.0", src["version"])
	require.Equal(t, "https://test-owner.github.io/test-repo", src["registry"])
}

func TestBuildPluginsArray_WithMCP(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", nil
	})

	dir := t.TempDir()
	makeCookedPlugin(t, dir, "beta", "1.0.0",
		`{"name":"beta"}`,
		`{"mcpServers":{"myserver":{"command":"./server"}}}`)

	plugins, err := readCookedPlugins(dir)
	require.NoError(t, err)

	result := buildPluginsArray(plugins, map[string]interface{}{})
	require.Len(t, result, 1)

	p := result[0].(map[string]interface{})
	mcpServers, ok := p["mcpServers"]
	require.True(t, ok)
	servers := mcpServers.(map[string]interface{})
	_, hasMyServer := servers["myserver"]
	require.True(t, hasMyServer)
}

func TestReadCookedPluginJSON(t *testing.T) {
	dir := t.TempDir()
	makeCookedPlugin(t, dir, "test", "1.0.0", `{"name":"test","description":"A plugin"}`, "")
	result, err := readCookedPluginJSON(filepath.Join(dir, "test"))
	require.NoError(t, err)
	require.Equal(t, "test", result["name"])
	require.Equal(t, "A plugin", result["description"])
}

func TestReadCookedPluginJSON_Missing(t *testing.T) {
	_, err := readCookedPluginJSON("/nonexistent")
	require.NotNil(t, err)
}

func TestReadCookedPluginJSON_BadJSON(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte("{bad"), 0644))
	_, err := readCookedPluginJSON(dir)
	require.NotNil(t, err)
}

func TestReadCookedMCPServers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"),
		[]byte(`{"mcpServers":{"srv":{"command":"./srv"}}}`), 0644))
	servers := readCookedMCPServers(dir)
	require.NotNil(t, servers)
	_, hasSrv := servers["srv"]
	require.True(t, hasSrv)
}

func TestReadCookedMCPServers_NoFile(t *testing.T) {
	require.Nil(t, readCookedMCPServers(t.TempDir()))
}

func TestReadCookedMCPServers_BadJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("{bad"), 0644))
	require.Nil(t, readCookedMCPServers(dir))
}

func TestReadCookedMCPServers_NoServersKey(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"other":"data"}`), 0644))
	require.Nil(t, readCookedMCPServers(dir))
}

func TestRunUpdateMarketplace(t *testing.T) {
	tmpDir := t.TempDir()
	claudePluginDir := filepath.Join(tmpDir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(claudePluginDir, 0755))

	marketplace := map[string]interface{}{
		"name":    "test-marketplace",
		"owner":   map[string]interface{}{"name": "test"},
		"plugins": []interface{}{},
	}
	data, err := json.MarshalIndent(marketplace, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(claudePluginDir, "marketplace.json"), data, 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	cookedDir := t.TempDir()
	makeCookedPlugin(t, cookedDir, "alpha", "1.0.0", `{"name":"alpha","description":"Alpha"}`, "")

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "rev-parse" && args[1] == "--abbrev-ref" {
			return "master\n", nil
		}
		if args[0] == "remote" {
			return "https://github.com/owner/repo.git\n", nil
		}
		return "", fmt.Errorf("unexpected git call: %v", args)
	})

	origInput := updateMarketplaceInput
	updateMarketplaceInput = cookedDir
	t.Cleanup(func() { updateMarketplaceInput = origInput })

	err = runUpdateMarketplace(updateMarketplaceCmd, nil)
	require.NoError(t, err)
}

func TestRunUpdateMarketplace_NoInputFlag(t *testing.T) {
	origInput := updateMarketplaceInput
	updateMarketplaceInput = ""
	t.Cleanup(func() { updateMarketplaceInput = origInput })

	err := runUpdateMarketplace(updateMarketplaceCmd, nil)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "--input is required")
}
