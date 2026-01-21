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
// Returns "0.0.0" if no tag exists
func GetLatestTagVersion(branch, plugin string) (string, error) {
	tagName := fmt.Sprintf("%s/%s/latest", branch, plugin)

	// Check if tag exists
	_, err := runGit("rev-parse", "--verify", fmt.Sprintf("refs/tags/%s", tagName))
	if err != nil {
		// Tag doesn't exist, return 0.0.0
		return "0.0.0", nil
	}

	// Get the version tag that latest points to by looking at sibling tags
	// We need to find the highest version tag for this branch/plugin
	out, err := runGit("tag", "-l", fmt.Sprintf("%s/%s/v*", branch, plugin))
	if err != nil {
		return "0.0.0", nil
	}

	tags := strings.Split(strings.TrimSpace(out), "\n")
	if len(tags) == 0 || tags[0] == "" {
		return "0.0.0", nil
	}

	// Find highest version
	var highestVersion string
	for _, tag := range tags {
		// Extract version from tag like "master/my-plugin/v1.2.3"
		parts := strings.Split(tag, "/")
		if len(parts) >= 3 {
			version := strings.TrimPrefix(parts[len(parts)-1], "v")
			if highestVersion == "" || compareVersions(version, highestVersion) > 0 {
				highestVersion = version
			}
		}
	}

	if highestVersion == "" {
		return "0.0.0", nil
	}
	return highestVersion, nil
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
	// Create a temporary index file
	tmpIndex, err := os.CreateTemp("", "git-index-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp index: %w", err)
	}
	tmpIndex.Close()
	defer os.Remove(tmpIndex.Name())

	// Set GIT_INDEX_FILE to use our temporary index
	env := append(os.Environ(), fmt.Sprintf("GIT_INDEX_FILE=%s", tmpIndex.Name()))

	// Add all files from sourceDir to the temporary index
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Add file to index
		cmd := exec.Command("git", "update-index", "--add", "--cacheinfo",
			fmt.Sprintf("100644,%s,%s", hashFile(path), relPath))
		cmd.Env = env
		cmd.Dir = getRepoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add %s to index: %s: %w", relPath, out, err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to add files to index: %w", err)
	}

	// Write the tree
	cmd := exec.Command("git", "write-tree")
	cmd.Env = env
	cmd.Dir = getRepoRoot()
	treeOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write tree: %w", err)
	}
	treeSHA := strings.TrimSpace(string(treeOut))

	// Create orphan commit (no parent)
	commitOut, err := runGit("commit-tree", treeSHA, "-m", message)
	if err != nil {
		return "", fmt.Errorf("failed to create commit: %w", err)
	}

	return strings.TrimSpace(commitOut), nil
}

// hashFile returns the git blob hash of a file
func hashFile(path string) string {
	out, err := runGit("hash-object", "-w", path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
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

// compareVersions compares two semver strings, returns >0 if a > b, <0 if a < b, 0 if equal
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := 0; i < 3; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &aNum)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bNum)
		}
		if aNum != bNum {
			return aNum - bNum
		}
	}
	return 0
}

// BumpPatchVersion increments the patch version
func BumpPatchVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "0.0.1"
	}

	var major, minor, patch int
	fmt.Sscanf(parts[0], "%d", &major)
	fmt.Sscanf(parts[1], "%d", &minor)
	fmt.Sscanf(parts[2], "%d", &patch)

	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}
