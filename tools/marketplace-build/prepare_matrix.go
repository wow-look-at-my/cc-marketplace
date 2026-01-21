package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func runPrepareMatrix(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	repoRoot := getRepoRoot()
	pluginsDir := filepath.Join(repoRoot, "plugins")

	// Find all plugins with mh.include_in_marketplace: true
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return fmt.Errorf("failed to read plugins directory: %w", err)
	}

	var changedPlugins []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginName := entry.Name()
		pluginPath := filepath.Join(pluginsDir, pluginName)
		pluginJSONPath := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")

		// Check if plugin.json exists
		if _, err := os.Stat(pluginJSONPath); os.IsNotExist(err) {
			continue
		}

		// Read and parse plugin.json
		data, err := os.ReadFile(pluginJSONPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read %s: %v\n", pluginJSONPath, err)
			continue
		}

		var pluginJSON map[string]interface{}
		if err := json.Unmarshal(data, &pluginJSON); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", pluginJSONPath, err)
			continue
		}

		// Check mh.include_in_marketplace
		if !isIncludedInMarketplace(pluginJSON) {
			continue
		}

		// Check if there are commits after the latest tag
		hasChanges, err := HasCommitsAfterTag(pluginName, pluginPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to check changes for %s: %v\n", pluginName, err)
			continue
		}

		if hasChanges {
			changedPlugins = append(changedPlugins, pluginName)
		}
	}

	// Check if branch marketplace tag exists
	branch, err := GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}
	marketplaceTag := fmt.Sprintf("%s/marketplace", branch)
	tags, _ := ListTagsWithPrefix(marketplaceTag)
	hasMarketplaceTag := len(tags) > 0

	// Output in GITHUB_OUTPUT format
	pluginsJSON, err := json.Marshal(changedPlugins)
	if err != nil {
		return fmt.Errorf("failed to marshal plugins: %w", err)
	}

	hasChanges := len(changedPlugins) > 0
	needsMarketplace := hasChanges || !hasMarketplaceTag

	fmt.Printf("plugins=%s\n", pluginsJSON)
	fmt.Printf("has_changes=%t\n", hasChanges)
	fmt.Printf("needs_marketplace=%t\n", needsMarketplace)

	return nil
}

// isIncludedInMarketplace checks if plugin.json has mh.include_in_marketplace: true
func isIncludedInMarketplace(pluginJSON map[string]interface{}) bool {
	mh, ok := pluginJSON["mh"].(map[string]interface{})
	if !ok {
		return false
	}

	include, ok := mh["include_in_marketplace"].(bool)
	return ok && include
}
