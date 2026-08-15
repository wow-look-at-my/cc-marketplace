package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeState(dir, "s1", map[string]bool{"a/one": true, "a/two": true}))

	seen := readState(dir, "s1")
	assert.True(t, seen["a/one"])
	assert.True(t, seen["a/two"])
	assert.False(t, seen["a/three"])
}

func TestUnknownSessionReadsEmpty(t *testing.T) {
	assert.Empty(t, readState(t.TempDir(), "never-written"))
}

func TestCorruptStateReadsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := statePath(dir, "s1")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{{{"), 0o600))

	assert.Empty(t, readState(dir, "s1"))
}

func TestSessionIDWithSeparatorStaysInsideTheDirectory(t *testing.T) {
	dir := t.TempDir()
	path := statePath(dir, "../../escape/attempt")
	assert.Equal(t, filepath.Join(dir, "repo-index"), filepath.Dir(path))
}

func TestWriteStateReportsAFailedDirectory(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))

	err := writeState(blocked, "s1", map[string]bool{"a/one": true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create")
}

func TestWriteStateReportsAFailedWrite(t *testing.T) {
	dir := t.TempDir()
	path := statePath(dir, "s1")
	require.NoError(t, os.MkdirAll(path, 0o700))

	err := writeState(dir, "s1", map[string]bool{"a/one": true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot write")
}
