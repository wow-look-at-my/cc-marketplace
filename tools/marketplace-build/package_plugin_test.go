package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

var errStub = stubError("stub")

type stubError string

func (e stubError) Error() string { return string(e) }
