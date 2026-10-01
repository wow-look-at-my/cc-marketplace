package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/go-containers/set"
)

// filler pads an index so a word's document frequency means something.
func filler(n int, text string) []repo {
	var out []repo
	for i := 0; i < n; i++ {
		out = append(out, repoFixture(fmt.Sprintf("filler%d", i), text))
	}
	return out
}

func TestAWordOnlyOneRepositoryUsesBecomesAPhrase(t *testing.T) {
	raw := append([]repo{repoFixture("xml-validator", "Strict XML 1.1 validator with optional XSD schema validation.")},
		filler(60, "Builds things and runs them.")...)

	got, _ := buildIndex(raw, nil, 0)
	require.NotEmpty(t, got)
	assert.Contains(t, got[0].Terms, "xsd",
		"a prompt says xsd where the repository is called xml-validator")
	assert.Contains(t, got[0].Terms, "strict")
}

func TestAWordMostRepositoriesUseIsNotAPhrase(t *testing.T) {
	raw := append([]repo{repoFixture("widget-press", "Stamps widgets with kubernetes.")},
		filler(60, "Runs on kubernetes.")...)

	got, _ := buildIndex(raw, nil, 0)
	require.NotEmpty(t, got)
	assert.NotContains(t, got[0].Terms, "kubernetes",
		"a word the whole index uses identifies nothing")
}

func TestCommonEnglishNeverSurvivesRarity(t *testing.T) {
	raw := append([]repo{repoFixture("widget-press", "This one stamps the widgets that are not bolts.")},
		filler(60, "This one runs the things that are not broken.")...)

	got, _ := buildIndex(raw, nil, 0)
	require.NotEmpty(t, got)
	for _, common := range []string{"this", "one", "the", "that", "are", "not"} {
		assert.NotContains(t, got[0].Terms, common)
	}
}

func TestDerivedPhrasesAreCapped(t *testing.T) {
	raw := []repo{repoFixture("widget-press", strings.Repeat("alpha bravo charlie delta echo foxtrot golf hotel india juliet ", 2))}
	got, _ := buildIndex(raw, nil, 0)

	require.Len(t, got, 1)
	ids, parts := phrasesFor(raw[0])
	assert.Len(t, got[0].Match, len(ids))
	assert.Len(t, got[0].Terms, len(parts)+maxDerived)
}

func TestADerivedPhraseIsNeverADuplicate(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("widget-press", "The widget press presses widgets.")}, nil, 0)
	require.Len(t, got, 1)

	seen := set.New[string]()
	for _, phrase := range append(got[0].Match, got[0].Terms...) {
		assert.False(t, seen.Contains(phrase), "%q appears twice", phrase)
		seen.Add(phrase)
	}
}

func TestOneDescriptionWordIsNotEnoughToSuggestARepository(t *testing.T) {
	raw := append([]repo{repoFixture("quick-write-this-code", "Write code quickly.")},
		filler(60, "Runs things.")...)
	got, _ := buildIndex(raw, nil, 0)

	assert.Empty(t, match("write a haiku about rain", got),
		"sharing one English word with a README is not a reason to suggest a repository")
	assert.NotEmpty(t, match("quick-write-this-code", got),
		"but naming the repository is")
}

func TestASmallIndexStillDerivesPhrases(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("widget-press", "Stamps widgets.")}, nil, 0)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Terms, "stamps", "with one repo, every word it uses is unique to it")
}

func TestGenericTermsNeverBecomePhrases(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("widget-press", "A simple tool for the web api.")}, nil, 0)
	require.Len(t, got, 1)
	for _, generic := range []string{"simple", "tool", "web", "api"} {
		assert.NotContains(t, got[0].Terms, generic)
	}
}

func TestTokensKeepsShortWordsAndDropsPunctuation(t *testing.T) {
	assert.Equal(t, []string{"xsd", "schema", "for", "xml"}, tokens("XSD schema, for XML 1.1!"),
		"tokens keeps common words on purpose: rarity is what removes them")
}

func TestUniquePreservesOrder(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, unique([]string{"a", "b", "a", "c", "b"}))
}
