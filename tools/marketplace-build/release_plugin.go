package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var releasePluginCmd = &cobra.Command{
	Use:   "release-plugin [plugin-name]",
	Short: "Prepare plugin for release (cooks contents into a temp dir for artifact upload)",
	Args:  cobra.ExactArgs(1),
	RunE:  runReleasePlugin,
}

func init() {
	rootCmd.AddCommand(releasePluginCmd)
}

func runReleasePlugin(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	pluginName := args[0]

	repoRoot := getRepoRoot()
	pluginPath := filepath.Join(repoRoot, "plugins", pluginName)

	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return fmt.Errorf("plugin not found: %s", pluginPath)
	}

	newVersion := releaseVersion()

	fmt.Fprintf(os.Stderr, "Preparing %s: version %d\n", pluginName, newVersion)

	sourceCommit, err := GetHeadSHA()
	if err != nil {
		return fmt.Errorf("failed to get HEAD SHA: %w", err)
	}

	// Get repo info for GitHub URL
	owner, repo, err := GetRepoInfo()
	if err != nil {
		return fmt.Errorf("failed to get repo info: %w", err)
	}
	srcURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s/plugins/%s", owner, repo, sourceCommit, pluginName)

	// Cook into a temp dir; the workflow uploads it as an artifact, so do not clean up.
	tmpDir, err := os.MkdirTemp("", "plugin-release-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Copy and cook plugin contents to temp directory
	meta := releaseMetadata{
		SourceCommit: sourceCommit,
		SourceURL:    srcURL,
		BuiltAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := cookPluginForRelease(pluginPath, tmpDir, newVersion, meta); err != nil {
		return fmt.Errorf("failed to cook plugin contents: %w", err)
	}

	if err := validateHooksPreserved(pluginPath, tmpDir); err != nil {
		return fmt.Errorf("hook validation failed: %w", err)
	}

	// Generate package.json for npm registry publishing
	npmPackageName := fmt.Sprintf("%s-%s", owner, pluginName)
	npmVersion := fmt.Sprintf("%d.0.0", newVersion)
	if err := writeNPMPackageJSON(tmpDir, npmPackageName, npmVersion); err != nil {
		return fmt.Errorf("failed to write package.json: %w", err)
	}

	// Output for GitHub Actions (parsed by workflow)
	fmt.Printf("source_dir=%s\n", tmpDir)
	fmt.Printf("package_name=%s\n", npmPackageName)
	fmt.Printf("package_version=%s\n", npmVersion)
	fmt.Printf("message=Release %s\n", pluginName)

	fmt.Fprintf(os.Stderr, "Prepared release in %s\n", tmpDir)
	return nil
}

func writeNPMPackageJSON(dir, packageName, version string) error {
	description := ""
	license := ""
	pluginJSONPath := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if data, err := os.ReadFile(pluginJSONPath); err == nil {
		var pluginJSON map[string]interface{}
		if json.Unmarshal(data, &pluginJSON) == nil {
			if desc, ok := pluginJSON["description"].(string); ok {
				description = desc
			}
			if lic, ok := pluginJSON["license"].(string); ok {
				license = lic
			}
		}
	}

	pkg := map[string]interface{}{
		"name":    packageName,
		"version": version,
	}
	if description != "" {
		pkg["description"] = description
	}
	if license != "" {
		pkg["license"] = license
	}

	data, _ := json.MarshalIndent(pkg, "", "  ")
	return os.WriteFile(filepath.Join(dir, "package.json"), data, 0644)
}

type releaseMetadata struct {
	SourceCommit string `json:"sourceCommit"`
	SourceURL    string `json:"sourceUrl"`
	BuiltAt      string `json:"builtAt"`
}

// releaseVersion returns a monotonically increasing integer version for the
// current build. Uses GITHUB_RUN_NUMBER when set (CI), otherwise 1.
func releaseVersion() int {
	if v := os.Getenv("GITHUB_RUN_NUMBER"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func cookPluginForRelease(srcDir, dstDir string, version int, meta releaseMetadata) error {
	metadataJSON, _ := json.MarshalIndent(meta, "", "\t")
	if err := os.WriteFile(filepath.Join(dstDir, "mh.plugin.json"), metadataJSON, 0644); err != nil {
		return fmt.Errorf("failed to write mh.plugin.json: %w", err)
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dstDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Skip .template. files and Go source files (binaries are in build/)
		if containsTemplate(info.Name()) || isGoSource(info.Name()) {
			return nil
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Cook JSON files
		if filepath.Ext(path) == ".json" {
			data, err = cookJSONForRelease(data, version, relPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cook %s: %v\n", relPath, err)
			}
		}

		// Write to destination
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func containsTemplate(filename string) bool {
	return strings.Contains(filename, ".template.")
}

func isGoSource(filename string) bool {
	return filepath.Ext(filename) == ".go" || filename == "go.mod" || filename == "go.sum"
}

// validateHooksPreserved checks that hooks defined in the source plugin.json
// survive the cook process. Prevents regressions where hook configuration is
// silently dropped, leaving the plugin unable to register hooks at runtime.
func validateHooksPreserved(srcDir, dstDir string) error {
	srcPath := filepath.Join(srcDir, ".claude-plugin", "plugin.json")
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read source plugin.json: %w", err)
	}

	var srcJSON map[string]interface{}
	if err := json.Unmarshal(srcData, &srcJSON); err != nil {
		return nil
	}

	srcHooks, ok := srcJSON["hooks"]
	if !ok || srcHooks == nil {
		return nil
	}

	srcHooksMap, ok := srcHooks.(map[string]interface{})
	if !ok || len(srcHooksMap) == 0 {
		return nil
	}

	dstPath := filepath.Join(dstDir, ".claude-plugin", "plugin.json")
	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		return fmt.Errorf("source plugin.json defines hooks but cooked plugin.json is missing — hooks will not register at runtime")
	}

	var dstJSON map[string]interface{}
	if err := json.Unmarshal(dstData, &dstJSON); err != nil {
		return fmt.Errorf("cooked plugin.json is invalid JSON — hooks will not register at runtime: %w", err)
	}

	dstHooks, ok := dstJSON["hooks"]
	if !ok || dstHooks == nil {
		return fmt.Errorf("source plugin.json defines hooks but cooked plugin.json does not — hooks will not register at runtime")
	}

	dstHooksMap, ok := dstHooks.(map[string]interface{})
	if !ok || len(dstHooksMap) == 0 {
		return fmt.Errorf("source plugin.json defines hooks but cooked plugin.json has empty hooks — hooks will not register at runtime")
	}

	for event := range srcHooksMap {
		if _, ok := dstHooksMap[event]; !ok {
			return fmt.Errorf("source plugin.json defines %q hooks but cooked plugin.json does not — hook will not register at runtime", event)
		}
	}

	return nil
}

func cookJSONForRelease(data []byte, version int, relPath string) ([]byte, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return data, err
	}

	// Remove $schema
	delete(obj, "$schema")

	// Remove mh.* keys
	delete(obj, "mh")

	// Add version to plugin.json
	if filepath.Base(relPath) == "plugin.json" {
		obj["version"] = fmt.Sprintf("%d", version)
	}

	return json.MarshalIndent(obj, "", "\t")
}
