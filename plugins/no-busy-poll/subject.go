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
var statusReadCommands = []string{
	"gh wait-ci",
	"gh pr view",
	"gh pr checks",
	"gh pr status",
	"gh pr list",
	"gh run view",
	"gh run list",
	"gh run watch",
	"git ls-remote",
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
	lower := strings.ToLower(cmd)
	for _, want := range statusReadCommands {
		for i := 0; ; {
			j := strings.Index(lower[i:], want)
			if j < 0 {
				break
			}
			at := i + j
			if reStatement.MatchString(lower[:at]) {
				return true
			}
			i = at + len(want)
		}
	}
	return false
}

// reCommand is hoisted because the ledger walk reads every Bash call in the
// window, and compiling this per call would pay for the pattern each time.
var reCommand = regexp.MustCompile(`"command"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// commandOf pulls the command string out of a Bash call's input.
func commandOf(input []byte) string {
	m := reCommand.FindSubmatch(input)
	if m == nil {
		return ""
	}
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n").Replace(string(m[1]))
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
