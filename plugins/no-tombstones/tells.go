// tells.go holds what a tombstone looks like on the page.
//
// A tombstone is a comment about a state the code is no longer in, or about
// the diff rather than the file. Two properties separate one from a comment
// worth keeping. Its REFERENT is gone: the flag, the test, the old spelling it
// names does not exist any more. Or its AUDIENCE is the reviewer: it argues for
// the change instead of telling the next editor what breaks.
//
// The table below matches the surface of both. Each entry names the tell so the
// refusal can say which property failed, because "do not write that" without a
// reason costs a round trip to find out.
package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"regexp"
	"strings"
)

// Hit is one tombstone found in added text.
type Hit struct {
	Tell   string // the rule that fired, for the refusal to name
	Phrase string // the matched words
	Line   string // the line they sit on
}

// tell is one recognisable shape. Name is what the refusal prints.
type tell struct {
	name string
	re   *regexp.Regexp
}

// changeParticiples are the verbs that describe an edit rather than a state.
// A comment reaches for one of these only to narrate what a commit did.
const changeParticiples = `renamed|removed|added|deleted|moved|replaced|` +
	`introduced|dropped|split|merged|reverted|refactored|extracted|migrated|` +
	`deprecated|rewritten|rewrote|bumped|reworked|consolidated|inlined|hoisted`

// tells is the table. It is data: extending this plugin is adding a row.
//
// Every row traces to a shape that survives paraphrase badly enough to be worth
// matching by surface. The tiers that do not depend on wording at all are the
// comment-volume cap in FindTombstones and the dead-referent corroborator in
// referents.go.
var tells = []tell{
	// A date is the purest tombstone marker: git already timestamps the line,
	// so a date in a comment can only be narrating when something happened.
	{"a date", regexp.MustCompile(`(?i)\b(?:19|20)\d{2}-\d{2}-\d{2}\b`)},
	{"a date", regexp.MustCompile(`(?i)\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(?:\d{1,2},?\s+)?(?:19|20)\d{2}\b`)},

	// A change reference points at the commit that made the edit. The commit
	// message is where that belongs; here it rots the moment the ref is stale.
	{"a change reference", regexp.MustCompile(`(?i)\b(?:pr|pull request|issue|ticket|commit)\s+#?\d+\b`)},
	{"a change reference", regexp.MustCompile(`(?i)\b[a-z0-9][\w.-]*/[\w.-]+#\d+\b`)},

	// A contrast marker states a "then" the reader cannot see. Whatever it is
	// contrasting with left the tree. These sit above the general former-state
	// rules because several sentences satisfy both, and the first match names
	// the finding: the more specific rule says more about what to rewrite.
	{"a then-and-now contrast", regexp.MustCompile(`(?i)\brather than (?:the )?(?:old|former|previous|legacy)\b`)},
	{"a then-and-now contrast", regexp.MustCompile(`(?i)\binstead of (?:the )?(?:old|former|previous|legacy)\b`)},
	{"a then-and-now contrast", regexp.MustCompile(`(?i)\bwhere (?:it|this|that) (?:used to|once)\b`)},

	// The referent is gone: the sentence's subject is a former state.
	{"a former state", regexp.MustCompile(`(?i)\bused to\b`)},
	{"a former state", regexp.MustCompile(`(?i)\b(?:previously|formerly|originally|hitherto)\b`)},
	{"a former state", regexp.MustCompile(`(?i)\bno longer\b|\banymore\b|\bnowadays\b|\bthese days\b`)},
	{"a former state", regexp.MustCompile(`(?i)\bthe (?:former|old|previous|legacy|original) \w+`)},
	{"a former state", regexp.MustCompile(`(?i)\b(?:was|were|has been|have been|had been|got|gets|is now|are now) (?:` + changeParticiples + `)\b`)},
	{"a former state", regexp.MustCompile(`(?i)\b(?:we|this|it|that) (?:` + changeParticiples + `)\b`)},
	{"a former state", regexp.MustCompile(`(?i)\bthis (?:replaces|supersedes|used to)\b`)},
	{"a former state", regexp.MustCompile(`(?i)\bstopped (?:being|working|doing)\b|\bstarted (?:being|failing)\b`)},

	// The audience is the reviewer, not the next editor.
	{"an address to the reviewer", regexp.MustCompile(`(?i)\bthis (?:pr|pull request|change|diff|commit|patch|cl)\b`)},
	{"an address to the reviewer", regexp.MustCompile(`(?i)\bworth (?:noting|your attention|knowing here)\b`)},
	{"an address to the reviewer", regexp.MustCompile(`(?i)\b(?:as|when) requested\b|\bwas never requested\b|\bnobody asked\b`)},
	{"an address to the reviewer", regexp.MustCompile(`(?i)\bdo not (?:reintroduce|add this back|bring (?:it|this) back)\b`)},
	{"an address to the reviewer", regexp.MustCompile(`(?i)\bper the (?:review|reviewer|feedback|comment)\b`)},
	{"an address to the reviewer", regexp.MustCompile(`(?i)\b(?:flagging|to be clear|just to note|for the reviewer)\b`)},

	// A defence of the change: an argument, aimed at somebody deciding whether
	// to accept it, parked permanently in a file.
	{"a defence of the change", regexp.MustCompile(`(?i)\bthat is not (?:tidying|cleanup|cosmetic|churn|a rename|style|gratuitous)\b`)},
	{"a defence of the change", regexp.MustCompile(`(?i)\bthis is not (?:just )?(?:tidying|cleanup|cosmetic|churn|a rename|refactoring for)\b`)},
	{"a defence of the change", regexp.MustCompile(`(?i)\bwould have been\b|\bwould otherwise have\b`)},
	{"a defence of the change", regexp.MustCompile(`(?i)\bnot (?:tidying|scope creep|gold.?plating)\b`)},

	// Somebody's instruction, quoted. It reads as authority the code cannot
	// check, and it dates the moment they said it.
	{"a quoted instruction", regexp.MustCompile(`(?i)\bthe (?:owner|operator|user|reviewer|maintainer) (?:said|says|ruled|asked|wants|requested)\b`)},
	{"a quoted instruction", regexp.MustCompile(`(?i)\bper (?:the )?(?:owner|operator|user|maintainer)\b`)},
	{"a quoted instruction", regexp.MustCompile(`(?i)\bby (?:owner|operator) (?:ruling|request|decree)\b`)},

	// An experiment reported to the reviewer. It happened once, to a tree that
	// no longer exists, and nothing re-runs it.
	{"a report of an experiment", regexp.MustCompile(`(?i)\bnegative control\b`)},
	{"a report of an experiment", regexp.MustCompile(`(?i)\b(?:i|we) (?:ran|tried|tested|measured|verified this by|checked this by)\b`)},
	{"a report of an experiment", regexp.MustCompile(`(?i)\bverified by (?:breaking|deleting|removing|reverting)\b`)},
	{"a report of an experiment", regexp.MustCompile(`(?i)\brun before trusting\b`)},
}

// FindTombstones returns every tombstone the added text carries.
//
// blocks are the comment blocks (or, for a document, the prose paragraphs) the
// write introduces. maxLines caps a single block: a tombstone is almost always
// surplus text, so volume catches the essay whose every individual sentence
// reads as true and current. That cap is the one tier no rewording defeats.
func FindTombstones(blocks []Block, maxLines int) []Hit {
	var hits []Hit
	seen := set.New[string]()
	for _, b := range blocks {
		if maxLines > 0 && b.Lines > maxLines {
			hits = append(hits, Hit{
				Tell:   "a comment block of " + itoa(b.Lines) + " lines",
				Phrase: firstLine(b.Text),
				Line:   firstLine(b.Text),
			})
		}
		for _, line := range strings.Split(b.Text, "\n") {
			for _, t := range tells {
				at := t.re.FindStringIndex(line)
				if at == nil {
					continue
				}
				phrase := strings.TrimSpace(line[at[0]:at[1]])
				key := t.name + "\x00" + strings.ToLower(phrase)
				if seen.Contains(key) {
					continue
				}
				seen.Add(key)
				hits = append(hits, Hit{Tell: t.name, Phrase: phrase, Line: strings.TrimSpace(line)})
			}
		}
	}
	return hits
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
