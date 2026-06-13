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

	// Find all plugins with mh.include_in_marketplace: true. Without orphan
	// tags there is no per-plugin "previous version" to diff against, and the
	// pages registry has to carry every plugin every push (otherwise unchanged
	// plugins fall out of the freshly-deployed gh-pages snapshot), so include
	// every marketplace plugin unconditionally.
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return fmt.Errorf("failed to read plugins directory: %w", err)
	}

	var pluginNames []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginName := entry.Name()
		pluginJSONPath := filepath.Join(pluginsDir, pluginName, ".claude-plugin", "plugin.json")

		if _, err := os.Stat(pluginJSONPath); os.IsNotExist(err) {
			continue
		}

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

		if !isIncludedInMarketplace(pluginJSON) {
			continue
		}

		pluginNames = append(pluginNames, pluginName)
	}

	pluginsJSON, err := json.Marshal(pluginNames)
	if err != nil {
		return fmt.Errorf("failed to marshal plugins: %w", err)
	}

	fmt.Printf("plugins=%s\n", pluginsJSON)
	fmt.Printf("has_changes=%t\n", len(pluginNames) > 0)

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
