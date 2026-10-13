package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// comment-truth: a Stop hook that asks whether the comments this session wrote
// are TRUE.
//
// Comments are the one artifact nothing verifies. Compilers ignore them, tests
// ignore them, CI ignores them -- the reader is the only check, and by then the
// author is not there to correct it. So a wrong comment is read as authority
// and believed for years, which makes it worse than no comment at all.
//
// Three passes, cheapest first, each one narrowing what the next has to look
// at (scope.go, resolve.go, review.go carry the detail):
//
//	1. SCOPE   -- only comments this session's diff added or changed.
//	2. CHECK   -- names must resolve; a figure must agree with the document the
//	              comment itself cites; a hedge is an unverified claim. Free.
//	3. JUDGE   -- what is left goes to an external model WITH the evidence
//	              already resolved, in one bounded request.
//
// Exit 2 blocks the stop and the findings go to the model as work to do.

type hookInput struct {
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
	CWD            string `json:"cwd"`
	SessionID      string `json:"session_id"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stderr))
}

func run(stdin io.Reader, stderr io.Writer) int {
	raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return 0
	}
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		// Garbage on stdin is not this hook's problem to report.
		return 0
	}
	// Already blocking once is enough: a hook that re-blocks on its own block
	// is an infinite loop, and the findings are already in front of the model.
	if in.StopHookActive {
		return 0
	}

	dir := in.CWD
	if dir == "" {
		dir, _ = os.Getwd()
	}

	findings := check(dir)
	if len(findings) == 0 {
		return 0
	}
	fmt.Fprint(stderr, report(findings))
	return 2
}

// check runs the three passes over every repository root under dir. A
// multi-repo session has several checkouts; each is scoped to its own diff.
func check(dir string) []Finding {
	var all []Finding
	for _, root := range repoRoots(dir) {
		repo, ok := openRepo(root)
		if !ok {
			continue
		}
		blocks := repo.changedBlocks()
		if len(blocks) == 0 {
			continue
		}
		mech, needJudgment := repo.checkMechanically(blocks)
		all = append(all, mech...)

		if len(needJudgment) == 0 {
			continue
		}
		rv, ok := newReviewer()
		if !ok {
			continue // documented no-op: the mechanical floor still applied
		}
		judged, err := rv.Review(context.Background(), repo, needJudgment)
		if err != nil {
			// Loud, never silent: a checker that reports nothing when its
			// backend fails is claiming the comments were checked.
			all = append(all, Finding{
				File: root, Kind: "judgment",
				Problem: "the comment reviewer could not be reached, so " +
					fmt.Sprintf("%d comment(s) making claims were NOT checked", len(needJudgment)),
				Evidence: err.Error(),
			})
			continue
		}
		all = append(all, judged...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		return all[i].Line < all[j].Line
	})
	return all
}

// changedBlocks is the scope pass: the comment blocks this session touched.
func (r *Repo) changedBlocks() []Block {
	var out []Block
	for _, f := range r.changedFiles() {
		touched := r.addedLines(f)
		if len(touched) == 0 {
			continue
		}
		src, err := os.ReadFile(filepath.Join(r.Root, f))
		if err != nil {
			continue
		}
		out = append(out, blocksIn(f, src, touched)...)
	}
	return out
}

// repoRoots finds the checkouts to inspect: dir itself when it is a repo, else
// its immediate children (the shape of a multi-repo session).
func repoRoots(dir string) []string {
	if isRepo(dir) {
		return []string{dir}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if p := filepath.Join(dir, e.Name()); isRepo(p) {
			roots = append(roots, p)
		}
	}
	return roots
}

func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func report(findings []Finding) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"STOP: %d comment(s) you wrote this session make claims that do not hold up.\n\n",
		len(findings)))
	for _, f := range findings {
		sb.WriteString(f.String())
		sb.WriteString("\n\n")
	}
	sb.WriteString(
		"Fix each one at the source: check the claim and correct it, or cut the sentence.\n" +
			"Do not soften a claim into a hedge to get past this -- a hedge is the same\n" +
			"unverified claim with an escape hatch, and it is also reported.\n")
	return sb.String()
}
