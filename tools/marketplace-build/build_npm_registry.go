package main

import (
	"crypto/sha256"
	"encoding/hex"
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

// cookedPlugin represents a single plugin's cooked artifact directory
// produced by `marketplace-build release-plugin`.
type cookedPlugin struct {
	name    string
	dir     string
	version string
}

// readCookedPlugins enumerates subdirectories of inputDir as cooked plugins.
// Each subdirectory is expected to contain a package.json (with a version
// field) and the cooked plugin tree.
func readCookedPlugins(inputDir string) ([]cookedPlugin, error) {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return nil, fmt.Errorf("read input dir %s: %w", inputDir, err)
	}

	var plugins []cookedPlugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pluginDir := filepath.Join(inputDir, e.Name())
		pkgPath := filepath.Join(pluginDir, "package.json")
		data, err := os.ReadFile(pkgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s (no package.json): %v\n", e.Name(), err)
			continue
		}
		var pkg struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s (bad package.json): %v\n", e.Name(), err)
			continue
		}
		if pkg.Version == "" {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s (no version in package.json)\n", e.Name())
			continue
		}
		plugins = append(plugins, cookedPlugin{
			name:    e.Name(),
			dir:     pluginDir,
			version: pkg.Version,
		})
	}

	sort.Slice(plugins, func(i, j int) bool { return plugins[i].name < plugins[j].name })
	return plugins, nil
}

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
	buildNPMRegistryCmd.Flags().StringVar(&buildNPMRegistryInput, "input", "", "directory of cooked plugin subdirectories (one per plugin)")
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
	cmd := exec.Command("tar", "-czf", outputPath, "--transform", `s,^\./,package/,`, "-C", srcDir, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}

var hashFile = hashFileReal

// hashFileReal returns the lowercase hex-encoded SHA-256 digest of the file at path.
func hashFileReal(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for hashing: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func runBuildNPMRegistry(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	if buildNPMRegistryInput == "" {
		return fmt.Errorf("--input is required: directory of cooked plugin subdirectories")
	}

	owner, repo, err := GetRepoInfo()
	if err != nil {
		return err
	}

	pagesBase := buildNPMBaseURL
	if pagesBase == "" {
		pagesBase = fmt.Sprintf("https://%s.github.io/%s", owner, repo)
	}

	cookedPlugins, err := readCookedPlugins(buildNPMRegistryInput)
	if err != nil {
		return fmt.Errorf("failed to read cooked plugins: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "npm-registry-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	tarballDir := filepath.Join(tmpDir, "tarballs")
	if err := os.MkdirAll(tarballDir, 0755); err != nil {
		return fmt.Errorf("failed to create tarball dir: %w", err)
	}

	for _, plugin := range cookedPlugins {
		pluginName := plugin.name
		pkgName := fmt.Sprintf("%s-%s", owner, pluginName)
		version := plugin.version

		versions := make(map[string]interface{})
		platformVersions := make(map[string]map[string]interface{})

		platforms := detectPlatformBinaries(plugin.dir)

		// Create tarball to a temp file, hash it, then rename to content-addressed name.
		tmpTarball := filepath.Join(tarballDir, fmt.Sprintf("tmp-%s.tgz", pkgName))

		if len(platforms) == 0 {
			if err := createTarball(plugin.dir, tmpTarball); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to tar %s: %v\n", pluginName, err)
				continue
			}
			hash, err := hashFile(tmpTarball)
			if err != nil {
				os.Remove(tmpTarball)
				fmt.Fprintf(os.Stderr, "Warning: failed to hash tarball for %s: %v\n", pluginName, err)
				continue
			}
			tarballName := hash + ".tgz"
			tarballPath := filepath.Join(tarballDir, tarballName)
			if err := os.Rename(tmpTarball, tarballPath); err != nil {
				os.Remove(tmpTarball)
				return fmt.Errorf("rename tarball for %s: %w", pkgName, err)
			}
			tarballURL := fmt.Sprintf("%s/tarballs/%s", pagesBase, tarballName)
			versions[version] = map[string]interface{}{
				"name":    pkgName,
				"version": version,
				"dist": map[string]interface{}{
					"tarball": tarballURL,
					"shasum":  hash,
				},
			}
		} else {
			mainDir, err := buildMainPackageDir(plugin.dir, platforms, pkgName, version)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to build main pkg for %s: %v\n", pluginName, err)
				continue
			}
			if err := createTarball(mainDir, tmpTarball); err != nil {
				os.RemoveAll(mainDir)
				continue
			}
			os.RemoveAll(mainDir)

			hash, err := hashFile(tmpTarball)
			if err != nil {
				os.Remove(tmpTarball)
				fmt.Fprintf(os.Stderr, "Warning: failed to hash tarball for %s: %v\n", pluginName, err)
				continue
			}
			tarballName := hash + ".tgz"
			tarballPath := filepath.Join(tarballDir, tarballName)
			if err := os.Rename(tmpTarball, tarballPath); err != nil {
				os.Remove(tmpTarball)
				return fmt.Errorf("rename tarball for %s: %w", pkgName, err)
			}
			tarballURL := fmt.Sprintf("%s/tarballs/%s", pagesBase, tarballName)

			optDeps := map[string]string{}
			for _, pk := range uniquePlatforms(platforms) {
				optDeps[pkgName+"-"+npmPlatformName(pk.os, pk.arch)] = version
			}
			versions[version] = map[string]interface{}{
				"name":                 pkgName,
				"version":              version,
				"dist": map[string]interface{}{
					"tarball": tarballURL,
					"shasum":  hash,
				},
				"optionalDependencies": optDeps,
			}

			for _, pk := range uniquePlatforms(platforms) {
				platPkgName := pkgName + "-" + npmPlatformName(pk.os, pk.arch)
				platDir, err := buildPlatformPackageDir(platforms, pk.os, pk.arch, platPkgName, version)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to build platform pkg %s: %v\n", platPkgName, err)
					continue
				}

				platTmpTarball := filepath.Join(tarballDir, fmt.Sprintf("tmp-%s.tgz", platPkgName))
				if err := createTarball(platDir, platTmpTarball); err != nil {
					os.RemoveAll(platDir)
					continue
				}
				os.RemoveAll(platDir)

				platHash, err := hashFile(platTmpTarball)
				if err != nil {
					os.Remove(platTmpTarball)
					fmt.Fprintf(os.Stderr, "Warning: failed to hash platform tarball for %s: %v\n", platPkgName, err)
					continue
				}
				platTarballName := platHash + ".tgz"
				platTarballPath := filepath.Join(tarballDir, platTarballName)
				if err := os.Rename(platTmpTarball, platTarballPath); err != nil {
					os.Remove(platTmpTarball)
					return fmt.Errorf("rename platform tarball for %s: %w", platPkgName, err)
				}
				platTarballURL := fmt.Sprintf("%s/tarballs/%s", pagesBase, platTarballName)

				if platformVersions[platPkgName] == nil {
					platformVersions[platPkgName] = make(map[string]interface{})
				}
				platformVersions[platPkgName][version] = map[string]interface{}{
					"name":    platPkgName,
					"version": version,
					"os":      []string{pk.os},
					"cpu":     []string{npmArch[pk.arch]},
					"dist": map[string]interface{}{
						"tarball": platTarballURL,
						"shasum":  platHash,
					},
				}
			}
		}

		if len(versions) == 0 {
			continue
		}

		if err := writePackument(tmpDir, pkgName, version, versions); err != nil {
			return err
		}

		for platPkgName, platVers := range platformVersions {
			if err := writePackument(tmpDir, platPkgName, version, platVers); err != nil {
				return err
			}
		}

		fmt.Fprintf(os.Stderr, "  %s: %s", pkgName, version)
		if len(platformVersions) > 0 {
			fmt.Fprintf(os.Stderr, " + %d platform packages", len(platformVersions))
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
