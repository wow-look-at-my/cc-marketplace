package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"regexp"
	"strings"
)

// Repo is one entry in the built index. Every field comes from GitHub: the
// description is the repository's own, and the match phrases are its name and
// its topics. Nothing here is written by hand, so nothing here can disagree
// with the repository it describes.
type Repo struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	// Match holds identifiers: the repository's name, the parts of it, and
	// the topics its owner set. A prompt that says one of these means this
	// repository.
	Match []string `json:"match"`
	// Terms holds words taken from the description. A prompt that says one of
	// these may mean this repository, or may just be English, so a term is
	// worth less than an identifier. See match().
	Terms []string `json:"terms"`
}

// maxDescription keeps one entry to about three lines of prompt.
const maxDescription = 240

// genericTerms are words that would match most prompts, so a repository named
// after one earns nothing by matching. This list is about English and software
// in general. It says nothing about any particular repository.
var genericTerms = map[string]bool{
	"api": true, "app": true, "apps": true, "code": true, "common": true,
	"config": true, "core": true, "data": true, "demo": true, "dev": true,
	"docs": true, "example": true, "examples": true, "helper": true,
	"helpers": true, "lib": true, "libs": true, "main": true, "misc": true,
	"new": true, "old": true, "project": true, "repo": true, "sample": true,
	"scripts": true, "server": true, "service": true, "shared": true,
	"simple": true, "site": true, "test": true, "tests": true, "tool": true,
	"tools": true, "util": true, "utils": true, "web": true, "www": true,
}

var (
	splitName    = regexp.MustCompile(`[-_.]+`)
	badgeLine    = regexp.MustCompile(`^\s*[!\[]`)
	htmlComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	inlineLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	inlineMarkup = regexp.MustCompile("[`*_]+")
	spaces       = regexp.MustCompile(`\s+`)
)

// buildIndex turns raw repositories into index entries. A repository is
// dropped when it is archived, a fork, has nothing to say about itself, or
// has no phrase specific enough to match on. The counts travel back to the
// caller so a refresh can report what it left out.
type buildStats struct {
	Archived    int
	Forks       int
	NoText      int
	NoPhrase    int
	ReadmeUsed  int
	ReadmeCalls int
}

// describer supplies a README for a repository that has no description.
type describer func(fullName string) string

func buildIndex(raw []repo, readme describer, readmeBudget int) ([]Repo, buildStats) {
	var stats buildStats
	var out []Repo
	for _, r := range raw {
		if r.Archived {
			stats.Archived++
			continue
		}
		if r.Fork {
			stats.Forks++
			continue
		}

		description := strings.TrimSpace(r.Description)
		if description == "" && readme != nil && stats.ReadmeCalls < readmeBudget {
			stats.ReadmeCalls++
			if summary := summarize(readme(r.FullName)); summary != "" {
				description = summary
				stats.ReadmeUsed++
			}
		}
		if description == "" {
			stats.NoText++
			continue
		}

		phrases, parts := phrasesFor(r)
		if len(phrases) == 0 {
			stats.NoPhrase++
			continue
		}

		out = append(out, Repo{
			Name:        r.FullName,
			URL:         r.HTMLURL,
			Description: truncate(description, maxDescription),
			Match:       phrases,
			Terms:       parts,
		})
	}
	addDistinctiveTerms(out)
	return out, stats
}

// maxDerived caps how many words a description contributes, so one wordy
// README cannot crowd out every other repository.
const maxDerived = 8

// rarityCut is the share of the index a word may appear in and still count as
// distinctive. A word in one repository's description identifies it; a word in
// a tenth of them identifies nothing.
const rarityCut = 0.02

// addDistinctiveTerms lets a repository match on the words that only it uses.
// Rarity decides which those are, measured across this index, so no list of
// interesting words has to be written or maintained. Without this a repository
// matches its own name and nothing else, and a prompt says "xsd" where the
// repository is called xml-validator.
func addDistinctiveTerms(repos []Repo) {
	docFreq := map[string]int{}
	words := make([][]string, len(repos))
	for i, r := range repos {
		words[i] = tokens(r.Description)
		for _, w := range unique(words[i]) {
			docFreq[w]++
		}
	}

	limit := int(float64(len(repos)) * rarityCut)
	if limit < 1 {
		limit = 1
	}
	for i := range repos {
		have := set.New[string]()
		for _, phrase := range append(repos[i].Match, repos[i].Terms...) {
			have.Add(phrase)
		}
		derived := 0
		for _, w := range unique(words[i]) {
			if derived == maxDerived {
				break
			}
			if docFreq[w] <= limit && !have.Contains(w) {
				repos[i].Terms = append(repos[i].Terms, w)
				have.Add(w)
				derived++
			}
		}
	}
}

var word = regexp.MustCompile(`[a-z][a-z0-9]{2,}`)

// tokens lowercases the text and keeps every word of three characters or
// more. There is no length rule beyond that on purpose: rarity already
// removes the common words, and a short word can be the whole point -- "xsd"
// is three characters and names exactly one repository.
func tokens(text string) []string {
	var out []string
	for _, w := range word.FindAllString(strings.ToLower(text), -1) {
		if !genericTerms[w] {
			out = append(out, w)
		}
	}
	return out
}

func unique(in []string) []string {
	seen := set.New[string]()
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen.Contains(s) {
			seen.Add(s)
			out = append(out, s)
		}
	}
	return out
}

// phrasesFor derives what a prompt must say for this repository to be
// relevant. It returns two tiers.
//
// The identifiers are the whole name, the same name spaced, and the topics the
// owner set. Each of those names this repository and nothing else.
//
// The parts are the single words inside the name, and they are weak on
// purpose: half of them are ordinary English. A prompt about how to "write a
// haiku" must not pull in a repository called quick-write-this-code.
func phrasesFor(r repo) (identifiers, parts []string) {
	seen := set.New[string]()
	keep := func(phrase string, into *[]string) {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if phrase == "" || seen.Contains(phrase) || genericTerms[phrase] || len(phrase) < 3 {
			return
		}
		seen.Add(phrase)
		*into = append(*into, phrase)
	}

	keep(r.Name, &identifiers)
	words := splitName.Split(r.Name, -1)
	if len(words) > 1 {
		keep(strings.Join(words, " "), &identifiers)
	}
	// Any run of two adjacent words in the name is still the name, and people
	// shorten names: someone asking about "pr preview" means pr-preview-action.
	for i := 0; i+1 < len(words); i++ {
		keep(words[i]+" "+words[i+1], &identifiers)
	}
	for _, topic := range r.Topics {
		keep(topic, &identifiers)
		keep(strings.ReplaceAll(topic, "-", " "), &identifiers)
	}
	if len(words) > 1 {
		for _, w := range words {
			if len(w) >= 4 {
				keep(w, &parts)
			}
		}
	}
	return identifiers, parts
}

// summarize takes the first real sentence of a README: no heading, no badge,
// no HTML comment, and no markdown link syntax.
func summarize(readme string) string {
	if readme == "" {
		return ""
	}
	var para []string
	var inFence bool
	for _, line := range strings.Split(htmlComment.ReplaceAllString(readme, ""), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			// A fence holds code, never prose. Skip to the far side of it.
			inFence = !inFence
			if !inFence && len(para) > 0 {
				break
			}
			continue
		}
		if inFence {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") ||
			badgeLine.MatchString(trimmed) {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, trimmed)
	}
	text := inlineLink.ReplaceAllString(strings.Join(para, " "), "$1")
	text = inlineMarkup.ReplaceAllString(text, "")
	return strings.TrimSpace(spaces.ReplaceAllString(text, " "))
}

// truncate cuts at a word boundary so an entry never ends mid-word.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := text[:limit]
	if i := strings.LastIndex(cut, " "); i > limit/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-") + "..."
}
