package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func mockExtractTag(t *testing.T, contents map[string]map[string][]byte) {
	t.Helper()
	orig := extractTagContents
	t.Cleanup(func() { extractTagContents = orig })

	extractTagContents = func(tag, destDir string) error {
		files, ok := contents[tag]
		if !ok {
			return nil
		}
		for relPath, data := range files {
			full := filepath.Join(destDir, relPath)
			os.MkdirAll(filepath.Dir(full), 0755)
			if err := os.WriteFile(full, data, 0755); err != nil {
				return err
			}
		}
		return nil
	}
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

	// Platform binaries excluded
	_, err = os.Stat(filepath.Join(mainDir, "build", "hook_linux_amd64"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(mainDir, "build", "hook_darwin_arm64"))
	require.True(t, os.IsNotExist(err))

	// Wrapper rewritten
	wrapper, err := os.ReadFile(filepath.Join(mainDir, "build", "hook"))
	require.NoError(t, err)
	require.Contains(t, string(wrapper), "node_modules/owner-test-")
	require.NotContains(t, string(wrapper), "old wrapper")

	// Other files present
	data, err := os.ReadFile(filepath.Join(mainDir, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "readme", string(data))

	// package.json has optionalDependencies
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

	// Both linux_amd64 binaries present under bin/
	hookData, err := os.ReadFile(filepath.Join(platDir, "bin", "hook"))
	require.NoError(t, err)
	require.Equal(t, "elf binary", string(hookData))

	valData, err := os.ReadFile(filepath.Join(platDir, "bin", "validate"))
	require.NoError(t, err)
	require.Equal(t, "elf validate", string(valData))

	// darwin binary NOT present
	_, err = os.Stat(filepath.Join(platDir, "bin", "hook_darwin_arm64"))
	require.True(t, os.IsNotExist(err))

	// package.json with os/cpu
	pkgData, err := os.ReadFile(filepath.Join(platDir, "package.json"))
	require.NoError(t, err)
	var pkg map[string]interface{}
	require.NoError(t, json.Unmarshal(pkgData, &pkg))
	require.Equal(t, "owner-test-linux-x64", pkg["name"])
	require.Equal(t, "2.0.0", pkg["version"])
	require.Equal(t, []interface{}{"linux"}, pkg["os"])
	require.Equal(t, []interface{}{"x64"}, pkg["cpu"])
}

func TestRunBuildNPMRegistry_NoPlatformBinaries(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/jq#1",
		"plugin/jq#latest",
	})

	mockExtractTag(t, map[string]map[string][]byte{
		"plugin/jq#1": {
			".claude-plugin/plugin.json": []byte(`{"name":"jq"}`),
			"commands/jq.md":             []byte("jq command"),
		},
	})

	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	require.NoError(t, err)
}

func TestRunBuildNPMRegistry_WithPlatformBinaries(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/hook#3",
		"plugin/hook#latest",
	})

	mockExtractTag(t, map[string]map[string][]byte{
		"plugin/hook#3": {
			".claude-plugin/plugin.json":  []byte(`{"name":"hook"}`),
			"build/hook":                  []byte("#!/bin/sh\nwrapper"),
			"build/hook_linux_amd64":      []byte("elf"),
			"build/hook_linux_arm64":      []byte("elf arm"),
			"build/hook_darwin_amd64":     []byte("mach-o x64"),
			"build/hook_darwin_arm64":     []byte("mach-o arm"),
		},
	})

	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	require.NoError(t, err)
}

func TestRunBuildNPMRegistry_PackumentStructure(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/myplugin#5",
	})

	orig := extractTagContents
	t.Cleanup(func() { extractTagContents = orig })
	extractTagContents = func(tag, destDir string) error {
		os.MkdirAll(filepath.Join(destDir, "build"), 0755)
		os.MkdirAll(filepath.Join(destDir, ".claude-plugin"), 0755)
		os.WriteFile(filepath.Join(destDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"myplugin"}`), 0644)
		os.WriteFile(filepath.Join(destDir, "build", "hook"), []byte("#!/bin/sh"), 0755)
		os.WriteFile(filepath.Join(destDir, "build", "hook_linux_amd64"), []byte("bin"), 0755)
		os.WriteFile(filepath.Join(destDir, "build", "hook_darwin_arm64"), []byte("bin"), 0755)
		return nil
	}

	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	require.NoError(t, err)
}

func TestRunBuildNPMRegistry_TarballFails_SimplePkg(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/p#1"})
	mockExtractTag(t, map[string]map[string][]byte{
		"plugin/p#1": {".claude-plugin/plugin.json": []byte(`{"name":"p"}`)},
	})
	orig := createTarball
	t.Cleanup(func() { createTarball = orig })
	createTarball = func(_, _ string) error { return fmt.Errorf("tar failed") }

	require.NoError(t, runBuildNPMRegistry(buildNPMRegistryCmd, nil))
}

func TestRunBuildNPMRegistry_TarballFails_PlatformPkg(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/p#1"})
	mockExtractTag(t, map[string]map[string][]byte{
		"plugin/p#1": {
			".claude-plugin/plugin.json": []byte(`{"name":"p"}`),
			"build/hook":                 []byte("#!/bin/sh"),
			"build/hook_linux_amd64":     []byte("elf"),
		},
	})
	orig := createTarball
	t.Cleanup(func() { createTarball = orig })
	createTarball = func(_, _ string) error { return fmt.Errorf("tar failed") }

	require.NoError(t, runBuildNPMRegistry(buildNPMRegistryCmd, nil))
}

func TestRunBuildNPMRegistry_ExtractError(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/broken#1",
	})

	orig := extractTagContents
	t.Cleanup(func() { extractTagContents = orig })
	extractTagContents = func(tag, destDir string) error {
		return fmt.Errorf("git archive failed")
	}

	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	require.NoError(t, err)
}

func TestRunBuildNPMRegistry_VerifyPlatformOutput(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/myplugin#2",
	})

	var capturedRegistryDir string
	orig := extractTagContents
	t.Cleanup(func() { extractTagContents = orig })
	extractTagContents = func(tag, destDir string) error {
		os.MkdirAll(filepath.Join(destDir, "build"), 0755)
		os.MkdirAll(filepath.Join(destDir, ".claude-plugin"), 0755)
		os.WriteFile(filepath.Join(destDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"myplugin"}`), 0644)
		os.WriteFile(filepath.Join(destDir, "build", "hook"), []byte("#!/bin/sh\nold"), 0755)
		os.WriteFile(filepath.Join(destDir, "build", "hook_linux_amd64"), []byte("elf64"), 0755)
		os.WriteFile(filepath.Join(destDir, "build", "hook_darwin_arm64"), []byte("macharm"), 0755)
		return nil
	}

	// Capture stdout to get registry_dir
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	w.Close()
	os.Stdout = origStdout
	require.NoError(t, err)

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "registry_dir=") {
			capturedRegistryDir = strings.TrimPrefix(line, "registry_dir=")
		}
	}
	require.NotEmpty(t, capturedRegistryDir)

	// Main packument exists with optionalDependencies
	mainPackument, err := os.ReadFile(filepath.Join(capturedRegistryDir, "test-owner-myplugin"))
	require.NoError(t, err)
	var mainPkg map[string]interface{}
	require.NoError(t, json.Unmarshal(mainPackument, &mainPkg))
	require.Equal(t, "test-owner-myplugin", mainPkg["name"])
	v := mainPkg["versions"].(map[string]interface{})["2.0.0"].(map[string]interface{})
	optDeps := v["optionalDependencies"].(map[string]interface{})
	require.Equal(t, "2.0.0", optDeps["test-owner-myplugin-linux-x64"])
	require.Equal(t, "2.0.0", optDeps["test-owner-myplugin-darwin-arm64"])

	// Platform packuments exist with os/cpu
	linuxPkg, err := os.ReadFile(filepath.Join(capturedRegistryDir, "test-owner-myplugin-linux-x64"))
	require.NoError(t, err)
	var lp map[string]interface{}
	require.NoError(t, json.Unmarshal(linuxPkg, &lp))
	lv := lp["versions"].(map[string]interface{})["2.0.0"].(map[string]interface{})
	require.Equal(t, []interface{}{"linux"}, lv["os"])
	require.Equal(t, []interface{}{"x64"}, lv["cpu"])

	darwinPkg, err := os.ReadFile(filepath.Join(capturedRegistryDir, "test-owner-myplugin-darwin-arm64"))
	require.NoError(t, err)
	var dp map[string]interface{}
	require.NoError(t, json.Unmarshal(darwinPkg, &dp))
	dv := dp["versions"].(map[string]interface{})["2.0.0"].(map[string]interface{})
	require.Equal(t, []interface{}{"darwin"}, dv["os"])
	require.Equal(t, []interface{}{"arm64"}, dv["cpu"])

	// Main tarball exists
	_, err = os.Stat(filepath.Join(capturedRegistryDir, "tarballs", "test-owner-myplugin", "test-owner-myplugin-2.0.0.tgz"))
	require.NoError(t, err)

	// Platform tarballs exist
	_, err = os.Stat(filepath.Join(capturedRegistryDir, "tarballs", "test-owner-myplugin-linux-x64", "test-owner-myplugin-linux-x64-2.0.0.tgz"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(capturedRegistryDir, "tarballs", "test-owner-myplugin-darwin-arm64", "test-owner-myplugin-darwin-arm64-2.0.0.tgz"))
	require.NoError(t, err)
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

	// Verify wrapper was rewritten
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

func TestRunBuildNPMRegistry_MultipleVersions(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/multi#1",
		"plugin/multi#2",
		"plugin/multi#3",
		"plugin/multi#latest",
	})

	mockExtractTag(t, map[string]map[string][]byte{
		"plugin/multi#1": {
			".claude-plugin/plugin.json": []byte(`{"name":"multi"}`),
			"commands/cmd.md":            []byte("cmd"),
		},
		"plugin/multi#2": {
			".claude-plugin/plugin.json": []byte(`{"name":"multi"}`),
			"commands/cmd.md":            []byte("cmd v2"),
		},
		"plugin/multi#3": {
			".claude-plugin/plugin.json":  []byte(`{"name":"multi"}`),
			"build/hook":                  []byte("#!/bin/sh"),
			"build/hook_linux_amd64":      []byte("elf"),
			"build/hook_darwin_arm64":     []byte("mach"),
		},
	})

	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	require.NoError(t, err)
}

func TestRunBuildNPMRegistry_AllExtractsFail(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/fail#1",
		"plugin/fail#2",
	})

	orig := extractTagContents
	t.Cleanup(func() { extractTagContents = orig })
	extractTagContents = func(tag, destDir string) error {
		return fmt.Errorf("archive broken")
	}

	err := runBuildNPMRegistry(buildNPMRegistryCmd, nil)
	require.NoError(t, err)
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
	writePackument(dir, "test-pkg", "1.0.0", versions)

	data, err := os.ReadFile(filepath.Join(dir, "test-pkg"))
	require.NoError(t, err)

	var packument map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &packument))
	require.Equal(t, "test-pkg", packument["name"])

	distTags := packument["dist-tags"].(map[string]interface{})
	require.Equal(t, "1.0.0", distTags["latest"])
}
