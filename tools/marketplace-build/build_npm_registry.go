package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var npmArch = map[string]string{"amd64": "x64", "arm64": "arm64"}

var platformBinaryPattern = regexp.MustCompile(`^(.+)_(linux|darwin)_(amd64|arm64)$`)

type platformBinary struct {
	goOS, goArch string
	name         string
	path         string
}

type platformKey struct{ os, arch string }

var buildNPMRegistryInput string
var buildNPMBaseURL string

var buildNPMRegistryCmd = &cobra.Command{
	Use:   "build-npm-registry",
	Short: "Generate static npm registry files for GitHub Pages deployment",
	RunE:  runBuildNPMRegistry,
}

func init() {
	buildNPMRegistryCmd.Flags().StringVar(&buildNPMRegistryInput, "input", "", "directory of packaged plugin subdirectories (one per plugin, each containing manifest.json + tarballs/)")
	buildNPMRegistryCmd.Flags().StringVar(&buildNPMBaseURL, "base-url", "", "Base URL for tarball downloads (defaults to GitHub Pages URL)")
	rootCmd.AddCommand(buildNPMRegistryCmd)
}

func detectPlatformBinaries(dir string) []platformBinary {
	buildDir := filepath.Join(dir, "build")
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return nil
	}

	var result []platformBinary
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := platformBinaryPattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		result = append(result, platformBinary{
			name:   m[1],
			goOS:   m[2],
			goArch: m[3],
			path:   filepath.Join(buildDir, e.Name()),
		})
	}
	return result
}

func uniquePlatforms(bins []platformBinary) []platformKey {
	seen := map[platformKey]bool{}
	var result []platformKey
	for _, b := range bins {
		k := platformKey{b.goOS, b.goArch}
		if !seen[k] {
			seen[k] = true
			result = append(result, k)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].os != result[j].os {
			return result[i].os < result[j].os
		}
		return result[i].arch < result[j].arch
	})
	return result
}

func npmPlatformName(goOS, goArch string) string {
	arch := npmArch[goArch]
	if arch == "" {
		arch = goArch
	}
	return goOS + "-" + arch
}

const npmWrapperTemplate = `#!/bin/sh
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_ROOT="$(dirname "$SCRIPT_DIR")"
BIN_NAME="$(basename "$0")"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH="x64" ;;
  aarch64) ARCH="arm64" ;;
esac
BINARY="$PLUGIN_ROOT/node_modules/PKG_PLACEHOLDER-${OS}-${ARCH}/bin/${BIN_NAME}"
if [ ! -f "$BINARY" ]; then
  exit 0
fi
exec "$BINARY" "$@"
`

func npmWrapperScript(pkgName string) string {
	return strings.Replace(npmWrapperTemplate, "PKG_PLACEHOLDER", pkgName, 1)
}

func buildMainPackageDir(srcDir string, bins []platformBinary, pkgName, version string) (string, error) {
	mainDir, err := os.MkdirTemp("", "npm-main-*")
	if err != nil {
		return "", err
	}

	binPaths := map[string]bool{}
	for _, b := range bins {
		binPaths[b.path] = true
	}

	wrapperNames := map[string]bool{}
	for _, b := range bins {
		wrapperNames[b.name] = true
	}

	err = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(srcDir, path)
		dstPath := filepath.Join(mainDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}
		if binPaths[path] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		mode := info.Mode()

		if filepath.Dir(relPath) == "build" && wrapperNames[filepath.Base(relPath)] {
			data = []byte(npmWrapperScript(pkgName))
			mode = 0755
		}

		return os.WriteFile(dstPath, data, mode)
	})
	if err != nil {
		os.RemoveAll(mainDir)
		return "", err
	}

	platforms := uniquePlatforms(bins)
	optDeps := map[string]string{}
	for _, p := range platforms {
		optDeps[pkgName+"-"+npmPlatformName(p.os, p.arch)] = version
	}
	pkg := map[string]interface{}{
		"name":                 pkgName,
		"version":              version,
		"optionalDependencies": optDeps,
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		os.RemoveAll(mainDir)
		return "", fmt.Errorf("marshal package.json for %s: %w", pkgName, err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "package.json"), data, 0644); err != nil {
		os.RemoveAll(mainDir)
		return "", fmt.Errorf("write package.json for %s: %w", pkgName, err)
	}

	return mainDir, nil
}

func buildPlatformPackageDir(bins []platformBinary, goOS, goArch, platPkgName, version string) (string, error) {
	platDir, err := os.MkdirTemp("", "npm-plat-*")
	if err != nil {
		return "", err
	}

	binDir := filepath.Join(platDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		os.RemoveAll(platDir)
		return "", err
	}

	for _, b := range bins {
		if b.goOS != goOS || b.goArch != goArch {
			continue
		}
		data, err := os.ReadFile(b.path)
		if err != nil {
			os.RemoveAll(platDir)
			return "", err
		}
		if err := os.WriteFile(filepath.Join(binDir, b.name), data, 0755); err != nil {
			os.RemoveAll(platDir)
			return "", err
		}
	}

	arch := npmArch[goArch]
	if arch == "" {
		arch = goArch
	}
	pkg := map[string]interface{}{
		"name":    platPkgName,
		"version": version,
		"os":      []string{goOS},
		"cpu":     []string{arch},
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		os.RemoveAll(platDir)
		return "", fmt.Errorf("marshal package.json for %s: %w", platPkgName, err)
	}
	if err := os.WriteFile(filepath.Join(platDir, "package.json"), data, 0644); err != nil {
		os.RemoveAll(platDir)
		return "", fmt.Errorf("write package.json for %s: %w", platPkgName, err)
	}

	return platDir, nil
}

var createTarball = createTarballReal

func createTarballReal(srcDir, outputPath string) error {
	cmd := exec.Command("bash", "-c",
		`set -eo pipefail; tar -cf - --transform 's,^\./,package/,' -C "$1" . | gzip -9 > "$2"`,
		"--", srcDir, outputPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}

// readPackagedPlugins enumerates subdirectories of inputDir as packaged plugins.
// Each subdirectory must contain a manifest.json (output of `package-plugin`)
// alongside the tarballs/ tree it references.
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
			fmt.Fprintf(os.Stderr, "Warning: skipping %s (no manifest.json): %v\n", e.Name(), err)
			continue
		}
		var m pluginPackageManifest
		if err := json.Unmarshal(data, &m); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s (bad manifest.json): %v\n", e.Name(), err)
			continue
		}
		if m.Name == "" || m.Version == "" || m.Main.Tarball == "" {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s (manifest missing name/version/main): %v\n", e.Name(), err)
			continue
		}
		plugins = append(plugins, packagedPlugin{name: e.Name(), dir: dir, manifest: m})
	}

	sort.Slice(plugins, func(i, j int) bool { return plugins[i].name < plugins[j].name })
	return plugins, nil
}

type packagedPlugin struct {
	name     string
	dir      string
	manifest pluginPackageManifest
}

func copyTarball(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func runBuildNPMRegistry(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	if buildNPMRegistryInput == "" {
		return fmt.Errorf("--input is required: directory of packaged plugin subdirectories")
	}

	owner, repo, err := GetRepoInfo()
	if err != nil {
		return err
	}

	pagesBase := buildNPMBaseURL
	if pagesBase == "" {
		pagesBase = fmt.Sprintf("https://%s.github.io/%s", owner, repo)
	}

	plugins, err := readPackagedPlugins(buildNPMRegistryInput)
	if err != nil {
		return fmt.Errorf("failed to read packaged plugins: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "npm-registry-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	for _, plugin := range plugins {
		m := plugin.manifest
		pkgName := m.Name
		version := m.Version

		mainSrc := filepath.Join(plugin.dir, filepath.FromSlash(m.Main.Tarball))
		mainDst := filepath.Join(tmpDir, filepath.FromSlash(m.Main.Tarball))
		if err := copyTarball(mainSrc, mainDst); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to copy main tarball for %s: %v\n", pkgName, err)
			continue
		}

		mainEntry := map[string]interface{}{
			"name":    pkgName,
			"version": version,
			"dist":    map[string]interface{}{"tarball": fmt.Sprintf("%s/%s", pagesBase, m.Main.Tarball)},
		}
		if len(m.Platforms) > 0 {
			optDeps := map[string]string{}
			for _, p := range m.Platforms {
				optDeps[p.Name] = version
			}
			mainEntry["optionalDependencies"] = optDeps
		}

		if err := writePackument(tmpDir, pkgName, version, map[string]interface{}{version: mainEntry}); err != nil {
			return err
		}

		for _, p := range m.Platforms {
			platSrc := filepath.Join(plugin.dir, filepath.FromSlash(p.Tarball))
			platDst := filepath.Join(tmpDir, filepath.FromSlash(p.Tarball))
			if err := copyTarball(platSrc, platDst); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to copy platform tarball %s: %v\n", p.Name, err)
				continue
			}
			platEntry := map[string]interface{}{
				"name":    p.Name,
				"version": version,
				"os":      []string{p.OS},
				"cpu":     []string{p.CPU},
				"dist":    map[string]interface{}{"tarball": fmt.Sprintf("%s/%s", pagesBase, p.Tarball)},
			}
			if err := writePackument(tmpDir, p.Name, version, map[string]interface{}{version: platEntry}); err != nil {
				return err
			}
		}

		fmt.Fprintf(os.Stderr, "  %s: %s", pkgName, version)
		if len(m.Platforms) > 0 {
			fmt.Fprintf(os.Stderr, " + %d platform packages", len(m.Platforms))
		}
		fmt.Fprintln(os.Stderr)
	}

	fmt.Printf("registry_dir=%s\n", tmpDir)
	fmt.Fprintf(os.Stderr, "Built npm registry in %s\n", tmpDir)
	return nil
}

func writePackument(registryDir, pkgName, latestVersion string, versions map[string]interface{}) error {
	packument := map[string]interface{}{
		"name": pkgName,
		"dist-tags": map[string]interface{}{
			"latest": latestVersion,
		},
		"versions": versions,
	}
	data, err := json.MarshalIndent(packument, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal packument for %s: %w", pkgName, err)
	}
	if err := os.WriteFile(filepath.Join(registryDir, pkgName), data, 0644); err != nil {
		return fmt.Errorf("write packument for %s: %w", pkgName, err)
	}
	return nil
}
