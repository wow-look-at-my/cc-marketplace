package main

import (
	"fmt"
	"strings"
	"testing"
)

// mockGit replaces runGit with a function that returns predefined responses
// based on the git subcommand. Returns a cleanup function to restore the original.
func mockGit(t *testing.T, handler func(args ...string) (string, error)) {
	t.Helper()
	origRunGit := runGit
	origRunGitNoOutput := runGitNoOutput
	t.Cleanup(func() {
		runGit = origRunGit
		runGitNoOutput = origRunGitNoOutput
	})
	runGit = handler
	runGitNoOutput = func(args ...string) error {
		_, err := handler(args...)
		return err
	}
}

// mockGitWithTags creates a mock that responds to tag listing and other common commands
func mockGitWithTags(t *testing.T, tags []string, extraHandlers ...func(args ...string) (string, error)) {
	t.Helper()
	mockGit(t, func(args ...string) (string, error) {
		if len(args) == 0 {
			return "", fmt.Errorf("no args")
		}

		// Check extra handlers first
		for _, h := range extraHandlers {
			out, err := h(args...)
			if out != "" || err != nil {
				return out, err
			}
		}

		switch args[0] {
		case "tag":
			if len(args) >= 2 && args[1] == "-l" {
				prefix := ""
				if len(args) >= 3 {
					prefix = strings.TrimSuffix(args[2], "*")
				}
				var matching []string
				for _, tag := range tags {
					if prefix == "" || strings.HasPrefix(tag, prefix) {
						matching = append(matching, tag)
					}
				}
				return strings.Join(matching, "\n"), nil
			}
			if len(args) >= 3 && args[1] == "-d" {
				return "", nil
			}
		case "rev-parse":
			if len(args) >= 2 {
				switch args[1] {
				case "--abbrev-ref":
					return "master\n", nil
				case "HEAD":
					return "abc123def456789012345678901234567890abcd\n", nil
				case "--show-toplevel":
					return "/mock/repo\n", nil
				}
			}
		case "remote":
			if len(args) >= 3 && args[1] == "get-url" {
				return "https://github.com/test-owner/test-repo.git\n", nil
			}
		case "push":
			return "", nil
		case "show":
			// Return empty metadata by default
			if len(args) >= 2 && strings.Contains(args[1], "mh.plugin.json") {
				return `{"sourceCommit":"old123"}`, nil
			}
			if len(args) >= 2 && strings.Contains(args[1], "plugin.json") {
				return `{"name":"test","description":"test plugin","version":"1"}`, nil
			}
		case "rev-list":
			return "0\n", nil
		case "ls-remote":
			return "", nil
		case "ls-tree":
			return "", nil
		case "merge-base":
			return "", fmt.Errorf("not ancestor")
		}
		return "", nil
	})
}
