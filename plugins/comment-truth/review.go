package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// THE JUDGMENT STAGE, and the reason it is affordable.
//
// The model is never given the repository and never asked to go looking. Every
// piece of evidence a verdict could need is resolved MECHANICALLY first -- the
// code the comment sits on, the definitions of the names it cites, the lines of
// the document it points at -- and shipped with the comment in one request.
//
// That inverts the usual cost. An agent turned loose on "is this comment true"
// spends its budget searching; here the searching is grep, which is free, and
// the model only does the part that actually needs reading comprehension:
// is this claim supported by what it is looking at, is it scoped to what was
// measured, and is it even about the thing it sits on.
//
// One request per Stop, every remaining block batched into it. No key
// configured means this stage is skipped and the mechanical findings still
// block -- the check degrades to less, never to nothing.

const (
	envURL   = "COMMENT_TRUTH_API_URL"
	envKey   = "COMMENT_TRUTH_API_KEY"
	envModel = "COMMENT_TRUTH_MODEL"

	// Evidence is capped so one enormous file cannot turn a cheap check into an
	// expensive one. A definition that does not fit in this much context was
	// not going to be judged well anyway.
	maxEvidenceBytes = 4000
	maxBlocks        = 25
	requestTimeout   = 90 * time.Second
)

// Reviewer is the external model, or nothing.
type Reviewer struct {
	URL, Key, Model string
	Client          *http.Client
}

// newReviewer reads the endpoint from the environment. Absent config is a
// documented no-op, not an error: the mechanical passes are the floor.
func newReviewer() (*Reviewer, bool) {
	key := os.Getenv(envKey)
	if key == "" {
		return nil, false
	}
	url := os.Getenv(envURL)
	if url == "" {
		url = "https://api.openai.com/v1/chat/completions"
	}
	model := os.Getenv(envModel)
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &Reviewer{URL: url, Key: key, Model: model, Client: &http.Client{Timeout: requestTimeout}}, true
}

// evidenceFor assembles what a verdict needs, all of it resolved locally.
func (r *Repo) evidenceFor(b Block) string {
	var sb strings.Builder
	c := Analyze(b)

	fmt.Fprintf(&sb, "FILE: %s:%d\n", b.File, b.Line)
	if b.Code != "" {
		fmt.Fprintf(&sb, "\nTHE CODE THIS COMMENT IS ATTACHED TO:\n%s\n", clamp(b.Code, 800))
	}
	for _, ref := range c.References {
		if def := r.definitionOf(ref); def != "" {
			fmt.Fprintf(&sb, "\nWHAT %q IS:\n%s\n", ref, clamp(def, 900))
		}
	}
	for _, doc := range c.Docs {
		if body, err := r.readDoc(doc, b.File); err == nil {
			if rel := relevantLines(body, c); rel != "" {
				fmt.Fprintf(&sb, "\nFROM %s (the document this comment cites):\n%s\n", doc, clamp(rel, 1200))
			}
		}
	}
	return clamp(sb.String(), maxEvidenceBytes)
}

// definitionOf finds where a name is defined, so a claim about it can be
// weighed against the thing itself rather than against its name.
func (r *Repo) definitionOf(name string) string {
	out, err := r.git("grep", "-n", "--fixed-strings", "-e", name, "--", ".")
	if err != nil {
		return ""
	}
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		// Prefer lines that look like a definition over lines that merely
		// mention the name.
		if strings.Contains(l, "func ") || strings.Contains(l, "const ") ||
			strings.Contains(l, "var ") || strings.Contains(l, "type ") ||
			strings.Contains(l, "class ") || strings.Contains(l, "function ") {
			keep = append(keep, l)
		}
		if len(keep) >= 6 {
			break
		}
	}
	return strings.Join(keep, "\n")
}

// relevantLines pulls the parts of a cited document that mention the block's
// figures, rather than shipping the whole document.
func relevantLines(doc string, c Claims) string {
	lines := strings.Split(doc, "\n")
	want := map[int]bool{}
	for i, l := range lines {
		for _, q := range c.Quantities {
			for _, n := range numberRe.FindAllString(l, -1) {
				if strings.HasPrefix(n, strings.SplitN(q.Raw, " ", 2)[0]) || strings.Contains(l, q.Unit) {
					want[i] = true
				}
				_ = n
			}
		}
	}
	var out []string
	for i := range lines {
		if want[i] {
			out = append(out, strings.TrimSpace(lines[i]))
		}
		if len(out) >= 25 {
			break
		}
	}
	return strings.Join(out, "\n")
}

const systemPrompt = `You verify whether a source-code comment is TRUE, given the code and evidence supplied. You are not reviewing style, length, or tone, and you must not suggest rewording.

You have everything you are going to get. Do not ask for more; judge on what is here, and when the evidence does not settle a claim, say so rather than guessing.

Report a comment ONLY when one of these is true:
1. UNSUPPORTED — it asserts something the evidence contradicts, or asserts a cause/history nothing supports (an invented "which is how X used to break" is the classic).
2. OVERSCOPED — a real measurement or property, widened to a claim that was never established (a decode benchmark quoted as an end-to-end result).
3. MISPLACED — the comment is not about the thing it is attached to.
4. ABSOLUTE-BUT-NOT — "always", "never", "only", "cannot", where the code plainly allows the other case.

Say nothing about a comment that is accurate, or that the evidence cannot settle. A false alarm costs more than a miss here: it teaches people to switch the check off.

Reply with JSON only: {"findings":[{"index":<int>,"problem":"<one sentence>","evidence":"<what in the supplied material shows it>"}]}
An empty findings array is the expected answer.`

type reviewFinding struct {
	Index    int    `json:"index"`
	Problem  string `json:"problem"`
	Evidence string `json:"evidence"`
}

// Review sends one batched request and returns findings for the blocks given.
func (rv *Reviewer) Review(ctx context.Context, repo *Repo, blocks []Block) ([]Finding, error) {
	if len(blocks) > maxBlocks {
		blocks = blocks[:maxBlocks]
	}
	var user strings.Builder
	for i, b := range blocks {
		fmt.Fprintf(&user, "=== COMMENT %d ===\n%s\n\n%s\n\n", i, b.Text, repo.evidenceFor(b))
	}

	body, _ := json.Marshal(map[string]any{
		"model": rv.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": user.String()},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rv.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rv.Key)

	resp, err := rv.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", rv.URL, resp.Status)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}
	return parseVerdict(out.Choices[0].Message.Content, blocks)
}

// parseVerdict maps the model's answer back onto the blocks it judged. An
// answer that does not parse is an error, never a silent pass -- a checker that
// goes quiet when its backend misbehaves reports green for work it never
// checked.
func parseVerdict(content string, blocks []Block) ([]Finding, error) {
	content = strings.TrimSpace(content)
	if i := strings.Index(content, "{"); i > 0 {
		content = content[i:]
	}
	if j := strings.LastIndex(content, "}"); j >= 0 && j < len(content)-1 {
		content = content[:j+1]
	}
	var parsed struct {
		Findings []reviewFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("unparseable verdict: %w", err)
	}
	var out []Finding
	for _, f := range parsed.Findings {
		if f.Index < 0 || f.Index >= len(blocks) {
			continue
		}
		b := blocks[f.Index]
		out = append(out, Finding{
			File: b.File, Line: b.Line, Kind: "judgment",
			Problem: f.Problem, Evidence: f.Evidence, Excerpt: excerpt(b.Text),
		})
	}
	return out, nil
}

func clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…"
}
