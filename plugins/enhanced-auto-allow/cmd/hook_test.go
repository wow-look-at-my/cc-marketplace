package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
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
	walk(xr.Commands)
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

func TestCookedRulesRoundTrip(t *testing.T) {
	repoRoot := getRepoRoot(t)
	rulesPath := filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.json")
	data, err := os.ReadFile(rulesPath)
	require.Nil(t, err)

	var generic map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &generic))
	delete(generic, "$schema")
	delete(generic, "mh")
	cooked, err := json.MarshalIndent(generic, "", "\t")
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal(cooked, &rules))

	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"gh repo view", "gh repo view wow-look-at-my/go-toolchain", "allow"},
		{"gh release list", "gh release list", "allow"},
		{"gh pr list", "gh pr list", "allow"},
		{"git status", "git status", "allow"},
		{"gh run view denied", "gh run view 123", "deny"},
		{"unknown passthrough", "python --version", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, tt.expected, decision, "cooked rules: evaluateCommand(%q)", tt.command)
		})
	}
}

func TestDuplicateEntriesMerged(t *testing.T) {
	saved := rules
	defer func() { rules = saved }()

	rules = Rules{
		Commands: []CommandNode{
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
		Commands: []CommandNode{
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
