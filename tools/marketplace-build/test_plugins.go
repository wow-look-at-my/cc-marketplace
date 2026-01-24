package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var testPluginsCmd = &cobra.Command{
	Use:   "test-plugins",
	Short: "Run tests for all marketplace plugins",
	RunE:  runTestPlugins,
}

func init() {
	rootCmd.AddCommand(testPluginsCmd)
}

func runTestPlugins(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	repoRoot := getRepoRoot()
	pluginsDir := filepath.Join(repoRoot, "plugins")

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return fmt.Errorf("failed to read plugins dir: %w", err)
	}

	var failed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(pluginsDir, entry.Name())
		if !isMarketplacePlugin(pluginPath) {
			continue
		}

		// Run test-plugin for this plugin
		if err := runTestPlugin(cmd, []string{entry.Name()}); err != nil {
			failed = append(failed, entry.Name())
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("tests failed for: %v", failed)
	}

	return nil
}

func isMarketplacePlugin(pluginPath string) bool {
	pluginJSONPath := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginJSONPath)
	if err != nil {
		return false
	}

	var pj map[string]interface{}
	if err := json.Unmarshal(data, &pj); err != nil {
		return false
	}

	return isIncludedInMarketplace(pj)
}
