package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var cacheSnapshotCmd = &cobra.Command{
	Use:   "cache-snapshot [paths...]",
	Short: "Snapshot cache directory state to a file",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runCacheSnapshot,
}

var cacheChangedCmd = &cobra.Command{
	Use:   "cache-changed [paths...]",
	Short: "Check if cache directories changed since snapshot, print changed files",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runCacheChanged,
}

var snapshotFile string

func init() {
	rootCmd.AddCommand(cacheSnapshotCmd)
	rootCmd.AddCommand(cacheChangedCmd)

	cacheSnapshotCmd.Flags().StringVarP(&snapshotFile, "output", "o", "/tmp/cache-snapshot.txt", "Output file for snapshot")
	cacheChangedCmd.Flags().StringVarP(&snapshotFile, "snapshot", "s", "/tmp/cache-snapshot.txt", "Snapshot file to compare against")
}

type fileEntry struct {
	Path    string
	ModTime int64
	Size    int64
}

func scanCacheDirs(paths []string) ([]fileEntry, error) {
	var entries []fileEntry

	for _, root := range paths {
		root = expandHome(root)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if d.IsDir() {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}

			entries = append(entries, fileEntry{
				Path:    path,
				ModTime: info.ModTime().Unix(),
				Size:    info.Size(),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return entries, nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}


func runCacheSnapshot(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	entries, err := scanCacheDirs(args)
	if err != nil {
		return err
	}

	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s %d %d", e.Path, e.ModTime, e.Size))
	}

	return os.WriteFile(snapshotFile, []byte(strings.Join(lines, "\n")), 0644)
}

func runCacheChanged(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	// Load snapshot
	data, err := os.ReadFile(snapshotFile)
	if err != nil {
		// No snapshot = everything is new
		entries, err := scanCacheDirs(args)
		if err != nil {
			return err
		}
		for _, e := range entries {
			fmt.Printf("+ %s\n", e.Path)
		}
		return nil
	}

	oldEntries := make(map[string]fileEntry)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var e fileEntry
		fmt.Sscanf(line, "%s %d %d", &e.Path, &e.ModTime, &e.Size)
		oldEntries[e.Path] = e
	}

	// Scan current state
	currentEntries, err := scanCacheDirs(args)
	if err != nil {
		return err
	}

	// Find added/modified
	for _, e := range currentEntries {
		old, exists := oldEntries[e.Path]
		if !exists {
			fmt.Printf("+ %s\n", e.Path)
		} else if old.ModTime != e.ModTime || old.Size != e.Size {
			fmt.Printf("~ %s\n", e.Path)
		}
	}

	return nil
}
