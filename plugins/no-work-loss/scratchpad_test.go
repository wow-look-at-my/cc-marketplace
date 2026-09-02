package main

import (
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

// The harness tells a session to put temp files in its own scratchpad, so a
// script run from there must not be refused. Everything else under /tmp still
// is: the exemption is the documented directory, not "anything in /tmp".
func TestSessionScratchpadIsNotRefusedAsAScratchScript(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		exempt bool
	}{
		{"the harness scratchpad", "/tmp/claude-0/-home-user/c671d65d-12da/scratchpad/gen.mjs", true},
		{"nested inside it", "/tmp/claude-0/-home-user/c671d65d-12da/scratchpad/lib/util.mjs", true},
		{"an underscore variant", "/tmp/claude_1/proj/sess/scratchpad/gen.mjs", true},
		{"a bare claude segment", "/tmp/claude/sess/scratchpad/gen.mjs", true},

		{"a plain temp script", "/tmp/gen.mjs", false},
		{"a scratchpad with no claude ancestor", "/tmp/scratchpad/gen.mjs", false},
		{"a claude dir that is not a scratchpad", "/tmp/claude-0/-home-user/sess/gen.mjs", false},
		{"claude AFTER the scratchpad segment", "/tmp/scratchpad/claude-0/gen.mjs", false},
		{"a lookalike outside any temp root", "/home/user/claude-0/scratchpad/gen.mjs", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isSessionScratchpad(c.path)
			require.Equal(t, c.exempt, got)

		})
	}
}

// The exemption has to reach the actual decision, not just the predicate.
func TestScratchScriptWritesAllowsTheSessionScratchpad(t *testing.T) {
	root := t.TempDir()
	pad := filepath.Join("/tmp", "claude-0", "proj", "sess", "scratchpad")

	arg := func(p string) []word { return []word{{text: p, static: true}} }

	w := scratchScriptWrites(segment{cwd: root}, "node", arg(filepath.Join(pad, "gen.mjs")), []string{root})
	require.Equal(t, 0, len(w))

	w = scratchScriptWrites(segment{cwd: root}, "node", arg("/tmp/gen.mjs"), []string{root})
	require.NotEqual(t, 0, len(w))

}
