package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiFor serves an owner's repositories and their READMEs, and points the
// client at itself through the environment newClient reads.
func apiFor(t *testing.T, repos []repo) {
	t.Helper()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/readme"):
			fmt.Fprint(w, `{"content":"From the readme.","encoding":"none"}`)
		case strings.HasSuffix(r.URL.Path, "/repos"):
			json.NewEncoder(w).Encode(repos)
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	})
	t.Setenv("GITHUB_API_URL", c.api)
	t.Setenv("GH_TOKEN", "t")
}

func TestRefreshWritesAnIndexBuiltFromGitHub(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	apiFor(t, []repo{
		repoFixture("widget-press", "Stamps widgets.", "widgets"),
		repoFixture("quiet", ""),
	})

	e, _, errOut := testEnv(t)
	writeConfig(t, e.home, `{"owners":["acme"]}`)
	require.NoError(t, refresh(e, t.TempDir(), epoch))

	got := readCache(e.home)
	require.NotNil(t, got)
	assert.Equal(t, []string{"acme"}, got.Owners)
	require.Len(t, got.Repos, 2)
	assert.Equal(t, "owner/widget-press", got.Repos[0].Name)
	assert.Equal(t, "From the readme.", got.Repos[1].Description)
	assert.Contains(t, errOut.String(), "2 of 2 repositories indexed")
}

func TestRefreshFailsLoudWithNoOwner(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	apiFor(t, nil)
	e, _, _ := testEnv(t)

	err := refresh(e, t.TempDir(), epoch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no owner to index")
}

func TestRefreshFailsLoudWhenGitHubDoes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	})
	t.Setenv("GITHUB_API_URL", c.api)

	e, _, _ := testEnv(t)
	writeConfig(t, e.home, `{"owners":["acme"]}`)

	err := refresh(e, t.TempDir(), epoch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot list repositories for acme")
}

func TestRefreshPassesAConfigErrorThrough(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, _, _ := testEnv(t)
	writeConfig(t, e.home, `{"owners":[`)

	require.ErrorContains(t, refresh(e, "", epoch), "not valid JSON")
}

func TestReportNamesEverythingItDropped(t *testing.T) {
	var out bytes.Buffer
	report(&out, []string{"acme"}, 10, 4, buildStats{
		Archived: 2, Forks: 1, NoText: 2, NoPhrase: 1, ReadmeUsed: 3, ReadmeCalls: readmeBudget,
	})

	text := out.String()
	assert.Contains(t, text, "4 of 10 repositories indexed for [acme]")
	assert.Contains(t, text, "dropped 2 archived, 1 fork(s), 2 with no description or README, 1 with no distinctive name or topic")
	assert.Contains(t, text, "described 3 repository(s) from the README")
	assert.Contains(t, text, "hit the README budget")
}

func TestSpawnRefreshStartsThisBinaryAndReturns(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(cacheDir(home), 0o700))

	require.NoError(t, spawnRefresh(home, t.TempDir()))
	assert.Equal(t, filepath.Join(cacheDir(home), "refresh.log"), refreshLog(home))

	// The report names every repository the refresh saw, so the log it lands
	// in is as private as the index itself.
	info, err := os.Stat(refreshLog(home))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// The refresh writes only under the cache directory. A cache that landed in
// the plugin's own directory would be packaged into the published plugin.
func TestRefreshWritesNothingIntoTheWorkingDirectory(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	apiFor(t, []repo{repoFixture("widget-press", "Stamps widgets.", "widgets")})

	e, _, _ := testEnv(t)
	writeConfig(t, e.home, `{"owners":["acme"]}`)
	cwd := t.TempDir()
	require.NoError(t, refresh(e, cwd, epoch))

	left, err := os.ReadDir(cwd)
	require.NoError(t, err)
	assert.Empty(t, left, "the refresh must leave nothing behind where it ran")
}
