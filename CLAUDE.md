# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Branch Protection

The `master` branch is protected. All changes require a pull request.

## Documentation

@README.md
@.claude-plugin/MARKETPLACE_GUIDE.md
@plugins/CREATING_PLUGINS.md
@plugins/PLUGIN_REFERENCE.md

## Schemas

@.claude-plugin/marketplace.schema.json
@plugins/example-plugin/.claude-plugin/plugin.schema.json
@plugins/example-plugin/.mcp.schema.json

## Templates

@plugins/example-plugin/.claude-plugin/plugin.template.json
@plugins/example-plugin/commands/command.template.md
@plugins/example-plugin/agents/agent.template.md
@plugins/example-plugin/skills/example-skill/SKILL.template.md
@plugins/example-plugin/.mcp.template.json
@plugins/example-plugin/README.template.md

## Cleanup Bash Cmds Plugin

The cleanup-bash-cmds plugin lives at `plugins/cleanup-bash-cmds/`. It is a PreToolUse hook in bash + jq + shfmt (cross-platform, no compiled binary) that inspects and rewrites Bash tool commands before execution by parsing them: `shfmt --to-json` produces the syntax tree, a jq program transforms it, and `shfmt --from-json` regenerates the command. Commands containing a heredoc (`<<`/`<<-`, anywhere in the tree, including inside `$()`) are DENIED outright via `permissionDecision: "deny"` — herestrings (`<<<`), strings containing `<<EOF`, and `$((x << 2))` bit shifts are not affected. Otherwise it strips ALL `2>/dev/null` stderr suppression anywhere in the tree (Redirect nodes only — quoted strings and fds like `12>/dev/null` are structurally safe), kills any chain of trailing `| head ...` / `| tail ...` pipeline stages on top-level statements (never inside `$(...)`/`<(...)` captures), rewrites a trailing stdout file redirect into `| tee file` (`>> file` → `| tee -a file`; `/dev/` targets, process substitutions, and multi-stdout-redirect statements excluded), prepends `set -o pipefail` unless the command already enables it, and removes trailing `2>&1`, trailing `|| true`, and trailing `| grep`. Strictness settings the user wrote (`set -e` etc.) are never removed. The hook is fully silent by design: every rewrite emits ONLY `updatedInput` + `suppressOutput: true` — never a `systemMessage` or `additionalContext` (a visible hook message lets the model blame the hook for its own command mistakes); only the heredoc deny returns a reason. shfmt operator numbers are version-dependent, so the hook probes them at runtime; missing tools or unparseable commands fail open (no pipefail injection into anything unparseable).

- **Hook script**: `plugins/cleanup-bash-cmds/hook.sh` — orchestration (extract command, probe ops, parse, transform, regenerate, emit)
- **AST transform**: `plugins/cleanup-bash-cmds/transform.jq` — all rewrite rules
- **Tests**: `plugins/cleanup-bash-cmds/tests/run-tests.sh` — self-contained runner (bootstraps a pinned shfmt if the machine lacks one); CI runs it via the plugin `justfile` `prebuild` recipe
- **Plugin config**: `plugins/cleanup-bash-cmds/.claude-plugin/plugin.json` — PreToolUse/Bash matcher invoking `hook.sh`

For rewrites the hook emits `hookSpecificOutput.updatedInput` WITHOUT a `permissionDecision`, so the normal permission flow evaluates the rewritten command (it does not auto-allow); only the heredoc ban emits a `permissionDecision` (`"deny"` + reason). The debug channel is `CLEANUP_BASH_CMDS_LOG`: every rewrite is logged with the rules that fired (`REWRITE ... rules="grep,pipefail"`), denies as `DENY ... reason="heredoc"`.

## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It whitelists read-only tools via a PermissionRequest hook.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` — XML-driven Bash command whitelist
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` — Go binary that evaluates permissions
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` — hook matchers for Bash, Read/Glob/Grep, and MCP tools

When adding new whitelisted tools:
- For Bash commands: add entries to `rules.xml`
- For MCP tools or other non-Bash tools: add to the tool name allowlist in `cmd/hook.go` and add a matcher in `plugin.json`

## Glob Plugin

The glob plugin lives at `plugins/glob/`. It is a Go stdio MCP server restoring the builtin Glob tool that Claude Code disabled by default in 2.1.117, mirroring 2.1.116 byte-for-byte: verbatim description and schema, ripgrep as the engine (`rg --files --glob <pat> --sort=modified --no-ignore --hidden`, so `.gitignore` is NOT respected and dotfiles/`.git` ARE listed — env-overridable via `CLAUDE_CODE_GLOB_NO_IGNORE`/`CLAUDE_CODE_GLOB_HIDDEN`), oldest-first mtime order, 25000-file cap with the verbatim truncation line, 20s (60s WSL) timeout via `CLAUDE_CODE_GLOB_TIMEOUT_SECONDS`, and >50000-char persist-to-temp-file. Requires `rg` on PATH (or `RIPGREP_PATH`); a missing ripgrep is a tool error. The MCP tool is `Glob` on server key `glob`, so the model-visible name is `mcp__plugin_glob_glob__Glob`; `.mcp.json` sets `alwaysLoad: true` so the tool is never deferred behind ToolSearch. A version gate hides the tool from `claude-code` clients whose version parses as < 2.1.117 (they still have the builtin); override with `CC_GLOB_PLUGIN=always|never|auto`.

- **Tool logic**: `plugins/glob/globtool.go` — verbatim description/schema consts, rg argv, validation, result formatting
- **Protocol / gate / runner / persist**: `plugins/glob/server.go`, `gate.go`, `rg.go`, `persist.go` — shared shape with the grep plugin
- **Tests**: `plugins/glob/*_test.go` — TestMain bootstraps a pinned ripgrep 14.1.0 when the machine lacks one (CI runners)

## Grep Plugin

The grep plugin lives at `plugins/grep/`. It is a Go stdio MCP server restoring the builtin Grep tool that Claude Code disabled by default in 2.1.117, mirroring 2.1.116 (verbatim rg argv: `--hidden`, the six VCS `!` excludes, `--max-columns 500`, `.gitignore` respected — the opposite of glob's default; context precedence `context` > `-C` > `-B`/`-A`; `-e` for dash-leading patterns; zod-style input coercions; Q46/l46 pagination with the exact result strings; 20s/60s timeout, 20MB caps, >20000-char persist) **except for a deliberately amended output-mode set**: `output_mode` is exactly `content` | `filenames_with_matches` | `filenames` | `count` — the old `files_with_matches` name is gone, no alias. `filenames_with_matches` (the default) is backed by `rg --json` and groups results per file: unindented `path:` headers newest-first, that file's match/context lines indented two spaces (`N:`/`N-` prefixes, `--` separators only when a nonzero context width is set), with `head_limit` paginating the match/context LINES across files. `filenames` returns exactly what the old `files_with_matches` returned; `content` is byte-parity; `count` adds the upstream `-c -H` single-file fix. Ripgrep exit 2 with no output (invalid regex/glob/type) surfaces rg's stderr as a tool error instead of the builtin's silent "No matches found". Requires `rg` on PATH (or `RIPGREP_PATH`). Model-visible name: `mcp__plugin_grep_grep__Grep` (`alwaysLoad: true`). Version gate: hidden for `claude-code` < 2.1.117; override `CC_GREP_PLUGIN=always|never|auto`.

- **Tool core**: `plugins/grep/greptool.go` — description/schema consts, argument parsing/coercions, rg argv, path validation
- **Mode formatting**: `plugins/grep/grepmodes.go` (content/count/filenames + Q46/l46 ports), `plugins/grep/grepfwm.go` (the grouped default mode over rg --json)
- **Protocol / gate / runner / persist**: `plugins/grep/server.go`, `gate.go`, `rg.go` (carries the exit-2 stderr divergence), `persist.go`, `paths.go`
- **Tests**: `plugins/grep/*_test.go` — byte-exact goldens for every mode, same ripgrep bootstrap as glob
