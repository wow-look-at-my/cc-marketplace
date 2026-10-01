package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// refresh rebuilds the index from GitHub and writes it to the cache. It runs
// in its own process, started by the hook, so a prompt never waits on the
// network.
func refresh(e env, cwd string, now time.Time) error {
	c := newClient()
	owners, err := discoverOwners(e.home, cwd, c)
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		return fmt.Errorf("no owner to index: none configured, no github remote in %q, and the GitHub API named no user", cwd)
	}

	var raw []repo
	for _, owner := range owners {
		list, err := c.repos(owner)
		if err != nil {
			return fmt.Errorf("cannot list repositories for %s: %w", owner, err)
		}
		raw = append(raw, list...)
	}

	repos, stats := buildIndex(raw, c.readmeText, readmeBudget)
	if err := writeCache(e.home, cache{FetchedAt: now, Owners: owners, Repos: repos}); err != nil {
		return err
	}

	report(e.stderr, owners, len(raw), len(repos), stats)
	return nil
}

// report states what the refresh kept and what it dropped. A count that
// vanishes without a word is how an index quietly stops covering things.
func report(w io.Writer, owners []string, seen, kept int, stats buildStats) {
	fmt.Fprintf(w, "repo-index: %d of %d repositories indexed for %v\n", kept, seen, owners)
	fmt.Fprintf(w, "repo-index: dropped %d archived, %d fork(s), %d with no description or README, %d with no distinctive name or topic\n",
		stats.Archived, stats.Forks, stats.NoText, stats.NoPhrase)
	if stats.ReadmeUsed > 0 {
		fmt.Fprintf(w, "repo-index: described %d repository(s) from the README, because GitHub carried no description\n", stats.ReadmeUsed)
	}
	if stats.ReadmeCalls >= readmeBudget {
		fmt.Fprintf(w, "repo-index: hit the README budget of %d, so some description-less repositories were dropped that a longer run would keep\n", readmeBudget)
	}
}

// spawnRefresh starts this same binary in refresh mode and does not wait. The
// child outlives this process on purpose: the work belongs to the machine, not
// to the prompt that noticed the index was due.
func spawnRefresh(home, cwd string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate this binary to refresh the index: %w", err)
	}
	cmd := exec.Command(self, "--refresh")
	cmd.Dir = cwd
	// The child keeps running after this process exits. Its report goes to a
	// file, because the hook's own stderr closes with the prompt.
	log, err := os.OpenFile(refreshLog(home), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err == nil {
		cmd.Stderr = log
		defer log.Close()
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start the index refresh: %w", err)
	}
	return cmd.Process.Release()
}

func refreshLog(home string) string {
	return filepath.Join(cacheDir(home), "refresh.log")
}
