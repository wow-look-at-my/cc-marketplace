package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rgmcp"
	"rgmcp/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(testkit.RunWithRipgrep(m))
}

var (
	discardLogf     = testkit.DiscardLogf
	writeFakeRg     = testkit.WriteFakeRg
	persistedPathRe = testkit.PersistedPathRe
	errorCode       = testkit.ErrorCode
)

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
		maxOutput:        rgmcp.RgOutputCapBytes,
		tempDir:          t.TempDir(),
		resolveRg:        rgmcp.ResolveRipgrep,
		logf:             discardLogf,
	}
}

// mkFiles creates the named files (slash-separated, relative to root)
// with strictly increasing mtimes in the given order, so the expected
// ascending-mtime output order is exactly the argument order.
func mkFiles(t *testing.T, root string, names ...string) {
	t.Helper()
	base := time.Now().Add(-2 * time.Hour)
	for i, n := range names {
		p := filepath.Join(root, filepath.FromSlash(n))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))

		require.NoError(t, os.WriteFile(p, []byte("x\n"), 0o644))

		mt := base.Add(time.Duration(i) * time.Second)
		require.NoError(t, os.Chtimes(p, mt, mt))

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
	require.Nil(t, err)

	res, rpcErr := g.Call(raw)
	require.Nil(t, rpcErr)

	return res.Text, res.IsError
}

func wantText(t *testing.T, got, want string) {
	t.Helper()
	assert.Equal(t, want, got)

}

// fixedRg returns a resolver that always yields path.
func fixedRg(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

// startServer runs the plugin's server, wired as main wires it, over
// in-memory pipes.
func startServer(t *testing.T, tools ...rgmcp.Tool) *testkit.Client {
	t.Helper()
	return testkit.Start(t, func(in io.Reader, out io.Writer) error {
		return rgmcp.NewServer(in, out, discardLogf, "glob", tools, gateEnvVar).Run()
	})
}
