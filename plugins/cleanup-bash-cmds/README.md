# cleanup-bash-cmds

A PreToolUse hook that rewrites Bash tool commands before they run. Pure bash + jq --
no compiled binary, so it works on any platform where bash and jq exist.

It does two jobs:

1. **Confiscates `2>/dev/null`.** Every stderr-to-/dev/null redirection is scrubbed
   from the command, wherever it appears. You cannot responsibly use that, so it has
   to be taken away: silencing stderr hides the very errors you need to see.
2. **Removes noise the model likes to add** (the original behavior of this plugin):
   trailing `2>&1`, trailing `|| true`, trailing `| head` / `| tail` / `| grep`
   (with their arguments), and leading `set -e;` / `set -e &&`.

## Before / After

Before (what the model asked for):

```bash
ls /nope 2>/dev/null
```

The command runs, the error is swallowed, and the model concludes the directory is
merely empty.

After (what actually executes):

```bash
ls /nope
```

stderr stays visible: `ls: cannot access '/nope': No such file or directory`.

## Exactly which forms are scrubbed

All of these are removed, anywhere in the command (start, middle, end, across
multi-line commands):

| Form | Example |
|------|---------|
| `2>/dev/null` | `grep x f 2>/dev/null && echo hit` |
| `2> /dev/null` | `cmd 2> /dev/null` |
| `2>>/dev/null` | `cmd 2>>/dev/null` |
| `2>> /dev/null` | `cmd 2>> /dev/null` |
| `2>'/dev/null'` | `cmd 2>'/dev/null'` |
| `2>"/dev/null"` | `cmd 2>"/dev/null"` |

Safety guards:

- **Multi-digit file descriptors are left alone.** `foo 12>/dev/null` redirects fd 12,
  not stderr, and is not touched (the `2` must be at the start of the command or
  preceded by a non-digit).
- **Distinct paths are left alone.** `2>/dev/null2` and `2>/dev/null.log` name real,
  different files and are not touched (the target must be followed by end-of-command
  or a non-path character).

## Non-goals (deliberately NOT touched)

- `&>/dev/null` (redirects both stdout and stderr)
- `>/dev/null` (stdout only)
- `>/dev/null 2>&1` -- the `>/dev/null` part survives; a *trailing* `2>&1` is still
  removed by the legacy noise rule, leaving `cmd >/dev/null`
- bare `2>&1` in the middle of a command (e.g. `cmd 2>&1 | wc`)
- `2 >/dev/null` (that is an argument `2` plus a stdout redirect)

## Caveats

- **Quoted strings and heredocs get scrubbed too.** The hook is a blunt instrument by
  design -- it completely scrubs all usages, including `echo "try 2>/dev/null"`. If a
  scrub removes text mid-command, a doubled space may remain; bash does not care.
- **jq is required; the hook fails open.** If jq is not installed, the input is not
  valid hook JSON, the tool is not Bash, or the command is empty, the hook exits 0
  and changes nothing.
- **The permission prompt still applies.** The hook emits
  `hookSpecificOutput.updatedInput` *without* a `permissionDecision`, so the normal
  permission flow evaluates the rewritten command (verified against
  `@anthropic-ai/claude-code` 2.1.201). This is a change from the previous Go
  implementation of this plugin, which returned `permissionDecision: "allow"` and
  made every rewritten command skip the permission prompt.
- The model is told about the rewrite via `additionalContext`, and the user sees a
  `systemMessage` notice.

## Logging

Set `CLEANUP_BASH_CMDS_LOG=/path/to/file` to append a record of every rewrite:

```
REWRITE	original="ls | grep foo"	cleaned="ls"
```

Log failures never break the hook.

## Installation

This plugin is part of the cc-marketplace marketplace.

```bash
# Add the marketplace (if not already added)
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json

# Install this plugin
claude plugin install cleanup-bash-cmds
```

## Development

The hook logic lives in `hook.sh`; the tests in `tests/run-tests.sh` feed synthetic
hook payloads to the script and assert on the emitted JSON:

```bash
bash tests/run-tests.sh
```

CI runs the same tests through the `prebuild` recipe in the `justfile`.

And no, `hook.sh` does not use `2>/dev/null` anywhere itself. We checked.
