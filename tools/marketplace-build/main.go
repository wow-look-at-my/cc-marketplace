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

var updateMarketplaceCmd = &cobra.Command{
	Use:   "update-marketplace",
	Short: "Build marketplace.json from cooked plugin artifacts",
	RunE:  runUpdateMarketplace,
}

func init() {
	updateMarketplaceCmd.Flags().StringVar(&updateMarketplaceInput, "input", "", "directory of cooked plugin subdirectories (one per plugin)")
	rootCmd.AddCommand(prepareMatrixCmd)
	rootCmd.AddCommand(updateMarketplaceCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
