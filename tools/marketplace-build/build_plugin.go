package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var buildTargets = []struct {
	goos, goarch string
}{
	{"linux", "amd64"},
	{"darwin", "arm64"},
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

	// Check for go.mod
	if _, err := os.Stat(filepath.Join(pluginPath, "go.mod")); os.IsNotExist(err) {
		fmt.Printf("  (no go.mod found, skipping build)\n")
		return nil
	}

	fmt.Printf("Building %s\n", pluginName)

	// Create bin directory
	binDir := filepath.Join(pluginPath, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	// Run go mod tidy
	fmt.Printf("  go mod tidy\n")
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = pluginPath
	tidyCmd.Stdout = os.Stdout
	tidyCmd.Stderr = os.Stderr
	if err := tidyCmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	// Build for each target
	for _, t := range buildTargets {
		fmt.Printf("  go build (%s-%s)\n", t.goos, t.goarch)
		binPath := filepath.Join(binDir, fmt.Sprintf("%s-%s-%s", pluginName, t.goos, t.goarch))
		buildCmd := exec.Command("go", "build", "-o", binPath, "./...")
		buildCmd.Dir = pluginPath
		buildCmd.Env = append(os.Environ(), "GOOS="+t.goos, "GOARCH="+t.goarch, "CGO_ENABLED=0")
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("%s-%s build failed: %w", t.goos, t.goarch, err)
		}
	}

	// Write run script
	runScript := fmt.Sprintf(runGoPluginScriptTemplate, pluginName)
	if err := os.WriteFile(filepath.Join(pluginPath, "run"), []byte(runScript), 0755); err != nil {
		return fmt.Errorf("failed to write run script: %w", err)
	}

	return nil
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
