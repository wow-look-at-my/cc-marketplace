package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// pluginReleaseManifest is what a cooked plugin carries for the marketplace
// job: which orphan tag now serves it, and the cooked manifests
// update-marketplace mirrors into marketplace.json so it needs no second copy
// of the tree.
//
// It replaced an npm-tarball manifest. Distribution is a git tag now, so there
// is no tarball path, no package name and no registry URL -- the tag IS the
// artifact.
type pluginReleaseManifest struct {
	Name       string                 `json:"name"`
	Version    string                 `json:"version"`
	Tag        string                 `json:"tag"`
	PluginJSON map[string]interface{} `json:"pluginJson,omitempty"`
	MCPJSON    map[string]interface{} `json:"mcpJson,omitempty"`
}

type packagedPlugin struct {
	name     string
	dir      string
	manifest pluginReleaseManifest
}

// writeReleaseManifest records the release facts inside the cooked directory,
// which is also what gets published as the orphan tag.
func writeReleaseManifest(cookedDir, name, version, tag string) error {
	m := pluginReleaseManifest{Name: name, Version: version, Tag: tag}
	if data, err := os.ReadFile(filepath.Join(cookedDir, ".claude-plugin", "plugin.json")); err == nil {
		_ = json.Unmarshal(data, &m.PluginJSON)
	}
	if data, err := os.ReadFile(filepath.Join(cookedDir, ".mcp.json")); err == nil {
		_ = json.Unmarshal(data, &m.MCPJSON)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cookedDir, "manifest.json"), data, 0o644)
}

// readPackagedPlugins enumerates subdirectories of inputDir as released
// plugins, each holding the manifest.json release-plugin wrote.
//
// Every failure here is FATAL, deliberately. This used to warn and skip, which
// meant a plugin whose manifest failed to write silently vanished from
// marketplace.json -- users could no longer install it and every check stayed
// green, which is the exact shape of failure a release pipeline must not have.
func readPackagedPlugins(inputDir string) ([]packagedPlugin, error) {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return nil, fmt.Errorf("read input dir %s: %w", inputDir, err)
	}

	var plugins []packagedPlugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(inputDir, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			return nil, fmt.Errorf("%s: no manifest.json (release-plugin did not run, or its output was not uploaded): %w", e.Name(), err)
		}
		var m pluginReleaseManifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("%s: unreadable manifest.json: %w", e.Name(), err)
		}
		if m.Name == "" || m.Version == "" || m.Tag == "" {
			return nil, fmt.Errorf("%s: manifest.json is missing name, version or tag (got %+v)", e.Name(), m)
		}
		plugins = append(plugins, packagedPlugin{name: e.Name(), dir: dir, manifest: m})
	}

	sort.Slice(plugins, func(i, j int) bool { return plugins[i].name < plugins[j].name })
	return plugins, nil
}
