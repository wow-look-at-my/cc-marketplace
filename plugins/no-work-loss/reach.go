package main

import (
	"strconv"
	"strings"
)

// Commands that destroy refs or commits are not automatically unsafe. If the
// commits survive somewhere else -- another branch, a tag, a remote, or simply
// because they were already merged -- then nothing is lost and the command is
// ordinary work. These checks answer "does this content exist anywhere else?"
// so the deny is reserved for the case where it genuinely does not.
type reachKind int

const (
	// reachRef: a ref is being deleted or overwritten. Safe when its tip is
	// contained by some other ref.
	reachRef reachKind = iota
	// reachPushed: history is being rewritten wholesale. Safe when everything
	// on HEAD is already on a remote.
	reachPushed
	// reachOrphans: the reflog is being destroyed. Safe when no commit depends
	// on it to stay findable.
	reachOrphans
	// reachWorktree: another worktree is being force-removed. Safe when that
	// worktree has nothing uncommitted.
	reachWorktree
)

type reachCheck struct {
	kind reachKind

	// reachRef
	ref    string   // resolved directly, e.g. refs/heads/feature
	remote string   // for a push: "" means resolve from upstream
	dst    string   // for a push: "" means HEAD's branch
	src    string   // for a push: what is being pushed; "" means HEAD
	ignore []string // refs that do not count as somewhere else

	// reachWorktree
	path string
}

// evaluate reports whether the content is recoverable, plus where it survives
// (for the allow path) or how much is at stake (for the message).
func (c *repoCache) evaluate(st *repoState, r *reachCheck) (safe bool, where string, err error) {
	switch r.kind {
	case reachPushed:
		out, _, e := runGit(st.root, "rev-list", "--count", "HEAD", "--not", "--remotes")
		if e != nil {
			return false, "", e
		}
		n, _ := strconv.Atoi(strings.TrimSpace(out))
		return n == 0, "every commit on HEAD is already on a remote", nil

	case reachOrphans:
		// --no-reflogs is the whole point: without it fsck treats a commit the
		// reflog still names as reachable, which is exactly the commit that
		// expiring the reflog would strand.
		out, _, e := runGit(st.root, "fsck", "--unreachable", "--no-reflogs", "--no-progress")
		if out == "" && e != nil {
			return false, "", e
		}
		n := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "unreachable commit") {
				n++
			}
		}
		return n == 0, "no commit depends on the reflog to stay findable", nil

	case reachWorktree:
		if r.path == "" {
			return false, "", errUnknownDir
		}
		wt := c.probe(r.path)
		if wt.err != nil {
			return false, "", wt.err
		}
		if !wt.inRepo {
			return true, "not a worktree", nil
		}
		n := len(wt.tracked) + len(wt.untracked)
		return n == 0, "that worktree has nothing uncommitted", nil
	}

	ref := r.ref
	// A push names its target indirectly, via the remote-tracking ref that
	// mirrors it. That distinction decides what a MISSING ref means below.
	viaPush := ref == ""
	if viaPush {
		ref, err = c.pushRef(st, r)
		if err != nil {
			return false, "", err
		}
		if ref == "" {
			return false, "", errNoRemoteRef
		}
	}

	sha, _, e := runGit(st.root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	sha = strings.TrimSpace(sha)
	if e != nil || sha == "" {
		if viaPush {
			// No local mirror of the remote branch means no local record of
			// what the push would overwrite. Absence of evidence is not
			// evidence the remote is empty.
			return false, "", errNoRemoteRef
		}
		// A local ref that does not exist has nothing to destroy; git will
		// fail on its own terms rather than losing anything.
		return true, "no such ref", nil
	}

	src := r.src
	if src == "" && r.kind == reachRef && (r.remote != "" || r.dst != "") {
		src = "HEAD"
	}
	if src != "" {
		if _, _, e := runGit(st.root, "merge-base", "--is-ancestor", sha, src); e == nil {
			return true, "fast-forward, nothing is rewritten", nil
		}
	}

	out, _, e := runGit(st.root, "for-each-ref", "--contains", sha, "--format=%(refname)")
	if e != nil {
		return false, "", e
	}
	var holders []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || name == ref || contains(r.ignore, name) {
			continue
		}
		// refs/remotes/<remote>/HEAD is a symbolic alias for the branch being
		// overwritten, so it is the same ref wearing a second name -- counting
		// it as "somewhere else" is what made an early version of this check
		// report every force push as safe.
		if isRemoteHead(name) {
			continue
		}
		holders = append(holders, name)
	}
	if len(holders) > 0 {
		return true, "still reachable from " + strings.Join(trimTo(holders, 3), ", "), nil
	}
	return false, "", nil
}

// pushRef works out which remote-tracking ref a push would overwrite.
func (c *repoCache) pushRef(st *repoState, r *reachCheck) (string, error) {
	remote, dst := r.remote, r.dst
	if remote == "" || dst == "" {
		up, _, err := runGit(st.root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
		up = strings.TrimSpace(up)
		if err != nil || up == "" {
			return "", nil // no upstream to reason about
		}
		part := strings.SplitN(up, "/", 2)
		if len(part) != 2 {
			return "", nil
		}
		if remote == "" {
			remote = part[0]
		}
		if dst == "" {
			dst = part[1]
		}
	}
	return "refs/remotes/" + remote + "/" + strings.TrimPrefix(dst, "refs/heads/"), nil
}

func isRemoteHead(ref string) bool {
	return strings.HasPrefix(ref, "refs/remotes/") && strings.HasSuffix(ref, "/HEAD")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func trimTo(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(list[:n:n], "...")
}

// parsePushSpec pulls the remote and refspec out of a push argv. An empty
// field means "resolve it from the repository at decision time".
func parsePushSpec(operands []word) (remote, dst, src string) {
	if len(operands) > 0 && operands[0].static {
		remote = operands[0].text
	}
	if len(operands) > 1 && operands[1].static {
		spec := strings.TrimPrefix(operands[1].text, "+")
		if from, to, ok := strings.Cut(spec, ":"); ok {
			src, dst = from, to
		} else {
			src, dst = spec, spec
		}
	}
	return remote, dst, src
}
