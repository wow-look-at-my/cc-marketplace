package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// release-plugin is the whole publishing pipeline for one plugin: cook, check
// the hooks survived, reduce build/ to the APE and its launcher, and write the
// manifest the marketplace job reads back. Its pieces are unit-tested
// individually; these drive the command itself, because the ORDER is the part
// that breaks -- staging before cooking would delete the binaries it just
// copied, and writing the manifest before staging would describe a layout that
// no longer exists.

// fakeRepo builds a repo root holding one plugin and points the package's
// cached repoRoot at it. Returns the plugin's source directory.
func fakeRepo(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "plugins", name)
	for rel, body := range files {
		p := filepath.Join(src, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		mode := os.FileMode(0o644)
		if strings.HasPrefix(rel, "build/") || strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		require.NoError(t, os.WriteFile(p, []byte(body), mode))
	}

	orig := repoRoot
	repoRoot = root
	t.Cleanup(func() { repoRoot = orig })

	// The cooked tree goes to os.MkdirTemp and is deliberately never cleaned up
	// (the workflow uploads it), so keep it inside the test's own temp dir.
	t.Setenv("TMPDIR", t.TempDir())
	return src
}

// releaseOutput runs the command with stdout captured: the workflow parses
// those key=value lines, so they are part of the contract.
func releaseOutput(t *testing.T, name string) (map[string]string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w

	runErr := runReleasePlugin(releasePluginCmd, []string{name})

	os.Stdout = orig
	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	out := map[string]string{}
	for _, line := range strings.Split(buf.String(), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			out[k] = v
		}
	}
	return out, runErr
}

func TestRunReleasePluginProducesAnInstallableTree(t *testing.T) {
	mockGitDefaults(t)
	t.Setenv("GITHUB_RUN_NUMBER", "42")
	fakeRepo(t, "glob", map[string]string{
		".claude-plugin/plugin.json": `{"name":"glob","version":"0.0.0","hooks":{"PreToolUse":[{"matcher":"Glob"}]}}`,
		".mcp.json":                  `{"mcpServers":{"glob":{"command":"./build/glob"}}}`,
		"README.md":                  "# glob",
		"README.template.md":         "# {{ template }}",
		"main.go":                    "package main",
		"build/glob" + apeSuffix:     apeMagic + "fat",
		"build/glob_linux_amd64":     apeMagic + "fat",
	})

	out, err := releaseOutput(t, "glob")
	require.NoError(t, err)

	require.Equal(t, "glob#42", out["tag"], "the immutable per-release tag marketplace.json pins")
	require.Equal(t, "42", out["version"])
	cooked := out["source_dir"]
	require.NotEmpty(t, cooked, "the workflow uploads this directory as the artifact")

	// build/ is reduced to the APE and the launcher every manifest names.
	entries, err := os.ReadDir(filepath.Join(cooked, "build"))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{"glob.ape", "glob"}, names,
		"staging must run AFTER cooking copied build/, or it would delete what it just staged")

	// The manifest describes the tree as it now stands.
	m := readManifest(t, cooked)
	require.Equal(t, "glob#42", m.Tag)
	require.Equal(t, "42", m.PluginJSON["version"], "the cooked version, not the source's 0.0.0")
	require.Contains(t, m.PluginJSON, "hooks",
		"hooks reaching the cooked plugin.json is what validateHooksPreserved guards; "+
			"lose them and the plugin installs but silently never fires")

	// Cooking rules still hold through the command.
	require.NoFileExists(t, filepath.Join(cooked, "README.template.md"))
	require.NoFileExists(t, filepath.Join(cooked, "main.go"))
	require.FileExists(t, filepath.Join(cooked, "README.md"))
	require.FileExists(t, filepath.Join(cooked, "mh.plugin.json"))
}

// A skills-only plugin ships no binary and must release exactly the same way.
func TestRunReleasePluginWithoutBinaries(t *testing.T) {
	mockGitDefaults(t)
	t.Setenv("GITHUB_RUN_NUMBER", "9")
	fakeRepo(t, "css-cascade", map[string]string{
		".claude-plugin/plugin.json": `{"name":"css-cascade","version":"0.0.0"}`,
		"skills/cascade/SKILL.md":    "# cascade",
	})

	out, err := releaseOutput(t, "css-cascade")
	require.NoError(t, err)
	require.Equal(t, "css-cascade#9", out["tag"])
	require.FileExists(t, filepath.Join(out["source_dir"], "skills", "cascade", "SKILL.md"))
	require.NoDirExists(t, filepath.Join(out["source_dir"], "build"))
}

// The fail-closed case, end to end: a plugin built with the per-platform matrix
// must abort the release rather than ship a package that works on some
// platforms and silently not others.
func TestRunReleasePluginFailsOnANonApeBuild(t *testing.T) {
	mockGitDefaults(t)
	fakeRepo(t, "glob", map[string]string{
		".claude-plugin/plugin.json": `{"name":"glob","version":"0.0.0"}`,
		// An ELF, not an APE: the per-platform matrix build this fails closed on.
		"build/glob_linux_amd64": "\x7fELFnative",
	})

	_, err := releaseOutput(t, "glob")
	require.Error(t, err)
	require.Contains(t, err.Error(), "stage plugin binary")
}

func TestRunReleasePluginRejectsAnUnknownPlugin(t *testing.T) {
	mockGitDefaults(t)
	fakeRepo(t, "glob", map[string]string{".claude-plugin/plugin.json": `{"name":"glob"}`})

	_, err := releaseOutput(t, "not-a-plugin")
	require.Error(t, err)
	require.Contains(t, err.Error(), "plugin not found")
}

// Both git lookups feed the source URL baked into mh.plugin.json, so neither
// may fail silently into a release that misattributes its own provenance.
func TestRunReleasePluginFailsWhenGitDoes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failOn  string
		wantErr string
	}{
		{"head sha", "rev-parse", "HEAD SHA"},
		{"repo info", "remote", "repo info"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockGit(t, func(args ...string) (string, error) {
				if len(args) > 0 && args[0] == tc.failOn {
					return "", fmt.Errorf("git is unhappy")
				}
				if len(args) > 0 && args[0] == "remote" {
					return "https://github.com/test-owner/test-repo.git\n", nil
				}
				return "abc123\n", nil
			})
			fakeRepo(t, "glob", map[string]string{".claude-plugin/plugin.json": `{"name":"glob"}`})

			_, err := releaseOutput(t, "glob")
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Outside CI there is no run number; the release still has to produce a valid
// version rather than a zero or a crash.
func TestReleaseVersionFallsBackOutsideCI(t *testing.T) {
	t.Setenv("GITHUB_RUN_NUMBER", "")
	require.Equal(t, 1, releaseVersion())

	t.Setenv("GITHUB_RUN_NUMBER", "not-a-number")
	require.Equal(t, 1, releaseVersion())

	t.Setenv("GITHUB_RUN_NUMBER", "17")
	require.Equal(t, 17, releaseVersion())
}

// mh.plugin.json is the provenance record: which commit produced this tag.
func TestRunReleasePluginRecordsProvenance(t *testing.T) {
	mockGitDefaults(t)
	fakeRepo(t, "glob", map[string]string{".claude-plugin/plugin.json": `{"name":"glob"}`})

	out, err := releaseOutput(t, "glob")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(out["source_dir"], "mh.plugin.json"))
	require.NoError(t, err)
	var meta releaseMetadata
	require.NoError(t, json.Unmarshal(data, &meta))
	require.Equal(t, "abc123def456789012345678901234567890abcd", meta.SourceCommit)
	require.Contains(t, meta.SourceURL, "test-owner/test-repo")
	require.Contains(t, meta.SourceURL, "/plugins/glob")
	require.NotEmpty(t, meta.BuiltAt)
}
