package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeBuildDir lays out a cooked plugin's build/ with the given files, each
// mode 0755 like a real build output.
func writeBuildDir(t *testing.T, names ...string) string {
	t.Helper()
	cooked := t.TempDir()
	build := filepath.Join(cooked, "build")
	require.NoError(t, os.MkdirAll(build, 0o755))
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(build, n), []byte("binary:"+n), 0o755))
	}
	return cooked
}

func buildEntries(t *testing.T, cooked string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cooked, "build"))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestApeName(t *testing.T) {
	require.Equal(t, "glob.ape", apeName("glob"))
}

// The shipping layout is exactly two files: the APE under its stable name, and
// the launcher at the path every manifest already points at.
func TestStageBinariesShipsTheApeAndItsLauncher(t *testing.T) {
	cooked := writeBuildDir(t, "glob"+apeSuffix)
	require.NoError(t, stageBinaries(cooked, "glob"))

	require.ElementsMatch(t, []string{"glob.ape", "glob"}, buildEntries(t, cooked))

	launcher, err := os.ReadFile(filepath.Join(cooked, "build", "glob"))
	require.NoError(t, err)
	body := string(launcher)
	require.True(t, strings.HasPrefix(body, "#!/bin/sh\n"),
		"Claude Code execve()s this path, so it must be a script the kernel accepts")
	require.Contains(t, body, `exec "$(dirname "$0")/glob.ape" "$@"`,
		"exec so the launcher does not linger as a parent, and $0-relative so a cache path with spaces still works")

	for _, name := range []string{"glob", "glob.ape"} {
		info, err := os.Stat(filepath.Join(cooked, "build", name))
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&0o111, "%s must be executable on the far side", name)
	}
}

// Everything else a cosmo build leaves behind (slot copies, the debug sidecar,
// checksums) is a byproduct; shipping it would restore the multi-copy packages
// the APE replaced.
func TestStageBinariesDropsBuildByproducts(t *testing.T) {
	cooked := writeBuildDir(t,
		"glob"+apeSuffix,
		"glob_linux_amd64",
		"glob_darwin_arm64",
		"glob.debug",
		"checksums.txt",
	)
	require.NoError(t, stageBinaries(cooked, "glob"))
	require.ElementsMatch(t, []string{"glob.ape", "glob"}, buildEntries(t, cooked))
}

// The REAL layout `go-toolchain matrix --targets cosmo` writes, copied from a
// live build: the fat APE's own name is a SYMLINK into a per-platform slot copy
// (buildhost rejects os=cosmo, so the fat build is published under a platform
// name), and the byproducts include a debug sidecar and an aarch64 ELF. Staging
// this by renaming the link and deleting the slots ships a dangling symlink --
// which is what the invented single-regular-file fixture above could never
// catch.
func TestStageBinariesFollowsTheSymlinkGoToolchainActuallyWrites(t *testing.T) {
	cooked := writeBuildDir(t,
		"glob_linux_amd64",
		"glob_linux_arm64",
		"glob_windows_amd64.exe",
		"glob"+apeSuffix+".aarch64.elf",
		"glob"+apeSuffix+".dbg",
		"checksums.txt",
		"profile.json",
	)
	build := filepath.Join(cooked, "build")
	require.NoError(t, os.WriteFile(filepath.Join(build, "glob_linux_amd64"), []byte("REAL APE BYTES"), 0o755))
	// go-toolchain links both the bare name and the fat name at the slot copy.
	require.NoError(t, os.Symlink("glob_linux_amd64", filepath.Join(build, "glob"+apeSuffix)))
	require.NoError(t, os.Symlink("glob_linux_amd64", filepath.Join(build, "glob_host")))

	require.NoError(t, stageBinaries(cooked, "glob"))
	require.ElementsMatch(t, []string{"glob.ape", "glob"}, buildEntries(t, cooked))

	shipped, err := os.ReadFile(filepath.Join(build, "glob.ape"))
	require.NoError(t, err)
	require.Equal(t, "REAL APE BYTES", string(shipped), "the shipped APE must be bytes, not a link to a file that was deleted")

	info, err := os.Lstat(filepath.Join(build, "glob.ape"))
	require.NoError(t, err)
	require.Zero(t, info.Mode()&os.ModeSymlink, "a tag holding a dangling symlink installs as a broken plugin")
	require.NotZero(t, info.Mode()&0o111)
}

// The failure this fails closed on: a plugin built with the per-platform matrix
// instead of `--targets cosmo`. Shipping those binaries would work on some
// platforms and silently not others, which is worse than a clean failure.
func TestStageBinariesFailsClosedWithoutAnApe(t *testing.T) {
	cooked := writeBuildDir(t, "glob_linux_amd64", "glob_darwin_arm64")
	err := stageBinaries(cooked, "glob")
	require.Error(t, err)
	require.Contains(t, err.Error(), "glob"+apeSuffix, "the message must name what was expected")
	require.Contains(t, err.Error(), "glob_linux_amd64", "and what was found instead")
	require.Contains(t, err.Error(), "--targets cosmo", "and the fix")
}

// A skills- or hooks-script-only plugin has no build/ at all and must pass
// through untouched -- distinct from a build that produced nothing.
func TestStageBinariesIgnoresAPluginWithNoBuildDir(t *testing.T) {
	cooked := t.TempDir()
	require.NoError(t, stageBinaries(cooked, "css-cascade"))

	_, err := os.Stat(filepath.Join(cooked, "build"))
	require.True(t, os.IsNotExist(err), "staging must not create a build dir that was never there")
}

// An empty build/ has no APE, so it is the fail-closed case, not the
// nothing-to-do case.
func TestStageBinariesRejectsAnEmptyBuildDir(t *testing.T) {
	cooked := writeBuildDir(t)
	require.Error(t, stageBinaries(cooked, "glob"))
}

// Directories under build/ are not binaries and must not be mistaken for the
// byproducts the stager deletes (os.Remove would fail on a non-empty one).
func TestStageBinariesSkipsSubdirectories(t *testing.T) {
	cooked := writeBuildDir(t, "glob"+apeSuffix)
	nested := filepath.Join(cooked, "build", "cache", "deep")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "junk"), []byte("x"), 0o644))

	require.NoError(t, stageBinaries(cooked, "glob"))
	require.ElementsMatch(t, []string{"glob.ape", "glob", "cache"}, buildEntries(t, cooked))
}

func TestStageBinariesReportsAnUnreadableBuildDir(t *testing.T) {
	cooked := t.TempDir()
	// A FILE named build/ is neither "no binaries" nor a readable directory.
	require.NoError(t, os.WriteFile(filepath.Join(cooked, "build"), []byte("not a dir"), 0o644))

	err := stageBinaries(cooked, "glob")
	require.Error(t, err)
	require.Contains(t, err.Error(), "read ")
}

func TestHasBinary(t *testing.T) {
	require.True(t, hasBinary(writeBuildDir(t, "glob"+apeSuffix)))
	require.False(t, hasBinary(writeBuildDir(t)), "an empty build dir ships no binary")
	require.False(t, hasBinary(t.TempDir()), "no build dir at all ships no binary")

	onlyDirs := writeBuildDir(t)
	require.NoError(t, os.MkdirAll(filepath.Join(onlyDirs, "build", "cache"), 0o755))
	require.False(t, hasBinary(onlyDirs), "a directory is not a binary")
}

// git preserves mode 0755 in a tag's tree and the installer copies it through,
// so this list is what decides whether the shipped launcher runs at all.
func TestExecutableFilesListsOnlyExecutableFilesByRelativeSlashPath(t *testing.T) {
	cooked := writeBuildDir(t, "glob"+apeSuffix)
	require.NoError(t, stageBinaries(cooked, "glob"))
	require.NoError(t, os.WriteFile(filepath.Join(cooked, "README.md"), []byte("docs"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(cooked, "hooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cooked, "hooks", "on-prompt.sh"), []byte("#!/bin/sh\n"), 0o755))

	got, err := executableFiles(cooked)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"build/glob", "build/glob.ape", "hooks/on-prompt.sh"}, got)
}

func TestExecutableFilesReportsAMissingDir(t *testing.T) {
	_, err := executableFiles(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}
