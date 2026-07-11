package main

import (
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
	fake := writeFakeRg(t, "sleep 5")
	r := testRunner(200 * time.Millisecond)
	start := time.Now()
	lines, err := r.run(fake, []string{"--files"}, t.TempDir())
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("timeout did not kill promptly (%v)", elapsed)
	}
	if lines != nil {
		t.Errorf("lines = %v, want none", lines)
	}
	if err == nil {
		t.Fatal("want timeout error")
	}
	want := "Ripgrep search timed out after 20 seconds. The search may have matched files but did not complete in time. Try searching a more specific path or pattern."
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestRunnerTimeoutLabelIsWSLConstantNotEffectiveTimeout(t *testing.T) {
	// Faithful quirk: the message reports the 20/60 default even when the
	// effective timeout differs.
	fake := writeFakeRg(t, "sleep 5")
	r := &rgRunner{timeout: 100 * time.Millisecond, timeoutLabel: 60, maxOutput: rgOutputCapBytes}
	_, err := r.run(fake, nil, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "timed out after 60 seconds") {
		t.Errorf("error = %v, want the 60-second label", err)
	}
}

func TestRunnerTimeoutWithPartialOutputDropsLastLine(t *testing.T) {
	fake := writeFakeRg(t, "printf 'a.txt\\nb.txt\\n'; sleep 5")
	r := testRunner(300 * time.Millisecond)
	lines, err := r.run(fake, []string{"--files"}, t.TempDir())
	if err != nil {
		t.Fatalf("partial output must resolve, got error: %v", err)
	}
	if len(lines) != 1 || lines[0] != "a.txt" {
		t.Errorf("lines = %v, want [a.txt] (last possibly-truncated line dropped)", lines)
	}
}

func TestRunnerOutputCapKillsAndResolvesPartial(t *testing.T) {
	fake := writeFakeRg(t, "while :; do echo 0123456789abcdef; done")
	r := &rgRunner{timeout: 30 * time.Second, timeoutLabel: 20, maxOutput: 4096}
	start := time.Now()
	lines, err := r.run(fake, nil, t.TempDir())
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("cap did not kill promptly (%v)", elapsed)
	}
	if err != nil {
		t.Fatalf("cap overflow must resolve partial results, got: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("want some retained lines before the cap")
	}
	for _, l := range lines {
		if l != "0123456789abcdef" {
			t.Errorf("retained a corrupt line %q", l)
		}
	}
}

func TestRunnerExitOneMeansNoMatches(t *testing.T) {
	fake := writeFakeRg(t, "exit 1")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	if err != nil || len(lines) != 0 {
		t.Errorf("lines=%v err=%v, want empty and nil", lines, err)
	}
}

func TestRunnerExitTwoResolvesStdout(t *testing.T) {
	fake := writeFakeRg(t, "echo half-result; echo 'rg: some error' >&2; exit 2")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	if err != nil {
		t.Fatalf("exit 2 must not error: %v", err)
	}
	if len(lines) != 1 || lines[0] != "half-result" {
		t.Errorf("lines = %v, want [half-result]", lines)
	}
}

func TestRunnerEAGAINRetriesSingleThreaded(t *testing.T) {
	// First invocation fails with the EAGAIN signature; the retry must
	// prepend -j 1, which the fake detects to succeed.
	fake := writeFakeRg(t, `if [ "$1" = "-j" ] && [ "$2" = "1" ]; then echo retried.txt; else echo 'rg: Resource temporarily unavailable (os error 11)' >&2; exit 2; fi`)
	lines, err := testRunner(5*time.Second).run(fake, []string{"--files"}, t.TempDir())
	if err != nil {
		t.Fatalf("retry path errored: %v", err)
	}
	if len(lines) != 1 || lines[0] != "retried.txt" {
		t.Errorf("lines = %v, want [retried.txt]", lines)
	}
}

func TestRunnerEAGAINRetriesOnlyOnce(t *testing.T) {
	fake := writeFakeRg(t, "echo 'rg: os error 11' >&2; exit 2")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	if err != nil || len(lines) != 0 {
		t.Errorf("lines=%v err=%v, want empty resolve after the single retry", lines, err)
	}
}

func TestRunnerCRLFAndBlankLineParsing(t *testing.T) {
	fake := writeFakeRg(t, "printf 'one\\r\\n\\r\\ntwo\\n\\n'")
	lines, err := testRunner(5*time.Second).run(fake, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "one" || lines[1] != "two" {
		t.Errorf("lines = %v, want [one two]", lines)
	}
}

func TestResolveRipgrepPrefersOverride(t *testing.T) {
	fake := writeFakeRg(t, "exit 0")
	t.Setenv("RIPGREP_PATH", fake)
	got, err := resolveRipgrep()
	if err != nil || got != fake {
		t.Errorf("resolveRipgrep = %q, %v; want %q", got, err, fake)
	}
}

func TestResolveRipgrepBadOverride(t *testing.T) {
	t.Setenv("RIPGREP_PATH", filepath.Join(t.TempDir(), "missing"))
	_, err := resolveRipgrep()
	if err == nil || !strings.Contains(err.Error(), "RIPGREP_PATH") {
		t.Errorf("err = %v, want a RIPGREP_PATH-specific failure", err)
	}
}

func TestResolveRipgrepFallsBackToPath(t *testing.T) {
	got, err := resolveRipgrep()
	if err != nil {
		t.Fatalf("rg is on PATH (bootstrapped), yet resolve failed: %v", err)
	}
	if _, statErr := os.Stat(got); statErr != nil {
		t.Errorf("resolved %q does not exist: %v", got, statErr)
	}
}

func TestResolveRipgrepNotFoundMessage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := resolveRipgrep()
	if err == nil {
		t.Fatal("want not-found error")
	}
	if err.Error() != ripgrepNotFoundMsg {
		t.Errorf("err = %q, want the canonical not-found message", err.Error())
	}
	if !strings.Contains(err.Error(), "brew install ripgrep") {
		t.Errorf("message lost its install hints: %q", err.Error())
	}
}

func TestRunnerSpawnFailure(t *testing.T) {
	_, err := testRunner(5*time.Second).run(filepath.Join(t.TempDir(), "gone"), nil, t.TempDir())
	if err == nil {
		t.Fatal("want spawn error")
	}
	if err.Error() != ripgrepNotFoundMsg {
		t.Errorf("err = %q, want the canonical not-found message", err.Error())
	}
}

func TestDefaultRgTimeout(t *testing.T) {
	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "")
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
	d, label := defaultRgTimeout()
	if isWSL() {
		// Host genuinely is WSL (via /proc/version): expect the 60s pair.
		if d != 60*time.Second || label != 60 {
			t.Errorf("got %v/%d, want 60s/60 on WSL", d, label)
		}
	} else if d != 20*time.Second || label != 20 {
		t.Errorf("got %v/%d, want 20s/20", d, label)
	}

	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "7")
	d, label2 := defaultRgTimeout()
	if d != 7*time.Second {
		t.Errorf("env override: got %v, want 7s", d)
	}
	if label2 != label {
		t.Errorf("label must stay the WSL constant, got %d", label2)
	}

	t.Setenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS", "not-a-number")
	if d, _ := defaultRgTimeout(); d != time.Duration(label)*time.Second {
		t.Errorf("garbage env override must be ignored, got %v", d)
	}
}

func TestIsWSLViaEnv(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	t.Setenv("WSL_INTEROP", "")
	if got, want := isWSL(), runtime.GOOS == "linux"; got != want {
		t.Errorf("isWSL with WSL_DISTRO_NAME set = %v, want %v on %s", got, want, runtime.GOOS)
	}
}
