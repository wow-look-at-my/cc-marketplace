// linkify.go turns a reference into the URL a reader can open.
//
// The rule it serves is not "put a link on everything". A link is a demand to
// stop reading and move your hand, and the reader pays that cost before they
// know whether it was worth paying. So a reference whose target cannot be shown
// to exist is left as plain text. Silence is the correct answer there, never a
// guess at a URL.
//
// Everything here runs in the render path, so each git call is bounded and only
// made when a token actually needs it.
package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// gitTimeout bounds one git call. This code runs while a message streams, so a
// repository that cannot answer promptly gets no link rather than a stall.
const gitTimeout = 300 * time.Millisecond

// Repo is the owner/repo a bare reference resolves against.
type Repo struct {
	Owner string
	Name  string
}

func (r Repo) valid() bool { return r.Owner != "" && r.Name != "" }

func (r Repo) url() string { return "https://github.com/" + r.Owner + "/" + r.Name }

// Resolver answers the questions a rewrite needs about the checkout it is
// running in. It is an interface so the tests never shell out to git.
type Resolver interface {
	// Repo is the origin remote's owner and name, if there is one.
	Repo() (Repo, bool)
	// BranchExists reports whether the branch is on the origin remote. A branch
	// that was never pushed has no page to open.
	BranchExists(branch string) bool
	// CommitExists reports whether the object is in this repository.
	CommitExists(sha string) bool
	// DefaultBranch is the base a compare URL is taken against.
	DefaultBranch() string
	// PullState reports what a pull request is doing, which decides the dot
	// beside it. StateUnknown covers every question that could not be
	// answered, and renders no dot at all: an unknown state must never show as
	// green.
	PullState(repo Repo, number string) PullState
}

// PullState is what a pull request is doing right now. The reader sees it as one
// character beside the reference, so they can tell whether the page is worth
// opening without opening it.
type PullState int

const (
	// StateUnknown is every question that could not be answered: no `gh`, no
	// network, a timeout, a number that names an issue rather than a pull
	// request. It renders no dot, and the reference is linked as it always was.
	StateUnknown PullState = iota
	StateMerged
	StateClosed
	StateFailing
	StateConflicted
	StatePending
	StateMergeable
)

// dot is the character rendered beside the reference.
//
// The first four colours are the owner's own mapping. StateClosed is not in
// that list: a pull request that closed without merging is finished the same way
// a merged one is, so it is rendered black, which reads as abandoned and
// collides with none of the five. That choice is this file's, not the owner's.
func dot(s PullState) string {
	switch s {
	case StateMerged:
		return "🟣"
	case StateClosed:
		return "⚫"
	case StateFailing:
		return "🔴"
	case StateConflicted:
		return "🟠"
	case StatePending:
		return "🟡"
	case StateMergeable:
		return "🟢"
	}
	return ""
}

// finished reports the states where the page has nothing left to do.
//
// A link is a demand: stop reading, move your hand, click this. The reader pays
// that cost before knowing whether it was worth paying, and on a merged pull
// request they pay it to reach a page somebody closed hours ago. An owner called
// that a prank, in those words.
//
// So for these states the words stay plain and the DOT carries the link. The
// dot already announces that the page is finished, so the demand shrinks to one
// character that tells the reader why they probably do not want it -- and the
// route to the page still exists for anyone who does.
func finished(s PullState) bool { return s == StateMerged || s == StateClosed }

// pullState asks about the reference behind one match, and answers StateUnknown
// for every kind that is not a pull request.
func pullState(ref Ref, res Resolver) PullState {
	var repo Repo
	var number string
	switch ref.Kind {
	case "an issue or pull request number":
		owner, name, n := splitNumber(ref.Text)
		repo, number = Repo{Owner: owner, Name: name}, n
	case "a bare GitHub URL":
		r, n, ok := IssueRef(ref.Text)
		if !ok {
			return StateUnknown
		}
		repo, number = r, n
	default:
		return StateUnknown
	}
	if !repo.valid() || number == "" {
		return StateUnknown
	}
	return res.PullState(repo, number)
}

// Linkify returns the markdown link for one reference, and false when the
// reference must be left as it was written.
func Linkify(ref Ref, res Resolver) (string, bool) {
	url, ok := refURL(ref, res)
	if !ok {
		return "", false
	}
	text := ref.Text
	if ref.Backticked {
		// The caller's Located range already swallowed the original backticks,
		// so putting them back here (inside the brackets) is what keeps the code
		// span and the link the same span, instead of one nested in the other.
		text = "`" + text + "`"
	}

	state := pullState(ref, res)
	switch {
	case state == StateUnknown:
		return "[" + text + "](" + url + ")", true
	case finished(state):
		// The words go back to plain text and the dot carries the link.
		return "[" + dot(state) + "](" + url + ") " + text, true
	default:
		return dot(state) + " [" + text + "](" + url + ")", true
	}
}

func refURL(ref Ref, res Resolver) (string, bool) {
	switch ref.Kind {
	case "a bare GitHub URL":
		// Already a URL, and already proven to exist by whoever wrote it. This
		// is the one kind that needs no repository and no probe.
		return ref.Text, true

	case "an issue or pull request number":
		owner, name, number := splitNumber(ref.Text)
		repo := Repo{Owner: owner, Name: name}
		// A BARE #N is never linked. Every other kind can be checked before it
		// is rendered -- a branch against refs/remotes/origin, a commit against
		// the object database, a slug carries its own repository, a URL is
		// self-evidently real. A bare #N cannot be, and the repository it would
		// be resolved against is a guess: a session with eleven checkouts has
		// one working directory. It is also the shape an ordinary numbered list
		// uses, so "#7" in a status message is usually not a reference at all.
		//
		// Guessing there does not produce a dead link, which the reader would
		// notice. It produces a link to a real, unrelated issue, which they
		// would not.
		if !repo.valid() || number == "" {
			return "", false
		}
		// /issues/N, never /pull/N: GitHub redirects an issue number to the
		// pull request when it is one, and /pull/N on a plain issue is a 404.
		// One spelling is right for both, and nothing here knows which it is.
		return repo.url() + "/issues/" + number, true

	case "a commit SHA":
		repo, ok := res.Repo()
		if !ok || !res.CommitExists(ref.Text) {
			return "", false
		}
		return repo.url() + "/commit/" + ref.Text, true

	case "a branch":
		repo, ok := res.Repo()
		if !ok || !res.BranchExists(ref.Text) {
			return "", false
		}
		base := res.DefaultBranch()
		if base == "" {
			return "", false
		}
		return repo.url() + "/compare/" + base + "..." + ref.Text + "?expand=1", true
	}
	return "", false
}

// splitNumber breaks `owner/repo#42` or `#42` into its parts.
func splitNumber(token string) (owner, name, number string) {
	hash := strings.IndexByte(token, '#')
	if hash < 0 {
		return "", "", ""
	}
	number = token[hash+1:]
	if slug := token[:hash]; slug != "" {
		if slash := strings.IndexByte(slug, '/'); slash > 0 && slash < len(slug)-1 {
			owner, name = slug[:slash], slug[slash+1:]
		}
	}
	return owner, name, number
}

// GitResolver answers from the checkout the session is standing in. Every answer
// is memoized: this runs once per flush, and a message with several references
// would otherwise ask git the same question repeatedly.
type GitResolver struct {
	Dir string

	once      sync.Once
	repo      Repo
	repoFound bool
	base      string

	mu    sync.Mutex
	cache map[string]bool

	// A pull request's state is not a yes or no, so it cannot share the memo
	// above. StateUnknown is memoized like any other answer: a `gh` that
	// cannot answer once will not answer differently in the same message.
	prMu   sync.Mutex
	prSeen map[string]PullState
}

func (g *GitResolver) git(args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func (g *GitResolver) load() {
	g.once.Do(func() {
		if url, ok := g.git("remote", "get-url", "origin"); ok {
			g.repo, g.repoFound = parseRemote(url)
		}
		// origin/HEAD names the default branch, when the clone recorded one.
		if head, ok := g.git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ok {
			g.base = strings.TrimPrefix(head, "origin/")
		}
	})
}

func (g *GitResolver) Repo() (Repo, bool) { g.load(); return g.repo, g.repoFound }

func (g *GitResolver) DefaultBranch() string { g.load(); return g.base }

func (g *GitResolver) memo(key string, probe func() bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cache == nil {
		g.cache = map[string]bool{}
	}
	if v, ok := g.cache[key]; ok {
		return v
	}
	v := probe()
	g.cache[key] = v
	return v
}

// BranchExists asks for the remote-tracking ref, not the local branch. A branch
// that exists only locally has no compare page: GitHub cannot show a diff
// against a ref it has never received.
func (g *GitResolver) BranchExists(branch string) bool {
	return g.memo("branch:"+branch, func() bool {
		_, ok := g.git("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
		return ok
	})
}

func (g *GitResolver) CommitExists(sha string) bool {
	return g.memo("commit:"+sha, func() bool {
		_, ok := g.git("cat-file", "-e", sha+"^{commit}")
		return ok
	})
}

// ghTimeout bounds the one call that leaves the machine. It is longer than
// gitTimeout because a remote answer is not a local one, and short enough that a
// slow answer costs the reader a pause rather than the message.
const ghTimeout = 2 * time.Second

// statusJQ flattens the rollup gh returns into the few words the decision needs.
// The array holds two shapes: a CheckRun carries `status` and `conclusion`, and
// a StatusContext carries `state`. Reducing both to one word here keeps the
// parsing on this side trivial.
const statusJQ = `{state:.state,mergeable:.mergeable,mergeState:.mergeStateStatus,` +
	`checks:[.statusCheckRollup[]?|if .__typename=="CheckRun" then ` +
	`(if .status!="COMPLETED" then "PENDING" else (.conclusion//"") end) else (.state//"") end]}`

// pullView is statusJQ's output.
type pullView struct {
	State      string   `json:"state"`
	Mergeable  string   `json:"mergeable"`
	MergeState string   `json:"mergeState"`
	Checks     []string `json:"checks"`
}

// PullState asks GitHub what a pull request is doing. It is memoized per
// reference, because one message names the same pull request several times and
// this runs while the message streams.
//
// Every failure answers StateUnknown: no `gh`, no network, a timeout, or a
// number that names an issue rather than a pull request, which `gh pr view`
// refuses outright. Unknown renders no dot, so a lookup that could not answer
// can never show as green.
//
// The call rides github-state-mirror through `gh`, which reads GH_HOST from the
// session env. Never name a host here, and never reach api.github.com: that
// spends real API quota for nothing. `--json` is served by GraphQL, so this
// posts to https://$GH_HOST/api/graphql -- verified by pointing GH_HOST at a
// host that does not exist and watching the call fail there.
func (g *GitResolver) PullState(repo Repo, number string) PullState {
	if !repo.valid() || number == "" {
		return StateUnknown
	}
	key := repo.Owner + "/" + repo.Name + "#" + number

	g.prMu.Lock()
	defer g.prMu.Unlock()
	if g.prSeen == nil {
		g.prSeen = map[string]PullState{}
	}
	if s, ok := g.prSeen[key]; ok {
		return s
	}

	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "pr", "view", number,
		"-R", repo.Owner+"/"+repo.Name,
		"--json", "state,mergeable,mergeStateStatus,statusCheckRollup",
		"--jq", statusJQ).Output()

	state := StateUnknown
	if err == nil {
		var v pullView
		if json.Unmarshal(out, &v) == nil {
			state = classify(v)
		}
	}
	g.prSeen[key] = state
	return state
}

// classify turns one pull request's fields into the dot it earns.
//
// Order is severity, not the owner's list order: a merged or closed pull request
// is finished whatever its checks say, a failing check is a defect in the code,
// a conflict is a merge-time chore, and a run still going is not yet news. Green
// is what is left, which is also the answer for a pull request with no checks at
// all.
func classify(v pullView) PullState {
	switch v.State {
	case "MERGED":
		return StateMerged
	case "CLOSED":
		return StateClosed
	}
	if slices.ContainsFunc(v.Checks, checkFailed) {
		return StateFailing
	}
	if v.Mergeable == "CONFLICTING" || v.MergeState == "DIRTY" {
		return StateConflicted
	}
	if slices.Contains(v.Checks, "PENDING") || slices.Contains(v.Checks, "EXPECTED") {
		return StatePending
	}
	return StateMergeable
}

// checkFailed reports the conclusions that mean a check did not pass. SUCCESS,
// SKIPPED and NEUTRAL are not among them: a skipped job is a job that correctly
// did not need to run, and colouring it red would make every message red.
func checkFailed(conclusion string) bool {
	switch conclusion {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return true
	}
	return false
}

// remoteRe pulls owner and repo out of every spelling of a GitHub remote:
// https, ssh, scp-style, with or without a .git suffix, and with or without
// embedded credentials.
var remoteRe = regexp.MustCompile(`github\.com[:/]+([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// parseRemote reads owner/repo off a remote URL. Credentials in the URL are
// discarded rather than carried into a rendered link: the pattern takes only
// the two path segments after the host, so a token in the userinfo cannot reach
// the user's screen.
func parseRemote(url string) (Repo, bool) {
	m := remoteRe.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return Repo{}, false
	}
	return Repo{Owner: m[1], Name: m[2]}, true
}
