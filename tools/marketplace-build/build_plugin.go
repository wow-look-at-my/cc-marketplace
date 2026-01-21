package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func runBuildPlugin(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	pluginName := args[0]

	repoRoot := getRepoRoot()
	pluginPath := filepath.Join(repoRoot, "plugins", pluginName)

	// Verify plugin exists
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return fmt.Errorf("plugin not found: %s", pluginPath)
	}

	// Get current version and bump it
	currentVersion, err := GetLatestTagVersion(pluginName)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}
	newVersion := currentVersion + 1

	fmt.Printf("Building %s: v%d -> v%d\n", pluginName, currentVersion, newVersion)

	// Run just build in plugin directory
	if err := runJustBuild(pluginPath); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Get source commit SHA for change detection
	sourceCommit, err := GetHeadSHA()
	if err != nil {
		return fmt.Errorf("failed to get HEAD SHA: %w", err)
	}

	// Get repo info for GitHub URL
	owner, repo, err := GetRepoInfo()
	if err != nil {
		return fmt.Errorf("failed to get repo info: %w", err)
	}

	versionTag := fmt.Sprintf("plugin/%s/v%d", pluginName, newVersion)
	distURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, versionTag)
	srcURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s/plugins/%s", owner, repo, sourceCommit, pluginName)

	// Create temp directory for cooked plugin contents
	tmpDir, err := os.MkdirTemp("", "plugin-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Copy and cook plugin contents to temp directory
	meta := buildMetadata{
		SourceCommit: sourceCommit,
		SourceURL:    srcURL,
		DistURL:      distURL,
	}
	if err := cookPluginContents(pluginPath, tmpDir, newVersion, meta); err != nil {
		return fmt.Errorf("failed to cook plugin contents: %w", err)
	}

	// Create orphan commit
	commitMsg := fmt.Sprintf("Release %s v%d", pluginName, newVersion)
	commitSHA, err := CreateOrphanCommit(tmpDir, commitMsg)
	if err != nil {
		return fmt.Errorf("failed to create orphan commit: %w", err)
	}

	fmt.Printf("Created orphan commit: %s\n", commitSHA)

	// Create and push version tag
	if err := CreateTag(versionTag, commitSHA); err != nil {
		return fmt.Errorf("failed to create version tag: %w", err)
	}

	if err := PushTags(versionTag); err != nil {
		return fmt.Errorf("failed to push version tag: %w", err)
	}

	fmt.Printf("Released %s v%d (tag: %s)\n", pluginName, newVersion, versionTag)
	return nil
}

func runJustBuild(pluginPath string) error {
	// Check if justfile exists
	justfilePath := filepath.Join(pluginPath, "justfile")
	if _, err := os.Stat(justfilePath); os.IsNotExist(err) {
		fmt.Printf("No justfile found, skipping build step\n")
		return nil
	}

	cmd := exec.Command("just", "build")
	cmd.Dir = pluginPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

type buildMetadata struct {
	SourceCommit string `json:"sourceCommit"`
	SourceURL    string `json:"sourceUrl"`
	DistURL      string `json:"distUrl"`
	BuiltAt      string `json:"builtAt"`
}

func cookPluginContents(srcDir, dstDir string, version int, meta buildMetadata) error {
	meta.BuiltAt = time.Now().UTC().Format(time.RFC3339)
	metadataJSON, _ := json.MarshalIndent(meta, "", "  ")
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

		// Skip .template. files
		if filepath.Base(path) != info.Name() {
			// This shouldn't happen, but just in case
		}
		if containsTemplate(info.Name()) {
			return nil
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Cook JSON files
		if filepath.Ext(path) == ".json" {
			data, err = cookJSON(data, version, relPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to cook %s: %v\n", relPath, err)
				// Still copy the file as-is
			}
		}

		// Write to destination
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func containsTemplate(filename string) bool {
	return len(filename) > 10 && filename[len(filename)-10:] == ".template."
}

func cookJSON(data []byte, version int, relPath string) ([]byte, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return data, err // Return original if not valid JSON
	}

	// Remove $schema
	delete(obj, "$schema")

	// Remove mh.* keys
	delete(obj, "mh")

	// Add version to plugin.json
	if filepath.Base(relPath) == "plugin.json" {
		obj["version"] = fmt.Sprintf("%d", version)
	}

	return json.MarshalIndent(obj, "", "  ")
}
