package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// testTool builds a grepTool rooted at root with production limits and a
// test-scoped persist dir. Tests tweak fields afterwards as needed.
func testTool(t *testing.T, root string) *grepTool {
	t.Helper()
	return &grepTool{
		root:             root,
		persistThreshold: grepPersistThreshold,
		timeout:          20 * time.Second,
		timeoutLabel:     20,
		maxOutput:        rgmcp.RgOutputCapBytes,
		tempDir:          t.TempDir(),
		resolveRg:        rgmcp.ResolveRipgrep,
		logf:             discardLogf,
	}
}

// tf is a single fixture file: a slash-relative name and its content.
type tf struct {
	name    string
	content string
}

// Grep's filenames/filenames_with_matches modes sort newest so the
// expected file order is the REVERSE of the argument order.
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

// fixedRg returns a resolver that always yields path.
func fixedRg(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

// startServer runs the plugin's server, wired as main wires it, over
// in-memory pipes.
func startServer(t *testing.T, tools ...rgmcp.Tool) *testkit.Client {
	t.Helper()
	return testkit.Start(t, func(in io.Reader, out io.Writer) error {
		return rgmcp.NewServer(in, out, discardLogf, "grep", tools, gateEnvVar).Run()
	})
}
