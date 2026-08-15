package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sample = []Repo{
	{
		Name: "a/one", URL: "https://example.com/one", Description: "One.",
		Match: []string{"widget", "widget press"}, Terms: []string{"stamps"},
	},
	{
		Name: "a/two", URL: "https://example.com/two", Description: "Two.",
		Match: []string{"bolt"}, Terms: []string{"cutting"},
	},
}

func TestPhraseMatchesWholeWordsOnly(t *testing.T) {
	assert.Empty(t, match("mega-widget is different", sample))
	assert.Empty(t, match("widgets", sample))
	assert.Len(t, match("use widget.", sample), 1)
	assert.Len(t, match("widget", sample), 1)
}

func TestMatchIsCaseInsensitive(t *testing.T) {
	assert.Len(t, match("Widget", sample), 1)
}

func TestNamingARepositoryIsEnoughOnItsOwn(t *testing.T) {
	hits := match("widget", sample)
	require.Len(t, hits, 1)
	assert.Equal(t, identifierWeight, hits[0].Score)
}

func TestOneDescriptionWordIsNotEnough(t *testing.T) {
	assert.Empty(t, match("the machine stamps things", sample))
}

func TestTwoDescriptionWordsAreEnough(t *testing.T) {
	repos := []Repo{{Name: "a/one", Match: []string{"one"}, Terms: []string{"stamps", "flattens"}}}
	hits := match("it stamps and flattens", repos)
	require.Len(t, hits, 1)
	assert.Equal(t, 2, hits[0].Score)
}

func TestMultiWordPhraseMatches(t *testing.T) {
	hits := match("the widget press jammed", sample)
	require.Len(t, hits, 1)
	assert.Equal(t, "a/one", hits[0].Repo.Name)
	assert.Equal(t, 2*identifierWeight, hits[0].Score)
}

func TestPhrasesRecordWhatMatched(t *testing.T) {
	hits := match("the widget press stamps", sample)
	require.Len(t, hits, 1)
	assert.Equal(t, []string{"widget", "widget press", "stamps"}, hits[0].Phrases)
}

func TestHigherScoreRanksFirst(t *testing.T) {
	hits := match("bolt cutting in the widget press", sample)
	require.Len(t, hits, 2)
	assert.Equal(t, "a/one", hits[0].Repo.Name)
}

func TestEqualScoresRankByName(t *testing.T) {
	repos := []Repo{
		{Name: "a/two", Match: []string{"bolt"}},
		{Name: "a/one", Match: []string{"widget"}},
	}
	hits := match("widget and bolt", repos)
	require.Len(t, hits, 2)
	assert.Equal(t, "a/one", hits[0].Repo.Name)
	assert.Equal(t, "a/two", hits[1].Repo.Name)
}

func TestRenderStatesNameLinkAndDescription(t *testing.T) {
	out := render(match("widget", sample))
	assert.Contains(t, out, "**a/one**")
	assert.Contains(t, out, "https://example.com/one")
	assert.Contains(t, out, "One.")
	assert.Contains(t, out, "once per session")
}
