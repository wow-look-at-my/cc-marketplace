package main

import (
	"fmt"
	"strings"
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

func TestListTagsWithPrefix_EmptyPrefix(t *testing.T) {
	_, err := ListTagsWithPrefix("")
	require.NoError(t, err)
}

func TestListTagsWithPrefix_NonExistentPrefix(t *testing.T) {
	tags, err := ListTagsWithPrefix("nonexistent-prefix-xyz/")
	require.NoError(t, err)
	require.Nil(t, tags)
}

func TestListTagsWithPrefix_WithTags(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/a/v1", "plugin/a/v2", "plugin/b/v1"})
	tags, err := ListTagsWithPrefix("plugin/a/")
	require.NoError(t, err)
	require.Len(t, tags, 2)
}

func TestListTagsWithPrefix_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("fail")
	})
	_, err := ListTagsWithPrefix("plugin/")
	require.NotNil(t, err)
}

func TestGetLatestTagVersion_NoTags(t *testing.T) {
	v, err := GetLatestTagVersion("nonexistent-plugin-xyz")
	require.NoError(t, err)
	require.Equal(t, 0, v)
}

func TestParsePluginTagVersion(t *testing.T) {
	cases := map[string]int{
		"plugin/foo/v1":           1,
		"plugin/foo/v42":          42,
		"plugin/foo/latest":       0,
		"plugin/foo/vabc":         0,
		"plugin/foo":              0,
		"plugin/foo/v1/extra":     0,
		"notplugin/foo/v1":        0,
		"plugin/foo/branch/v1":    0,
		"plugin/foo#1":            0,
	}
	for tag, want := range cases {
		got := parsePluginTagVersion(tag)
		require.Equal(t, want, got, "tag=%s", tag)
	}
}

func TestGetLatestTagVersion_WithVersions(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/myplugin/v1", "plugin/myplugin/v5", "plugin/myplugin/v3", "plugin/myplugin/latest"})
	v, err := GetLatestTagVersion("myplugin")
	require.NoError(t, err)
	require.Equal(t, 5, v)
}

func TestListPluginNames(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/alpha/v1", "plugin/alpha/v2", "plugin/beta/v1"})
	names, err := ListPluginNames()
	require.NoError(t, err)
	require.Len(t, names, 2)
	require.Contains(t, names, "alpha")
	require.Contains(t, names, "beta")
}

func TestListPluginNames_SkipsBadFormat(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/good/v1", "badformat", "no-v-suffix/x"})
	names, err := ListPluginNames()
	require.NoError(t, err)
	require.Len(t, names, 1)
	require.Equal(t, "good", names[0])
}

func TestListPluginTags_NonExistent(t *testing.T) {
	tags, err := ListPluginTags("nonexistent-plugin-xyz")
	require.NoError(t, err)
	require.Nil(t, tags)
}

func TestListPluginTags_FiltersLatest(t *testing.T) {
	// The /v prefix itself excludes the /latest pointer, so only versioned tags are returned.
	mockGitWithTags(t, []string{"plugin/myplugin/v1", "plugin/myplugin/v2"})
	tags, err := ListPluginTags("myplugin")
	require.NoError(t, err)
	require.Len(t, tags, 2)
	for _, tag := range tags {
		require.False(t, strings.HasSuffix(tag, "/latest"))
	}
}

func TestRemoteBranchExists(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "ls-remote" {
			return "abc123\trefs/heads/master\n", nil
		}
		return "", nil
	})
	exists, err := RemoteBranchExists("master")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestRemoteBranchExists_NotFound(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", nil
	})
	exists, err := RemoteBranchExists("nonexistent")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestRemoteBranchExists_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("network error")
	})
	_, err := RemoteBranchExists("master")
	require.NotNil(t, err)
}

func TestRunGit_SkipsPushLocally(t *testing.T) {
	// Use real runGit for this test
	t.Setenv("CI", "")
	out, err := runGitReal("push", "origin", "test")
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestDeleteLocalTags_NonExistent(t *testing.T) {
	err := DeleteLocalTags("nonexistent-tag-xyz")
	require.NoError(t, err)
}

func TestDeleteRemoteTags(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", nil
	})
	err := DeleteRemoteTags("tag1", "tag2")
	require.NoError(t, err)
}

func TestDeleteRemoteTags_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("push failed")
	})
	err := DeleteRemoteTags("tag1")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to delete tag")
}

func TestHasCommitsAfterTag_NoTags(t *testing.T) {
	has, err := HasCommitsAfterTag("nonexistent-plugin-xyz", "/tmp/nonexistent")
	require.NoError(t, err)
	require.True(t, has)
}

func TestHasCommitsAfterTag_NoChanges(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/myplugin/v1"})
	has, err := HasCommitsAfterTag("myplugin", "/tmp/path")
	require.NoError(t, err)
	require.False(t, has) // mock returns "0" for rev-list --count
}

func TestHasCommitsAfterTag_WithChanges(t *testing.T) {
	mockGitWithTags(t, []string{"plugin/myplugin/v1"}, func(args ...string) (string, error) {
		if args[0] == "rev-list" {
			return "3\n", nil
		}
		return "", nil
	})
	has, err := HasCommitsAfterTag("myplugin", "/tmp/path")
	require.NoError(t, err)
	require.True(t, has)
}

func TestReadFileFromTag(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "show" && args[1] == "v1:README.md" {
			return "# Hello\n", nil
		}
		return "", fmt.Errorf("not found")
	})
	content, err := ReadFileFromTag("v1", "README.md")
	require.NoError(t, err)
	require.Equal(t, "# Hello\n", content)
}

func TestReadFileFromTag_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("not found")
	})
	_, err := ReadFileFromTag("v1", "missing.txt")
	require.NotNil(t, err)
}

func TestListFilesInTag(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		if args[0] == "ls-tree" {
			return "commands/foo.md\ncommands/bar.md\n", nil
		}
		return "", nil
	})
	files, err := ListFilesInTag("v1", "commands")
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "foo.md", files[0])
	require.Equal(t, "bar.md", files[1])
}

func TestListFilesInTag_Empty(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", nil
	})
	files, err := ListFilesInTag("v1", "commands")
	require.NoError(t, err)
	require.Nil(t, files)
}

func TestListFilesInTag_Error(t *testing.T) {
	mockGit(t, func(args ...string) (string, error) {
		return "", fmt.Errorf("bad tag")
	})
	_, err := ListFilesInTag("bad", "dir")
	require.NotNil(t, err)
}

func TestRunGitReal_InvalidCommand(t *testing.T) {
	_, err := runGitReal("not-a-real-command")
	require.NotNil(t, err)
}
