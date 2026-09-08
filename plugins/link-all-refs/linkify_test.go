package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A remote URL can carry credentials. Only the two path segments after the host
// are taken, so a token in the userinfo cannot reach the user's screen.
func TestParseRemoteReadsEverySpellingAndDropsCredentials(t *testing.T) {
	cases := map[string]Repo{
		"https://github.com/o/r.git":                         {Owner: "o", Name: "r"},
		"https://github.com/o/r":                             {Owner: "o", Name: "r"},
		"https://github.com/o/r/":                            {Owner: "o", Name: "r"},
		"git@github.com:o/r.git":                             {Owner: "o", Name: "r"},
		"ssh://git@github.com/o/r.git":                       {Owner: "o", Name: "r"},
		"https://x-access-token:ghs_secret@github.com/o/r":   {Owner: "o", Name: "r"},
		"https://github.com/wow-look-at-my/go-toolchain.git": {Owner: "wow-look-at-my", Name: "go-toolchain"},
	}
	for url, want := range cases {
		got, ok := parseRemote(url)
		require.True(t, ok, "expected %q to parse", url)
		assert.Equal(t, want, got, "for %q", url)
	}
}

func TestParseRemoteRefusesWhatIsNotGitHub(t *testing.T) {
	for _, url := range []string{"https://gitlab.com/o/r.git", "", "not a url", "https://github.com/o"} {
		_, ok := parseRemote(url)
		assert.False(t, ok, "expected %q not to parse", url)
	}
}

// A rendered link must never carry a credential, whatever the remote holds.
func TestARenderedLinkNeverCarriesACredential(t *testing.T) {
	repo, ok := parseRemote("https://x-access-token:ghs_secret@github.com/o/r.git")
	require.True(t, ok)
	assert.NotContains(t, repo.url(), "ghs_secret")
	assert.Equal(t, "https://github.com/o/r", repo.url())
}

func TestSplitNumber(t *testing.T) {
	owner, name, number := splitNumber("wow-look-at-my/dats#12")
	assert.Equal(t, "wow-look-at-my", owner)
	assert.Equal(t, "dats", name)
	assert.Equal(t, "12", number)

	owner, name, number = splitNumber("#42")
	assert.Empty(t, owner)
	assert.Empty(t, name)
	assert.Equal(t, "42", number)
}

// IssueRef decides which URLs are worth asking GitHub about. `pull` and
// `issues` are one case, because GitHub serves a pull request under both.
func TestIssueRefReadsAPullRequestOrIssueURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/r/pull/376":                 "376",
		"https://github.com/o/r/issues/376":               "376",
		"https://www.github.com/o/r/pull/376":             "376",
		"http://github.com/o/r/pull/376":                  "376",
		"https://github.com/o/r/pull/376/files":           "376",
		"https://github.com/o/r/issues/12#issuecomment-1": "12",
	}
	for url, want := range cases {
		repo, number, ok := IssueRef(url)
		require.True(t, ok, "expected %q to parse", url)
		assert.Equal(t, Repo{Owner: "o", Name: "r"}, repo, "for %q", url)
		assert.Equal(t, want, number, "for %q", url)
	}
}

func TestIssueRefRefusesEveryOtherURL(t *testing.T) {
	for _, url := range []string{
		"https://github.com/o/r",
		"https://github.com/o/r/commit/6884dd2",
		"https://github.com/o/r/tree/claude/pushed",
		"https://github.com/o/r/compare/master...claude/x?expand=1",
		"https://gitlab.com/o/r/pull/1",
		"",
	} {
		_, _, ok := IssueRef(url)
		assert.False(t, ok, "expected %q not to parse", url)
	}
}

func TestLinkifyRefusesAnUnknownKind(t *testing.T) {
	_, ok := Linkify(Ref{Kind: "something else", Text: "x"}, live())
	assert.False(t, ok)
}

// A compare URL needs a base. Without one there is no page to open, so the
// reference stays plain rather than getting a guessed link.
func TestABranchWithNoDefaultBranchIsNotLinked(t *testing.T) {
	res := live()
	res.base = ""
	_, ok := Linkify(Ref{Kind: "a branch", Text: "claude/pushed"}, res)
	assert.False(t, ok)
}
