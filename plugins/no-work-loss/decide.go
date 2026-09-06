package main

import (
	"fmt"
	"strings"
)

// analyze is the whole decision: flatten the command, classify every unit,
// and consult repository state only for the units that could destroy
// something. It also returns the notices a preserved-and-allowed finding
// leaves behind, so a command that moves content into a ref is never silent
// about it just because nothing was denied.
func analyze(command, cwd string) (string, []string) {
	segs, _, ok := parseSegments(command, cwd)
	if !ok {
		// Ambiguity resolves to denial, but only when there is something worth
		// being ambiguous about.
		if label, has := destructiveKeyword(command); has {
			return "blocked: this command could not be parsed, and it names " + label +
				".\nrun: split it into separate commands so each one can be checked", nil
		}
		return "", nil
	}
	cache := newRepoCache()
	aliases := newAliasResolver()
	var notices []string
	for _, seg := range segs {
		for _, f := range classifySegment(seg, aliases, 0) {
			reason, notice := judge(f, cache)
			if reason != "" {
				return reason, notices
			}
			if notice != "" {
				notices = append(notices, notice)
			}
		}
	}
	return "", notices
}

func classifySegment(seg segment, aliases *aliasResolver, depth int) []*finding {
	out := classifyFS(seg)
	if len(seg.argv) == 0 || commandName(seg.argv[0].text) != "git" {
		return out
	}
	if f := classifyGit(seg); f != nil {
		return append(out, f)
	}
	// Nothing matched a builtin verb, so the verb may be an alias hiding a
	// destructive one behind an innocuous name.
	for _, expanded := range aliases.expand(seg, depth) {
		out = append(out, classifySegment(expanded, aliases, depth+1)...)
	}
	return out
}

// judge returns a denial reason, or a notice to surface once a finding was
// preserved and allowed instead of denied. At most one of the two is ever
// non-empty.
func judge(f *finding, cache *repoCache) (deny, notice string) {
	if f.always {
		return f.reason + "\nrun: " + f.rewrite, ""
	}
	// An operand that is not statically known makes the blast radius unknown,
	// which is the case this plugin exists to refuse rather than guess at.
	// The remedy is the one this finding already carries -- an rm and a `>`
	// truncation are not fixed the same way -- never a fixed line that fits
	// neither. Nothing here can be preserved: a path that cannot be resolved
	// cannot be named to `git add` either.
	for _, p := range f.paths {
		if !p.static {
			return fmt.Sprintf("blocked: %s targets a path this hook cannot resolve (%s), so what it would delete is unknown."+
				"\nrun: %s", f.label, describeUnresolved(p), f.rewrite), ""
		}
	}

	// A ref-destroying command asks a different question than a dirty tree
	// does: not "is there uncommitted work" but "does this content exist
	// anywhere else". Already pushed, already merged, or sitting on another
	// branch all mean nothing is lost. Preservation is not attempted here --
	// see docs/decision-model.md.
	if f.reach != nil {
		st := cache.probe(f.dir)
		if st.err != nil {
			return fmt.Sprintf("blocked: cannot tell whether %s would discard commits that exist nowhere else (%v)."+
				"\nrun: git status   # this hook denies destructive commands it cannot verify", f.label, st.err), ""
		}
		if !st.inRepo {
			return "", ""
		}
		safe, where, err := cache.evaluate(st, f.reach)
		if err != nil {
			return fmt.Sprintf("blocked: cannot tell whether %s would discard commits that exist nowhere else (%v)."+
				"\nrun: git status   # this hook denies destructive commands it cannot verify", f.label, err), ""
		}
		if safe {
			return "", "" // recoverable elsewhere, so this is ordinary work
		}
		return fmt.Sprintf("blocked: %s would discard commits that exist nowhere else -- not pushed, not merged, on no other branch.%s"+
			"\nrun: %s", f.label, where, f.rewrite), ""
	}

	st := cache.probe(f.dir)
	if st.err == nil && st.inRepo {
		if f.haz&hazIgnored != 0 {
			cache.ensureIgnored(st)
		}
		if f.haz&hazStash != 0 {
			cache.ensureStash(st)
		}
	}
	if st.err != nil {
		return fmt.Sprintf("blocked: cannot tell whether %s would lose uncommitted work (%v)."+
			"\nrun: git status   # this hook denies destructive commands it cannot verify", f.label, st.err), ""
	}
	if !st.inRepo {
		return "", "" // nothing here is under version control, so nothing here is this plugin's business
	}

	// Preservation is not attempted for a stash entry -- see
	// docs/decision-model.md.
	if f.haz&hazStash != 0 {
		if st.stash == 0 {
			return "", ""
		}
		return fmt.Sprintf("blocked: %s would destroy %s, and a dropped stash is in no branch."+
			"\nrun: %s", f.label, plural(st.stash, "stash entry", "stash entries"), f.rewrite), ""
	}

	tracked, untracked, ignored := st.atRisk(f, f.dir)
	if len(tracked)+len(untracked)+len(ignored) == 0 {
		return "", ""
	}
	// The invariant is that this content must not be lost -- not that this
	// exact command must be refused. Committing it satisfies the invariant
	// directly, and once the commit exists the command is safe by
	// construction, so it is allowed rather than denied.
	summary, names := describeAtRisk(tracked, untracked, ignored)
	if res, ok := preserveAtRiskPaths(st.root, names); ok {
		return "", res.notice(f.label, summary)
	}
	return fmt.Sprintf("blocked: %s would lose %s.\nrun: %s", f.label, summary, f.rewrite), ""
}

// describeAtRisk names what a finding would destroy, split by class rather
// than totalled -- "3 modified + 1 untracked" is the difference between a
// command that spares half of it and one that does not -- and shared between
// the denial and the preservation notice, so the two never drift apart.
func describeAtRisk(tracked, untracked, ignored []string) (summary string, names []string) {
	var parts []string
	add := func(entries []string, label string) {
		if len(entries) == 0 {
			return
		}
		parts = append(parts, fmt.Sprintf("%d %s", len(entries), label))
		names = append(names, entries...)
	}
	add(tracked, "modified")
	add(untracked, "untracked")
	add(ignored, "ignored")

	total := len(names)
	noun := "file"
	if total != 1 {
		noun = "files"
	}
	return fmt.Sprintf("%s %s (%s)", strings.Join(parts, " + "), noun, sample(names, 3)), names
}

// describeUnresolved names an unresolved word for a denial message. A word
// built purely from an expansion -- `$OUT` and nothing else -- carries no
// literal text at all, and quoting that as "" reads as a path rather than
// as what it is: nothing this hook could read.
func describeUnresolved(p word) string {
	if p.text == "" {
		return "an unresolved expansion"
	}
	return fmt.Sprintf("%q", p.text)
}

func sample(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, +%d more", strings.Join(names[:n], ", "), len(names)-n)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func internalErrorReason(label string) string {
	return "blocked: this hook failed while checking a command that names " + label +
		".\nrun: git status   # then re-run; the guard denies what it cannot verify"
}
