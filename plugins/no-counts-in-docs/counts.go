// counts.go finds the one sentence shape this plugin refuses: an inventory
// count, where a document says how many of something the repository, the
// project or the document itself currently holds.
//
// A count needs two halves to qualify, and the second half is what keeps this
// plugin usable. The FRAME says the sentence is talking about what is here --
// a possessive ("this repo's"), a having verb ("it ships", "there are"), or a
// document deictic ("the rules below"). The QUANTITY is a cardinal governing a
// plural noun. Both together is an inventory that the next commit falsifies
// with nothing to notice; the quantity alone is ordinary technical prose, and
// refusing that would fire on every page and get the plugin uninstalled.
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
// says "one plugin" is also a document a single edit makes wrong, so this is a
// known, deliberate gap rather than an oversight -- the same boundary
// link-all-refs draws around a bare owner/repo.
const numberWords = `two|three|four|five|six|seven|eight|nine|ten|` +
	`eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|` +
	`nineteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|dozen`

// quantity is a cardinal governing a plural noun, with adjectives allowed
// between them ("15 published marketplace plugins"). The digit case is guarded
// in Go rather than here, by looking at the character in front of the match:
// RE2 has no lookbehind, and spending the frame's own separating space on a
// negated class would stop the frame from ever meeting the quantity.
const quantity = `(?:\d{1,4}|\b(?:` + numberWords + `))` +
	`\s+(?:[a-z][a-z-]*\s+){0,3}?[a-z][a-z-]{2,}s\b`

// possessiveFrame is a determiner claiming the things belong here: "this
// repo's 15 plugins", "the payload's four steps", "our three servers".
var possessiveFrame = regexp.MustCompile(
	`(?i)\b(?:this|these|our|the)\s+(?:[a-z][a-z-]*\s+){0,2}?[a-z][a-z-]*'s\s+(` + quantity + `)`)

// havingFrame is a verb asserting possession or extent: "it ships two hooks",
// "there are three sections", "the plugin registers 15 servers".
var havingFrame = regexp.MustCompile(
	`(?i)\b(?:has|have|had|holds?|ships?|carries|carry|contains?|covers?|` +
		`includes?|lists?|defines?|registers?|installs?|answers?|serves?|` +
		`provides?|exposes?|declares?|embeds?|bundles?|comprises?|spans?|` +
		`there\s+(?:are|were))\s+(?:only\s+|just\s+|exactly\s+|all\s+)?(` + quantity + `)`)

// deicticFrame points inside the document: "the four rules below", "the three
// steps above". The count is of what this page shows, so editing the page
// breaks it.
var deicticFrame = regexp.MustCompile(
	`(?i)\b(?:the|these|those)\s+(` + quantity + `)\s+(?:\S+\s+){0,2}?(?:below|above|here)\b`)

var frames = []*regexp.Regexp{possessiveFrame, havingFrame, deicticFrame}

// measureNouns end a quantity that measures rather than counts. A limit, a size
// and a duration stay true after somebody adds a plugin, so even inside a frame
// ("the read has 20 seconds") there is nothing to go stale.
var measureNouns = set.Of[string](
	"seconds", "minutes", "hours", "days", "weeks", "months", "years",
	"milliseconds", "microseconds", "nanoseconds", "ms", "ns",
	"bytes", "kilobytes", "megabytes", "gigabytes", "kbs", "mbs", "gbs",
	"lines", "chars", "characters", "words", "columns", "pixels", "px",
	"times", "attempts", "retries", "levels", "degrees", "percents",
	"spaces", "tabs", "digits", "bits", "requests", "tokens",
)

// gapStopWords are the function words that prove the noun after them is not
// what the number counts. Without this check "it has 2 of the format drops"
// reads as a count of "drops", because a bare adjective run happily swallows
// "of the format".
var gapStopWords = set.Of[string](
	"of", "the", "a", "an", "in", "on", "to", "for", "and", "or", "is", "are",
	"was", "were", "that", "this", "with", "from", "by", "at", "as", "but",
	"if", "so", "than", "then", "when", "while", "not", "no", "it", "its",
)

// FindCounts returns every inventory count stated in the prose of a markdown
// document. Fenced code, indented code, HTML comments and YAML frontmatter are
// skipped whole, and inline backtick spans are blanked within a line: a number
// inside verbatim machinery is a literal, not the document's own claim about
// itself, and it is also how this plugin's own documentation quotes the shape
// it refuses.
func FindCounts(doc string) []Hit {
	var hits []Hit
	for _, line := range prose(doc) {
		text := blankInlineCode(line)
		seen := set.New[string]()
		for _, frame := range frames {
			for _, at := range frame.FindAllStringSubmatchIndex(text, -1) {
				start, end := at[2], at[3]
				phrase := text[start:end]
				if continuesANumber(text, start) || !isInventory(phrase) || seen.Contains(phrase) {
					continue
				}
				seen.Add(phrase)
				hits = append(hits, Hit{Phrase: phrase, Line: strings.TrimSpace(line)})
			}
		}
	}
	return hits
}

// continuesANumber reports that the character in front of a match makes it the
// tail of a longer number, so "pre-2.1.205 clients" is not read as a count of
// 205 things.
func continuesANumber(text string, start int) bool {
	if start == 0 {
		return false
	}
	c := text[start-1]
	return c == '.' || (c >= '0' && c <= '9')
}

// isInventory rejects a quantity whose noun measures, and one reached through a
// function word.
func isInventory(phrase string) bool {
	words := strings.Fields(strings.ToLower(phrase))
	if len(words) < 2 {
		return false
	}
	if measureNouns.Contains(words[len(words)-1]) {
		return false
	}
	for _, w := range words[1 : len(words)-1] {
		if gapStopWords.Contains(w) {
			return false
		}
	}
	return true
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
