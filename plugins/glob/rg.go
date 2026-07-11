// rg.go resolves and runs the ripgrep binary with the exact spawn
// semantics of claude-code 2.1.116's shared runner (LQ,
// 2.1.116:cli.js:148544-148675): 20s timeout (60s on WSL, env
// CLAUDE_CODE_GLOB_TIMEOUT_SECONDS override — the GLOB name is the
// builtin's; it governed Grep too), 20MB output caps, exit 1 = no
// matches, one -j 1 retry on EAGAIN, and timeout-with-partial-output
// resolving the parsed lines minus the last (possibly truncated) one.
//
// ONE deliberate divergence from the builtin, shared by both sibling
// plugins: exit code 2 with NO stdout surfaces rg's stderr (capped at
// rgStderrErrLimit) as an error instead of resolving empty. The builtin
// silently reported "No matches found" / "No files found" for an
// invalid regex/glob/type; the plugins make those failures visible.
// Exit 2 WITH stdout (e.g. matches found but some tree entries
// unreadable) still resolves the partial results like the builtin did.
//
// This file is tool-agnostic and copied verbatim between the grep and
// glob sibling plugins.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// rgOutputCapBytes mirrors DeH = 20000000 (2.1.116:cli.js:148704).
const rgOutputCapBytes = 20_000_000

// ripgrepNotFoundMsg is modeled on claude-code >= 2.1.175's ENOENT message
// (2.1.207:cli.js:279130), with the embedded-binary escape replaced by the
// override this plugin actually supports.
const ripgrepNotFoundMsg = "ripgrep not found on PATH. Install it (brew install ripgrep / apt install ripgrep / winget install BurntSushi.ripgrep.MSVC) or set RIPGREP_PATH to a ripgrep binary."

// resolveRipgrep returns the ripgrep binary to run: the RIPGREP_PATH
// override when set, else rg from PATH.
func resolveRipgrep() (string, error) {
	if p := os.Getenv("RIPGREP_PATH"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("RIPGREP_PATH is set to %q but it is not usable: %v", p, err)
		}
		return p, nil
	}
	if p, err := exec.LookPath("rg"); err == nil {
		return p, nil
	}
	return "", errors.New(ripgrepNotFoundMsg)
}

// defaultRgTimeout returns the effective spawn timeout and the seconds
// value baked into the timeout error message. Faithful quirk: the message
// always reports the 20/60 default even when the env var overrides the
// actual timeout (the original interpolates `wsl ? 60 : 20`, not the
// effective value).
func defaultRgTimeout() (time.Duration, int) {
	label := 20
	if isWSL() {
		label = 60
	}
	d := time.Duration(label) * time.Second
	if v := os.Getenv("CLAUDE_CODE_GLOB_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			d = time.Duration(n) * time.Second
		}
	}
	return d, label
}

// isWSL is a best-effort stand-in for claude-code's WSL detection.
func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

func rgTimeoutError(labelSeconds int) error {
	return fmt.Errorf("Ripgrep search timed out after %d seconds. The search may have matched files but did not complete in time. Try searching a more specific path or pattern.", labelSeconds)
}

type rgRunner struct {
	timeout      time.Duration
	timeoutLabel int // seconds shown in the timeout error message
	maxOutput    int // per-stream byte cap
}

// run executes rgPath with args in dir and applies the shared-runner
// result semantics. The returned lines are rg's stdout lines (trimmed,
// \r-stripped, empties dropped); err carries user-facing failure text.
func (r *rgRunner) run(rgPath string, args []string, dir string) ([]string, error) {
	lines, retryEAGAIN, err := r.runOnce(rgPath, args, dir)
	if retryEAGAIN {
		// One retry with single-threaded rg, mirroring the EAGAIN
		// workaround (2.1.116:cli.js:148540-148543, 148646-148654).
		lines, _, err = r.runOnce(rgPath, append([]string{"-j", "1"}, args...), dir)
	}
	return lines, err
}

func (r *rgRunner) runOnce(rgPath string, args []string, dir string) (lines []string, retryEAGAIN bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, rgPath, args...)
	cmd.Dir = dir
	stdout := &cappedBuffer{max: r.maxOutput, onOver: cancel}
	stderr := &cappedBuffer{max: r.maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// After a kill, don't let stray descendants holding the pipes delay
	// the (partial) result: give I/O one second to drain, then move on.
	cmd.WaitDelay = time.Second

	runErr := cmd.Run()
	lines = parseRgLines(stdout.String())
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)

	if stdout.exceeded() || timedOut {
		// Killed by us: resolve partial results after dropping the last
		// (possibly truncated) line; with nothing parsed, a timeout is an
		// error and an output-cap kill resolves empty.
		if len(lines) > 0 {
			return lines[:len(lines)-1], false, nil
		}
		if timedOut {
			return nil, false, rgTimeoutError(r.timeoutLabel)
		}
		return nil, false, nil
	}

	if runErr == nil {
		return lines, false, nil // exit 0
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if exitErr.ExitCode() == -1 {
			// Killed by an external signal: same partial semantics.
			if len(lines) > 0 {
				return lines[:len(lines)-1], false, nil
			}
			return nil, false, rgTimeoutError(r.timeoutLabel)
		}
		if exitErr.ExitCode() == 1 {
			return nil, false, nil // no matches
		}
		// Exit 2: EAGAIN earns one -j 1 retry; with stdout, resolve the
		// partial results like the builtin; with NOTHING on stdout,
		// surface rg's stderr (invalid regex/glob/type) as the failure
		// instead of the builtin's silent no-results text (deliberate
		// deviation, see the file comment).
		if stderrIndicatesEAGAIN(stderr.String()) {
			return nil, true, nil
		}
		if len(lines) == 0 {
			if msg := truncateErrText(strings.TrimSpace(stderr.String())); msg != "" {
				return nil, false, errors.New(msg)
			}
		}
		return lines, false, nil
	}
	// Spawn-level failure (binary vanished, permission denied, ...).
	if errors.Is(runErr, exec.ErrNotFound) || os.IsNotExist(runErr) {
		return nil, false, errors.New(ripgrepNotFoundMsg)
	}
	return nil, false, runErr
}

func stderrIndicatesEAGAIN(s string) bool {
	return strings.Contains(s, "os error 11") || strings.Contains(s, "Resource temporarily unavailable")
}

// rgStderrErrLimit caps how much of rg's stderr is surfaced as an error
// message, so a pathological run (e.g. megabytes of per-file warnings
// ending in a real error) cannot blow up the MCP result. Errors bypass
// the persistOversize path, hence the cap here.
const rgStderrErrLimit = 4000

const rgStderrTruncNote = "\n[ripgrep error output truncated]"

// truncateErrText caps s at rgStderrErrLimit bytes (cut on a rune
// boundary) and appends a truncation note when anything was dropped.
func truncateErrText(s string) string {
	if len(s) <= rgStderrErrLimit {
		return s
	}
	cut := rgStderrErrLimit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + rgStderrTruncNote
}

// parseRgLines mirrors the shared runner's line handling: whole output
// trimmed, split on \n, \r stripped per line, empty lines dropped
// (2.1.116:cli.js:148617-148630).
func parseRgLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSuffix(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// cappedBuffer retains at most max bytes and reports (and optionally
// reacts to) writes past the cap. It never returns a write error so the
// child is killed via onOver instead of a broken pipe.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	max       int
	attempted int
	over      bool
	onOver    func()
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.attempted += len(p)
	if remain := b.max - b.buf.Len(); remain > 0 {
		if len(p) > remain {
			b.buf.Write(p[:remain])
		} else {
			b.buf.Write(p)
		}
	}
	fireOver := b.attempted > b.max && !b.over
	if fireOver {
		b.over = true
	}
	cb := b.onOver
	b.mu.Unlock()
	if fireOver && cb != nil {
		cb()
	}
	return len(p), nil
}

func (b *cappedBuffer) exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.over
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
