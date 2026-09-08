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
	"os/exec"
	"regexp"
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
	// Settled reports whether a pull request or issue has nothing left to do:
	// merged, or closed. The second return is false when the state is not
	// known, and unknown must leave the reference exactly as it was written.
	// Stripping on a failed lookup turns a network blip into lost information.
	Settled(repo Repo, number string) (settled bool, known bool)
}

// settled reports whether a reference names a pull request or issue that is
// already merged or closed.
//
// A link is a demand: stop reading, move your hand, click this. The reader pays
// that cost before knowing whether it was worth paying. A merged or closed pull
// request has nothing left to do, so the link spends that attention on a page
// they closed hours ago. An owner called it a prank, in those words.
//
// This is the same call the rest of this file already makes for a branch and a
// commit. Where those ask whether the target exists, this asks whether it is
// still worth opening. A false answer here means leave the text alone, which
// covers both "still open" and "could not find out".
func settled(repo Repo, number string, res Resolver) bool {
	if !repo.valid() || number == "" {
		return false
	}
	done, known := res.Settled(repo, number)
	return known && done
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
	return "[" + text + "](" + url + ")", true
}

func refURL(ref Ref, res Resolver) (string, bool) {
	switch ref.Kind {
	case "a bare GitHub URL":
		// Already a URL, and already proven to exist by whoever wrote it, so
		// this kind needs no repository and no existence probe. It still needs
		// the liveness one: a bare URL to a merged pull request stays plain
		// text rather than becoming something to click.
		if repo, number, ok := IssueRef(ref.Text); ok && settled(repo, number, res) {
			return "", false
		}
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
		// A slug that names something already merged or closed gets no link
		// either. Adding one and stripping one the model wrote are the same
		// rule, so they ask the same question in the same place.
		if settled(repo, number, res) {
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

	// Liveness is answered from disk rather than from git, so it gets its own
	// memo. See prstate.go for why it never reaches the network from here.
	prMu   sync.Mutex
	prSeen map[string]prAnswer
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
