package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGH installs a fake `gh` on the path this tool invokes, and returns the
// file its every invocation is logged to. Nothing here reaches the network.
func stubGH(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	path := filepath.Join(dir, "gh")

	body := "#!/bin/sh\necho \"$@\" >> " + log + "\n" + script
	require.NoError(t, os.WriteFile(path, []byte(body), 0o755))

	previous := ghBinary
	ghBinary = path
	t.Cleanup(func() { ghBinary = previous })

	return log
}

func TestResolveReturnsTheCommit(t *testing.T) {
	stubGH(t, "echo 0123456789abcdef0123456789abcdef01234567\n")

	commit, err := newGHClient().resolve(dockerDocs)

	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", commit)
}

// A short or empty answer means the ref did not resolve. Vendoring against it
// would record provenance that points nowhere.
func TestResolveRejectsSomethingThatIsNotACommit(t *testing.T) {
	stubGH(t, "echo not-a-sha\n")

	_, err := newGHClient().resolve(dockerDocs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "40-character commit")
}

func TestResolveReportsTheCommandFailure(t *testing.T) {
	stubGH(t, "echo 'HTTP 404' >&2\nexit 1\n")

	_, err := newGHClient().resolve(dockerDocs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404", "the stderr reason survives")
}

func TestGetReadsAFileAndCachesIt(t *testing.T) {
	log := stubGH(t, "echo file-body\n")
	c := newGHClient()

	first, err := c.get("docker/docs", "abc", "content/reference/compose-file/services.md")
	require.NoError(t, err)
	second, err := c.get("docker/docs", "abc", "content/reference/compose-file/services.md")
	require.NoError(t, err)

	assert.Equal(t, "file-body\n", first)
	assert.Equal(t, first, second)

	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, 1, countLines(string(calls)), "the second read is served from cache")
}

// The cache key carries the commit, so a refresh against a new commit does not
// hand back the previous run's bytes.
func TestGetDoesNotReuseAnotherCommitsBytes(t *testing.T) {
	log := stubGH(t, "echo body\n")
	c := newGHClient()

	_, err := c.get("docker/docs", "aaa", "same/path.md")
	require.NoError(t, err)
	_, err = c.get("docker/docs", "bbb", "same/path.md")
	require.NoError(t, err)

	calls, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, 2, countLines(string(calls)))
}

func TestGetReportsAFailedRead(t *testing.T) {
	stubGH(t, "echo 'Not Found' >&2\nexit 1\n")

	_, err := newGHClient().get("docker/docs", "abc", "missing.md")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Not Found")
}

func countLines(s string) int {
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}
