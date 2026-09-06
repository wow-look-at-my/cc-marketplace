package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRepo builds a real repository with one commit, plus a bare "origin" it can
// push to. The resolver shells out to git, so the only honest test of it is a
// checkout: a fake here would be testing the fake.
func newRepo(t *testing.T) (dir string, head string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir = filepath.Join(root, "work")

	run := func(wd string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = wd
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return string(out)
	}

	require.NoError(t, os.MkdirAll(origin, 0o755))
	run(origin, "init", "--bare", "--initial-branch=master", ".")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	run(dir, "init", "--initial-branch=master", ".")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644))
	run(dir, "add", "f.txt")
	run(dir, "commit", "-m", "one")
	run(dir, "remote", "add", "origin", origin)
	run(dir, "push", "-u", "origin", "master")
	// origin/HEAD is what names the default branch, and a push does not set it.
	run(dir, "remote", "set-head", "origin", "master")
	// Point origin at a github.com URL now that the remote-tracking refs exist.
	// parseRemote deliberately refuses a non-GitHub remote, and a real checkout
	// of a GitHub repository is what this resolver is written against.
	run(dir, "remote", "set-url", "origin", "https://github.com/o/r.git")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	raw, err := cmd.Output()
	require.NoError(t, err)
	return dir, string(raw[:7])
}

func TestGitResolverReadsTheCheckout(t *testing.T) {
	dir, head := newRepo(t)
	res := &GitResolver{Dir: dir}

	repo, ok := res.Repo()
	require.True(t, ok, "expected the origin remote to resolve")
	assert.Equal(t, Repo{Owner: "o", Name: "r"}, repo)

	assert.Equal(t, "master", res.DefaultBranch())
	assert.True(t, res.CommitExists(head), "HEAD must exist")
	assert.True(t, res.BranchExists("master"), "master is on the remote")
}

// A branch that exists only locally has no compare page: GitHub cannot diff
// against a ref it has never received. This is what stops the hook rendering a
// link into nothing.
func TestABranchIsOnlyFoundOnceItIsPushed(t *testing.T) {
	dir, _ := newRepo(t)
	res := &GitResolver{Dir: dir}

	cmd := exec.Command("git", "branch", "claude/local-only")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	assert.False(t, res.BranchExists("claude/local-only"), "a local branch has no page")
	assert.False(t, res.BranchExists("claude/never-existed"))
}

func TestCommitExistsRejectsWhatIsNotThere(t *testing.T) {
	dir, _ := newRepo(t)
	res := &GitResolver{Dir: dir}
	assert.False(t, res.CommitExists("e3665a4689bb"))
	assert.False(t, res.CommitExists("6884dd2"))
}

// Every answer is memoized, because one message can name the same reference
// several times and this runs in the render path.
func TestAnswersAreMemoized(t *testing.T) {
	dir, head := newRepo(t)
	res := &GitResolver{Dir: dir}

	assert.True(t, res.CommitExists(head))
	assert.True(t, res.CommitExists(head), "the second answer comes from the memo")
	assert.False(t, res.BranchExists("claude/absent"))
	assert.False(t, res.BranchExists("claude/absent"))

	first, _ := res.Repo()
	second, _ := res.Repo()
	assert.Equal(t, first, second)
}

// A directory that is not a checkout answers "no" rather than failing, so the
// hook renders the original text.
func TestOutsideACheckoutNothingResolves(t *testing.T) {
	res := &GitResolver{Dir: t.TempDir()}
	_, ok := res.Repo()
	assert.False(t, ok)
	assert.Empty(t, res.DefaultBranch())
	assert.False(t, res.BranchExists("master"))
	assert.False(t, res.CommitExists("6884dd2"))
}

// End to end through the real resolver: a reference to something in the
// checkout is rendered, and one to something absent is left alone.
func TestRewriteAgainstARealCheckout(t *testing.T) {
	dir, head := newRepo(t)
	res := &GitResolver{Dir: dir}

	out, changed := RewriteDelta("at "+head+" on master.", false, res)
	require.True(t, changed)
	assert.Contains(t, out, "](https://github.com/")
	assert.Contains(t, out, "/commit/"+head)

	_, changed = RewriteDelta("at e3665a4689bb now.", false, res)
	assert.False(t, changed, "an absent commit must not be linked")
}

func TestStateFileIgnoresAnUnsafeMessageID(t *testing.T) {
	assert.Equal(t, filepath.Join(stateDir, "abc123.txt"), stateFile("../../abc/123"))
	assert.Empty(t, priorText("no-such-message-id-here"))
}

func TestRememberAndForget(t *testing.T) {
	id := "test-" + filepath.Base(t.TempDir())
	rememberText(id, "```\nfenced")
	assert.Equal(t, "```\nfenced", priorText(id))
	assert.True(t, EndsInsideFence(priorText(id)))
	forgetText(id)
	assert.Empty(t, priorText(id))
	sweep()
}
