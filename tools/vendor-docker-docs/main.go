// Command vendor-docker-docs refreshes the Docker reference text that the
// `docs` plugin's dockerfile and docker-compose skills read from.
//
// The reference is vendored rather than summarized. A paraphrase of a
// specification is a second source of truth that goes stale with nothing to
// signal it; a verbatim copy pinned to a commit can be diffed against upstream
// and regenerated in one command.
//
// Usage: go run ./tools/vendor-docker-docs [-root <repo root>]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wow-look-at-my/go-containers/set"
)

func main() {
	root := flag.String("root", ".", "repository root to write into")
	flag.Parse()

	if err := run(*root); err != nil {
		fmt.Fprintln(os.Stderr, "vendor-docker-docs:", err)
		os.Exit(1)
	}
}

func run(root string) error {
	// One commit per upstream, resolved once, so every file in a run comes
	// from the same tree.
	commits := map[string]string{}
	fetchers := map[string]*fetcher{}

	for _, b := range bundles {
		for _, p := range b.Pages {
			if _, done := commits[p.Src.Repo]; done {
				continue
			}
			commit, err := resolve(p.Src)
			if err != nil {
				return err
			}
			commits[p.Src.Repo] = commit
			fetchers[p.Src.Repo] = newFetcher(p.Src.Repo, commit)
			fmt.Printf("%s@%s -> %s\n", p.Src.Repo, p.Src.Ref, commit)
		}
	}

	for _, b := range bundles {
		dir := filepath.Join(root, "plugins", "docs", "skills", b.Skill, "reference")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := writeBundle(dir, b, commits, fetchers); err != nil {
			return err
		}
	}
	return nil
}

func writeBundle(dir string, b bundle, commits map[string]string, fetchers map[string]*fetcher) error {
	written := set.New[string]()

	for _, p := range b.Pages {
		f := fetchers[p.Src.Repo]
		raw, err := f.get(p.Path)
		if err != nil {
			return err
		}
		out, err := render(raw, p, commits[p.Src.Repo], f.get)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Path, err)
		}
		if err := os.WriteFile(filepath.Join(dir, p.Out), []byte(out), 0o644); err != nil {
			return err
		}
		written.Add(p.Out)
		fmt.Printf("  %s/%s (%d bytes)\n", b.Skill, p.Out, len(out))
	}

	if err := pruneStale(dir, b.Skill, written); err != nil {
		return err
	}

	text, err := notice(b, commits)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, noticeFile), []byte(text), 0o644)
}

// pruneStale deletes a file the plan no longer produces. Left behind, it reads
// as current reference while nothing regenerates it.
func pruneStale(dir, skill string, written set.Set[string]) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == noticeFile || written.Contains(name) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
		fmt.Printf("  %s/%s removed (no longer in the plan)\n", skill, name)
	}
	return nil
}
