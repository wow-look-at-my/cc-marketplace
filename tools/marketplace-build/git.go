package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Handle HTTPS format: https://github.com/owner/repo.git
	httpsPattern := regexp.MustCompile(`https://github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := httpsPattern.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	return "", "", fmt.Errorf("could not parse github repo from origin URL: %s", url)
}

// GetLatestTagVersion finds the highest version from plugin/{plugin}/v* tags
// Returns 0 if no tags exist
func GetLatestTagVersion(plugin string) (int, error) {
	out, err := runGit("tag", "-l", fmt.Sprintf("plugin/%s/v*", plugin))
	if err != nil {
		return 0, nil
	}

	tags := strings.Split(strings.TrimSpace(out), "\n")
	if len(tags) == 0 || tags[0] == "" {
		return 0, nil
	}

	// Find highest version
	highest := 0
	for _, tag := range tags {
		// Extract version from tag like "plugin/my-plugin/v3"
		parts := strings.Split(tag, "/")
		if len(parts) >= 2 {
			vStr := strings.TrimPrefix(parts[len(parts)-1], "v")
			var v int
			fmt.Sscanf(vStr, "%d", &v)
			if v > highest {
				highest = v
			}
		}
	}

	return highest, nil
}

// HasCommitsAfterTag checks if there are commits to pluginPath after the latest version tag
func HasCommitsAfterTag(plugin, pluginPath string) (bool, error) {
	// Find highest version tag for this plugin
	version, err := GetLatestTagVersion(plugin)
	if err != nil || version == 0 {
		// No tags exist, so definitely has changes (first build)
		return true, nil
	}

	tagName := fmt.Sprintf("plugin/%s/v%d", plugin, version)

	// Read mh.plugin.json from the tag to get source commit
	out, err := runGit("show", fmt.Sprintf("%s:mh.plugin.json", tagName))
	if err != nil {
		// Can't read metadata, assume we need to build
		return true, nil
	}

	var metadata struct {
		SourceCommit string `json:"sourceCommit"`
	}
	if err := json.Unmarshal([]byte(out), &metadata); err != nil || metadata.SourceCommit == "" {
		// Invalid metadata, assume we need to build
		return true, nil
	}

	// Count commits to plugin path since the source commit
	out, err = runGit("rev-list", "--count", fmt.Sprintf("%s..HEAD", metadata.SourceCommit), "--", pluginPath)
	if err != nil {
		// If this fails, assume we need to build
		return true, nil
	}

	count := strings.TrimSpace(out)
	return count != "0", nil
}

// CreateOrphanCommit creates an orphan commit with the given directory contents
// Returns the commit SHA
func CreateOrphanCommit(sourceDir, message string) (string, error) {
	// Create temp dir with a fresh git repo
	tmpDir, err := os.MkdirTemp("", "orphan-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Init repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init failed: %s: %w", out, err)
	}

	// Copy files
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(tmpDir, relPath)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		os.MkdirAll(filepath.Dir(destPath), 0755)
		return os.WriteFile(destPath, data, info.Mode())
	})
	if err != nil {
		return "", fmt.Errorf("failed to copy files: %w", err)
	}

	// Add and commit
	cmd = exec.Command("git", "add", "-A")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add failed: %s: %w", out, err)
	}

	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=CI", "GIT_AUTHOR_EMAIL=ci@localhost",
		"GIT_COMMITTER_NAME=CI", "GIT_COMMITTER_EMAIL=ci@localhost")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit failed: %s: %w", out, err)
	}

	// Get SHA
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = tmpDir
	shaOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get SHA: %w", err)
	}
	sha := strings.TrimSpace(string(shaOut))

	// Fetch into main repo
	if _, err := runGit("fetch", tmpDir, sha); err != nil {
		return "", fmt.Errorf("failed to fetch commit: %w", err)
	}

	return sha, nil
}

// CreateTag creates a git tag pointing to a commit
func CreateTag(tagName, commitSHA string) error {
	// Delete existing tag if it exists (for updating latest)
	_ = runGitNoOutput("tag", "-d", tagName)

	_, err := runGit("tag", tagName, commitSHA)
	return err
}

// PushTags pushes tags to origin
func PushTags(tags ...string) error {
	args := []string{"push", "origin"}
	args = append(args, tags...)
	_, err := runGit(args...)
	return err
}

// ForcePushTag force-pushes a single tag (for updating latest)
func ForcePushTag(tag string) error {
	_, err := runGit("push", "--force", "origin", tag)
	return err
}

// DeleteRemoteTags deletes tags from origin
func DeleteRemoteTags(tags ...string) error {
	for _, tag := range tags {
		if _, err := runGit("push", "origin", ":refs/tags/"+tag); err != nil {
			return fmt.Errorf("failed to delete tag %s: %w", tag, err)
		}
	}
	return nil
}

// DeleteLocalTags deletes local tags
func DeleteLocalTags(tags ...string) error {
	for _, tag := range tags {
		_ = runGitNoOutput("tag", "-d", tag)
	}
	return nil
}

// ReadFileFromTag reads a file's content from a git tag
func ReadFileFromTag(tag, path string) (string, error) {
	out, err := runGit("show", fmt.Sprintf("%s:%s", tag, path))
	if err != nil {
		return "", err
	}
	return out, nil
}

// ListFilesInTag lists files in a directory at a git tag
// Returns slice of filenames (not full paths)
func ListFilesInTag(tag, dirPath string) ([]string, error) {
	// Use ls-tree to list directory contents
	out, err := runGit("ls-tree", "--name-only", tag, dirPath+"/")
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		// ls-tree returns full paths, extract just the filename
		name := filepath.Base(line)
		files = append(files, name)
	}
	return files, nil
}

// ListTagsWithPrefix lists all tags matching a prefix (empty prefix = all tags)
func ListTagsWithPrefix(prefix string) ([]string, error) {
	var out string
	var err error
	if prefix == "" {
		out, err = runGit("tag", "-l")
	} else {
		out, err = runGit("tag", "-l", prefix+"*")
	}
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	return strings.Split(strings.TrimSpace(out), "\n"), nil
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

// runGit runs a git command and returns stdout
func runGit(args ...string) (string, error) {
	if os.Getenv("CI") == "" && len(args) > 0 && args[0] == "push" {
		fmt.Printf("[local] skipping: git %v\n", args)
		return "", nil
	}

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

// runGitNoOutput runs a git command without returning output
func runGitNoOutput(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = getRepoRoot()
	return cmd.Run()
}

// RemoteBranchExists checks if a branch exists on the remote
func RemoteBranchExists(branch string) (bool, error) {
	out, err := runGit("ls-remote", "--heads", "origin", branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

