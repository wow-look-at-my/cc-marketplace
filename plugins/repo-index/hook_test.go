package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEnv(t *testing.T) (env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return env{
		home:    t.TempDir(),
		stateIn: t.TempDir(),
		stdout:  &out,
		stderr:  &errOut,
		now:     epoch,
	}, &out, &errOut
}

// seed writes an index the hook can serve, dated now so nothing refreshes.
func seed(t *testing.T, e env, repos ...Repo) {
	t.Helper()
	require.NoError(t, writeCache(e.home, cache{FetchedAt: e.now, Owners: []string{"acme"}, Repos: repos}))
}

var pressRepo = Repo{
	Name:        "acme/widget-press",
	URL:         "https://github.com/acme/widget-press",
	Description: "Stamps widgets into shape.",
	Match:       []string{"widget-press", "widget", "press"},
}

var boltRepo = Repo{
	Name:        "acme/bolt-cutter",
	URL:         "https://github.com/acme/bolt-cutter",
	Description: "Cuts bolts.",
	Match:       []string{"bolt-cutter", "bolt", "cutter"},
}

func submit(prompt, session string) string {
	in := hookInput{HookEventName: "UserPromptSubmit", SessionID: session, Prompt: prompt}
	data, _ := json.Marshal(in)
	return string(data)
}

func contextOf(t *testing.T, stdout string) string {
	t.Helper()
	var out hookOutput
	require.NoError(t, json.Unmarshal([]byte(stdout), &out))
	if out.HookSpecificOutput == nil {
		return ""
	}
	return out.HookSpecificOutput.AdditionalContext
}

func TestSuggestsMatchingRepo(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)
	seed(t, e, pressRepo)

	require.Equal(t, 0, run(strings.NewReader(submit("the widget press is jammed", "s1")), e))

	ctx := contextOf(t, out.String())
	assert.Contains(t, ctx, "acme/widget-press")
	assert.Contains(t, ctx, "https://github.com/acme/widget-press")
	assert.Contains(t, ctx, "Stamps widgets into shape.")
	assert.Empty(t, errOut.String())
}

func TestSuggestsEachRepoOncePerSession(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, _ := testEnv(t)
	seed(t, e, pressRepo)

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s1")), e))
	assert.Contains(t, contextOf(t, out.String()), "widget-press")

	out.Reset()
	require.Equal(t, 0, run(strings.NewReader(submit("widget-press again", "s1")), e))
	assert.Equal(t, "{}\n", out.String())
}

func TestADifferentSessionSeesTheSuggestion(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, _ := testEnv(t)
	seed(t, e, pressRepo)

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s1")), e))
	out.Reset()
	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s2")), e))
	assert.Contains(t, contextOf(t, out.String()), "widget-press")
}

func TestASecondRepoIsStillSuggestedAfterTheFirst(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, _ := testEnv(t)
	seed(t, e, pressRepo, boltRepo)

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s1")), e))
	out.Reset()
	require.Equal(t, 0, run(strings.NewReader(submit("about the bolt cutter", "s1")), e))

	ctx := contextOf(t, out.String())
	assert.Contains(t, ctx, "bolt-cutter")
	assert.NotContains(t, ctx, "widget-press")
}

func TestNoMatchProducesNoContext(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)
	seed(t, e, pressRepo)

	require.Equal(t, 0, run(strings.NewReader(submit("write me a haiku about rain", "s1")), e))
	assert.Equal(t, "{}\n", out.String())
	assert.Empty(t, errOut.String())
}

func TestOtherEventsAreIgnored(t *testing.T) {
	e, out, _ := testEnv(t)
	in := `{"hook_event_name":"PreToolUse","prompt":"widget-press","session_id":"s1"}`
	require.Equal(t, 0, run(strings.NewReader(in), e))
	assert.Equal(t, "{}\n", out.String())
}

func TestEmptyPromptIsIgnored(t *testing.T) {
	e, out, _ := testEnv(t)
	require.Equal(t, 0, run(strings.NewReader(submit("   ", "s1")), e))
	assert.Equal(t, "{}\n", out.String())
}

func TestMalformedInputFailsLoud(t *testing.T) {
	e, out, errOut := testEnv(t)
	assert.Equal(t, 1, run(strings.NewReader("not json"), e))
	assert.Empty(t, out.String())
	assert.Contains(t, errOut.String(), "not valid JSON")
}

func TestUnreadableInputFailsLoud(t *testing.T) {
	e, _, errOut := testEnv(t)
	assert.Equal(t, 1, run(errReader{}, e))
	assert.Contains(t, errOut.String(), "cannot read hook input")
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, assert.AnError }

func TestMissingSessionIDStillSuggestsAndSaysSo(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)
	seed(t, e, pressRepo)

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "")), e))
	assert.Contains(t, contextOf(t, out.String()), "widget-press")
	assert.Contains(t, errOut.String(), "no session_id")
}

func TestCapReportsWhatItDropped(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)
	seed(t, e,
		pressRepo, boltRepo,
		Repo{Name: "acme/pipe-bender", URL: "u", Description: "Bends pipes.", Match: []string{"pipe"}},
		Repo{Name: "acme/nail-gun", URL: "u", Description: "Drives nails.", Match: []string{"nail"}},
	)

	require.Equal(t, 0, run(strings.NewReader(submit("widget bolt pipe nail work", "s1")), e))

	ctx := contextOf(t, out.String())
	assert.Equal(t, maxSuggestions, strings.Count(ctx, "- **"))
	assert.Contains(t, errOut.String(), "beyond the cap")
}

func TestUnwritableStateWarnsAndStillSuggests(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)
	seed(t, e, pressRepo)
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))
	e.stateIn = blocked

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s1")), e))
	assert.Contains(t, contextOf(t, out.String()), "widget-press")
	assert.Contains(t, errOut.String(), "can repeat later in the session")
}

func TestAColdIndexSuggestsNothingAndSaysWhere(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s1")), e))
	assert.Equal(t, "{}\n", out.String())
	assert.Contains(t, errOut.String(), "no index yet")
	assert.Contains(t, errOut.String(), refreshLog(e.home))
}

func TestAStaleIndexStillSuggests(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, _ := testEnv(t)
	require.NoError(t, writeCache(e.home, cache{
		FetchedAt: e.now.Add(-2 * ttl), Owners: []string{"acme"}, Repos: []Repo{pressRepo},
	}))

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s1")), e))
	assert.Contains(t, contextOf(t, out.String()), "widget-press")
}

func TestAnIndexForOtherOwnersSaysSoAndStillSuggests(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)
	seed(t, e, pressRepo)
	cwd := gitRepo(t, "https://github.com/beta/thing.git")

	in := hookInput{HookEventName: "UserPromptSubmit", SessionID: "s1", Prompt: "widget-press", Cwd: cwd}
	data, _ := json.Marshal(in)
	require.Equal(t, 0, run(bytes.NewReader(data), e))

	assert.Contains(t, contextOf(t, out.String()), "widget-press")
	assert.Contains(t, errOut.String(), "being rebuilt")
}

func TestAnUnusableConfigStopsTheHookLoudly(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	e, out, errOut := testEnv(t)
	seed(t, e, pressRepo)
	writeConfig(t, e.home, `{"owners":[`)

	require.Equal(t, 0, run(strings.NewReader(submit("widget-press", "s1")), e))
	assert.Equal(t, "{}\n", out.String())
	assert.Contains(t, errOut.String(), "not valid JSON")
}
