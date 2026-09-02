package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// fetcher reads files out of a GitHub repository at one pinned commit.
//
// It shells out to `gh` rather than calling the API directly: `gh` is already
// authenticated and proxy-configured everywhere this runs, and an unauthenticated
// request to raw.githubusercontent.com is rate limited enough to fail a full run.
type fetcher struct {
	repo   string
	commit string
	cache  map[string]string
}

func gh(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("gh", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// resolve turns a branch name into the commit it points at right now, so every
// file in one run is read from the same tree and the recorded provenance is exact.
func resolve(u upstream) (string, error) {
	out, err := gh("api", "repos/"+u.Repo+"/commits/"+u.Ref, "--jq", ".sha")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(out)
	if len(sha) != 40 {
		return "", fmt.Errorf("%s: expected a 40-character commit, got %q", u.Repo, sha)
	}
	return sha, nil
}

func newFetcher(repo, commit string) *fetcher {
	return &fetcher{repo: repo, commit: commit, cache: map[string]string{}}
}

// get reads one file. Results are cached because several pages include the same
// partial, and each cache miss is a network round trip.
func (f *fetcher) get(path string) (string, error) {
	if hit, ok := f.cache[path]; ok {
		return hit, nil
	}
	out, err := gh("api",
		"-H", "Accept: application/vnd.github.raw",
		fmt.Sprintf("repos/%s/contents/%s?ref=%s", f.repo, path, f.commit))
	if err != nil {
		return "", err
	}
	f.cache[path] = out
	return out, nil
}
