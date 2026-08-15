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
	}, &out, &errOut
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

func writeOverlay(t *testing.T, root string, body string) {
	t.Helper()
	dir := filepath.Join(root, ".claude")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "repo-index.json"), []byte(body), 0o644))
}

func TestSuggestsMatchingRepo(t *testing.T) {
	e, out, errOut := testEnv(t)
	require.Equal(t, 0, run(strings.NewReader(submit("how do I publish a binary to buildhost?", "s1")), e))

	ctx := contextOf(t, out.String())
	assert.Contains(t, ctx, "wow-look-at-my/buildhost")
	assert.Contains(t, ctx, "https://github.com/wow-look-at-my/buildhost")
	assert.Contains(t, ctx, "Universal package registry")
	assert.Empty(t, errOut.String())
}

func TestSuggestsEachRepoOncePerSession(t *testing.T) {
	e, out, _ := testEnv(t)
	require.Equal(t, 0, run(strings.NewReader(submit("upload to buildhost", "s1")), e))
	assert.Contains(t, contextOf(t, out.String()), "buildhost")

	out.Reset()
	require.Equal(t, 0, run(strings.NewReader(submit("buildhost again please", "s1")), e))
	assert.Equal(t, "{}\n", out.String())
}

func TestADifferentSessionSeesTheSuggestion(t *testing.T) {
	e, out, _ := testEnv(t)
	require.Equal(t, 0, run(strings.NewReader(submit("upload to buildhost", "s1")), e))
	out.Reset()
	require.Equal(t, 0, run(strings.NewReader(submit("upload to buildhost", "s2")), e))
	assert.Contains(t, contextOf(t, out.String()), "buildhost")
}

func TestSecondRepoStillSuggestedAfterTheFirst(t *testing.T) {
	e, out, _ := testEnv(t)
	require.Equal(t, 0, run(strings.NewReader(submit("upload to buildhost", "s1")), e))
	out.Reset()
	require.Equal(t, 0, run(strings.NewReader(submit("run go test on this", "s1")), e))

	ctx := contextOf(t, out.String())
	assert.Contains(t, ctx, "go-toolchain")
	assert.NotContains(t, ctx, "buildhost")
}

func TestNoMatchProducesNoContext(t *testing.T) {
	e, out, errOut := testEnv(t)
	require.Equal(t, 0, run(strings.NewReader(submit("write me a haiku about rain", "s1")), e))
	assert.Equal(t, "{}\n", out.String())
	assert.Empty(t, errOut.String())
}

func TestOtherEventsAreIgnored(t *testing.T) {
	e, out, _ := testEnv(t)
	in := `{"hook_event_name":"PreToolUse","prompt":"buildhost","session_id":"s1"}`
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
	e, out, errOut := testEnv(t)
	require.Equal(t, 0, run(strings.NewReader(submit("upload to buildhost", "")), e))
	assert.Contains(t, contextOf(t, out.String()), "buildhost")
	assert.Contains(t, errOut.String(), "no session_id")
}

func TestCapReportsWhatItDropped(t *testing.T) {
	e, out, errOut := testEnv(t)
	prompt := "buildhost and go test and github action and webgpu and xsd work"
	require.Equal(t, 0, run(strings.NewReader(submit(prompt, "s1")), e))

	ctx := contextOf(t, out.String())
	assert.Equal(t, maxSuggestions, strings.Count(ctx, "- **"))
	assert.Contains(t, errOut.String(), "beyond the cap")
}

func TestUnwritableStateWarnsAndStillSuggests(t *testing.T) {
	e, out, errOut := testEnv(t)
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))
	e.stateIn = blocked

	require.Equal(t, 0, run(strings.NewReader(submit("upload to buildhost", "s1")), e))
	assert.Contains(t, contextOf(t, out.String()), "buildhost")
	assert.Contains(t, errOut.String(), "can repeat later in the session")
}

func TestHomeOverlayAddsARepo(t *testing.T) {
	e, out, _ := testEnv(t)
	writeOverlay(t, e.home, `{"repos":[{"name":"me/thing","url":"https://example.com/thing",
		"description":"A local thing.","match":["frobnicate"]}]}`)

	require.Equal(t, 0, run(strings.NewReader(submit("time to frobnicate", "s1")), e))
	assert.Contains(t, contextOf(t, out.String()), "me/thing")
}

func TestCwdOverlayWinsOverHome(t *testing.T) {
	e, out, _ := testEnv(t)
	cwd := t.TempDir()
	writeOverlay(t, e.home, `{"repos":[{"name":"me/thing","url":"https://example.com/home",
		"description":"Home copy.","match":["frobnicate"]}]}`)
	writeOverlay(t, cwd, `{"repos":[{"name":"me/thing","url":"https://example.com/project",
		"description":"Project copy.","match":["frobnicate"]}]}`)

	in := hookInput{HookEventName: "UserPromptSubmit", SessionID: "s1", Prompt: "frobnicate", Cwd: cwd}
	data, _ := json.Marshal(in)
	require.Equal(t, 0, run(bytes.NewReader(data), e))

	ctx := contextOf(t, out.String())
	assert.Contains(t, ctx, "https://example.com/project")
	assert.NotContains(t, ctx, "https://example.com/home")
}

func TestOverlayMayReplaceABuiltInEntry(t *testing.T) {
	e, out, _ := testEnv(t)
	writeOverlay(t, e.home, `{"repos":[{"name":"wow-look-at-my/buildhost","url":"https://mirror.example.com",
		"description":"Our mirror.","match":["buildhost"]}]}`)

	require.Equal(t, 0, run(strings.NewReader(submit("buildhost", "s1")), e))
	ctx := contextOf(t, out.String())
	assert.Contains(t, ctx, "https://mirror.example.com")
	assert.NotContains(t, ctx, "https://github.com/wow-look-at-my/buildhost")
}

func TestMalformedOverlayFailsLoud(t *testing.T) {
	e, out, errOut := testEnv(t)
	writeOverlay(t, e.home, `{"repos":[`)

	assert.Equal(t, 1, run(strings.NewReader(submit("buildhost", "s1")), e))
	assert.Empty(t, out.String())
	assert.Contains(t, errOut.String(), "not valid JSON")
}

func TestUnreadableOverlayFailsLoud(t *testing.T) {
	e, _, errOut := testEnv(t)
	dir := filepath.Join(e.home, ".claude", "repo-index.json")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	assert.Equal(t, 1, run(strings.NewReader(submit("buildhost", "s1")), e))
	assert.Contains(t, errOut.String(), "cannot read")
}

func TestOverlayEntryMustBeUsable(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"no name":        {`{"repos":[{"url":"u","description":"d","match":["m"]}]}`, "has no name"},
		"no url":         {`{"repos":[{"name":"n","description":"d","match":["m"]}]}`, "has no url"},
		"no description": {`{"repos":[{"name":"n","url":"u","match":["m"]}]}`, "has no description"},
		"no match":       {`{"repos":[{"name":"n","url":"u","description":"d"}]}`, "never be suggested"},
		"empty phrase":   {`{"repos":[{"name":"n","url":"u","description":"d","match":[""]}]}`, "empty match phrase"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e, _, errOut := testEnv(t)
			writeOverlay(t, e.home, tc.body)
			assert.Equal(t, 1, run(strings.NewReader(submit("anything", "s1")), e))
			assert.Contains(t, errOut.String(), tc.want)
		})
	}
}
