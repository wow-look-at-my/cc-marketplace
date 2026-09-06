package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// protectedRefPrefix names a ref this hook created to hold content a
// destructive command was about to lose. It is the ONLY place that content
// survives, so a command that would delete or overwrite it must never be
// judged safe on the "it exists somewhere else" test the other ref-destroying
// verbs use -- see gitverb.go's checks against this prefix.
const protectedRefPrefix = "refs/no-work-loss/"

var preserveRefSeq uint64

// preserveResult is what a successful commit produced, for the notice the
// caller shows once the destructive command is allowed to proceed.
type preserveResult struct {
	ref     string
	commit  string
	pushed  bool
	pushErr string
}

// preserveAtRiskPaths satisfies the guard's invariant directly instead of
// refusing: it commits the exact paths a destructive command would destroy
// into a dedicated ref, so the command is safe by construction once the
// commit exists. It never touches the user's own index or working tree --
// every step below runs against a throwaway GIT_INDEX_FILE, and nothing here
// runs `git add` or `git commit` against the repository's real index.
//
// ok is false only when the commit itself could not be made. Preservation
// that did not happen must never read as success, so the caller falls back
// to the ordinary denial in that case.
func preserveAtRiskPaths(root string, paths []string) (res *preserveResult, ok bool) {
	if root == "" || len(paths) == 0 {
		return nil, false
	}

	tmp, err := os.CreateTemp("", "no-work-loss-index-*")
	if err != nil {
		return nil, false
	}
	tmpIndex := tmp.Name()
	tmp.Close()
	// A 0-byte file is not an empty index -- git reads its header and refuses
	// it ("index file smaller than expected"). Removing it leaves the path
	// merely reserved: git treats a GIT_INDEX_FILE that does not exist yet as
	// starting from a genuinely empty index, which is what a repository with
	// no HEAD to read-tree from needs.
	os.Remove(tmpIndex)
	defer os.Remove(tmpIndex)
	env := []string{"GIT_INDEX_FILE=" + tmpIndex}

	// read-tree HEAD seeds the temp index with the last committed tree, so the
	// commit below carries the CURRENT content of the at-risk paths and HEAD's
	// content for everything else. A repository with no commits yet has no
	// HEAD to seed from; the index starts empty and the commit gets no parent.
	hasHead := true
	if _, _, err := runGitEnvTimeout(root, gitTimeout, env, "read-tree", "HEAD"); err != nil {
		hasHead = false
	}

	// --force: an ignored file (clean -fdx) is exactly the case `git add`
	// otherwise refuses to stage, and a tracked or plain untracked path is
	// unaffected by the flag.
	addArgs := append([]string{"add", "--force", "--"}, paths...)
	if _, _, err := runGitEnvTimeout(root, gitTimeout, env, addArgs...); err != nil {
		return nil, false
	}

	treeOut, _, err := runGitEnvTimeout(root, gitTimeout, env, "write-tree")
	if err != nil {
		return nil, false
	}
	tree := strings.TrimSpace(treeOut)
	if tree == "" {
		return nil, false
	}

	commitArgs := []string{"commit-tree", tree, "-m", preserveMessage(paths)}
	if hasHead {
		commitArgs = append(commitArgs, "-p", "HEAD")
	}
	commitOut, _, err := runGit(root, commitArgs...)
	if err != nil {
		return nil, false
	}
	commit := strings.TrimSpace(commitOut)
	if commit == "" {
		return nil, false
	}

	ref := protectedRefPrefix + preserveRefName()
	if _, _, err := runGit(root, "update-ref", ref, commit); err != nil {
		// The commit object exists but nothing names it, so git gc can reap
		// it. That is not durable preservation, so this must not read as one.
		return nil, false
	}

	res = &preserveResult{ref: ref, commit: commit}
	if _, stderr, err := runGitEnvTimeout(root, preservePushTimeout, nil, "push", "origin", commit+":"+ref); err != nil {
		res.pushErr = strings.TrimSpace(stderr)
		if res.pushErr == "" {
			res.pushErr = err.Error()
		}
	} else {
		res.pushed = true
	}
	return res, true
}

func preserveMessage(paths []string) string {
	return fmt.Sprintf("no-work-loss: preserved %d path(s) before a destructive command\n\n%s",
		len(paths), strings.Join(paths, "\n"))
}

// preserveRefName is nanosecond-timestamped and counter-suffixed: two
// preservations in one invocation, or two invocations racing a coarse system
// clock, must never collide and silently overwrite one another's ref.
func preserveRefName() string {
	ts := time.Now().UTC().Format("20060102T150405.000000000")
	n := atomic.AddUint64(&preserveRefSeq, 1)
	return ts + "." + strconv.FormatUint(n, 10)
}

// notice reports the preservation once, so a session never learns about it
// only by noticing a strange ref later. label is the finding's own name for
// the command that would have destroyed the content; summary is the same
// tracked/untracked/ignored breakdown a denial would have shown.
func (r *preserveResult) notice(label, summary string) string {
	if r.pushed {
		return fmt.Sprintf("preserved: %s would have lost %s, so it was committed to %s (%s) and pushed to origin before being allowed to proceed.",
			label, summary, r.ref, shortSHA(r.commit))
	}
	return fmt.Sprintf("preserved: %s would have lost %s, so it was committed to %s (%s) before being allowed to proceed. The push to origin failed (%s) -- the content is safe in that local ref; push it yourself when you can.",
		label, summary, r.ref, shortSHA(r.commit), r.pushErr)
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// isProtectedRef reports whether ref names a preservation ref this hook
// created. Deleting or force-overwriting one is refused unconditionally --
// unlike an ordinary branch or tag, it has no "somewhere else" to check
// against, because it IS the somewhere else.
func isProtectedRef(ref string) bool {
	return ref != "" && strings.HasPrefix(ref, protectedRefPrefix)
}

// protectedRefFinding is the unconditional denial for a command that names a
// preservation ref. It skips the reachability question entirely: a ref under
// protectedRefPrefix is by definition the only place its content survives, so
// asking "does it exist somewhere else" would always answer no in the way
// that matters and yes in the way that does not (the ref itself, trivially).
func protectedRefFinding(dir, label string) *finding {
	return &finding{
		label: label, always: true, dir: dir,
		reason: "blocked: " + label + " names a ref this hook created to preserve content before a destructive command; it is the only copy and must not be deleted or overwritten.",
		rewrite: "git fetch origin " + protectedRefPrefix + "*:" + protectedRefPrefix + "*   " +
			"# recover it first if you need to, then work with a different ref",
	}
}
