# claude-md-budget

Keeps `CLAUDE.md` and `@`-imported snippets inside the character budget every
request pays for.

Claude Code loads every instruction file **verbatim** into the prompt, and
nothing truncates them — the only hard cap skips a file over 4 MiB entirely. So
an oversized `CLAUDE.md` isn't cut off, it's billed in full on every request for
the life of the session. The CLI does warn, but only in the terminal UI: never on
the web surface, and never to the model.

| Hook | When | What it does |
|---|---|---|
| `SessionStart` | session opens | Names every loaded file over budget, worst first |
| `PostToolUse` | after any edit | Names a file the moment this session pushes it over |
| `Stop` | end of turn | **Blocks** the stop while a file this session wrote is broken |

## What counts as broken

- **Over budget** — more than 40,000 characters (the CLI's own floor).
- **At the wall** — at or above 97.5% of it. Landing one character under the
  limit is not a fix: the next edit of any size breaks it, and whoever makes that
  edit inherits the reorganization that got skipped.
- **Unwrapped** — a line past 150 columns that could have been wrapped. Code
  fences, tables, indented blocks, headings and unbreakable URLs are exempt.
  **Off unless `CC_CLAUDE_MD_WIDTH` is set** to `1`/`true`/`yes`/`on`: it mostly
  fired on files nowhere near the budget, and a wrapping complaint sitting next
  to a size number teaches the reader to skim both.

Characters, not bytes — so `wc -c` overstates any file with non-ASCII text.

The three are independent, and each report says which one fired. A file that is
only unwrapped is reported as a width problem and told to wrap; it is never
described as being at the budget wall, and it is never handed the extraction
advice, because it has nothing to extract. A guard that overstates the finding
gets skimmed on the run where the number is real.

## Why it watches files, not tool calls

`tool_input.file_path` exists only for `Write`, `Edit` and `MultiEdit`. A
`CLAUDE.md` rewritten through Bash — a heredoc, `sed -i`, `tee`, a formatter —
names no path, so a check keyed on that field measures nothing and the Stop gate
gets an empty list. That is not hypothetical: it is how a session edited an
over-budget `CLAUDE.md` a dozen times while the guard said nothing.

So the sweep diffs a size+mtime snapshot of the candidate files after every tool
call. Watching the files is the only version of this that can't be walked around
by choosing a different tool.

## Why a plugin and not a config hook

Config is installed once, when an environment is built, so a container whose
snapshot predates a guard never receives it and has no way to fetch it. One ran
for months with a `CLAUDE.md` at 3x budget and nothing went red.

## The Stop gate can insist, but never wedge

It fires once per `(file, content)`. A file left exactly as the gate found it
never blocks twice — that's what makes a hard block safe. But a file this session
touches and *still* leaves broken is a new violation and blocks again, as is a
second file. Blocking once per session was the other half of what made the
original guard ignorable: one nag bought silence for everything after it.

`stop_hook_active` is honored, and every error path fails open.

## Installation

```bash
/plugin install claude-md-budget
```

Set `CC_CLAUDE_MD_BUDGET` to override the budget, or `0` to disable entirely.
Set `CC_CLAUDE_MD_WIDTH=1` to switch the line-width check back on.

## CI usage

The three hooks above cover a live Claude Code session. A push from anything
else -- a bot, a merge-train branch, a plain `git push` -- never runs a
session, so nothing above ever sees it. `full_scan` is for that case: same
walk (skipping `.git`/`node_modules`) as every other event -- there is no
shallower mode to fall back to -- but it means its exit code -- 0 clean, 1 a
file is genuinely over budget, never on a file merely near the wall --
because this input is never sent by Claude Code and answers to a different
caller.

```bash
printf '{"full_scan": true, "cwd": "%s"}' "$PWD" | claude-md-budget
```

CI needs the same binary this plugin ships, fetched the same way it publishes
(the `claude-md-budget#latest` orphan tag), not a hand-rolled reimplementation
of the walk or the threshold -- a duplicate is exactly what drifts, and a
drifted copy either misses a real violation or fails a build for no reason.
