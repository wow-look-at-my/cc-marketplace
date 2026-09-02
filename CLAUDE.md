## Link All Refs Plugin

The link-all-refs plugin lives at `plugins/link-all-refs/`. It is one Stop hook: a turn does not end while its closing message names a pull request number, a commit SHA, a branch, or a GitHub URL with no markdown link to it. The refusal is exit code 2 with the offending tokens and their lines on stderr, which is how a Stop hook hands the model its objection.

**The test is one step.** Remove every markdown link from the closing message and look at what is left; anything a matcher finds was never linked. There is no credit for a link given earlier in the message or in an earlier turn -- the reader is looking at THIS text. `[text](url)` is the one spelling that is right on both surfaces: a link on the web client, and a real clickable hyperlink in the terminal, because Claude Code renders a markdown link through the terminal's OSC 8 escape when the terminal advertises support (`cli.js`: the renderer's `link` case wraps the href in `\x1b]8;;`).

**A branch is matched by its prefix, and a bare `owner/repo` is not matched at all.** `claude/x`, `feature/x`, `fix/x` and the rest of the conventional prefixes are unmistakable; a bare `owner/repo` has the same shape as a directory path, and nothing separates `go/core` from a repository slug by looks alone. The `owner/repo#N` form IS matched, because the number carries it. That is the deliberate boundary of the check, not an oversight to fix by guessing.

A commit SHA needs both a digit and an a-f letter, which is what separates one from a decimal number and from a word spelled in hex ("defaced"). A `&#N;` character reference is stripped before matching, so this org's `&#0;` is not read as an issue number. Every failure path -- unparseable payload, missing transcript, a message with no text -- ALLOWS the stop: a guard that blocks because it could not read a file is worse than no guard.

- **Hook binary**: `plugins/link-all-refs/hook.go` -- the Stop payload, the allow/refuse decision, and the refusal text (which escalates on `stop_hook_active`, but still refuses: the model can always satisfy this by writing the link)
- **Detection**: `plugins/link-all-refs/refs.go` -- link stripping, the fenced/quoted exemption, the four matchers and their boundary checks
- **Transcript**: `plugins/link-all-refs/transcript.go` -- the last assistant message's text blocks, read from a bounded tail
- **Tests**: `plugins/link-all-refs/refs_test.go`, `hook_test.go`, `transcript_test.go` -- linked vs unlinked, the prose that must NOT trip it (file paths, decimal numbers, `&#0;`, headings), the fenced/quoted exemption, inline backticks NOT being exempt, and every fail-open path
- **Plugin config**: `plugins/link-all-refs/.claude-plugin/plugin.json` -- one Stop hook registration
