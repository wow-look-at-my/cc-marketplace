package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// GetCurrentBranch returns the current git branch name
func GetCurrentBranch() (string, error) {
	out, err := runGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// GetHeadSHA returns the SHA of HEAD
func GetHeadSHA() (string, error) {
	out, err := runGit("rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD SHA: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// GetRepoInfo extracts owner/repo from git remote origin
func GetRepoInfo() (owner, repo string, err error) {
	out, err := runGit("remote", "get-url", "origin")
	if err != nil {
		return "", "", fmt.Errorf("failed to get remote origin: %w", err)
	}
	url := strings.TrimSpace(out)

	// Handle SSH format: git@github.com:owner/repo.git
	sshPattern := regexp.MustCompile(`git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := sshPattern.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	// Handle HTTPS format: https://github.com/owner/repo.git (with optional user@ credentials)
	httpsPattern := regexp.MustCompile(`https://(?:[^@]+@)?github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := httpsPattern.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	return "", "", fmt.Errorf("could not parse github repo from origin URL: %s", url)
}

var repoRoot string

// getRepoRoot returns the root directory of the git repository
func getRepoRoot() string {
	if repoRoot != "" {
		return repoRoot
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		repoRoot = "."
	} else {
		repoRoot = strings.TrimSpace(string(out))
	}
	return repoRoot
}

// runGit runs a git command and returns stdout.
// This is a variable so tests can replace it with a mock.
var runGit = runGitReal

func runGitReal(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = getRepoRoot()
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %v failed: %s", args, exitErr.Stderr)
		}
		return "", err
	}
	return string(out), nil
}
