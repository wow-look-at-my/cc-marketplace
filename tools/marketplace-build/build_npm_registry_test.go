package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestRunBuildNPMRegistry_NoPlugins(t *testing.T) {
	mockGitWithTags(t, nil)

	origArchive := gitArchiveTag
	t.Cleanup(func() { gitArchiveTag = origArchive })
	gitArchiveTag = func(tag, path string) error {
		return os.WriteFile(path, []byte("fake"), 0644)
	}

	cmd := buildNPMRegistryCmd
	err := runBuildNPMRegistry(cmd, nil)
	require.NoError(t, err)
}

func TestRunBuildNPMRegistry_WithPlugins(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/jq#1",
		"plugin/jq#2",
		"plugin/jq#latest",
	})

	origArchive := gitArchiveTag
	t.Cleanup(func() { gitArchiveTag = origArchive })
	var archivedTags []string
	gitArchiveTag = func(tag, path string) error {
		archivedTags = append(archivedTags, tag)
		return os.WriteFile(path, []byte("fake tarball"), 0644)
	}

	cmd := buildNPMRegistryCmd
	err := runBuildNPMRegistry(cmd, nil)
	require.NoError(t, err)

	// Should have archived both versioned tags (not #latest)
	require.Contains(t, archivedTags, "plugin/jq#1")
	require.Contains(t, archivedTags, "plugin/jq#2")
	require.Len(t, archivedTags, 2)
}

func TestRunBuildNPMRegistry_PackumentContent(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/jq#3",
	})

	origArchive := gitArchiveTag
	t.Cleanup(func() { gitArchiveTag = origArchive })
	var registryDir string
	gitArchiveTag = func(tag, path string) error {
		// registry dir is 3 levels up: tarballs/<pkg>/<tarball>.tgz
		registryDir = filepath.Dir(filepath.Dir(filepath.Dir(path)))
		return os.WriteFile(path, []byte("fake"), 0644)
	}

	cmd := buildNPMRegistryCmd
	err := runBuildNPMRegistry(cmd, nil)
	require.NoError(t, err)
	require.NotEmpty(t, registryDir)

	pkgName := "test-owner-jq"
	packumentPath := filepath.Join(registryDir, pkgName)

	data, err := os.ReadFile(packumentPath)
	require.NoError(t, err)

	var packument map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &packument))
	require.Equal(t, pkgName, packument["name"])

	distTags := packument["dist-tags"].(map[string]interface{})
	require.Equal(t, "3.0.0", distTags["latest"])

	versions := packument["versions"].(map[string]interface{})
	require.Contains(t, versions, "3.0.0")

	v := versions["3.0.0"].(map[string]interface{})
	dist := v["dist"].(map[string]interface{})
	tarball := dist["tarball"].(string)
	require.Contains(t, tarball, "test-owner.github.io")
	require.Contains(t, tarball, "tarballs/"+pkgName)
	require.Contains(t, tarball, "test-owner-jq-3.0.0.tgz")
}

func TestRunBuildNPMRegistry_ArchiveError(t *testing.T) {
	mockGitWithTags(t, []string{
		"plugin/broken#1",
	})

	origArchive := gitArchiveTag
	t.Cleanup(func() { gitArchiveTag = origArchive })
	gitArchiveTag = func(tag, path string) error {
		return fmt.Errorf("git archive failed")
	}

	cmd := buildNPMRegistryCmd
	// Should not error — archive failures are warnings, skipped plugin
	err := runBuildNPMRegistry(cmd, nil)
	require.NoError(t, err)
}

func TestGitArchiveTagReal_NotInCI(t *testing.T) {
	// gitArchiveTagReal calls real git — just verify the function is callable
	// (it will fail with a real git error in the test environment, which is expected)
	err := gitArchiveTagReal("nonexistent-tag", filepath.Join(t.TempDir(), "out.tgz"))
	require.Error(t, err) // expected: git will fail on nonexistent tag
}
