// relocate.go is why this plugin refuses without destroying anything.
//
// A deny that says "delete that sentence" gets argued with, because the
// sentence is usually TRUE and the model knows it. The history is real; it is
// only in the wrong file. So the refused text is appended to .git/TOMBSTONES
// first, and the refusal says where it went. The commit message is where a
// change gets narrated, and draining that file into the commit body is a step
// the author takes rather than a fact this plugin invents.
package main

import (
	"os"
	"path/filepath"
	"strings"
)

// ledger is the file the refused text lands in, under the working tree's git
// directory: untracked by construction, and thrown away with the clone.
const ledger = "TOMBSTONES"

// Relocate appends the refused lines to the ledger and returns its path, or ""
// when it could not be written. A failure here never changes the verdict: the
// deny stands on its own, and a missing ledger only costs the author a
// copy-paste.
func Relocate(path string, hits []Hit) string {
	root := RepoRoot(path)
	if root == "" {
		return ""
	}
	dir := filepath.Join(root, ".git")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "" // a worktree's .git is a file; nothing safe to append to
	}
	dest := filepath.Join(dir, ledger)

	var b strings.Builder
	b.WriteString("# " + path + "\n")
	seen := map[string]bool{}
	for _, h := range hits {
		if h.Line == "" || seen[h.Line] {
			continue
		}
		seen[h.Line] = true
		b.WriteString(h.Line + "\n")
	}
	b.WriteString("\n")

	f, err := os.OpenFile(dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return ""
	}
	return dest
}
