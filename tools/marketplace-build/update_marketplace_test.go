package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestWriteSummary(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "summary.md")

	pluginRefs := map[string]string{
		"my-plugin": "plugin/my-plugin/v3",
	}

	writeSummary(summaryPath, pluginRefs, "owner", "repo", "master")

	data, err := os.ReadFile(summaryPath)
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "## Marketplace Updated")
	require.Contains(t, content, "master")
	require.Contains(t, content, "my-plugin")
	require.Contains(t, content, "plugin/my-plugin/v3")
}

func TestWriteSummary_BadPath(t *testing.T) {
	// Should not panic on bad path
	writeSummary("/nonexistent/dir/summary.md", nil, "o", "r", "b")
}

func TestBumpMarketplaceVersion_NoExistingTag(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("tag not found")
	})
	v := bumpMarketplaceVersion("feature-branch")
	require.Equal(t, 1, v)
}

func TestBumpMarketplaceVersion_ExistingStringVersion(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "show" {
			return `{"metadata":{"version":"5"}}`, nil
		}
		return "", nil
	})
	v := bumpMarketplaceVersion("master")
	require.Equal(t, 6, v)
}

func TestBumpMarketplaceVersion_ExistingFloatVersion(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "show" {
			return `{"metadata":{"version":10}}`, nil
		}
		return "", nil
	})
	v := bumpMarketplaceVersion("master")
	require.Equal(t, 11, v)
}

func TestBumpMarketplaceVersion_NoMetadata(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "show" {
			return `{"name":"marketplace"}`, nil
		}
		return "", nil
	})
	v := bumpMarketplaceVersion("master")
	require.Equal(t, 1, v)
}

func TestBumpMarketplaceVersion_InvalidJSON(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "show" {
			return "{bad json", nil
		}
		return "", nil
	})
	v := bumpMarketplaceVersion("master")
	require.Equal(t, 1, v)
}

func TestBumpMarketplaceVersion_BranchTag(t *testing.T) {
	// For non-master branches, should use marketplace/{branch}#latest tag
	var requestedTag string
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "show" {
			requestedTag = args[1]
			return "", fmt.Errorf("not found")
		}
		return "", nil
	})
	bumpMarketplaceVersion("feature-x")
	require.Contains(t, requestedTag, "marketplace/feature-x#latest")
}

func TestGetPluginRefs(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/alpha/v1",
		"plugin/alpha/v3",
		"plugin/alpha/v2",
		"plugin/beta/v1",
		"plugin/beta/latest",
	})

	refs, err := getPluginRefs("owner", "repo")
	require.NoError(t, err)
	require.Len(t, refs, 2)
	require.Equal(t, "plugin/alpha/v3", refs["alpha"])
	require.Equal(t, "plugin/beta/v1", refs["beta"])
}

func TestGetPluginRefs_SkipsBranchTags(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/alpha/v1",
		"plugin/alpha/feature/v1", // branch-specific, should be skipped (4 path parts)
	})

	refs, err := getPluginRefs("owner", "repo")
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Contains(t, refs, "alpha")
}

func TestGetPluginRefs_Empty(t *testing.T) {
	mockGitWithTags(t, nil)
	refs, err := getPluginRefs("owner", "repo")
	require.NoError(t, err)
	require.Empty(t, refs)
}

func TestCleanupLegacyPluginTags_AtFormat(t *testing.T) {
	var deletedTags []string
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "plugin/foo@v1\nplugin/foo@v2\nplugin/bar/v1\n", nil
		}
		if args[0] == "push" && strings.HasPrefix(args[2], ":refs/tags/") {
			deletedTags = append(deletedTags, strings.TrimPrefix(args[2], ":refs/tags/"))
			return "", nil
		}
		if args[0] == "tag" && args[1] == "-d" {
			return "", nil
		}
		return "", nil
	})

	err := cleanupLegacyPluginTags()
	require.NoError(t, err)
	require.Contains(t, deletedTags, "plugin/foo@v1")
	require.Contains(t, deletedTags, "plugin/foo@v2")
	require.Len(t, deletedTags, 2)
}

func TestCleanupLegacyPluginTags_HashFormat(t *testing.T) {
	var deletedTags []string
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "plugin/foo#1\nplugin/foo#2\nplugin/foo#latest\nplugin/bar/v1\n", nil
		}
		if args[0] == "push" {
			deletedTags = append(deletedTags, strings.TrimPrefix(args[2], ":refs/tags/"))
			return "", nil
		}
		if args[0] == "tag" && args[1] == "-d" {
			return "", nil
		}
		return "", nil
	})

	err := cleanupLegacyPluginTags()
	require.NoError(t, err)
	require.Contains(t, deletedTags, "plugin/foo#1")
	require.Contains(t, deletedTags, "plugin/foo#2")
	require.Contains(t, deletedTags, "plugin/foo#latest")
	require.Len(t, deletedTags, 3)
}

func TestCleanupLegacyPluginTags_None(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/foo/v1", "plugin/bar/v2"})
	err := cleanupLegacyPluginTags()
	require.NoError(t, err)
}

func TestCleanupStaleBranchTags(t *testing.T) {
	var deletedTags []string
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "marketplace/feature-x\nmarketplace/feature-y\n", nil
		}
		if args[0] == "ls-remote" {
			// feature-x exists, feature-y doesn't
			branch := args[3]
			if branch == "feature-x" {
				return "abc123\trefs/heads/feature-x\n", nil
			}
			return "", nil
		}
		if args[0] == "push" {
			deletedTags = append(deletedTags, strings.TrimPrefix(args[2], ":refs/tags/"))
			return "", nil
		}
		if args[0] == "tag" && args[1] == "-d" {
			return "", nil
		}
		return "", nil
	})

	err := cleanupStaleBranchTags()
	require.NoError(t, err)
	require.Len(t, deletedTags, 1)
	require.Equal(t, "marketplace/feature-y", deletedTags[0])
}

func TestCleanupStaleBranchTags_NoneStale(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" {
			return "", nil
		}
		return "", nil
	})
	err := cleanupStaleBranchTags()
	require.NoError(t, err)
}

func TestCleanupStalePluginTags(t *testing.T) {
	// Set up a temp plugins dir with one plugin
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "plugins", "existing-plugin", ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	pj := `{"name":"existing-plugin","mh":{"include_in_marketplace":true}}`
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pj), 0644))

	// Override repoRoot
	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	var deletedTags []string
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			prefix := ""
			if len(args) >= 3 {
				prefix = strings.TrimSuffix(args[2], "*")
			}
			allTags := []string{
				"plugin/existing-plugin/v1",
				"plugin/existing-plugin/v2",
				"plugin/existing-plugin/v3",
				"plugin/existing-plugin/v4",
				"plugin/existing-plugin/v5",
				"plugin/removed-plugin/v1",
				"plugin/removed-plugin/v2",
			}
			var matching []string
			for _, tag := range allTags {
				if prefix == "" || strings.HasPrefix(tag, prefix) {
					matching = append(matching, tag)
				}
			}
			if len(matching) == 0 {
				return "", nil
			}
			return strings.Join(matching, "\n"), nil
		}
		if args[0] == "push" {
			deletedTags = append(deletedTags, strings.TrimPrefix(args[2], ":refs/tags/"))
			return "", nil
		}
		if args[0] == "tag" && args[1] == "-d" {
			return "", nil
		}
		return "", nil
	})

	err := cleanupStalePluginTags()
	require.NoError(t, err)

	// removed-plugin tags should be deleted (all of them + /latest)
	require.Contains(t, deletedTags, "plugin/removed-plugin/v1")
	require.Contains(t, deletedTags, "plugin/removed-plugin/v2")
	require.Contains(t, deletedTags, "plugin/removed-plugin/latest")

	// existing-plugin: has 5 versions, keep top 3 (5,4,3), prune 1,2
	require.Contains(t, deletedTags, "plugin/existing-plugin/v1")
	require.Contains(t, deletedTags, "plugin/existing-plugin/v2")
}

func TestBuildPluginsArray(t *testing.T) {
	mockGitWithTags(t, nil, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		if args[0] == "show" {
			if strings.Contains(args[1], "plugin.json") {
				return `{"name":"alpha","description":"Alpha plugin","version":"3","keywords":["test"],"author":{"name":"Dev"}}`, nil
			}
			if strings.Contains(args[1], ".mcp.json") {
				return "", fmt.Errorf("not found")
			}
		}
		return "", nil
	})

	pluginRefs := map[string]string{
		"alpha": "plugin/alpha/v3",
	}

	existing := map[string]interface{}{
		"plugins": []interface{}{
			map[string]interface{}{
				"name":     "alpha",
				"category": "development",
			},
		},
	}

	plugins := buildPluginsArray(pluginRefs, existing)
	require.Len(t, plugins, 1)

	p := plugins[0].(map[string]interface{})
	require.Equal(t, "alpha", p["name"])
	require.Equal(t, "Alpha plugin", p["description"])
	require.Equal(t, "3", p["version"])
	// Preserved from existing
	require.Equal(t, "development", p["category"])
	// Source set correctly
	src := p["source"].(map[string]interface{})
	require.Equal(t, "github", src["source"])
	require.Equal(t, "plugin/alpha/v3", src["ref"])
}

func TestBuildPluginsArray_WithMCP(t *testing.T) {
	mockGitWithTags(t, nil, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		if args[0] == "show" {
			if strings.Contains(args[1], "plugin.json") {
				return `{"name":"beta"}`, nil
			}
			if strings.Contains(args[1], ".mcp.json") {
				return `{"mcpServers":{"myserver":{"command":"./server"}}}`, nil
			}
		}
		return "", nil
	})

	plugins := buildPluginsArray(map[string]string{"beta": "plugin/beta/v1"}, map[string]interface{}{})
	require.Len(t, plugins, 1)

	p := plugins[0].(map[string]interface{})
	mcpServers, ok := p["mcpServers"]
	require.True(t, ok)
	servers := mcpServers.(map[string]interface{})
	_, hasMyServer := servers["myserver"]
	require.True(t, hasMyServer)
}

func TestReadPluginJSONFromTag(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "show" {
			return `{"name":"test","description":"A plugin"}`, nil
		}
		return "", nil
	})

	result, err := readPluginJSONFromTag("plugin/test/v1")
	require.NoError(t, err)
	require.Equal(t, "test", result["name"])
	require.Equal(t, "A plugin", result["description"])
}

func TestReadPluginJSONFromTag_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("not found")
	})
	_, err := readPluginJSONFromTag("bad-tag")
	require.NotNil(t, err)
}

func TestReadPluginJSONFromTag_InvalidJSON(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "{bad", nil
	})
	_, err := readPluginJSONFromTag("v1")
	require.NotNil(t, err)
}

func TestReadMCPFromTag(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return `{"mcpServers":{"srv":{"command":"./srv"}}}`, nil
	})
	servers := readMCPFromTag("v1")
	require.NotNil(t, servers)
	_, hasSrv := servers["srv"]
	require.True(t, hasSrv)
}

func TestReadMCPFromTag_NoFile(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("not found")
	})
	require.Nil(t, readMCPFromTag("v1"))
}

func TestReadMCPFromTag_InvalidJSON(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "{bad", nil
	})
	require.Nil(t, readMCPFromTag("v1"))
}

func TestReadMCPFromTag_NoServersKey(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return `{"other":"data"}`, nil
	})
	require.Nil(t, readMCPFromTag("v1"))
}

func TestGetOldestPluginTagCommit_NoTags(t *testing.T) {
	mockGitWithTags(t, nil)
	commit := getOldestPluginTagCommit()
	require.Empty(t, commit)
}

func TestGetOldestPluginTagCommit_WithTags(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "plugin/a/v1\nplugin/a/v2\nplugin/a/latest\n", nil
		}
		if args[0] == "show" {
			if strings.Contains(args[1], "/v1:") {
				return `{"sourceCommit":"oldest111"}`, nil
			}
			if strings.Contains(args[1], "/v2:") {
				return `{"sourceCommit":"newer222"}`, nil
			}
		}
		if args[0] == "merge-base" {
			// oldest111 is ancestor of newer222
			if args[2] == "oldest111" {
				return "", nil // success = is ancestor
			}
			return "", fmt.Errorf("not ancestor")
		}
		return "", nil
	})

	commit := getOldestPluginTagCommit()
	require.Equal(t, "oldest111", commit)
}

func TestHasInfraChanges_NoTags(t *testing.T) {
	mockGitWithTags(t, nil)
	require.False(t, hasInfraChanges("/tmp/repo"))
}

func TestHasInfraChanges_WithChanges(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "plugin/a/v1\n", nil
		}
		if args[0] == "show" {
			return `{"sourceCommit":"abc123"}`, nil
		}
		if args[0] == "rev-list" {
			return "2\n", nil // 2 commits changed infra
		}
		return "", nil
	})
	require.True(t, hasInfraChanges("/tmp/repo"))
}

func TestHasInfraChanges_NoChanges(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "plugin/a/v1\n", nil
		}
		if args[0] == "show" {
			return `{"sourceCommit":"abc123"}`, nil
		}
		if args[0] == "rev-list" {
			return "0\n", nil
		}
		return "", nil
	})
	require.False(t, hasInfraChanges("/tmp/repo"))
}

func TestRunUpdateMarketplace(t *testing.T) {
	// Set up temp repo structure
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

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "", nil // no tags
		}
		if args[0] == "rev-parse" && args[1] == "--abbrev-ref" {
			return "master\n", nil
		}
		if args[0] == "remote" {
			return "https://github.com/owner/repo.git\n", nil
		}
		if args[0] == "ls-remote" {
			return "", nil
		}
		if args[0] == "push" {
			return "", nil
		}
		return "", nil
	})

	// Use cobra command
	cmd := updateMarketplaceCmd
	err = runUpdateMarketplace(cmd, nil)
	require.NoError(t, err)
}
