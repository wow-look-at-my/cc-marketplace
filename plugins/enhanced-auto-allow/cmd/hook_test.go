package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Note: Schema validation is handled by plugin.bats during build

func TestGitStatusAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git status")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git status', got %q", decision)
	}
}

func TestGitDiffAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git diff")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git diff', got %q", decision)
	}
}

func TestGitLogAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log --oneline -10")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git log --oneline -10', got %q", decision)
	}
}

func TestGitBranchListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git branch -a")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git branch -a', got %q", decision)
	}
}

func TestGitBranchDeletePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git branch -d feature")
	if decision != "" {
		t.Errorf("Expected passthrough for 'git branch -d feature', got %q", decision)
	}
}

func TestGitConfigGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config --get user.email")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git config --get user.email', got %q", decision)
	}
}

func TestGitConfigListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config --list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git config --list', got %q", decision)
	}
}

func TestGitConfigSetPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config user.email foo@bar.com")
	if decision != "" {
		t.Errorf("Expected passthrough for 'git config user.email foo@bar.com', got %q", decision)
	}
}

func TestGitRemoteVerboseAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git remote -v")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git remote -v', got %q", decision)
	}
}

func TestGitRemoteShowAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git remote show origin")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git remote show origin', got %q", decision)
	}
}

func TestGhPrListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh pr list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'gh pr list', got %q", decision)
	}
}

func TestGhPrViewAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh pr view 123")
	if decision != "allow" {
		t.Errorf("Expected allow for 'gh pr view 123', got %q", decision)
	}
}

func TestGhPrCreatePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh pr create")
	if decision != "" {
		t.Errorf("Expected passthrough for 'gh pr create', got %q", decision)
	}
}

func TestGhApiGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh api /repos/owner/repo")
	if decision != "allow" {
		t.Errorf("Expected allow for 'gh api /repos/owner/repo', got %q", decision)
	}
}

func TestGhApiPostPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh api -X POST /repos/owner/repo")
	if decision != "" {
		t.Errorf("Expected passthrough for 'gh api -X POST /repos/owner/repo', got %q", decision)
	}
}

func TestGhBrowseAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh browse")
	if decision != "allow" {
		t.Errorf("Expected allow for 'gh browse', got %q", decision)
	}
}

func TestGhRunViewDenied(t *testing.T) {
	loadTestRules(t)
	decision, message := evaluateCommand("gh run view 123")
	if decision != "deny" {
		t.Errorf("Expected deny for 'gh run view 123', got %q", decision)
	}
	if message == "" {
		t.Error("Expected deny message for 'gh run view'")
	}
}

func TestGhRunWatchDenied(t *testing.T) {
	loadTestRules(t)
	decision, message := evaluateCommand("gh run watch 123")
	if decision != "deny" {
		t.Errorf("Expected deny for 'gh run watch 123', got %q", decision)
	}
	if message == "" {
		t.Error("Expected deny message for 'gh run watch'")
	}
}

func TestGhRunListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh run list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'gh run list', got %q", decision)
	}
}

func TestFindPipeGrepAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`find /home/mhaynie -type f -name "*decode*" -o -name "*parse*" 2>/dev/null | grep -i tool`)
	if decision != "allow" {
		t.Errorf("Expected allow for find|grep file search, got %q", decision)
	}
}

func TestFindBasicAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("find . -name '*.go' -type f")
	if decision != "allow" {
		t.Errorf("Expected allow for basic find, got %q", decision)
	}
}

func TestFindExecGrepAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`find /home/mhaynie/repos/UnrealEngine -name "*.h" -type f -exec grep -l "class FSkeletalMeshSceneProxy" {} \;`)
	if decision != "allow" {
		t.Errorf("Expected allow for find -exec grep, got %q", decision)
	}
}

func TestFindExecRmPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("find . -name '*.tmp' -exec rm {} \\;")
	if decision != "" {
		t.Errorf("Expected passthrough for find -exec rm, got %q", decision)
	}
}

func TestFindDeletePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("find . -name '*.tmp' -delete")
	if decision != "" {
		t.Errorf("Expected passthrough for find with -delete, got %q", decision)
	}
}

func TestGrepAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("grep -ri 'TODO' src/")
	if decision != "allow" {
		t.Errorf("Expected allow for grep, got %q", decision)
	}
}

func TestCommandSubstitutionPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log $(echo test)")
	if decision != "" {
		t.Errorf("Expected passthrough for command with substitution, got %q", decision)
	}
}

func TestPipeBothAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log | head")
	if decision != "allow" {
		t.Errorf("Expected allow for 'git log | head' (both safe), got %q", decision)
	}
}

func TestPipeOneUnknown(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log | python")
	if decision != "" {
		t.Errorf("Expected passthrough for 'git log | python', got %q", decision)
	}
}

func TestUnknownCommandPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("python --version")
	if decision != "" {
		t.Errorf("Expected passthrough for unknown command 'python', got %q", decision)
	}
}

func TestEchoRedirectPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("echo foo > file.txt")
	if decision != "" {
		t.Errorf("Expected passthrough for echo with redirect, got %q", decision)
	}
}

func TestEchoAppendPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("echo foo >> file.txt")
	if decision != "" {
		t.Errorf("Expected passthrough for echo with append, got %q", decision)
	}
}

func TestSortRedirectPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("sort input.txt > output.txt")
	if decision != "" {
		t.Errorf("Expected passthrough for sort with redirect, got %q", decision)
	}
}

func TestEchoPipeAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("echo hello | grep hello")
	if decision != "allow" {
		t.Errorf("Expected allow for echo piped to grep, got %q", decision)
	}
}

func TestGitShowWithEchoAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`git show 54d7aa918a94 --stat && echo "---" && git show 54d7aa918a94 -- path/to/file.cpp`)
	if decision != "allow" {
		t.Errorf("Expected allow for git show && echo && git show, got %q", decision)
	}
}

func TestJustListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("just --list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'just --list', got %q", decision)
	}
}

func TestJustListWithRedirectAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`just --list 2>/dev/null || echo "no justfile"`)
	if decision != "allow" {
		t.Errorf("Expected allow for 'just --list 2>/dev/null || echo no justfile', got %q", decision)
	}
}

func TestJustBuildPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("just build")
	if decision != "" {
		t.Errorf("Expected passthrough for 'just build', got %q", decision)
	}
}

func TestClaudeMcpListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude mcp list', got %q", decision)
	}
}

func TestClaudeMcpHelpAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp --help")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude mcp --help', got %q", decision)
	}
}

func TestClaudeMcpGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp get server-name")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude mcp get server-name', got %q", decision)
	}
}

func TestClaudeMcpAddPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp add my-server -- npx server")
	if decision != "" {
		t.Errorf("Expected passthrough for 'claude mcp add', got %q", decision)
	}
}

func TestClaudeMcpRemovePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp remove my-server")
	if decision != "" {
		t.Errorf("Expected passthrough for 'claude mcp remove', got %q", decision)
	}
}

func TestClaudeVersionAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude --version")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude --version', got %q", decision)
	}
}

func TestClaudeHelpAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude --help")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude --help', got %q", decision)
	}
}

func TestClaudePluginListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude plugin list', got %q", decision)
	}
}

func TestClaudePluginMarketplaceListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin marketplace list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude plugin marketplace list', got %q", decision)
	}
}

func TestClaudePluginInstallPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin install some-plugin")
	if decision != "" {
		t.Errorf("Expected passthrough for 'claude plugin install', got %q", decision)
	}
}

func TestClaudeConfigListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude config list")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude config list', got %q", decision)
	}
}

func TestClaudeConfigGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude config get key")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude config get key', got %q", decision)
	}
}

func TestClaudeConfigSetPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude config set key value")
	if decision != "" {
		t.Errorf("Expected passthrough for 'claude config set', got %q", decision)
	}
}

func TestClaudePluginHelpAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin --help")
	if decision != "allow" {
		t.Errorf("Expected allow for 'claude plugin --help', got %q", decision)
	}
}

func TestCompoundCommands(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"and both allowed", "git status && git diff", "allow"},
		{"and one unknown", "git status && python --version", ""},
		{"or both allowed", "git status || git diff", "allow"},
		{"fetch and show", "git fetch origin latest && git show HEAD", "allow"},
		{"basic commands", "ls && cat file.txt", "allow"},
		{"with deny", "git status && gh run view 123", "deny"},
		{"triple and", "git status && git diff && git log", "allow"},
		{"semicolon", "git status; git diff", "allow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			if decision != tt.expected {
				t.Errorf("evaluateCommand(%q) = %q, want %q", tt.command, decision, tt.expected)
			}
		})
	}
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
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("Failed to parse output: %v\nOutput was: %s", err, output)
	}
	if resp.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Errorf("Expected allow for Read, got %q", resp.HookSpecificOutput.Decision.Behavior)
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
	rulesPath := filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.json")
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("Failed to read rules: %v", err)
	}
	if err := json.Unmarshal(data, &rules); err != nil {
		t.Fatalf("Failed to parse rules: %v", err)
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
