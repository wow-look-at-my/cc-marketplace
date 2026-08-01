package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadEmbeddedTests(t *testing.T) []struct{ Command, Expected string } {
	t.Helper()
	repoRoot := getRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.xml"))
	require.NoError(t, err)

	var xr xmlRules
	require.NoError(t, xml.Unmarshal(data, &xr))

	type testCase = struct{ Command, Expected string }
	var cases []testCase
	for _, tt := range xr.Tests {
		cases = append(cases, testCase{tt.Command, tt.Expected})
	}
	var walk func([]xmlCommand)
	walk = func(cmds []xmlCommand) {
		for _, cmd := range cmds {
			for _, tt := range cmd.Tests {
				cases = append(cases, testCase{tt.Command, tt.Expected})
			}
			walk(cmd.Subcommands)
		}
	}
	// Tests live wherever their rule lives, in any of the three sections.
	walk(xr.Allow.Rules)
	walk(xr.Ask.Rules)
	walk(xr.Deny.Rules)
	require.NotEmpty(t, cases, "no embedded tests found in rules.xml")
	return cases
}

func TestEvaluateCommands(t *testing.T) {
	loadTestRules(t)
	for _, tt := range loadEmbeddedTests(t) {
		t.Run(tt.Command, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.Command)
			assert.Equal(t, tt.Expected, decision, "evaluateCommand(%q)", tt.Command)
		})
	}
}

func TestDuplicateEntriesMerged(t *testing.T) {
	saved := rules
	defer func() { rules = saved }()

	rules = Rules{
		Allow: []CommandNode{
			{
				Name:        "mycmd",
				Description: "first entry",
				Subcommands: []CommandNode{
					{Name: "sub1", AllowedFlags: "*"},
				},
			},
			{
				Name:        "mycmd",
				Description: "second entry",
				Subcommands: []CommandNode{
					{Name: "sub2", AllowedFlags: "*"},
				},
			},
		},
	}

	decision, _ := evaluateCommand("mycmd sub1")
	assert.Equal(t, "allow", decision, "mycmd sub1 should match first entry")

	decision, _ = evaluateCommand("mycmd sub2")
	assert.Equal(t, "allow", decision, "mycmd sub2 should match second entry")

	decision, _ = evaluateCommand("mycmd sub3")
	assert.Equal(t, "", decision, "mycmd sub3 should passthrough (no match)")
}

func TestDuplicateEntriesDenyWins(t *testing.T) {
	saved := rules
	defer func() { rules = saved }()

	rules = Rules{
		Allow: []CommandNode{
			{
				Name: "mycmd",
				Subcommands: []CommandNode{
					{Name: "ok", AllowedFlags: "*"},
				},
			},
			{
				Name: "mycmd",
				Subcommands: []CommandNode{
					{Name: "ok", DenyWithMessage: "blocked"},
				},
			},
		},
	}

	decision, msg := evaluateCommand("mycmd ok")
	assert.Equal(t, "deny", decision, "deny should win over allow for duplicate entries")
	assert.Equal(t, "blocked", msg)
}

func TestReadAllowed(t *testing.T) {
	input := `{"hook_event_name":"PermissionRequest","tool_name":"Read","tool_input":{"file_path":"/any/path/file.txt"}}`
	output := captureOutput(func() {
		old := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		go func() {
			w.Write([]byte(input))
			w.Close()
		}()
		main()
		os.Stdin = old
	})

	var resp PermissionResponse
	require.NoError(t, json.Unmarshal([]byte(output), &resp), "output was: %s", output)
	assert.Equal(t, "allow", resp.HookSpecificOutput.Decision.Behavior, "Read should be allowed")
}

func TestEndToEndGhRepoView(t *testing.T) {
	repoRoot := getRepoRoot(t)
	pluginDir := filepath.Join(repoRoot, "plugins/enhanced-auto-allow")

	buildDir := filepath.Join(pluginDir, "build")
	os.MkdirAll(buildDir, 0o755)
	binaryPath := filepath.Join(buildDir, "enhanced-auto-allow-test")
	defer os.Remove(binaryPath)

	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/")
	cmd.Dir = pluginDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", out)

	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"gh repo view", "gh repo view wow-look-at-my/go-toolchain", "allow"},
		{"gh repo view --json", "gh repo view wow-look-at-my/go-toolchain --json name,description", "allow"},
		{"gh release list", "gh release list", "allow"},
		{"gh release list -R", "gh release list -R owner/repo", "allow"},
		{"gh pr list (known good)", "gh pr list", "allow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := HookInput{
				HookEventName: "PermissionRequest",
				ToolName:      "Bash",
				ToolInput:     ToolInput{Command: tt.command},
			}
			inputBytes, _ := json.Marshal(input)

			cmd := exec.Command(binaryPath)
			cmd.Stdin = bytes.NewReader(inputBytes)
			output, err := cmd.Output()
			require.Nil(t, err, "binary exited with error: %v, output: %s", err, output)
			require.NotEqual(t, 0, len(output), "binary produced no output (passthrough) for %q -- expected %s", tt.command, tt.expected)

			var resp PermissionResponse
			require.NoError(t, json.Unmarshal(output, &resp), "output was: %s", output)
			assert.Equal(t, tt.expected, resp.HookSpecificOutput.Decision.Behavior,
				"end-to-end: %q should be %s", tt.command, tt.expected)
		})
	}
}

// PermissionRequest is answered only once the permission engine has landed on
// "ask", so a deny that rides it alone is silent under defaultMode "auto" --
// which is how python stayed runnable. These cases pin the deny half to
// PreToolUse, which is unconditional, and pin the allow half OUT of it: an
// allow from PreToolUse settles the call before the user's own deny rules are
// consulted.
func TestPreToolUseDeniesButNeverAllows(t *testing.T) {
	binaryPath := buildTestBinary(t)

	for _, command := range []string{
		"python3 script.py",
		"python --version",
		"env python3 script.py",
		"uv run python script.py",
		"git status && python3 script.py",
		"echo $(python3 -c 'print(1)')",
		"node -e 'console.log(1)'",
	} {
		t.Run("deny/"+command, func(t *testing.T) {
			out := runHookBinary(t, binaryPath, HookInput{
				HookEventName: eventPreToolUse,
				ToolName:      "Bash",
				ToolInput:     ToolInput{Command: command},
			})
			require.NotEmpty(t, out, "PreToolUse produced no output for %q -- a passthrough means the deny never fires", command)

			var resp PreToolUseResponse
			require.NoError(t, json.Unmarshal(out, &resp), "output was: %s", out)
			// The CLI rejects a payload whose hookEventName is not the event it
			// dispatched, so the shape must follow the event.
			assert.Equal(t, eventPreToolUse, resp.HookSpecificOutput.HookEventName)
			assert.Equal(t, "deny", resp.HookSpecificOutput.PermissionDecision, "%q must be denied", command)
			assert.NotEmpty(t, resp.HookSpecificOutput.PermissionDecisionReason, "a deny must say why -- the reason reaches the model verbatim")
		})
	}

	for _, tt := range []struct {
		name  string
		input HookInput
	}{
		{"allowed bash command", HookInput{HookEventName: eventPreToolUse, ToolName: "Bash", ToolInput: ToolInput{Command: "gh repo view wow-look-at-my/go-toolchain"}}},
		{"read-only tool", HookInput{HookEventName: eventPreToolUse, ToolName: "Read"}},
		{"unmatched command", HookInput{HookEventName: eventPreToolUse, ToolName: "Bash", ToolInput: ToolInput{Command: "make build"}}},
	} {
		t.Run("silent/"+tt.name, func(t *testing.T) {
			out := runHookBinary(t, binaryPath, tt.input)
			assert.Empty(t, out, "PreToolUse must stay silent unless it denies; an allow here outranks the user's own deny rules")
		})
	}
}

// The CCR approval card is answered on PermissionRequest, which is the only
// event that can answer it: the -32003 retry hands canUseTool a precomputed
// "ask", so the call reaches the ask path where this hook races the human
// dialog. An allow there aborts the dialog and the retry succeeds. On
// PreToolUse the same tools must stay SILENT -- an allow before the permission
// engine runs would outrank the user's own deny rules.
func TestCCRManagementToolsAutoAllowed(t *testing.T) {
	binaryPath := buildTestBinary(t)

	allowed := []string{
		"mcp__Claude_Code_Remote__send_later",
		"mcp__Claude_Code_Remote__add_repo",
		"mcp__Claude_Code_Remote__register_repo_root",
		"mcp__Claude_Code_Remote__list_repos",
		"mcp__Claude_Code_Remote__list_environments",
		"mcp__Claude_Code_Remote__list_triggers",
		"mcp__Claude_Code_Remote__subscribe_pr_activity",
		"mcp__Claude_Code_Remote__unsubscribe_pr_activity",
	}
	for _, toolName := range allowed {
		t.Run("allow/"+toolName, func(t *testing.T) {
			out := runHookBinary(t, binaryPath, HookInput{HookEventName: eventPermissionRequest, ToolName: toolName})
			require.NotEmpty(t, out, "%s must be auto-allowed; empty output means the human dialog stands", toolName)

			var resp PermissionResponse
			require.NoError(t, json.Unmarshal(out, &resp), "output was: %s", out)
			assert.Equal(t, eventPermissionRequest, resp.HookSpecificOutput.HookEventName)
			assert.Equal(t, "allow", resp.HookSpecificOutput.Decision.Behavior)
		})

		t.Run("silent-on-pretooluse/"+toolName, func(t *testing.T) {
			out := runHookBinary(t, binaryPath, HookInput{HookEventName: eventPreToolUse, ToolName: toolName})
			assert.Empty(t, out, "PreToolUse must not allow %s -- that would settle the call before the user's deny rules are consulted", toolName)
		})
	}

	// Account-wide Routine mutation is NOT in the set and must still prompt:
	// an empty hook response is what lets the human dialog stand.
	for _, toolName := range []string{
		"mcp__Claude_Code_Remote__delete_trigger",
		"mcp__Claude_Code_Remote__create_trigger",
		"mcp__Claude_Code_Remote__update_trigger",
		"mcp__Claude_Code_Remote__fire_trigger",
	} {
		t.Run("still-prompts/"+toolName, func(t *testing.T) {
			out := runHookBinary(t, binaryPath, HookInput{HookEventName: eventPermissionRequest, ToolName: toolName})
			assert.Empty(t, out, "%s must NOT be auto-allowed -- it edits Routines account-wide", toolName)
		})
	}
}

// The flattened name is split on the LAST "__", so a server name carrying
// single underscores (Claude_Code_Remote) survives the split intact. Getting
// this wrong silently matches nothing and every call falls back to the dialog.
func TestParseMCPToolHandlesUnderscoredServerName(t *testing.T) {
	for _, tt := range []struct{ full, server, tool string }{
		{"mcp__Claude_Code_Remote__send_later", "Claude_Code_Remote", "send_later"},
		{"mcp__Claude_Code_Remote__register_repo_root", "Claude_Code_Remote", "register_repo_root"},
		{"mcp__Claude_Code_Remote__subscribe_pr_activity", "Claude_Code_Remote", "subscribe_pr_activity"},
		{"mcp__github__get_me", "github", "get_me"},
	} {
		server, tool := parseMCPTool(tt.full)
		assert.Equal(t, tt.server, server, "server of %q", tt.full)
		assert.Equal(t, tt.tool, tool, "tool of %q", tt.full)
	}
}

// The same command on PermissionRequest keeps the nested decision shape.
func TestPermissionRequestKeepsItsOwnShape(t *testing.T) {
	binaryPath := buildTestBinary(t)

	out := runHookBinary(t, binaryPath, HookInput{
		HookEventName: eventPermissionRequest,
		ToolName:      "Bash",
		ToolInput:     ToolInput{Command: "python3 script.py"},
	})
	require.NotEmpty(t, out)

	var resp PermissionResponse
	require.NoError(t, json.Unmarshal(out, &resp), "output was: %s", out)
	assert.Equal(t, eventPermissionRequest, resp.HookSpecificOutput.HookEventName)
	assert.Equal(t, "deny", resp.HookSpecificOutput.Decision.Behavior)
}

func buildTestBinary(t *testing.T) string {
	t.Helper()
	pluginDir := filepath.Join(getRepoRoot(t), "plugins/enhanced-auto-allow")

	buildDir := filepath.Join(pluginDir, "build")
	require.NoError(t, os.MkdirAll(buildDir, 0o755))
	binaryPath := filepath.Join(buildDir, "enhanced-auto-allow-test")
	t.Cleanup(func() { os.Remove(binaryPath) })

	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/")
	cmd.Dir = pluginDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", out)
	return binaryPath
}

func runHookBinary(t *testing.T, binaryPath string, input HookInput) []byte {
	t.Helper()
	inputBytes, err := json.Marshal(input)
	require.NoError(t, err)

	cmd := exec.Command(binaryPath)
	cmd.Stdin = bytes.NewReader(inputBytes)
	out, err := cmd.Output()
	require.NoError(t, err, "binary exited with error: %v, output: %s", err, out)
	return out
}

func getRepoRoot(t *testing.T) string {
	t.Helper()
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		out, err := cmd.Output()
		if err != nil {
			t.Skip("Cannot find repo root")
		}
		repoRoot = string(bytes.TrimSpace(out))
	}
	return repoRoot
}

func loadTestRules(t *testing.T) {
	t.Helper()
	repoRoot := getRepoRoot(t)
	rulesPath := filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.xml")
	data, err := os.ReadFile(rulesPath)
	require.Nil(t, err, "Failed to read rules.xml")
	rules, err = loadXMLRules(data)
	require.NoError(t, err, "Failed to parse rules.xml")
}

// One malformed byte disables EVERY rule, since loadXMLRules failing makes the
// hook exit 0 and pass everything through. The usual cause is a "--" inside an
// <!-- --> comment, which XML forbids: it turns thirty unrelated tests red at
// once and none of them says "the rules file does not parse".
func TestRulesXMLParses(t *testing.T) {
	repoRoot := getRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.xml"))
	require.NoError(t, err)

	_, err = loadXMLRules(data)
	require.NoError(t, err, "rules.xml does not parse; a '--' inside an XML comment is the usual cause")
}

// Indentation is tabs, so every reader picks their own width. Spaces are
// alignment only -- they follow a tab, never open a line.
func TestRulesXMLIndentedWithTabs(t *testing.T) {
	repoRoot := getRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.xml"))
	require.NoError(t, err)

	for i, line := range strings.Split(string(data), "\n") {
		assert.False(t, strings.HasPrefix(line, " "), "rules.xml:%d indents with spaces: %q", i+1, line)
	}
}

func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}
