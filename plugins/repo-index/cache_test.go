package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var epoch = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

func TestCacheRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", "")
	want := cache{FetchedAt: epoch, Owners: []string{"acme"}, Repos: []Repo{{Name: "acme/one"}}}
	require.NoError(t, writeCache(home, want))

	got := readCache(home)
	require.NotNil(t, got)
	assert.Equal(t, want.Owners, got.Owners)
	assert.Equal(t, want.Repos, got.Repos)
	assert.True(t, want.FetchedAt.Equal(got.FetchedAt))
}

func TestCacheHonoursXDGCacheHome(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	require.NoError(t, writeCache(t.TempDir(), cache{FetchedAt: epoch}))

	_, err := os.Stat(filepath.Join(xdg, "repo-index", "index.json"))
	assert.NoError(t, err)
}

func TestAMissingOrCorruptCacheReadsAsAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", "")
	assert.Nil(t, readCache(home))

	require.NoError(t, os.MkdirAll(cacheDir(home), 0o700))
	require.NoError(t, os.WriteFile(cachePath(home), []byte("{{{"), 0o600))
	assert.Nil(t, readCache(home))
}

func TestWriteCacheReportsAnUnusableDirectory(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cache"), []byte("not a directory"), 0o600))

	err := writeCache(home, cache{FetchedAt: epoch})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create")
}

func TestCoversIsFalseForAnExpiredOrDifferentlyScopedIndex(t *testing.T) {
	c := &cache{FetchedAt: epoch, Owners: []string{"acme"}}

	fresh, same := c.covers([]string{"acme"}, epoch.Add(time.Hour))
	assert.True(t, fresh)
	assert.True(t, same)

	fresh, same = c.covers([]string{"acme"}, epoch.Add(ttl+time.Minute))
	assert.False(t, fresh, "an index older than the ttl is due for a refresh")
	assert.True(t, same)

	fresh, same = c.covers([]string{"beta"}, epoch)
	assert.False(t, fresh, "a different owner set is stale however recent it is")
	assert.False(t, same)
}

func TestCoversAsksNothingWhenTheHookCannotNameAnOwner(t *testing.T) {
	c := &cache{FetchedAt: epoch, Owners: []string{"acme"}}
	fresh, same := c.covers(nil, epoch)
	assert.True(t, fresh)
	assert.True(t, same)
}

func TestCoversIsFalseForAnAbsentCache(t *testing.T) {
	var c *cache
	fresh, same := c.covers([]string{"acme"}, epoch)
	assert.False(t, fresh)
	assert.False(t, same)
}

func TestOnlyOneRefreshIsClaimedWithinTheFloor(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()

	assert.True(t, claimRefresh(home, epoch))
	assert.False(t, claimRefresh(home, epoch.Add(time.Minute)), "a refresh is already in flight")
	assert.True(t, claimRefresh(home, epoch.Add(refreshFloor+time.Minute)), "a crashed refresh must not wedge the index")
}

func TestClaimRefreshIsFalseWhenTheLockCannotBeWritten(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cache"), []byte("not a directory"), 0o600))

	assert.False(t, claimRefresh(home, epoch))
}
