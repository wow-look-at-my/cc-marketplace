package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A plugin that ships a binary ships exactly ONE: the single file `--targets
// cosmo` builds, which runs on Linux, macOS and Windows.
//
// That file cannot be the command a manifest names. Claude Code execve()s a
// hook/MCP/LSP command directly, the kernel answers ENOEXEC, and the spawn
// fails ("undefined is not an object (evaluating 'this.#handle')" in the
// client's log). A shell can run it, so the manifest's path holds the launcher
// below and the binary sits beside it.

// apeMagic is the build output's first eight bytes.
//
// The file is identified by these bytes, NEVER by filename: build/ may hold it
// under a _cosmo_fat name, under a symlink of that name, or only under
// <name>_linux_amd64, and all three are byte-identical. This is also what keeps
// the fail-closed check honest -- a plugin built with the per-platform matrix
// carries ELF/PE binaries in those same names and no magic anywhere, so it
// fails rather than shipping a package that runs on some platforms only.
const apeMagic = "MZqFpD='"

// apeSuffix names the fat build before go-toolchain copies it into the slots.
// Only used to prefer it when several files carry the magic, and to name it in
// the failure message.
const apeSuffix = "_cosmo_fat"

// launcherScript is written at build/<name>, the path every plugin manifest
// already points at (hooks, .mcp.json and .lsp.json alike).
//
// `exec` matters: the launcher must not linger as a parent process, because an
// LSP client tracks the pid it spawned and an MCP server's stdio must be the
// binary's own. $0 is followed rather than assumed so a plugin cache directory
// with spaces in its path still works.
const launcherScript = `#!/bin/sh
# Claude Code execve()s this path, and the binary beside it is not ELF and has
# no shebang, so exec'ing it straight is ENOEXEC. A shell can run it.
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
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
		// Prefer the fat name when it survived, but any file carrying the
		// prologue is the same bytes.
		if isAPE(filepath.Join(buildDir, e.Name())) &&
			(ape == "" || strings.HasSuffix(e.Name(), apeSuffix)) {
			ape = e.Name()
		}
	}
	if ape == "" {
		return fmt.Errorf("no fat APE in %s: none of [%s] starts with the %q prologue -- build the plugin with `--targets cosmo`",
			buildDir, strings.Join(names, ", "), apeMagic)
	}

	// Read the bytes BEFORE deleting anything, and read through the name: the
	// chosen entry may be a symlink into a slot copy that is about to go.
	data, err := os.ReadFile(filepath.Join(buildDir, ape))
	if err != nil {
		return fmt.Errorf("read APE %s: %w", ape, err)
	}

	// Everything in build/ is a byproduct: the slot copies (identical to the
	// APE), the debug sidecar, the aarch64 ELF, checksums, the profile. The
	// chosen entry goes too -- it is rewritten under the staged name below.
	for _, name := range names {
		if err := os.Remove(filepath.Join(buildDir, name)); err != nil {
			return fmt.Errorf("drop build byproduct %s: %w", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(buildDir, apeName(pluginName)), data, 0o755); err != nil {
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

// isAPE reports whether the file at path begins with the APE prologue. A
// read failure is not an APE -- the caller's fail-closed path covers it.
func isAPE(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	head := make([]byte, len(apeMagic))
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	return string(head) == apeMagic
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
