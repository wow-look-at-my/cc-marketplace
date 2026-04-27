package main

import (
	"fmt"
	"os"
	"path/filepath"

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

	// Go plugins are built and tested by the wow-look-at-my/go-toolchain action in CI.
	fmt.Printf("Testing %s\n", pluginName)
	fmt.Printf("  (no additional tests to run)\n")
	return nil
}
