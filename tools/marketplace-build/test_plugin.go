package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type testEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
	Output string `json:"Output"`
}

func parseTestJSON(data []byte) (passed, failed int, coverage float64) {
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		// Extract coverage from output events
		if ev.Action == "output" && strings.HasPrefix(ev.Output, "coverage:") {
			// Format: "coverage: 81.0% of statements\n"
			parts := strings.Fields(ev.Output)
			if len(parts) >= 2 {
				pct := strings.TrimSuffix(parts[1], "%")
				coverage, _ = strconv.ParseFloat(pct, 64)
			}
			continue
		}

		if ev.Test == "" {
			continue // package-level event
		}
		switch ev.Action {
		case "pass":
			passed++
		case "fail":
			failed++
		}
	}
	return
}

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

	// Check for go.mod
	if _, err := os.Stat(filepath.Join(pluginPath, "go.mod")); os.IsNotExist(err) {
		fmt.Printf("  (no go.mod found, skipping tests)\n")
		return nil
	}

	testCmd := exec.Command("go", "test", "-json", "-cover", "./...")
	testCmd.Dir = pluginPath
	testCmd.Env = append(os.Environ(), "REPO_ROOT="+repoRoot)
	out, err := testCmd.Output()

	// Parse JSON output for summary
	passed, failed, coverage := parseTestJSON(out)
	if failed > 0 {
		fmt.Printf("  %d passed, %d failed\n", passed, failed)
		return fmt.Errorf("tests failed")
	}
	fmt.Printf("  %d passed, %.1f%% coverage\n", passed, coverage)

	if err != nil {
		return fmt.Errorf("tests failed: %w", err)
	}

	return nil
}
