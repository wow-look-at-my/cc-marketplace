package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

func isRust(path string) bool {
	return strings.HasSuffix(path, ".rs")
}

// formatFile rewrites path in place with the rustfmt output. It feeds the file
// on stdin, so rustfmt does not follow `mod` declarations into other files.
// The working directory is the file's directory, so rustfmt finds the
// project's rustfmt.toml and rust-toolchain.toml.
func formatFile(path string) (bool, error) {
	bin, err := exec.LookPath(rustfmtBin())
	if err != nil {
		return false, errors.New("rustfmt is not on PATH. Install it with `rustup component add rustfmt`")
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	args := []string{"--emit", "stdout"}
	if ed := cargoEdition(filepath.Dir(path)); ed != "" {
		args = append(args, "--edition", ed)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = filepath.Dir(path)
	cmd.Stdin = bytes.NewReader(src)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("%v\n%s", err, strings.TrimSpace(errOut.String()))
	}
	if bytes.Equal(out.Bytes(), src) {
		return false, nil
	}
	if err := os.WriteFile(path, out.Bytes(), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

// rustfmtBin lets a test point the hook at a stub.
func rustfmtBin() string {
	if b := os.Getenv("RUSTFMT_ENFORCE_BIN"); b != "" {
		return b
	}
	return "rustfmt"
}

// cargoEdition gives the edition that `cargo fmt` passes for a file in dir.
// It returns "" outside a Cargo package, and rustfmt then reads the edition
// from rustfmt.toml or uses its own default.
func cargoEdition(dir string) string {
	inherit := false
	for d := dir; ; d = filepath.Dir(d) {
		manifest := readManifest(filepath.Join(d, "Cargo.toml"))
		if manifest != nil {
			if !inherit {
				if pkg, ok := manifest["package"].(map[string]any); ok {
					switch ed := pkg["edition"].(type) {
					case string:
						return ed
					case map[string]any:
						inherit = ed["workspace"] == true
					case nil:
						return "2015"
					}
				}
			}
			if ws, ok := manifest["workspace"].(map[string]any); ok && inherit {
				if pkg, ok := ws["package"].(map[string]any); ok {
					if ed, ok := pkg["edition"].(string); ok {
						return ed
					}
				}
			}
		}
		if parent := filepath.Dir(d); parent == d {
			return ""
		}
	}
}

func readManifest(path string) map[string]any {
	var m map[string]any
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil
	}
	return m
}
