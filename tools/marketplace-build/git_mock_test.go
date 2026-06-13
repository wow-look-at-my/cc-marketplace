package main

import (
	"fmt"
	"testing"
)

// mockGit replaces runGit with a function that returns predefined responses
// based on the git subcommand. Returns a cleanup function to restore the original.
func mockGit(t *testing.T, handler func(args ...string) (string, error)) {
	t.Helper()
	origRunGit := runGit
	t.Cleanup(func() {
		runGit = origRunGit
	})
	runGit = handler
}

// mockGitDefaults installs a runGit that answers the metadata queries that
// release-plugin / update-marketplace make (HEAD SHA, current branch, repo URL).
func mockGitDefaults(t *testing.T) {
	t.Helper()
	mockGit(t, func(args ...string) (string, error) {
		if len(args) == 0 {
			return "", fmt.Errorf("no args")
		}
		switch args[0] {
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
		}
		return "", nil
	})
}
