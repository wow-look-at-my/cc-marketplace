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
)

var platformBinaryPattern = regexp.MustCompile(`^(.+)_(linux|darwin)_(amd64|arm64)$`)

type platformBinary struct {
	goOS, goArch string
	name         string
	path         string
}

// detectPlatformBinaries finds the per-platform binaries
// (build/<name>_<goos>_<goarch>) in a cooked plugin directory -- the
// cross-compiled outputs go-toolchain emits.
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

// npmWrapper is the launcher placed at build/<name> in a bundled plugin package.
// The real binaries ship beside it as build/<name>_<goos>_<goarch> (go-toolchain's
// naming), so the launcher just execs the sibling matching the host. The whole
// package is self-contained -- no npm optionalDependencies and nothing to resolve
// from the registry -- so it can never be left half-published (the failure mode
// that made every binary plugin a no-op). If no binary matches the host (e.g. an
// unsupported OS/arch) it exits 0 so a PreToolUse hook never blocks.
const npmWrapper = `#!/bin/sh
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_NAME="$(basename "$0")"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64 | amd64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
esac
BINARY="$SCRIPT_DIR/${BIN_NAME}_${OS}_${ARCH}"
if [ ! -f "$BINARY" ]; then
  exit 0
fi
exec "$BINARY" "$@"
`

// writeWrappers writes the launcher at build/<name> for each binary name.
func writeWrappers(buildDir string, names map[string]bool) error {
	if len(names) == 0 {
		return nil
	}
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}
	for name := range names {
		if err := os.WriteFile(filepath.Join(buildDir, name), []byte(npmWrapper), 0755); err != nil {
			return err
		}
	}
	return nil
}

// buildMainPackageDir stages the single self-contained npm package for a plugin
// that ships per-platform binaries. Every build/<name>_<goos>_<goarch> binary is
// kept in the package and a launcher wrapper is written at build/<name>; there are
// no separate platform packages and no optionalDependencies, so the published
// tarball always carries the binary it points at.
func buildMainPackageDir(srcDir string, bins []platformBinary) (string, error) {
	mainDir, err := os.MkdirTemp("", "npm-main-*")
	if err != nil {
		return "", err
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
		// Skip any pre-existing unsuffixed build/<name> stub; the launcher wrapper
		// is written there below. The per-platform binaries are copied through
		// unchanged so they ship inside the package.
		if filepath.Dir(relPath) == "build" && wrapperNames[filepath.Base(relPath)] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		return os.WriteFile(dstPath, data, info.Mode())
	})
	if err != nil {
		os.RemoveAll(mainDir)
		return "", err
	}

	if err := writeWrappers(filepath.Join(mainDir, "build"), wrapperNames); err != nil {
		os.RemoveAll(mainDir)
		return "", err
	}

	return mainDir, nil
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
// alongside the tarball it references.
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
			fmt.Fprintf(os.Stderr, "Warning: skipping %s (manifest missing name/version/main)\n", e.Name())
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
