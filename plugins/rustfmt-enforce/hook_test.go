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

const ugly = "fn  main( ){let x=1;println!(\"{}\",x);}\n"
const pretty = "fn main() {\n    let x = 1;\n    println!(\"{}\", x);\n}\n"

func requireRustfmt(t *testing.T) {
	t.Helper()
	_, err := exec.LookPath("rustfmt")
	require.NoError(t, err, "these tests run the real rustfmt; install it with `rustup component add rustfmt`")
}

func payload(t *testing.T, v map[string]any) *strings.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return strings.NewReader(string(b))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func postEdit(t *testing.T, path string) result {
	return run(payload(t, map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Edit",
		"tool_input":      map[string]any{"file_path": path},
	}))
}

func TestPostToolUseFormatsTheFile(t *testing.T) {
	requireRustfmt(t)
	path := filepath.Join(t.TempDir(), "main.rs")
	write(t, path, ugly)
	res := postEdit(t, path)
	assert.Equal(t, 0, res.code)
	assert.Equal(t, pretty, read(t, path))
	assert.Contains(t, res.stdout, "rustfmt reformatted")
	assert.Contains(t, res.stdout, `"hookEventName":"PostToolUse"`)
}

func TestPostToolUseIsSilentOnACleanFile(t *testing.T) {
	requireRustfmt(t)
	path := filepath.Join(t.TempDir(), "main.rs")
	write(t, path, pretty)
	res := postEdit(t, path)
	assert.Equal(t, result{}, res)
}

func TestPostToolUseReportsAParseError(t *testing.T) {
	requireRustfmt(t)
	path := filepath.Join(t.TempDir(), "main.rs")
	write(t, path, "fn main( {\n")
	res := postEdit(t, path)
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "rustfmt failed on "+path)
	assert.Equal(t, "fn main( {\n", read(t, path), "a failed format must leave the file as it was")
}

func TestPostToolUseIgnoresOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	write(t, path, ugly)
	assert.Equal(t, result{}, postEdit(t, path))
	assert.Equal(t, ugly, read(t, path))
}

func TestPostToolUseDoesNotFollowModDeclarations(t *testing.T) {
	requireRustfmt(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "lib.rs"), "mod child;\n")
	write(t, filepath.Join(dir, "child.rs"), ugly)
	postEdit(t, filepath.Join(dir, "lib.rs"))
	assert.Equal(t, ugly, read(t, filepath.Join(dir, "child.rs")))
}

func TestMissingRustfmtFailsLoud(t *testing.T) {
	t.Setenv("RUSTFMT_ENFORCE_BIN", "rustfmt-does-not-exist")
	path := filepath.Join(t.TempDir(), "main.rs")
	write(t, path, ugly)
	res := postEdit(t, path)
	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "rustup component add rustfmt")
}

func TestCargoEdition(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "Cargo.toml"), "[workspace]\nmembers = [\"a\", \"b\", \"c\"]\n[workspace.package]\nedition = \"2024\"\n")
	write(t, filepath.Join(root, "a", "Cargo.toml"), "[package]\nname = \"a\"\nedition = \"2018\"\n")
	write(t, filepath.Join(root, "b", "Cargo.toml"), "[package]\nname = \"b\"\nedition.workspace = true\n")
	write(t, filepath.Join(root, "c", "Cargo.toml"), "[package]\nname = \"c\"\n")
	assert.Equal(t, "2018", cargoEdition(filepath.Join(root, "a", "src")))
	assert.Equal(t, "2024", cargoEdition(filepath.Join(root, "b", "src")))
	assert.Equal(t, "2015", cargoEdition(filepath.Join(root, "c", "src")))
	assert.Equal(t, "", cargoEdition(t.TempDir()))
}

func TestCommitDirs(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"git commit -m x", []string{"/w"}},
		{"git add -A && git commit -am 'x'", []string{"/w"}},
		{"cd sub && git commit -m x", []string{"/w/sub"}},
		{"git -C /r -c user.name=a commit", []string{"/r"}},
		{"git log --grep commit", nil},
		{"echo git commit", nil},
		{"git status; git commit\ngit push", []string{"/w"}},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, commitDirs(c.cmd, "/w"), c.cmd)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		_, err := git(dir, args...)
		require.NoError(t, err)
	}
	return dir
}

func preCommit(t *testing.T, cwd string) result {
	return run(payload(t, map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"cwd":             cwd,
		"tool_input":      map[string]any{"command": "git commit -m wip"},
	}))
}

func TestCommitDeniedOnUnformattedStagedFile(t *testing.T) {
	requireRustfmt(t)
	repo := gitRepo(t)
	path := filepath.Join(repo, "src", "main.rs")
	write(t, path, ugly)
	_, err := git(repo, "add", "-A")
	require.NoError(t, err)
	res := preCommit(t, repo)
	assert.Contains(t, res.stdout, `"permissionDecision":"deny"`)
	assert.Contains(t, res.stdout, "rustfmt reformatted it")
	assert.Equal(t, pretty, read(t, path))

	_, err = git(repo, "add", "-A")
	require.NoError(t, err)
	assert.Equal(t, result{}, preCommit(t, repo), "after git add the commit goes through")
}

func TestCommitDeniedOnUnformattedModifiedFile(t *testing.T) {
	requireRustfmt(t)
	repo := gitRepo(t)
	path := filepath.Join(repo, "main.rs")
	write(t, path, pretty)
	_, err := git(repo, "add", "-A")
	require.NoError(t, err)
	_, err = git(repo, "commit", "-qm", "init")
	require.NoError(t, err)
	write(t, path, ugly)
	res := preCommit(t, repo)
	assert.Contains(t, res.stdout, `"permissionDecision":"deny"`)
}

func TestCommitAllowedWithoutRustChanges(t *testing.T) {
	repo := gitRepo(t)
	write(t, filepath.Join(repo, "a.txt"), ugly)
	_, err := git(repo, "add", "-A")
	require.NoError(t, err)
	assert.Equal(t, result{}, preCommit(t, repo))
}

func TestBadPayloadFailsLoud(t *testing.T) {
	res := run(strings.NewReader("not json"))
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.stderr, "parse hook payload")
}
