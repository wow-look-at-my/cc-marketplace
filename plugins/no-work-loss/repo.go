package main

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A hook runs in front of every Bash call, so a hung git costs the session
// directly. Three seconds is far past a normal status on any real tree; past
// it the answer is unknown, and unknown denies.
const gitTimeout = 3 * time.Second

var (
	errUnknownDir  = errors.New("target directory is not statically known")
	errNoRemoteRef = errors.New("no remote-tracking ref locally, so what the remote holds is unknown -- git fetch first")
)

type repoState struct {
	root      string
	inRepo    bool
	tracked   []string
	untracked []string
	ignored   []string
	stash     int
	err       error

	ignoredLoaded bool
	stashLoaded   bool
}

// repoCache keeps one probe per directory per hook invocation. A chain like
// `git checkout master && git reset --hard` must not pay for status twice.
type repoCache struct{ m map[string]*repoState }

func newRepoCache() *repoCache { return &repoCache{m: map[string]*repoState{}} }

func (c *repoCache) probe(dir string) *repoState {
	if dir == unknownDirText {
		return &repoState{err: errUnknownDir}
	}
	if st, ok := c.m[dir]; ok {
		return st
	}
	st := probeDir(dir)
	c.m[dir] = st
	return st
}

func probeDir(dir string) *repoState {
	st := &repoState{}
	root, stderr, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		// "not a repository" is a real answer: there is no uncommitted work
		// here to lose. Anything else is a failure to determine state.
		if strings.Contains(strings.ToLower(stderr), "not a git repository") {
			return st
		}
		st.err = err
		return st
	}
	st.root = strings.TrimSpace(root)
	st.inRepo = st.root != ""
	if !st.inRepo {
		return st
	}

	// -uall matters: the default collapses an untracked directory to its name,
	// so `rm internal/config/env.go` would find no entry called that and read
	// as safe. Ignored files stay out of this listing, which is what keeps a
	// gitignored build artifact deletable.
	out, _, err := runGit(st.root, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		st.err = err
		return st
	}
	st.tracked, st.untracked, _ = parseStatusZ(out)
	return st
}

// ensureIgnored is separate because --ignored makes git walk every ignored
// path, which is the expensive case. Only `clean -x` needs it.
func (c *repoCache) ensureIgnored(st *repoState) {
	if st.ignoredLoaded || !st.inRepo || st.err != nil {
		return
	}
	st.ignoredLoaded = true
	out, _, err := runGit(st.root, "status", "--porcelain", "-z", "--ignored=matching")
	if err != nil {
		st.err = err
		return
	}
	_, _, st.ignored = parseStatusZ(out)
}

func (c *repoCache) ensureStash(st *repoState) {
	if st.stashLoaded || !st.inRepo || st.err != nil {
		return
	}
	st.stashLoaded = true
	out, _, err := runGit(st.root, "stash", "list")
	if err != nil {
		st.err = err
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			st.stash++
		}
	}
}

// parseStatusZ splits `status --porcelain -z`. NUL-separated records mean paths
// arrive raw rather than quoted, and a rename record is followed by a second
// field holding the original path -- consuming it is what keeps the entries
// after a rename from being read as status codes.
func parseStatusZ(out string) (tracked, untracked, ignored []string) {
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if len(rec) < 4 {
			continue
		}
		code, path := rec[:2], rec[3:]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // the original path rides in its own field
		}
		switch code {
		case "??":
			untracked = append(untracked, path)
		case "!!":
			ignored = append(ignored, path)
		default:
			tracked = append(tracked, path)
		}
	}
	return tracked, untracked, ignored
}

func runGit(dir string, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	// Never let a hook prompt for credentials or open an editor.
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat")
	err = cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.String(), errb.String(), err
}

// atRisk returns the entries of each requested class that the finding's paths
// actually cover. A nil path list means the command operates on the whole
// repository.
func (st *repoState) atRisk(f *finding, cwd string) (tracked, untracked, ignored []string) {
	sel := func(entries []string) []string {
		if f.paths == nil {
			return entries
		}
		var hit []string
		for _, e := range entries {
			for _, p := range f.paths {
				if coversPath(st.root, cwd, p, e) {
					hit = append(hit, e)
					break
				}
			}
		}
		return hit
	}
	if f.haz&hazTracked != 0 {
		tracked = sel(st.tracked)
	}
	if f.haz&hazUntracked != 0 {
		untracked = sel(st.untracked)
	}
	if f.haz&hazIgnored != 0 {
		ignored = sel(st.ignored)
	}
	return tracked, untracked, ignored
}

// coversPath reports whether a command operand names a status entry, either
// directly or by containing it.
func coversPath(root, cwd string, operand word, entry string) bool {
	if !operand.static {
		return false // handled earlier: unknown operands deny outright
	}
	abs := operand.text
	if !filepath.IsAbs(abs) {
		if cwd == unknownDirText {
			return false
		}
		abs = filepath.Join(cwd, abs)
	}
	rel, err := filepath.Rel(root, filepath.Clean(abs))
	if err != nil || strings.HasPrefix(rel, "..") {
		return false // outside this repository
	}
	entry = strings.TrimSuffix(entry, "/")
	if rel == "." {
		return true
	}
	// Containment counts in both directions: the operand may be a directory
	// holding the entry, or -- where git still reports a collapsed directory --
	// the entry may be the directory holding the operand.
	return rel == entry ||
		strings.HasPrefix(entry, rel+"/") ||
		strings.HasPrefix(rel, entry+"/")
}
