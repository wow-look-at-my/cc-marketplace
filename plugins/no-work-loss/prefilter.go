package main

import "strings"

// mayDestroy is the cheap gate in front of everything expensive. It must never
// return false for a command this plugin would otherwise deny, so it matches on
// raw substrings rather than words: every reachable destructive form names git,
// one of the three file-removing utilities, or a truncating redirect.
//
// False positives are fine and expected -- "2>&1" trips the ">" needle. They
// cost one parse, and a parse alone never shells out to git.
func mayDestroy(command string) bool {
	for _, n := range prefilterNeedles {
		if strings.Contains(command, n) {
			return true
		}
	}
	return false
}

var prefilterNeedles = []string{"git", "rm", "mv", ">", "tee", "truncate"}

// destructiveKeyword reports whether raw text names something that can destroy
// work, and what to call it. Only consulted when the parser has already failed
// or the analysis panicked, so the answer decides between "deny on suspicion"
// and "let it through". Ordered most specific first so the reason names the
// most useful thing it found.
func destructiveKeyword(command string) (string, bool) {
	for _, m := range destructiveMarkers {
		if strings.Contains(command, m.needle) {
			return m.label, true
		}
	}
	return "", false
}

var destructiveMarkers = []struct{ needle, label string }{
	{"--hard", "git reset --hard"},
	{"--force", "a --force flag"},
	{"force-with-lease", "git push --force-with-lease"},
	{"checkout", "git checkout"},
	{"cherry-pick", "git cherry-pick"},
	{"truncate", "truncate"},
	{"restore", "git restore"},
	{"rebase", "git rebase"},
	{"switch", "git switch"},
	{"reset", "git reset"},
	{"stash", "git stash"},
	{"clean", "git clean"},
	{"merge", "git merge"},
	{"rm", "rm"},
	{"mv", "mv"},
}
