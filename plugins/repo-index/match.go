package main

import (
	"regexp"
	"strings"
	"sync"
)

// maxSuggestions caps one prompt's injection. A wall of repos costs the model
// more attention than it repays.
const maxSuggestions = 3

// Hit is a repo the prompt matched, with the phrases that matched it.
type Hit struct {
	Repo    Repo
	Score   int
	Phrases []string
}

var (
	patternCache   = map[string]*regexp.Regexp{}
	patternCacheMu sync.Mutex
)

// phrasePattern matches a phrase on whole words. The boundary class excludes
// letters, digits, underscore, and hyphen, so "buildhost" does not match
// "go-buildhost" and "xsd" does not match "xsdfoo".
func phrasePattern(phrase string) *regexp.Regexp {
	patternCacheMu.Lock()
	defer patternCacheMu.Unlock()
	if re, ok := patternCache[phrase]; ok {
		return re
	}
	const edge = `[^\p{L}\p{N}_-]`
	re := regexp.MustCompile(`(?i)(?:^|` + edge + `)` + regexp.QuoteMeta(phrase) + `(?:$|` + edge + `)`)
	patternCache[phrase] = re
	return re
}

// match scores every repo against the prompt. The score is the number of
// distinct phrases that hit, so a prompt about several parts of one repo ranks
// that repo above an incidental single hit.
func match(prompt string, repos []Repo) []Hit {
	var hits []Hit
	for _, r := range repos {
		var phrases []string
		for _, phrase := range r.Match {
			if phrasePattern(phrase).MatchString(prompt) {
				phrases = append(phrases, phrase)
			}
		}
		if len(phrases) > 0 {
			hits = append(hits, Hit{Repo: r, Score: len(phrases), Phrases: phrases})
		}
	}
	sortByName(hits)
	return hits
}

// render writes the block that goes into the prompt. It states the repo, the
// link, and one description, and nothing else.
func render(hits []Hit) string {
	var b strings.Builder
	b.WriteString("Repositories that look relevant to this request. ")
	b.WriteString("Read one before you build something it already provides. ")
	b.WriteString("Each repo appears here at most once per session.\n")
	for _, h := range hits {
		b.WriteString("\n- **")
		b.WriteString(h.Repo.Name)
		b.WriteString("** -- ")
		b.WriteString(h.Repo.URL)
		b.WriteString("\n  ")
		b.WriteString(h.Repo.Description)
		b.WriteString("\n")
	}
	return b.String()
}
