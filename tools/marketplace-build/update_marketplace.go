package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func runUpdateMarketplace(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	branch, err := GetCurrentBranch()
	if err != nil {
		return err
	}

	owner, repo, err := GetRepoInfo()
	if err != nil {
		return err
	}

	repoRoot := getRepoRoot()
	marketplacePath := filepath.Join(repoRoot, ".claude-plugin", "marketplace.json")

	// Read existing marketplace.json
	data, err := os.ReadFile(marketplacePath)
	if err != nil {
		return fmt.Errorf("failed to read marketplace.json: %w", err)
	}

	var marketplace map[string]interface{}
	if err := json.Unmarshal(data, &marketplace); err != nil {
		return fmt.Errorf("failed to parse marketplace.json: %w", err)
	}

	// Get all plugins with their latest tags (global)
	pluginRefs, err := getPluginRefs(owner, repo)
	if err != nil {
		return fmt.Errorf("failed to get plugin refs: %w", err)
	}

	// Update plugins array
	plugins := buildPluginsArray(pluginRefs, marketplace)
	marketplace["plugins"] = plugins

	// Cook marketplace.json (remove $schema, mh.*)
	delete(marketplace, "$schema")
	delete(marketplace, "mh")

	// Create temp directory for marketplace tag contents
	tmpDir, err := os.MkdirTemp("", "marketplace-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .claude-plugin directory in temp
	tmpPluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(tmpPluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp plugin dir: %w", err)
	}

	// Write cooked marketplace.json
	cookedData, err := json.MarshalIndent(marketplace, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal marketplace.json: %w", err)
	}

	tmpMarketplacePath := filepath.Join(tmpPluginDir, "marketplace.json")
	if err := os.WriteFile(tmpMarketplacePath, cookedData, 0644); err != nil {
		return fmt.Errorf("failed to write marketplace.json: %w", err)
	}

	// Create orphan commit
	commitMsg := fmt.Sprintf("Update marketplace for %s branch", branch)
	commitSHA, err := CreateOrphanCommit(tmpDir, commitMsg)
	if err != nil {
		return fmt.Errorf("failed to create orphan commit: %w", err)
	}

	fmt.Printf("Created marketplace commit: %s\n", commitSHA)

	// Create/update marketplace tag for this branch
	marketplaceTag := fmt.Sprintf("%s/marketplace", branch)
	if err := CreateTag(marketplaceTag, commitSHA); err != nil {
		return fmt.Errorf("failed to create marketplace tag: %w", err)
	}

	// Force push marketplace tag (updates existing)
	if err := ForcePushTag(marketplaceTag); err != nil {
		return fmt.Errorf("failed to push marketplace tag: %w", err)
	}
	fmt.Printf("Updated marketplace tag: %s\n", marketplaceTag)

	// If master branch, also update latest tag
	if branch == "master" {
		latestTag := "latest"
		if err := CreateTag(latestTag, commitSHA); err != nil {
			return fmt.Errorf("failed to create latest tag: %w", err)
		}
		if err := ForcePushTag(latestTag); err != nil {
			return fmt.Errorf("failed to push latest tag: %w", err)
		}
		fmt.Printf("Updated latest tag\n")
	}

	return nil
}

// getPluginRefs returns a map of plugin name -> latest version tag (global)
func getPluginRefs(owner, repo string) (map[string]string, error) {
	refs := make(map[string]string)

	// List all plugin tags (format: plugin-name/vN)
	tags, err := ListTagsWithPrefix("")
	if err != nil {
		return nil, err
	}

	// Find latest version for each plugin
	pluginVersions := make(map[string]int) // plugin -> highest version

	for _, tag := range tags {
		// Skip marketplace and latest tags
		if strings.HasSuffix(tag, "/marketplace") || strings.HasSuffix(tag, "/latest") || tag == "latest" {
			continue
		}

		// Parse tag: plugin-name/vN
		parts := strings.Split(tag, "/")
		if len(parts) != 2 {
			continue
		}

		pluginName := parts[0]
		vStr := strings.TrimPrefix(parts[1], "v")
		var v int
		fmt.Sscanf(vStr, "%d", &v)

		if existing, ok := pluginVersions[pluginName]; !ok || v > existing {
			pluginVersions[pluginName] = v
			refs[pluginName] = tag
		}
	}

	return refs, nil
}

// buildPluginsArray creates the plugins array for marketplace.json
func buildPluginsArray(pluginRefs map[string]string, existingMarketplace map[string]interface{}) []interface{} {
	var plugins []interface{}

	// Get existing plugins to preserve metadata
	existingPlugins := make(map[string]map[string]interface{})
	if existing, ok := existingMarketplace["plugins"].([]interface{}); ok {
		for _, p := range existing {
			if plugin, ok := p.(map[string]interface{}); ok {
				if name, ok := plugin["name"].(string); ok {
					existingPlugins[name] = plugin
				}
			}
		}
	}

	owner, repo, _ := GetRepoInfo()

	for pluginName, tagRef := range pluginRefs {
		plugin := map[string]interface{}{
			"name": pluginName,
			"source": map[string]interface{}{
				"source": "github",
				"repo":   fmt.Sprintf("%s/%s", owner, repo),
				"ref":    tagRef,
			},
		}

		// Preserve existing metadata
		if existing, ok := existingPlugins[pluginName]; ok {
			for k, v := range existing {
				if k != "name" && k != "source" && k != "mh" && k != "$schema" {
					plugin[k] = v
				}
			}
		}

		plugins = append(plugins, plugin)
	}

	return plugins
}
