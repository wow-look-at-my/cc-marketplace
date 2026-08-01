// Command claude-md-budget keeps instruction files inside the size budget an
// agent session pays on every request.
//
// Claude Code loads every CLAUDE.md and every @-imported snippet VERBATIM into
// the prompt. Nothing truncates them: the only hard cap skips a file over 4 MiB
// entirely, and the "limit" the CLI mentions is a warning threshold that changes
// what gets sent not at all. So an oversized instruction file is billed, in
// full, on every request for the life of the session -- and the CLI's own
// warning renders only in the terminal UI, never on the web surface and never
// to the model. This closes that loop.
//
// It ships as a plugin, not as config, because config is installed once when a
// container is built: a session whose snapshot predates the guard never gets it
// and has no way to fetch it. Plugins are reinstalled from the marketplace every
// session, so this reaches an environment the config cannot.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The CLI's own floor for the same measurement. Deliberately pinned rather than
// recomputed per model: a budget that quintupled on a 1M-token model would
// defeat the point, which is keeping instruction files skimmable, not merely
// loadable.
const defaultBudget = 40000

const minBudget = 1000

// The room under the budget is a quota and is meant to be spent. What this
// catches is spending the LAST of it, so the next agent's first ordinary edit is
// the one that breaks. Hence a threshold where "no room left" is literally true
// (1,000 characters at the default budget), not a comfortable margin that would
// quietly make the top of the quota unusable.
const nearFraction = 0.975

// Hard-wrap width. An unwrapped file makes every edit a one-line diff no
// reviewer can read, and a paragraph running for thousands of columns is the
// SHAPE of an item that should have been a pointer to docs/.
const widthLimit = 150

// Sibling-checkout scan bound: /home/user is the repo in a single-repo session
// and the parent of every repo in a multi-repo one.
const maxSiblings = 32

// offender is one instruction file that is over budget, at the wall, or
// unwrapped. Wide is 1-based line numbers; a file can be well under budget and
// still be unreadable, so it is an independent offense rather than a tiebreak.
type offender struct {
	Path  string
	Chars int
	Wide  []int
}

// budget returns the character budget, honoring CC_CLAUDE_MD_BUDGET. Zero means
// the guard is disabled entirely.
func budget() int {
	raw := strings.TrimSpace(os.Getenv("CC_CLAUDE_MD_BUDGET"))
	if raw == "" {
		return defaultBudget
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultBudget
	}
	if n <= 0 {
		return 0
	}
	if n < minBudget {
		return minBudget
	}
	return n
}

// isInstructionFile reports whether a path is something this guard measures: a
// CLAUDE.md anywhere, or a snippet @-imported into one. Everything else a
// session writes is not instruction text and costs nothing per request.
func isInstructionFile(path string) bool {
	if filepath.Base(path) == "CLAUDE.md" {
		return true
	}
	return strings.HasSuffix(path, ".md") && filepath.Base(filepath.Dir(path)) == "claude_snippets"
}

// wideLines returns 1-based line numbers that could have been wrapped and were
// not. Code fences, tables, indented blocks and headings cannot be rewrapped
// without changing what they render as, and a line whose first widthLimit
// columns hold no space is a single unbreakable token (a URL).
func wideLines(text string) []int {
	var out []int
	fenced := false
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || utf8.RuneCountInString(line) <= widthLimit {
			continue
		}
		if strings.HasPrefix(line, "    ") {
			continue
		}
		if trimmed != "" && strings.ContainsRune("|>#", rune(trimmed[0])) {
			continue
		}
		// A prefix with no space in it cannot be broken.
		prefix := line
		if utf8.RuneCountInString(prefix) > widthLimit {
			prefix = string([]rune(prefix)[:widthLimit])
		}
		if !strings.Contains(prefix, " ") {
			continue
		}
		out = append(out, i+1)
	}
	return out
}

// measure reads a file once and returns both measurements. Characters, the way
// the CLI counts them -- not bytes, so a file with non-ASCII text measures
// smaller than wc -c reports.
func measure(path string) (chars int, wide []int, ok bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return 0, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, false
	}
	text := string(data)
	return utf8.RuneCountInString(text), wideLines(text), true
}

// candidates lists every file the session plausibly loaded as memory, in the
// order a reader would care about. Duplicates are removed by the caller.
func candidates(cwd string) []string {
	var out []string
	if home := os.Getenv("HOME"); home != "" {
		out = append(out, filepath.Join(home, ".claude", "CLAUDE.md"))
		// Snippets are @-imported and the CLI measures each import as its own
		// entry, so an oversized snippet is just as much a problem as an
		// oversized CLAUDE.md -- and just as fixable.
		snippets := filepath.Join(home, ".claude", "claude_snippets")
		if entries, err := os.ReadDir(snippets); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".md") {
					out = append(out, filepath.Join(snippets, e.Name()))
				}
			}
		}
	}

	out = append(out, filepath.Join(cwd, "CLAUDE.md"))
	if entries, err := os.ReadDir(cwd); err == nil {
		n := 0
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if n >= maxSiblings {
				break
			}
			n++
			out = append(out, filepath.Join(cwd, e.Name(), "CLAUDE.md"))
		}
	}
	return out
}

// nearLimit is the "no room left" floor.
func nearLimit(limit int) int {
	n := int(float64(limit) * nearFraction)
	if float64(n) < float64(limit)*nearFraction {
		n++
	}
	return n
}

// signature is the cheap half: size+mtime, enough to tell that a file changed
// without reading it. This is what lets the post-edit sweep watch FILES rather
// than the tool call.
func signature(path string) (string, bool) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return strconv.FormatInt(st.Size(), 10) + ":" + strconv.FormatInt(st.ModTime().UnixMilli(), 10), true
}

// growthOverHead reports how much the working tree's copy grew over the last
// committed one, or false when there is no git, no commit, or nothing to
// compare against.
func growthOverHead(path string, chars int) (int, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	top, err := exec.Command("git", "-C", filepath.Dir(abs), "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return 0, false
	}
	root := strings.TrimSpace(string(top))
	if root == "" || !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return 0, false
	}
	rel := abs[len(root)+1:]
	before, err := exec.Command("git", "-C", root, "show", "HEAD:"+rel).Output()
	if err != nil {
		return 0, false
	}
	return chars - utf8.RuneCountInString(string(before)), true
}

// comma formats n with thousands separators, matching how the numbers read in
// the reports.
func comma(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
