package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var updateMarketplaceInput string
var updateMarketplaceBaseURL string

func init() {
	updateMarketplaceCmd.Flags().StringVar(&updateMarketplaceBaseURL, "base-url", "", "Base URL for npm registry (defaults to GitHub Pages URL)")
}

func runUpdateMarketplace(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	if updateMarketplaceInput == "" {
		return fmt.Errorf("--input is required: directory of cooked plugin subdirectories")
	}

	branch, err := GetCurrentBranch()
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

	// Read cooked plugins from the artifact directory
	cookedPlugins, err := readCookedPlugins(updateMarketplaceInput)
	if err != nil {
		return fmt.Errorf("failed to read cooked plugins: %w", err)
	}

	// Update plugins array
	owner, repo, _ := GetRepoInfo()
	pagesRegistry := updateMarketplaceBaseURL
	if pagesRegistry == "" {
		pagesRegistry = fmt.Sprintf("https://%s.github.io/%s", owner, repo)
	}
	plugins := buildPluginsArray(cookedPlugins, marketplace, pagesRegistry)
	marketplace["plugins"] = plugins

	// Marketplace version mirrors the build's run number for monotonicity.
	metadata, ok := marketplace["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
	}
	metadata["version"] = fmt.Sprintf("%d", releaseVersion()) // Must be string per schema
	marketplace["metadata"] = metadata

	// Cook marketplace.json (remove $schema, mh.*)
	delete(marketplace, "$schema")
	delete(marketplace, "mh")

	tmpDir, err := os.MkdirTemp("", "marketplace-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	cookedData, err := json.MarshalIndent(marketplace, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal marketplace.json: %w", err)
	}

	tmpMarketplacePath := filepath.Join(tmpDir, "marketplace.json")
	if err := os.WriteFile(tmpMarketplacePath, cookedData, 0644); err != nil {
		return fmt.Errorf("failed to write marketplace.json: %w", err)
	}

	// Output for GitHub Actions (parsed by workflow)
	fmt.Printf("source_dir=%s\n", tmpDir)
	fmt.Printf("message=Update marketplace for %s\n", branch)

	if summaryPath := os.Getenv("GITHUB_STEP_SUMMARY"); summaryPath != "" {
		writeSummary(summaryPath, cookedPlugins, owner, repo, branch)
	}

	fmt.Fprintf(os.Stderr, "Prepared marketplace update in %s\n", tmpDir)
	return nil
}

func writeSummary(path string, plugins []cookedPlugin, owner, repo, branch string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "## Marketplace Updated\n\n")
	fmt.Fprintf(f, "**Branch:** `%s`\n\n", branch)
	fmt.Fprintf(f, "| Plugin | Package | Version |\n")
	fmt.Fprintf(f, "|--------|---------|--------|\n")

	for _, p := range plugins {
		pkgName := fmt.Sprintf("%s-%s", owner, p.name)
		fmt.Fprintf(f, "| %s | `%s` | `%s` |\n", p.name, pkgName, p.version)
	}
}

// buildPluginsArray creates the plugins array for marketplace.json from the
// cooked plugin artifacts produced by `release-plugin`.
func buildPluginsArray(plugins []cookedPlugin, existingMarketplace map[string]interface{}, pagesRegistry string) []interface{} {
	var out []interface{}

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

	owner, _, _ := GetRepoInfo()

	for _, p := range plugins {
		entry := map[string]interface{}{
			"name":    p.name,
			"version": p.version,
			"source": map[string]interface{}{
				"source":   "npm",
				"package":  fmt.Sprintf("%s-%s", owner, p.name),
				"version":  p.version,
				"registry": pagesRegistry,
			},
		}

		if pluginJSON, err := readCookedPluginJSON(p.dir); err == nil {
			if desc, ok := pluginJSON["description"].(string); ok && desc != "" {
				entry["description"] = desc
			}
			if author, ok := pluginJSON["author"]; ok {
				entry["author"] = author
			}
			if keywords, ok := pluginJSON["keywords"]; ok {
				entry["keywords"] = keywords
			}
			if license, ok := pluginJSON["license"].(string); ok && license != "" {
				entry["license"] = license
			}
			if version, ok := pluginJSON["version"].(string); ok && version != "" {
				entry["version"] = version
			}
			if mcpServers, ok := pluginJSON["mcpServers"]; ok {
				entry["mcpServers"] = mcpServers
			}
			if hooks, ok := pluginJSON["hooks"]; ok {
				entry["hooks"] = hooks
			}
		}

		if _, hasMCP := entry["mcpServers"]; !hasMCP {
			if mcpServers := readCookedMCPServers(p.dir); mcpServers != nil {
				entry["mcpServers"] = mcpServers
			}
		}

		if existing, ok := existingPlugins[p.name]; ok {
			for k, v := range existing {
				if k != "name" && k != "source" && k != "mh" && k != "$schema" && k != "commands" && k != "agents" && k != "skills" {
					if _, exists := entry[k]; !exists {
						entry[k] = v
					}
				}
			}
		}

		out = append(out, entry)
	}

	return out
}

// readCookedPluginJSON reads .claude-plugin/plugin.json from a cooked plugin dir.
func readCookedPluginJSON(pluginDir string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// readCookedMCPServers reads .mcp.json from a cooked plugin dir and returns the mcpServers map.
func readCookedMCPServers(pluginDir string) map[string]interface{} {
	data, err := os.ReadFile(filepath.Join(pluginDir, ".mcp.json"))
	if err != nil {
		return nil
	}
	var mcpConfig map[string]interface{}
	if err := json.Unmarshal(data, &mcpConfig); err != nil {
		return nil
	}
	if servers, ok := mcpConfig["mcpServers"]; ok {
		if serversMap, ok := servers.(map[string]interface{}); ok {
			return serversMap
		}
	}
	return nil
}
