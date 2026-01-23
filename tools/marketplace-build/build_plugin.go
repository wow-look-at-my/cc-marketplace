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

// BuildStep represents a single build step from mh.build
type BuildStep struct {
	Type    string   `json:"type"`
	Name    string   `json:"name,omitempty"`
	Source  string   `json:"source,omitempty"`
	Command string   `json:"command,omitempty"`
	Depends []string `json:"depends,omitempty"`
}

// PluginJSON represents the plugin.json structure we care about
type PluginJSON struct {
	MH struct {
		Build map[string]BuildStep `json:"build,omitempty"`
	} `json:"mh,omitempty"`
}

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

	// Run build steps
	if err := runBuildSteps(pluginPath); err != nil {
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

func runBuildSteps(pluginPath string) error {
	// Read plugin.json
	pluginJSONPath := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginJSONPath)
	if err != nil {
		return fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var pj PluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return fmt.Errorf("failed to parse plugin.json: %w", err)
	}

	// If no mh.build, nothing to build
	if len(pj.MH.Build) == 0 {
		fmt.Printf("  (no build steps)\n")
		return nil
	}

	// Topologically sort build steps
	order, err := topoSort(pj.MH.Build)
	if err != nil {
		return fmt.Errorf("failed to resolve build order: %w", err)
	}

	// Execute each step
	for _, stepID := range order {
		step := pj.MH.Build[stepID]
		fmt.Printf("  [%s] %s\n", stepID, step.Type)
		if err := executeStep(pluginPath, stepID, step); err != nil {
			return fmt.Errorf("step %q failed: %w", stepID, err)
		}
	}

	return nil
}

func topoSort(steps map[string]BuildStep) ([]string, error) {
	// Build adjacency and in-degree
	inDegree := make(map[string]int)
	for id := range steps {
		inDegree[id] = 0
	}
	for _, step := range steps {
		for _, dep := range step.Depends {
			if _, ok := steps[dep]; !ok {
				return nil, fmt.Errorf("unknown dependency: %s", dep)
			}
		}
	}
	for id, step := range steps {
		for _, dep := range step.Depends {
			_ = dep // dep must come before id
		}
		inDegree[id] = len(step.Depends)
	}

	// Kahn's algorithm
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var result []string
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		result = append(result, curr)

		// Find steps that depend on curr
		for id, step := range steps {
			for _, dep := range step.Depends {
				if dep == curr {
					inDegree[id]--
					if inDegree[id] == 0 {
						queue = append(queue, id)
					}
				}
			}
		}
	}

	if len(result) != len(steps) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return result, nil
}

func executeStep(pluginPath, stepID string, step BuildStep) error {
	switch step.Type {
	case "go_plugin":
		return executeGoPlugin(pluginPath, stepID, step)
	case "go_test":
		return executeGoTest(pluginPath, step)
	case "exec":
		return executeExec(pluginPath, step)
	default:
		return fmt.Errorf("unknown build step type: %s", step.Type)
	}
}

func executeGoPlugin(pluginPath, stepID string, step BuildStep) error {
	// Determine binary name (use step.Name or stepID)
	name := step.Name
	if name == "" {
		name = stepID
	}

	// Determine source directory
	srcDir := pluginPath
	if step.Source != "" {
		srcDir = filepath.Join(pluginPath, step.Source)
	}

	// Create bin directory
	binDir := filepath.Join(pluginPath, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	// Run go mod tidy
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = srcDir
	tidyCmd.Stdout = os.Stdout
	tidyCmd.Stderr = os.Stderr
	if err := tidyCmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	// Build for linux-amd64
	linuxBin := filepath.Join(binDir, fmt.Sprintf("%s-linux-amd64", name))
	linuxCmd := exec.Command("go", "build", "-o", linuxBin, ".")
	linuxCmd.Dir = srcDir
	linuxCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	linuxCmd.Stdout = os.Stdout
	linuxCmd.Stderr = os.Stderr
	if err := linuxCmd.Run(); err != nil {
		return fmt.Errorf("linux build failed: %w", err)
	}

	// Build for darwin-arm64
	darwinBin := filepath.Join(binDir, fmt.Sprintf("%s-darwin-arm64", name))
	darwinCmd := exec.Command("go", "build", "-o", darwinBin, ".")
	darwinCmd.Dir = srcDir
	darwinCmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0")
	darwinCmd.Stdout = os.Stdout
	darwinCmd.Stderr = os.Stderr
	if err := darwinCmd.Run(); err != nil {
		return fmt.Errorf("darwin build failed: %w", err)
	}

	// Write .go-binary marker
	markerPath := filepath.Join(pluginPath, ".go-binary")
	if err := os.WriteFile(markerPath, []byte(name+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write .go-binary: %w", err)
	}

	// Write run script
	runScript := fmt.Sprintf(runGoPluginScriptTemplate, name)
	if err := os.WriteFile(filepath.Join(pluginPath, "run"), []byte(runScript), 0755); err != nil {
		return fmt.Errorf("failed to write run script: %w", err)
	}

	return nil
}

func executeGoTest(pluginPath string, step BuildStep) error {
	srcDir := pluginPath
	if step.Source != "" {
		srcDir = filepath.Join(pluginPath, step.Source)
	}

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = srcDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func executeExec(pluginPath string, step BuildStep) error {
	if step.Command == "" {
		return fmt.Errorf("exec step requires command")
	}

	cmd := exec.Command("sh", "-c", step.Command)
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

const runGoPluginScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail
OS=$(uname -s | tr 'A-Z' 'a-z')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
esac
exec "$(dirname "$0")/bin/%s-${OS}-${ARCH}" "$@"
`

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
