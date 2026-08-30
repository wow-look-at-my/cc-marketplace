package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The Write tool's own two refusals, which were a separate plugin until the
// delete-then-Write loophole made them one question with the Bash rules.
//
// Write authors a whole file. Aimed at a path that already holds something, it
// replaces content nobody reviewed the loss of -- Edit is the tool for that. And
// once that is refused, "delete the path, then Write it" is the obvious way
// round, so a path sitting in the recycle bin is refused too: in that window the
// only other copy of the file is in the model's context, where a compaction
// destroys it.

const recyclerTimeout = 3 * time.Second

// writeToolReason is the Write-specific half of editToolReason. Edit and
// NotebookEdit are not covered: they change a file that exists, which is the
// behaviour this rule is steering toward.
func writeToolReason(path string) string {
	if path == "" {
		return ""
	}
	if _, err := os.Lstat(path); err == nil {
		return "blocked: " + path + " already exists, and Write replaces the whole file. Use Edit to change it."
	}
	if original, ok := inRecycleBin(path); ok {
		return "blocked: " + path + " is not missing -- it is in the recycle bin, and Write would author a fresh file over the top of it.\n" +
			"run: recycler restore " + original + "   # then use Edit"
	}
	return ""
}

// inRecycleBin asks recycler, which already tracks each item's original
// location, so this keeps no ledger of its own. Every failure -- no recycler, an
// unreadable bin, no match -- falls through to allow: a guard that blocks a
// legitimate Write because a helper is missing is worse than no guard.
func inRecycleBin(path string) (string, bool) {
	out, err := recyclerList()
	if err != nil {
		return "", false
	}
	var items []struct {
		OriginalPath string `json:"original_path"`
	}
	if json.Unmarshal(out, &items) != nil {
		return "", false
	}
	// recycler records the physically resolved path, so on macOS /tmp/x arrives
	// as /private/tmp/x. The file itself does not exist, so its parent is what
	// can be resolved.
	physical := path
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		physical = filepath.Join(parent, filepath.Base(path))
	}
	for _, item := range items {
		if item.OriginalPath == path || item.OriginalPath == physical {
			return item.OriginalPath, true
		}
	}
	return "", false
}

func recyclerList() ([]byte, error) {
	if _, err := exec.LookPath("recycler"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), recyclerTimeout)
	defer cancel()
	var out, errb strings.Builder
	cmd := exec.CommandContext(ctx, "recycler", "list", "--json")
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}
