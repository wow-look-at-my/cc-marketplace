package main

import (
	"path/filepath"
	"strings"
)

// What class of content a command can destroy. Keeping these apart is the
// whole point: `reset --hard` discards tracked modifications and leaves
// untracked files alone, `clean -fd` does the exact opposite. Collapsing both
// into one "dirty" bit produces a guard that denies the safe half of each and
// gets switched off.
type hazard uint8

// There is deliberately no class for refs and commits. A command that
// destroys those is refused outright rather than weighed against state: the
// remote history a force push overwrites is in nobody's reflog but the
// author's, so there is no local state that could make it safe.
const (
	hazTracked hazard = 1 << iota
	hazUntracked
	hazIgnored
	hazStash
)

// A reason to look at repository state, or -- when always is set -- to refuse
// without looking.
type finding struct {
	label   string // the command, normalized for the message
	haz     hazard
	dir     string
	paths   []word // path-scoped check; nil means the whole repository
	always  bool
	reason  string // complete message, always-deny only
	rewrite string
	// reach turns a ref-destroying command into a question about whether the
	// commits survive elsewhere, rather than a blanket refusal.
	reach *reachCheck
}

type gitCall struct {
	verb     string
	sub      string // first operand, for verbs that dispatch on one
	flags    map[string]bool
	operands []word
	dashDash bool
	dir      string
	static   bool
	argv     []word
}

func (g *gitCall) has(names ...string) bool {
	for _, n := range names {
		if g.flags[n] {
			return true
		}
	}
	return false
}

// parseGit walks git's global options to find the verb, applying -C and
// --work-tree to the directory as it goes. Reports false when the text runs
// out mid-option, which the caller treats as unparseable.
func parseGit(argv []word, cwd string, gitEnv bool) (*gitCall, bool) {
	g := &gitCall{flags: map[string]bool{}, dir: cwd, static: true}
	if gitEnv {
		g.dir = unknownDirText
	}
	i := 1
	for ; i < len(argv); i++ {
		t := argv[i].text
		switch {
		case t == "-C", t == "--work-tree":
			if i+1 >= len(argv) {
				return nil, false
			}
			g.dir = joinDir(g.dir, argv[i+1])
			i++
		case strings.HasPrefix(t, "--work-tree="):
			g.dir = joinDir(g.dir, word{text: strings.TrimPrefix(t, "--work-tree="), static: argv[i].static})
		case t == "--git-dir", t == "--namespace", t == "-c", t == "--exec-path":
			if i+1 >= len(argv) {
				return nil, false
			}
			if t == "--git-dir" {
				g.dir = unknownDirText
			}
			i++
		case strings.HasPrefix(t, "--git-dir="):
			g.dir = unknownDirText
		case strings.HasPrefix(t, "-"):
			// Any other global option is a flag with no separate value.
		default:
			g.verb = t
			g.argv = argv
			g.flags, g.operands, g.dashDash, g.static = splitArgs(argv[i+1:])
			if len(g.operands) > 0 {
				g.sub = g.operands[0].text
			}
			return g, true
		}
	}
	return nil, false // `git` with no verb destroys nothing, but says nothing either
}

// splitArgs separates flags from operands. Short flags bundle, so -fdx has to
// register as -f, -d and -x independently or a flag check misses every
// clustered spelling.
func splitArgs(rest []word) (flags map[string]bool, operands []word, dashDash, static bool) {
	flags = map[string]bool{}
	static = true
	for _, a := range rest {
		if !a.static {
			static = false
		}
		t := a.text
		switch {
		case dashDash:
			operands = append(operands, a)
		case t == "--":
			dashDash = true
		case strings.HasPrefix(t, "--"):
			name := t
			if i := strings.Index(t, "="); i > 0 {
				name = t[:i]
			}
			flags[name] = true
		case len(t) > 1 && strings.HasPrefix(t, "-"):
			flags[t] = true
			for _, c := range t[1:] {
				flags["-"+string(c)] = true
			}
		default:
			operands = append(operands, a)
		}
	}
	return flags, operands, dashDash, static
}

func joinDir(cwd string, w word) string {
	if !w.static || w.text == "" {
		return unknownDirText
	}
	if filepath.IsAbs(w.text) {
		return filepath.Clean(w.text)
	}
	if cwd == unknownDirText {
		return unknownDirText
	}
	return filepath.Clean(filepath.Join(cwd, w.text))
}

// classifyGit returns what this git invocation puts at risk, or nil when it
// risks nothing. Only destructive verbs are enumerated -- every other verb,
// known or not, falls through to allow, so read-only work never pays and a new
// git subcommand does not arrive pre-blocked.
func classifyGit(seg segment) *finding {
	g, ok := parseGit(seg.argv, seg.cwd, seg.relocated)
	if !ok {
		return nil
	}
	orig := shellJoin(seg.argv)

	switch g.verb {
	case "reset":
		// Soft and mixed resets move the ref and leave the working tree
		// alone; only these three overwrite files.
		if !g.has("--hard", "--merge", "--keep") {
			return nil
		}
		return &finding{
			label: "git reset --hard", haz: hazTracked, dir: g.dir,
			rewrite: `git stash push -u -m "pre-reset" && ` + orig,
		}

	case "checkout":
		if g.has("-b", "-B", "--orphan") {
			return nil // creating a branch carries changes across, never drops them
		}
		label := "git checkout <ref>"
		if g.dashDash || g.has("-f", "--force", "--ours", "--theirs") {
			label = "git checkout -- <path>"
		}
		return &finding{
			label: label, haz: hazTracked, dir: g.dir,
			rewrite: `git stash push -u -m "pre-checkout" && ` + orig,
		}

	case "switch":
		if g.has("-c", "-C", "--create", "--force-create") {
			return nil
		}
		return &finding{
			label: "git switch", haz: hazTracked, dir: g.dir,
			rewrite: `git stash push -u -m "pre-switch" && ` + orig,
		}

	case "restore":
		// --staged alone rewrites the index from HEAD and leaves the file on
		// disk, so the content survives. Anything touching the worktree does not.
		if g.has("--staged", "-S") && !g.has("--worktree", "-W") {
			return nil
		}
		return &finding{
			label: "git restore", haz: hazTracked, dir: g.dir,
			rewrite: `git stash push -u -m "pre-restore" && ` + orig,
		}

	case "clean":
		if g.has("-n", "--dry-run", "-i", "--interactive") {
			return nil
		}
		// Deliberately not gated on -f: clean.requireForce is configurable, so
		// treating an unforced clean as harmless would leave a hole that
		// depends on someone else's config.
		haz := hazUntracked
		rewrite := `git stash push -u -m "pre-clean"`
		if g.has("-x", "-X") {
			haz |= hazIgnored
			rewrite = `git stash push -a -m "pre-clean"`
		}
		return &finding{
			label: "git clean", haz: haz, dir: g.dir,
			rewrite: rewrite + `   # stashes them instead of deleting them`,
		}

	case "stash":
		switch g.sub {
		case "drop":
			return &finding{
				label: "git stash drop", haz: hazStash, dir: g.dir,
				rewrite: "git stash pop   # apply it instead of discarding it",
			}
		case "clear":
			return &finding{
				label: "git stash clear", haz: hazStash, dir: g.dir,
				rewrite: "git stash list   # apply or branch each entry first",
			}
		}
		return nil // push/save/list/show/apply/pop/branch all preserve content

	case "push":
		if g.has("--force-with-lease", "--force-if-includes") {
			return nil
		}
		if g.has("--mirror") {
			// --mirror rewrites every ref at once, so there is no bounded set
			// of commits to check for survival.
			return &finding{
				label: "git push --mirror", always: true, dir: g.dir,
				reason:  "blocked: --mirror overwrites every remote ref at once, so what it would discard cannot be enumerated.",
				rewrite: "git push --force-with-lease <remote> <branch>   # one ref at a time",
			}
		}
		forced := g.has("-f", "--force")
		for _, o := range g.operands {
			if strings.HasPrefix(o.text, "+") && strings.Contains(o.text, ":") {
				forced = true
			}
		}
		deleting := g.has("--delete", "-d")
		if !forced && !deleting {
			return nil
		}
		remote, dst, src := parsePushSpec(g.operands)
		label, rewrite := "git push --force", shellJoin(replaceFlag(seg.argv, []string{"-f", "--force"}, "--force-with-lease"))+
			"   # refuses if the remote moved since you last fetched"
		if deleting {
			// A deletion pushes nothing, so there is no source that could make
			// the old tip a fast-forward.
			src = ""
			if len(g.operands) > 1 {
				remote, dst = g.operands[0].text, g.operands[len(g.operands)-1].text
			}
			label = "git push --delete"
			rewrite = "git tag archive/" + dst + " " + remote + "/" + dst + " && " + orig + "   # keep a pointer, then delete"
		}
		return &finding{
			label: label, dir: g.dir, rewrite: rewrite,
			reach: &reachCheck{kind: reachRef, remote: remote, dst: dst, src: src},
		}

	case "branch":
		if g.has("-D") || (g.has("-d", "--delete") && g.has("-f", "--force")) {
			name := lastOperand(g.operands)
			if name == "" {
				return nil
			}
			ref := "refs/heads/" + name
			return &finding{
				label: "git branch -D " + name, dir: g.dir,
				reach:   &reachCheck{kind: reachRef, ref: ref, ignore: []string{ref}},
				rewrite: shellJoin(replaceFlag(seg.argv, []string{"-D", "-d", "--delete", "-f", "--force"}, "-d")) + "   # merged-only delete, or tag it first",
			}
		}
		if g.has("-M") {
			name := lastOperand(g.operands)
			if name == "" {
				return nil
			}
			ref := "refs/heads/" + name
			return &finding{
				label: "git branch -M " + name, dir: g.dir,
				reach:   &reachCheck{kind: reachRef, ref: ref, ignore: []string{ref}},
				rewrite: shellJoin(replaceFlag(seg.argv, []string{"-M"}, "-m")) + "   # refuses if the name is taken",
			}
		}
		return nil

	case "rebase", "merge", "cherry-pick", "revert", "am":
		// The recovery halves of these verbs are how a wedged tree gets fixed;
		// blocking them would trap the session in the state it is trying to leave.
		if g.has("--continue", "--abort", "--skip", "--quit", "--edit-todo") {
			return nil
		}
		return &finding{
			label: "git " + g.verb, haz: hazTracked, dir: g.dir,
			rewrite: `git stash push -u -m "pre-` + g.verb + `" && ` + orig,
		}

	case "rm":
		if g.has("--cached") {
			return nil // unstages only; the file stays on disk
		}
		return &finding{
			label: "git rm", haz: hazTracked | hazUntracked, dir: g.dir,
			paths:   g.operands,
			rewrite: "git stash push -u -- " + shellJoin(g.operands),
		}

	case "checkout-index":
		if !g.has("-f", "--force") {
			return nil
		}
		return &finding{
			label: "git checkout-index -f", haz: hazTracked, dir: g.dir,
			rewrite: `git stash push -u -m "pre-checkout-index" && ` + orig,
		}

	case "reflog":
		if g.sub != "expire" && g.sub != "delete" {
			return nil
		}
		return &finding{
			label: "git reflog " + g.sub, dir: g.dir,
			reach:   &reachCheck{kind: reachOrphans},
			rewrite: "git fsck --unreachable --no-reflogs   # see what only the reflog is holding, and tag it first",
		}

	case "update-ref":
		if !g.has("-d") {
			return nil
		}
		ref := lastOperand(g.operands)
		if ref == "" {
			return nil
		}
		return &finding{
			label: "git update-ref -d " + ref, dir: g.dir,
			reach:   &reachCheck{kind: reachRef, ref: ref, ignore: []string{ref}},
			rewrite: "git tag archive/<name> " + ref + " && " + orig + "   # keep a pointer, then delete",
		}

	case "filter-branch":
		return &finding{
			label: "git filter-branch", dir: g.dir,
			reach:   &reachCheck{kind: reachPushed},
			rewrite: "git push origin HEAD   # get the current history onto a remote first",
		}

	case "worktree":
		if g.sub != "remove" || !g.has("-f", "--force") {
			return nil // without --force git already refuses a dirty worktree
		}
		path := ""
		if len(g.operands) > 1 {
			path = joinDir(g.dir, g.operands[len(g.operands)-1])
		}
		return &finding{
			label: "git worktree remove --force", dir: g.dir,
			reach:   &reachCheck{kind: reachWorktree, path: path},
			rewrite: "git worktree remove <path>   # refuses while the worktree is dirty",
		}

	case "submodule":
		if g.sub == "deinit" && g.has("-f", "--force") {
			return &finding{
				label: "git submodule deinit -f", haz: hazTracked, dir: g.dir,
				rewrite: "git submodule deinit <path>   # refuses while the submodule is dirty",
			}
		}
		if g.sub == "update" && g.has("-f", "--force") {
			return &finding{
				label: "git submodule update --force", haz: hazTracked, dir: g.dir,
				rewrite: "git submodule update   # without --force",
			}
		}
		return nil
	}
	return nil
}

// lastOperand returns the final statically-known operand, which for the
// ref-taking verbs is the ref being acted on.
func lastOperand(operands []word) string {
	for i := len(operands) - 1; i >= 0; i-- {
		if operands[i].static && operands[i].text != "" {
			return operands[i].text
		}
	}
	return ""
}

// replaceFlag swaps the first destructive flag it finds for a safe one and
// drops the rest, so the suggested command is the user's own command.
func replaceFlag(argv []word, remove []string, add string) []word {
	drop := map[string]bool{}
	for _, r := range remove {
		drop[r] = true
	}
	out := make([]word, 0, len(argv)+1)
	added := false
	for _, a := range argv {
		if drop[a.text] {
			if !added {
				out = append(out, word{text: add, static: true})
				added = true
			}
			continue
		}
		out = append(out, a)
	}
	if !added {
		out = append(out, word{text: add, static: true})
	}
	return out
}

// shellJoin renders argv back into something copy-pasteable.
func shellJoin(argv []word) string {
	parts := make([]string, 0, len(argv))
	for _, a := range argv {
		parts = append(parts, shellQuote(a.text))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.ContainsAny(s, " \t\n'\"\\$`&|;<>()*?[]{}!#~") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}
