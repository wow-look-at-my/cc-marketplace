package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"os"
	"path/filepath"
	"strings"
)

// An interpreter handed a script writes whatever the script says, and the script
// is not something this hook can resolve. Two shapes follow from that: an inline
// script denies wherever it runs, because its targets are unknowable; a script
// FILE is followed when it is shell (see segment.go) and judged by where it
// lives when it is not.

// evalFlags names, per interpreter, the flags that hand it a program rather than
// a file. The set is per-tool because the spelling is: ruby's -E sets an
// encoding while perl's runs code.
var evalFlags = map[string][]string{
	"node":      {"-e", "--eval", "-p", "--print"},
	"nodejs":    {"-e", "--eval", "-p", "--print"},
	"bun":       {"-e", "--eval", "-p", "--print"},
	"deno":      {"--eval"},
	"ruby":      {"-e"},
	"perl":      {"-e", "-E"},
	"python":    {"-c"},
	"php":       {"-r"},
	"lua":       {"-e"},
	"luajit":    {"-e"},
	"tclsh":     {},
	"rscript":   {"-e"},
	"osascript": {"-e"},
	"ts-node":   {"-e"},
	"tsx":       {"-e"},
	"jq":        nil, // jq has no way to write a file; listed so nobody adds one
}

// Editors exist to rewrite the file they open, so they are writers by default
// rather than by flag.
var editors = set.Of[string]("ed", "ex", "vi", "vim", "nvim",
	"emacs", "emacsclient")

func interpreterWrites(seg segment, name string, rest []word, roots []string) ([]write, bool) {
	name = stripVersion(name)
	if editors.Contains(name) {
		return editorWrites(seg, name, rest), true
	}
	flags, ok := evalFlags[name]
	if !ok {
		return nil, false
	}
	if flags == nil {
		return nil, true // recognised and unable to write
	}
	for _, a := range rest {
		for _, f := range flags {
			if a.text == f || strings.HasPrefix(a.text, f+"=") {
				return []write{{route: name + " " + f, opaque: "an inline " + name + " script; the files it writes are named in code this hook cannot resolve"}}, true
			}
		}
		// `deno eval` and the like put the program behind a subcommand.
		if a.text == "eval" && name == "deno" {
			return []write{{route: "deno eval", opaque: "an inline deno script; the files it writes are named in code this hook cannot resolve"}}, true
		}
		if a.text == "-" {
			return []write{{route: name + " -", opaque: "a " + name + " script read from stdin, which is not in the command text"}}, true
		}
	}
	if seg.stdinScript {
		return []write{{route: name + " (stdin)", opaque: "a " + name + " script piped in on stdin, which is not in the command text"}}, true
	}
	return scratchScriptWrites(seg, name, rest, roots), true
}

// scratchScriptWrites closes the write-elsewhere-then-run route: the Write tool
// aimed at /tmp is allowed, so running that file is the half that puts its
// content into the tree. A script that lives inside the tree is not this -- it
// got there through Write or Edit and is visible in the diff -- and a system
// script is the tool it belongs to, so only a scratch script denies.
func scratchScriptWrites(seg segment, name string, rest []word, roots []string) []write {
	_, operands := scanArgs(rest, nil)
	for _, o := range operands {
		if !o.static {
			return []write{{route: name, opaque: "a " + name + " script path built from an expansion, so what it runs is not in the command text"}}
		}
		p := abs(seg.cwd, o.text)
		if _, guarded := insideGuarded(roots, p); guarded {
			break // a script in the tree got there through Write or Edit
		}
		if p != "" && isScratchPath(p) {
			return []write{{route: name + " " + o.text, opaque: "a " + name + " script under a temporary directory; write the file with Write or Edit instead of generating it from a scratch script"}}
		}
		break // the first operand is the script; the rest are its arguments
	}
	return nil
}

func editorWrites(seg segment, name string, rest []word) []write {
	// -s is silent/script mode for ed, ex and vim and takes no value; reading it
	// as one swallows the file operand and the write disappears.
	valueFlags := map[string]bool{"-c": true, "--command": true, "--eval": true, "-u": true, "-i": true, "--load": true}
	flags, operands := scanArgs(rest, valueFlags)
	if len(operands) > 0 {
		return []write{{route: name, paths: operands, dir: seg.cwd}}
	}
	// No file operand and a script to run means the target is named inside the
	// script. No file and no script means the invocation edits nothing at all --
	// `emacs --version` is not an editing session.
	for _, f := range []string{"-c", "--command", "--eval", "--batch", "--script", "-s"} {
		if _, ok := flags[f]; ok {
			return []write{{route: name, opaque: "an " + name + " session whose target file is named in its script rather than on the command line"}}
		}
	}
	if seg.stdinScript {
		return []write{{route: name, opaque: "an " + name + " session driven from stdin, so its target file is not in the command text"}}
	}
	return nil
}

// scratch directories: the places a file can appear without any review.
func isScratchPath(p string) bool {
	dirs := []string{"/tmp", "/var/tmp", "/dev/shm"}
	if t := os.Getenv("TMPDIR"); t != "" {
		dirs = append(dirs, filepath.Clean(t))
	}
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// stripVersion turns `perl5.36` and `python3.11` into the tool, so a rule is
// written once instead of once per installed version.
func stripVersion(name string) string {
	trimmed := strings.TrimRight(name, "0123456789.")
	if trimmed == "" {
		return strings.ToLower(name)
	}
	return strings.ToLower(trimmed)
}

// allowedFormatter is the explicit decision about the tools that rewrite files
// by design. Each one below writes only a canonical reformat, or a regeneration
// the repository owns, of the file it is handed -- none can be pointed at
// content the model authored, which is what separates them from `sed -i`.
//
// A tool that is NOT on this list does not become allowed by being a formatter:
// an unrecognised in-place rewrite denies (see inPlaceRewrite), and the way to
// run one is through a named recipe -- `just fmt`, `make fmt`, `npm run format`
// -- which is a reviewable line in the repository rather than an argv.
func allowedFormatter(name string, rest []word) bool {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0].text
	}
	switch stripVersion(name) {
	case "gofmt", "goimports", "shfmt", "rustfmt", "clang-format", "prettier", "dprint", "biome":
		return true
	case "go-toolchain":
		// The org's Go entry point: it tidies go.mod, formats source and runs the
		// tests as one unit, and refusing its rewrites would refuse every Go build.
		return true
	case "go":
		return sub == "generate" || sub == "fmt" || sub == "mod"
	case "cargo":
		return sub == "fmt"
	case "terraform":
		return sub == "fmt"
	}
	return false
}
