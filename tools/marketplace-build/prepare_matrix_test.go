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

	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "README.md"), []byte("readme"), 0644))

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runPrepareMatrix(prepareMatrixCmd, nil)
	require.NoError(t, err)
}

func TestRunPrepareMatrix_NoPluginsDir(t *testing.T) {
	tmpDir := t.TempDir()

	origRoot := repoRoot
	repoRoot = tmpDir
	t.Cleanup(func() { repoRoot = origRoot })

	err := runPrepareMatrix(prepareMatrixCmd, nil)
	require.NotNil(t, err)
}
