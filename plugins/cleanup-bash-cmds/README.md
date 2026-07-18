# cleanup-bash-cmds

A PreToolUse hook that rewrites Bash tool commands before they run. The command is
**parsed, not pattern-matched**: shfmt turns it into a syntax tree, a jq program
rewrites the tree, and shfmt turns the tree back into a command. No compiled
binary -- bash + jq + shfmt work the same on every platform.

It does eight jobs:

1. **Destroys heredocs and denies perl.** Any command containing a heredoc, or
   invoking `perl` (in any position the parser recognizes as a command), is
   DENIED outright -- not rewritten, denied. See "Heredocs: banned" and
   "perl: banned" below.
2. **Confiscates `2>/dev/null`.** Every stderr-to-/dev/null redirection is removed,
   wherever it appears -- including inside command substitutions. You cannot
   responsibly use that, so it has to be taken away: silencing stderr hides the
   very errors you need to see.
3. **Kills trailing `| head` / `| tail` stages** -- on the FINAL statement only.
   Any flags or arguments (`| head`, `|head -50`, `| head -n 100`, `| head -c 4k`,
   `| tail -n +2`, `| tail -f`, ...), unwound until stable, so `cmd | head -5 |
   tail -2` collapses all the way to `cmd`. Truncating output hides the rest of
   it. A limiting pipe on an EARLIER statement of a multi-statement script is a
   deliberate part of that script and is preserved.
4. **Turns a trailing `> file` into `| tee file`** -- same final-statement scope.
   The output lands in the file AND stays visible (`cmd >> f` becomes
   `cmd | tee -a f`). Mid-script redirects are preserved. See "Stdout redirects
   become tee" below.
5. **Ensures `set -o pipefail`.** Every command runs with pipefail enabled --
   silently prepended unless the command already turns it on. This also keeps the
   producer's exit status observable through the injected `| tee`.
6. **Caps every `sleep` at 3 seconds.** Anywhere in the tree, including loops,
   functions, and `$( )`. Literal durations summing to <= 3 are kept; everything
   else (`sleep 30`, `sleep 1m`, `sleep $DELAY`, `sleep infinity`, junk, no args)
   becomes `sleep 3`. See "Sleep capped at 3 seconds" below.
7. **Removes constant narration echoes/printfs.** A terminal-bound `echo` with
   all-constant arguments, or a `printf` that just prints a single constant
   string with no `%` directive, is removed entirely -- its whole command is
   rewritten to the no-op `:` (no output, exit status 0, surrounding structure
   intact). A `printf` that actually formats (a `%` directive, extra args, or an
   expansion) is kept. The matcher sees through `command` / `builtin` / a leading
   `\` / quoting wrappers. See "Constant narration echoes and printfs are
   removed" below.
8. **Removes other noise:** trailing `2>&1` and trailing `|| true`, plus trailing
   `| grep ...` (all anchored at the end of the command, like head/tail).
   Strictness settings the user wrote (`set -e` and friends) are NEVER removed --
   this hook only ever adds strictness.

## Fully silent by design

The hook never announces a rewrite -- no `systemMessage`, no
`additionalContext`, ever, for any rule combination. Every rewrite emits only
the replacement input plus `suppressOutput: true`, so nothing about the hook
appears in the transcript. Reason: any visible hook message just gives the
model something to blame for its own command mistakes.

The only observable trace of a rewrite is the executed command itself; for
debugging, set `CLEANUP_BASH_CMDS_LOG` (see Logging) -- the log records every
rewrite with the rules that fired. The single exception is the heredoc ban,
which must return a `permissionDecisionReason` (without one the model would
retry heredocs forever); it carries no `systemMessage` either.

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
                -> statement-count guard    (fewer top-level statements than
                                             the original? fail open: no rewrite)
                -> emit hookSpecificOutput.updatedInput
```

Because the rewrite operates on real `Redirect` and pipeline nodes, string
literals and comments that merely *contain* `2>/dev/null` or `| head` are never
touched. A rewrite is emitted only when the tree semantically changed (positions
are ignored in the comparison); untouched commands pass through byte-for-byte.

The statement-count guard is belt and braces: no rule can splice the statement
list (they map over it, edit within a statement, or prepend pipefail), but if a
future rule bug or a shfmt regeneration regression ever ate a statement, the
hook would drop the whole rewrite (and log a `GUARD` line) rather than execute
a truncated command.

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

## perl: banned

`perl` is banned in this environment. Any command whose **effective command** is
`perl` -- matched against the anchored pattern `^perl[0-9.]*$`, so `perl5.36`
counts too -- is **denied**, not rewritten:

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny",
 "permissionDecisionReason": "perl is banned in this environment."}}
```

- The **effective-command resolver** applies, so wrappers do not get you past it:
  `command perl -e 1`, `\perl -e 1`, and `builtin perl` all resolve to `perl` and
  are denied. Deny beats rewrite, and the deny is logged as a `DENY` line with
  `reason="perl"`.
- `perl` as an **argument** or a **different command** is not denied -- the walk
  never enters word-internal contexts, so `grep perl file` and `perlcritic file`
  run, and `command -v perl` is a lookup, not an invocation.
- `perl` inside a **command/process substitution** (`echo $(perl -e 1)`) is a
  deliberate non-goal (see Non-goals) -- the same word-scoping that keeps
  `grep perl` safe.
- Fail-open still applies: an unparseable command passes through.

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

A trailing `| head ...` or `| tail ...` stage is dropped from the end of the
command, repeatedly, with whatever flags and arguments it carries:

```bash
git log | head -5 | tail -2   ->   git log
cat f | grep x | head -3      ->   cat f      # grep becomes trailing next pass
a | head -2 && b | tail -3    ->   a | head -2 && b
```

Scope and guards:

- **Final statement only.** The rule anchors at the textual end of the command:
  the last top-level statement, and within it the rightmost `&&` / `||` member.
  A limiting pipe on an earlier statement of a multi-line / `;`-joined script
  (`ls | tail -12` followed by more commands) is a deliberate part of that
  script and is preserved.
- **Never inside `$(...)` or `<(...)`.** `VAR=$(ls | head -1)` is functional
  capture, not output truncation, and is preserved.
- **Word boundaries are real.** `| headache`, `| tailscale status`, `| head5` are
  different commands and stay untouched (the stage's command word must be exactly
  `head` or `tail`).
- **Mid-pipeline stages stay.** `cmd | head -5 | wc` keeps its `head`. If a later
  trailing stage is stripped and `head`/`tail` becomes trailing, the next pass
  strips it too; that is the point.
- Strings are safe: `grep "foo | head" f` contains no pipeline.

## Stdout redirects become tee

A trailing stdout file redirect on the FINAL top-level statement (rightmost
`&&` / `||` member -- the same anchoring as head/tail) is rewritten into a pipe
through tee, so the file is still written but the output is no longer hidden
from the transcript:

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

- anything before the final statement -- `make > build.log` followed by more
  commands keeps its redirect (mid-script output routing is intentional)
- targets under `/dev/` -- `cmd > /dev/null` is a deliberate stdout discard and
  stays a discard
- process-substitution targets (`cmd > >(gzip)`)
- statements with more than one stdout file redirect (`cmd > a > b`)
- anything inside `$(...)` or `<(...)` -- `VAR=$(cmd > f)` is untouched
- non-stdout redirects (`cmd 2> err.log`, `cmd < in`)

## Sleep capped at 3 seconds

Every `sleep` in real command position -- top level, loop bodies, function
bodies, subshells, `$( )` captures, either side of `&&` / `||` / `;` -- is
capped. If every argument is a literal word that parses as a GNU sleep duration
(decimal with optional `s`/`m`/`h`/`d` suffix) and the durations sum to <= 3
seconds, the command is untouched. EVERYTHING else has its whole argument list
replaced with the single literal `3`:

```bash
sleep 2                  ->   sleep 2          # literal, under the cap
sleep 0.5s               ->   sleep 0.5s       # suffixes understood
sleep 30                 ->   sleep 3
sleep 1m                 ->   sleep 3          # 60s > 3s
sleep 1m 30              ->   sleep 3          # durations SUM
sleep $DELAY             ->   sleep 3          # non-literal: cannot be trusted
sleep "$(get_delay)"     ->   sleep 3
sleep infinity           ->   sleep 3
sleep                    ->   sleep 3          # zero args (an error anyway)
FOO=1 sleep 30 2>>e.log  ->   FOO=1 sleep 3 2>>e.log   # assigns/redirs kept
```

Notes:

- The cap is **per command**, not per script: `sleep 2 && sleep 2` is fine.
- `timeout 5 sleep 30` and `"sleep 30"` inside a string are word arguments, not
  command position, and are untouched by construction.
- The duration grammar is deliberately strict; anything it does not recognize
  (including scientific notation like `sleep 1e-3`) takes the junk path and
  becomes `sleep 3`.

## Constant narration echoes and printfs are removed

`echo <constants>` (or `printf <constant>`) is the model narrating into the
transcript -- `echo "=== step 2 ==="` separators and friends. A matched
narration command is **removed**: its whole command is rewritten to the no-op
`:`, so it produces no output, exits 0, and leaves every surrounding statement,
redirect, and pipe stage intact. `:` is used instead of deleting the statement
so the top-level statement count never drops and exit-0 is preserved.

```bash
echo "=== files present ==="   ->   :
echo done && rm -f x           ->   : && rm -f x
echo warn 2>>err.log           ->   : 2>>err.log     # the redirect stays
```

The two commands differ in what counts as narration:

- **`echo`** is removed when every argument after the command word is constant
  (flags count; plain literals with glob characters `* ? [ {` or a leading `~`
  do NOT count -- their output is runtime data). Bare `echo` is removed too.
- **`printf`** is removed only for the literal-print form: exactly ONE argument
  after the command word, a static string with NO `%` in it. A `%` directive, a
  `%%`, extra args beyond the format, or an expansion mean it is really
  formatting and it is kept:

  ```bash
  printf 'done\n'    ->   :                 # a constant string, no %
  printf '%s\n' x    ->   printf '%s\n' x   # formats: kept
  printf '%d' 5      ->   printf '%d' 5     # kept
  printf 'a%%b'      ->   printf 'a%%b'     # kept (has %)
  printf x y z       ->   printf x y z      # kept (more than one arg)
  ```

The matcher **sees through wrappers and quoting** via an effective-command
resolver, so a wrapped narration command is still removed:

```bash
command echo hi        ->   :
builtin printf 'hi'    ->   :
\echo hi               ->   :
"printf" hi            ->   :
command command echo x ->   :
```

`command -v NAME` / `command -V NAME` are lookups, not invocations, so they are
left alone (`command -v printf` stays), and `command printf '%s\n' x` is kept
because the underlying `printf` is formatting.

"Reaches the terminal" is computed structurally, walking down from the file
root. An echo or printf is NOT removed when its output is data:

- feeding a pipe (`echo foo | cat`, `echo '{"x":1}' | jq .x`) -- but a FINAL
  pipe stage (`x | echo foo`) prints to the terminal and IS removed
- captured (`X=$(echo abc)`, backticks, `<( )`, `>( )`)
- redirected (`echo foo > file`, `echo foo >&2`); pure stderr redirects
  (`echo warn 2>>err.log`) do not disqualify, and any redirect on an enclosing
  compound (`{ echo a; } > f`, `for ...; done > log`) makes the whole body data
- inside a function body (visibility is decided at the call site: `x=$(f)`
  would capture) or a coproc
- carrying any expansion (`echo "$VAR"`, `echo $(date)`, `echo $((1+2))`) or
  glob/tilde (`echo *.txt`, `echo ~`) -- that output is information, not
  narration

Compound bodies stay in scope: if/elif/else branches, while/until/for bodies
and conditions, case items, subshells, `time`, `!`, and both sides of `&&` /
`||` all count as terminal-bound statement positions.

## Non-goals (deliberately NOT touched)

- `&>/dev/null` (redirects both stdout and stderr)
- `>/dev/null` (a stdout discard stays a discard)
- `>/dev/null 2>&1` -- the trailing `2>&1` is silently removed, leaving
  `cmd >/dev/null`
- `2>&1` anywhere except the very end of the command (e.g. `cmd 2>&1 | wc`, or
  on a non-final statement of a longer script)
- `2 >/dev/null` (that is an argument `2` plus a stdout redirect)
- `head`/`tail` used mid-pipeline, standalone (`head -5 file`), on a non-final
  statement, or inside command/process substitutions
- `> file` on a non-final statement (only the final statement's redirect
  becomes tee)
- `| grep`, `|| true`, `| head`, `| tail`, `> file`: all anchored at the very
  end of the command (last statement, rightmost `&&`/`||` member) -- one shared
  anchoring for every trailing-noise rule
- `command sleep` / `\sleep` -- the sleep/head/tail/grep/true rules still match
  the plain literal command word only. Only **narration removal** and the
  **perl deny** see through `command` / `builtin` / a leading `\` / quoting;
  the other name-keyed rules do not.
- `perl` inside a command substitution or process substitution
  (`echo $(perl -e 1)`, `<(perl ...)`) is NOT denied -- the resolver never
  descends into word-internal contexts (the same scoping that keeps
  `grep perl` / `perlcritic` safe). Flag it if you want it closed.
- `sh -c '...'` / `bash -c '...'` payloads are opaque strings -- the hook parses
  the outer command, not the quoted script, so narration, perl, and every other
  rule inside a `-c` payload are not seen (would need recursive sub-parsing)
- `env echo ...`, aliases, and other non-`command`/`builtin` wrappers are not
  resolved through
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
  made every rewritten command skip the permission prompt. Only the heredoc and
  perl bans use a `permissionDecision` (`"deny"`).
## Logging

The log file is the hook's debug channel (the transcript shows nothing). Set
`CLEANUP_BASH_CMDS_LOG=/path/to/file` to append a record of every rewrite --
tagged with the rules that fired -- every deny, and every statement-count
fail-open:

```
REWRITE	original="ls | grep foo"	cleaned="set -o pipefail\nls"	rules="grep,pipefail"
REWRITE	original="sleep 30; echo hi"	cleaned="set -o pipefail\nsleep 3\n:"	rules="sleep_cap,narration_remove,pipefail"
DENY	original="cat <<EOF\nhi\nEOF"	reason="heredoc"
DENY	original="perl -e 1"	reason="perl"
GUARD	original="..."	cleaned="..."	reason="stmt-count"
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
