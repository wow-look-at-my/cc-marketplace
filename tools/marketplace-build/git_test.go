package main

import (
	"fmt"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func TestGetRepoRoot(t *testing.T) {
	root := getRepoRoot()
	require.NotEmpty(t, root)
	require.NotEqual(t, ".", root)
}

func TestGetCurrentBranch(t *testing.T) {
	branch, err := GetCurrentBranch()
	require.NoError(t, err)
	require.NotEmpty(t, branch)
}

func TestGetCurrentBranch_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("not a git repo")
	})
	_, err := GetCurrentBranch()
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to get current branch")
}

func TestGetHeadSHA(t *testing.T) {
	sha, err := GetHeadSHA()
	require.NoError(t, err)
	require.Len(t, sha, 40)
}

func TestGetHeadSHA_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("fail")
	})
	_, err := GetHeadSHA()
	require.NotNil(t, err)
}

func TestGetRepoInfo_SSH(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "git@github.com:myowner/myrepo.git\n", nil
	})
	owner, repo, err := GetRepoInfo()
	require.NoError(t, err)
	require.Equal(t, "myowner", owner)
	require.Equal(t, "myrepo", repo)
}

func TestGetRepoInfo_HTTPS(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "https://github.com/myowner/myrepo.git\n", nil
	})
	owner, repo, err := GetRepoInfo()
	require.NoError(t, err)
	require.Equal(t, "myowner", owner)
	require.Equal(t, "myrepo", repo)
}

func TestGetRepoInfo_HTTPSNoGit(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "https://github.com/myowner/myrepo\n", nil
	})
	owner, repo, err := GetRepoInfo()
	require.NoError(t, err)
	require.Equal(t, "myowner", owner)
	require.Equal(t, "myrepo", repo)
}

func TestGetRepoInfo_HTTPSWithCredentials(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "https://x-access-token:ghp_abc123@github.com/myowner/myrepo\n", nil
	})
	owner, repo, err := GetRepoInfo()
	require.NoError(t, err)
	require.Equal(t, "myowner", owner)
	require.Equal(t, "myrepo", repo)
}

func TestGetRepoInfo_UnknownFormat(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "http://localhost/repo\n", nil
	})
	_, _, err := GetRepoInfo()
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "could not parse github repo")
}

func TestGetRepoInfo_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("no remote")
	})
	_, _, err := GetRepoInfo()
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to get remote origin")
}

func TestRunGitReal_InvalidCommand(t *testing.T) {
	_, err := runGitReal("not-a-real-command")
	require.NotNil(t, err)
}
