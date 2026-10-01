package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The matcher is compared against the tool NAME and nothing else -- a hook
// matcher never sees tool_input, so a Dockerfile or a compose path cannot be
// selected here and the filtering has to stay in topicFor. What the matcher can
// do is keep the binary from being spawned on the tools this hook ignores.
//
// A matcher of only letters and pipes is not a regex: Claude Code splits it on
// `|` and compares each name for equality. So a name that drifts out of sync
// with topicFor does not merely widen the match, it silences that tool
// completely, with nothing to see at run time.
func TestMatcherListsExactlyTheToolsTheHookReads(t *testing.T) {
	raw, err := os.ReadFile(".claude-plugin/plugin.json")
	require.NoError(t, err)

	var manifest struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.Len(t, manifest.Hooks.PreToolUse, 1)

	matcher := manifest.Hooks.PreToolUse[0].Matcher
	require.NotEqual(t, "*", matcher, "a matcher of * spawns this binary on every tool call")

	for _, name := range strings.Split(matcher, "|") {
		require.NotEmpty(t, topicFor(name, dockerTargets(name)),
			"%q is in the matcher but topicFor ignores it, so the hook pays for the spawn and says nothing", name)
	}

	// The reverse direction: a tool topicFor handles but the matcher omits is
	// never delivered, and the only symptom is a reminder that stops arriving.
	for _, name := range []string{"Read", "Write", "Edit", "MultiEdit", "NotebookEdit", "Bash"} {
		require.Contains(t, strings.Split(matcher, "|"), name)
	}
}

// dockerTargets builds an input the given tool would carry for a Docker file,
// so a tool that reads file_path and one that reads command are both exercised.
func dockerTargets(tool string) toolInput {
	if tool == "Bash" {
		return toolInput{Command: "docker build ."}
	}
	return toolInput{FilePath: "Dockerfile"}
}
