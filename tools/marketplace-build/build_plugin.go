package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const goToolchainRepo = "wow-look-at-my/go-toolchain"

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

	// Run prebuild recipe if available
	if err := runJustRecipe(pluginPath, "prebuild"); err != nil {
		return fmt.Errorf("prebuild failed: %w", err)
	}

	// If .go files found, invoke go-toolchain
	if hasGoFiles(pluginPath) {
		if err := runGoToolchain(pluginPath); err != nil {
			return fmt.Errorf("go-toolchain build failed: %w", err)
		}
	}

	// Run postbuild recipe if available
	if err := runJustRecipe(pluginPath, "postbuild"); err != nil {
		return fmt.Errorf("postbuild failed: %w", err)
	}

	return nil
}

func hasGoFiles(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".go" {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func runJustRecipe(dir, recipe string) error {
	if _, err := os.Stat(filepath.Join(dir, "justfile")); os.IsNotExist(err) {
		return nil
	}

	// Check if recipe exists
	summaryCmd := exec.Command("just", "--summary")
	summaryCmd.Dir = dir
	out, err := summaryCmd.Output()
	if err != nil {
		return nil
	}

	found := false
	for _, name := range strings.Fields(string(out)) {
		if name == recipe {
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	fmt.Printf("  just %s\n", recipe)
	cmd := exec.Command("just", recipe)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runGoToolchain(pluginPath string) error {
	fmt.Printf("  Invoking go-toolchain (https://github.com/%s/releases/latest)\n", goToolchainRepo)

	toolchainDir, err := os.MkdirTemp("", "go-toolchain-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(toolchainDir)

	// Download latest release assets
	dlCmd := exec.Command("gh", "release", "download",
		"--repo", goToolchainRepo,
		"-D", toolchainDir)
	dlCmd.Stdout = os.Stdout
	dlCmd.Stderr = os.Stderr
	if err := dlCmd.Run(); err != nil {
		return fmt.Errorf("failed to download go-toolchain: %w", err)
	}

	// Find binary matching current platform
	binPath, err := findToolchainBinary(toolchainDir)
	if err != nil {
		return err
	}

	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("failed to make go-toolchain executable: %w", err)
	}

	// Run toolchain in plugin directory
	runCmd := exec.Command(binPath)
	runCmd.Dir = pluginPath
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("go-toolchain failed: %w", err)
	}

	return nil
}

func findToolchainBinary(dir string) (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to read toolchain dir: %w", err)
	}

	// First pass: look for platform-specific binary
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, goos) && strings.Contains(name, goarch) {
			return filepath.Join(dir, name), nil
		}
	}

	// Second pass: use first file found
	for _, e := range entries {
		if !e.IsDir() {
			return filepath.Join(dir, e.Name()), nil
		}
	}

	return "", fmt.Errorf("no go-toolchain binary found in release assets")
}
