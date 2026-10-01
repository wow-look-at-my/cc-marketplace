package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repoFixture(name, description string, topics ...string) repo {
	return repo{
		Name:        name,
		FullName:    "owner/" + name,
		HTMLURL:     "https://github.com/owner/" + name,
		Description: description,
		Topics:      topics,
	}
}

func TestEntryComesFromTheRepositoryItself(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("go-toolchain", "Builds Go projects.", "golang", "ci")}, nil, 0)
	require.Len(t, got, 1)

	assert.Equal(t, "owner/go-toolchain", got[0].Name)
	assert.Equal(t, "https://github.com/owner/go-toolchain", got[0].URL)
	assert.Equal(t, "Builds Go projects.", got[0].Description)
	assert.Equal(t, []string{"go-toolchain", "go toolchain", "golang"}, got[0].Match,
		"a two-letter topic is never specific enough to spend an injection on")
	assert.Equal(t, "toolchain", got[0].Terms[0], "a name part is weak evidence, not an identifier")
}

func TestArchivedAndForkedRepositoriesAreDropped(t *testing.T) {
	archived := repoFixture("one", "A thing.", "widget")
	archived.Archived = true
	forked := repoFixture("two", "Another thing.", "widget")
	forked.Fork = true

	got, stats := buildIndex([]repo{archived, forked}, nil, 0)
	assert.Empty(t, got)
	assert.Equal(t, 1, stats.Archived)
	assert.Equal(t, 1, stats.Forks)
}

func TestAReadmeDescribesARepositoryGitHubDoesNot(t *testing.T) {
	readme := func(string) string {
		return "# widget-press\n\n[![build](img)](url)\n\nStamps widgets into shape.\n\nMore detail here."
	}
	got, stats := buildIndex([]repo{repoFixture("widget-press", "")}, readme, 5)

	require.Len(t, got, 1)
	assert.Equal(t, "Stamps widgets into shape.", got[0].Description)
	assert.Equal(t, 1, stats.ReadmeUsed)
}

func TestARepositoryWithNothingToSayIsDropped(t *testing.T) {
	got, stats := buildIndex([]repo{repoFixture("quiet", "")}, func(string) string { return "" }, 5)
	assert.Empty(t, got)
	assert.Equal(t, 1, stats.NoText)
}

func TestTheReadmeBudgetIsHonoured(t *testing.T) {
	var calls int
	readme := func(string) string {
		calls++
		return "Some prose."
	}
	got, stats := buildIndex([]repo{repoFixture("alpha", ""), repoFixture("bravo", ""), repoFixture("charlie", "")}, readme, 2)

	assert.Equal(t, 2, calls)
	assert.Equal(t, 2, stats.ReadmeCalls)
	assert.Len(t, got, 2, "only the repos described within budget survive")
	assert.Equal(t, 1, stats.NoText)
}

func TestAGenericNameEarnsNoPhraseAndIsDropped(t *testing.T) {
	got, stats := buildIndex([]repo{repoFixture("tools", "Assorted tools.")}, nil, 0)
	assert.Empty(t, got)
	assert.Equal(t, 1, stats.NoPhrase)
}

func TestGenericNamePartsAreDroppedButTheWholeNameSurvives(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("widget-tools", "Widget tools.")}, nil, 0)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Match, "widget-tools")
	assert.Contains(t, got[0].Terms, "widget")
	assert.NotContains(t, got[0].Terms, "tools")
}

func TestATwoWordRunOfTheNameIsAnIdentifier(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("pr-preview-action", "Deploys a preview.")}, nil, 0)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"pr-preview-action", "pr preview action", "pr preview", "preview action"}, got[0].Match)

	hits := match("set up a pr preview on github pages", got)
	require.Len(t, hits, 1, "people shorten a repository's name and still mean it")
}

func TestShortNamePartsAreDropped(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("go-pressure", "Measures pressure.")}, nil, 0)
	require.Len(t, got, 1)
	assert.NotContains(t, got[0].Terms, "go")
	assert.Contains(t, got[0].Terms, "pressure")
}

func TestPhrasesAreDeduplicated(t *testing.T) {
	got, _ := buildIndex([]repo{repoFixture("widget", "A widget.", "widget", "Widget")}, nil, 0)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"widget"}, got[0].Match)
}

func TestALongDescriptionIsCutAtAWordBoundary(t *testing.T) {
	long := strings.Repeat("alpha beta ", 40)
	got, _ := buildIndex([]repo{repoFixture("widget", long)}, nil, 0)

	require.Len(t, got, 1)
	assert.LessOrEqual(t, len(got[0].Description), maxDescription+3)
	assert.True(t, strings.HasSuffix(got[0].Description, "..."))
	assert.NotContains(t, strings.TrimSuffix(got[0].Description, "..."), "alph...")
}

func TestSummarizeSkipsHeadingsBadgesAndComments(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"heading":  {"# Title\n\nThe prose.", "The prose."},
		"badge":    {"![b](u)\n\nThe prose.", "The prose."},
		"comment":  {"<!-- hidden -->\nThe prose.", "The prose."},
		"quote":    {"> a note\n\nThe prose.", "The prose."},
		"fence":    {"```sh\nrun\n```\n\nThe prose.", "The prose."},
		"link":     {"A [linked](http://x) word.", "A linked word."},
		"markup":   {"Some `code` and *stress*.", "Some code and stress."},
		"wrapped":  {"One line\nand its rest.\n\nLater.", "One line and its rest."},
		"empty":    {"", ""},
		"headings": {"# One\n## Two\n", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, summarize(tc.in))
		})
	}
}
