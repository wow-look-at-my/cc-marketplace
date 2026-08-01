package main

import (
	"strings"
	"testing"
)

// The cases are the real ones. Every "must be reported" is a comment somebody
// actually shipped; every "must NOT be reported" is a shape that would produce
// a false alarm, which is the failure that gets a check switched off.

func TestQuantityAgreement(t *testing.T) {
	// The document this hook was written after: the measurements a comment
	// nearby got wrong from memory.
	doc := `| JSON (before) | 27.70 MB -> 2.80 MB gzipped | 277 B | 315.1 ms |
	        | columnar (now) | 2.38 MB -> 749 KB gzipped | 23.8 B raw / 7.5 B gzipped | 63.2 ms |`

	tests := []struct {
		name  string
		claim string
		agree bool
	}{
		// The actual defect: the doc says 7.5, the comment said ~10.
		{"recalled figure that is simply wrong", "~10 B/event", false},
		{"the other recalled figure", "~283 B/event", false},

		// Rounding is honest and must pass, or nobody can write "~24".
		{"rounded to zero decimals", "~24 B/event", true},
		{"exact with decimals", "23.8 B/event", true},
		{"exact integer", "277 B/event", true},
		{"a duration from the table", "63.2 ms", true},
		{"rounded duration", "63 ms", true},

		// Precision is a promise: claiming two decimals means two must agree.
		{"over-precise against a rounded source", "23.9 B/event", false},

		// A scaled restatement of the same measurement.
		{"KB quoted as MB", "0.749 MB", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Analyze(Block{Text: "the payload is " + tc.claim + " see docs/x.md"})
			if len(c.Quantities) == 0 {
				t.Fatalf("no quantity parsed from %q", tc.claim)
			}
			if got := c.Quantities[0].agreesWith(doc); got != tc.agree {
				t.Errorf("%q: agreesWith = %v, want %v", tc.claim, got, tc.agree)
			}
		})
	}
}

// A bare number is not a measurement. Treating "step 2" or "v1" as a claim is
// how a checker becomes noise nobody reads.
func TestBareNumbersAreNotQuantities(t *testing.T) {
	for _, text := range []string{
		"there are 3 cases below",
		"see step 2",
		"the v1 layout",
		"RFC 8628",
		"index 0 is the reserved empty string",
	} {
		if c := Analyze(Block{Text: text}); len(c.Quantities) > 0 {
			t.Errorf("%q: parsed %v as a measurement claim", text, c.Quantities)
		}
	}
}

func TestReferenceExtraction(t *testing.T) {
	c := Analyze(Block{Text: "TestTimelineFrameBudget gates on the average; " +
		"see docs/timeline-wire-format.md and `internal/api/timeline.go`. " +
		"The helper `fetchTimelineBytes` throws."})

	for _, want := range []string{
		"TestTimelineFrameBudget", "docs/timeline-wire-format.md",
		"internal/api/timeline.go", "fetchTimelineBytes",
	} {
		if !contains(c.References, want) {
			t.Errorf("missed reference %q in %v", want, c.References)
		}
	}
	if !contains(c.Docs, "docs/timeline-wire-format.md") {
		t.Errorf("the cited doc was not recorded as evidence: %v", c.Docs)
	}
}

// Prose must not be mined for symbols. An ordinary English sentence naming
// nothing should yield nothing.
func TestProseIsNotMinedForSymbols(t *testing.T) {
	c := Analyze(Block{Text: "Absorb the state a GitHub response contains and " +
		"rebuild the response from it, dropping every URL field."})
	if len(c.References) != 0 {
		t.Errorf("invented references from prose: %v", c.References)
	}
}

func TestHedgeDetection(t *testing.T) {
	reported := []string{
		"this probably keeps the lock held",
		"presumably the caller drains it",
		"I believe the retry is bounded",
		"seems to be safe under concurrency",
	}
	for _, text := range reported {
		if c := Analyze(Block{Text: text}); len(c.Hedges) == 0 {
			t.Errorf("%q: hedge not detected", text)
		}
	}
	// "should" is normative here, not unsure -- flagging it would punish
	// correct contract documentation.
	notReported := []string{
		"callers should drain the manager before closing the DB",
		"every response-cache table should be pruned on write",
	}
	for _, text := range notReported {
		if c := Analyze(Block{Text: text}); len(c.Hedges) > 0 {
			t.Errorf("%q: false hedge %v", text, c.Hedges)
		}
	}
}

func TestJudgmentGate(t *testing.T) {
	// A figure WITH a cited doc is settled mechanically: no model needed.
	settled := Analyze(Block{Text: "23.8 B/event, see docs/x.md"})
	if settled.NeedsJudgment() {
		t.Error("a figure with a cited doc should not need a model")
	}
	// A figure with no source cannot be settled by looking.
	unsourced := Analyze(Block{Text: "decodes 5x faster in the browser"})
	if !unsourced.NeedsJudgment() {
		t.Error("an unsourced measurement needs judgment")
	}
	// A causal claim always does -- this is the invented-history shape.
	causal := Analyze(Block{Text: "a producer imports this package rather than " +
		"reimplementing it, which is how the format used to drift"})
	if !causal.NeedsJudgment() {
		t.Error("a causal claim needs judgment")
	}
	// Ordinary description costs nothing.
	plain := Analyze(Block{Text: "bump the counter and return"})
	if plain.NeedsJudgment() {
		t.Error("a plain comment must not reach the model")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestExcerptStaysOneLine(t *testing.T) {
	got := excerpt("first line\nsecond line\n\nthird")
	if strings.Contains(got, "\n") {
		t.Errorf("excerpt kept newlines: %q", got)
	}
}

// A filename shown as an EXAMPLE is a specimen, not a claim that the file is
// here. Reporting those is how a tool ends up flagging its own documentation --
// which this one did, before the quoting rule existed.
func TestQuotedExamplesAreNotCitations(t *testing.T) {
	c := Analyze(Block{Text: `A partial path ("assets/timeline.js") is relative to the citing file. ` +
		"The real citation is `internal/api/timeline.go`."})
	if contains(c.References, "assets/timeline.js") {
		t.Errorf("a quoted example was treated as a citation: %v", c.References)
	}
	if !contains(c.References, "internal/api/timeline.go") {
		t.Errorf("a backticked citation was lost: %v", c.References)
	}
}

// URLs and absolute paths are full of path-shaped text that is not a file here.
func TestURLsAndAbsolutePathsAreNotCitations(t *testing.T) {
	c := Analyze(Block{Text: "imported from https://sites.pazer.build/js-snippets/branch/library/ui/timeline-view.js " +
		"and dumped to /tmp/timeline-view.js"})
	if len(c.References) != 0 {
		t.Errorf("mined a URL or an absolute path for citations: %v", c.References)
	}
}

// Comment prose wraps, so a quoted example routinely spans lines. Missing that
// was the last false positive this tool reported against its own source.
func TestQuotedExampleSpanningLines(t *testing.T) {
	c := Analyze(Block{Text: "a build output cited by partial path (\"the\nassets/timeline.js the embed names\") resolves."})
	if contains(c.References, "assets/timeline.js") {
		t.Errorf("a wrapped quoted example was treated as a citation: %v", c.References)
	}
}
