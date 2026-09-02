package main

import (
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
	"shellwalk"
)

// The vocabulary the walk is built on: what a word says, which program a
// spelling names, and where a path lands. Everything about reading a word and
// resolving a program lives in shellwalk, shared with enhanced-auto-allow --
// a wrapper one plugin misreads is a rule the other still enforces, so the two
// must not answer "which program runs here" separately.

func wordText(wd *syntax.Word) word {
	w := shellwalk.WordText(wd)
	return word{text: w.Text, static: w.Static}
}

func commandName(t string) string { return shellwalk.CommandName(t) }

func stripWrappers(argv []word) []word {
	shared := make([]shellwalk.Word, len(argv))
	for i, a := range argv {
		shared[i] = shellwalk.Word{Text: a.text, Static: a.static}
	}
	shared = shellwalk.StripWrappers(shared)
	out := make([]word, len(shared))
	for i, a := range shared {
		out[i] = word{text: a.Text, static: a.Static}
	}
	return out
}

func isShell(name string) bool { return shellwalk.IsShell(name) }

func hasShellShebang(path string) bool {
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 128)
	n, _ := f.Read(buf)
	line := string(buf[:n])
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if !strings.HasPrefix(line, "#!") {
		return false
	}
	for _, field := range strings.Fields(line[2:]) {
		if isShell(filepath.Base(field)) {
			return true
		}
	}
	return false
}

// abs resolves an operand against the directory its command runs in. An empty
// result means the answer is not knowable, which every caller treats as deny.
func abs(cwd, p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if cwd == unknownDirText {
		return ""
	}
	return filepath.Clean(filepath.Join(cwd, p))
}

// resolveDir computes the directory a cd lands in. An operand that is not
// statically known -- `cd $DIR`, `cd -` -- yields the unknown marker, which makes
// every later write in the chain undecidable and therefore denied.
func resolveDir(cwd string, eff []word) string {
	for _, a := range eff[1:] {
		if a.text == "-" {
			return unknownDirText
		}
		if strings.HasPrefix(a.text, "-") {
			continue
		}
		if !a.static {
			return unknownDirText
		}
		return abs(cwd, a.text)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Clean(home) // bare `cd`
	}
	return unknownDirText
}
