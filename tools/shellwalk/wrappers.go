package shellwalk

import (
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
)

// StripWrappers peels the layers that stand between the words as written and
// the program that actually runs, so one rule for sed covers `sudo -E env
// FOO=1 sed`, `xargs sed` and `timeout 5 sed` alike. Enumerating spellings of a
// program can never be finished; this resolves instead.
//
// A wrapper that takes its own VALUE flag has that flag's value dropped with
// it. Without that, `nice -n 10 sed -i f` leaves `10` where the program should
// be and the sed behind it is never seen at all -- the same hole `timeout 5
// python` opens, one operand instead of one flag value.
func StripWrappers(argv []Word) []Word {
	for len(argv) > 0 {
		switch CommandName(argv[0].Text) {
		case "env":
			argv = argv[1:]
			for len(argv) > 0 {
				t := argv[0].Text
				if envValueFlags.Contains(t) {
					argv = DropN(argv, 2)
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
			for len(argv) > 0 && strings.HasPrefix(argv[0].Text, "-") {
				if sudoValueFlags.Contains(argv[0].Text) {
					argv = DropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
			for len(argv) > 0 && strings.Contains(argv[0].Text, "=") && !strings.HasPrefix(argv[0].Text, "-") {
				argv = argv[1:]
			}
		case "command", "builtin", "exec", "nohup", "setsid", "stdbuf", "time", "script", "watch":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].Text, "-") {
				argv = argv[1:]
			}
		case "nice", "ionice":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].Text, "-") {
				if niceValueFlags.Contains(argv[0].Text) {
					argv = DropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
		case "timeout":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].Text, "-") {
				if timeoutValueFlags.Contains(argv[0].Text) {
					argv = DropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
			argv = DropN(argv, 1) // the duration operand
		case "xargs":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].Text, "-") {
				if xargsValueFlags.Contains(argv[0].Text) {
					argv = DropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
		case "busybox":
			// A multi-call binary: the applet is the real program, so
			// `busybox sed -i` must reach the sed rule.
			argv = argv[1:]
		case "uv", "uvx", "pipx", "poetry", "hatch", "pdm", "conda", "rye", "micromamba", "npx", "bunx":
			// A package runner: its own flags, then an optional `run`/`exec`
			// subcommand, then the program.
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].Text, "-") {
				argv = argv[1:]
			}
			if len(argv) > 0 && runnerSubcommands.Contains(argv[0].Text) {
				argv = argv[1:]
				for len(argv) > 0 && strings.HasPrefix(argv[0].Text, "-") {
					argv = argv[1:]
				}
			}
		default:
			return argv
		}
	}
	return argv
}

// ResolveProgram names the program a command runs, and its own arguments.
// An empty name means nothing runs -- the words were all wrapper.
func ResolveProgram(argv []Word) (string, []Word) {
	eff := StripWrappers(argv)
	if len(eff) == 0 {
		return "", nil
	}
	return CommandName(eff[0].Text), eff[1:]
}

// DropN drops the first n words, returning nil rather than a short slice.
func DropN(argv []Word, n int) []Word {
	if len(argv) <= n {
		return nil
	}
	return argv[n:]
}

var envValueFlags = set.Of[string]("-u", "--unset", "-C", "--chdir")

var sudoValueFlags = set.Of[string]("-u", "-g", "-p", "-C", "--user", "--group")

var niceValueFlags = set.Of[string]("-n", "-c", "-p", "-P", "-u",
	"--adjustment", "--class", "--classdata", "--pid")

var timeoutValueFlags = set.Of[string]("-s", "-k", "--signal", "--kill-after")

var xargsValueFlags = set.Of[string]("-n", "-I", "-i", "-L", "-P", "-s",
	"-d", "-E", "-a",
	"--max-args", "--replace", "--max-lines",
	"--max-procs", "--max-chars", "--delimiter",
	"--eof", "--arg-file")

var runnerSubcommands = set.Of[string]("run", "exec")
