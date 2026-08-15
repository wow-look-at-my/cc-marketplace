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

// The index carries the names and descriptions of every repository the user
// can see, private ones included. It is theirs, and it stays on their disk
// readable by them alone. These modes are the whole protection, so they are
// asserted rather than assumed.
func TestTheIndexIsReadableOnlyByItsOwner(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()
	require.NoError(t, writeCache(home, cache{FetchedAt: epoch, Repos: []Repo{{Name: "private/thing"}}}))
	require.True(t, claimRefresh(home, epoch))

	dir, err := os.Stat(cacheDir(home))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm(), "the cache directory must not be listable by others")

	for _, path := range []string{cachePath(home), lockPath(home)} {
		info, err := os.Stat(path)
		require.NoError(t, err, path)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "%s must not be readable by others", path)
	}
}

// A temporary file is a real file for as long as it exists. It must not be the
// one moment the index is world readable.
func TestTheIndexIsNeverBrieflyWorldReadable(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(cacheDir(home), 0o700))

	tmp, err := os.CreateTemp(cacheDir(home), "index-*.json")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())
	require.NoError(t, tmp.Close())

	info, err := os.Stat(tmp.Name())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
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
