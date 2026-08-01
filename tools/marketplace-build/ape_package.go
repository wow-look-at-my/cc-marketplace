package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A plugin that ships a binary ships exactly ONE: the GOOS=cosmo fat APE
// go-toolchain builds (`--targets cosmo --cosmo-slots none`), which runs on
// Linux, macOS and Windows from a single file. The per-platform matrix it
// replaced put four to six copies of the same program in every package.
//
// The APE cannot be the command a manifest names, though, and that is not a
// style choice: Claude Code execve()s a hook/MCP/LSP command directly, and an
// unassimilated APE is neither ELF nor a `#!` script, so the kernel answers
// ENOEXEC and the spawn fails ("undefined is not an object (evaluating
// 'this.#handle')" in the client's log). A shell CAN interpret its prologue --
// which also self-assimilates the file into a native binary on first run -- so
// what ships at the manifest's path is this launcher, with the APE beside it.

// apeSuffix is what go-toolchain names a cosmo build.
const apeSuffix = "_cosmo_fat"

// launcherScript is written at build/<name>, the path every plugin manifest
// already points at (hooks, .mcp.json and .lsp.json alike), so switching to
// the APE needs no manifest change anywhere.
//
// `exec` matters: the launcher must not linger as a parent process, because an
// LSP client tracks the pid it spawned and an MCP server's stdio must be the
// binary's own. $0 is followed rather than assumed so a plugin cache directory
// with spaces in its path still works.
const launcherScript = `#!/bin/sh
# Claude Code execve()s this path directly. A fat APE is not ELF and has no
# shebang, so exec'ing it straight is ENOEXEC -- a shell has to interpret its
# prologue (which also self-assimilates it into a native binary on first run).
exec "$(dirname "$0")/%s%s" "$@"
`

// apeName is the on-disk name of the shipped APE, beside its launcher.
func apeName(pluginName string) string { return pluginName + ".ape" }

// stageBinaries rewrites a cooked plugin's build/ directory into the shipping
// layout: the fat APE plus its launcher, and nothing else. Plugins that ship no
// binary (a skills- or hooks-script-only plugin) are left untouched.
//
// It fails closed when a build/ directory exists but holds no APE: that means
// the plugin was built with the per-platform matrix instead of `--targets
// cosmo`, and silently shipping those binaries would restore the six-copy
// packages this replaced -- on some platforms only, which is worse than a
// clean failure.
func stageBinaries(cookedDir, pluginName string) error {
	buildDir := filepath.Join(cookedDir, "build")
	entries, err := os.ReadDir(buildDir)
	if os.IsNotExist(err) {
		return nil // no binaries in this plugin
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", buildDir, err)
	}

	ape := ""
	var others []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch {
		case strings.HasSuffix(e.Name(), apeSuffix):
			ape = e.Name()
		default:
			others = append(others, e.Name())
		}
	}
	if ape == "" {
		return fmt.Errorf("no %s* binary in %s (found: %s) -- build the plugin with `--targets cosmo --cosmo-slots none` so it ships one fat APE",
			pluginName+apeSuffix, buildDir, strings.Join(others, ", "))
	}

	// Everything else in build/ is a byproduct: per-platform slot copies, the
	// debug sidecar, checksums, the profile. None of it ships.
	for _, name := range others {
		if err := os.Remove(filepath.Join(buildDir, name)); err != nil {
			return fmt.Errorf("drop build byproduct %s: %w", name, err)
		}
	}

	if err := os.Rename(filepath.Join(buildDir, ape), filepath.Join(buildDir, apeName(pluginName))); err != nil {
		return fmt.Errorf("stage APE: %w", err)
	}
	if err := os.Chmod(filepath.Join(buildDir, apeName(pluginName)), 0o755); err != nil {
		return fmt.Errorf("chmod APE: %w", err)
	}

	launcher := filepath.Join(buildDir, pluginName)
	body := fmt.Sprintf(launcherScript, pluginName, ".ape")
	if err := os.WriteFile(launcher, []byte(body), 0o755); err != nil {
		return fmt.Errorf("write launcher: %w", err)
	}
	return nil
}

// hasBinary reports whether a cooked plugin ships a binary at all, so callers
// can tell "no binaries to stage" apart from "the build produced nothing".
func hasBinary(cookedDir string) bool {
	entries, err := os.ReadDir(filepath.Join(cookedDir, "build"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
	}
	return false
}

// executableFiles lists the files under dir that carry the executable bit.
// git preserves mode 0755 in a tag's tree and the plugin installer copies it
// through, so this is what makes the shipped launcher runnable on the far side.
func executableFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&0o111 != 0 {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out, err
}
