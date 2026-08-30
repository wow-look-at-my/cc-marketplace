// counts.go finds the one sentence shape this plugin refuses: a cardinal
// number quantifying a plural noun, in the prose of a markdown document.
//
// "This repo's 15 plugins", "the four rules below", "three sections": each
// states how many of something exists at the moment it was typed, and the edit
// that adds an item leaves it wrong with nothing to notice. Describing what is
// there and letting the reader count never goes stale.
package main

import (
	"regexp"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
)

// Hit is a count found in a document: the phrase itself and the line holding it.
type Hit struct {
	Phrase string
	Line   string
}

// numberWords are the cardinals spelled out. "One" is deliberately absent: in
// English prose it is overwhelmingly a pronoun ("the wrong one", "one of them")
// and matching it would refuse far more good writing than bad. A document that
// says "one plugin" is also the document a single edit makes wrong, so this is
// a known, deliberate gap rather than an oversight -- the same boundary
// link-all-refs draws around a bare owner/repo.
var numberWords = strings.Join([]string{
	"two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
	"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen",
	"seventeen", "eighteen", "nineteen", "twenty", "thirty", "forty", "fifty",
	"sixty", "seventy", "eighty", "ninety", "hundred", "thousand",
	"dozen", "both",
}, "|")

// countPattern matches a cardinal followed by a plural noun, allowing adjectives
// and possessives between them ("15 published marketplace plugins"). The noun is
// captured so measureNouns can excuse it.
var countPattern = regexp.MustCompile(
	`(?i)\b(\d{1,4}|` + numberWords + `)\s+((?:[a-z][a-z-]*(?:'s)?\s+){0,3}?)([a-z][a-z-]{2,}s)\b`)

// measureNouns end a phrase that measures rather than counts. A limit, a size,
// a duration and a version are all still true after somebody adds a plugin;
// only a tally of what exists goes stale. Excusing them is what keeps this
// plugin off ordinary technical prose ("20 seconds", "500 lines", "4 attempts
// per minute") instead of firing on every page.
var measureNouns = set.Of[string](
	"seconds", "minutes", "hours", "days",
	"weeks", "months", "years", "ms", "ns",
	"bytes", "kilobytes", "megabytes", "gigabytes",
	"kbs", "mbs", "gbs", "kibs", "mibs",
	"lines", "chars", "characters", "words",
	"columns", "pixels", "px", "points",
	"times", "attempts", "retries", "levels",
	"degrees", "percents", "spaces", "tabs",
	"digits", "bits", "requests", "tokens",
	"milliseconds", "nanoseconds", "microseconds",
)

// gapStopWords are the function words that prove the noun after them is not
// what the number counts. Without this check "Version 2 of the format drops the
// header" reads as a count of "drops", because a bare adjective run happily
// swallows "of the format".
var gapStopWords = set.Of[string](
	"of", "the", "a", "an", "in", "on", "to", "for", "and", "or", "is", "are",
	"was", "were", "that", "this", "with", "from", "by", "at", "as", "but",
	"if", "so", "than", "then", "when", "while", "not", "no", "it", "its",
)

// FindCounts returns every count stated in the prose of a markdown document.
// Fenced code, indented code, HTML comments and YAML frontmatter are skipped
// whole, and inline backtick spans are blanked within a line: a number inside
// verbatim machinery is a literal, not the document's own claim about itself.
func FindCounts(doc string) []Hit {
	var hits []Hit
	for _, line := range prose(doc) {
		text := blankInlineCode(line)
		for _, m := range countPattern.FindAllStringSubmatch(text, -1) {
			if measureNouns.Contains(strings.ToLower(m[3])) || hasStopWord(m[2]) {
				continue
			}
			hits = append(hits, Hit{Phrase: strings.TrimSpace(m[0]), Line: strings.TrimSpace(line)})
		}
	}
	return hits
}

// hasStopWord reports whether the words between the number and the noun contain
// a function word.
func hasStopWord(gap string) bool {
	for _, w := range strings.Fields(gap) {
		if gapStopWords.Contains(strings.ToLower(w)) {
			return true
		}
	}
	return false
}

var (
	fenceLine       = regexp.MustCompile("^\\s*(```|~~~)")
	frontmatterLine = regexp.MustCompile(`^---\s*$`)
	inlineCode      = regexp.MustCompile("`[^`]*`")
)

// prose returns the lines of doc that carry the document's own voice.
func prose(doc string) []string {
	lines := strings.Split(doc, "\n")
	var out []string
	inFence, inComment := false, false
	start := 0
	if len(lines) > 0 && frontmatterLine.MatchString(lines[0]) {
		for i := 1; i < len(lines); i++ {
			if frontmatterLine.MatchString(lines[i]) {
				start = i + 1
				break
			}
		}
	}
	for _, line := range lines[start:] {
		switch {
		case fenceLine.MatchString(line):
			inFence = !inFence
			continue
		case inFence:
			continue
		case strings.Contains(line, "<!--"):
			inComment = !strings.Contains(line, "-->")
			continue
		case inComment:
			inComment = !strings.Contains(line, "-->")
			continue
		case strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t"):
			continue // indented code
		}
		out = append(out, line)
	}
	return out
}

// blankInlineCode replaces the contents of each backtick span with spaces,
// keeping every byte offset so the reported line still reads correctly.
func blankInlineCode(line string) string {
	return inlineCode.ReplaceAllStringFunc(line, func(s string) string {
		return strings.Repeat(" ", len(s))
	})
}

// IsMarkdown reports whether a path names a document this plugin governs.
func IsMarkdown(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}
