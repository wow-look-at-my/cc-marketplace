package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// isolate gives each test its own HOME, TMPDIR and working tree, so the marker,
// the candidate sweep and the real session's files never collide.
func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("TMPDIR", filepath.Join(root, "tmp"))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "home", ".claude"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o755))
	repo := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	return repo
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// wrapped builds a file of n characters that is legitimately hard-wrapped, so
// size violations can be tested without also tripping the width rule.
func wrapped(n int) string {
	line := strings.Repeat("a", 100) + "\n"
	return strings.Repeat(line, n/len(line)+1)[:n]
}

func fire(t *testing.T, payload map[string]any) string {
	t.Helper()
	out, _ := fireCode(t, payload)
	return out
}

// fireCode is fire plus the exit code, for full_scan: the only path where the
// code carries a real signal instead of always being 0.
func fireCode(t *testing.T, payload map[string]any) (string, int) {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return run(strings.NewReader(string(data)))
}

// additionalContext pulls the model-facing text out of a hook response.
func additionalContext(t *testing.T, out string) string {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return ""
	}
	var parsed struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	require.NoErrorf(t, json.Unmarshal([]byte(out), &parsed), "invalid JSON: %q", out)
	return parsed.HookSpecificOutput.AdditionalContext
}

func blockReason(t *testing.T, out string) string {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return ""
	}
	var parsed struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	require.NoErrorf(t, json.Unmarshal([]byte(out), &parsed), "invalid JSON: %q", out)
	if parsed.Decision != "block" {
		return ""
	}
	return parsed.Reason
}

func TestSessionStartNamesAnOversizedFile(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(50000))

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo,
	}))
	require.Contains(t, ctx, "BUDGET EXCEEDED")
	require.Contains(t, ctx, "50,000 chars")
	require.Contains(t, ctx, "1.25x budget")
}

func TestSessionStartSilentWhenClean(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(1000))

	out := fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	require.Empty(t, out, "a healthy file must cost the model zero tokens")
}

// The wall band: a file that passes a plain >budget test in silence is a loaded
// gun for whoever edits it next.
func TestSessionStartReportsTheWall(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(39500))

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo,
	}))
	require.Contains(t, ctx, "AT THE BUDGET WALL")
	require.Contains(t, ctx, "of room left")
}

func TestSessionStartSeesSnippetsAndSiblingRepos(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(os.Getenv("HOME"), ".claude/claude_snippets/huge.md"), wrapped(41000))
	writeFile(t, filepath.Join(repo, "sibling", "CLAUDE.md"), wrapped(45000))

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo,
	}))
	require.Contains(t, ctx, "huge.md", "an @-imported snippet is measured on its own")
	require.Contains(t, ctx, filepath.Join("sibling", "CLAUDE.md"), "a sibling checkout counts")
	// Worst first: that is the one worth fixing.
	require.Less(t, strings.Index(ctx, "sibling"), strings.Index(ctx, "huge.md"))
}

// The blind spot that made the whole guard skippable: a CLAUDE.md rewritten by
// Bash names no file_path, so a check keyed on that field measures nothing.
func TestPostToolUseCatchesAnEditThatNamesNoPath(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	writeFile(t, claude, wrapped(1000))

	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, wrapped(60000))

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Bash", "tool_input": map[string]any{"command": "sed -i ..."},
	}))
	require.Contains(t, ctx, "the file you just wrote is OVER")
}

func TestPostToolUseCatchesANamedEdit(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, wrapped(60000))

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude},
	}))
	require.Contains(t, ctx, "OVER the 40,000-character budget")
}

// Off by default: an unwrapped file that is nowhere near the budget is not a
// finding at all unless the width check is switched on.
func TestWidthIsOffByDefault(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, "# ok\n\n"+strings.Repeat("word ", 200)+"\n")

	require.Empty(t, fire(t, map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude},
	}), "a small unwrapped file must report nothing while the width check is off")
	require.Empty(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo,
	}), "and must not block the end of the turn either")
}

// Width is an offense in its own right: a file can be well under budget and
// still be unreviewable. Opt-in via CC_CLAUDE_MD_WIDTH.
func TestPostToolUseFlagsUnwrappedLines(t *testing.T) {
	t.Setenv("CC_CLAUDE_MD_WIDTH", "1")
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, "# ok\n\n"+strings.Repeat("word ", 200)+"\n")

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude},
	}))
	require.Contains(t, ctx, "over 150 columns")
	require.Contains(t, ctx, "within budget", "a small unwrapped file is not also a size violation")

	// The hole that let the false headline ship: the two assertions above are
	// both about the DETAIL line, which was always right. Nothing checked what
	// the report LEADS with, so every width-only notice opened by claiming the
	// budget wall on a file with thousands of characters to spare -- and a guard
	// that cries wolf gets skimmed on the run where the number is real.
	require.NotContains(t, ctx, "budget wall",
		"a file under budget must not be reported as being at the wall")
	require.NotContains(t, ctx, "OVER the",
		"a file under budget must not be reported as over it")
	require.Contains(t, ctx, "INSTRUCTION-FILE WIDTH",
		"the headline must name the offense that actually fired")

	// And the remedy has to be actionable: extraction advice is noise for a file
	// with nothing to extract.
	require.NotContains(t, ctx, "docs/<topic>.md",
		"do not prescribe extraction for a file that is under budget")
	require.Contains(t, ctx, "Hard-wrap at 150 columns")
}

func TestFencedAndIndentedBlocksAreNotWidthViolations(t *testing.T) {
	long := strings.Repeat("x", 400)
	cases := map[string]string{
		"code fence":      "```\n" + long + "\n```\n",
		"indented block":  "    " + long + "\n",
		"table row":       "| " + long + " |\n",
		"blockquote":      "> " + long + "\n",
		"heading":         "# " + long + "\n",
		"unbreakable URL": "https://example.com/" + long + "\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			require.Empty(t, wideLines(text))
		})
	}
	require.NotEmpty(t, wideLines(strings.Repeat("word ", 200)+"\n"), "ordinary prose must still be caught")
}

func TestStopBlocksAFileThisSessionLeftOversized(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, wrapped(60000))
	fire(t, map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude}})

	reason := blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo,
	}))
	require.Contains(t, reason, "ending the turn")
	require.Contains(t, reason, "VERBATIM")
}

// The no-wedge property: a file left exactly as the gate found it never blocks
// twice, or a session could never end.
func TestStopDoesNotBlockTwiceForAnUnchangedFile(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, wrapped(60000))
	fire(t, map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude}})

	require.NotEmpty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo})))
	require.Empty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo})),
		"an unchanged file must not block a second time")
}

// ...but touching it and still leaving it broken is a NEW violation. Blocking
// once per session was the other half of what made this ignorable.
func TestStopBlocksAgainAfterAnotherBadEdit(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, wrapped(60000))
	fire(t, map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude}})
	require.NotEmpty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo})))

	writeFile(t, claude, wrapped(70000))
	fire(t, map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude}})

	require.NotEmpty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo})),
		"re-breaking the same file must block again")
}

func TestStopAllowsOnceTheFileIsFixed(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, wrapped(60000))
	fire(t, map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude}})

	writeFile(t, claude, wrapped(9000))
	require.Empty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo})))
}

// A clean turn must not disarm the guard for the rest of the session: dropping
// the marker threw away the snapshot the sweep diffs against.
func TestACleanStopKeepsTheGuardArmed(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	writeFile(t, claude, wrapped(1000))
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	require.Empty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo})))

	writeFile(t, claude, wrapped(60000))
	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo, "tool_name": "Bash"}))
	require.Contains(t, ctx, "OVER", "a clean stop must not disarm the sweep")
}

func TestStopHookActiveNeverWedges(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, claude, wrapped(60000))
	fire(t, map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude}})

	require.Empty(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo, "stop_hook_active": true}))
}

// A file the session never touched is reported at session start but must not
// block the turn: the gate is about what THIS session left broken.
func TestStopIgnoresAFileThisSessionDidNotWrite(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(60000))
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})

	require.Empty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "s", "cwd": repo})))
}

func TestNonInstructionFilesAreIgnored(t *testing.T) {
	repo := isolate(t)
	other := filepath.Join(repo, "README.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo})
	writeFile(t, other, wrapped(90000))

	require.Empty(t, fire(t, map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": other}}))
}

func TestFailsOpen(t *testing.T) {
	isolate(t)
	for _, raw := range []string{"", "not json", "[]", "null"} {
		require.NotPanics(t, func() { run(strings.NewReader(raw)) }, "garbage stdin: %q", raw)
	}
}

func TestDisabledByEnv(t *testing.T) {
	repo := isolate(t)
	t.Setenv("CC_CLAUDE_MD_BUDGET", "0")
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(90000))

	require.Empty(t, fire(t, map[string]any{
		"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo}))
}

func TestBudgetOverride(t *testing.T) {
	repo := isolate(t)
	t.Setenv("CC_CLAUDE_MD_BUDGET", "5000")
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(6000))

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo}))
	require.Contains(t, ctx, "5,000-character budget")
}

// Characters, not bytes: a file of multi-byte text must not be judged by its
// byte count.
func TestMeasuresCharactersNotBytes(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	// 30,000 three-byte runes = 90,000 bytes but only 30,000 characters.
	writeFile(t, claude, strings.Repeat("世\n", 15000))

	require.Empty(t, fire(t, map[string]any{
		"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo}),
		"90,000 bytes of 30,000 characters is within budget")
}

func TestMarkerIsPerSession(t *testing.T) {
	repo := isolate(t)
	claude := filepath.Join(repo, "CLAUDE.md")
	fire(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "a", "cwd": repo})
	writeFile(t, claude, wrapped(60000))
	fire(t, map[string]any{"hook_event_name": "PostToolUse", "session_id": "a", "cwd": repo,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": claude}})

	require.Empty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "b", "cwd": repo})),
		"another session must not inherit the offender list")
	require.NotEmpty(t, blockReason(t, fire(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "a", "cwd": repo})))
}

func TestNoSessionIDDoesNotWedge(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(60000))
	// The census still reports; the Stop gate has nothing to consult.
	require.NotEmpty(t, fire(t, map[string]any{"hook_event_name": "SessionStart", "cwd": repo}))
	require.Empty(t, fire(t, map[string]any{"hook_event_name": "Stop", "cwd": repo}))
}

// TestSessionStartFindsNestedOffender is the case that once let a real
// violation through unseen: the census used to guess one level of siblings
// from cwd, so a file two directories down never entered it, and only a CI
// job's separate hand-rolled walk ever caught it. SessionStart now shares
// full_scan's recursive walk (allCandidatePaths -> claudeMdFiles), so there
// is no shallower mode left for a live session to fall back to.
func TestSessionStartFindsNestedOffender(t *testing.T) {
	repo := isolate(t)
	nested := filepath.Join(repo, "src", "hooks", "pr-resolve", "CLAUDE.md")
	writeFile(t, nested, wrapped(45000))

	ctx := additionalContext(t, fire(t, map[string]any{
		"hook_event_name": "SessionStart", "session_id": "s", "cwd": repo,
	}))
	require.Contains(t, ctx, "BUDGET EXCEEDED")
	require.Contains(t, ctx, nested)
}

// ---- full_scan: the CI-oriented sweep, never sent by Claude Code ----

// TestFullScanFindsNestedOffender proves full_scan and SessionStart
// (TestSessionStartFindsNestedOffender above) see the same tree.
func TestFullScanFindsNestedOffender(t *testing.T) {
	repo := isolate(t)
	nested := filepath.Join(repo, "src", "hooks", "pr-resolve", "CLAUDE.md")
	writeFile(t, nested, wrapped(45000))

	out, code := fireCode(t, map[string]any{"full_scan": true, "cwd": repo})
	require.Equal(t, 1, code, "a genuinely over-budget file must exit non-zero")
	require.Contains(t, out, "BUDGET EXCEEDED")
	require.Contains(t, out, nested)
}

func TestFullScanCleanTreeExitsZeroSilently(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, "src", "hooks", "x", "CLAUDE.md"), "short")

	out, code := fireCode(t, map[string]any{"full_scan": true, "cwd": repo})
	require.Equal(t, 0, code)
	require.Empty(t, out)
}

// TestFullScanNearWallDoesNotFailTheBuild: a file AT the wall but not over is
// worth naming in the log, exactly like the SessionStart census -- but it
// must not flip CI red, because that would be a behavior CI does not have
// today failing builds that currently pass.
func TestFullScanNearWallDoesNotFailTheBuild(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(39500)) // 98.75% of 40,000

	out, code := fireCode(t, map[string]any{"full_scan": true, "cwd": repo})
	require.Equal(t, 0, code, "near the wall is a warning, not a violation")
	require.Contains(t, out, "BUDGET WALL")
}

// TestFullScanSkipsGitAndNodeModules: a vendored or object-database CLAUDE.md
// (a fixture, a dependency's docs) must not fail someone else's build.
func TestFullScanSkipsGitAndNodeModules(t *testing.T) {
	repo := isolate(t)
	writeFile(t, filepath.Join(repo, ".git", "CLAUDE.md"), wrapped(50000))
	writeFile(t, filepath.Join(repo, "node_modules", "dep", "CLAUDE.md"), wrapped(50000))
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "short")

	out, code := fireCode(t, map[string]any{"full_scan": true, "cwd": repo})
	require.Equal(t, 0, code)
	require.Empty(t, out)
}

// TestFullScanRespectsBudgetOverride: full_scan shares budget() with every
// other path, so CC_CLAUDE_MD_BUDGET=0 disables it too, exactly like a real
// session.
func TestFullScanRespectsBudgetOverride(t *testing.T) {
	repo := isolate(t)
	t.Setenv("CC_CLAUDE_MD_BUDGET", "0")
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), wrapped(90000))

	out, code := fireCode(t, map[string]any{"full_scan": true, "cwd": repo})
	require.Equal(t, 0, code)
	require.Empty(t, out)
}
