// referents.go carries the one tier that does not read the wording at all: a
// comment naming a symbol that exists nowhere is describing a tree that is
// gone. "see TestDarwinStatfsToLinux for the pin", written beside the change
// that deleted that test, is a tombstone with no tell in its phrasing, and no
// rewording makes it true again.
//
// Precision comes entirely from which identifiers are eligible. A comment about
// low-level work is full of names the repository does not define -- ENOSYS,
// RLIMIT_CORE, SYS_STATFS -- and denying over those is how a guard earns the
// reputation that gets it turned off. So an all-caps name is never a candidate,
// and neither is a short one: the shape this checks is a repository's own
// mixed-case or snake_case symbol, which the repository must contain.
package main

import (
	"context"
	"github.com/wow-look-at-my/go-containers/set"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// candidate matches an identifier long enough, and mixed enough, to be a symbol
// this repository owns rather than a word or an external constant.
var candidate = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+\b|\b[a-z]+[A-Z][A-Za-z0-9]*\b|\b[A-Z][a-z0-9]+[A-Z][A-Za-z0-9]*\b`)

// probeTimeout bounds the search. A guard that hangs is worse than one that
// misses, so an expired probe reports nothing.
const probeTimeout = 2 * time.Second

// DeadReferents returns the identifiers a comment names that appear neither in
// the text being written nor anywhere in the repository holding path.
//
// It returns nothing at all when it cannot answer: no repository, no ripgrep, a
// timeout, or a search that errors. An absent answer must never read as "the
// symbol is gone".
func DeadReferents(path, added string, blocks []Block) []string {
	root := RepoRoot(path)
	if root == "" {
		return nil
	}
	rg, err := exec.LookPath("rg")
	if err != nil {
		return nil
	}

	names := set.New[string]()
	for _, b := range blocks {
		for _, m := range candidate.FindAllString(b.Text, -1) {
			if len(m) < 8 || m == strings.ToUpper(m) {
				continue
			}
			names.Add(m)
		}
	}
	if names.Len() == 0 || names.Len() > 40 {
		return nil // an unbounded set is a comment this rule cannot judge cheaply
	}

	args := []string{"--no-messages", "--fixed-strings", "--files-with-matches", "--max-count", "1"}
	var ordered []string
	for n := range names.All() {
		if strings.Count(added, n) > 1 {
			continue
		}
		ordered = append(ordered, n)
	}
	if len(ordered) == 0 {
		return nil
	}

	var dead []string
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	for _, name := range ordered {
		cmd := exec.CommandContext(ctx, rg, append(append([]string{}, args...), "-e", name, root)...)
		out, err := cmd.Output()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() > 1 {
			return nil // ripgrep failed rather than found nothing
		}
		if strings.TrimSpace(string(out)) == "" {
			dead = append(dead, name)
		}
	}
	return dead
}

// RepoRoot walks up from path looking for a working tree. An empty result means
// the write is not inside one.
func RepoRoot(path string) string {
	dir := path
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (fi.IsDir() || fi.Mode().IsRegular()) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
