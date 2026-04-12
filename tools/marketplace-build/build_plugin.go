package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var crossBuildTargets = []string{"linux,darwin", "amd64,arm64"}

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

	justfilePath := filepath.Join(pluginPath, "justfile")
	if _, err := os.Stat(justfilePath); err == nil {
		if err := validateJustfile(justfilePath); err != nil {
			return err
		}
	}

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

	// Validate hook binary paths exist after build
	if err := validateHookBinaries(pluginPath); err != nil {
		return err
	}

	return nil
}

var forbiddenJustfilePattern = regexp.MustCompile(`\b(go\s+build|go\s+test|go-toolchain|go-safe-build)\b`)

func validateJustfile(justfilePath string) error {
	f, err := os.Open(justfilePath)
	if err != nil {
		return fmt.Errorf("failed to read justfile: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		// Skip comments
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := forbiddenJustfilePattern.FindString(line); m != "" {
			return fmt.Errorf("justfile:%d: forbidden command %q — go-toolchain is invoked automatically by the build system", lineNum, m)
		}
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
	fmt.Printf("  Invoking go-toolchain matrix (https://github.com/%s/releases/latest)\n", goToolchainRepo)

	toolchainDir, err := os.MkdirTemp("", "go-toolchain-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(toolchainDir)

	// Download latest release assets
	dlCmd := exec.Command("gh", "release", "download",
		"--repo", goToolchainRepo,
		"--pattern", "*",
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

	// Run toolchain with matrix cross-compilation
	runCmd := exec.Command(binPath, "matrix",
		"--os", crossBuildTargets[0],
		"--arch", crossBuildTargets[1])
	runCmd.Dir = pluginPath
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("go-toolchain matrix failed: %w", err)
	}

	// Replace symlinks with a platform-detection shell script wrapper
	moduleName, err := getGoModuleName(pluginPath)
	if err != nil {
		return fmt.Errorf("failed to read module name: %w", err)
	}
	if err := generatePlatformWrapper(filepath.Join(pluginPath, "build"), moduleName); err != nil {
		return fmt.Errorf("failed to generate platform wrapper: %w", err)
	}

	return nil
}

// getGoModuleName reads go.mod and returns the module name.
func getGoModuleName(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(line[len("module "):]), nil
		}
	}
	return "", fmt.Errorf("no module directive found in go.mod")
}

const platformWrapperScript = `#!/bin/sh
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac
BINARY="$SCRIPT_DIR/$(basename "$0")_${OS}_${ARCH}"
if [ ! -f "$BINARY" ]; then
  exit 0
fi
exec "$BINARY" "$@"
`

// generatePlatformWrapper removes the symlinks created by go-toolchain matrix
// and writes a shell script that detects the platform and execs the right binary.
func generatePlatformWrapper(buildDir, name string) error {
	wrapperPath := filepath.Join(buildDir, name)
	hostLink := filepath.Join(buildDir, name+"_host")

	// Remove symlinks (go-toolchain matrix creates these)
	os.Remove(wrapperPath)
	os.Remove(hostLink)

	if err := os.WriteFile(wrapperPath, []byte(platformWrapperScript), 0755); err != nil {
		return fmt.Errorf("failed to write wrapper script: %w", err)
	}

	fmt.Printf("  Generated platform wrapper: build/%s\n", name)
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
