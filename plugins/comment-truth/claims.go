package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// TRIAGE. Most comments assert nothing a checker could be right or wrong about
// ("// bump the counter"). Sorting the few that DO claim something, by what
// kind of claim it is, decides what each one costs: two kinds are settled
// mechanically for free, and only the rest are worth a model's attention.
//
// The kinds are not invented -- each is a defect this hook exists because
// somebody shipped.

type ClaimKind string

const (
	// A name the comment cites: a symbol, a test, a file. Settled by looking.
	ClaimReference ClaimKind = "reference"
	// A measurement. Settled against the doc the comment itself cites; needs a
	// model only when it cites nothing.
	ClaimQuantity ClaimKind = "quantity"
	// "always", "never", "every", "only" -- the claims most likely to be
	// almost-true, and never checkable without reading the code.
	ClaimUniversal ClaimKind = "universal"
	// "because", "which is why" -- an invented cause survives review because
	// nobody can disprove a story.
	ClaimCausal ClaimKind = "causal"
	// A hedge. Settled immediately: an unverified claim with an escape hatch.
	ClaimHedge ClaimKind = "hedge"
)

// Claims is what a block asserts, by kind.
type Claims struct {
	References []string
	Quantities []Quantity
	// Docs are paths the block cites -- the evidence for its own figures.
	Docs      []string
	Universal []string
	Causal    []string
	Hedges    []string
}

// NeedsJudgment reports whether anything is left that mechanical checks cannot
// settle. This is the gate in front of the only expensive stage.
func (c Claims) NeedsJudgment() bool {
	return len(c.Universal) > 0 || len(c.Causal) > 0 ||
		(len(c.Quantities) > 0 && len(c.Docs) == 0)
}

// Quantity is a measurement claim: the number as written, plus the unit that
// makes it a measurement rather than an index.
type Quantity struct {
	Raw   string
	Value float64
	// Decimals is the precision the comment chose, which sets how exactly a
	// source has to agree with it.
	Decimals int
	Unit     string
}

var (
	// A figure with a unit or a multiplier. A bare integer is deliberately NOT
	// a quantity claim: "step 1", "v2" and "the 3 cases" are not measurements,
	// and treating them as such is how a checker becomes noise.
	quantityRe = regexp.MustCompile(`(?i)([~≈<>]?\s*)(\d+(?:\.\d+)?)\s*(x\b|%|ms\b|µs\b|us\b|ns\b|s\b|m\b|h\b|d\b|[kmgt]?i?b(?:/\w+)?\b|bytes?(?:/\w+)?\b|fps\b|hz\b|lines?\b|frames?\b|allocs?\b)`)

	// Backticked names, test/benchmark identifiers, and paths with a known
	// source extension. Deliberately narrow: an unqualified CamelCase word in
	// prose is not reliably a symbol, and guessing produces false alarms that
	// teach people to ignore the check.
	backtickRe = regexp.MustCompile("`([A-Za-z_][\\w./-]*(?:\\(\\))?)`")
	testNameRe = regexp.MustCompile(`\b((?:Test|Benchmark|Fuzz|Example)[A-Z]\w+)\b`)
	pathRe     = regexp.MustCompile(`\b([\w][\w./-]*\.(?:go|ts|tsx|js|mjs|jsx|md|ya?ml|json|sql|wgsl|glsl|sh|py|rs|toml))\b`)

	universalRe = regexp.MustCompile(`(?i)\b(always|never|every|all of|none of|only|cannot|can't|must not|must|guaranteed|impossible|no way to)\b`)
	causalRe    = regexp.MustCompile(`(?i)\b(because|which is (?:how|why)|so that|the reason|this is why|used to|caused by|due to|otherwise)\b`)

	// Epistemic hedges only. "should" alone is excluded: "callers should drain"
	// is a normative statement about the contract, not an unsure one.
	hedgeRe = regexp.MustCompile(`(?i)\b(probably|presumably|i believe|i think|seems to|appears to|as far as i know|afaik|not sure|might be wrong|maybe|possibly)\b`)
)

// Analyze sorts one block's claims.
func Analyze(b Block) Claims {
	var c Claims
	text := b.Text

	seen := map[string]bool{}
	addRef := func(s string) {
		s = strings.TrimSuffix(s, "()")
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		c.References = append(c.References, s)
	}
	for _, m := range backtickRe.FindAllStringSubmatch(text, -1) {
		addRef(m[1])
	}
	for _, m := range testNameRe.FindAllStringSubmatch(text, -1) {
		addRef(m[1])
	}
	// Paths are read from prose only after the things that merely LOOK like
	// citations are removed: URLs, absolute paths, and double-quoted spans.
	// The quoting rule is the convention this relies on -- a backticked or
	// bare name is a citation and gets checked, a "quoted" one is an example
	// being shown, and checking specimens is how a tool starts reporting the
	// documentation of itself.
	for _, m := range pathRe.FindAllStringSubmatch(citations(text), -1) {
		addRef(m[1])
		if isDoc(m[1]) {
			c.Docs = appendUnique(c.Docs, m[1])
		}
	}

	for _, m := range quantityRe.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		dec := 0
		if dot := strings.Index(m[2], "."); dot >= 0 {
			dec = len(m[2]) - dot - 1
		}
		c.Quantities = append(c.Quantities, Quantity{
			Raw: strings.TrimSpace(m[0]), Value: v, Decimals: dec, Unit: strings.ToLower(m[3]),
		})
	}

	c.Universal = uniqueMatches(universalRe, text)
	c.Causal = uniqueMatches(causalRe, text)
	c.Hedges = uniqueMatches(hedgeRe, text)
	return c
}

// urlRe matches a URL and everything path-like trailing it.
var urlRe = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)

// absPathRe matches a filesystem path rooted outside the repository.
var absPathRe = regexp.MustCompile(`(^|\s)/\S+`)

// quotedRe matches a double- or single-quoted span: an example, not a citation.
// Comment prose WRAPS, so a quoted example routinely spans lines and the span
// must too; the length bound keeps an unbalanced quote from swallowing the rest
// of the comment.
var quotedRe = regexp.MustCompile(`(?s)"[^"]{0,200}"|'[^']{0,200}'`)

// citations strips everything path-shaped that is not a claim about this
// repository: URLs, absolute filesystem paths, and quoted examples.
func citations(text string) string {
	text = urlRe.ReplaceAllString(text, " ")
	text = absPathRe.ReplaceAllString(text, " ")
	return quotedRe.ReplaceAllString(text, " ")
}

func isDoc(path string) bool {
	return strings.HasSuffix(path, ".md")
}

func uniqueMatches(re *regexp.Regexp, text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllString(text, -1) {
		k := strings.ToLower(m)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, m)
	}
	return out
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// numberRe finds every figure in a source document, so a comment's number can
// be checked against what the document actually measured.
var numberRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

// agreesWith reports whether a document contains a figure the comment's number
// could honestly be a rounding of.
//
// TWO things make this precise enough to be worth running.
//
// The comparison is scoped to figures carrying the SAME UNIT. Matching a bare
// number against the whole document is far too permissive: a claim of "~10
// B/event" is satisfied by any stray 10 -- a version, a percentage, a count --
// and the check passes a figure that is simply wrong. Against the byte figures
// only, it fails, which is the answer.
//
// And the tolerance is the comment's OWN precision. A comment saying "~24
// B/event" over a document measuring 23.8 is CORRECT; demanding a literal match
// would make the check unusable and it would be switched off. So a document
// figure is rounded to however many decimals the comment chose: "~24" accepts
// 23.8, "~10" rejects 7.5. Writing a number to no decimal places is choosing
// how exactly the source has to agree.
func (q Quantity) agreesWith(doc string) bool {
	for _, cand := range figuresInUnit(doc, q.Unit) {
		if q.matches(cand) {
			return true
		}
		// The same measurement restated at another scale: a document's 749 KB
		// quoted as 0.749 MB.
		for _, scale := range []float64{1000, 1024, 0.001, 1.0 / 1024} {
			if q.matches(cand * scale) {
				return true
			}
		}
	}
	return false
}

// matches accepts a source figure the comment could honestly be reporting:
// rounded to the comment's precision, or TRUNCATED to it. Writing 23.8 as "~23"
// is as ordinary as writing it "~24", and a checker that accepts only one of
// them is picking a house style rather than finding errors. Neither reading
// rescues a wrong number: 7.5 is "~7" or "~8", never "~10".
func (q Quantity) matches(cand float64) bool {
	p := math.Pow(10, float64(q.Decimals))
	want := roundTo(q.Value, q.Decimals)
	return roundTo(cand, q.Decimals) == want || math.Trunc(cand*p)/p == want
}

// figuresInUnit pulls every figure a document states in the same unit family as
// the claim, so "B/event" is checked against bytes and not against a stray
// integer somewhere else on the page.
func figuresInUnit(doc, unit string) []float64 {
	family := unitFamily(unit)
	var out []float64
	for _, m := range quantityRe.FindAllStringSubmatch(doc, -1) {
		if unitFamily(strings.ToLower(m[3])) != family {
			continue
		}
		if v, err := strconv.ParseFloat(m[2], 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// unitFamily collapses spellings that measure the same thing, so a comment in
// "B/event" is checked against a document written in "B", and a duration in
// "ms" is not checked against a count of frames.
func unitFamily(unit string) string {
	u := strings.TrimSpace(strings.ToLower(unit))
	if i := strings.Index(u, "/"); i >= 0 {
		u = u[:i]
	}
	switch u {
	case "b", "kb", "mb", "gb", "tb", "kib", "mib", "gib", "tib", "byte", "bytes":
		return "bytes"
	case "ms", "s", "m", "h", "d", "ns", "us", "µs":
		return "duration"
	case "x":
		return "ratio"
	case "%":
		return "percent"
	}
	return u
}

func roundTo(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}

func (q Quantity) String() string { return fmt.Sprintf("%s", q.Raw) }
