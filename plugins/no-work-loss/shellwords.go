package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// The vocabulary the walk is built on: what a word says, which program a
// spelling names, and where a path lands.

func wordText(wd *syntax.Word) word {
	if wd == nil {
		return word{static: true}
	}
	var b strings.Builder
	static := true
	for _, p := range wd.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, dp := range x.Parts {
				if lit, ok := dp.(*syntax.Lit); ok {
					b.WriteString(lit.Value)
					continue
				}
				static = false
			}
		default:
			static = false
		}
	}
	return word{text: b.String(), static: static}
}

// commandName resolves the spelling to the program: `\sed`, `/usr/bin/sed` and
// `"sed"` are all sed.
func commandName(t string) string {
	t = strings.TrimPrefix(t, `\`)
	if t == "" {
		return ""
	}
	return filepath.Base(t)
}

// stripWrappers peels the layers that stand between the words as written and the
// program that actually runs. Enumerating spellings of a program can never be
// finished, so this resolves instead: one rule for sed covers `sudo -E env
// FOO=1 sed`, `xargs sed` and `timeout 5 sed` alike.
func stripWrappers(argv []word) []word {
	for len(argv) > 0 {
		switch commandName(argv[0].text) {
		case "env":
			argv = argv[1:]
			for len(argv) > 0 {
				t := argv[0].text
				if t == "-u" || t == "--unset" || t == "-C" || t == "--chdir" {
					argv = dropN(argv, 2)
					continue
				}
				if strings.HasPrefix(t, "-") {
					argv = argv[1:]
					continue
				}
				if i := strings.Index(t, "="); i > 0 {
					argv = argv[1:] // a VAR=VAL prefix, not the program
					continue
				}
				break
			}
		case "sudo", "doas":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				t := argv[0].text
				if t == "-u" || t == "-g" || t == "-p" || t == "-C" || t == "--user" || t == "--group" {
					argv = dropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
			for len(argv) > 0 && strings.Contains(argv[0].text, "=") && !strings.HasPrefix(argv[0].text, "-") {
				argv = argv[1:]
			}
		case "command", "builtin", "exec", "nohup", "setsid", "stdbuf", "time":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				argv = argv[1:]
			}
		case "nice", "ionice":
			// These take a separate numeric value, so skipping flags alone would
			// leave the number where the program should be and the writer behind
			// it would never be seen.
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				if niceValueFlags.Contains(argv[0].text) {
					argv = dropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
		case "timeout":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				t := argv[0].text
				if t == "-s" || t == "-k" || t == "--signal" || t == "--kill-after" {
					argv = dropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
			argv = dropN(argv, 1) // the duration operand
		case "xargs":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				if xargsValueFlags.Contains(argv[0].text) {
					argv = dropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
		case "busybox":
			// busybox is a multi-call binary: the applet is the real program, so
			// `busybox sed -i` must reach the sed rule.
			argv = argv[1:]
		default:
			return argv
		}
	}
	return argv
}

var niceValueFlags = set.Of[string]("-n", "-c", "-p", "-P", "-u",
	"--adjustment", "--class", "--classdata", "--pid")

var xargsValueFlags = set.Of[string]("-n", "-I", "-i", "-L", "-P", "-s",
	"-d", "-E", "-a",
	"--max-args", "--replace", "--max-lines",
	"--max-procs", "--max-chars", "--delimiter",
	"--eof", "--arg-file")

func isShell(name string) bool {
	switch name {
	case "sh", "bash", "dash", "zsh", "ksh", "mksh", "ash", "busybox":
		return true
	}
	return false
}

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

func dropN(argv []word, n int) []word {
	if len(argv) <= n {
		return nil
	}
	return argv[n:]
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
