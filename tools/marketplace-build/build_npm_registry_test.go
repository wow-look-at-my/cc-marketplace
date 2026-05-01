package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

// hashTgzPattern matches content-addressed tarball filenames: 64 hex chars + .tgz
var hashTgzPattern = regexp.MustCompile(`^[0-9a-f]{64}\.tgz$`)

// makeCookedPluginWithFiles creates a fake cooked plugin directory at root/name
// with package.json (version) and arbitrary files.
func makeCookedPluginWithFiles(t *testing.T, root, name, version string, files map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0755))

	pkg := map[string]string{
		"name":    "test-owner-" + name,
		"version": version,
	}
	pkgData, _ := json.Marshal(pkg)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), pkgData, 0644))

	for relPath, data := range files {
		full := filepath.Join(dir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, data, 0755))
	}
	return dir
}

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

func TestUniquePlatforms(t *testing.T) {
	bins := []platformBinary{
		{goOS: "linux", goArch: "amd64", name: "hook"},
		{goOS: "linux", goArch: "amd64", name: "validate"},
		{goOS: "darwin", goArch: "arm64", name: "hook"},
	}
	platforms := uniquePlatforms(bins)
	require.Len(t, platforms, 2)
	require.Equal(t, platformKey{"darwin", "arm64"}, platforms[0])
	require.Equal(t, platformKey{"linux", "amd64"}, platforms[1])
}

func TestNpmPlatformName(t *testing.T) {
	require.Equal(t, "linux-x64", npmPlatformName("linux", "amd64"))
	require.Equal(t, "darwin-arm64", npmPlatformName("darwin", "arm64"))
	require.Equal(t, "linux-arm64", npmPlatformName("linux", "arm64"))
}

func TestNpmWrapperScript(t *testing.T) {
	script := npmWrapperScript("owner-myplugin")
	require.Contains(t, script, "owner-myplugin-${OS}-${ARCH}")
	require.Contains(t, script, "node_modules/")
	require.Contains(t, script, "#!/bin/sh")
	require.NotContains(t, script, "PKG_PLACEHOLDER")
}

func TestBuildMainPackageDir(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "build"), 0755)
	os.MkdirAll(filepath.Join(srcDir, ".claude-plugin"), 0755)

	os.WriteFile(filepath.Join(srcDir, "build", "hook"), []byte("#!/bin/sh\nold wrapper"), 0755)
	os.WriteFile(filepath.Join(srcDir, "build", "hook_linux_amd64"), []byte("elf binary"), 0755)
	os.WriteFile(filepath.Join(srcDir, "build", "hook_darwin_arm64"), []byte("mach-o binary"), 0755)
	os.WriteFile(filepath.Join(srcDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("readme"), 0644)

	bins := detectPlatformBinaries(srcDir)
	require.Len(t, bins, 2)

	mainDir, err := buildMainPackageDir(srcDir, bins, "owner-test", "1.0.0")
	require.NoError(t, err)
	defer os.RemoveAll(mainDir)

	_, err = os.Stat(filepath.Join(mainDir, "build", "hook_linux_amd64"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(mainDir, "build", "hook_darwin_arm64"))
	require.True(t, os.IsNotExist(err))

	wrapper, err := os.ReadFile(filepath.Join(mainDir, "build", "hook"))
	require.NoError(t, err)
	require.Contains(t, string(wrapper), "node_modules/owner-test-")
	require.NotContains(t, string(wrapper), "old wrapper")

	data, err := os.ReadFile(filepath.Join(mainDir, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "readme", string(data))

	pkgData, err := os.ReadFile(filepath.Join(mainDir, "package.json"))
	require.NoError(t, err)
	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(pkgData, &pkg))
	require.Equal(t, "owner-test", pkg["name"])
	require.Equal(t, "1.0.0", pkg["version"])
	optDeps := pkg["optionalDependencies"].(map[string]interface{})
	require.Equal(t, "1.0.0", optDeps["owner-test-linux-x64"])
	require.Equal(t, "1.0.0", optDeps["owner-test-darwin-arm64"])
}

func TestBuildPlatformPackageDir(t *testing.T) {
	srcDir := t.TempDir()
	buildDir := filepath.Join(srcDir, "build")
	os.MkdirAll(buildDir, 0755)

	os.WriteFile(filepath.Join(buildDir, "hook_linux_amd64"), []byte("elf binary"), 0755)
	os.WriteFile(filepath.Join(buildDir, "validate_linux_amd64"), []byte("elf validate"), 0755)
	os.WriteFile(filepath.Join(buildDir, "hook_darwin_arm64"), []byte("mach-o"), 0755)

	bins := detectPlatformBinaries(srcDir)

	platDir, err := buildPlatformPackageDir(bins, "linux", "amd64", "owner-test-linux-x64", "2.0.0")
	require.NoError(t, err)
	defer os.RemoveAll(platDir)

	hookData, err := os.ReadFile(filepath.Join(platDir, "bin", "hook"))
	require.NoError(t, err)
	require.Equal(t, "elf binary", string(hookData))

	valData, err := os.ReadFile(filepath.Join(platDir, "bin", "validate"))
	require.NoError(t, err)
	require.Equal(t, "elf validate", string(valData))

	_, err = os.Stat(filepath.Join(platDir, "bin", "hook_darwin_arm64"))
	require.True(t, os.IsNotExist(err))

	pkgData, err := os.ReadFile(filepath.Join(platDir, "package.json"))
	require.NoError(t, err)
	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(pkgData, &pkg))
	require.Equal(t, "owner-test-linux-x64", pkg["name"])
	require.Equal(t, "2.0.0", pkg["version"])
	require.Equal(t, []interface{}{"linux"}, pkg["os"])
	require.Equal(t, []interface{}{"x64"}, pkg["cpu"])
}

func runRegistryWithInput(t *testing.T, inputDir string) string {
	t.Helper()
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", fmt.Errorf("unexpected git call: %v", args)
	})

	origInput := buildNPMRegistryInput
	buildNPMRegistryInput = inputDir
	t.Cleanup(func() { buildNPMRegistryInput = origInput })

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	w.Close()
	os.Stdout = origStdout
	require.NoError(t, err)

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "registry_dir=") {
			return strings.TrimPrefix(line, "registry_dir=")
		}
	}
	t.Fatal("registry_dir not found in output")
	return ""
}

func TestRunBuildNPMRegistry_NoPlatformBinaries(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "jq", "1.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"jq"}`),
		"commands/jq.md":             []byte("jq command"),
	})
	registryDir := runRegistryWithInput(t, dir)

	packument, err := os.ReadFile(filepath.Join(registryDir, "test-owner-jq"))
	require.NoError(t, err)
	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(packument, &pkg))
	versions := pkg["versions"].(map[string]interface{})
	require.Contains(t, versions, "1.0.0")

	// Verify dist has shasum and content-addressed tarball URL.
	v := versions["1.0.0"].(map[string]interface{})
	dist := v["dist"].(map[string]interface{})
	shasum, ok := dist["shasum"].(string)
	require.True(t, ok, "dist must contain shasum")
	require.Regexp(t, `^[0-9a-f]{64}$`, shasum)

	tarballURL := dist["tarball"].(string)
	require.Contains(t, tarballURL, "/tarballs/"+shasum+".tgz")
	// URL must NOT contain the package name (flat pool).
	require.NotContains(t, tarballURL, "/tarballs/test-owner-jq/")
}

func TestRunBuildNPMRegistry_WithPlatformBinaries(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "hook", "3.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"hook"}`),
		"build/hook":                 []byte("#!/bin/sh\nwrapper"),
		"build/hook_linux_amd64":     []byte("elf"),
		"build/hook_linux_arm64":     []byte("elf arm"),
		"build/hook_darwin_amd64":    []byte("mach-o x64"),
		"build/hook_darwin_arm64":    []byte("mach-o arm"),
	})
	runRegistryWithInput(t, dir)
}

func TestRunBuildNPMRegistry_VerifyPlatformOutput(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "myplugin", "2.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"myplugin"}`),
		"build/hook":                 []byte("#!/bin/sh\nold"),
		"build/hook_linux_amd64":     []byte("elf64"),
		"build/hook_darwin_arm64":    []byte("macharm"),
	})
	registryDir := runRegistryWithInput(t, dir)

	// Verify main packument structure.
	mainPackument, err := os.ReadFile(filepath.Join(registryDir, "test-owner-myplugin"))
	require.NoError(t, err)
	var mainPkg map[string]interface{}
	require.NoError(t, json.Unmarshal(mainPackument, &mainPkg))
	require.Equal(t, "test-owner-myplugin", mainPkg["name"])
	v := mainPkg["versions"].(map[string]interface{})["2.0.0"].(map[string]interface{})
	optDeps := v["optionalDependencies"].(map[string]interface{})
	require.Equal(t, "2.0.0", optDeps["test-owner-myplugin-linux-x64"])
	require.Equal(t, "2.0.0", optDeps["test-owner-myplugin-darwin-arm64"])

	// Verify main package dist has shasum and content-addressed URL.
	mainDist := v["dist"].(map[string]interface{})
	mainShasum, ok := mainDist["shasum"].(string)
	require.True(t, ok, "main dist must contain shasum")
	require.Regexp(t, `^[0-9a-f]{64}$`, mainShasum)
	mainURL := mainDist["tarball"].(string)
	require.Contains(t, mainURL, "/tarballs/"+mainShasum+".tgz")
	require.NotContains(t, mainURL, "/tarballs/test-owner-myplugin/")

	// Verify linux platform packument.
	linuxPkg, err := os.ReadFile(filepath.Join(registryDir, "test-owner-myplugin-linux-x64"))
	require.NoError(t, err)
	var lp map[string]interface{}
	require.NoError(t, json.Unmarshal(linuxPkg, &lp))
	lv := lp["versions"].(map[string]interface{})["2.0.0"].(map[string]interface{})
	require.Equal(t, []interface{}{"linux"}, lv["os"])
	require.Equal(t, []interface{}{"x64"}, lv["cpu"])
	linuxDist := lv["dist"].(map[string]interface{})
	linuxShasum, ok := linuxDist["shasum"].(string)
	require.True(t, ok, "linux dist must contain shasum")
	require.Regexp(t, `^[0-9a-f]{64}$`, linuxShasum)

	// Verify darwin platform packument.
	darwinPkg, err := os.ReadFile(filepath.Join(registryDir, "test-owner-myplugin-darwin-arm64"))
	require.NoError(t, err)
	var dp map[string]interface{}
	require.NoError(t, json.Unmarshal(darwinPkg, &dp))
	dv := dp["versions"].(map[string]interface{})["2.0.0"].(map[string]interface{})
	require.Equal(t, []interface{}{"darwin"}, dv["os"])
	require.Equal(t, []interface{}{"arm64"}, dv["cpu"])
	darwinDist := dv["dist"].(map[string]interface{})
	darwinShasum, ok := darwinDist["shasum"].(string)
	require.True(t, ok, "darwin dist must contain shasum")
	require.Regexp(t, `^[0-9a-f]{64}$`, darwinShasum)

	// Verify tarballs are in flat directory (no per-package subdirs).
	tarballDir := filepath.Join(registryDir, "tarballs")
	entries, err := os.ReadDir(tarballDir)
	require.NoError(t, err)
	// Should have exactly 3 tarballs (main + 2 platforms), all hash-named.
	require.Len(t, entries, 3)
	for _, e := range entries {
		require.False(t, e.IsDir(), "tarball dir should be flat, found subdir: %s", e.Name())
		require.Regexp(t, hashTgzPattern, e.Name(), "tarball filename must be content-addressed")
	}

	// Each tarball referenced in packuments should exist on disk.
	for _, hash := range []string{mainShasum, linuxShasum, darwinShasum} {
		_, err := os.Stat(filepath.Join(tarballDir, hash+".tgz"))
		require.NoError(t, err, "tarball %s.tgz must exist", hash)
	}
}

func TestRunBuildNPMRegistry_TarballFails_SimplePkg(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "p", "1.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"p"}`),
	})

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", fmt.Errorf("unexpected: %v", args)
	})

	orig := createTarball
	t.Cleanup(func() { createTarball = orig })
	createTarball = func(_, _ string) error { return fmt.Errorf("tar failed") }

	origInput := buildNPMRegistryInput
	buildNPMRegistryInput = dir
	t.Cleanup(func() { buildNPMRegistryInput = origInput })

	require.NoError(t, runBuildNPMRegistry(buildNPMRegistryCmd, nil))
}

func TestRunBuildNPMRegistry_TarballFails_PlatformPkg(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "p", "1.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"p"}`),
		"build/hook":                 []byte("#!/bin/sh"),
		"build/hook_linux_amd64":     []byte("elf"),
	})

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", fmt.Errorf("unexpected: %v", args)
	})

	orig := createTarball
	t.Cleanup(func() { createTarball = orig })
	createTarball = func(_, _ string) error { return fmt.Errorf("tar failed") }

	origInput := buildNPMRegistryInput
	buildNPMRegistryInput = dir
	t.Cleanup(func() { buildNPMRegistryInput = origInput })

	require.NoError(t, runBuildNPMRegistry(buildNPMRegistryCmd, nil))
}

func TestRunBuildNPMRegistry_NoInputFlag(t *testing.T) {
	origInput := buildNPMRegistryInput
	buildNPMRegistryInput = ""
	t.Cleanup(func() { buildNPMRegistryInput = origInput })

	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "--input is required")
}

func TestBuildPlatformPackageDir_MissingBinary(t *testing.T) {
	bins := []platformBinary{
		{goOS: "linux", goArch: "amd64", name: "hook", path: "/nonexistent/hook_linux_amd64"},
	}
	_, err := buildPlatformPackageDir(bins, "linux", "amd64", "test-linux-x64", "1.0.0")
	require.Error(t, err)
}

func TestBuildMainPackageDir_ErrorPaths(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "build"), 0755)
	os.WriteFile(filepath.Join(srcDir, "build", "hook"), []byte("#!/bin/sh"), 0755)
	os.WriteFile(filepath.Join(srcDir, "build", "hook_linux_amd64"), []byte("bin"), 0755)

	bins := detectPlatformBinaries(srcDir)

	mainDir, err := buildMainPackageDir(srcDir, bins, "pkg", "1.0.0")
	require.NoError(t, err)
	defer os.RemoveAll(mainDir)

	data, _ := os.ReadFile(filepath.Join(mainDir, "build", "hook"))
	require.Contains(t, string(data), "pkg-${OS}-${ARCH}")
}

func TestCreateTarball(t *testing.T) {
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("world"), 0644)

	outPath := filepath.Join(t.TempDir(), "out.tgz")
	err := createTarball(srcDir, outPath)
	require.NoError(t, err)

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

func TestCreateTarball_BadSrcDir(t *testing.T) {
	err := createTarball("/nonexistent", filepath.Join(t.TempDir(), "out.tgz"))
	require.Error(t, err)
}

func TestNpmPlatformName_UnknownArch(t *testing.T) {
	require.Equal(t, "linux-riscv64", npmPlatformName("linux", "riscv64"))
}

func TestWritePackument(t *testing.T) {
	dir := t.TempDir()
	versions := map[string]interface{}{
		"1.0.0": map[string]interface{}{
			"name":    "test-pkg",
			"version": "1.0.0",
		},
	}
	require.NoError(t, writePackument(dir, "test-pkg", "1.0.0", versions))

	data, err := os.ReadFile(filepath.Join(dir, "test-pkg"))
	require.NoError(t, err)

	var packument map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &packument))
	require.Equal(t, "test-pkg", packument["name"])

	distTags := packument["dist-tags"].(map[string]interface{})
	require.Equal(t, "1.0.0", distTags["latest"])
}

func TestWritePackument_BadDir(t *testing.T) {
	versions := map[string]interface{}{
		"1.0.0": map[string]interface{}{"name": "test-pkg", "version": "1.0.0"},
	}
	err := writePackument("/nonexistent/dir", "test-pkg", "1.0.0", versions)
	require.Error(t, err)
	require.Contains(t, err.Error(), "write packument")
}

func TestHashFileReal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

	hash, err := hashFileReal(path)
	require.NoError(t, err)
	// sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	require.Equal(t, "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9", hash)
}

func TestHashFileReal_NotFound(t *testing.T) {
	_, err := hashFileReal("/nonexistent/file")
	require.Error(t, err)
}

func TestRunBuildNPMRegistry_HashFails_SimplePkg(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "p", "1.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"p"}`),
	})

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", fmt.Errorf("unexpected: %v", args)
	})

	orig := hashFile
	t.Cleanup(func() { hashFile = orig })
	hashFile = func(_ string) (string, error) { return "", fmt.Errorf("hash failed") }

	origInput := buildNPMRegistryInput
	buildNPMRegistryInput = dir
	t.Cleanup(func() { buildNPMRegistryInput = origInput })

	// Hash failure is non-fatal (warning + skip), so no error returned.
	require.NoError(t, runBuildNPMRegistry(buildNPMRegistryCmd, nil))
}

func TestRunBuildNPMRegistry_HashFails_MainWithPlatforms(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "p", "1.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"p"}`),
		"build/hook":                 []byte("#!/bin/sh"),
		"build/hook_linux_amd64":     []byte("elf"),
	})

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", fmt.Errorf("unexpected: %v", args)
	})

	orig := hashFile
	t.Cleanup(func() { hashFile = orig })
	// Hash always fails -- covers the main-package-with-platforms hash failure path.
	hashFile = func(_ string) (string, error) { return "", fmt.Errorf("hash failed") }

	origInput := buildNPMRegistryInput
	buildNPMRegistryInput = dir
	t.Cleanup(func() { buildNPMRegistryInput = origInput })

	require.NoError(t, runBuildNPMRegistry(buildNPMRegistryCmd, nil))
}

func TestRunBuildNPMRegistry_HashFails_PlatformPkg(t *testing.T) {
	dir := t.TempDir()
	makeCookedPluginWithFiles(t, dir, "p", "1.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"p"}`),
		"build/hook":                 []byte("#!/bin/sh"),
		"build/hook_linux_amd64":     []byte("elf"),
	})

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "remote" {
			return "https://github.com/test-owner/test-repo.git\n", nil
		}
		return "", fmt.Errorf("unexpected: %v", args)
	})

	callCount := 0
	orig := hashFile
	t.Cleanup(func() { hashFile = orig })
	hashFile = func(path string) (string, error) {
		callCount++
		if callCount == 1 {
			// First call (main package) succeeds.
			return orig(path)
		}
		// Second call (platform package) fails.
		return "", fmt.Errorf("platform hash failed")
	}

	origInput := buildNPMRegistryInput
	buildNPMRegistryInput = dir
	t.Cleanup(func() { buildNPMRegistryInput = origInput })

	require.NoError(t, runBuildNPMRegistry(buildNPMRegistryCmd, nil))
}
