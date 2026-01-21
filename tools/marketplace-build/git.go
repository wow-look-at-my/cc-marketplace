package main

import (
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

// GetLatestTagVersion gets the version from {branch}/{plugin}/latest tag
// Returns 0 if no tag exists
func GetLatestTagVersion(branch, plugin string) (int, error) {
	tagName := fmt.Sprintf("%s/%s/latest", branch, plugin)

	// Check if tag exists
	_, err := runGit("rev-parse", "--verify", fmt.Sprintf("refs/tags/%s", tagName))
	if err != nil {
		return 0, nil
	}

	// Find highest version tag for this branch/plugin
	out, err := runGit("tag", "-l", fmt.Sprintf("%s/%s/v*", branch, plugin))
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
		// Extract version from tag like "master/my-plugin/v3"
		parts := strings.Split(tag, "/")
		if len(parts) >= 3 {
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

// HasCommitsAfterTag checks if there are commits to pluginPath after the latest tag
func HasCommitsAfterTag(branch, plugin, pluginPath string) (bool, error) {
	tagName := fmt.Sprintf("%s/%s/latest", branch, plugin)

	// Check if tag exists
	_, err := runGit("rev-parse", "--verify", fmt.Sprintf("refs/tags/%s", tagName))
	if err != nil {
		// No tag exists, so definitely has changes (first build)
		return true, nil
	}

	// Count commits to plugin path since the tag
	out, err := runGit("rev-list", "--count", fmt.Sprintf("%s..HEAD", tagName), "--", pluginPath)
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
	if dryRun {
		fmt.Printf("[dry-run] Would push tags: %v\n", tags)
		return nil
	}

	args := []string{"push", "origin"}
	args = append(args, tags...)
	_, err := runGit(args...)
	return err
}

// DeleteRemoteTags deletes tags from origin
func DeleteRemoteTags(tags ...string) error {
	if dryRun {
		fmt.Printf("[dry-run] Would delete remote tags: %v\n", tags)
		return nil
	}

	for _, tag := range tags {
		if _, err := runGit("push", "origin", ":refs/tags/"+tag); err != nil {
			return fmt.Errorf("failed to delete tag %s: %w", tag, err)
		}
	}
	return nil
}

// DeleteLocalTags deletes local tags
func DeleteLocalTags(tags ...string) error {
	if dryRun {
		fmt.Printf("[dry-run] Would delete local tags: %v\n", tags)
		return nil
	}

	for _, tag := range tags {
		_ = runGitNoOutput("tag", "-d", tag)
	}
	return nil
}

// ListTagsWithPrefix lists all tags matching a prefix
func ListTagsWithPrefix(prefix string) ([]string, error) {
	out, err := runGit("tag", "-l", prefix+"*")
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

