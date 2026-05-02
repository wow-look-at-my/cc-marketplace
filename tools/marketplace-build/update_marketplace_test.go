package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

// makePackagedPlugin creates a fake packaged plugin directory under root by
// running packagePluginToDir on a temp cooked dir built with the given
// plugin.json + .mcp.json strings.
func makePackagedPlugin(t *testing.T, root, name, version, pluginJSON, mcpJSON string) string {
	t.Helper()

	cookedDir := t.TempDir()
	pkg := map[string]string{
		"name":    "test-owner-" + name,
		"version": version,
	}
	pkgData, _ := json.Marshal(pkg)
	require.NoError(t, os.WriteFile(filepath.Join(cookedDir, "package.json"), pkgData, 0644))

	if pluginJSON != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(cookedDir, ".claude-plugin"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(cookedDir, ".claude-plugin", "plugin.json"), []byte(pluginJSON), 0644))
	}
	if mcpJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(cookedDir, ".mcp.json"), []byte(mcpJSON), 0644))
	}

	outDir := filepath.Join(root, name)
	require.NoError(t, packagePluginToDir(cookedDir, "test-owner-"+name, version, outDir))
	return outDir
}

func TestReadPackagedPlugins(t *testing.T) {
	dir := t.TempDir()
	makePackagedPlugin(t, dir, "alpha", "5.0.0", `{"name":"alpha"}`, "")
	makePackagedPlugin(t, dir, "beta", "1.0.0", `{"name":"beta"}`, "")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("nope"), 0644))

	plugins, err := readPackagedPlugins(dir)
	require.NoError(t, err)
	require.Len(t, plugins, 2)
	require.Equal(t, "alpha", plugins[0].name)
	require.Equal(t, "5.0.0", plugins[0].manifest.Version)
	require.Equal(t, "beta", plugins[1].name)
	require.Equal(t, "1.0.0", plugins[1].manifest.Version)
}

func TestReadPackagedPlugins_NoManifest(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "broken"), 0755))

	plugins, err := readPackagedPlugins(dir)
	require.NoError(t, err)
	require.Empty(t, plugins)
}

func TestReadPackagedPlugins_BadManifest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "broken")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte("{not json"), 0644))

	plugins, err := readPackagedPlugins(dir)
	require.NoError(t, err)
	require.Empty(t, plugins)
}

func TestReadPackagedPlugins_MissingDir(t *testing.T) {
	_, err := readPackagedPlugins("/nonexistent/dir/xyz")
	require.NotNil(t, err)
}

func TestWriteSummary(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "summary.md")

	plugins := []packagedPlugin{
		{
			name: "my-plugin",
			manifest: pluginPackageManifest{
				Name:    "owner-my-plugin",
				Version: "3.0.0",
			},
		},
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
	makePackagedPlugin(t, dir, "alpha", "3.0.0",
		`{"name":"alpha","description":"Alpha plugin","version":"3","keywords":["test"],"author":{"name":"Dev"}}`, "")

	plugins, err := readPackagedPlugins(dir)
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

	result := buildPluginsArray(plugins, existing, "https://test-owner.github.io/test-repo", "test-owner")
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
	makePackagedPlugin(t, dir, "beta", "1.0.0",
		`{"name":"beta"}`,
		`{"mcpServers":{"myserver":{"command":"./server"}}}`)

	plugins, err := readPackagedPlugins(dir)
	require.NoError(t, err)

	result := buildPluginsArray(plugins, map[string]interface{}{}, "https://test-owner.github.io/test-repo", "test-owner")
	require.Len(t, result, 1)

	p := result[0].(map[string]interface{})
	mcpServers, ok := p["mcpServers"]
	require.True(t, ok)
	servers := mcpServers.(map[string]interface{})
	_, hasMyServer := servers["myserver"]
	require.True(t, hasMyServer)
}

func TestMcpServersFromManifest(t *testing.T) {
	require.Nil(t, mcpServersFromManifest(nil))
	require.Nil(t, mcpServersFromManifest(map[string]interface{}{"other": "data"}))

	mcp := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"srv": map[string]interface{}{"command": "./srv"},
		},
	}
	servers := mcpServersFromManifest(mcp)
	require.NotNil(t, servers)
	_, ok := servers["srv"]
	require.True(t, ok)
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

	packagedDir := t.TempDir()
	makePackagedPlugin(t, packagedDir, "alpha", "1.0.0", `{"name":"alpha","description":"Alpha"}`, "")

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
	updateMarketplaceInput = packagedDir
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
