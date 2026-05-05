package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

// makeCookedDirForPackaging creates a fake cooked plugin directory with
// package.json + arbitrary files for use as input to packagePluginToDir.
func makeCookedDirForPackaging(t *testing.T, name, version string, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	pkg := map[string]string{
		"name":    name,
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

func TestPackagePluginToDir_NoPlatformBinaries(t *testing.T) {
	cookedDir := makeCookedDirForPackaging(t, "owner-jq", "1.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"jq"}`),
		"commands/jq.md":             []byte("jq"),
	})
	outDir := t.TempDir()

	require.NoError(t, packagePluginToDir(cookedDir, "owner-jq", "1.0.0", outDir))

	manifestData, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	require.NoError(t, err)
	var m pluginPackageManifest
	require.NoError(t, json.Unmarshal(manifestData, &m))
	require.Equal(t, "owner-jq", m.Name)
	require.Equal(t, "1.0.0", m.Version)
	require.Equal(t, "tarballs/owner-jq/owner-jq-1.0.0.tgz", m.Main.Tarball)
	require.Empty(t, m.Platforms)

	_, err = os.Stat(filepath.Join(outDir, "tarballs", "owner-jq", "owner-jq-1.0.0.tgz"))
	require.NoError(t, err)
}

func TestPackagePluginToDir_WithPlatformBinaries(t *testing.T) {
	cookedDir := makeCookedDirForPackaging(t, "owner-hook", "2.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"hook"}`),
		"build/hook":                 []byte("#!/bin/sh\nold"),
		"build/hook_linux_amd64":     []byte("elf"),
		"build/hook_darwin_arm64":    []byte("macharm"),
	})
	outDir := t.TempDir()

	require.NoError(t, packagePluginToDir(cookedDir, "owner-hook", "2.0.0", outDir))

	var m pluginPackageManifest
	manifestData, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(manifestData, &m))
	require.Equal(t, "owner-hook", m.Name)
	require.Equal(t, "2.0.0", m.Version)
	require.Len(t, m.Platforms, 2)

	platsByName := map[string]manifestPlatformPackage{}
	for _, p := range m.Platforms {
		platsByName[p.Name] = p
	}
	linux := platsByName["owner-hook-linux-x64"]
	require.Equal(t, "linux", linux.OS)
	require.Equal(t, "x64", linux.CPU)
	require.Equal(t, "tarballs/owner-hook-linux-x64/owner-hook-linux-x64-2.0.0.tgz", linux.Tarball)

	darwin := platsByName["owner-hook-darwin-arm64"]
	require.Equal(t, "darwin", darwin.OS)
	require.Equal(t, "arm64", darwin.CPU)

	_, err = os.Stat(filepath.Join(outDir, "tarballs", "owner-hook", "owner-hook-2.0.0.tgz"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(outDir, "tarballs", "owner-hook-linux-x64", "owner-hook-linux-x64-2.0.0.tgz"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(outDir, "tarballs", "owner-hook-darwin-arm64", "owner-hook-darwin-arm64-2.0.0.tgz"))
	require.NoError(t, err)
}

func TestRunPackagePlugin_NoInput(t *testing.T) {
	origInput, origOutput := packagePluginInput, packagePluginOutput
	t.Cleanup(func() { packagePluginInput, packagePluginOutput = origInput, origOutput })

	packagePluginInput = ""
	packagePluginOutput = t.TempDir()
	err := runPackagePlugin(packagePluginCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--input is required")
}

func TestRunPackagePlugin_NoOutput(t *testing.T) {
	origInput, origOutput := packagePluginInput, packagePluginOutput
	t.Cleanup(func() { packagePluginInput, packagePluginOutput = origInput, origOutput })

	packagePluginInput = t.TempDir()
	packagePluginOutput = ""
	err := runPackagePlugin(packagePluginCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--output is required")
}

func TestRunPackagePlugin_NoPackageJSON(t *testing.T) {
	origInput, origOutput := packagePluginInput, packagePluginOutput
	t.Cleanup(func() { packagePluginInput, packagePluginOutput = origInput, origOutput })

	packagePluginInput = t.TempDir()
	packagePluginOutput = t.TempDir()
	err := runPackagePlugin(packagePluginCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read package.json")
}

func TestRunPackagePlugin_BadPackageJSON(t *testing.T) {
	origInput, origOutput := packagePluginInput, packagePluginOutput
	t.Cleanup(func() { packagePluginInput, packagePluginOutput = origInput, origOutput })

	cooked := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cooked, "package.json"), []byte("{not json"), 0644))

	packagePluginInput = cooked
	packagePluginOutput = t.TempDir()
	err := runPackagePlugin(packagePluginCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse package.json")
}

func TestRunPackagePlugin_PackageJSONMissingFields(t *testing.T) {
	origInput, origOutput := packagePluginInput, packagePluginOutput
	t.Cleanup(func() { packagePluginInput, packagePluginOutput = origInput, origOutput })

	cooked := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cooked, "package.json"), []byte(`{}`), 0644))

	packagePluginInput = cooked
	packagePluginOutput = t.TempDir()
	err := runPackagePlugin(packagePluginCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing name or version")
}

func TestRunPackagePlugin_Success(t *testing.T) {
	origInput, origOutput := packagePluginInput, packagePluginOutput
	t.Cleanup(func() { packagePluginInput, packagePluginOutput = origInput, origOutput })

	cooked := makeCookedDirForPackaging(t, "owner-foo", "5.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"foo"}`),
	})

	packagePluginInput = cooked
	packagePluginOutput = t.TempDir()
	require.NoError(t, runPackagePlugin(packagePluginCmd, nil))

	_, err := os.Stat(filepath.Join(packagePluginOutput, "manifest.json"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(packagePluginOutput, "tarballs", "owner-foo", "owner-foo-5.0.0.tgz"))
	require.NoError(t, err)
}

func TestPackagePluginToDir_TarballFails_NoPlatforms(t *testing.T) {
	cookedDir := makeCookedDirForPackaging(t, "owner-x", "1.0.0", nil)
	outDir := t.TempDir()

	orig := createTarball
	t.Cleanup(func() { createTarball = orig })
	createTarball = func(_, _ string) error { return errStub }

	err := packagePluginToDir(cookedDir, "owner-x", "1.0.0", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create main tarball")
}

func TestPackagePluginToDir_TarballFails_MainWithPlatforms(t *testing.T) {
	cookedDir := makeCookedDirForPackaging(t, "owner-x", "1.0.0", map[string][]byte{
		"build/hook":             []byte("#!/bin/sh"),
		"build/hook_linux_amd64": []byte("elf"),
	})
	outDir := t.TempDir()

	orig := createTarball
	t.Cleanup(func() { createTarball = orig })
	createTarball = func(_, _ string) error { return errStub }

	err := packagePluginToDir(cookedDir, "owner-x", "1.0.0", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create main tarball")
}

func TestPackagePluginToDir_TarballFails_Platform(t *testing.T) {
	cookedDir := makeCookedDirForPackaging(t, "owner-x", "1.0.0", map[string][]byte{
		"build/hook":             []byte("#!/bin/sh"),
		"build/hook_linux_amd64": []byte("elf"),
	})
	outDir := t.TempDir()

	orig := createTarball
	t.Cleanup(func() { createTarball = orig })
	calls := 0
	createTarball = func(src, dst string) error {
		calls++
		if calls == 1 {
			return orig(src, dst)
		}
		return errStub
	}

	err := packagePluginToDir(cookedDir, "owner-x", "1.0.0", outDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create platform tarball")
}

func TestValidateMainTarball_MissingPluginJSON(t *testing.T) {
	orig := tarballContents
	t.Cleanup(func() { tarballContents = orig })
	tarballContents = func(_ string) ([]string, error) {
		return []string{"package/package.json", "package/build/hook", "package/mh.plugin.json"}, nil
	}

	pluginJSON := map[string]interface{}{
		"name": "test",
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/build/hook"},
					},
				},
			},
		},
	}

	err := validateMainTarballReal("/fake.tgz", pluginJSON)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing .claude-plugin/plugin.json")
}

func TestValidateMainTarball_MissingHookBinary(t *testing.T) {
	orig := tarballContents
	t.Cleanup(func() { tarballContents = orig })
	tarballContents = func(_ string) ([]string, error) {
		return []string{"package/package.json", "package/.claude-plugin/plugin.json", "package/mh.plugin.json"}, nil
	}

	pluginJSON := map[string]interface{}{
		"name": "test",
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/build/hook"},
					},
				},
			},
		},
	}

	err := validateMainTarballReal("/fake.tgz", pluginJSON)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing hook binary")
}

func TestValidateMainTarball_Passes(t *testing.T) {
	orig := tarballContents
	t.Cleanup(func() { tarballContents = orig })
	tarballContents = func(_ string) ([]string, error) {
		return []string{
			"package/package.json",
			"package/.claude-plugin/plugin.json",
			"package/build/hook",
			"package/mh.plugin.json",
		}, nil
	}

	pluginJSON := map[string]interface{}{
		"name": "test",
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/build/hook"},
					},
				},
			},
		},
	}

	err := validateMainTarballReal("/fake.tgz", pluginJSON)
	require.NoError(t, err)
}

func TestValidateMainTarball_NoHooksSkipsValidation(t *testing.T) {
	orig := tarballContents
	t.Cleanup(func() { tarballContents = orig })
	tarballContents = func(_ string) ([]string, error) {
		return []string{"package/package.json", "package/.claude-plugin/plugin.json"}, nil
	}

	pluginJSON := map[string]interface{}{"name": "test"}
	err := validateMainTarballReal("/fake.tgz", pluginJSON)
	require.NoError(t, err)
}

func TestValidateMainTarball_NilPluginJSON(t *testing.T) {
	err := validateMainTarballReal("/fake.tgz", nil)
	require.NoError(t, err)
}

func TestPackagePluginToDir_MainTarballContainsPluginJSON(t *testing.T) {
	cookedDir := makeCookedDirForPackaging(t, "owner-hook", "3.0.0", map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"hook","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/build/hook"}]}]}}`),
		"build/hook":                 []byte("#!/bin/sh\nold"),
		"build/hook_linux_amd64":     []byte("elf-linux"),
		"build/hook_darwin_arm64":    []byte("mach-darwin"),
		"mh.plugin.json":            []byte(`{"sourceCommit":"abc"}`),
	})
	outDir := t.TempDir()

	require.NoError(t, packagePluginToDir(cookedDir, "owner-hook", "3.0.0", outDir))

	mainTarball := filepath.Join(outDir, "tarballs", "owner-hook", "owner-hook-3.0.0.tgz")
	files := listTarballContents(t, mainTarball)

	require.Contains(t, files, "package/.claude-plugin/plugin.json",
		"main tarball must contain .claude-plugin/plugin.json for hooks to work")
	require.Contains(t, files, "package/mh.plugin.json")
	require.Contains(t, files, "package/build/hook",
		"main tarball must contain the wrapper script in build/")
	require.NotContains(t, files, "package/build/hook_linux_amd64",
		"main tarball must not contain platform-specific binaries")
}

func listTarballContents(t *testing.T, path string) []string {
	t.Helper()
	out, err := exec.Command("tar", "-tzf", path).Output()
	require.NoError(t, err, "failed to list tarball contents")
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSuffix(line, "/")
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

var errStub = stubError("stub")

type stubError string

func (e stubError) Error() string { return string(e) }
