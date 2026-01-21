package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func runCleanupBranch(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	branchName := args[0]

	// With new naming scheme, branch marketplace tag is marketplace/{branch}
	marketplaceTag := fmt.Sprintf("marketplace/%s", branchName)

	// Check if the tag exists
	tags, err := ListTagsWithPrefix("marketplace/")
	if err != nil {
		return fmt.Errorf("failed to list tags: %w", err)
	}

	var toDelete []string
	for _, tag := range tags {
		if tag == marketplaceTag {
			toDelete = append(toDelete, tag)
			break
		}
	}

	if len(toDelete) == 0 {
		fmt.Printf("No marketplace tag found for branch: %s\n", branchName)
		return nil
	}

	fmt.Printf("Deleting marketplace tag for branch %s: %s\n", branchName, marketplaceTag)

	// Delete remote tag first
	if err := DeleteRemoteTags(toDelete...); err != nil {
		return fmt.Errorf("failed to delete remote tag: %w", err)
	}

	// Delete local tag
	if err := DeleteLocalTags(toDelete...); err != nil {
		return fmt.Errorf("failed to delete local tag: %w", err)
	}

	fmt.Printf("Deleted marketplace tag: %s\n", marketplaceTag)
	return nil
}
