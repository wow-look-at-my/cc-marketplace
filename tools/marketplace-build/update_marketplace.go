package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func runUpdateMarketplace(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	// Clean up stale plugin tags (removed plugins + old versions)
	if err := cleanupStalePluginTags(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup stale plugin tags: %v\n", err)
	}

	// Clean up legacy tags using old @v and /v formats
	if err := cleanupLegacyPluginTags(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup legacy plugin tags: %v\n", err)
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

	// Bump marketplace version
	newVersion := bumpMarketplaceVersion()
	metadata, ok := marketplace["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
	}
	metadata["version"] = fmt.Sprintf("%d", newVersion) // Must be string per schema
	marketplace["metadata"] = metadata

	// Cook marketplace.json (remove $schema, mh.*)
	delete(marketplace, "$schema")
	delete(marketplace, "mh")

	// Create temp directory for Pages deployment (NOT cleaned up - workflow uses it)
	tmpDir, err := os.MkdirTemp("", "marketplace-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Write cooked marketplace.json into .claude-plugin/ so the dir validates directly
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude-plugin dir: %w", err)
	}

	cookedData, err := json.MarshalIndent(marketplace, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal marketplace.json: %w", err)
	}

	// Write to .claude-plugin/marketplace.json (for validation)
	if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), cookedData, 0644); err != nil {
		return fmt.Errorf("failed to write marketplace.json: %w", err)
	}

	// Also write to root marketplace.json (for Pages URL: /marketplace.json)
	if err := os.WriteFile(filepath.Join(tmpDir, "marketplace.json"), cookedData, 0644); err != nil {
		return fmt.Errorf("failed to write root marketplace.json: %w", err)
	}

	// Output for GitHub Actions
	fmt.Printf("source_dir=%s\n", tmpDir)

	// Write step summary if GITHUB_STEP_SUMMARY is set
	if summaryPath := os.Getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		writeSummary(summaryPath, pluginRefs, owner, repo)
	}

	fmt.Fprintf(os.Stderr, "Prepared marketplace update in %s\n", tmpDir)
	return nil
}

func writeSummary(path string, pluginRefs map[string]string, owner, repo string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	pagesURL := fmt.Sprintf("https://%s.github.io/%s/marketplace.json", owner, repo)

	fmt.Fprintf(f, "## Marketplace Updated\n\n")
	fmt.Fprintf(f, "**URL:** [marketplace.json](%s)\n\n", pagesURL)
	fmt.Fprintf(f, "| Plugin | Version |\n")
	fmt.Fprintf(f, "|--------|--------|\n")

	for plugin, tag := range pluginRefs {
		parts := strings.Split(tag, "/")
		version := parts[len(parts)-1]
		tagURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, tag)
		fmt.Fprintf(f, "| %s | [%s](%s) |\n", plugin, version, tagURL)
	}
}

// getPluginRefs returns a map of plugin name -> latest version tag (global, master branch only)
func getPluginRefs(owner, repo string) (map[string]string, error) {
	refs := make(map[string]string)

	// List all plugin tags (format: plugin/{name}#{version} for master)
	tags, err := ListTagsWithPrefix("plugin/")
	if err != nil {
		return nil, err
	}

	// Find latest version for each plugin (only master/main branch tags)
	pluginVersions := make(map[string]int) // plugin -> highest version

	for _, tag := range tags {
		// Skip 'latest' tags
		if strings.HasSuffix(tag, "#latest") {
			continue
		}

		// Parse tag: plugin/{name}#{version}
		// Split on # first to get version
		hashParts := strings.Split(tag, "#")
		if len(hashParts) != 2 {
			continue
		}

		// Parse plugin/{name} - skip branch tags (plugin/{name}/{branch}#version)
		pathParts := strings.Split(hashParts[0], "/")
		if len(pathParts) != 2 || pathParts[0] != "plugin" {
			continue
		}

		pluginName := pathParts[1]
		var v int
		fmt.Sscanf(hashParts[1], "%d", &v)

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

		// Read plugin.json from the tag to get description and other metadata
		if pluginJSON, err := readPluginJSONFromTag(tagRef); err == nil {
			if desc, ok := pluginJSON["description"].(string); ok && desc != "" {
				plugin["description"] = desc
			}
			if author, ok := pluginJSON["author"]; ok {
				plugin["author"] = author
			}
			if keywords, ok := pluginJSON["keywords"]; ok {
				plugin["keywords"] = keywords
			}
			if license, ok := pluginJSON["license"].(string); ok && license != "" {
				plugin["license"] = license
			}
			if version, ok := pluginJSON["version"].(string); ok && version != "" {
				plugin["version"] = version
			}
			// Copy any inline component configs from plugin.json
			if mcpServers, ok := pluginJSON["mcpServers"]; ok {
				plugin["mcpServers"] = mcpServers
			}
			if hooks, ok := pluginJSON["hooks"]; ok {
				plugin["hooks"] = hooks
			}
		}

		// Read .mcp.json if it exists and mcpServers not already set
		if _, hasMCP := plugin["mcpServers"]; !hasMCP {
			if mcpServers := readMCPFromTag(tagRef); mcpServers != nil {
				plugin["mcpServers"] = mcpServers
			}
		}

		// Preserve existing metadata not already set
		if existing, ok := existingPlugins[pluginName]; ok {
			for k, v := range existing {
				if k != "name" && k != "source" && k != "mh" && k != "$schema" && k != "commands" && k != "agents" && k != "skills" {
					if _, exists := plugin[k]; !exists {
						plugin[k] = v
					}
				}
			}
		}

		plugins = append(plugins, plugin)
	}

	return plugins
}

// readPluginJSONFromTag reads and parses .claude-plugin/plugin.json from a tag
func readPluginJSONFromTag(tag string) (map[string]interface{}, error) {
	content, err := ReadFileFromTag(tag, ".claude-plugin/plugin.json")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// readMCPFromTag reads .mcp.json from the tag and returns mcpServers config
func readMCPFromTag(tag string) map[string]interface{} {
	content, err := ReadFileFromTag(tag, ".mcp.json")
	if err != nil {
		return nil
	}
	var mcpConfig map[string]interface{}
	if err := json.Unmarshal([]byte(content), &mcpConfig); err != nil {
		return nil
	}
	if servers, ok := mcpConfig["mcpServers"]; ok {
		if serversMap, ok := servers.(map[string]interface{}); ok {
			return serversMap
		}
	}
	return nil
}

// bumpMarketplaceVersion fetches current version from the live marketplace URL and increments it
func bumpMarketplaceVersion() int {
	marketplaceURL := os.Getenv("MARKETPLACE_URL")
	if marketplaceURL == "" {
		fmt.Fprintf(os.Stderr, "Warning: MARKETPLACE_URL not set, starting at version 1\n")
		return 1
	}

	resp, err := http.Get(marketplaceURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch marketplace: %v (starting at version 1)\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Warning: marketplace returned %d (starting at version 1)\n", resp.StatusCode)
		return 1
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 1
	}

	var marketplace map[string]interface{}
	if err := json.Unmarshal(body, &marketplace); err != nil {
		return 1
	}

	metadata, ok := marketplace["metadata"].(map[string]interface{})
	if !ok {
		return 1
	}

	switch v := metadata["version"].(type) {
	case float64:
		return int(v) + 1
	case int:
		return v + 1
	case string:
		var ver int
		fmt.Sscanf(v, "%d", &ver)
		return ver + 1
	}

	return 1
}

// cleanupStalePluginTags removes plugin tags for plugins that no longer exist
// in the repo and prunes old version tags (keeping only the latest 3 versions)
func cleanupStalePluginTags() error {
	repoRoot := getRepoRoot()
	pluginsDir := filepath.Join(repoRoot, "plugins")

	// Get all plugin names that have tags
	taggedPlugins, err := ListPluginNames()
	if err != nil {
		return fmt.Errorf("failed to list plugin names from tags: %w", err)
	}

	var allStale []string

	for _, pluginName := range taggedPlugins {
		pluginPath := filepath.Join(pluginsDir, pluginName)
		pluginJSONPath := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")

		// Check if plugin still exists and is included in marketplace
		removed := false
		if _, err := os.Stat(pluginJSONPath); os.IsNotExist(err) {
			removed = true
		} else if err == nil {
			// Plugin exists — check if it's still included in marketplace
			data, readErr := os.ReadFile(pluginJSONPath)
			if readErr == nil {
				var pluginJSON map[string]interface{}
				if json.Unmarshal(data, &pluginJSON) == nil {
					if !isIncludedInMarketplace(pluginJSON) {
						removed = true
					}
				}
			}
		}

		pluginTags, err := ListPluginTags(pluginName)
		if err != nil {
			continue
		}

		if removed {
			// Plugin removed — delete ALL its tags (versioned + #latest)
			fmt.Fprintf(os.Stderr, "Plugin %s removed — marking all %d tags for deletion\n", pluginName, len(pluginTags))
			allStale = append(allStale, pluginTags...)
			// Also delete the #latest pointer
			allStale = append(allStale, fmt.Sprintf("plugin/%s#latest", pluginName))
			continue
		}

		// Plugin still exists — prune old versions, keep latest 3
		if len(pluginTags) <= 3 {
			continue
		}

		// Tags are unordered, sort by version number
		type versionedTag struct {
			tag     string
			version int
		}
		var vTags []versionedTag
		for _, tag := range pluginTags {
			hashParts := strings.SplitN(tag, "#", 2)
			if len(hashParts) != 2 {
				continue
			}
			var v int
			fmt.Sscanf(hashParts[1], "%d", &v)
			vTags = append(vTags, versionedTag{tag: tag, version: v})
		}

		// Sort descending by version
		for i := 0; i < len(vTags); i++ {
			for j := i + 1; j < len(vTags); j++ {
				if vTags[j].version > vTags[i].version {
					vTags[i], vTags[j] = vTags[j], vTags[i]
				}
			}
		}

		// Keep top 3, delete the rest
		for i := 3; i < len(vTags); i++ {
			allStale = append(allStale, vTags[i].tag)
		}

		if len(vTags) > 3 {
			fmt.Fprintf(os.Stderr, "Plugin %s: pruning %d old version tags (keeping latest 3)\n",
				pluginName, len(vTags)-3)
		}
	}

	if len(allStale) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Cleaning up %d stale plugin tags\n", len(allStale))

	// Delete remote tags
	if err := DeleteRemoteTags(allStale...); err != nil {
		return fmt.Errorf("failed to delete stale plugin tags: %w", err)
	}

	// Delete local tags
	_ = DeleteLocalTags(allStale...)

	return nil
}

// cleanupLegacyPluginTags removes plugin tags using old naming formats:
// - plugin/{name}@v{N} (original @ format)
// - plugin/{name}/v{N} (intermediate /v format)
// These were replaced by the current plugin/{name}#{N} format.
func cleanupLegacyPluginTags() error {
	allTags, err := ListTagsWithPrefix("plugin/")
	if err != nil {
		return err
	}

	var legacy []string
	for _, tag := range allTags {
		// Match plugin/{name}@v{N}
		if strings.Contains(tag, "@") {
			legacy = append(legacy, tag)
			continue
		}
		// Match plugin/{name}/v{N} — these have 3 parts when split by /
		// (current format uses # so no extra / segments)
		parts := strings.Split(tag, "/")
		if len(parts) == 3 && parts[0] == "plugin" && strings.HasPrefix(parts[2], "v") {
			legacy = append(legacy, tag)
		}
	}

	if len(legacy) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Cleaning up %d legacy plugin tags (old @v and /v formats)\n", len(legacy))
	for _, tag := range legacy {
		fmt.Fprintf(os.Stderr, "  - %s\n", tag)
	}

	if err := DeleteRemoteTags(legacy...); err != nil {
		return fmt.Errorf("failed to delete legacy plugin tags: %w", err)
	}

	_ = DeleteLocalTags(legacy...)
	return nil
}
