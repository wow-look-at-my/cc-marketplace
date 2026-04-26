package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestIsIncludedInMarketplace_True(t *testing.T) {
	pj := map[string]interface{}{
		"name": "test",
		"mh":   map[string]interface{}{"include_in_marketplace": true},
	}
	require.True(t, isIncludedInMarketplace(pj))
}

func TestIsIncludedInMarketplace_False(t *testing.T) {
	pj := map[string]interface{}{
		"name": "test",
		"mh":   map[string]interface{}{"include_in_marketplace": false},
	}
	require.False(t, isIncludedInMarketplace(pj))
}

func TestIsIncludedInMarketplace_NoMH(t *testing.T) {
	require.False(t, isIncludedInMarketplace(map[string]interface{}{"name": "test"}))
}

func TestIsIncludedInMarketplace_MHNotMap(t *testing.T) {
	require.False(t, isIncludedInMarketplace(map[string]interface{}{"mh": "string"}))
}

func TestIsIncludedInMarketplace_MissingField(t *testing.T) {
	require.False(t, isIncludedInMarketplace(map[string]interface{}{"mh": map[string]interface{}{}}))
}

func TestIsIncludedInMarketplace_WrongType(t *testing.T) {
	pj := map[string]interface{}{
		"mh": map[string]interface{}{"include_in_marketplace": "yes"},
	}
	require.False(t, isIncludedInMarketplace(pj))
}

func TestRunPrepareMatrix(t *testing.T) {
	tmpDir := t.TempDir()
	pluginsDir := filepath.Join(tmpDir, "plugins")

	// Create two plugins: one included, one not
	for _, tc := range []struct {
		name    string
		include bool
	}{
		{"included-plugin", true},
		{"excluded-plugin", false},
	} {
		dir := filepath.Join(pluginsDir, tc.name, ".claude-plugin")
		require.NoError(t, os.MkdirAll(dir, 0755))
		pj := map[string]interface{}{
			"name": tc.name,
			"mh":   map[string]interface{}{"include_in_marketplace": tc.include},
		}
		data, err := json.Marshal(pj)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644))
	}

	// Also create a non-directory file in plugins/
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "README.md"), []byte("readme"), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	// Mock git to return no tags (first build = has changes)
	mockGitWithTags(t, nil)

	err := runPrepareMatrix(prepareMatrixCmd, nil)
	require.NoError(t, err)
}

func TestRunPrepareMatrix_InfraChanged(t *testing.T) {
	tmpDir := t.TempDir()
	pluginsDir := filepath.Join(tmpDir, "plugins")

	dir := filepath.Join(pluginsDir, "my-plugin", ".claude-plugin")
	require.NoError(t, os.MkdirAll(dir, 0755))
	pj := map[string]interface{}{
		"name": "my-plugin",
		"mh":   map[string]interface{}{"include_in_marketplace": true},
	}
	data, err := json.Marshal(pj)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "plugin/my-plugin/v1\n", nil
		}
		if args[0] == "show" {
			return `{"sourceCommit":"old123"}`, nil
		}
		if args[0] == "rev-list" {
			return "1\n", nil // infra changed
		}
		return "", nil
	})

	err = runPrepareMatrix(prepareMatrixCmd, nil)
	require.NoError(t, err)
}
