package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestValidateJustfile_Clean(t *testing.T) {
	dir := t.TempDir()
	jf := filepath.Join(dir, "justfile")
	require.NoError(t, os.WriteFile(jf, []byte("[private]\nhelp:\n    @just --list\n\nprebuild:\n    npm install\n"), 0644))
	require.NoError(t, validateJustfile(jf))
}

func TestValidateJustfile_ForbiddenGoBuild(t *testing.T) {
	dir := t.TempDir()
	jf := filepath.Join(dir, "justfile")
	require.NoError(t, os.WriteFile(jf, []byte("prebuild:\n    go build ./...\n"), 0644))
	err := validateJustfile(jf)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "forbidden command")
	require.Contains(t, err.Error(), "go build")
}

func TestValidateJustfile_ForbiddenGoTest(t *testing.T) {
	dir := t.TempDir()
	jf := filepath.Join(dir, "justfile")
	require.NoError(t, os.WriteFile(jf, []byte("test:\n    go test ./...\n"), 0644))
	err := validateJustfile(jf)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "go test")
}

func TestValidateJustfile_ForbiddenGoToolchain(t *testing.T) {
	dir := t.TempDir()
	jf := filepath.Join(dir, "justfile")
	require.NoError(t, os.WriteFile(jf, []byte("build:\n    go-toolchain\n"), 0644))
	err := validateJustfile(jf)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "go-toolchain")
}

func TestValidateJustfile_ForbiddenGoSafeBuild(t *testing.T) {
	dir := t.TempDir()
	jf := filepath.Join(dir, "justfile")
	require.NoError(t, os.WriteFile(jf, []byte("build:\n    go-safe-build\n"), 0644))
	err := validateJustfile(jf)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "go-safe-build")
}

func TestValidateJustfile_CommentSkipped(t *testing.T) {
	dir := t.TempDir()
	jf := filepath.Join(dir, "justfile")
	require.NoError(t, os.WriteFile(jf, []byte("# go build is forbidden\nprebuild:\n    echo ok\n"), 0644))
	require.NoError(t, validateJustfile(jf))
}

func TestValidateJustfile_NonExistent(t *testing.T) {
	err := validateJustfile("/nonexistent/justfile")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to read justfile")
}

func TestHasGoFiles_WithGoFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644))
	require.True(t, hasGoFiles(dir))
}

func TestHasGoFiles_WithNestedGoFiles(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "cmd")
	require.NoError(t, os.MkdirAll(sub, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "main.go"), []byte("package main\n"), 0644))
	require.True(t, hasGoFiles(dir))
}

func TestHasGoFiles_NoGoFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("hello\n"), 0644))
	require.False(t, hasGoFiles(dir))
}

func TestHasGoFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	require.False(t, hasGoFiles(dir))
}

func TestFindToolchainBinary_PlatformMatch(t *testing.T) {
	dir := t.TempDir()
	name := "go-toolchain-" + runtime.GOOS + "-" + runtime.GOARCH
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("binary"), 0755))
	// Also add a decoy
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go-toolchain-other-os"), []byte("decoy"), 0755))

	path, err := findToolchainBinary(dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, name), path)
}

func TestFindToolchainBinary_FallbackToFirstFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "some-binary"), []byte("binary"), 0755))

	path, err := findToolchainBinary(dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "some-binary"), path)
}

func TestFindToolchainBinary_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actual-binary"), []byte("binary"), 0755))

	path, err := findToolchainBinary(dir)
	require.NoError(t, err)
	require.Contains(t, path, "actual-binary")
}

func TestFindToolchainBinary_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := findToolchainBinary(dir)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "no go-toolchain binary found")
}

func TestFindToolchainBinary_OnlyDirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir1"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir2"), 0755))
	_, err := findToolchainBinary(dir)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "no go-toolchain binary found")
}

func TestFindToolchainBinary_BadDir(t *testing.T) {
	_, err := findToolchainBinary("/nonexistent/dir")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to read toolchain dir")
}

func TestRunJustRecipe_NoJustfile(t *testing.T) {
	dir := t.TempDir()
	err := runJustRecipe(dir, "prebuild")
	require.NoError(t, err)
}

func TestRunBuildPlugin_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "plugins"), 0755))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runBuildPlugin(buildPluginCmd, []string{"nonexistent"})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "plugin not found")
}

func TestRunBuildPlugin_NoGoNoJustfile(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "plugins", "simple-plugin")
	require.NoError(t, os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"simple-plugin"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "README.md"), []byte("readme"), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runBuildPlugin(buildPluginCmd, []string{"simple-plugin"})
	require.NoError(t, err)
}

func TestRunBuildPlugin_ForbiddenJustfile(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "plugins", "bad-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "justfile"), []byte("build:\n    go build .\n"), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runBuildPlugin(buildPluginCmd, []string{"bad-plugin"})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "forbidden command")
}

func TestRunBuildPlugin_HookValidationFails(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "plugins", "hook-plugin")
	require.NoError(t, os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0755))

	pj := `{"name":"hook-plugin","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/missing-binary"}]}]}}`
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), []byte(pj), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runBuildPlugin(buildPluginCmd, []string{"hook-plugin"})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "hook binary validation failed")
}
