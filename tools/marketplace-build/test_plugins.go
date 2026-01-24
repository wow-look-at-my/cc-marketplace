package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var testPluginsCmd = &cobra.Command{
	Use:   "test-plugins [plugin-name...]",
	Short: "Run tests for plugins (all if no args, or specific plugins)",
	RunE:  runTestPlugins,
}

func init() {
	rootCmd.AddCommand(testPluginsCmd)
}

func runTestPlugins(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	repoRoot := getRepoRoot()
	pluginsDir := filepath.Join(repoRoot, "plugins")

	var pluginsToTest []string

	if len(args) > 0 {
		pluginsToTest = args
	} else {
		// Find all plugins with mh.include_in_marketplace: true
		entries, err := os.ReadDir(pluginsDir)
		if err != nil {
			return fmt.Errorf("failed to read plugins dir: %w", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pluginPath := filepath.Join(pluginsDir, entry.Name())
			if isTestablePlugin(pluginPath) {
				pluginsToTest = append(pluginsToTest, entry.Name())
			}
		}
	}

	if len(pluginsToTest) == 0 {
		fmt.Println("No testable plugins found")
		return nil
	}

	var failed []string
	for _, pluginName := range pluginsToTest {
		pluginPath := filepath.Join(pluginsDir, pluginName)
		fmt.Printf("Testing %s...\n", pluginName)

		if err := runPluginTests(pluginPath); err != nil {
			fmt.Printf("  FAILED: %v\n", err)
			failed = append(failed, pluginName)
		} else {
			fmt.Printf("  PASSED\n")
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("tests failed for: %v", failed)
	}

	return nil
}

func isTestablePlugin(pluginPath string) bool {
	pluginJSONPath := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginJSONPath)
	if err != nil {
		return false
	}

	var pj struct {
		MH struct {
			IncludeInMarketplace bool `json:"include_in_marketplace"`
		} `json:"mh"`
	}
	if err := json.Unmarshal(data, &pj); err != nil {
		return false
	}

	return pj.MH.IncludeInMarketplace
}

func runPluginTests(pluginPath string) error {
	pluginJSONPath := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginJSONPath)
	if err != nil {
		return fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var pj PluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return fmt.Errorf("failed to parse plugin.json: %w", err)
	}

	// Find test steps
	hasTests := false
	for stepID, step := range pj.MH.Build {
		if step.Type == "go_test" {
			hasTests = true
			fmt.Printf("  [%s] running go test\n", stepID)

			srcDir := pluginPath
			if step.Source != "" {
				srcDir = filepath.Join(pluginPath, step.Source)
			}

			coverFile := filepath.Join("/tmp", fmt.Sprintf("coverage-%s.out", filepath.Base(pluginPath)))
			cmd := exec.Command("go", "test", "-v", "-coverprofile="+coverFile, "./...")
			cmd.Dir = srcDir
			cmd.Env = append(os.Environ(), "REPO_ROOT="+getRepoRoot())
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return err
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
		}
	}

	if !hasTests {
		fmt.Printf("  (no test steps)\n")
	}

	return nil
}
