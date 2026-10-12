package testkit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DiscardLogf is a logf that drops every line.
func DiscardLogf(string, ...any) {}

// PersistedPathRe captures the file path from a persisted-output block.
var PersistedPathRe = regexp.MustCompile(`Full output saved to: (.+)\n`)

// WriteFakeRg writes an executable shell script standing in for ripgrep and
// returns its path, a single time the kernel will start it.
func WriteFakeRg(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-rg")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755))

	for {
		// Start, never Run: the answer is whether the kernel will EXEC this file.
		cmd := exec.Command(p, "--fake-rg-startup-probe")
		err := cmd.Start()
		if err == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return p
		}
		if !errors.Is(err, syscall.ETXTBSY) {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Client is an in-process MCP client over pipes.
type Client struct {
	t      *testing.T
	w      io.WriteCloser
	r      *bufio.Reader
	done   chan error
	once   sync.Once
	runErr error
}

// Start runs serve over in-memory pipes. Every request a test sends must
// have its response read, or Cleanup will block.
func Start(t *testing.T, serve func(in io.Reader, out io.Writer) error) *Client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serve(inR, outW)
		outW.Close()
	}()
	c := &Client{t: t, w: inW, r: bufio.NewReader(outR), done: done}
	t.Cleanup(func() {
		assert.NoError(t, c.Shutdown())
	})
	return c
}

// Shutdown closes the server's stdin and returns what the server's run
// returned. A second call returns the same value.
func (c *Client) Shutdown() error {
	c.once.Do(func() {
		c.w.Close()
		c.runErr = <-c.done
	})
	return c.runErr
}

func (c *Client) Send(raw string) {
	c.t.Helper()
	if _, err := io.WriteString(c.w, raw+"\n"); err != nil {
		c.t.Fatalf("send: %v", err)
	}
}

// RecvRaw returns the next response line without the trailing newline.
func (c *Client) RecvRaw() string {
	c.t.Helper()
	line, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("recv: %v", err)
	}
	return strings.TrimSuffix(line, "\n")
}

func (c *Client) Recv() map[string]any {
	c.t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(c.RecvRaw()), &m); err != nil {
		c.t.Fatalf("recv unmarshal: %v", err)
	}
	return m
}

// RoundTrip sends a raw request line and decodes the response.
func (c *Client) RoundTrip(raw string) map[string]any {
	c.t.Helper()
	c.Send(raw)
	return c.Recv()
}

func InitializeReq(id int, clientName, clientVersion string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":%q,"version":%q}}}`,
		id, clientName, clientVersion)
}

// Handshake performs initialize + notifications/initialized and returns
// the initialize result.
func (c *Client) Handshake(clientName, clientVersion string) map[string]any {
	c.t.Helper()
	resp := c.RoundTrip(InitializeReq(1, clientName, clientVersion))
	c.Send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	return resp
}

// ListTools returns the tools array from tools/list.
func (c *Client) ListTools(id int) []any {
	c.t.Helper()
	resp := c.RoundTrip(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list"}`, id))
	result, ok := resp["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("tools/list: no result in %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		c.t.Fatalf("tools/list: no tools array in %v", result)
	}
	return tools
}

// ErrorCode returns the JSON-RPC error code of resp.
func ErrorCode(t *testing.T, resp map[string]any) int {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok)
	code, ok := errObj["code"].(float64)
	require.True(t, ok)
	return int(code)
}
