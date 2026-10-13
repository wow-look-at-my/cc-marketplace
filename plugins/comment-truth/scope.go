package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// SCOPE is the whole efficiency story. A repository holds thousands of
// comments; a session writes a handful. Everything downstream -- the name
// resolution, the doc reads, the model call -- runs only over comment lines
// this session ADDED or CHANGED, so cost tracks the size of the change and not
// the size of the repo.
//
// The diff is taken against the merge-base with the default branch rather than
// HEAD, so a session that has already committed is still checked. A session
// that wrote no comments does no work at all.

// Block is one comment, with what it is attached to.
type Block struct {
	File string
	// Line is the 1-based line of the block's first line.
	Line int
	// Text is the comment's prose, markers stripped.
	Text string
	// Raw keeps the source lines for reporting.
	Raw []string
	// Code is the few lines that follow the block -- the thing the comment is
	// ABOUT. Evidence for "is this comment even about what it sits on".
	Code string
}

// changedFiles lists files with added/changed lines in the session's range,
// filtered to those that can carry comments.
func (r *Repo) changedFiles() []string {
	out, err := r.git("diff", "--name-only", "--diff-filter=d", r.Base+"...HEAD")
	if err != nil {
		return nil
	}
	// Uncommitted work counts too: a Stop hook fires before a commit as often
	// as after one. UNTRACKED files count as well -- a file the session just
	// created is in no diff at all, and skipping it would leave exactly the
	// newest code unchecked.
	dirty, _ := r.git("diff", "--name-only", "--diff-filter=d", "HEAD")
	staged, _ := r.git("diff", "--name-only", "--cached", "--diff-filter=d")
	untracked, _ := r.git("ls-files", "--others", "--exclude-standard")

	seen := map[string]bool{}
	var files []string
	for _, chunk := range []string{out, dirty, staged, untracked} {
		for _, f := range strings.Split(chunk, "\n") {
			f = strings.TrimSpace(f)
			if f == "" || seen[f] || !commentable(f) {
				continue
			}
			seen[f] = true
			files = append(files, f)
		}
	}
	return files
}

// addedLines returns the 1-based line numbers this session added or changed in
// one file, read from the unified diff's hunk headers and +lines.
func (r *Repo) addedLines(file string) map[int]bool {
	added := map[int]bool{}
	// A file git has never seen has no diff to read: every line of it is new.
	if r.untracked(file) {
		src, err := os.ReadFile(filepath.Join(r.Root, file))
		if err != nil {
			return added
		}
		for i := range strings.Split(string(src), "\n") {
			added[i+1] = true
		}
		return added
	}
	for _, args := range [][]string{
		{"diff", "-U0", r.Base + "...HEAD", "--", file},
		{"diff", "-U0", "HEAD", "--", file},
		{"diff", "-U0", "--cached", "--", file},
	} {
		out, err := r.git(args...)
		if err != nil {
			continue
		}
		line := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "@@") {
				line = parseHunkStart(l)
				continue
			}
			switch {
			case strings.HasPrefix(l, "+++"):
				// The file header, not an added line.
			case strings.HasPrefix(l, "+"):
				if line > 0 {
					added[line] = true
					line++
				}
			case strings.HasPrefix(l, "-"), strings.HasPrefix(l, "\\"):
				// Removals and "\ No newline" do not advance the new-file
				// cursor.
			}
		}
	}
	return added
}

// untracked reports whether git has no index entry for a path.
func (r *Repo) untracked(file string) bool {
	out, err := r.git("ls-files", "--error-unmatch", "--", file)
	return err != nil || strings.TrimSpace(out) == ""
}

// parseHunkStart reads the new-file start line out of "@@ -a,b +c,d @@".
func parseHunkStart(hunk string) int {
	plus := strings.Index(hunk, "+")
	if plus < 0 {
		return 0
	}
	rest := hunk[plus+1:]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}

// commentable reports whether a path is source this tool knows how to read
// comments out of. Everything else -- lockfiles, fixtures, binaries -- is
// skipped before it costs anything.
func commentable(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".c", ".h", ".cc", ".cpp",
		".java", ".rs", ".swift", ".kt", ".scala", ".cs", ".php", ".wgsl", ".glsl":
		return true
	case ".py", ".rb", ".sh", ".bash", ".yml", ".yaml", ".toml":
		return true
	}
	return false
}

// Repo is the checkout under inspection.
type Repo struct {
	Root string
	// Base is the commit the session's work is measured against.
	Base string
}

func (r *Repo) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Root}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

// openRepo resolves the checkout and the commit to diff against. A repo with
// no discoverable base (a shallow clone, a fresh init) still works: the base
// falls back to HEAD, which scopes the check to uncommitted work.
func openRepo(dir string) (*Repo, bool) {
	r := &Repo{Root: dir}
	top, err := r.git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, false
	}
	r.Root = strings.TrimSpace(top)

	for _, ref := range []string{"origin/master", "origin/main", "master", "main"} {
		if mb, err := r.git("merge-base", ref, "HEAD"); err == nil {
			if s := strings.TrimSpace(mb); s != "" {
				r.Base = s
				return r, true
			}
		}
	}
	r.Base = "HEAD"
	return r, true
}
