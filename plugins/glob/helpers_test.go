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
)

func discardLogf(string, ...any) {}

// testTool builds a globTool rooted at root with production limits and a
// test-scoped persist dir. Tests tweak fields afterwards as needed.
func testTool(t *testing.T, root string) *globTool {
	t.Helper()
	return &globTool{
		root:             root,
		maxResults:       globMaxResults,
		persistThreshold: globPersistThreshold,
		timeout:          20 * time.Second,
		timeoutLabel:     20,
		maxOutput:        rgOutputCapBytes,
		tempDir:          t.TempDir(),
		resolveRg:        resolveRipgrep,
		logf:             discardLogf,
	}
}

// mkFiles creates the named files (slash-separated, relative to root)
// with strictly increasing mtimes in the given order, so the expected
// --sort=modified output order is exactly the argument order.
func mkFiles(t *testing.T, root string, names ...string) {
	t.Helper()
	base := time.Now().Add(-2 * time.Hour)
	for i, n := range names {
		p := filepath.Join(root, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
}

// runGlob invokes the tool through its public Call entry point and fails
// the test on JSON-RPC-level errors.
func runGlob(t *testing.T, g *globTool, pattern string, path ...string) (string, bool) {
	t.Helper()
	args := map[string]any{"pattern": pattern}
	if len(path) > 0 {
		args["path"] = path[0]
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, rpcErr := g.Call(raw)
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %+v", rpcErr)
	}
	return res.Text, res.IsError
}

func wantText(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("result text mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// writeFakeRg writes an executable shell script standing in for ripgrep
// and returns its path.
func writeFakeRg(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-rg")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	srv := newServer(inR, outW, discardLogf, "glob", tools, gateEnvVar)
	done := make(chan error, 1)
	go func() {
		done <- srv.run()
		outW.Close()
	}()
	c := &pipeClient{t: t, w: inW, r: bufio.NewReader(outR), done: done}
	t.Cleanup(func() {
		inW.Close()
		if err := <-done; err != nil {
			t.Errorf("server run: %v", err)
		}
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
	if !ok {
		t.Fatalf("expected error in response, got %v", resp)
	}
	code, ok := errObj["code"].(float64)
	if !ok {
		t.Fatalf("error without numeric code: %v", errObj)
	}
	return int(code)
}
