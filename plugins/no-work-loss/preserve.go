package main

import (
	"fmt"
	"os"
	"strings"
)

// protectedRefPrefix names a ref an EARLIER build of this hook created to hold
// content a destructive command was about to lose. Preservation now commits to
// the branch, so nothing creates one any more. The protection stays because a
// repository can still carry one, and there it is the ONLY place that content
// survives -- see gitverb.go's checks against this prefix.
const protectedRefPrefix = "refs/no-work-loss/"

// preserveResult is what a successful commit produced, for the notice the
// caller shows once the destructive command is allowed to proceed.
// ref names the branch the commit landed on.
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

	headTree := ""
	if hasHead {
		out, _, err := runGit(root, "rev-parse", "HEAD^{tree}")
		if err == nil {
			headTree = strings.TrimSpace(out)
		}
	}

	// A staged version is a THIRD state, distinct from HEAD and from the
	// working tree, and it lives only in the index. Committing the working
	// tree and then refreshing the index to match it destroys that state, so
	// it is captured first and the working tree lands on top of it. A path
	// with nothing staged contributes no entry, and a run where nothing was
	// staged produces no commit here at all.
	var trees []string
	if t := stagedTree(root, env, paths, hasHead); t != "" && t != headTree {
		trees = append(trees, t)
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
	if len(trees) == 0 || trees[len(trees)-1] != tree {
		trees = append(trees, tree)
	}

	commit := ""
	parent := ""
	if hasHead {
		parent = "HEAD"
	}
	for i, t := range trees {
		commitArgs := []string{"commit-tree", t, "-m", preserveMessage(paths, i == len(trees)-1)}
		if parent != "" {
			commitArgs = append(commitArgs, "-p", parent)
		}
		commitOut, _, err := runGit(root, commitArgs...)
		if err != nil {
			return nil, false
		}
		commit = strings.TrimSpace(commitOut)
		if commit == "" {
			return nil, false
		}
		parent = commit
	}

	// The commit lands on the CURRENT BRANCH. A commit under a private ref
	// prefix is invisible to every ordinary command, so nobody reviews it and
	// the first session that notices the prefix deletes it. A commit on the
	// branch is in the log, in the diff, and in the next push.
	if _, _, err := runGit(root, "update-ref", "HEAD", commit); err != nil {
		// The commit object exists but nothing names it, so git gc can reap
		// it. That is not durable preservation, so this must not read as one.
		return nil, false
	}
	// The branch moved under the real index, which still holds the old tree
	// for these paths. Left alone, `git status` reports a STAGED REVERT of the
	// content just preserved, and the next `git commit` takes it. Only the
	// preserved paths are refreshed, so other staged work is untouched and the
	// working tree is never written.
	resetArgs := append([]string{"reset", "-q", commit, "--"}, paths...)
	runGit(root, resetArgs...)

	res = &preserveResult{ref: branchName(root), commit: commit}
	if _, stderr, err := runGitEnvTimeout(root, preservePushTimeout, nil, "push", "origin", "HEAD"); err != nil {
		res.pushErr = strings.TrimSpace(stderr)
		if res.pushErr == "" {
			res.pushErr = err.Error()
		}
	} else {
		res.pushed = true
	}
	return res, true
}

// stagedTree builds a tree holding HEAD's content everywhere except the
// at-risk paths, which take the content sitting in the USER'S index. It reads
// the real index with `ls-files --stage` and copies each entry into the
// throwaway one; the user's own index is never written. An empty result means
// no at-risk path had a staged entry to keep, which is the ordinary case.
func stagedTree(root string, env, paths []string, hasHead bool) string {
	// Ask first, in one call, which at-risk paths have anything staged at
	// all. A hook runs in front of every Bash call, and the ordinary tree has
	// nothing staged, so the walk below must cost nothing there rather than
	// one subprocess per path. A repository with no HEAD has no tree to
	// differ from; its working-tree commit is the whole story.
	if !hasHead {
		return ""
	}
	diffArgs := append([]string{"diff", "--cached", "--name-only", "-z", "--"}, paths...)
	diff, _, err := runGit(root, diffArgs...)
	if err != nil {
		return ""
	}
	var changed []string
	for _, p := range strings.Split(diff, "\x00") {
		if p != "" {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return ""
	}

	args := append([]string{"ls-files", "--stage", "-z", "--"}, changed...)
	out, _, err := runGit(root, args...)
	if err != nil {
		return ""
	}
	staged := 0
	for _, rec := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[2] != "0" {
			// Stage 1, 2 or 3 is an unresolved merge conflict. Its entries do
			// not make a tree, and a conflicted path is not a state a commit
			// can hold, so it is left to the working-tree pass below.
			continue
		}
		info := fields[0] + "," + fields[1] + "," + path
		if _, _, err := runGitEnvTimeout(root, gitTimeout, env, "update-index", "--add", "--cacheinfo", info); err != nil {
			return ""
		}
		staged++
	}
	if staged == 0 {
		return ""
	}
	tree, _, err := runGitEnvTimeout(root, gitTimeout, env, "write-tree")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(tree)
}

func preserveMessage(paths []string, working bool) string {
	what := "the staged version of"
	if working {
		what = "the working-tree version of"
	}
	return fmt.Sprintf("no-work-loss: preserved %s %d path(s) before a destructive command\n\n%s",
		what, len(paths), strings.Join(paths, "\n"))
}

// branchName is the branch HEAD points at, for the notice. A detached HEAD
// has no name, and saying so is better than printing an empty one.
func branchName(root string) string {
	out, _, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD")
	name := strings.TrimSpace(out)
	if err != nil || name == "" || name == "HEAD" {
		return "a detached HEAD"
	}
	return name
}

// notice reports the preservation once. The commit is on the branch, so it is
// visible in the log without this -- but a commit the session did not write
// itself must still be announced. label is the finding's own name for the
// command that would have destroyed the content; summary is the same
// tracked/untracked/ignored breakdown a denial would have shown.
func (r *preserveResult) notice(label, summary string) string {
	if r.pushed {
		return fmt.Sprintf("preserved: %s would have lost %s, so it was committed to %s (%s) and pushed before being allowed to proceed.",
			label, summary, r.ref, shortSHA(r.commit))
	}
	return fmt.Sprintf("preserved: %s would have lost %s, so it was committed to %s (%s) before being allowed to proceed. The push failed (%s) -- the commit is on your branch; push it when you can.",
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
