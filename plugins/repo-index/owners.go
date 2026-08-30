package main

import (
	"encoding/json"
	"fmt"
	"github.com/wow-look-at-my/go-containers/set"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// config is the only file a user writes. It names owners, never repositories:
// what each repository is comes from GitHub, so nothing here can go stale.
type config struct {
	Owners []string `json:"owners"`
}

func configPaths(home, cwd string) []string {
	var paths []string
	if home != "" {
		paths = append(paths, filepath.Join(home, ".claude", "repo-index.json"))
	}
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, ".claude", "repo-index.json"))
	}
	return paths
}

// readConfig merges the owners named by each config file that exists. A file
// that is present and malformed is an error: the user wrote it, and silence
// would leave them with an index that quietly ignores it.
func readConfig(home, cwd string) ([]string, error) {
	var owners []string
	for _, path := range configPaths(home, cwd) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
		var c config
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
		for _, owner := range c.Owners {
			if strings.TrimSpace(owner) == "" {
				return nil, fmt.Errorf("%s: owners contains an empty name", path)
			}
			owners = append(owners, owner)
		}
	}
	return owners, nil
}

var remotePattern = regexp.MustCompile(`(?:github\.com[:/])([^/]+)/`)

// remoteOwner reads the owner out of the checkout's origin remote. This is the
// zero-config source: work in a repository, and that owner's repositories are
// what the index covers.
func remoteOwner(cwd string) string {
	if cwd == "" {
		return ""
	}
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	m := remotePattern.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return ""
	}
	return m[1]
}

// discoverOwners returns the owners to index, in priority order and without
// duplicates. Comparison folds case, because GitHub owner names do. A nil
// client skips the authenticated-user lookup: the hook path must not touch
// the network, so only the refresh passes a client.
func discoverOwners(home, cwd string, c *client) ([]string, error) {
	configured, err := readConfig(home, cwd)
	if err != nil {
		return nil, err
	}
	candidates := configured
	if len(candidates) == 0 {
		candidates = append(candidates, remoteOwner(cwd))
		if c != nil {
			candidates = append(candidates, c.login())
		}
	}

	seen := set.New[string]()
	var owners []string
	for _, owner := range candidates {
		key := strings.ToLower(owner)
		if owner == "" || seen.Contains(key) {
			continue
		}
		seen.Add(key)
		owners = append(owners, owner)
	}
	return owners, nil
}
