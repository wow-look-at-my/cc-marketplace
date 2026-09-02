package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/go-containers/set"
)

// fakeClient answers every read from a table, so the whole vendoring pipeline
// runs with no network at all.
type fakeClient struct {
	commit  string
	files   map[string]string
	missing bool
}

func (f *fakeClient) resolve(upstream) (string, error) {
	if f.missing {
		return "", fmt.Errorf("no such ref")
	}
	return f.commit, nil
}

func (f *fakeClient) get(repo, commit, path string) (string, error) {
	if body, ok := f.files[path]; ok {
		return body, nil
	}
	// Every page in the real plan resolves; an unlisted path in a test is a
	// page the fixture forgot, so say which one rather than returning empty.
	return "", fmt.Errorf("fake client has no %s (%s@%s)", path, repo, commit)
}

// everyPlannedPage builds a fixture covering the real bundle plan, so the test
// exercises the same pages the tool actually vendors.
func everyPlannedPage() map[string]string {
	files := map[string]string{}
	for _, b := range bundles {
		for _, p := range b.Pages {
			files[p.Path] = "---\ntitle: " + p.Out + "\n---\n\nBody of " + p.Out + ".\n"
		}
	}
	return files
}

const fakeCommit = "0123456789abcdef0123456789abcdef01234567"

func TestRunVendorsEveryPlannedPage(t *testing.T) {
	root := t.TempDir()
	c := &fakeClient{commit: fakeCommit, files: everyPlannedPage()}

	require.NoError(t, run(root, c))

	for _, b := range bundles {
		dir := filepath.Join(root, "plugins", "docs", "skills", b.Skill, "reference")
		for _, p := range b.Pages {
			body, err := os.ReadFile(filepath.Join(dir, p.Out))
			require.NoError(t, err, "%s/%s should exist", b.Skill, p.Out)
			assert.Contains(t, string(body), "Body of "+p.Out)
			assert.Contains(t, string(body), fakeCommit, "provenance names the commit")
		}
		notice, err := os.ReadFile(filepath.Join(dir, noticeFile))
		require.NoError(t, err)
		assert.Contains(t, string(notice), fakeCommit)
	}
}

func TestRunReportsAFailedRefResolution(t *testing.T) {
	err := run(t.TempDir(), &fakeClient{missing: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such ref")
}

func TestRunReportsAPageItCannotFetch(t *testing.T) {
	c := &fakeClient{commit: fakeCommit, files: map[string]string{}}

	err := run(t.TempDir(), c)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fake client has no")
}

// A page dropped from the plan must not leave a file behind: nothing would
// regenerate it, and it would still read as current reference.
func TestRunRemovesAFileTheePlanNoLongerProduces(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "docs", "skills", bundles[0].Skill, "reference")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stale := filepath.Join(dir, "retired-page.md")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))

	require.NoError(t, run(root, &fakeClient{commit: fakeCommit, files: everyPlannedPage()}))

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "stale page should be gone")
}

func TestPruneStaleKeepsTheNoticeAndThePlannedPages(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{noticeFile, "kept.md", "dropped.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
	}

	require.NoError(t, pruneStale(dir, "demo", set.Of[string]("kept.md")))

	for _, name := range []string{noticeFile, "kept.md"} {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.NoError(t, err, "%s should survive", name)
	}
	_, err := os.Stat(filepath.Join(dir, "dropped.md"))
	assert.True(t, os.IsNotExist(err))
}

func TestNoticeNamesEverySourceAndItsLicense(t *testing.T) {
	b := bundle{Skill: "demo", Pages: []page{
		composePage("services", "services"),
		{Src: buildkit, Path: "frontend/dockerfile/docs/reference.md", Out: "dockerfile.md"},
	}}

	out, err := notice(b, map[string]string{"docker/docs": "aaa", "moby/buildkit": "bbb"})
	require.NoError(t, err)

	assert.Contains(t, out, "`docker/docs` at commit `aaa`, licensed Apache-2.0.")
	assert.Contains(t, out, "`moby/buildkit` at commit `bbb`, licensed Apache-2.0.")
	// Sorted, so a regenerate that changed nothing produces no diff.
	assert.Less(t, strings.Index(out, "docker/docs"), strings.Index(out, "moby/buildkit"))
}
