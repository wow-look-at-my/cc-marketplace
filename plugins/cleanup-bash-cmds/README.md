# cleanup-bash-cmds

A PreToolUse hook that rewrites Bash tool commands before they run. The command is
**parsed, not pattern-matched**: shfmt turns it into a syntax tree, a jq program
rewrites the tree, and shfmt turns the tree back into a command. No compiled
binary -- bash + jq + shfmt work the same on every platform.

It does six jobs:

1. **Destroys heredocs.** Any command containing a heredoc is DENIED outright --
   not rewritten, denied. See "Heredocs: banned" below.
2. **Confiscates `2>/dev/null`.** Every stderr-to-/dev/null redirection is removed,
   wherever it appears -- including inside command substitutions. You cannot
   responsibly use that, so it has to be taken away: silencing stderr hides the
   very errors you need to see.
3. **Kills trailing `| head` / `| tail` stages.** Any flags or arguments (`| head`,
   `|head -50`, `| head -n 100`, `| head -c 4k`, `| tail -n +2`, `| tail -f`, ...),
   unwound until stable, so `cmd | head -5 | tail -2` collapses all the way to
   `cmd`. Truncating output hides the rest of it.
4. **Turns trailing `> file` into `| tee file`.** The output lands in the file AND
   stays visible (`cmd >> f` becomes `cmd | tee -a f`). See "Stdout redirects
   become tee" below.
5. **Ensures `set -o pipefail`.** Every command runs with pipefail enabled --
   silently prepended unless the command already turns it on. This also keeps the
   producer's exit status observable through the injected `| tee`.
6. **Removes other noise:** trailing `2>&1` and trailing `|| true`, plus trailing
   `| grep ...`. Strictness settings the user wrote (`set -e` and friends) are
   NEVER removed -- this hook only ever adds strictness.

## Announced vs silent

Changes that alter what you can observe are announced in a `systemMessage`
listing each actual edit ("removed 2>/dev/null; removed | head -50"); plumbing
that changes nothing observable is applied silently:

| Rule | Class |
|------|-------|
| heredoc ban | deny (its own message) |
| `2>/dev/null` scrub | announced |
| trailing `| head` / `| tail` strip | announced |
| trailing `| grep` strip | announced |
| trailing `|| true` removal | announced |
| trailing `> file` -> `| tee file` | announced |
| trailing `2>&1` removal | silent |
| `set -o pipefail` injection | silent |

A silent-only rewrite carries `suppressOutput: true` and no message at all; when
announced changes fire, silent ones ride along unannounced.

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

## How it works

```
hook stdin JSON -> jq (extract .tool_input.command)
                -> shfmt --to-json          (parse to a typed syntax tree)
                -> jq -f transform.jq       (inspect + rewrite the tree)
                -> heredoc found?           (emit permissionDecision deny; stop)
                -> shfmt --from-json        (regenerate the command)
                -> emit hookSpecificOutput.updatedInput
```

Because the rewrite operates on real `Redirect` and pipeline nodes, string
literals and comments that merely *contain* `2>/dev/null` or `| head` are never
touched. A rewrite is emitted only when the tree semantically changed (positions
are ignored in the comparison); untouched commands pass through byte-for-byte.

## Heredocs: banned

You cannot be trusted with heredocs, so they are gone. Any command whose syntax
tree contains a heredoc redirect -- `<<` or `<<-`, quoted or unquoted delimiter,
anywhere in the command including inside `$(...)`, process substitutions, and
function bodies -- is **denied**, not rewritten:

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny",
 "permissionDecisionReason": "Heredocs are banned in this environment. Write file
 content with the Write/Edit tools; for command stdin use printf '%s' ... | cmd
 or a temp file."}}
```

- **Deny beats rewrite**: a command with a heredoc AND scrubbable noise is denied
  immediately; nothing is rewritten.
- **Herestrings (`<<<`) are allowed** -- they are not heredocs -- and still get the
  other cleanups (`grep x <<<"data" 2>/dev/null` becomes `grep x <<<"data"`).
- Detection is AST-based: `echo "here is <<EOF in a string"` and the arithmetic
  bit shift `$((x << 2))` (which shares shfmt's `<<` token number) are untouched.
- What to use instead: the Write/Edit tools for file content; `printf '%s' ... |
  cmd` or a temp file for stdin.
- Denies are logged to `CLEANUP_BASH_CMDS_LOG` as `DENY` lines.
- Fail-open still applies: if the command does not parse (or shfmt/jq are
  missing), the hook cannot see a heredoc and passes the command through.

Two implementation notes:

- shfmt's typed JSON encodes operators as version-dependent numbers (`|` is 12 in
  v3.8.0 but 13 in v3.13.1), so the hook probes the numbers at runtime by parsing
  a tiny fixed script with the same shfmt binary it will use.
- A rewritten command comes back in shfmt's canonical formatting (normalized
  spacing; `a; b` becomes two lines). Formatting-only differences never trigger a
  rewrite on their own.

## Exactly which forms are scrubbed

Stderr redirections to /dev/null, anywhere in the command, in any of these
spellings:

| Form | Example |
|------|---------|
| `2>/dev/null` | `grep x f 2>/dev/null && echo hit` |
| `2> /dev/null` | `cmd 2> /dev/null` |
| `2>>/dev/null` | `cmd 2>>/dev/null` |
| `2>> /dev/null` | `cmd 2>> /dev/null` |
| `2>'/dev/null'` | `cmd 2>'/dev/null'` |
| `2>"/dev/null"` | `cmd 2>"/dev/null"` |

Because the match is a parsed `Redirect` node (fd 2, `>` or `>>`, target exactly
`/dev/null`), the old regex hazards are structurally impossible: `12>/dev/null`
redirects fd 12 and stays; `2>/dev/null2` and `2>/dev/null.log` name different
files and stay; `echo "try 2>/dev/null"` is a string and stays.

## Trailing `| head` / `| tail` removal

A trailing `| head ...` or `| tail ...` stage is dropped from the end of a
pipeline, repeatedly, with whatever flags and arguments it carries:

```bash
git log | head -5 | tail -2   ->   git log
cat f | grep x | head -3      ->   cat f      # grep becomes trailing next pass
a | head -2 && b | tail -3    ->   a && b
```

Scope and guards:

- Applies to every top-level statement (each line / `;` member) and both sides of
  top-level `&&` / `||` chains.
- **Never inside `$(...)` or `<(...)`.** `VAR=$(ls | head -1)` is functional
  capture, not output truncation, and is preserved.
- **Word boundaries are real.** `| headache`, `| tailscale status`, `| head5` are
  different commands and stay untouched (the stage's command word must be exactly
  `head` or `tail`).
- **Mid-pipeline stages stay.** `cmd | head -5 | wc` keeps its `head`. If a later
  trailing stage is stripped and `head`/`tail` becomes trailing, the next pass
  strips it too; that is the point.
- Strings are safe: `echo "foo | head"` contains no pipeline.

## Stdout redirects become tee

A trailing stdout file redirect on a top-level pipeline/statement is rewritten
into a pipe through tee, so the file is still written but the output is no
longer hidden from the transcript:

```bash
cmd > build.log         ->   cmd | tee build.log
cmd >> build.log        ->   cmd | tee -a build.log
a | b > out             ->   a | b | tee out
make > "$OUT" 2>err     ->   make 2>err | tee "$OUT"
```

The redirect's target word is reused verbatim (quoting and expansions like
`"$OUT"` survive), every other redirect stays on the producer, and the injected
`set -o pipefail` keeps the producer's exit status from being masked by tee.

Exclusions (left exactly as written, deliberately):

- targets under `/dev/` -- `cmd > /dev/null` is a deliberate stdout discard and
  stays a discard
- process-substitution targets (`cmd > >(gzip)`)
- statements with more than one stdout file redirect (`cmd > a > b`)
- anything inside `$(...)` or `<(...)` -- `VAR=$(cmd > f)` is untouched
- non-stdout redirects (`cmd 2> err.log`, `cmd < in`)

## Non-goals (deliberately NOT touched)

- `&>/dev/null` (redirects both stdout and stderr)
- `>/dev/null` (a stdout discard stays a discard)
- `>/dev/null 2>&1` -- the trailing `2>&1` is silently removed, leaving
  `cmd >/dev/null`
- `2>&1` anywhere except the very end of the command (e.g. `cmd 2>&1 | wc`)
- `2 >/dev/null` (that is an argument `2` plus a stdout redirect)
- `head`/`tail` used mid-pipeline, standalone (`head -5 file`), or inside
  command/process substitutions
- trailing `| grep` keeps its original anchoring: only at the very end of the
  command (last statement), unlike head/tail which apply per statement
- `set -e`, `set -u`, `set -euo pipefail`, ... -- strictness settings are never
  removed (and `set -euo pipefail` is recognized, so no duplicate injection)

## Requirements

The hook needs three tools on PATH at runtime; if any is missing it **fails
open** (the command runs unmodified):

| Tool | Why | Notes |
|------|-----|-------|
| bash | orchestration | |
| jq | JSON handling + AST transform | 1.6+ |
| shfmt | parse/print bash | needs `--to-json` / `--from-json` (v3.7.0+; verified with 3.8.0 and 3.13.1) |

Installing shfmt:

```bash
# Debian/Ubuntu                # macOS                    # Windows
sudo apt-get install shfmt     brew install shfmt         scoop install shfmt

# Anywhere with Go                             # Static binaries
go install mvdan.cc/sh/v3/cmd/shfmt@latest     https://github.com/mvdan/sh/releases
```

## Caveats

- **Rewritten commands come back shfmt-formatted.** Spacing is normalized and
  `a; b` prints as two lines. Only commands that had a real change are reformatted.
- **The command must parse as bash.** Anything shfmt cannot parse passes through
  untouched (fail-open), as does anything when shfmt/jq are absent.
- **The permission prompt still applies to rewrites.** For rewrites the hook emits
  `hookSpecificOutput.updatedInput` *without* a `permissionDecision`, so the normal
  permission flow evaluates the rewritten command (verified against
  `@anthropic-ai/claude-code` 2.1.201). This is a change from the original Go
  implementation of this plugin, which returned `permissionDecision: "allow"` and
  made every rewritten command skip the permission prompt. Only the heredoc ban
  uses a `permissionDecision` (`"deny"`).
- For announced rewrites the model is told what actually runs via
  `additionalContext`, and the user sees the per-change `systemMessage`. Silent
  rewrites emit neither (plus `suppressOutput: true`).

## Logging

Set `CLEANUP_BASH_CMDS_LOG=/path/to/file` to append a record of every rewrite
(including silent ones, tagged -- the spam concern is the transcript, not the
log) and every deny:

```
REWRITE	original="ls | grep foo"	cleaned="set -o pipefail\nls"
REWRITE	original="git status"	cleaned="set -o pipefail\ngit status"	reason="silent"
DENY	original="cat <<EOF\nhi\nEOF"	reason="heredoc"
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

The orchestration lives in `hook.sh`, the AST rewrite in `transform.jq`, and the
tests in `tests/run-tests.sh` (synthetic hook payloads in, JSON assertions out):

```bash
bash tests/run-tests.sh
```

If the machine has no usable shfmt, the test runner bootstraps a pinned release
binary into a temp directory for the run (this is how bare CI runners pass). CI
runs the same tests through the `prebuild` recipe in the `justfile`.

And no, `hook.sh` does not use `2>/dev/null` anywhere itself. We checked.
