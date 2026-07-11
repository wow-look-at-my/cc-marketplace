# grep

Restores the builtin **Grep** tool that Claude Code disabled by default in
2.1.117, as a plugin MCP server. Behavior mirrors version 2.1.116 (the last
release that shipped the builtin by default): same description skeleton, same
input schema shape, same ripgrep invocation, same pagination and error
strings — except for a deliberately redesigned output-mode set (below).

## Installation

```bash
# Add the marketplace (if not already added)
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json

# Install
claude plugin install grep
```

## Requirements

- **ripgrep** (`rg`) on `PATH`, or `RIPGREP_PATH` pointing at a ripgrep
  binary
  - **Linux**: `apt install ripgrep`
  - **macOS**: `brew install ripgrep`
- Linux or macOS (amd64/arm64). Windows binaries are not built.

Without ripgrep, tool calls fail with:

> ripgrep not found on PATH. Install it (brew install ripgrep / apt install
> ripgrep / winget install BurntSushi.ripgrep.MSVC) or set RIPGREP_PATH to a
> ripgrep binary.

## The tool

The MCP tool is named `Grep`; because plugin MCP servers are prefixed, the
full model-visible name is **`mcp__plugin_grep_grep__Grep`**. The server
sets `alwaysLoad`, so the tool is present in the model's tool list from
turn 1 instead of being deferred behind ToolSearch.

### Output modes (amended from the builtin)

The builtin's default mode, `files_with_matches`, returned bare file paths —
a model almost always followed up with a second search to see the matching
lines. This plugin replaces it (no alias is accepted):

| Mode | Returns |
|------|---------|
| `filenames_with_matches` (**default**) | Each matching file as an unindented `path:` header, followed by that file's matching lines indented two spaces. Files newest-first, lines ascending. Honors `-n` (default true), `-i`, `glob`/`type`, and the context flags. |
| `filenames` | Exactly what the builtin's `files_with_matches` returned: bare paths, newest-first, `Found N files` header, `head_limit` capping the file count. |
| `content` | Byte-parity with the builtin: raw `path:line:text` lines from ripgrep. |
| `count` | Byte-parity with the builtin except the `-H` fix (below): `path:count` lines plus the `Found N total occurrences across M files.` trailer. |

`filenames_with_matches` example (`pattern: "needle"`, two files, `-C 1`;
matches on lines 12 and 40 of the newer file, which ends at line 40):

```
Found 2 files
src/parser.go:
  11-	// tokenize the header
  12:	tok := needle(line)
  13-	if tok == nil {
  --
  39-	// fallback
  40:	return needle(rest)
older:colon.txt:
  1:needle: found
```

- Match lines render `N:text`, context lines `N-text`, and an indented `--`
  separates non-contiguous chunks within a file (only when a context flag
  with nonzero width is in effect, mirroring ripgrep's own printer); with
  `"-n": false` the two-space indent stays but the `N:`/`N-` prefixes are
  dropped.
- Parsing rule: after the `Found N files` header, every line starting with
  two spaces belongs to the current file; any other line is the next file's
  header (strip the one trailing `:`). Unambiguous for filenames containing
  `:` or content that looks like a path.
- `head_limit`/`offset` paginate the flattened stream of match/context
  lines across all files (headers and `--` separators are not counted); a
  file whose lines are entirely cut is omitted. Default `head_limit` is
  250 lines; `0` means unlimited.
- Lines whose byte length (including the terminator) exceeds 500 render as
  ripgrep's `[Omitted long matching line]` / `[Omitted long context line]`,
  exactly like content mode's `--max-columns 500`.

### Search behavior (inherited from the 2.1.116 builtin)

- Runs ripgrep with `--hidden`, explicit `!` exclusions for
  `.git .svn .hg .bzr .jj .sl`, and `--max-columns 500`; `.gitignore` IS
  respected (no `--no-ignore`) — note this is the opposite of the sibling
  glob plugin's default. A positive `glob` parameter acts as a ripgrep
  whitelist: a gitignored or type-filtered file that directly matches it is
  still searched (builtin parity — same argv).
- `path` may be a file or a directory; missing paths return
  `Path does not exist: ... Note: your current working directory is ...`
  with a did-you-mean suggestion when a re-rooted candidate exists.
- Context precedence: `context` beats `-C` beats `-B`/`-A`. `-n` defaults
  to true. Patterns starting with `-` are passed via `-e`.
- Searches time out after 20 seconds (60 on WSL), overridable via
  `CLAUDE_CODE_GLOB_TIMEOUT_SECONDS` (the builtin's env var — it governed
  both Grep and Glob); output is capped at 20MB per stream; a timeout or
  cap kill with partial output returns the complete lines seen so far.
- Results over 20000 characters (UTF-16 units, matching the builtin's
  maxResultSizeChars) are written to a temp file and replaced by a
  `<persisted-output>` block with a ~2KB preview.

## Version gate

Old Claude Code versions still have the builtin Grep, so the server hides
its tool when it can prove the builtin exists: if the MCP client identifies
as `claude-code` with a parseable version **< 2.1.117**, `tools/list`
returns an empty list. Unknown clients, unparseable versions, and 2.1.117+
get the tool. Override with:

| `CC_GREP_PLUGIN` | Effect |
|------------------|--------|
| `auto` (default) | Gate on clientInfo as above |
| `always` | Always expose the tool |
| `never` | Never expose the tool |

## Deliberate divergences from the builtin

- **Output modes**: `files_with_matches` is gone; `filenames_with_matches`
  (grouped, the new default) and `filenames` (the old listing) replace it,
  and the tool description/schema text was surgically edited to match. The
  context flags and `-n` now apply to `filenames_with_matches` too.
- **Invalid regex/glob/type are errors**: ripgrep exit code 2 with no
  output surfaces rg's actual stderr as a tool error. The builtin silently
  reported "No matches found". Exit 2 with partial output (e.g. unreadable
  directory entries mid-search) still returns the partial results like the
  builtin.
- **count mode passes `-c -H`** (claude-code's own >= 2.1.175 fix): a
  single-file search keeps its filename prefix instead of producing a bare
  count the formatter scores as 0 files.
- ripgrep resolution: system `rg` from PATH (or `RIPGREP_PATH`), not the
  binary embedded in the claude executable; the not-found error suggests
  `RIPGREP_PATH` instead of the native-binary escape hatch.
- Oversized results persist under the OS temp directory instead of the
  session transcript's `tool-results/` directory (an MCP server has neither
  the transcript path nor the tool_use_id used for those filenames).
- The permission-rule glob exclusions and claude-internal cache exclusions
  the builtin appended are not available to a plugin and are omitted.
- Paths are not Unicode-NFC-normalized (the builtin normalizes resolved
  paths; this server is stdlib-only).
- The search root defaults to `$CLAUDE_PROJECT_DIR` (injected by Claude
  Code) rather than the session cwd; for plugin servers these are the same
  directory. (`.mcp.json` `cwd` is deliberately unset: Claude Code does not
  expand `${...}` variables in that field.)
- In `filenames_with_matches` mode, an explicitly-targeted binary file
  renders its actual matching lines (from ripgrep's JSON events) rather
  than content mode's `binary file matches ...` note; directory searches
  skip binary files entirely in both (ripgrep default).

## License

MIT
