package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogf(string, ...any) {}

// testTool builds a grepTool rooted at root with production limits and a
// test-scoped persist dir. Tests tweak fields afterwards as needed.
func testTool(t *testing.T, root string) *grepTool {
	t.Helper()
	return &grepTool{
		root:             root,
		persistThreshold: grepPersistThreshold,
		timeout:          20 * time.Second,
		timeoutLabel:     20,
		maxOutput:        rgOutputCapBytes,
		tempDir:          t.TempDir(),
		resolveRg:        resolveRipgrep,
		logf:             discardLogf,
	}
}

// tf is one fixture file: a slash-relative name and its content.
type tf struct {
	name    string
	content string
}

// mkTree creates the fixture files with strictly increasing mtimes in
// argument order (index 0 oldest). Grep's filenames/filenames_with_matches
// modes sort newest FIRST, so the expected file order is the REVERSE of
// the argument order.
func mkTree(t *testing.T, root string, files ...tf) {
	t.Helper()
	base := time.Now().Add(-2 * time.Hour)
	for i, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(f.content), 0o644))
		mt := base.Add(time.Duration(i) * time.Second)
		require.NoError(t, os.Chtimes(p, mt, mt))
	}
}

// runGrep invokes the tool through its public Call entry point with the
// given arguments and fails the test on JSON-RPC-level errors.
func runGrep(t *testing.T, g *grepTool, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	res, rpcErr := g.Call(raw)
	require.Nil(t, rpcErr)
	return res.Text, res.IsError
}

// grepOK runs runGrep and asserts the result is not an error.
func grepOK(t *testing.T, g *grepTool, args map[string]any) string {
	t.Helper()
	text, isErr := runGrep(t, g, args)
	require.False(t, isErr, "unexpected tool error: %s", text)
	return text
}

func wantText(t *testing.T, got, want string) {
	t.Helper()
	assert.Equal(t, want, got)
}

func containsLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l == line {
			return true
		}
	}
	return false
}

// writeFakeRg writes an executable shell script standing in for ripgrep
// and returns its path.
func writeFakeRg(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-rg")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755))
	return p
}

// fixedRg returns a resolver that always yields path.
func fixedRg(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

// ---- in-process MCP client over pipes ----

type pipeClient struct {
	t    *testing.T
	w    io.WriteCloser
	r    *bufio.Reader
	done chan error
}

// startServer runs a server over in-memory pipes. Every request a test
// sends must have its response read, or Cleanup will block.
func startServer(t *testing.T, tools ...mcpTool) *pipeClient {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := newServer(inR, outW, discardLogf, "grep", tools, gateEnvVar)
	done := make(chan error, 1)
	go func() {
		done <- srv.run()
		outW.Close()
	}()
	c := &pipeClient{t: t, w: inW, r: bufio.NewReader(outR), done: done}
	t.Cleanup(func() {
		inW.Close()
		assert.NoError(t, <-done)
	})
	return c
}

func (c *pipeClient) send(raw string) {
	c.t.Helper()
	if _, err := io.WriteString(c.w, raw+"\n"); err != nil {
		c.t.Fatalf("send: %v", err)
	}
}

// recvRaw returns the next response line without the trailing newline.
func (c *pipeClient) recvRaw() string {
	c.t.Helper()
	line, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("recv: %v", err)
	}
	return strings.TrimSuffix(line, "\n")
}

func (c *pipeClient) recv() map[string]any {
	c.t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(c.recvRaw()), &m); err != nil {
		c.t.Fatalf("recv unmarshal: %v", err)
	}
	return m
}

// roundTrip sends a raw request line and decodes the one response.
func (c *pipeClient) roundTrip(raw string) map[string]any {
	c.t.Helper()
	c.send(raw)
	return c.recv()
}

func initializeReq(id int, clientName, clientVersion string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":%q,"version":%q}}}`,
		id, clientName, clientVersion)
}

// handshake performs initialize + notifications/initialized and returns
// the initialize result.
func (c *pipeClient) handshake(clientName, clientVersion string) map[string]any {
	c.t.Helper()
	resp := c.roundTrip(initializeReq(1, clientName, clientVersion))
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	return resp
}

// listTools returns the tools array from tools/list.
func (c *pipeClient) listTools(id int) []any {
	c.t.Helper()
	resp := c.roundTrip(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list"}`, id))
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

func errorCode(t *testing.T, resp map[string]any) int {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok)
	code, ok := errObj["code"].(float64)
	require.True(t, ok)
	return int(code)
}
