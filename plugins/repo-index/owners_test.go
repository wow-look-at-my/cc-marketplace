package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".claude")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "repo-index.json"), []byte(body), 0o644))
}

// gitRepo makes a checkout whose origin points at the given URL.
func gitRepo(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", url}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}
	return dir
}

func TestConfiguredOwnersWin(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `{"owners":["acme"]}`)
	cwd := gitRepo(t, "https://github.com/someone-else/thing.git")

	owners, err := discoverOwners(home, cwd, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"acme"}, owners)
}

func TestProjectConfigAddsToTheHomeConfig(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	writeConfig(t, home, `{"owners":["acme"]}`)
	writeConfig(t, cwd, `{"owners":["beta"]}`)

	owners, err := discoverOwners(home, cwd, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"acme", "beta"}, owners)
}

func TestTheCheckoutsRemoteIsTheZeroConfigSource(t *testing.T) {
	for name, url := range map[string]string{
		"https": "https://github.com/acme/thing.git",
		"ssh":   "git@github.com:acme/thing.git",
		"plain": "https://github.com/acme/thing",
	} {
		t.Run(name, func(t *testing.T) {
			owners, err := discoverOwners(t.TempDir(), gitRepo(t, url), nil)
			require.NoError(t, err)
			assert.Equal(t, []string{"acme"}, owners)
		})
	}
}

func TestANonGitHubRemoteNamesNoOwner(t *testing.T) {
	owners, err := discoverOwners(t.TempDir(), gitRepo(t, "https://gitlab.com/acme/thing.git"), nil)
	require.NoError(t, err)
	assert.Empty(t, owners)
}

func TestADirectoryWithoutGitNamesNoOwner(t *testing.T) {
	owners, err := discoverOwners(t.TempDir(), t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, owners)

	owners, err = discoverOwners(t.TempDir(), "", nil)
	require.NoError(t, err)
	assert.Empty(t, owners)
}

func TestTheAuthenticatedUserIsTheLastResort(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"login":"someone"}`)
	})

	owners, err := discoverOwners(t.TempDir(), t.TempDir(), c)
	require.NoError(t, err)
	assert.Equal(t, []string{"someone"}, owners)
}

func TestOwnersAreDeduplicatedIgnoringCase(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `{"owners":["Acme","acme","beta"]}`)

	owners, err := discoverOwners(home, "", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"Acme", "beta"}, owners)
}

func TestAnUnusableConfigFailsLoud(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"malformed":  {`{"owners":[`, "not valid JSON"},
		"emptyOwner": {`{"owners":["acme",""]}`, "empty name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			writeConfig(t, home, tc.body)
			_, err := discoverOwners(home, "", nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestAnUnreadableConfigFailsLoud(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "repo-index.json"), 0o755))

	_, err := discoverOwners(home, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot read")
}
