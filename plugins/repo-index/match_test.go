package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sample = []Repo{
	{Name: "a/one", URL: "https://example.com/one", Description: "One.", Match: []string{"buildhost", "pazer.build"}},
	{Name: "a/two", URL: "https://example.com/two", Description: "Two.", Match: []string{"go test"}},
}

func TestPhraseMatchesWholeWordsOnly(t *testing.T) {
	assert.Empty(t, match("go-buildhost is different", sample))
	assert.Empty(t, match("buildhosting", sample))
	assert.Len(t, match("use buildhost.", sample), 1)
	assert.Len(t, match("buildhost", sample), 1)
}

func TestMatchIsCaseInsensitive(t *testing.T) {
	assert.Len(t, match("BuildHost", sample), 1)
}

func TestMultiWordPhraseMatches(t *testing.T) {
	hits := match("please go test the package", sample)
	require.Len(t, hits, 1)
	assert.Equal(t, "a/two", hits[0].Repo.Name)
}

func TestScoreCountsDistinctPhrases(t *testing.T) {
	hits := match("buildhost lives at pazer.build", sample)
	require.Len(t, hits, 1)
	assert.Equal(t, 2, hits[0].Score)
	assert.Equal(t, []string{"buildhost", "pazer.build"}, hits[0].Phrases)
}

func TestHigherScoreRanksFirst(t *testing.T) {
	hits := match("go test against buildhost on pazer.build", sample)
	require.Len(t, hits, 2)
	assert.Equal(t, "a/one", hits[0].Repo.Name)
}

func TestEqualScoresRankByName(t *testing.T) {
	hits := match("buildhost and go test", sample)
	require.Len(t, hits, 2)
	assert.Equal(t, "a/one", hits[0].Repo.Name)
	assert.Equal(t, "a/two", hits[1].Repo.Name)
}

func TestRenderStatesNameLinkAndDescription(t *testing.T) {
	out := render(match("buildhost", sample))
	assert.Contains(t, out, "**a/one**")
	assert.Contains(t, out, "https://example.com/one")
	assert.Contains(t, out, "One.")
	assert.Contains(t, out, "once per session")
}

func TestBuiltInIndexIsUsable(t *testing.T) {
	repos, err := loadIndex("", "")
	require.NoError(t, err)
	require.NotEmpty(t, repos)

	seen := map[string]bool{}
	for i, r := range repos {
		require.NoError(t, validate(r, "repos.json", i))
		assert.False(t, seen[r.Name], "duplicate entry for %s", r.Name)
		seen[r.Name] = true
		assert.Regexp(t, `^https://`, r.URL)
	}
}

func TestEveryBuiltInPhraseFindsItsOwnRepo(t *testing.T) {
	repos, err := loadIndex("", "")
	require.NoError(t, err)
	for _, r := range repos {
		for _, phrase := range r.Match {
			hits := match(phrase, repos)
			require.NotEmpty(t, hits, "phrase %q matched nothing", phrase)
			var found bool
			for _, h := range hits {
				if h.Repo.Name == r.Name {
					found = true
				}
			}
			assert.True(t, found, "phrase %q did not match its own repo %s", phrase, r.Name)
		}
	}
}
