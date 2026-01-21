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

func TestCommandSubstitutionPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log $(echo test)")
	if decision != "" {
		t.Errorf("Expected passthrough for command with substitution, got %q", decision)
	}
}

func TestPipePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log | head")
	if decision != "" {
		t.Errorf("Expected passthrough for piped command, got %q", decision)
	}
}

func TestUnknownCommandPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("python --version")
	if decision != "" {
		t.Errorf("Expected passthrough for unknown command 'python', got %q", decision)
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
