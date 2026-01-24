package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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

	fmt.Printf("Building %s\n", pluginName)

	// Find go.mod to determine source directory
	srcDir := findGoModDir(pluginPath)
	if srcDir == "" {
		fmt.Printf("  (no go.mod found, skipping build)\n")
		return nil
	}

	// Create bin directory
	binDir := filepath.Join(pluginPath, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	// Run go mod tidy
	fmt.Printf("  go mod tidy\n")
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = srcDir
	tidyCmd.Stdout = os.Stdout
	tidyCmd.Stderr = os.Stderr
	if err := tidyCmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	// Build for linux-amd64
	fmt.Printf("  go build (linux-amd64)\n")
	linuxBin := filepath.Join(binDir, fmt.Sprintf("%s-linux-amd64", pluginName))
	linuxCmd := exec.Command("go", "build", "-o", linuxBin, ".")
	linuxCmd.Dir = srcDir
	linuxCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	linuxCmd.Stdout = os.Stdout
	linuxCmd.Stderr = os.Stderr
	if err := linuxCmd.Run(); err != nil {
		return fmt.Errorf("linux build failed: %w", err)
	}

	// Build for darwin-arm64
	fmt.Printf("  go build (darwin-arm64)\n")
	darwinBin := filepath.Join(binDir, fmt.Sprintf("%s-darwin-arm64", pluginName))
	darwinCmd := exec.Command("go", "build", "-o", darwinBin, ".")
	darwinCmd.Dir = srcDir
	darwinCmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0")
	darwinCmd.Stdout = os.Stdout
	darwinCmd.Stderr = os.Stderr
	if err := darwinCmd.Run(); err != nil {
		return fmt.Errorf("darwin build failed: %w", err)
	}

	// Write run script
	runScript := fmt.Sprintf(runGoPluginScriptTemplate, pluginName)
	if err := os.WriteFile(filepath.Join(pluginPath, "run"), []byte(runScript), 0755); err != nil {
		return fmt.Errorf("failed to write run script: %w", err)
	}

	return nil
}

func findGoModDir(pluginPath string) string {
	// Check plugin root
	if _, err := os.Stat(filepath.Join(pluginPath, "go.mod")); err == nil {
		return pluginPath
	}
	// Check cmd subdirectory
	cmdDir := filepath.Join(pluginPath, "cmd")
	if _, err := os.Stat(filepath.Join(cmdDir, "go.mod")); err == nil {
		return cmdDir
	}
	return ""
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
