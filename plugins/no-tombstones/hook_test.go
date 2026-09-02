package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func payload(t *testing.T, event, tool string, input map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	out, err := json.Marshal(map[string]any{
		"hook_event_name": event,
		"tool_name":       tool,
		"tool_input":      json.RawMessage(raw),
	})
	require.NoError(t, err)
	return string(out)
}

func decision(t *testing.T, out string) (string, string) {
	t.Helper()
	if out == "" {
		return "", ""
	}
	var res response
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res.HookSpecificOutput.PermissionDecision, res.HookSpecificOutput.PermissionDecisionReason
}

// Each write shape carries the same tombstone, and each is paired with the same
// text in a file this plugin does not judge, so a deny cannot come from
// somewhere else.
func TestEveryWriteShapeIsJudged(t *testing.T) {
	const tomb = "// the old flag was removed here\nfunc f() {}"
	shapes := map[string]map[string]any{
		"Write": {"file_path": "/repo/a.go", "content": tomb},
		"Edit":  {"file_path": "/repo/a.go", "new_string": tomb},
		"MultiEdit": {"file_path": "/repo/a.go", "edits": []map[string]any{
			{"new_string": "func g() {}"}, {"new_string": tomb},
		}},
	}
	for tool, input := range shapes {
		t.Run(tool, func(t *testing.T) {
			verdict, reason := decision(t, run(strings.NewReader(payload(t, "PreToolUse", tool, input))))
			assert.Equal(t, "deny", verdict)
			assert.Contains(t, reason, "a former state")

			control := map[string]any{}
			for k, v := range input {
				control[k] = v
			}
			control["file_path"] = "/repo/a.xyzzy"
			out := run(strings.NewReader(payload(t, "PreToolUse", tool, control)))
			assert.Empty(t, out, "the same text in an unjudged file must pass")
		})
	}
}

func TestOrdinaryCodeAndCommentsPass(t *testing.T) {
	src := "// f returns EINVAL when the buffer is smaller than the Apple struct.\nfunc f() {}"
	out := run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/a.go", "content": src})))
	assert.Empty(t, out)
}

func TestADocumentIsJudgedWithoutTheVolumeCap(t *testing.T) {
	var lines []string
	for range 30 {
		lines = append(lines, "the emulation forwards a pointer and validates the size.")
	}
	out := run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/notes.md", "content": strings.Join(lines, "\n")})))
	assert.Empty(t, out, "a long paragraph is ordinary writing")

	verdict, _ := decision(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/notes.md", "content": "The apetest path previously skipped this."}))))
	assert.Equal(t, "deny", verdict)
}

func TestTheVolumeCapIsTunable(t *testing.T) {
	var lines []string
	for range 20 {
		lines = append(lines, "// the handler runs after entersyscall")
	}
	in := payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/a.go", "content": strings.Join(lines, "\n")})

	verdict, _ := decision(t, run(strings.NewReader(in)))
	assert.Equal(t, "deny", verdict)

	t.Setenv("NO_TOMBSTONES_MAX_COMMENT_LINES", "0")
	assert.Empty(t, run(strings.NewReader(in)))
}

// The refused lines are appended rather than dropped: the history is real and
// belongs in the commit message, so a deny that destroyed it would be argued
// with rather than obeyed.
func TestRefusedLinesAreRelocatedIntoTheGitDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	file := filepath.Join(root, "a.go")

	verdict, reason := decision(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": file, "content": "// the old flag was removed here"}))))
	require.Equal(t, "deny", verdict)

	ledgerPath := filepath.Join(root, ".git", "TOMBSTONES")
	assert.Contains(t, reason, ledgerPath)
	body, err := os.ReadFile(ledgerPath)
	require.NoError(t, err)
	assert.Contains(t, string(body), "the old flag was removed here")
	assert.Contains(t, string(body), file)
}

// A name the repository does not define is the tier no rewording defeats: this
// sentence carries no tell at all, only a referent that is gone.
func TestANameNothingDefinesIsRefused(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed; this tier reports nothing without it")
	}
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	require.NoError(t, os.WriteFile(filepath.Join(root, "live.go"),
		[]byte("package p\n\nfunc TestDarwinStatfsToLinux() {}\n"), 0o600))
	file := filepath.Join(root, "a.go")

	verdict, reason := decision(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": file, "content": "// see TestDarwinMntFlagsToLinux for the pin\nfunc f() {}"}))))
	assert.Equal(t, "deny", verdict)
	assert.Contains(t, reason, "TestDarwinMntFlagsToLinux")

	out := run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": file, "content": "// see TestDarwinStatfsToLinux for the pin\nfunc f() {}"})))
	assert.Empty(t, out, "a name the repository does define must pass")
}

// An external constant is exactly the shape a low-level comment is full of, and
// denying over one is how a guard gets turned off.
func TestExternalConstantsAreNeverCandidates(t *testing.T) {
	blocks := commentBlocks("// returns ENOSYS unless RLIMIT_CORE and SYS_STATFS agree", cLike)
	assert.Empty(t, DeadReferents("/nonexistent/a.go", "", blocks))

	for _, name := range []string{"ENOSYS", "RLIMIT_CORE", "SYS_STATFS", "statfs", "buf"} {
		assert.NotContains(t, candidate.FindAllString(name, -1), name,
			"%s must not be eligible, or must be filtered by length/case", name)
	}
}

func TestEveryFailurePathAllowsTheCall(t *testing.T) {
	cases := map[string]string{
		"unparseable payload": "{not json",
		"other event":         payload(t, "Stop", "Write", map[string]any{"file_path": "/repo/a.go", "content": "// used to work"}),
		"other tool":          payload(t, "PreToolUse", "Bash", map[string]any{"file_path": "/repo/a.go", "content": "// used to work"}),
		"no file path":        payload(t, "PreToolUse", "Write", map[string]any{"content": "// used to work"}),
		"empty write":         payload(t, "PreToolUse", "Write", map[string]any{"file_path": "/repo/a.go", "content": ""}),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, run(strings.NewReader(in)))
		})
	}

	bad := `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":"not an object"}`
	assert.Empty(t, run(strings.NewReader(bad)))
}

func TestRepoRootReportsNothingOutsideAWorkingTree(t *testing.T) {
	assert.Empty(t, RepoRoot(filepath.Join(t.TempDir(), "a.go")))
	assert.Empty(t, Relocate(filepath.Join(t.TempDir(), "a.go"), []Hit{{Line: "x"}}))
}
