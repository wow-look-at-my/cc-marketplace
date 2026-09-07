// subject.go answers two questions about a tool call: is it a status read,
// and what is it a status read ABOUT.
//
// The subject is what makes this half work where the Stop half's signature
// comparison does not. A session that asks the same question four different
// ways -- `gh wait-ci`, then `gh wait-ci checks`, then `pull_request_read`,
// then `list_commits` -- produces four different signatures and no streak,
// while every call asks after the same pull request and learns the same
// nothing. Keying on the subject rather than the command text is what stops
// a re-spelling from laundering a repeat.
package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"regexp"
	"sort"
	"strings"
)

// statusReadTools are the non-Bash tools whose entire product is "what is
// the state of X right now". A tool that CHANGES something is never here.
var statusReadTools = set.Of(
	"pull_request_read",
	"list_pull_requests",
	"search_pull_requests",
	"get_commit",
	"list_commits",
	"list_branches",
	"get_session",
	"list_sessions",
	"list_triggers",
	"readnotifications",
	"taskget",
	"taskoutput",
	"tasklist",
)

// statusReadCommands are the Bash spellings of the same question. Each is
// matched in statement position, so `grep 'gh pr view' notes.md` is not one.
//
// Every entry runs `gh`, and no entry may ever run `git`. A `git` command
// reads local objects: `git show <sha>:src/cmd/go.mod`, `git ls-tree <sha>`
// and `git log <sha>` cost nothing, reach no network, and answer a question
// about a file rather than about a state. All three were refused while this
// list carried `git ls-remote`, because a SHA anywhere in the command text was
// enough to make the call a status read of that commit.
var statusReadCommands = []string{
	"gh wait-ci",
	"gh pr view",
	"gh pr checks",
	"gh pr status",
	"gh pr list",
	"gh run view",
	"gh run list",
	"gh run watch",
}

// contentSubcommands read a run's OUTPUT rather than its state: a log, a
// search of one, the errors GitHub extracted, the artifacts, the job list.
//
// None of them is the poll this guard exists to stop. A finished run's log
// does not change, and reading one is not asking the question again -- it is
// how the failure gets diagnosed. Counting them refused the second of two red
// jobs on one commit as a repeat of the first, which is the guard standing
// between a session and the fix it was told to make.
var contentSubcommands = set.Of(
	"log", "grep", "annotations", "artifacts", "jobs", "workflows",
)

// afterWaitCI is the subcommand word following `gh wait-ci`, lowercased, or
// "" when the command names none.
func afterWaitCI(lower string, at int) string {
	rest := strings.TrimSpace(lower[at+len("gh wait-ci"):])
	for _, f := range strings.Fields(rest) {
		if strings.HasPrefix(f, "-") {
			continue
		}
		return f
	}
	return ""
}

var (
	reSlugNum   = regexp.MustCompile(`([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#(\d+)`)
	reOwner     = regexp.MustCompile(`"owner"\s*:\s*"([^"]+)"`)
	reRepo      = regexp.MustCompile(`"repo"\s*:\s*"([^"]+)"`)
	rePullNum   = regexp.MustCompile(`"pull(?:_?[Nn]umber|Request(?:_?[Nn]umber)?)"\s*:\s*(\d+)`)
	reRepoFlag  = regexp.MustCompile(`--repo[= ]([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)`)
	rePRCommand = regexp.MustCompile(`\bgh\s+pr\s+(?:view|checks|status)\s+(\d+)`)
	reSHA       = regexp.MustCompile(`\b([0-9a-f]{7,40})\b`)
	reHasDigit  = regexp.MustCompile(`[0-9]`)
	reHasHexAZ  = regexp.MustCompile(`[a-f]`)
	reStatement = regexp.MustCompile(`(^|[|;&(]\s*|&&\s*|\|\|\s*)$`)
)

// isStatusRead reports whether this call's only product is a current-state
// answer. Anything that writes, pushes, edits or runs a build is not one --
// those calls CHANGE the world, and re-reading after one is legitimate.
func isStatusRead(c toolCall) bool {
	name := strings.ToLower(c.name)
	if name == "bash" {
		return namesAStatusCommand(commandOf(c.input))
	}
	// An MCP tool arrives as mcp__<server>__<Tool>; the bare trailing name
	// is what identifies it, since the same tool is served under several
	// server prefixes in this environment.
	if i := strings.LastIndex(name, "__"); i >= 0 {
		name = name[i+2:]
	}
	return statusReadTools.Contains(name)
}

// namesAStatusCommand reports whether cmd runs one of the status commands in
// statement position. Requiring statement position is what keeps a command
// that merely MENTIONS one -- a grep, a commit message -- from counting.
func namesAStatusCommand(cmd string) bool {
	return len(statusStatements(cmd)) > 0
}

// statusStatements returns the text of each status command cmd runs, from its
// command word to the end of that statement, and nothing else.
//
// The bound is what makes the subject the command's own. Reading a subject out
// of the whole command string let a SHA that belonged to a neighbouring
// statement -- or to no command at all -- decide what a `gh` call was asking
// about, so the first genuine read of that commit came back refused as a
// repeat. A subject now has to sit in the arguments of the GitHub read itself.
func statusStatements(cmd string) []string {
	lower := strings.ToLower(cmd)
	var out []string
	for _, want := range statusReadCommands {
		for i := 0; ; {
			j := strings.Index(lower[i:], want)
			if j < 0 {
				break
			}
			at := i + j
			i = at + len(want)
			if !reStatement.MatchString(lower[:at]) {
				continue
			}
			if want == "gh wait-ci" && contentSubcommands.Contains(afterWaitCI(lower, at)) {
				continue
			}
			out = append(out, cmd[at:at+statementLen(cmd[at:])])
		}
	}
	return out
}

// statementLen is how far a statement runs from s[0]. Anything that starts a
// new command ends it, which is enough here: the subject regexes read flags
// and operands, and a quoted separator inside one of those widens the span by
// a few harmless characters rather than swallowing a neighbouring command.
func statementLen(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ';', '\n', '|', '&', ')', '`':
			return i
		}
	}
	return len(s)
}

// reCommand is hoisted because the ledger walk reads every Bash call in the
// window, and compiling this per call would pay for the pattern each time.
var reCommand = regexp.MustCompile(`"command"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// commandOf pulls the command string out of a Bash call's input, undone to
// the text the shell would have run.
//
// It shares the record walk's replacer rather than keeping a shorter one of
// its own. A Go encoder writes `&&` as `&&` and a JavaScript one
// leaves it alone, so a private replacer that folded only the backslash
// escapes read `a && b` as one long statement on half the transcripts it may
// be handed -- and a SHA in the second statement then named the subject of the
// first.
func commandOf(input []byte) string {
	m := reCommand.FindSubmatch(input)
	if m == nil {
		return ""
	}
	return unescapeReplacer.Replace(string(m[1]))
}

// subjectsIn returns every state-carrying thing named in text, normalized so
// the same pull request reached by two different tools compares equal. It is
// used on both sides of the decision -- the call being judged, and the
// results already in the transcript -- so a subject that cannot be spelled
// consistently simply never matches, which allows rather than denies.
func subjectsIn(text string) []string {
	seen := set.New[string]()

	for _, m := range reSlugNum.FindAllStringSubmatch(text, -1) {
		seen.Add("pr:" + strings.ToLower(m[1]) + "#" + m[2])
	}

	owner := firstGroup(reOwner, text)
	repo := firstGroup(reRepo, text)
	if num := firstGroup(rePullNum, text); num != "" {
		if owner != "" && repo != "" {
			seen.Add("pr:" + strings.ToLower(owner+"/"+repo) + "#" + num)
		} else {
			seen.Add("pr:#" + num)
		}
	}

	if m := rePRCommand.FindStringSubmatch(text); m != nil {
		if slug := firstGroup(reRepoFlag, text); slug != "" {
			seen.Add("pr:" + strings.ToLower(slug) + "#" + m[1])
		} else {
			seen.Add("pr:#" + m[1])
		}
	}

	for _, m := range reSHA.FindAllStringSubmatch(text, -1) {
		if looksLikeSHA(m[1]) {
			seen.Add("sha:" + m[1][:7])
		}
	}

	out := make([]string, 0, seen.Len())
	for s := range seen.All() {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// looksLikeSHA separates a commit hash from a decimal number and from a word
// spelled in hex. The rule is the sibling link-all-refs plugin's: a real SHA
// carries both a digit and an a-f letter.
func looksLikeSHA(s string) bool {
	return reHasDigit.MatchString(s) && reHasHexAZ.MatchString(s)
}

func firstGroup(re *regexp.Regexp, text string) string {
	if m := re.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// prSubjects filters to pull-request subjects, which are the only ones a
// merge verdict can be attributed to.
func prSubjects(subs []string) []string {
	var out []string
	for _, s := range subs {
		if strings.HasPrefix(s, "pr:") {
			out = append(out, s)
		}
	}
	return out
}
