package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var testPluginCmd = &cobra.Command{
	Use:   "test-plugin [plugin-name]",
	Short: "Run tests for a single plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  runTestPlugin,
}

func init() {
	rootCmd.AddCommand(testPluginCmd)
}

func runTestPlugin(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	pluginName := args[0]

	repoRoot := getRepoRoot()
	pluginPath := filepath.Join(repoRoot, "plugins", pluginName)

	// Verify plugin exists
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return fmt.Errorf("plugin not found: %s", pluginPath)
	}

	fmt.Printf("Testing %s\n", pluginName)

	// Find go.mod to determine source directory
	srcDir := findGoModDir(pluginPath)
	if srcDir == "" {
		fmt.Printf("  (no go.mod found, skipping tests)\n")
		return nil
	}

	fmt.Printf("  go test\n")
	coverFile := filepath.Join("/tmp", fmt.Sprintf("coverage-%s.out", pluginName))
	testCmd := exec.Command("go", "test", "-v", "-coverprofile="+coverFile, "./...")
	testCmd.Dir = srcDir
	testCmd.Env = append(os.Environ(), "REPO_ROOT="+repoRoot)
	testCmd.Stdout = os.Stdout
	testCmd.Stderr = os.Stderr
	if err := testCmd.Run(); err != nil {
		return fmt.Errorf("tests failed: %w", err)
	}

	// Show coverage percentage
	covCmd := exec.Command("go", "tool", "cover", "-func="+coverFile)
	covOut, _ := covCmd.Output()
	lines := strings.Split(string(covOut), "\n")
	pct := "unknown"
	for _, line := range lines {
		if strings.HasPrefix(line, "total:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				pct = parts[len(parts)-1]
			}
			break
		}
	}
	fmt.Printf("  Coverage: %s (%s)\n", pct, coverFile)

	return nil
}
