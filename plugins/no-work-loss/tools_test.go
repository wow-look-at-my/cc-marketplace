package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The routes that are not Bash at all. Each keeps the same pair as the shell
// cases: the call that must be refused, and the neighbouring call that must
// still work, so a rule that denied the whole tool would fail the second half.

func TestTheEditToolsThemselvesAreLeftAlone(t *testing.T) {
	root := newTree(t)
	existing := filepath.Join(root, "src.txt")
	for _, tool := range []string{"Edit", "MultiEdit", "NotebookEdit"} {
		assert.Empty(t, askTool(t, tool, root, map[string]any{"file_path": existing}),
			"%s is the sanctioned route and must never be denied for using it", tool)
	}
	assert.Empty(t, askTool(t, "Write", root, map[string]any{"file_path": filepath.Join(root, "new.txt")}),
		"Write is how a new file is created")
}

// Write authors a whole file, so aiming it at one that already exists replaces
// content nobody reviewed the loss of.
func TestWriteOverAnExistingFileIsRefused(t *testing.T) {
	root := newTree(t)
	reason := askTool(t, "Write", root, map[string]any{"file_path": filepath.Join(root, "src.txt")})
	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "already exists")
	assert.Contains(t, reason, "Use Edit")

	// A path outside the tree is no different: this rule is about the tool's
	// semantics, not about which directory the file is in.
	out := outsideTree(t)
	assert.NotEmpty(t, askTool(t, "Write", root, map[string]any{"file_path": filepath.Join(out, "src.txt")}))
	assert.Empty(t, askTool(t, "Write", root, map[string]any{"file_path": filepath.Join(out, "fresh.txt")}))
}

func TestEditingTheLiveSettingsIsRefused(t *testing.T) {
	root := newTree(t)
	home := t.TempDir()
	live := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, live, "{}")

	reason := askTool(t, "Edit", root, map[string]any{"file_path": live})
	require.NotEmpty(t, reason, "a session must not edit the settings that gate it")
	assert.Contains(t, reason, "live Claude Code settings")

	// The control: this is about the live settings, not about every file called
	// settings.json, and certainly not about a plugin's own source.
	assert.Empty(t, askTool(t, "Write", root, map[string]any{"file_path": filepath.Join(root, "settings.json")}))
	assert.Empty(t, askTool(t, "Write", root, map[string]any{
		"file_path": filepath.Join(root, "plugins", "x", ".claude-plugin", "plugin.json"),
	}))
}

func TestAConfigSkillCannotReGrantWhatIsDenied(t *testing.T) {
	root := newTree(t)
	for _, skill := range []string{"update-config", "fewer-permission-prompts"} {
		reason := askTool(t, "Skill", root, map[string]any{"skill": skill})
		require.NotEmpty(t, reason, "%s rewrites settings.json as its whole purpose", skill)
		assert.Contains(t, reason, "settings")
	}
	for _, skill := range []string{"dataviz", "docs:dockerfile", "code-review"} {
		assert.Empty(t, askTool(t, "Skill", root, map[string]any{"skill": skill}),
			"%s has nothing to do with permissions", skill)
	}
}

// Delegating is fine. Handing the child something the parent does not have is
// the one-call bypass of everything else in this plugin.
func TestASubagentCannotBeHandedAWiderGrant(t *testing.T) {
	root := newTree(t)
	widened := []map[string]any{
		{"prompt": "fix it", "permissionMode": "bypassPermissions"},
		{"prompt": "fix it", "permissionMode": "acceptEdits"},
		{"prompt": "fix it", "permission_mode": "dontAsk"},
		{"prompt": "fix it", "tools": []string{"Read", "Bash"}},
		{"prompt": "fix it", "allowedTools": []string{"Bash"}},
		{"prompt": "fix it", "extra_allowed_tools": []string{"Bash"}},
		{"prompt": "fix it", "allowedTools": "Read,Bash"},
	}
	for _, input := range widened {
		for _, tool := range []string{"Agent", "Task", "mcp__Claude_Code_Remote__create_session"} {
			reason := askTool(t, tool, root, input)
			require.NotEmpty(t, reason, "%s with %v must be refused", tool, input)
			assert.Contains(t, reason, "child")
		}
	}

	ordinary := []map[string]any{
		{"prompt": "research the parser", "subagent_type": "Explore"},
		{"prompt": "review this", "model": "sonnet"},
		{"prompt": "look around", "tools": []string{"Read", "Grep"}},
		{"prompt": "plan it", "permissionMode": "plan"},
	}
	for _, input := range ordinary {
		assert.Empty(t, askTool(t, "Agent", root, input),
			"ordinary delegation must not be denied: %v", input)
	}
}

func TestTheGitHubContentAPITools(t *testing.T) {
	root := newTree(t)
	denied := []string{
		"mcp__github__create_or_update_file",
		"mcp__github__push_files",
		"mcp__github__delete_file",
		"mcp__GitHub_API_MCP__create_or_update_file",
		"mcp__GitHub_API_MCP__push_files",
	}
	for _, tool := range denied {
		reason := askTool(t, tool, root, map[string]any{"path": "README.md", "content": "x"})
		require.NotEmpty(t, reason, "%s commits content no edit tool ever saw", tool)
		assert.Contains(t, reason, "never exists as a file")
	}

	allowed := []string{
		"mcp__github__get_file_contents",
		"mcp__github__list_commits",
		"mcp__github__pull_request_read",
		"mcp__github__update_pull_request",
		"mcp__plugin_grep_grep__Grep",
	}
	for _, tool := range allowed {
		assert.Empty(t, askTool(t, tool, root, map[string]any{"path": "README.md"}),
			"%s reads, or edits metadata rather than file content", tool)
	}
}

func TestNothingIsWrittenForAnAllowedCall(t *testing.T) {
	root := newTree(t)
	out := captureStdout(t, func() {
		if r := ask(t, root, "git status"); r != "" {
			emitDeny(r)
		}
	})
	assert.Empty(t, out, "an allowed call must leave the normal permission flow untouched")
}

func TestOtherEventsAndToolsAreIgnored(t *testing.T) {
	root := newTree(t)
	for _, event := range []string{"PostToolUse", "Stop", "SessionStart", ""} {
		assert.Empty(t, decideWithEvent(t, event, "Bash", root, "sed -i s/a/b/ src.txt"),
			"%s is not this hook's event", event)
	}
	assert.Empty(t, ask(t, root, ""), "an empty command decides nothing")
	assert.Empty(t, decide([]byte("not json")))
}
