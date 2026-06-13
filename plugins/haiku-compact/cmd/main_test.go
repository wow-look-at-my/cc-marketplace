package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("HC_TEST_X", "val")
	require.Equal(t, "val", envOr("HC_TEST_X", "def"))
	require.Equal(t, "def", envOr("HC_TEST_UNSET_ZZZ", "def"))
}

func TestEnvInt(t *testing.T) {
	t.Setenv("HC_TEST_N", "123")
	require.Equal(t, int64(123), envInt("HC_TEST_N", 7))
	t.Setenv("HC_TEST_BAD", "notnum")
	require.Equal(t, int64(7), envInt("HC_TEST_BAD", 7))
	require.Equal(t, int64(7), envInt("HC_TEST_UNSET_ZZZ", 7))
}

func TestFlagValue(t *testing.T) {
	args := []string{"serve", "--port", "9999", "--log", "/tmp/x"}
	require.Equal(t, "9999", flagValue(args, "--port", "8788"))
	require.Equal(t, "/tmp/x", flagValue(args, "--log", "def"))
	require.Equal(t, "def", flagValue(args, "--missing", "def"))
	require.Equal(t, "def", flagValue([]string{"--port"}, "--port", "def")) // trailing flag, no value
}

func TestParseClaudeArgs(t *testing.T) {
	require.Equal(t, []string{"--continue", "-p"}, parseClaudeArgs([]string{"--", "--continue", "-p"}))
	require.Equal(t, []string{"--continue"}, parseClaudeArgs([]string{"--continue"}))
	require.Empty(t, parseClaudeArgs([]string{"--"}))
}

func TestIdleCheckInterval(t *testing.T) {
	require.Equal(t, time.Minute, idleCheckInterval(10*time.Hour))
	require.Equal(t, 250*time.Millisecond, idleCheckInterval(time.Second))
	require.Equal(t, time.Second, idleCheckInterval(0))
}

func TestBuildConfig_Defaults(t *testing.T) {
	t.Setenv("HAIKU_COMPACT_UPSTREAM", "")
	t.Setenv("HAIKU_COMPACT_MODEL", "")
	t.Setenv("HAIKU_COMPACT_MAX_INPUT_BYTES", "")
	cfg, err := buildConfig(log.New(io.Discard, "", 0))
	require.NoError(t, err)
	require.Equal(t, defaultUpstream, cfg.upstream.String())
	require.Equal(t, defaultModel, cfg.model)
	require.Equal(t, int64(defaultMaxInput), cfg.maxInputBytes)
}

func TestBuildConfig_Overrides(t *testing.T) {
	t.Setenv("HAIKU_COMPACT_UPSTREAM", "https://gw.example.com/api")
	t.Setenv("HAIKU_COMPACT_MODEL", "claude-haiku-4-5")
	t.Setenv("HAIKU_COMPACT_MAX_INPUT_BYTES", "1234")
	cfg, err := buildConfig(log.New(io.Discard, "", 0))
	require.NoError(t, err)
	require.Equal(t, "https://gw.example.com/api", cfg.upstream.String())
	require.Equal(t, "claude-haiku-4-5", cfg.model)
	require.Equal(t, int64(1234), cfg.maxInputBytes)
}

func TestBuildConfig_BadURL(t *testing.T) {
	t.Setenv("HAIKU_COMPACT_UPSTREAM", "http://%zz")
	_, err := buildConfig(log.New(io.Discard, "", 0))
	require.Error(t, err)
}

func TestNewLogger(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	lg := newLogger(p)
	require.NotNil(t, lg)
	lg.Print("hello world")
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Contains(t, string(data), "hello world")
	// An unopenable path still yields a usable (stderr) logger.
	require.NotNil(t, newLogger(filepath.Join(p, "nested", "log.txt")))
}

func TestDefaultPaths(t *testing.T) {
	require.True(t, strings.HasSuffix(defaultLogPath(), "haiku-compact.log"))
	require.True(t, strings.HasSuffix(pidPath(), "haiku-compact.pid"))
}

func TestAlreadyListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.True(t, alreadyListening(addr))
	require.NoError(t, ln.Close())
	require.False(t, alreadyListening(addr))
}

func TestSingleJoiningSlash(t *testing.T) {
	require.Equal(t, "/a/b", singleJoiningSlash("/a/", "/b"))
	require.Equal(t, "/a/b", singleJoiningSlash("/a", "b"))
	require.Equal(t, "/a/b", singleJoiningSlash("/a", "/b"))
	require.Equal(t, "/a/b", singleJoiningSlash("/a/", "b"))
	require.Equal(t, "/v1/messages", singleJoiningSlash("", "/v1/messages"))
}

func TestStartProxy_ServesAndSwaps(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	server, addr, _, err := startProxy(cfg, "127.0.0.1:0", 0)
	require.NoError(t, err)
	defer server.Close()

	resp, err := http.Post("http://"+addr+"/v1/messages", "application/json",
		strings.NewReader(string(compactionBody("claude-opus-4-8"))))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, cfg.model, resp.Header.Get("X-Got-Model"))
}

func TestStartProxy_IdleShutdown(t *testing.T) {
	cfg := testConfig(t, "http://127.0.0.1:1")
	server, _, done, err := startProxy(cfg, "127.0.0.1:0", 200*time.Millisecond)
	require.NoError(t, err)
	defer server.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("idle shutdown did not occur")
	}
}

func TestServeSetup(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HAIKU_COMPACT_IDLE_MINUTES", "0")
	server, addr, _, err := serveSetup([]string{"--port", "0", "--log", filepath.Join(t.TempDir(), "l.log")})
	require.NoError(t, err)
	defer server.Close()
	require.NotEmpty(t, addr)
	// pid file written
	data, err := os.ReadFile(pidPath())
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(os.Getpid()), strings.TrimSpace(string(data)))
}

func TestLaunchSetup(t *testing.T) {
	t.Setenv("HAIKU_COMPACT_UPSTREAM", "https://api.anthropic.com")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	claudeArgs, server, err := launchSetup([]string{"--", "--continue", "-p", "hi"})
	require.NoError(t, err)
	defer server.Close()
	require.Equal(t, []string{"--continue", "-p", "hi"}, claudeArgs)
	require.True(t, strings.HasPrefix(os.Getenv("ANTHROPIC_BASE_URL"), "http://127.0.0.1:"))
}

func TestCmdDaemon_AlreadyRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	// A listener already owns the port, so daemon must return without spawning.
	cmdDaemon([]string{"--port", port})
}

func TestStopDaemon_NoFile(t *testing.T) {
	stopDaemon(filepath.Join(t.TempDir(), "absent.pid"))
}

func TestStopDaemon_BadPid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.pid")
	require.NoError(t, os.WriteFile(p, []byte("notanumber"), 0o600))
	stopDaemon(p)
}

func TestStopDaemon_KillsProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	p := filepath.Join(t.TempDir(), "run.pid")
	require.NoError(t, os.WriteFile(p, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600))
	stopDaemon(p)
	_, statErr := os.Stat(p)
	require.True(t, os.IsNotExist(statErr))

	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process not terminated by stopDaemon")
	}
}

func TestWritePidFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	writePidFile()
	data, err := os.ReadFile(pidPath())
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(os.Getpid()), strings.TrimSpace(string(data)))
}

func TestMaybeSwap_Ignored(t *testing.T) {
	cfg := testConfig(t, "http://127.0.0.1:1")
	// GET is never swapped, and a nil body must not panic.
	r := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	r.Body = nil
	maybeSwap(r, cfg) // no panic, no-op
}

func TestUsage(t *testing.T) {
	usage() // smoke: prints help without panicking
}
