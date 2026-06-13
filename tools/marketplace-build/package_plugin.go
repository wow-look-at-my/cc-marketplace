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

// pluginPackageManifest describes the npm tarballs produced for a single plugin
// by `marketplace-build package-plugin`. The pages job uses it to know which
// tarballs to publish and how to build the npm packument metadata, and embeds
// the cooked plugin.json + .mcp.json so update-marketplace doesn't need a
// separate copy of the cooked tree.
type pluginPackageManifest struct {
	Name       string                 `json:"name"`
	Version    string                 `json:"version"`
	Main       manifestTarball        `json:"main"`
	PluginJSON map[string]interface{} `json:"pluginJson,omitempty"`
	MCPJSON    map[string]interface{} `json:"mcpJson,omitempty"`
}

type manifestTarball struct {
	// Tarball is a forward-slash-delimited path relative to the per-plugin
	// output directory (e.g. "tarballs/owner-plugin/owner-plugin-1.0.0.tgz").
	Tarball string `json:"tarball"`
}

var packagePluginInput string
var packagePluginOutput string

var packagePluginCmd = &cobra.Command{
	Use:   "package-plugin",
	Short: "Build npm tarballs for a single cooked plugin",
	RunE:  runPackagePlugin,
}

func init() {
	packagePluginCmd.Flags().StringVar(&packagePluginInput, "input", "", "cooked plugin directory (output of release-plugin)")
	packagePluginCmd.Flags().StringVar(&packagePluginOutput, "output", "", "output directory for tarballs and manifest.json")
	rootCmd.AddCommand(packagePluginCmd)
}

func runPackagePlugin(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	if packagePluginInput == "" {
		return fmt.Errorf("--input is required: cooked plugin directory")
	}
	if packagePluginOutput == "" {
		return fmt.Errorf("--output is required: directory for tarballs and manifest.json")
	}

	pkgData, err := os.ReadFile(filepath.Join(packagePluginInput, "package.json"))
	if err != nil {
		return fmt.Errorf("read package.json: %w", err)
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		return fmt.Errorf("parse package.json: %w", err)
	}
	if pkg.Name == "" || pkg.Version == "" {
		return fmt.Errorf("package.json missing name or version")
	}

	if err := packagePluginToDir(packagePluginInput, pkg.Name, pkg.Version, packagePluginOutput); err != nil {
		return err
	}

	fmt.Printf("output_dir=%s\n", packagePluginOutput)
	fmt.Fprintf(os.Stderr, "Packaged %s %s in %s\n", pkg.Name, pkg.Version, packagePluginOutput)
	return nil
}

// packageSlug converts a package name (possibly scoped, e.g. @scope/name) to a
// filesystem-safe slug by stripping the leading @ and replacing / with -.
func packageSlug(pkgName string) string {
	s := strings.TrimPrefix(pkgName, "@")
	return strings.ReplaceAll(s, "/", "-")
}

// packagePluginToDir builds the single npm tarball for a cooked plugin. When the
// plugin ships cross-platform binaries, all of them are bundled into that one
// tarball behind a launcher wrapper (see buildMainPackageDir) -- there are no
// separate per-platform packages, so the published artifact always contains the
// binary it runs. The output dir contains manifest.json plus
// tarballs/<slug>/<slug>-<version>.tgz.
func packagePluginToDir(cookedDir, pkgName, version, outDir string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	platforms := detectPlatformBinaries(cookedDir)

	manifest := pluginPackageManifest{
		Name:    pkgName,
		Version: version,
	}

	if data, err := os.ReadFile(filepath.Join(cookedDir, ".claude-plugin", "plugin.json")); err == nil {
		var pj map[string]interface{}
		if err := json.Unmarshal(data, &pj); err == nil {
			manifest.PluginJSON = pj
		}
	}
	if data, err := os.ReadFile(filepath.Join(cookedDir, ".mcp.json")); err == nil {
		var mj map[string]interface{}
		if err := json.Unmarshal(data, &mj); err == nil {
			manifest.MCPJSON = mj
		}
	}

	slug := packageSlug(pkgName)
	mainTarballRel := filepath.ToSlash(filepath.Join("tarballs", slug, fmt.Sprintf("%s-%s.tgz", slug, version)))
	mainTarballPath := filepath.Join(outDir, filepath.FromSlash(mainTarballRel))
	if err := os.MkdirAll(filepath.Dir(mainTarballPath), 0755); err != nil {
		return err
	}

	if len(platforms) == 0 {
		if err := createTarball(cookedDir, mainTarballPath); err != nil {
			return fmt.Errorf("create main tarball: %w", err)
		}
	} else {
		// Bundle every per-platform binary into the one plugin package behind a
		// launcher wrapper. No separate platform packages means no
		// optionalDependencies that could go unpublished and leave the plugin a
		// no-op.
		mainDir, err := buildMainPackageDir(cookedDir, platforms)
		if err != nil {
			return fmt.Errorf("build main package dir: %w", err)
		}
		err = createTarball(mainDir, mainTarballPath)
		os.RemoveAll(mainDir)
		if err != nil {
			return fmt.Errorf("create main tarball: %w", err)
		}
		if err := validateBundledBinaries(mainTarballPath, platforms); err != nil {
			return fmt.Errorf("main tarball validation failed: %w", err)
		}
	}

	manifest.Main = manifestTarball{Tarball: mainTarballRel}

	if err := validateMainTarball(mainTarballPath, manifest.PluginJSON); err != nil {
		return fmt.Errorf("main tarball validation failed: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(outDir, "manifest.json"), data, 0644)
}

// validateBundledBinaries verifies the bundled plugin tarball actually contains
// every per-platform binary the launcher wrapper will exec
// (build/<name>_<goos>_<goarch>). Without this a plugin could publish a wrapper
// whose target binaries are absent -- installing cleanly but silently doing
// nothing, the exact failure that left every binary plugin a no-op.
var validateBundledBinaries = validateBundledBinariesReal

func validateBundledBinariesReal(tarballPath string, bins []platformBinary) error {
	contents, err := tarballContents(tarballPath)
	if err != nil {
		return fmt.Errorf("list tarball: %w", err)
	}
	have := make(map[string]bool, len(contents))
	for _, f := range contents {
		have[f] = true
	}
	for _, b := range bins {
		want := fmt.Sprintf("package/build/%s_%s_%s", b.name, b.goOS, b.goArch)
		if !have[want] {
			return fmt.Errorf("bundled tarball is missing platform binary %q", want)
		}
	}
	return nil
}

// validateMainTarball extracts the main tarball and verifies that required files
// are present. If the plugin declares hooks, .claude-plugin/plugin.json must be
// in the tarball — without it Claude Code cannot register the hooks at runtime.
var validateMainTarball = validateMainTarballReal

func validateMainTarballReal(tarballPath string, pluginJSON map[string]interface{}) error {
	if pluginJSON == nil {
		return nil
	}

	contents, err := tarballContents(tarballPath)
	if err != nil {
		return fmt.Errorf("list tarball: %w", err)
	}

	fileSet := make(map[string]bool, len(contents))
	for _, f := range contents {
		fileSet[f] = true
	}

	if !fileSet["package/.claude-plugin/plugin.json"] {
		return fmt.Errorf("tarball is missing .claude-plugin/plugin.json — plugin will have no metadata at runtime")
	}

	if hooks, ok := pluginJSON["hooks"]; ok && hooks != nil {
		hooksMap, ok := hooks.(map[string]interface{})
		if ok && len(hooksMap) > 0 {
			for _, matchers := range hooksMap {
				matchersList, ok := matchers.([]interface{})
				if !ok {
					continue
				}
				for _, m := range matchersList {
					matcher, ok := m.(map[string]interface{})
					if !ok {
						continue
					}
					hooksList, ok := matcher["hooks"].([]interface{})
					if !ok {
						continue
					}
					for _, h := range hooksList {
						hook, ok := h.(map[string]interface{})
						if !ok {
							continue
						}
						cmd, _ := hook["command"].(string)
						if cmd == "" {
							continue
						}
						const prefix = "${CLAUDE_PLUGIN_ROOT}/"
						if !strings.HasPrefix(cmd, prefix) {
							continue
						}
						rel := cmd[len(prefix):]
						if sp := strings.IndexByte(rel, ' '); sp != -1 {
							rel = rel[:sp]
						}
						tarPath := "package/" + rel
						if !fileSet[tarPath] {
							return fmt.Errorf("tarball is missing hook binary %q (from command: %s)", rel, cmd)
						}
					}
				}
			}
		}
	}

	return nil
}

var tarballContents = tarballContentsReal

func tarballContentsReal(path string) ([]string, error) {
	cmd := exec.Command("tar", "-tzf", path)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tar -tzf %s: %w", path, err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSuffix(line, "/")
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
