package main

import (
	"encoding/json"
	"fmt"
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

var buildNPMRegistryCmd = &cobra.Command{
	Use:   "build-npm-registry",
	Short: "Generate static npm registry files for GitHub Pages deployment",
	RunE:  runBuildNPMRegistry,
}

func init() {
	rootCmd.AddCommand(buildNPMRegistryCmd)
}

var extractTagContents = extractTagContentsReal

func extractTagContentsReal(tag, destDir string) error {
	archiveCmd := exec.Command("git", "archive", "--format=tar", tag)
	archiveCmd.Dir = getRepoRoot()

	stdout, err := archiveCmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := archiveCmd.Start(); err != nil {
		return fmt.Errorf("git archive: %w", err)
	}

	tarCmd := exec.Command("tar", "-x", "-C", destDir)
	tarCmd.Stdin = stdout
	if err := tarCmd.Run(); err != nil {
		archiveCmd.Wait()
		return fmt.Errorf("tar extract: %w", err)
	}
	return archiveCmd.Wait()
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
	data, _ := json.MarshalIndent(pkg, "", "  ")
	os.WriteFile(filepath.Join(mainDir, "package.json"), data, 0644)

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
	data, _ := json.MarshalIndent(pkg, "", "  ")
	os.WriteFile(filepath.Join(platDir, "package.json"), data, 0644)

	return platDir, nil
}

var createTarball = createTarballReal

func createTarballReal(srcDir, outputPath string) error {
	cmd := exec.Command("tar", "-czf", outputPath, "-C", srcDir, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
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
		platformVersions := make(map[string]map[string]interface{})

		for _, tag := range allTags {
			version := semverFromTag(tag)

			extractDir, err := os.MkdirTemp("", "tag-extract-*")
			if err != nil {
				continue
			}
			if err := extractTagContents(tag, extractDir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to extract %s: %v\n", tag, err)
				os.RemoveAll(extractDir)
				continue
			}

			platforms := detectPlatformBinaries(extractDir)

			tarballName := fmt.Sprintf("%s-%s.tgz", pkgName, version)
			tarballPath := filepath.Join(tarballDir, tarballName)
			tarballURL := fmt.Sprintf("%s/tarballs/%s/%s", pagesBase, pkgName, tarballName)

			if len(platforms) == 0 {
				if err := createTarball(extractDir, tarballPath); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to tar %s: %v\n", tag, err)
					os.RemoveAll(extractDir)
					continue
				}
				versions[version] = map[string]interface{}{
					"name":    pkgName,
					"version": version,
					"dist":    map[string]interface{}{"tarball": tarballURL},
				}
			} else {
				mainDir, err := buildMainPackageDir(extractDir, platforms, pkgName, version)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to build main pkg for %s: %v\n", tag, err)
					os.RemoveAll(extractDir)
					continue
				}
				if err := createTarball(mainDir, tarballPath); err != nil {
					os.RemoveAll(mainDir)
					os.RemoveAll(extractDir)
					continue
				}
				os.RemoveAll(mainDir)

				optDeps := map[string]string{}
				for _, pk := range uniquePlatforms(platforms) {
					optDeps[pkgName+"-"+npmPlatformName(pk.os, pk.arch)] = version
				}
				versions[version] = map[string]interface{}{
					"name":                 pkgName,
					"version":              version,
					"dist":                 map[string]interface{}{"tarball": tarballURL},
					"optionalDependencies": optDeps,
				}

				for _, pk := range uniquePlatforms(platforms) {
					platPkgName := pkgName + "-" + npmPlatformName(pk.os, pk.arch)
					platDir, err := buildPlatformPackageDir(platforms, pk.os, pk.arch, platPkgName, version)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to build platform pkg %s: %v\n", platPkgName, err)
						continue
					}

					platTarballDir := filepath.Join(tmpDir, "tarballs", platPkgName)
					os.MkdirAll(platTarballDir, 0755)
					platTarballName := fmt.Sprintf("%s-%s.tgz", platPkgName, version)
					platTarballPath := filepath.Join(platTarballDir, platTarballName)
					platTarballURL := fmt.Sprintf("%s/tarballs/%s/%s", pagesBase, platPkgName, platTarballName)

					if err := createTarball(platDir, platTarballPath); err != nil {
						os.RemoveAll(platDir)
						continue
					}
					os.RemoveAll(platDir)

					if platformVersions[platPkgName] == nil {
						platformVersions[platPkgName] = make(map[string]interface{})
					}
					platformVersions[platPkgName][version] = map[string]interface{}{
						"name":    platPkgName,
						"version": version,
						"os":      []string{pk.os},
						"cpu":     []string{npmArch[pk.arch]},
						"dist":    map[string]interface{}{"tarball": platTarballURL},
					}
				}
			}

			os.RemoveAll(extractDir)
		}

		if len(versions) == 0 {
			continue
		}

		writePackument(tmpDir, pkgName, latestVersion, versions)

		for platPkgName, platVers := range platformVersions {
			writePackument(tmpDir, platPkgName, latestVersion, platVers)
		}

		fmt.Fprintf(os.Stderr, "  %s: %d version(s)", pkgName, len(versions))
		if len(platformVersions) > 0 {
			fmt.Fprintf(os.Stderr, " + %d platform packages", len(platformVersions))
		}
		fmt.Fprintln(os.Stderr)
	}

	fmt.Printf("registry_dir=%s\n", tmpDir)
	fmt.Fprintf(os.Stderr, "Built npm registry in %s\n", tmpDir)
	return nil
}

func writePackument(registryDir, pkgName, latestVersion string, versions map[string]interface{}) {
	packument := map[string]interface{}{
		"name": pkgName,
		"dist-tags": map[string]interface{}{
			"latest": latestVersion,
		},
		"versions": versions,
	}
	data, _ := json.MarshalIndent(packument, "", "  ")
	os.WriteFile(filepath.Join(registryDir, pkgName), data, 0644)
}
