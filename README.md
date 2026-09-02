# glob

Brings back the **Glob** tool that Claude Code removed in 2.1.117, as a
plugin. Works exactly like the old builtin.

## Install

```bash
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json
claude plugin install glob
```

Requires [ripgrep](https://github.com/BurntSushi/ripgrep) 13+ on PATH
(`brew install ripgrep` / `apt install ripgrep`), or set `RIPGREP_PATH`.
Linux and macOS.

## Usage

The model sees a `Glob` tool with two parameters:

| Parameter | Required | Description |
|-----------|----------|-------------|
| `pattern` | Yes | Glob pattern, e.g. `**/*.js` or `src/**/*.ts` |
| `path` | No | Directory to search; defaults to the project directory |

Results come back oldest-first by modification time. `.gitignore` is NOT
respected and dotfiles ARE listed (like the builtin; set
`CLAUDE_CODE_GLOB_NO_IGNORE=0` / `CLAUDE_CODE_GLOB_HIDDEN=0` to change).

One improvement over the builtin: an invalid glob returns ripgrep's actual
error message instead of a silent "No files found".

## Notes

- On Claude Code older than 2.1.117 the builtin still exists, so the
  plugin's tool hides itself. Force with `CC_GLOB_PLUGIN=always|never`.
- Searches time out after 20 seconds (`CLAUDE_CODE_GLOB_TIMEOUT_SECONDS`).

## License

MIT
