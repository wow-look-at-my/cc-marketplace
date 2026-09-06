## Link All Refs Plugin

The link-all-refs plugin lives at `plugins/link-all-refs/`. It is one Stop hook.

**The test is one step.** Remove every markdown link from the closing message and look at what is left. Anything a matcher finds was never linked. There is no credit for a link given earlier in the message or in an earlier turn -- the reader is looking at THIS text.

A bare `owner/repo` has the same shape as a directory path, and nothing separates `go/core` from a repository slug by looks alone. The `owner/repo#N` form IS matched, because the number carries it. That is the deliberate boundary of the check, not an oversight to fix by guessing.

A commit SHA needs both a digit and an a-f letter. A `&#N;` character reference is stripped before matching, so this org's `&#0;` is not read as an issue number.

- **Hook binary**: `plugins/link-all-refs/hook.go` -- the Stop payload, the allow/refuse decision, and the refusal text (which escalates on `stop_hook_active`, but still refuses. The model can always satisfy this by writing the link)
- **Detection**: `plugins/link-all-refs/refs.go` -- link stripping, the fenced/quoted exemption, the four matchers and their boundary checks
- **Transcript**: `plugins/link-all-refs/transcript.go` -- the last assistant message's text blocks, read from a bounded tail
- **Tests**: `plugins/link-all-refs/refs_test.go`, `hook_test.go`, `transcript_test.go` -- linked vs unlinked. The prose that must NOT trip it (file paths, decimal numbers, `&#0;`, headings), the fenced/quoted exemption, inline backticks NOT being exempt, and every fail-open path
- **Plugin config**: `plugins/link-all-refs/.claude-plugin/plugin.json` -- one Stop hook registration
