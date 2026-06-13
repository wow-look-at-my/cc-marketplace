// Command haiku-compact is a localhost intercepting proxy for the Anthropic API.
// It forwards every request untouched EXCEPT Claude Code's context-compaction
// summarization call, whose `model` field it rewrites to a cheaper Haiku model.
// This makes compaction cheap without affecting the model used for normal turns
// -- something no Claude Code hook can do, because the compaction model is read
// from in-memory state, not re-read from settings mid-session.
//
// Subcommands:
//
//	serve    Run the proxy in the foreground.
//	daemon   Start the proxy detached if it is not already running (idempotent).
//	         This is what the plugin's SessionStart hook invokes.
//	launch   Start the proxy and exec `claude` with ANTHROPIC_BASE_URL pointed at
//	         it -- a zero-config alternative to editing settings.json.
//	stop     Stop a running daemon.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultPort        = "8788"
	defaultUpstream    = "https://api.anthropic.com"
	defaultModel       = "claude-haiku-4-5-20251001"
	defaultMaxInput    = 700000 // ~ Haiku 200k-token context guard; skip swap above this
	defaultIdleMinutes = 60     // daemon self-exits after this much inactivity; 0 disables

	envPort        = "HAIKU_COMPACT_PORT"
	envUpstream    = "HAIKU_COMPACT_UPSTREAM"
	envModel       = "HAIKU_COMPACT_MODEL"
	envMaxInput    = "HAIKU_COMPACT_MAX_INPUT_BYTES"
	envIdleMinutes = "HAIKU_COMPACT_IDLE_MINUTES"
	envLog         = "HAIKU_COMPACT_LOG"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "daemon":
		cmdDaemon(os.Args[2:])
	case "launch":
		cmdLaunch(os.Args[2:])
	case "stop":
		cmdStop()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "haiku-compact: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `haiku-compact -- swap the model to Haiku for Claude Code context compaction

Usage:
  haiku-compact serve     Run the proxy in the foreground
  haiku-compact daemon    Start the proxy detached if not already running
  haiku-compact launch -- claude [args...]
                          Start the proxy and run claude through it
  haiku-compact stop      Stop a running daemon

Configuration (env vars):
  HAIKU_COMPACT_PORT             listen port (default `+defaultPort+`)
  HAIKU_COMPACT_UPSTREAM         real API base URL (default `+defaultUpstream+`)
  HAIKU_COMPACT_MODEL            model to swap in (default `+defaultModel+`)
  HAIKU_COMPACT_MAX_INPUT_BYTES  skip swap above this body size (default `+strconv.Itoa(defaultMaxInput)+`; 0 disables)
  HAIKU_COMPACT_IDLE_MINUTES     daemon idle self-exit (default `+strconv.Itoa(defaultIdleMinutes)+`; 0 disables)
  HAIKU_COMPACT_LOG              log file path
`)
}

// envOr returns the value of key, or def if unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt returns the integer value of key, or def if unset/invalid.
func envInt(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func defaultLogPath() string { return filepath.Join(os.TempDir(), "haiku-compact.log") }
func pidPath() string        { return filepath.Join(os.TempDir(), "haiku-compact.pid") }

// newLogger opens the log file (appending) and returns a logger. Falls back to
// stderr if the file cannot be opened.
func newLogger(path string) *log.Logger {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return log.New(os.Stderr, "haiku-compact ", log.LstdFlags)
	}
	return log.New(f, "haiku-compact ", log.LstdFlags)
}

// buildConfig assembles the proxy configuration from the environment.
func buildConfig(logger *log.Logger) (*proxyConfig, error) {
	up, err := url.Parse(envOr(envUpstream, defaultUpstream))
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %w", err)
	}
	return &proxyConfig{
		upstream:      up,
		model:         envOr(envModel, defaultModel),
		maxInputBytes: envInt(envMaxInput, defaultMaxInput),
		logger:        logger,
	}, nil
}

// startProxy binds addr and serves the proxy on it, returning the server, the
// actual listen address (useful with an ephemeral :0 port), and a channel closed
// when the server stops. When idle > 0 the server self-closes after that much
// inactivity.
func startProxy(cfg *proxyConfig, addr string, idle time.Duration) (*http.Server, string, <-chan struct{}, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", nil, err
	}
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	base := newProxyHandler(cfg)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last.Store(time.Now().UnixNano())
		base.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		server.Serve(ln)
		close(done)
	}()
	if idle > 0 {
		go func() {
			t := time.NewTicker(idleCheckInterval(idle))
			defer t.Stop()
			for range t.C {
				if time.Since(time.Unix(0, last.Load())) > idle {
					cfg.logger.Printf("idle for %s; shutting down", idle)
					server.Close()
					return
				}
			}
		}()
	}
	return server, ln.Addr().String(), done, nil
}

// idleCheckInterval picks how often to test for inactivity: frequently enough to
// honor short idle windows in tests, but never more than once a minute.
func idleCheckInterval(idle time.Duration) time.Duration {
	iv := idle / 4
	if iv > time.Minute {
		iv = time.Minute
	}
	if iv <= 0 {
		iv = time.Second
	}
	return iv
}

// alreadyListening reports whether something already accepts connections at addr.
func alreadyListening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// parseClaudeArgs returns the claude command line: everything after a "--"
// separator if present, otherwise the args as given.
func parseClaudeArgs(args []string) []string {
	for i, a := range args {
		if a == "--" {
			return args[i+1:]
		}
	}
	return args
}

// serveSetup builds the proxy and starts it listening, writing the pid file. It
// returns the running server and a channel closed when it stops. The signal wait
// is left to the caller so this core is testable.
func serveSetup(args []string) (*http.Server, string, <-chan struct{}, error) {
	port := flagValue(args, "--port", envOr(envPort, defaultPort))
	logger := newLogger(flagValue(args, "--log", envOr(envLog, defaultLogPath())))
	cfg, err := buildConfig(logger)
	if err != nil {
		return nil, "", nil, err
	}
	idle := time.Duration(envInt(envIdleMinutes, defaultIdleMinutes)) * time.Minute
	server, addr, done, err := startProxy(cfg, net.JoinHostPort("127.0.0.1", port), idle)
	if err != nil {
		return nil, "", nil, fmt.Errorf("listen on 127.0.0.1:%s failed: %w", port, err)
	}
	logger.Printf("serving on http://%s -> %s (model %s)", addr, cfg.upstream, cfg.model)
	writePidFile()
	return server, addr, done, nil
}

func cmdServe(args []string) {
	server, _, done, err := serveSetup(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "haiku-compact: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(pidPath())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	waitForShutdown(server, done, stop)
}

// waitForShutdown blocks until either a termination signal arrives (closing the
// server) or the server stops on its own (e.g. idle shutdown).
func waitForShutdown(server *http.Server, done <-chan struct{}, sig <-chan os.Signal) {
	select {
	case <-sig:
		server.Close()
	case <-done:
	}
}

// configuredForProxy reports whether ANTHROPIC_BASE_URL points at our loopback
// port -- i.e. the user opted in via settings.json env. When it does not, the
// SessionStart daemon is a no-op, so merely installing the plugin (or using the
// `launch` wrapper, which uses its own ephemeral port) never spawns a stray
// background proxy.
func configuredForProxy(port string) bool {
	base := os.Getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		return false
	}
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return u.Port() == port
	}
	return false
}

func cmdDaemon(args []string) {
	port := flagValue(args, "--port", envOr(envPort, defaultPort))
	if !configuredForProxy(port) {
		return // ANTHROPIC_BASE_URL is not pointed at us; nothing to do
	}
	addr := net.JoinHostPort("127.0.0.1", port)
	// Already running? A successful dial means the port is owned; nothing to do.
	if alreadyListening(addr) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "haiku-compact: cannot locate executable: %v\n", err)
		return // fail-open: never break the session
	}
	logPath := envOr(envLog, defaultLogPath())
	if err := startDetached(exe, []string{"serve", "--port", port, "--log", logPath}, logPath); err != nil {
		fmt.Fprintf(os.Stderr, "haiku-compact: failed to start daemon: %v\n", err)
		return
	}
	// Give it a moment to bind so the first API call already routes through it.
	for i := 0; i < 30; i++ {
		if alreadyListening(addr) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startDetached launches exe as a background process in its own session, with
// output redirected to logPath. It is a package variable so tests can stub the
// spawn without re-executing the binary.
var startDetached = func(exe string, args []string, logPath string) error {
	logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	if logf != nil {
		cmd.Stdout, cmd.Stderr = logf, logf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach into its own session
	return cmd.Start()
}

// launchSetup starts an ephemeral proxy and points ANTHROPIC_BASE_URL at it,
// returning the claude command line to run and the proxy server. The actual exec
// of claude is left to the caller so this core is testable.
func launchSetup(args []string) ([]string, *http.Server, error) {
	claudeArgs := parseClaudeArgs(args)
	// The real upstream is whatever ANTHROPIC_BASE_URL pointed at before we
	// hijack it (or the configured/default endpoint).
	upstream := envOr(envUpstream, os.Getenv("ANTHROPIC_BASE_URL"))
	if upstream == "" {
		upstream = defaultUpstream
	}
	os.Setenv(envUpstream, upstream)

	logger := newLogger(envOr(envLog, defaultLogPath()))
	cfg, err := buildConfig(logger)
	if err != nil {
		return nil, nil, err
	}
	// Ephemeral port avoids clashing with an existing daemon.
	server, addr, _, err := startProxy(cfg, "127.0.0.1:0", 0)
	if err != nil {
		return nil, nil, err
	}
	_, port, _ := net.SplitHostPort(addr)
	os.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:"+port)
	logger.Printf("launch: proxy on %s -> %s (model %s)", addr, cfg.upstream, cfg.model)
	return claudeArgs, server, nil
}

func cmdLaunch(args []string) {
	claudeArgs, server, err := launchSetup(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "haiku-compact: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()
	c := exec.Command("claude", claudeArgs...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "haiku-compact: failed to run claude: %v\n", err)
		os.Exit(1)
	}
}

func cmdStop() { stopDaemon(pidPath()) }

// stopDaemon reads a pid from path, signals that process to terminate, and
// removes the file. Missing or malformed pid files are reported, not fatal.
func stopDaemon(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "haiku-compact: no daemon running")
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "haiku-compact: bad pid file: %v\n", err)
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		p.Signal(syscall.SIGTERM)
	}
	os.Remove(path)
}

func writePidFile() {
	os.WriteFile(pidPath(), []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// flagValue returns the value following name in args, or def if not present.
func flagValue(args []string, name, def string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return def
}
