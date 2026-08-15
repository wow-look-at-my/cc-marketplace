package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ttl is how long a built index is served before a refresh is due. Repository
// descriptions and topics change on the scale of weeks, so a day is generous
// and still costs one background run.
const ttl = 24 * time.Hour

// refreshFloor stops a stale cache from starting a refresh on every prompt
// while one is already in flight.
const refreshFloor = 10 * time.Minute

// readmeBudget caps the extra request per description-less repository. A
// refresh that hits the cap says so.
const readmeBudget = 200

type cache struct {
	FetchedAt time.Time `json:"fetched_at"`
	Owners    []string  `json:"owners"`
	Repos     []Repo    `json:"repos"`
}

func cacheDir(home string) string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "repo-index")
	}
	return filepath.Join(home, ".cache", "repo-index")
}

func cachePath(home string) string { return filepath.Join(cacheDir(home), "index.json") }
func lockPath(home string) string  { return filepath.Join(cacheDir(home), "refresh.lock") }

func readCache(home string) *cache {
	data, err := os.ReadFile(cachePath(home))
	if err != nil {
		return nil
	}
	var c cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

// writeCache replaces the file atomically, so a hook that reads while a
// refresh writes never sees half a document.
func writeCache(home string, c cache) error {
	dir := cacheDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		return fmt.Errorf("cannot encode the index: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "index-*.json")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file in %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), cachePath(home)); err != nil {
		return fmt.Errorf("cannot replace %s: %w", cachePath(home), err)
	}
	return nil
}

// covers reports whether a cached index was built for exactly these owners.
// A different set is as stale as an expired one, however recent it is. An
// empty owner list means the hook could not tell locally, so it asks nothing
// of the cache's coverage.
func (c *cache) covers(owners []string, now time.Time) (fresh bool, sameOwners bool) {
	if c == nil {
		return false, false
	}
	sameOwners = len(owners) == 0 ||
		strings.EqualFold(strings.Join(c.Owners, "\x00"), strings.Join(owners, "\x00"))
	return sameOwners && now.Sub(c.FetchedAt) < ttl, sameOwners
}

// claimRefresh returns true when this process should run the refresh. The lock
// is advisory and time-based: a crashed refresh frees itself after the floor
// rather than wedging the index forever.
func claimRefresh(home string, now time.Time) bool {
	path := lockPath(home)
	if info, err := os.Stat(path); err == nil && now.Sub(info.ModTime()) < refreshFloor {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}
	if err := os.WriteFile(path, []byte(now.UTC().Format(time.RFC3339)), 0o600); err != nil {
		return false
	}
	// The floor is measured against the caller's clock, so the lock must carry
	// that clock rather than the filesystem's.
	_ = os.Chtimes(path, now, now)
	return true
}
