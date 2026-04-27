package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var buildNPMRegistryCmd = &cobra.Command{
	Use:   "build-npm-registry",
	Short: "Generate static npm registry files for GitHub Pages deployment",
	RunE:  runBuildNPMRegistry,
}

func init() {
	rootCmd.AddCommand(buildNPMRegistryCmd)
}

func runBuildNPMRegistry(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	owner, repo, err := GetRepoInfo()
	if err != nil {
		return err
	}

	pagesBase := fmt.Sprintf("https://%s.github.io/%s", owner, repo)

	pluginRefs, err := getPluginRefs(owner, repo)
	if err != nil {
		return fmt.Errorf("failed to get plugin refs: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "npm-registry-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	for pluginName, latestTag := range pluginRefs {
		pkgName := fmt.Sprintf("%s-%s", owner, pluginName)
		latestVersion := semverFromTag(latestTag)

		allTags, err := ListPluginTags(pluginName)
		if err != nil || len(allTags) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: no tags for %s: %v\n", pluginName, err)
			continue
		}

		tarballDir := filepath.Join(tmpDir, "tarballs", pkgName)
		if err := os.MkdirAll(tarballDir, 0755); err != nil {
			return fmt.Errorf("failed to create tarball dir for %s: %w", pkgName, err)
		}

		versions := make(map[string]interface{})
		for _, tag := range allTags {
			version := semverFromTag(tag)
			tarballName := fmt.Sprintf("%s-%s.tgz", pkgName, version)
			tarballPath := filepath.Join(tarballDir, tarballName)
			tarballURL := fmt.Sprintf("%s/tarballs/%s/%s", pagesBase, pkgName, tarballName)

			if err := gitArchiveTag(tag, tarballPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to archive %s: %v\n", tag, err)
				continue
			}

			versions[version] = map[string]interface{}{
				"name":    pkgName,
				"version": version,
				"dist": map[string]interface{}{
					"tarball": tarballURL,
				},
			}
		}

		if len(versions) == 0 {
			continue
		}

		packument := map[string]interface{}{
			"name": pkgName,
			"dist-tags": map[string]interface{}{
				"latest": latestVersion,
			},
			"versions": versions,
		}

		data, _ := json.MarshalIndent(packument, "", "  ")
		if err := os.WriteFile(filepath.Join(tmpDir, pkgName), data, 0644); err != nil {
			return fmt.Errorf("failed to write packument for %s: %w", pkgName, err)
		}

		fmt.Fprintf(os.Stderr, "  %s: %d version(s)\n", pkgName, len(versions))
	}

	fmt.Printf("registry_dir=%s\n", tmpDir)
	fmt.Fprintf(os.Stderr, "Built npm registry in %s\n", tmpDir)
	return nil
}

// gitArchiveTag is a variable so tests can replace it.
var gitArchiveTag = gitArchiveTagReal

func gitArchiveTagReal(tag, outputPath string) error {
	cmd := exec.Command("git", "archive", "--format=tgz", "-o", outputPath, tag)
	cmd.Dir = getRepoRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}
