// repo-index suggests org repositories that look relevant to a prompt. It
// injects the link and one description per repo, once per session.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	Prompt        string `json:"prompt"`
	Cwd           string `json:"cwd"`
}

type hookOutput struct {
	HookSpecificOutput *hookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// env carries the parts of the machine the hook reads, so a test can supply
// its own temp directory and home.
type env struct {
	home    string
	stateIn string
	stdout  io.Writer
	stderr  io.Writer
}

func main() {
	e := env{
		home:    os.Getenv("HOME"),
		stateIn: os.TempDir(),
		stdout:  os.Stdout,
		stderr:  os.Stderr,
	}
	os.Exit(run(os.Stdin, e))
}

// run returns the process exit code. Code 1 reports a real fault to the user
// and leaves the prompt untouched. It never returns 2, which would block the
// prompt: a suggestion is worth nothing at that price.
func run(r io.Reader, e env) int {
	data, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintf(e.stderr, "repo-index: cannot read hook input: %v\n", err)
		return 1
	}
	var in hookInput
	if err := json.Unmarshal(data, &in); err != nil {
		fmt.Fprintf(e.stderr, "repo-index: hook input is not valid JSON: %v\n", err)
		return 1
	}
	if in.HookEventName != "UserPromptSubmit" || strings.TrimSpace(in.Prompt) == "" {
		fmt.Fprint(e.stdout, "{}\n")
		return 0
	}

	repos, err := loadIndex(e.home, in.Cwd)
	if err != nil {
		fmt.Fprintf(e.stderr, "repo-index: %v\n", err)
		return 1
	}

	hits := match(in.Prompt, repos)
	if len(hits) == 0 {
		fmt.Fprint(e.stdout, "{}\n")
		return 0
	}

	dedupe := in.SessionID != ""
	if !dedupe {
		fmt.Fprintln(e.stderr, "repo-index: hook input carries no session_id, so this prompt's suggestions are not deduplicated")
	}

	seen := map[string]bool{}
	if dedupe {
		seen = readState(e.stateIn, in.SessionID)
	}
	fresh := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if !seen[h.Repo.Name] {
			fresh = append(fresh, h)
		}
	}
	if len(fresh) == 0 {
		fmt.Fprint(e.stdout, "{}\n")
		return 0
	}

	if len(fresh) > maxSuggestions {
		dropped := make([]string, 0, len(fresh)-maxSuggestions)
		for _, h := range fresh[maxSuggestions:] {
			dropped = append(dropped, h.Repo.Name)
		}
		fmt.Fprintf(e.stderr, "repo-index: %d repo(s) matched beyond the cap of %d and were not suggested: %s\n",
			len(dropped), maxSuggestions, strings.Join(dropped, ", "))
		fresh = fresh[:maxSuggestions]
	}

	if dedupe {
		for _, h := range fresh {
			seen[h.Repo.Name] = true
		}
		if err := writeState(e.stateIn, in.SessionID, seen); err != nil {
			fmt.Fprintf(e.stderr, "repo-index: %v -- these suggestions can repeat later in the session\n", err)
		}
	}

	out := hookOutput{HookSpecificOutput: &hookSpecificOutput{
		HookEventName:     "UserPromptSubmit",
		AdditionalContext: render(fresh),
	}}
	encoded, err := json.Marshal(out)
	if err != nil {
		fmt.Fprintf(e.stderr, "repo-index: cannot encode output: %v\n", err)
		return 1
	}
	fmt.Fprintf(e.stdout, "%s\n", encoded)
	return 0
}
