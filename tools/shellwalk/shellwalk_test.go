package shellwalk

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

// argv parses one command and renders its words, so a case reads as the command
// a session would actually type.
func argv(t *testing.T, command string) []Word {
	t.Helper()
	f, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)
	require.Len(t, f.Stmts, 1)
	call, ok := f.Stmts[0].Cmd.(*syntax.CallExpr)
	require.True(t, ok, "not a simple command: %q", command)
	return Words(call.Args)
}

func program(t *testing.T, command string) string {
	t.Helper()
	name, _ := ResolveProgram(argv(t, command))
	return name
}

func TestAWordKeepsItsTextAndSaysWhenItIsNotTheWholeStory(t *testing.T) {
	w := argv(t, `echo "py"thon3`)[1]
	assert.Equal(t, "python3", w.Text)
	assert.True(t, w.Static)

	w = argv(t, `echo "$HOME/x"`)[1]
	assert.False(t, w.Static, "a parameter can expand to anything")
	assert.Equal(t, "/x", w.Text, "the literal parts are still readable")
}

func TestASpellingResolvesToTheProgramItNames(t *testing.T) {
	for _, spelling := range []string{`sed -i f`, `\sed -i f`, `/usr/bin/sed -i f`, `"sed" -i f`} {
		assert.Equal(t, "sed", program(t, spelling), spelling)
	}
}

// Each of these left the wrapper's own operand where the program should be
// before the two plugins shared one implementation.
func TestAWrapperValueNeverStandsInForTheProgram(t *testing.T) {
	cases := map[string]string{
		"env FOO=1 python3 x.py":       "python3",
		"env -u PATH python3 x.py":     "python3",
		"sudo -E python3 x.py":         "python3",
		"sudo -u root python3 x.py":    "python3",
		"nice -n 10 sed -i f":          "sed",
		"ionice -c 2 -n 7 sed -i f":    "sed",
		"timeout 5 python3 x.py":       "python3",
		"timeout -k 1 5 python3 x.py":  "python3",
		"xargs -n 1 sed -i f":          "sed",
		"xargs -I {} sed -i {}":        "sed",
		"busybox sed -i f":             "sed",
		"uv run python x.py":           "python",
		"npx prettier --write f":       "prettier",
		"command command echo hi":      "echo",
		"sudo -E env FOO=1 xargs sed":  "sed",
		"nohup setsid stdbuf -o0 perl": "perl",
	}
	for command, want := range cases {
		assert.Equal(t, want, program(t, command), command)
	}
}

func TestAllWrapperAndNothingElseNamesNoProgram(t *testing.T) {
	name, rest := ResolveProgram(argv(t, "sudo -E env"))
	assert.Empty(t, name)
	assert.Empty(t, rest)
}

func TestATrailingVersionIsTheSameProgram(t *testing.T) {
	assert.True(t, MatchesProgram("python", "python"))
	assert.True(t, MatchesProgram("python3", "python"))
	assert.True(t, MatchesProgram("python3.11", "python"))
	assert.False(t, MatchesProgram("pythonista", "python"))
	assert.False(t, MatchesProgram("py", "python"))
}

func TestStdinIsAProgramOnlyWhenNothingElseNamedOne(t *testing.T) {
	assert.True(t, NamesAScript(argv(t, "node hook.ts")[1:]), "the script is right there in the argv")
	assert.True(t, NamesAScript(argv(t, "node --experimental-x hook.ts")[1:]))
	assert.True(t, NamesAScript(argv(t, "node -- hook.ts")[1:]))

	assert.False(t, NamesAScript(argv(t, "node")[1:]))
	assert.False(t, NamesAScript(argv(t, "node -")[1:]), "a stdin marker names stdin, not a script")
	assert.False(t, NamesAScript(argv(t, "node /dev/stdin")[1:]))
	assert.False(t, NamesAScript(argv(t, "node --experimental-x")[1:]))
}

func TestASyntaxCheckIsNotARun(t *testing.T) {
	assert.True(t, ShellNoExec(argv(t, "bash -n deploy.sh")))
	assert.True(t, ShellNoExec(argv(t, "bash --noexec deploy.sh")))
	assert.True(t, ShellNoExec(argv(t, "bash -nx deploy.sh")))

	assert.False(t, ShellNoExec(argv(t, "bash deploy.sh")))
	assert.False(t, ShellNoExec(argv(t, "bash -x deploy.sh")))
	assert.False(t, ShellNoExec(argv(t, "bash -c 'echo hi'")))
	assert.False(t, ShellNoExec(argv(t, "bash -- -n")), "an operand named -n is a file")
}

func TestAShellIsRecognisedByName(t *testing.T) {
	assert.True(t, IsShell("bash"))
	assert.True(t, IsShell("dash"))
	assert.False(t, IsShell("node"))
}
