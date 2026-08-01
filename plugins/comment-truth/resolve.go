package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The mechanical verdicts: everything decidable by looking, decided for free.
// Whatever these settle never reaches the model, which is most of why the
// expensive stage stays small.

// Finding is one thing wrong with one comment.
type Finding struct {
	File string
	Line int
	Kind ClaimKind
	// What is wrong, in one sentence.
	Problem string
	// The evidence, so the reader can disagree with the checker.
	Evidence string
	Excerpt  string
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s:%d  [%s] %s", f.File, f.Line, f.Kind, f.Problem)
	if f.Evidence != "" {
		s += "\n      " + f.Evidence
	}
	if f.Excerpt != "" {
		s += "\n      comment: " + f.Excerpt
	}
	return s
}

// checkMechanically returns findings settled without a model, plus the blocks
// whose remaining claims need one.
func (r *Repo) checkMechanically(blocks []Block) (findings []Finding, needJudgment []Block) {
	docs := map[string]string{}

	for _, b := range blocks {
		c := Analyze(b)

		// A hedge is settled by reading it: the author is telling you they did
		// not check. There is nothing for a model to weigh.
		if len(c.Hedges) > 0 {
			findings = append(findings, Finding{
				File: b.File, Line: b.Line, Kind: ClaimHedge,
				Problem:  fmt.Sprintf("hedged claim (%s)", strings.Join(c.Hedges, ", ")),
				Evidence: "A hedge is an unverified claim with an escape hatch. Check it and state it, or cut it.",
				Excerpt:  excerpt(b.Text),
			})
		}

		for _, ref := range c.References {
			if where, ok := r.resolve(ref, b.File); !ok {
				findings = append(findings, Finding{
					File: b.File, Line: b.Line, Kind: ClaimReference,
					Problem:  fmt.Sprintf("cites %q, which does not exist in this repo", ref),
					Evidence: "Naming a symbol, test or file is a claim it exists. Deleting one and leaving the comment behind is how this happens.",
					Excerpt:  excerpt(b.Text),
				})
			} else if where != "" {
				_ = where
			}
		}

		// A block that cites a document has supplied its own evidence, so its
		// figures are checkable right here.
		for _, doc := range c.Docs {
			if _, ok := docs[doc]; ok {
				continue
			}
			if body, err := r.readDoc(doc, b.File); err == nil {
				docs[doc] = body
			}
		}
		checkedFigures := false
		for _, q := range c.Quantities {
			for _, doc := range c.Docs {
				body, ok := docs[doc]
				if !ok {
					continue
				}
				checkedFigures = true
				if !q.agreesWith(body) {
					findings = append(findings, Finding{
						File: b.File, Line: b.Line, Kind: ClaimQuantity,
						Problem: fmt.Sprintf("says %q, but %s has no figure that rounds to it", q.Raw, doc),
						Evidence: "A number in a comment is a measurement or it is not written. " +
							"Recalling a figure you produced yourself is guessing about your own work.",
						Excerpt: excerpt(b.Text),
					})
				}
			}
		}

		if c.NeedsJudgment() && !(checkedFigures && len(c.Universal) == 0 && len(c.Causal) == 0) {
			needJudgment = append(needJudgment, b)
		}
	}
	return findings, needJudgment
}

// resolve reports whether a cited name exists. A path is checked as a path; a
// symbol is looked for anywhere in tracked content, which is deliberately a
// weak test -- it answers "does this exist at all", the question that catches a
// name left behind by a deletion, without pretending to be a type checker.
func (r *Repo) resolve(name, from string) (string, bool) {
	if looksLikePath(name) {
		// A partial path ("assets/timeline.js") is relative to somewhere on the
		// citing file's way up to the root, so try each ancestor. Existence and
		// IGNORED-ness both resolve it: a build output is a real file the repo
		// deliberately does not track, and whether it happens to be built right
		// now must not change the answer.
		for _, rel := range r.candidates(name, from) {
			if _, err := os.Stat(filepath.Join(r.Root, rel)); err == nil {
				return rel, true
			}
			if r.ignored(rel) {
				return rel, true
			}
		}
		// A comment often cites a file by NAME rather than by path
		// ("timeline-wire.test.ts decodes them"), and that name is not wrong
		// just because the file lives somewhere else. Fall back to the
		// basename anywhere in the repo before calling it missing.
		if hit := r.findByBase(filepath.Base(name)); hit != "" {
			return hit, true
		}
		// A build output is a real file the repo deliberately does not track,
		// and naming one is correct. Ask git whether the path is IGNORED
		// rather than whether it happens to exist right now, so the answer
		// does not depend on whether anyone has run the build.
		if r.ignored(name) || r.ignoredByBase(filepath.Base(name)) {
			return "", true
		}
		// Finally, a sibling checkout: in a multi-repo session a comment
		// legitimately points at a file in the repo next door.
		if r.inSibling(filepath.Base(name)) {
			return "", true
		}
		return "", false
	}
	if len(name) < 3 {
		return "", true // too short to search for meaningfully
	}
	out, err := r.git("grep", "--fixed-strings", "-l", "-e", name, "--", ".")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}
	return strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]), true
}

// findByBase locates a tracked file by base name, so a citation that names a
// file without its directory still resolves.
func (r *Repo) findByBase(base string) string {
	out, err := r.git("ls-files", "--", "*/"+base, base)
	if err != nil {
		return ""
	}
	if first := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]); first != "" {
		return first
	}
	return ""
}

// candidates lists where a cited path could live: at the repo root, and under
// every directory between the citing file and the root.
func (r *Repo) candidates(name, from string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" || p == "." || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(name)
	for dir := filepath.Dir(from); ; dir = filepath.Dir(dir) {
		add(filepath.Join(dir, name))
		if dir == "." || dir == string(filepath.Separator) {
			break
		}
	}
	return out
}

// ignored reports whether a path is covered by .gitignore -- a generated
// artifact the repo means to have, not a missing file.
func (r *Repo) ignored(name string) bool {
	_, err := r.git("check-ignore", "-q", "--no-index", "--", name)
	return err == nil
}

// ignoredByBase looks for an ignored file with this base name anywhere in the
// working tree, which is how a build output cited by partial path ("the
// assets/timeline.js the embed names") resolves.
func (r *Repo) ignoredByBase(base string) bool {
	out, err := r.git("ls-files", "--others", "--ignored", "--exclude-standard", "--", "*/"+base, base)
	return err == nil && strings.TrimSpace(out) != ""
}

// inSibling looks for the name in the other checkouts of a multi-repo session.
// A cross-repo citation is normal and must not be reported as missing.
func (r *Repo) inSibling(base string) bool {
	parent := filepath.Dir(r.Root)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return false
	}
	for _, e := range entries {
		p := filepath.Join(parent, e.Name())
		if !e.IsDir() || p == r.Root || !isRepo(p) {
			continue
		}
		sib := &Repo{Root: p}
		if sib.findByBase(base) != "" {
			return true
		}
	}
	return false
}

func looksLikePath(name string) bool {
	return strings.Contains(name, "/") || strings.Contains(name, ".") &&
		filepath.Ext(name) != "" && !strings.HasSuffix(name, "()")
}

// readDoc loads a cited document, relative to the repo root or to the file that
// cites it.
func (r *Repo) readDoc(path, from string) (string, error) {
	for _, cand := range []string{
		filepath.Join(r.Root, path),
		filepath.Join(r.Root, filepath.Dir(from), path),
	} {
		if b, err := os.ReadFile(cand); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("not found: %s", path)
}

func excerpt(text string) string {
	const max = 160
	one := strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
	if len(one) <= max {
		return one
	}
	return one[:max] + "…"
}
