package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func runPrepareMatrix(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	repoRoot := getRepoRoot()
	pluginsDir := filepath.Join(repoRoot, "plugins")

	// Check if infrastructure changed (workflow or marketplace-build)
	infraChanged := hasInfraChanges(repoRoot)
	if infraChanged {
		fmt.Fprintf(os.Stderr, "Infrastructure changed - rebuilding all plugins\n")
	}

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

		// If infra changed, rebuild all plugins
		if infraChanged {
			changedPlugins = append(changedPlugins, pluginName)
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

	// Output in GITHUB_OUTPUT format
	pluginsJSON, err := json.Marshal(changedPlugins)
	if err != nil {
		return fmt.Errorf("failed to marshal plugins: %w", err)
	}

	fmt.Printf("plugins=%s\n", pluginsJSON)
	fmt.Printf("has_changes=%t\n", len(changedPlugins) > 0)

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

// hasInfraChanges checks if workflow or marketplace-build changed since the oldest plugin tag
func hasInfraChanges(repoRoot string) bool {
	// Get the oldest plugin tag to use as baseline
	oldestCommit := getOldestPluginTagCommit()
	if oldestCommit == "" {
		// No tags exist, so this is first build - not an infra change
		return false
	}

	// Check if workflow or tools changed since oldest tag
	infraPaths := []string{
		filepath.Join(repoRoot, ".github/workflows"),
		filepath.Join(repoRoot, "tools/marketplace-build"),
	}

	for _, path := range infraPaths {
		out, err := runGit("rev-list", "--count", fmt.Sprintf("%s..HEAD", oldestCommit), "--", path)
		if err != nil {
			continue
		}
		count := strings.TrimSpace(out)
		if count != "0" {
			return true
		}
	}

	return false
}

// getOldestPluginTagCommit returns the source commit of the oldest plugin tag
func getOldestPluginTagCommit() string {
	// List all plugin tags
	out, err := runGit("tag", "-l", "plugin/*/v*")
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}

	tags := strings.Split(strings.TrimSpace(out), "\n")
	if len(tags) == 0 {
		return ""
	}

	// Find the oldest source commit across all tags
	var oldestCommit string
	for _, tag := range tags {
		// Read mh.plugin.json from the tag
		metaOut, err := runGit("show", fmt.Sprintf("%s:mh.plugin.json", tag))
		if err != nil {
			continue
		}

		var metadata struct {
			SourceCommit string `json:"sourceCommit"`
		}
		if err := json.Unmarshal([]byte(metaOut), &metadata); err != nil || metadata.SourceCommit == "" {
			continue
		}

		if oldestCommit == "" {
			oldestCommit = metadata.SourceCommit
			continue
		}

		// Check if this commit is older (is an ancestor of current oldest)
		_, err = runGit("merge-base", "--is-ancestor", metadata.SourceCommit, oldestCommit)
		if err == nil {
			// metadata.SourceCommit is an ancestor of oldestCommit, so it's older
			oldestCommit = metadata.SourceCommit
		}
	}

	return oldestCommit
}
