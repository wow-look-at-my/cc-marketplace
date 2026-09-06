package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func payload(t *testing.T, event, tool string, input map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	out, err := json.Marshal(map[string]any{
		"hook_event_name": event,
		"tool_name":       tool,
		"tool_input":      json.RawMessage(raw),
	})
	require.NoError(t, err)
	return string(out)
}

func decision(t *testing.T, out string) (string, string) {
	t.Helper()
	if out == "" {
		return "", ""
	}
	var res response
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res.HookSpecificOutput.PermissionDecision, res.HookSpecificOutput.PermissionDecisionReason
}

// strippedWrite is the decoded shape of an allow-with-updatedInput response.
type strippedWrite struct {
	toolInput map[string]any
	context   string
}

// stripResult decodes out as a strip response, or returns nil when out is not
// one (an empty allow, or a deny).
func stripResult(t *testing.T, out string) *strippedWrite {
	t.Helper()
	if out == "" {
		return nil
	}
	var res response
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	if len(res.HookSpecificOutput.UpdatedInput) == 0 {
		return nil
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal(res.HookSpecificOutput.UpdatedInput, &m))
	return &strippedWrite{toolInput: m, context: res.HookSpecificOutput.AdditionalContext}
}

// Each write shape carries the same tombstone on its own comment-only line, so
// each must be stripped and allowed rather than denied, and each is paired
// with the same text in a file this plugin does not judge, so a strip cannot
// come from somewhere else.
func TestEveryWriteShapeStripsTheTombstoneLine(t *testing.T) {
	const tomb = "// the old flag was removed here\nfunc f() {}"
	const clean = "func f() {}"
	shapes := map[string]map[string]any{
		"Write": {"file_path": "/repo/a.go", "content": tomb},
		"Edit":  {"file_path": "/repo/a.go", "new_string": tomb},
		"MultiEdit": {"file_path": "/repo/a.go", "edits": []map[string]any{
			{"new_string": "func g() {}"}, {"new_string": tomb},
		}},
	}
	fields := map[string]string{"Write": "content", "Edit": "new_string"}
	for tool, input := range shapes {
		t.Run(tool, func(t *testing.T) {
			s := stripResult(t, run(strings.NewReader(payload(t, "PreToolUse", tool, input))))
			require.NotNil(t, s, "a comment-only tombstone line must be stripped, not denied")
			assert.Contains(t, s.context, "the old flag was removed here")
			if field, ok := fields[tool]; ok {
				assert.Equal(t, clean, s.toolInput[field])
			} else {
				edits := s.toolInput["edits"].([]any)
				require.Len(t, edits, 2)
				assert.Equal(t, "func g() {}", edits[0].(map[string]any)["new_string"], "an edit with no finding must be untouched")
				assert.Equal(t, clean, edits[1].(map[string]any)["new_string"])
			}

			control := map[string]any{}
			for k, v := range input {
				control[k] = v
			}
			control["file_path"] = "/repo/a.xyzzy"
			out := run(strings.NewReader(payload(t, "PreToolUse", tool, control)))
			assert.Empty(t, out, "the same text in an unjudged file must pass")
		})
	}
}

// A trailing comment on a code line shares its raw source line with real code,
// so deleting the line would delete the code too. This is the negative
// control the corruption precedent warns about: no clean line boundary means
// no strip, deny instead.
func TestATrailingCommentOnACodeLineIsDeniedNotStripped(t *testing.T) {
	verdict, reason := decision(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/a.go", "content": "doStuff() // the old flag was removed here"}))))
	assert.Equal(t, "deny", verdict)
	assert.Contains(t, reason, "a former state")
}

// MultiEdit's edits are independent units: stripping one edit's tombstone
// line must never touch a sibling edit's text, and every other field on the
// stripped edit (old_string, replace_all) must survive untouched.
func TestMultiEditStripsOnlyTheOffendingEditsOwnLine(t *testing.T) {
	input := map[string]any{
		"file_path": "/repo/a.go",
		"edits": []map[string]any{
			{"old_string": "a", "new_string": "func clean() {}"},
			{"old_string": "b", "new_string": "// the old flag was removed here\nfunc g() {}", "replace_all": true},
		},
	}
	s := stripResult(t, run(strings.NewReader(payload(t, "PreToolUse", "MultiEdit", input))))
	require.NotNil(t, s)
	edits := s.toolInput["edits"].([]any)
	require.Len(t, edits, 2)
	first := edits[0].(map[string]any)
	assert.Equal(t, "func clean() {}", first["new_string"], "an edit with no finding must be untouched")
	second := edits[1].(map[string]any)
	assert.Equal(t, "func g() {}", second["new_string"])
	assert.Equal(t, "b", second["old_string"], "fields besides new_string must survive the strip")
	assert.Equal(t, true, second["replace_all"])
}

// An interior line of a multi-line block comment is entirely comment content
// by construction, so it strips cleanly and leaves its neighbors alone. The
// closing line is a different physical line: when code shares it with the
// `*/`, that line is not a clean boundary and the write is denied instead.
func TestBlockCommentInteriorLineStripsButASharedClosingLineDenies(t *testing.T) {
	s := stripResult(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/a.go",
			"content": "/* keep this\nthe old dispatch spine is replaced\nkeep this too */\nfunc f() {}"}))))
	require.NotNil(t, s)
	assert.Equal(t, "/* keep this\nkeep this too */\nfunc f() {}", s.toolInput["content"])

	verdict, _ := decision(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/a.go",
			"content": "/* keep this\nthe old dispatch spine is replaced */ doStuff();"}))))
	assert.Equal(t, "deny", verdict)
}

// A truncated list must say it is truncated, or the next write fixes what it
// was shown and is refused again for what it was not. A document forces the
// deny path (see TestADocumentNeverStrips), which is what keeps this list
// long enough to truncate.
func TestATruncatedFindingListSaysSo(t *testing.T) {
	var lines []string
	for i := range 9 {
		lines = append(lines, "sentence "+itoa(i)+" instead of the old spelling.")
	}
	_, reason := decision(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/notes.md", "content": strings.Join(lines, "\n")}))))
	assert.Contains(t, reason, "more, not listed")
}

func TestOrdinaryCodeAndCommentsPass(t *testing.T) {
	src := "// f returns EINVAL when the buffer is smaller than the Apple struct.\nfunc f() {}"
	out := run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/a.go", "content": src})))
	assert.Empty(t, out)
}

func TestADocumentIsJudgedWithoutTheVolumeCap(t *testing.T) {
	var lines []string
	for range 30 {
		lines = append(lines, "the emulation forwards a pointer and validates the size.")
	}
	out := run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/notes.md", "content": strings.Join(lines, "\n")})))
	assert.Empty(t, out, "a long paragraph is ordinary writing")

	verdict, _ := decision(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/notes.md", "content": "The apetest path previously skipped this."}))))
	assert.Equal(t, "deny", verdict)
}

// A document line is a paragraph, not a sentence: the org's own no-hard-wrap
// convention puts several sentences on one raw line, so a document finding is
// never stripped even when the whole raw line, taken alone, would look pure.
func TestADocumentNeverStrips(t *testing.T) {
	out := run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/notes.md", "content": "The apetest path previously skipped this."})))
	require.Nil(t, stripResult(t, out), "a document finding must deny, never strip")
	verdict, _ := decision(t, out)
	assert.Equal(t, "deny", verdict)
}

func TestTheVolumeCapIsTunable(t *testing.T) {
	var lines []string
	for range 20 {
		lines = append(lines, "// the handler runs after entersyscall")
	}
	in := payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": "/repo/a.go", "content": strings.Join(lines, "\n")})

	verdict, _ := decision(t, run(strings.NewReader(in)))
	assert.Equal(t, "deny", verdict)

	t.Setenv("NO_TOMBSTONES_MAX_COMMENT_LINES", "0")
	assert.Empty(t, run(strings.NewReader(in)))
}

// A comment-only line is stripped straight out, not relocated anywhere:
// nothing else records what was removed besides the one-time notice in the
// same response, and git history already holds anything worth keeping.
func TestAStrippedLineIsDeletedNotRelocated(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	file := filepath.Join(root, "a.go")

	s := stripResult(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": file, "content": "// the old flag was removed here\nfunc f() {}"}))))
	require.NotNil(t, s)
	assert.Equal(t, "func f() {}", s.toolInput["content"])
	assert.Contains(t, s.context, "the old flag was removed here")

	_, err := os.Stat(filepath.Join(root, ".git", "TOMBSTONES"))
	assert.True(t, os.IsNotExist(err), "no ledger file must be written")
}

// stubRipgrep puts an `rg` on PATH that answers the one question this plugin
// asks it, using grep. A runner without ripgrep would otherwise skip the whole
// dead-referent tier, which is how an untested tier reaches production looking
// covered.
func stubRipgrep(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\npat=''\nroot='.'\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -e) pat=\"$2\"; shift 2;;\n" +
		"    --max-count) shift 2;;\n" +
		"    -*) shift;;\n" +
		"    *) root=\"$1\"; shift;;\n" +
		"  esac\n" +
		"done\n" +
		"grep -rlF -- \"$pat\" \"$root\" 2>/dev/null\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rg"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A name the repository does not define is the tier no rewording defeats: this
// sentence carries no tell at all, only a referent that is gone. It sits on
// its own comment line, so it is stripped rather than denied.
func TestADeadReferentNameIsStrippedNotDenied(t *testing.T) {
	stubRipgrep(t)
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	require.NoError(t, os.WriteFile(filepath.Join(root, "live.go"),
		[]byte("package p\n\nfunc TestDarwinStatfsToLinux() {}\n"), 0o600))
	file := filepath.Join(root, "a.go")

	s := stripResult(t, run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": file, "content": "// see TestDarwinMntFlagsToLinux for the pin\nfunc f() {}"}))))
	require.NotNil(t, s)
	assert.Equal(t, "func f() {}", s.toolInput["content"])
	assert.Contains(t, s.context, "TestDarwinMntFlagsToLinux")

	out := run(strings.NewReader(payload(t, "PreToolUse", "Write",
		map[string]any{"file_path": file, "content": "// see TestDarwinStatfsToLinux for the pin\nfunc f() {}"})))
	assert.Empty(t, out, "a name the repository does define must pass")
}

// Absence of an answer must never read as "the symbol is gone", so every path
// that cannot look reports nothing.
func TestTheReferentTierReportsNothingWhenItCannotLook(t *testing.T) {
	blocks := commentBlocks("// see TestSomethingMissing for the pin", cLike)

	t.Run("outside a working tree", func(t *testing.T) {
		stubRipgrep(t)
		assert.Empty(t, DeadReferents(filepath.Join(t.TempDir(), "a.go"), "", blocks))
	})

	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	file := filepath.Join(root, "a.go")

	t.Run("no ripgrep on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		assert.Empty(t, DeadReferents(file, "", blocks))
	})

	t.Run("too many names to judge cheaply", func(t *testing.T) {
		stubRipgrep(t)
		var names []string
		for i := range 50 {
			names = append(names, "// symbolNumber"+itoa(i)+"Here")
		}
		many := commentBlocks(strings.Join(names, "\n"), cLike)
		assert.Empty(t, DeadReferents(file, "", many))
	})

	t.Run("a symbol the write itself carries", func(t *testing.T) {
		stubRipgrep(t)
		added := "// newHelperName does the conversion\nfunc newHelperName() {}"
		assert.Empty(t, DeadReferents(file, added, commentBlocks(added, cLike)),
			"a symbol this write defines is not a dead referent")
	})
}

// An external constant is exactly the shape a low-level comment is full of, and
// denying over one is how a guard gets turned off.
func TestExternalConstantsAreNeverCandidates(t *testing.T) {
	blocks := commentBlocks("// returns ENOSYS unless RLIMIT_CORE and SYS_STATFS agree", cLike)
	assert.Empty(t, DeadReferents("/nonexistent/a.go", "", blocks))

	for _, name := range []string{"ENOSYS", "RLIMIT_CORE", "SYS_STATFS", "statfs", "buf", "syscall"} {
		assert.False(t, isCandidate(name), "%s is not this repository's to define", name)
	}
	for _, name := range []string{"TestDarwinStatfsToLinux", "darwin_mnt_flags", "syscall6SlowDarwin"} {
		assert.True(t, isCandidate(name), "%s is a repository symbol", name)
	}
}

// The compiled matcher replaced a pattern, so the pattern is the oracle: the
// generated switch plus the Go tokenizer must pick out exactly the words
// `regexp` did. Keeping the regex here and nowhere else is the point -- it
// proves the swap rather than describing it.
func TestTheCompiledShapeMatcherAgreesWithThePatternItReplaced(t *testing.T) {
	oracle := regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+\b|\b[a-z][a-z0-9]*[A-Z][A-Za-z0-9]*\b|\b[A-Z][a-z0-9]+[A-Z][A-Za-z0-9]*\b`)

	corpus := []string{
		"see TestDarwinStatfsToLinux for the pin",
		"returns ENOSYS unless RLIMIT_CORE and SYS_STATFS agree",
		"darwin_mnt_flags and syscall6SlowDarwin and plain words here",
		"_leading and trailing_ and __dunder__ and a1B2c3",
		"CamelCase lowerCamel snake_case SCREAMING_CASE mixed_Snake_Case",
		"punctuation:separated;names,like/this.and(that)",
		"", "   ", "x", "ab_cd", "URLParser", "parseURL", "a", "9lives",
		"unicode é and ünïcodeName next to asciiName",
	}

	for _, text := range corpus {
		var got []string
		for _, w := range identifierWords(text) {
			if matchesIdentifierShape(w) {
				got = append(got, w)
			}
		}
		assert.Equal(t, oracle.FindAllString(text, -1), got,
			"tokenize+compiled-match must equal the pattern on %q", text)
	}

	// Whole-string agreement too, which is the call isCandidate actually makes.
	for _, w := range []string{"TestDarwinStatfsToLinux", "darwin_mnt_flags", "ENOSYS",
		"statfs", "lowerCamel", "UpperCamelCase", "no", "_x", "a_b", "AB", "aB"} {
		assert.Equal(t, oracle.FindString(w) == w, matchesIdentifierShape(w),
			"whole-string verdict must agree on %q", w)
	}
}

func TestEveryFailurePathAllowsTheCall(t *testing.T) {
	cases := map[string]string{
		"unparseable payload": "{not json",
		"other event":         payload(t, "Stop", "Write", map[string]any{"file_path": "/repo/a.go", "content": "// used to work"}),
		"other tool":          payload(t, "PreToolUse", "Bash", map[string]any{"file_path": "/repo/a.go", "content": "// used to work"}),
		"no file path":        payload(t, "PreToolUse", "Write", map[string]any{"content": "// used to work"}),
		"empty write":         payload(t, "PreToolUse", "Write", map[string]any{"file_path": "/repo/a.go", "content": ""}),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, run(strings.NewReader(in)))
		})
	}

	bad := `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":"not an object"}`
	assert.Empty(t, run(strings.NewReader(bad)))
}

func TestRepoRootReportsNothingOutsideAWorkingTree(t *testing.T) {
	assert.Empty(t, RepoRoot(filepath.Join(t.TempDir(), "a.go")))
}
