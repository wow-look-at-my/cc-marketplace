package main

import (
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestRunCleanupBranch_TagExists(t *testing.T) {
	var deletedTag string
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "marketplace/feature-x\nmarketplace/other\n", nil
		}
		if args[0] == "push" {
			deletedTag = strings.TrimPrefix(args[2], ":refs/tags/")
			return "", nil
		}
		if args[0] == "tag" && args[1] == "-d" {
			return "", nil
		}
		return "", nil
	})

	err := runCleanupBranch(cleanupBranchCmd, []string{"feature-x"})
	require.NoError(t, err)
	require.Equal(t, "marketplace/feature-x", deletedTag)
}

func TestRunCleanupBranch_NoTag(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "tag" && args[1] == "-l" {
			return "marketplace/other\n", nil
		}
		return "", nil
	})

	err := runCleanupBranch(cleanupBranchCmd, []string{"feature-x"})
	require.NoError(t, err)
}

func TestRunCleanupBranch_NoTags(t *testing.T) {
	mockGitWithTags(t, nil)
	err := runCleanupBranch(cleanupBranchCmd, []string{"feature-x"})
	require.NoError(t, err)
}
