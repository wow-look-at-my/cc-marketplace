package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateHookBinaries reads plugin.json hooks config and verifies that any
// command referencing ${CLAUDE_PLUGIN_ROOT}/... points to a file that actually
// exists in the built plugin directory.
func validateHookBinaries(pluginPath string) error {
	pluginJSON := filepath.Join(pluginPath, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginJSON)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no plugin.json, nothing to validate
		}
		return fmt.Errorf("failed to read plugin.json: %w", err)
	}

	var plugin struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &plugin); err != nil {
		return fmt.Errorf("failed to parse plugin.json: %w", err)
	}

	if plugin.Hooks == nil {
		return nil
	}

	const prefix = "${CLAUDE_PLUGIN_ROOT}/"
	var missing []string

	for event, matchers := range plugin.Hooks {
		for _, matcher := range matchers {
			for _, hook := range matcher.Hooks {
				cmd := hook.Command
				if !strings.HasPrefix(cmd, prefix) {
					continue
				}
				relPath := cmd[len(prefix):]
				// Take only the binary path (first token before any space/args)
				if idx := strings.IndexByte(relPath, ' '); idx != -1 {
					relPath = relPath[:idx]
				}
				absPath := filepath.Join(pluginPath, relPath)
				if _, err := os.Stat(absPath); os.IsNotExist(err) {
					missing = append(missing, fmt.Sprintf("  %s hook command %q: file not found at %s", event, cmd, absPath))
				}
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("hook binary validation failed:\n%s", strings.Join(missing, "\n"))
	}

	return nil
}
