# grep

Brings back the **Grep** tool that Claude Code removed in 2.1.117, as a
plugin. Works like the old builtin, with a better default output mode.

## Install

```bash
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json
claude plugin install grep
```

Requires [ripgrep](https://github.com/BurntSushi/ripgrep) 13+ on PATH
(`brew install ripgrep` / `apt install ripgrep`), or set `RIPGREP_PATH`.
Linux and macOS.

## Usage

The model sees a `Grep` tool:

| Parameter | Required | Description |
|-----------|----------|-------------|
| `pattern` | Yes | ripgrep regex to search for |
| `path` | No | File or directory to search; defaults to the project directory |
| `glob` | No | Filter files, e.g. `*.go` or `*.{ts,tsx}` |
| `output_mode` | No | `filenames_with_matches` (default), `content`, `filenames`, `count` |
| `-A` / `-B` / `-C` / `context` | No | Context lines around matches |
| `-n` | No | Line numbers (default on) |
| `-i` | No | Case-insensitive |
| `head_limit` / `offset` | No | Pagination (default 250 lines) |
| `multiline` | No | Let patterns span lines |

The default mode groups matching lines under a `path:` header per file,
newest files first:

```
Found 2 files
src/parser.go:
  12:	tok := needle(line)
  40:	return needle(rest)
notes.txt:
  1:needle: found
```

The old builtin's default only returned bare filenames, which forced a
second search to see the matches.

- Match lines render `N:text`, context lines `N-text`, and an indented `--`
  separates non-contiguous chunks within a file (only when a context flag
  with nonzero width is in effect, mirroring ripgrep's own printer); with
  `"-n": false` the two-space indent stays but the `N:`/`N-` prefixes are
  dropped.
- Parsing rule: after the `Found N files` header, every line starting with
  two spaces belongs to the current file; any other line is the next file's
  header (strip the one trailing `:`). This holds for filenames containing
  `:` and content that looks like a path — the one blind spot is a filename
  that itself begins with two spaces, whose header line is
  indistinguishable from content.
- `head_limit`/`offset` paginate the flattened stream of match/context
  lines across all files (headers and `--` separators are not counted); a
  file whose lines are entirely cut is omitted. Default `head_limit` is
  250 lines; `0` means unlimited.
- A matching line is never dropped. The builtin's `--max-columns 500` cap
  replaced any longer line with `[Omitted long matching line]`; this plugin
  instead shows the line, bounded to ~4096 characters. A line wider than
  that renders as a 4096-rune window with an ellipsis (`…`) marking each cut
  edge — centered on the match here (rg's JSON gives the match column) so it
  stays visible however deep into the line it sits, whereas content mode has
  no column and anchors the window at the start.

`.gitignore` is respected (the opposite of the glob plugin). Searches time
out after 20 seconds (`CLAUDE_CODE_GLOB_TIMEOUT_SECONDS`).

- Runs ripgrep with `--hidden` and explicit `!` exclusions for
  `.git .svn .hg .bzr .jj .sl`; `.gitignore` IS respected (no `--no-ignore`)
  — note this is the opposite of the sibling glob plugin's default. (The
  builtin's `--max-columns 500` is dropped so long lines are shown, then
  clamped in Go; see the mode notes above.) A positive `glob` parameter acts as a ripgrep
  whitelist: a gitignored or type-filtered file that directly matches it is
  still searched (builtin parity — same argv).
- `path` may be a file or a directory; it is whitespace-trimmed and
  accepts `~` / `~/sub` (expanded to the home directory) like the
  builtin's path resolution — `~user` is NOT expanded (the builtin didn't
  either) and null bytes are rejected with `Path contains null bytes`.
  Missing paths return
  `Path does not exist: ... Note: your current working directory is ...`
  with a did-you-mean suggestion when a re-rooted candidate exists.
- `filenames_with_matches` and `filenames` order files newest-first;
  equal mtimes tie-break by the builtin's localeCompare (ported via ICU
  root collation — case-insensitive at primary strength, so `a.txt`
  sorts before `B.txt`).
- Context precedence: `context` beats `-C` beats `-B`/`-A`. `-n` defaults
  to true. Patterns starting with `-` are passed via `-e`.
- Searches time out after 20 seconds (60 on WSL), overridable via
  `CLAUDE_CODE_GLOB_TIMEOUT_SECONDS` (the builtin's env var — it governed
  both Grep and Glob); output is capped at 20MB per stream; a timeout or
  cap kill with partial output returns the complete lines seen so far.
- Results over 20000 characters (UTF-16 units, matching the builtin's
  maxResultSizeChars) are written to a temp file and replaced by a
  `<persisted-output>` block with a ~2KB preview.

Differences from the old builtin:

- Invalid regex/glob returns ripgrep's actual error message instead of a
  silent "No matches found".
- The ambiguous `type` parameter is gone; use `glob`.
- The old `files_with_matches` mode is now called `filenames`.

## Notes

- On Claude Code older than 2.1.117 the builtin still exists, so the
  plugin's tool hides itself. Force with `CC_GREP_PLUGIN=always|never`.

## License

MIT
