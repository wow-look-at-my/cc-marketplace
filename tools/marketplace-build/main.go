package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "marketplace-build",
	Short: "CI automation for Claude Code plugin marketplaces",
}

var prepareMatrixCmd = &cobra.Command{
	Use:   "prepare-matrix",
	Short: "Output changed plugins in GITHUB_OUTPUT format",
	RunE:  runPrepareMatrix,
}

var buildPluginCmd = &cobra.Command{
	Use:   "build-plugin [plugin-name]",
	Short: "Build a plugin, bump version, create orphan tag, push",
	Args:  cobra.ExactArgs(1),
	RunE:  runBuildPlugin,
}

var updateMarketplaceCmd = &cobra.Command{
	Use:   "update-marketplace",
	Short: "Scan tags, update marketplace.json, create marketplace tag",
	RunE:  runUpdateMarketplace,
}

var cleanupBranchCmd = &cobra.Command{
	Use:   "cleanup-branch [branch-name]",
	Short: "Delete all tags for a deleted branch",
	Args:  cobra.ExactArgs(1),
	RunE:  runCleanupBranch,
}

func init() {
	rootCmd.AddCommand(prepareMatrixCmd)
	rootCmd.AddCommand(buildPluginCmd)
	rootCmd.AddCommand(updateMarketplaceCmd)
	rootCmd.AddCommand(cleanupBranchCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
