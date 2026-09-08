package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver answers without a checkout, so the suite never shells out to git
// and every case states the repository state it is written against.
type fakeResolver struct {
	repo     Repo
	found    bool
	base     string
	branches []string
	commits  []string
	// settled and open are the references whose state is known. Anything named
	// in neither is a lookup that could not answer, which is the case that must
	// leave the text exactly as it was written.
	settled []string
	open    []string
}

func (f fakeResolver) Repo() (Repo, bool)         { return f.repo, f.found }
func (f fakeResolver) DefaultBranch() string      { return f.base }
func (f fakeResolver) BranchExists(b string) bool { return slices.Contains(f.branches, b) }
func (f fakeResolver) CommitExists(s string) bool { return slices.Contains(f.commits, s) }

func (f fakeResolver) Settled(repo Repo, number string) (bool, bool) {
	key := repo.Owner + "/" + repo.Name + "#" + number
	switch {
	case slices.Contains(f.settled, key):
		return true, true
	case slices.Contains(f.open, key):
		return false, true
	}
	return false, false
}

// live is a checkout of o/r whose master exists, with one pushed branch and one
// known commit.
func live() fakeResolver {
	return fakeResolver{
		repo:     Repo{Owner: "o", Name: "r"},
		found:    true,
		base:     "master",
		branches: []string{"claude/pushed"},
		commits:  []string{"6884dd2"},
	}
}

// bare is a directory that is not a checkout at all.
func bare() fakeResolver { return fakeResolver{} }

func rewrite(t *testing.T, text string, res Resolver) string {
	t.Helper()
	out, changed := RewriteDelta(text, false, res)
	if !changed {
		return text
	}
	return out
}

func TestAReferenceIsRenderedAsALink(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			"owner repo number needs no checkout",
			"wow-look-at-my/go-toolchain#376 landed.",
			"[wow-look-at-my/go-toolchain#376](https://github.com/wow-look-at-my/go-toolchain/issues/376) landed.",
		},
		{
			"a commit in this repository",
			"re-pushed as 6884dd2.",
			"re-pushed as [6884dd2](https://github.com/o/r/commit/6884dd2).",
		},
		{
			"a branch that is on the remote",
			"pushed claude/pushed to origin.",
			"pushed [claude/pushed](https://github.com/o/r/compare/master...claude/pushed?expand=1) to origin.",
		},
		{
			"a bare URL becomes clickable in place",
			"here: https://github.com/o/r/pull/30",
			"here: [https://github.com/o/r/pull/30](https://github.com/o/r/pull/30)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rewrite(t, tc.text, live()))
		})
	}
}

// The rule this plugin serves bans a dead link outright: a link is a demand on
// the reader's attention, paid before they know whether it was worth paying. So
// a reference whose target cannot be shown to exist stays plain text.
func TestAReferenceWithNoPageIsLeftAlone(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"a branch that was never pushed", "working on claude/never-pushed now."},
		{"a commit this repository does not have", "master is at e3665a4689bb now."},
		{"a branch with no repository to resolve against", "pushed claude/pushed to origin."},
	}
	res := []Resolver{live(), live(), bare()}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, changed := RewriteDelta(tc.text, false, res[i])
			assert.False(t, changed, "expected no rewrite, got %q", out)
		})
	}
}

// A merged or closed pull request has nothing left to do, so a link to one
// spends the reader's attention on a page they closed hours ago. It stays plain
// text -- the words the model wrote, with no link put on them.
func TestASettledPullRequestIsNotLinked(t *testing.T) {
	res := live()
	res.settled = []string{"o/r#376"}

	cases := []string{
		"o/r#376 is merged.",
		"here: https://github.com/o/r/pull/376",
		"see https://github.com/o/r/issues/376 for the detail",
		"the diff: https://github.com/o/r/pull/376/files",
	}
	for _, text := range cases {
		out, changed := RewriteDelta(text, false, res)
		assert.False(t, changed, "expected no link on %q, got %q", text, out)
	}
}

// The negative control: an OPEN pull request is still something to click, and
// still gets linked. Without this the case above passes by linking nothing.
func TestAnOpenPullRequestIsStillLinked(t *testing.T) {
	res := live()
	res.open = []string{"o/r#376"}

	got := rewrite(t, "o/r#376 is green.", res)
	assert.Equal(t, "[o/r#376](https://github.com/o/r/issues/376) is green.", got)

	got = rewrite(t, "here: https://github.com/o/r/pull/376", res)
	assert.Contains(t, got, "](https://github.com/o/r/pull/376)")
}

// A lookup that cannot answer leaves the reference exactly as it was written.
// Stripping on a failed call turns a network blip into lost information, so an
// unknown state must behave like an open one.
func TestAnUnknownStateLinksAsBefore(t *testing.T) {
	// live() names no state at all, so every lookup here reports unknown.
	got := rewrite(t, "o/r#376 is up.", live())
	assert.Equal(t, "[o/r#376](https://github.com/o/r/issues/376) is up.", got)
}

// A settled answer for one pull request says nothing about another.
func TestOnlyTheSettledReferenceLosesItsLink(t *testing.T) {
	res := live()
	res.settled = []string{"o/r#1"}
	res.open = []string{"o/r#2"}

	got := rewrite(t, "o/r#1 merged, o/r#2 is next.", res)
	assert.Equal(t, "o/r#1 merged, [o/r#2](https://github.com/o/r/issues/2) is next.", got)
}

// A URL that is not a pull request or an issue is never asked about, and is
// linked the way it always was.
func TestANonIssueURLIsUnaffected(t *testing.T) {
	res := live()
	res.settled = []string{"o/r#376"}
	got := rewrite(t, "tree: https://github.com/o/r/tree/claude/pushed", res)
	assert.Contains(t, got, "](https://github.com/o/r/tree/claude/pushed)")
}

// An owner/repo#N slug carries its own repository, so it resolves even when the
// hook is not standing in a checkout at all.
func TestASlugResolvesWithoutACheckout(t *testing.T) {
	got := rewrite(t, "see wow-look-at-my/dats#12 for the suite.", bare())
	assert.Equal(t, "see [wow-look-at-my/dats#12](https://github.com/wow-look-at-my/dats/issues/12) for the suite.", got)
}

// /issues/N and never /pull/N: GitHub redirects an issue number to the pull
// request when it is one, and /pull/N on a plain issue is a 404. Nothing here
// knows which it is, so it must use the spelling that is right for both.
func TestNumbersUseTheSpellingThatWorksForBoth(t *testing.T) {
	got := rewrite(t, "o/r#42", live())
	assert.Contains(t, got, "/issues/42")
	assert.NotContains(t, got, "/pull/42")
}

// A bare #N is never linked. It cannot be checked before rendering, the
// repository it would resolve against is a guess in a multi-checkout session,
// and it is the shape an ordinary numbered list uses. Guessing produces a link
// to a real but unrelated issue, which the reader cannot tell is wrong.
func TestABareNumberIsNeverLinked(t *testing.T) {
	cases := []string{
		"PR #376 is green.",
		"- **#7** the sweep is still open",
		"#1 blocked, then #2 landed.",
	}
	for _, text := range cases {
		out, changed := RewriteDelta(text, false, live())
		assert.False(t, changed, "expected no rewrite of %q, got %q", text, out)
	}
}

func TestTextThatIsAlreadyLinkedIsNotRewrittenAgain(t *testing.T) {
	cases := []string{
		"[claude/pushed](https://github.com/o/r/compare/master...claude/pushed?expand=1) is up.",
		"[6884dd2](https://github.com/o/r/commit/6884dd2) fixes it.",
		"[o/r#376](https://github.com/o/r/pull/376) is merged.",
		"see <https://github.com/o/r/pull/1>",
	}
	for _, text := range cases {
		out, changed := RewriteDelta(text, false, live())
		assert.False(t, changed, "expected no rewrite of %q, got %q", text, out)
	}
}

// A URL contains a slug that the branch matcher also matches. Rewriting both
// would nest one link inside another.
func TestAURLIsRewrittenOnceNotTwice(t *testing.T) {
	got := rewrite(t, "https://github.com/o/r/tree/claude/pushed", live())
	assert.Equal(t, 1, strings.Count(got, "]("), "expected exactly one link in %q", got)
}

func TestQuotedAndFencedLinesAreLeftAlone(t *testing.T) {
	text := "```\nPR #376 on claude/pushed\n```\n> quoted: PR #376\n    indented #376"
	out, changed := RewriteDelta(text, false, live())
	assert.False(t, changed, "expected no rewrite, got %q", out)
}

// One flush cannot see the ``` that opened in an earlier one, so the state has
// to be carried in.
func TestFenceStateCarriesAcrossFlushes(t *testing.T) {
	out, changed := RewriteDelta("PR #376 inside the fence\n", true, live())
	assert.False(t, changed, "expected no rewrite inside a carried fence, got %q", out)

	assert.True(t, EndsInsideFence("intro\n```\ncode"))
	assert.False(t, EndsInsideFence("intro\n```\ncode\n```\nafter"))
}

// A reference wrapped in inline backticks must render with the backticks
// INSIDE the link text. Splicing the link over just the bare token instead
// leaves the backticks straddling it (`` `[x](url)` ``), which markdown does
// not render as a link at all -- the reader sees literal brackets.
func TestABacktickWrappedReferenceKeepsTheBackticksInsideTheLink(t *testing.T) {
	res := fakeResolver{repo: Repo{Owner: "wow-look-at-my", Name: "slopfmt"}, found: true, commits: []string{"c4f997e"}}
	got := rewrite(t, "Resolved and pushed `c4f997e`.", res)
	assert.Equal(t, "Resolved and pushed [`c4f997e`](https://github.com/wow-look-at-my/slopfmt/commit/c4f997e).", got)
}

func TestEveryOccurrenceIsRewritten(t *testing.T) {
	got := rewrite(t, "o/r#1 blocked o/r#1 then o/r#2 landed.", live())
	assert.Equal(t, 3, strings.Count(got, "]("), "expected three links in %q", got)
}

func TestRunEmitsTheDisplayContentEnvelope(t *testing.T) {
	in := `{"hook_event_name":"MessageDisplay","message_id":"m1","index":0,"final":true,"delta":"PR o/r#376 is green."}`
	out := run(strings.NewReader(in), live())
	require.NotEmpty(t, out)

	var got Output
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "MessageDisplay", got.HookSpecificOutput.HookEventName)
	assert.Equal(t, "PR [o/r#376](https://github.com/o/r/issues/376) is green.", got.HookSpecificOutput.DisplayContent)
}

// Printing nothing leaves the CLI showing the original delta, which is the only
// acceptable failure for a hook in the render path.
func TestEverySurpriseRendersTheOriginal(t *testing.T) {
	cases := map[string]string{
		"not json":        "not json",
		"empty":           "",
		"another event":   `{"hook_event_name":"Stop","delta":"PR o/r#376"}`,
		"no event name":   `{"delta":"PR o/r#376"}`,
		"nothing to link": `{"hook_event_name":"MessageDisplay","delta":"the suite is green."}`,
		"empty delta":     `{"hook_event_name":"MessageDisplay","delta":""}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, run(strings.NewReader(in), live()))
		})
	}
}

func TestTheHookCanBeTurnedOff(t *testing.T) {
	t.Setenv("CC_LINK_ALL_REFS", "0")
	in := `{"hook_event_name":"MessageDisplay","delta":"PR o/r#376 is green."}`
	assert.Empty(t, run(strings.NewReader(in), live()))
}
