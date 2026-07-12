# glob

Restores the builtin **Glob** tool that Claude Code disabled by default in
2.1.117, as a plugin MCP server. Behavior mirrors version 2.1.116 (the last
release that shipped the builtin by default): same description, same input
schema, same ripgrep invocation, same sorting, same truncation and error
strings.

## Installation

```bash
# Add the marketplace (if not already added)
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json

# Install
claude plugin install glob
```

## Requirements

- **ripgrep** (`rg`) on `PATH`, or `RIPGREP_PATH` pointing at a ripgrep
  binary
  - **Linux**: `apt install ripgrep`
  - **macOS**: `brew install ripgrep`
- Any ripgrep from **13.0.0** up works (13/14/15 are tested): result
  ordering happens in the plugin, not via rg's `--sort` (which rg 13
  implemented per-directory instead of globally). The only rg-13
  difference is cosmetic — its error messages lack the `rg: ` prefix
  that 14+ prepend, visible only in surfaced error text.
- Linux or macOS (amd64/arm64). Windows binaries are not built.

Without ripgrep, tool calls fail with:

> ripgrep not found on PATH. Install it (brew install ripgrep / apt install
> ripgrep / winget install BurntSushi.ripgrep.MSVC) or set RIPGREP_PATH to a
> ripgrep binary.

## The tool

The MCP tool is named `Glob`; because plugin MCP servers are prefixed, the
full model-visible name is **`mcp__plugin_glob_glob__Glob`**. The server
sets `alwaysLoad`, so the tool is present in the model's tool list from
turn 1 instead of being deferred behind ToolSearch.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pattern` | string | Yes | The glob pattern to match files against |
| `path` | string | No | Directory to search in; defaults to the project directory |

Behavior (all inherited from the 2.1.116 builtin):

- Runs `rg --files --glob <pattern> --no-ignore --hidden` in the search
  root and sorts the results **oldest-first** (ascending mtime) in the
  plugin, so the order is identical on every ripgrep version (the
  builtin delegated to `--sort=modified`, which rg 13 applied
  per-directory rather than globally). Equal mtimes tie-break by
  locale-aware path collation — the same localeCompare port the grep
  sibling uses for its newest-first sort; the builtin left equal-mtime
  order to rg's walk order.
- `.gitignore` is **not** respected and dotfiles (including `.git/`) **are**
  included, unless overridden via `CLAUDE_CODE_GLOB_NO_IGNORE` /
  `CLAUDE_CODE_GLOB_HIDDEN` (set to `0`/`false` to disable). Note that even
  with the overrides, ripgrep treats the positive glob as a whitelist, so a
  hidden or gitignored file that directly matches the pattern is still
  returned; the overrides prune hidden/ignored directories. The builtin
  behaved identically (same argv).
- `path` is whitespace-trimmed and accepts `~` / `~/sub` (expanded to the
  home directory), exactly like the builtin's path resolution; `~user` is
  NOT expanded (the builtin didn't either) and null bytes are rejected
  with `Path contains null bytes`.
- Paths are returned relative to the project directory when under it,
  absolute otherwise.
- An absolute `pattern` overrides `path`: the portion before the first glob
  metachar becomes the search root.
- Results cap at **25000 files**, then append the line
  `(Results are truncated. Consider using a more specific path or pattern.)`
- No matches return `No files found`. **Invalid glob syntax is a tool
  error**: ripgrep exit 2 with no output surfaces rg's stderr (capped at
  4000 characters) instead of the builtin's silent `No files found` — a
  deliberate deviation shared with the grep sibling.
- Results over 50000 characters are written to a temp file and replaced by
  a `<persisted-output>` block with a ~2KB preview.
- Searches time out after 20 seconds (60 on WSL), overridable via
  `CLAUDE_CODE_GLOB_TIMEOUT_SECONDS`; a timeout with partial output returns
  the complete lines seen so far.

## Version gate

Old Claude Code versions still have the builtin Glob, so the server hides
its tool when it can prove the builtin exists: if the MCP client identifies
as `claude-code` with a parseable version **< 2.1.117**, `tools/list`
returns an empty list. Unknown clients, unparseable versions, and 2.1.117+
get the tool. Override with:

| `CC_GLOB_PLUGIN` | Effect |
|------------------|--------|
| `auto` (default) | Gate on clientInfo as above |
| `always` | Always expose the tool |
| `never` | Never expose the tool |

## Deliberate divergences from the builtin

- **Invalid glob syntax is an error**: ripgrep exit code 2 with no output
  surfaces rg's actual stderr as a tool error (capped at 4000 characters
  with a truncation note). The builtin silently reported `No files
  found`. Exit 2 with partial output still returns the partial results
  like the builtin.
- The ascending-mtime sort happens in the plugin rather than via
  `--sort=modified`: same order as the builtin under rg 14+, rg 13's
  per-directory sort quirk bypassed, deterministic collation tie-breaks
  for equal mtimes (the builtin's were rg's unspecified walk order), and
  rg gets to walk in parallel instead of `--sort`'s forced single
  thread.
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

## License

MIT
