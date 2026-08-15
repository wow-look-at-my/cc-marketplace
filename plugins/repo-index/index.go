package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

//go:embed repos.json
var defaultIndex []byte

// Repo is one entry in the index. Match holds the phrases that make this repo
// relevant to a prompt. A phrase matches on whole words only.
type Repo struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Match       []string `json:"match"`
}

type Index struct {
	Repos []Repo `json:"repos"`
}

// overlayPaths returns the user index files, in the order they apply. A later
// file wins over an earlier one for the same repo name.
func overlayPaths(home, cwd string) []string {
	var paths []string
	if home != "" {
		paths = append(paths, filepath.Join(home, ".claude", "repo-index.json"))
	}
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, ".claude", "repo-index.json"))
	}
	return paths
}

// loadIndex merges the built-in index with each overlay that exists. A
// malformed overlay is an error, not a skip: the user wrote it and must hear
// that it does nothing.
func loadIndex(home, cwd string) ([]Repo, error) {
	var base Index
	if err := json.Unmarshal(defaultIndex, &base); err != nil {
		return nil, fmt.Errorf("built-in index is corrupt: %w", err)
	}

	merged := make(map[string]Repo, len(base.Repos))
	order := make([]string, 0, len(base.Repos))
	add := func(r Repo) {
		if _, seen := merged[r.Name]; !seen {
			order = append(order, r.Name)
		}
		merged[r.Name] = r
	}
	for _, r := range base.Repos {
		add(r)
	}

	for _, path := range overlayPaths(home, cwd) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
		var overlay Index
		if err := json.Unmarshal(data, &overlay); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
		for i, r := range overlay.Repos {
			if err := validate(r, path, i); err != nil {
				return nil, err
			}
			add(r)
		}
	}

	repos := make([]Repo, 0, len(order))
	for _, name := range order {
		repos = append(repos, merged[name])
	}
	return repos, nil
}

// validate rejects an entry that cannot produce a useful suggestion. A repo
// with no match phrases can never fire, so it is a mistake and not a choice.
func validate(r Repo, path string, i int) error {
	switch {
	case r.Name == "":
		return fmt.Errorf("%s: repos[%d] has no name", path, i)
	case r.URL == "":
		return fmt.Errorf("%s: repo %q has no url", path, r.Name)
	case r.Description == "":
		return fmt.Errorf("%s: repo %q has no description", path, r.Name)
	case len(r.Match) == 0:
		return fmt.Errorf("%s: repo %q has no match phrases, so it can never be suggested", path, r.Name)
	}
	for _, phrase := range r.Match {
		if phrase == "" {
			return fmt.Errorf("%s: repo %q has an empty match phrase", path, r.Name)
		}
	}
	return nil
}

// sortByName keeps the output stable when two repos score the same.
func sortByName(hits []Hit) {
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		return hits[a].Repo.Name < hits[b].Repo.Name
	})
}
