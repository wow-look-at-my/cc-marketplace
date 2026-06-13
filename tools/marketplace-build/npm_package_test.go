package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectPlatformBinaries(t *testing.T) {
	dir := t.TempDir()
	buildDir := filepath.Join(dir, "build")
	require.NoError(t, os.MkdirAll(buildDir, 0755))

	os.WriteFile(filepath.Join(buildDir, "hook"), []byte("#!/bin/sh"), 0755)
	os.WriteFile(filepath.Join(buildDir, "hook_linux_amd64"), []byte("elf"), 0755)
	os.WriteFile(filepath.Join(buildDir, "hook_darwin_arm64"), []byte("mach-o"), 0755)
	os.WriteFile(filepath.Join(buildDir, "readme.txt"), []byte("nope"), 0644)

	bins := detectPlatformBinaries(dir)
	require.Len(t, bins, 2)

	names := map[string]bool{}
	for _, b := range bins {
		names[b.goOS+"_"+b.goArch] = true
		require.Equal(t, "hook", b.name)
	}
	require.True(t, names["linux_amd64"])
	require.True(t, names["darwin_arm64"])
}

func TestDetectPlatformBinaries_NoBuildDir(t *testing.T) {
	bins := detectPlatformBinaries(t.TempDir())
	require.Nil(t, bins)
}

// TestNpmWrapper proves the launcher execs a sibling binary bundled in the same
// package (build/<name>_<os>_<arch>) and does NOT reach into node_modules for a
// separate platform package -- the change that un-zombies binary plugins.
func TestNpmWrapper(t *testing.T) {
	require.Contains(t, npmWrapper, "#!/bin/sh")
	require.Contains(t, npmWrapper, `BINARY="$SCRIPT_DIR/${BIN_NAME}_${OS}_${ARCH}"`)
	require.Contains(t, npmWrapper, `exec "$BINARY" "$@"`)
	require.NotContains(t, npmWrapper, "node_modules")
	require.NotContains(t, npmWrapper, "PLUGIN_ROOT")
}

func TestBuildMainPackageDir(t *testing.T) {
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "build"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755))

	// No unsuffixed build/hook stub: buildMainPackageDir must synthesize the
	// wrapper from the per-platform binaries alone.
	os.WriteFile(filepath.Join(srcDir, "build", "hook_linux_amd64"), []byte("elf binary"), 0755)
	os.WriteFile(filepath.Join(srcDir, "build", "hook_darwin_arm64"), []byte("mach-o binary"), 0755)
	os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("readme"), 0644)
	pkgJSON := `{"name":"owner-test","version":"1.0.0"}`
	os.WriteFile(filepath.Join(srcDir, "package.json"), []byte(pkgJSON), 0644)

	bins := detectPlatformBinaries(srcDir)
	require.Len(t, bins, 2)

	mainDir, err := buildMainPackageDir(srcDir, bins)
	require.NoError(t, err)
	defer os.RemoveAll(mainDir)

	// The per-platform binaries are bundled in the package, unchanged.
	linux, err := os.ReadFile(filepath.Join(mainDir, "build", "hook_linux_amd64"))
	require.NoError(t, err)
	require.Equal(t, "elf binary", string(linux))
	darwin, err := os.ReadFile(filepath.Join(mainDir, "build", "hook_darwin_arm64"))
	require.NoError(t, err)
	require.Equal(t, "mach-o binary", string(darwin))

	// The launcher wrapper execs a sibling binary, not a node_modules package.
	wrapper, err := os.ReadFile(filepath.Join(mainDir, "build", "hook"))
	require.NoError(t, err)
	require.Contains(t, string(wrapper), `$SCRIPT_DIR/${BIN_NAME}_${OS}_${ARCH}`)
	require.NotContains(t, string(wrapper), "node_modules")

	data, err := os.ReadFile(filepath.Join(mainDir, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "readme", string(data))

	// package.json is copied through verbatim -- no optionalDependencies injected.
	pkgData, err := os.ReadFile(filepath.Join(mainDir, "package.json"))
	require.NoError(t, err)
	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(pkgData, &pkg))
	require.Equal(t, "owner-test", pkg["name"])
	require.Equal(t, "1.0.0", pkg["version"])
	_, hasOptDeps := pkg["optionalDependencies"]
	require.False(t, hasOptDeps, "bundled package must not declare optionalDependencies")
}

func TestBuildMainPackageDir_OverwritesStalePlaceholder(t *testing.T) {
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "build"), 0755))
	os.WriteFile(filepath.Join(srcDir, "build", "hook"), []byte("stale placeholder"), 0755)
	os.WriteFile(filepath.Join(srcDir, "build", "hook_linux_amd64"), []byte("elf"), 0755)

	bins := detectPlatformBinaries(srcDir)
	mainDir, err := buildMainPackageDir(srcDir, bins)
	require.NoError(t, err)
	defer os.RemoveAll(mainDir)

	wrapper, err := os.ReadFile(filepath.Join(mainDir, "build", "hook"))
	require.NoError(t, err)
	require.Contains(t, string(wrapper), `$SCRIPT_DIR/${BIN_NAME}_${OS}_${ARCH}`)
	require.NotContains(t, string(wrapper), "stale placeholder")

	// The real binary still ships alongside the wrapper.
	bin, err := os.ReadFile(filepath.Join(mainDir, "build", "hook_linux_amd64"))
	require.NoError(t, err)
	require.Equal(t, "elf", string(bin))
}

func TestBuildMainPackageDir_MultipleBinaries(t *testing.T) {
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "build"), 0755))
	os.WriteFile(filepath.Join(srcDir, "build", "hook_linux_amd64"), []byte("elf"), 0755)
	os.WriteFile(filepath.Join(srcDir, "build", "validate_linux_amd64"), []byte("elf"), 0755)

	bins := detectPlatformBinaries(srcDir)
	mainDir, err := buildMainPackageDir(srcDir, bins)
	require.NoError(t, err)
	defer os.RemoveAll(mainDir)

	for _, name := range []string{"hook", "validate"} {
		wrapper, err := os.ReadFile(filepath.Join(mainDir, "build", name))
		require.NoError(t, err)
		require.Contains(t, string(wrapper), `$SCRIPT_DIR/${BIN_NAME}_${OS}_${ARCH}`)
		bin, err := os.ReadFile(filepath.Join(mainDir, "build", name+"_linux_amd64"))
		require.NoError(t, err)
		require.Equal(t, "elf", string(bin))
	}
}

func TestWriteWrappers_NoBinaries(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeWrappers(filepath.Join(dir, "build"), nil))

	_, err := os.Stat(filepath.Join(dir, "build"))
	require.True(t, os.IsNotExist(err), "no build/ should be created when there are no binaries")
}

func TestWriteWrappers_MkdirFails(t *testing.T) {
	dir := t.TempDir()
	// Place a regular file where MkdirAll wants to create a directory.
	blocker := filepath.Join(dir, "build")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0644))

	err := writeWrappers(filepath.Join(blocker, "nested"), map[string]bool{"hook": true})
	require.Error(t, err)
}

func TestWriteWrappers_WriteFails(t *testing.T) {
	dir := t.TempDir()
	buildDir := filepath.Join(dir, "build")
	require.NoError(t, os.MkdirAll(buildDir, 0755))
	// A directory at build/hook will make WriteFile fail.
	require.NoError(t, os.MkdirAll(filepath.Join(buildDir, "hook"), 0755))

	err := writeWrappers(buildDir, map[string]bool{"hook": true})
	require.Error(t, err)
}

func TestCreateTarball(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("world"), 0644)

	outPath := filepath.Join(t.TempDir(), "out.tgz")
	require.NoError(t, createTarball(srcDir, outPath))

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))

	// Entries are rooted under package/ so npm unpacks them correctly.
	files := listTarballContents(t, outPath)
	require.Contains(t, files, "package/file.txt")
}

func TestCreateTarball_BadSrcDir(t *testing.T) {
	err := createTarball("/nonexistent", filepath.Join(t.TempDir(), "out.tgz"))
	require.Error(t, err)
}

func TestReadPackagedPlugins_SkipsBadAndMissing(t *testing.T) {
	dir := t.TempDir()

	// Valid packaged plugin.
	good := filepath.Join(dir, "good")
	require.NoError(t, os.MkdirAll(good, 0755))
	m := pluginPackageManifest{Name: "owner-good", Version: "1.0.0", Main: manifestTarball{Tarball: "tarballs/owner-good/owner-good-1.0.0.tgz"}}
	data, _ := json.Marshal(m)
	require.NoError(t, os.WriteFile(filepath.Join(good, "manifest.json"), data, 0644))

	// No manifest.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "empty"), 0755))
	// Bad manifest.
	bad := filepath.Join(dir, "bad")
	require.NoError(t, os.MkdirAll(bad, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bad, "manifest.json"), []byte("{not json"), 0644))

	plugins, err := readPackagedPlugins(dir)
	require.NoError(t, err)
	require.Len(t, plugins, 1)
	require.Equal(t, "good", plugins[0].name)
	require.Equal(t, "owner-good", plugins[0].manifest.Name)
}

func TestReadPackagedPlugins_BadInputDir(t *testing.T) {
	_, err := readPackagedPlugins("/nonexistent/input/dir")
	require.Error(t, err)
}
