package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// makePackagedPlugin creates a released plugin directory under root: the
// cooked tree plus the manifest.json release-plugin writes. There is no npm
// tarball any more -- the cooked directory IS what gets published, as a git
// orphan tag.
func makePackagedPlugin(t *testing.T, root, name, version, pluginJSON, mcpJSON string) string {
	t.Helper()

	outDir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	if pluginJSON != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(outDir, ".claude-plugin"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(outDir, ".claude-plugin", "plugin.json"), []byte(pluginJSON), 0o644))
	}
	if mcpJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(outDir, ".mcp.json"), []byte(mcpJSON), 0o644))
	}
	require.NoError(t, writeReleaseManifest(outDir, name, version, fmt.Sprintf("%s#%s", name, version)))
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

// A plugin whose release output can't be read is a FAILED RELEASE, not one to
// skip: warning and continuing published a marketplace.json with that plugin
// missing, so nobody could install it and every check stayed green.
func TestReadPackagedPlugins_NoManifest(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "broken"), 0755))

	_, err := readPackagedPlugins(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "broken")
}

func TestReadPackagedPlugins_BadManifest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "broken")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte("{not json"), 0644))

	_, err := readPackagedPlugins(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "broken")
}

func TestReadPackagedPlugins_IncompleteManifest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "broken")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(`{"name":"broken"}`), 0644))

	_, err := readPackagedPlugins(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing name, version or tag")
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
			manifest: pluginReleaseManifest{
				Name:    "my-plugin",
				Version: "3",
				Tag:     "my-plugin#3",
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
	require.Contains(t, content, "my-plugin#3")
	require.Contains(t, content, "3")
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

	result := buildPluginsArray(plugins, existing, "test-owner/test-repo", "test-owner")
	require.Len(t, result, 1)

	p := result[0].(map[string]interface{})
	require.Equal(t, "alpha", p["name"])
	require.Equal(t, "Alpha plugin", p["description"])
	require.Equal(t, "3", p["version"])
	require.Equal(t, "development", p["category"])

	// A git source, not npm: installing a plugin is a shallow clone of its
	// orphan tag, so nothing on the far side needs node. The ref is the
	// immutable per-release tag, never the moving "#latest" pointer.
	//
	// And an https URL, because a PUBLIC plugin must install with no GitHub
	// account: the "github" source type clones over SSH, which needs a key
	// the installing machine may not have (no Actions runner does). This is
	// the assertion that keeps anonymous installs working.
	src := p["source"].(map[string]interface{})
	require.Equal(t, "url", src["source"])
	require.Equal(t, "https://github.com/test-owner/test-repo.git", src["url"])
	require.Equal(t, "alpha#3.0.0", src["ref"])
	require.NotContains(t, src, "repo", "the owner/repo shorthand is what resolved to ssh")
	require.NotContains(t, src, "package", "no npm package name survives")
	require.NotContains(t, src, "registry", "no npm registry survives")
	require.NotContains(t, fmt.Sprint(src), "git@", "no ssh URL may ever reach a published source")
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

	result := buildPluginsArray(plugins, map[string]interface{}{}, "test-owner/test-repo", "test-owner")
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
