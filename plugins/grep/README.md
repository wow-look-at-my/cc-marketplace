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

`.gitignore` is respected (the opposite of the glob plugin). Searches time
out after 20 seconds (`CLAUDE_CODE_GLOB_TIMEOUT_SECONDS`).

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
