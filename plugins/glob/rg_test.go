package main

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testRunner(timeout time.Duration) *rgRunner {
	return &rgRunner{timeout: timeout, timeoutLabel: 20, maxOutput: rgOutputCapBytes}
}

func TestRunnerTimeoutWithoutOutput(t *testing.T) {
	fake := writeFakeRg(t, "exec sleep 5")
	r := testRunner(200 * time.Millisecond)
	start := time.Now()
	lines, err := r.run(fake, []string{"--files"}, t.TempDir())
	elapsed := time.Since(start)
	assert.LessOrEqual(t, elapsed, 3*time.Second)

	assert.Nil(t, lines)

	require.NotNil(t, err)

	want := "Ripgrep search timed out after 20 seconds. The search may have matched files but did not complete in time. Try searching a more specific path or pattern."
	assert.Equal(t, want, err.Error())

}

func TestRunnerTimeoutLabelIsWSLConstantNotEffectiveTimeout(t *testing.T) {
	// Faithful quirk: the message reports the 20/60 default even when the
	// effective timeout differs.
	fake := writeFakeRg(t, "exec sleep 5")
	r := &rgRunner{timeout: 100 * time.Millisecond, timeoutLabel: 60, maxOutput: rgOutputCapBytes}
	_, err := r.run(fake, nil, t.TempDir())
	assert.False(t, err == nil || !strings.Contains(err.Error(), "timed out after 60 seconds"))

}

func TestRunnerTimeoutWithPartialOutputDropsLastLine(t *testing.T) {
	fake := writeFakeRg(t, "printf 'a.txt\\nb.txt\\n'; exec sleep 5")
	r := testRunner(300 * time.Millisecond)
	lines, err := r.run(fake, []string{"--files"}, t.TempDir())
	require.Nil(t, err)

	assert.False(t, len(lines) != 1 || lines[0] != "a.txt")

}

func TestRunnerOutputCapKillsAndResolvesPartial(t *testing.T) {
	fake := writeFakeRg(t, "while :; do echo 0123456789abcdef; done")
	r := &rgRunner{timeout: 30 * time.Second, timeoutLabel: 20, maxOutput: 4096}
	start := time.Now()
	lines, err := r.run(fake, nil, t.TempDir())
	elapsed := time.Since(start)
	assert.LessOrEqual(t, elapsed, 15*time.Second)

	require.Nil(t, err)

	require.NotEqual(t, 0, len(lines))

	for _, l := range lines {
		assert.Equal(t, "0123456789abcdef", l)

	}
}

func TestRunnerExitOneMeansNoMatches(t *testing.T) {
	fake := writeFakeRg(t, "exit 1")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	assert.False(t, err != nil || len(lines) != 0)

}

func TestRunnerExitTwoResolvesStdout(t *testing.T) {
	fake := writeFakeRg(t, "echo half-result; echo 'rg: some error' >&2; exit 2")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	require.Nil(t, err)

	assert.False(t, len(lines) != 1 || lines[0] != "half-result")

}

func TestRunnerEAGAINRetriesSingleThreaded(t *testing.T) {
	// First invocation fails with the EAGAIN signature; the retry must
	// prepend -j 1, which the fake detects to succeed.
	fake := writeFakeRg(t, `if [ "$1" = "-j" ] && [ "$2" = "1" ]; then echo retried.txt; else echo 'rg: Resource temporarily unavailable (os error 11)' >&2; exit 2; fi`)
	lines, err := testRunner(5*time.Second).run(fake, []string{"--files"}, t.TempDir())
	require.Nil(t, err)

	assert.False(t, len(lines) != 1 || lines[0] != "retried.txt")

}

func TestRunnerEAGAINRetriesOnlyOnce(t *testing.T) {
	fake := writeFakeRg(t, "echo 'rg: os error 11' >&2; exit 2")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	assert.False(t, err != nil || len(lines) != 0)

}

func TestRunnerCRLFAndBlankLineParsing(t *testing.T) {
	fake := writeFakeRg(t, "printf 'one\\r\\n\\r\\ntwo\\n\\n'")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	require.Nil(t, err)

	assert.False(t, len(lines) != 2 || lines[0] != "one" || lines[1] != "two")

}

func TestResolveRipgrepPrefersOverride(t *testing.T) {
	fake := writeFakeRg(t, "exit 0")
	t.Setenv("RIPGREP_PATH", fake)
	got, err := resolveRipgrep()
	assert.False(t, err != nil || got != fake)

}

func TestResolveRipgrepBadOverride(t *testing.T) {
	t.Setenv("RIPGREP_PATH", filepath.Join(t.TempDir(), "missing"))
	_, err := resolveRipgrep()
	assert.False(t, err == nil || !strings.Contains(err.Error(), "RIPGREP_PATH"))

}

func TestResolveRipgrepFallsBackToPath(t *testing.T) {
	got, err := resolveRipgrep()
	require.Nil(t, err)

	_, statErr := os.Stat(got)
	assert.Nil(t, statErr)

}

func TestResolveRipgrepNotFoundMessage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := resolveRipgrep()
	require.NotNil(t, err)

	assert.Equal(t, ripgrepNotFoundMsg, err.Error())

	assert.Contains(t, err.Error(), "brew install ripgrep")

}

func TestRunnerSpawnFailure(t *testing.T) {
	_, err := testRunner(5*time.Second).run(filepath.Join(t.TempDir(), "gone"), nil, t.TempDir())
	require.NotNil(t, err)

	assert.Equal(t, ripgrepNotFoundMsg, err.Error())

}

func TestDefaultRgTimeout(t *testing.T) {
	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "")
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
	d, label := defaultRgTimeout()
	if isWSL() {
		// Host genuinely is WSL (via /proc/version): expect the 60s pair.
		assert.False(t, d != 60*time.Second || label != 60)

	} else if d != 20*time.Second || label != 20 {
		t.Errorf("got %v/%d, want 20s/20", d, label)
	}

	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "7")
	d, label2 := defaultRgTimeout()
	assert.Equal(t, 7*time.Second, d)

	assert.Equal(t, label, label2)

	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "not-a-number")
	d, _ = defaultRgTimeout()
	assert.Equal(t, time.Duration(label)*time.Second, d)

}

func TestIsWSLViaEnv(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "")
	got, want := isWSL(), runtime.GOOS == "linux"
	assert.Equal(t, want, got)

}
