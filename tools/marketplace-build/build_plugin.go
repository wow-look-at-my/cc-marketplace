package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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

	// Go compilation is handled by the wow-look-at-my/go-toolchain action in CI.

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

