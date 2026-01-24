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

	// Clean up stale branch tags first
	if err := cleanupStaleBranchTags(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup stale tags: %v\n", err)
	}

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

	// Bump marketplace version
	newVersion := bumpMarketplaceVersion(branch)
	metadata, ok := marketplace["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
	}
	metadata["version"] = fmt.Sprintf("%d", newVersion) // Must be string per schema
	marketplace["metadata"] = metadata

	// Cook marketplace.json (remove $schema, mh.*)
	delete(marketplace, "$schema")
	delete(marketplace, "mh")

	// Create temp directory for marketplace tag contents (NOT cleaned up - workflow uses it)
	tmpDir, err := os.MkdirTemp("", "marketplace-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	// Note: NOT removing tmpDir - the orphan-tag action needs it

	// Write cooked marketplace.json to root (orphan-tag's move will relocate to .claude-plugin/)
	cookedData, err := json.MarshalIndent(marketplace, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal marketplace.json: %w", err)
	}

	tmpMarketplacePath := filepath.Join(tmpDir, "marketplace.json")
	if err := os.WriteFile(tmpMarketplacePath, cookedData, 0644); err != nil {
		return fmt.Errorf("failed to write marketplace.json: %w", err)
	}

	var marketplaceTag string
	if branch == "master" {
		marketplaceTag = fmt.Sprintf("marketplace@v%d", newVersion)
	} else {
		marketplaceTag = fmt.Sprintf("marketplace/%s@v%d", branch, newVersion)
	}
	commitMsg := fmt.Sprintf("Update marketplace v%d for %s branch", newVersion, branch)

	// Output for GitHub Actions (parsed by workflow)
	fmt.Printf("source_dir=%s\n", tmpDir)
	fmt.Printf("tag=%s\n", marketplaceTag)
	fmt.Printf("message=%s\n", commitMsg)
	if branch == "master" {
		fmt.Printf("latest_tag=latest\n")
	}

	// Write step summary if GITHUB_STEP_SUMMARY is set
	if summaryPath := os.Getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		writeSummary(summaryPath, pluginRefs, owner, repo, branch)
	}

	fmt.Fprintf(os.Stderr, "Prepared marketplace update in %s\n", tmpDir)
	return nil
}

func writeSummary(path string, pluginRefs map[string]string, owner, repo, branch string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	marketplaceTag := fmt.Sprintf("marketplace/%s", branch)
	marketplaceURL := fmt.Sprintf("https://github.com/%s/%s/blob/%s/.claude-plugin/marketplace.json", owner, repo, marketplaceTag)

	fmt.Fprintf(f, "## Marketplace Updated\n\n")
	fmt.Fprintf(f, "**Branch:** `%s`\n\n", branch)
	fmt.Fprintf(f, "**Marketplace:** [marketplace.json](%s)\n\n", marketplaceURL)
	fmt.Fprintf(f, "| Plugin | Version |\n")
	fmt.Fprintf(f, "|--------|--------|\n")

	for plugin, tag := range pluginRefs {
		// Extract version from tag (plugin/vN -> vN)
		parts := strings.Split(tag, "/")
		version := parts[len(parts)-1]
		tagURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, tag)
		fmt.Fprintf(f, "| %s | [%s](%s) |\n", plugin, version, tagURL)
	}
}

// getPluginRefs returns a map of plugin name -> latest version tag (global)
func getPluginRefs(owner, repo string) (map[string]string, error) {
	refs := make(map[string]string)

	// List all plugin tags (format: plugin/{plugin-name}@vN)
	tags, err := ListTagsWithPrefix("plugin/")
	if err != nil {
		return nil, err
	}

	// Find latest version for each plugin
	pluginVersions := make(map[string]int) // plugin -> highest version

	for _, tag := range tags {
		// Parse tag: plugin/{plugin-name}@vN
		// Split on @ first to get version
		atParts := strings.Split(tag, "@")
		if len(atParts) != 2 {
			continue
		}

		// Parse plugin/{plugin-name}
		pathParts := strings.Split(atParts[0], "/")
		if len(pathParts) != 2 || pathParts[0] != "plugin" {
			continue
		}

		pluginName := pathParts[1]
		vStr := strings.TrimPrefix(atParts[1], "v")
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

		// Scan tag for component files
		if commands := scanCommandsFromTag(tagRef); len(commands) > 0 {
			plugin["commands"] = commands
		}
		if agents := scanAgentsFromTag(tagRef); len(agents) > 0 {
			plugin["agents"] = agents
		}
		if skills := scanSkillsFromTag(tagRef); len(skills) > 0 {
			plugin["skills"] = skills
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
				if k != "name" && k != "source" && k != "mh" && k != "$schema" {
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

// scanCommandsFromTag returns command names from the tag's commands/ directory
func scanCommandsFromTag(tag string) []string {
	files, err := ListFilesInTag(tag, "commands")
	if err != nil {
		return nil
	}
	var commands []string
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			// Remove .md extension to get command name
			commands = append(commands, strings.TrimSuffix(f, ".md"))
		}
	}
	return commands
}

// scanAgentsFromTag returns agent names from the tag's agents/ directory
func scanAgentsFromTag(tag string) []string {
	files, err := ListFilesInTag(tag, "agents")
	if err != nil {
		return nil
	}
	var agents []string
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			agents = append(agents, strings.TrimSuffix(f, ".md"))
		}
	}
	return agents
}

// scanSkillsFromTag returns skill names from the tag's skills/ directory
func scanSkillsFromTag(tag string) []string {
	// Skills are directories with SKILL.md inside
	files, err := ListFilesInTag(tag, "skills")
	if err != nil {
		return nil
	}
	var skills []string
	for _, f := range files {
		// Check if this is a directory containing SKILL.md
		_, err := ReadFileFromTag(tag, fmt.Sprintf("skills/%s/SKILL.md", f))
		if err == nil {
			skills = append(skills, f)
		}
	}
	return skills
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

// bumpMarketplaceVersion reads current version from marketplace tag and increments it
func bumpMarketplaceVersion(branch string) int {
	tag := fmt.Sprintf("marketplace/%s", branch)

	// Try to read current marketplace.json from tag
	content, err := ReadFileFromTag(tag, ".claude-plugin/marketplace.json")
	if err != nil {
		return 1 // First version
	}

	var marketplace map[string]interface{}
	if err := json.Unmarshal([]byte(content), &marketplace); err != nil {
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

// cleanupStaleBranchTags removes marketplace/{branch} tags for branches that no longer exist
func cleanupStaleBranchTags() error {
	// List all marketplace tags
	tags, err := ListTagsWithPrefix("marketplace/")
	if err != nil {
		return err
	}

	var stale []string
	for _, tag := range tags {
		// Extract branch name from marketplace/{branch}
		parts := strings.Split(tag, "/")
		if len(parts) != 2 || parts[0] != "marketplace" {
			continue
		}
		branch := parts[1]

		// Check if branch exists on remote
		exists, err := RemoteBranchExists(branch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to check branch %s: %v\n", branch, err)
			continue
		}

		if !exists {
			stale = append(stale, tag)
		}
	}

	if len(stale) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Cleaning up %d stale marketplace tags:\n", len(stale))
	for _, tag := range stale {
		fmt.Fprintf(os.Stderr, "  - %s\n", tag)
	}

	// Delete remote tags
	if err := DeleteRemoteTags(stale...); err != nil {
		return fmt.Errorf("failed to delete remote tags: %w", err)
	}

	// Delete local tags
	_ = DeleteLocalTags(stale...)

	return nil
}
