package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// client reads upstream repositories. The whole network surface of this tool is
// these two calls, so a test supplies its own and never touches the network.
type client interface {
	// resolve turns a branch name into the commit it points at right now, so
	// every file in one run comes from the same tree and the recorded
	// provenance is exact.
	resolve(u upstream) (string, error)
	// get reads one file from a repository at a pinned commit.
	get(repo, commit, path string) (string, error)
}

// ghClient shells out to `gh` rather than calling the API directly: `gh` is
// already authenticated and proxy-configured everywhere this runs, and an
// unauthenticated request to raw.githubusercontent.com is rate limited enough
// to fail a full run.
type ghClient struct {
	cache map[string]string
}

func newGHClient() *ghClient {
	return &ghClient{cache: map[string]string{}}
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

func (c *ghClient) resolve(u upstream) (string, error) {
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

// get caches its results because several pages include the same partial, and
// each cache miss is a network round trip.
func (c *ghClient) get(repo, commit, path string) (string, error) {
	key := repo + "@" + commit + "/" + path
	if hit, ok := c.cache[key]; ok {
		return hit, nil
	}
	out, err := gh("api",
		"-H", "Accept: application/vnd.github.raw",
		fmt.Sprintf("repos/%s/contents/%s?ref=%s", repo, path, commit))
	if err != nil {
		return "", err
	}
	c.cache[key] = out
	return out, nil
}
