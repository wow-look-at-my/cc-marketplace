package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func runCleanupBranch(cmd *cobra.Command, args []string) error {
	branchName := args[0]

	// List all tags for this branch
	prefix := branchName + "/"
	tags, err := ListTagsWithPrefix(prefix)
	if err != nil {
		return fmt.Errorf("failed to list tags: %w", err)
	}

	if len(tags) == 0 {
		fmt.Printf("No tags found for branch: %s\n", branchName)
		return nil
	}

	fmt.Printf("Found %d tags for branch %s:\n", len(tags), branchName)
	for _, tag := range tags {
		fmt.Printf("  - %s\n", tag)
	}

	// Delete remote tags first
	if err := DeleteRemoteTags(tags...); err != nil {
		return fmt.Errorf("failed to delete remote tags: %w", err)
	}

	// Delete local tags
	if err := DeleteLocalTags(tags...); err != nil {
		return fmt.Errorf("failed to delete local tags: %w", err)
	}

	fmt.Printf("Deleted %d tags for branch: %s\n", len(tags), branchName)
	return nil
}
