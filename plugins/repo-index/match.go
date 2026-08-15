package main

import (
	"regexp"
	"sort"
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

// identifierWeight is what one name or topic hit is worth. It is the whole
// threshold on its own: naming a repository is enough to mean it.
const identifierWeight = 2

// threshold is the score a repository needs before it costs an injection. One
// description word never reaches it, so a prompt about writing a haiku does
// not pull in every repository whose README happens to say "write".
const threshold = 2

// match scores every repo against the prompt. An identifier hit counts double
// a description word, and several hits beat one, so the repository a prompt
// actually names ranks above one that shares a word with it.
func match(prompt string, repos []Repo) []Hit {
	var hits []Hit
	for _, r := range repos {
		var phrases []string
		score := 0
		for _, phrase := range r.Match {
			if phrasePattern(phrase).MatchString(prompt) {
				phrases = append(phrases, phrase)
				score += identifierWeight
			}
		}
		for _, term := range r.Terms {
			if phrasePattern(term).MatchString(prompt) {
				phrases = append(phrases, term)
				score++
			}
		}
		if score >= threshold {
			hits = append(hits, Hit{Repo: r, Score: score, Phrases: phrases})
		}
	}
	sortByName(hits)
	return hits
}

// sortByName keeps the output stable when two repos score the same.
func sortByName(hits []Hit) {
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		return hits[a].Repo.Name < hits[b].Repo.Name
	})
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
