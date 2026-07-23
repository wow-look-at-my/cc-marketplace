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

The cleanup-bash-cmds plugin lives at `plugins/cleanup-bash-cmds/`. It is a PreToolUse hook in bash + jq + shfmt (cross-platform, no compiled binary) that inspects and rewrites Bash tool commands before execution by parsing them: `shfmt --to-json` produces the syntax tree, a jq program transforms it, and `shfmt --from-json` regenerates the command. Commands containing a heredoc (`<<`/`<<-`, anywhere in the tree, including inside `$()`) are DENIED outright via `permissionDecision: "deny"` — herestrings (`<<<`), strings containing `<<EOF`, and `$((x << 2))` bit shifts are not affected. Commands invoking `perl` are likewise DENIED (effective command name matching `^perl[0-9.]*$`, so `perl5.36`, `command perl`, and `\perl` all count; `perl` as an argument like `grep perl`/`perlcritic`, `command -v perl`, and `perl` inside a command substitution such as `echo $(perl -e 1)` are NOT denied). Otherwise it strips ALL `2>/dev/null` stderr suppression anywhere in the tree (Redirect nodes only — quoted strings and fds like `12>/dev/null` are structurally safe), kills any chain of trailing `| head ...` / `| tail ...` pipeline stages on the FINAL top-level statement only (the rightmost `&&`/`||` leaf — mid-script limiting pipes are deliberate and preserved; never inside `$(...)`/`<(...)` captures), rewrites a trailing stdout file redirect into `| tee file` on that same final statement only (`>> file` → `| tee -a file`; `/dev/` targets, process substitutions, and multi-stdout-redirect statements excluded; mid-script `> file` preserved), prepends `set -o pipefail` unless the command already enables it, removes trailing `2>&1`, trailing `|| true`, and trailing `| grep` (final-statement-anchored as well), caps every `sleep` anywhere in the tree at 3 seconds (all-literal GNU durations summing ≤ 3 are kept; everything else — over-cap sums, `$VAR`/`$( )` args, `infinity`, junk, zero args — becomes `sleep 3`, assigns/redirects preserved), and removes constant narration `echo`/`printf` whose stdout reaches the terminal (not feeding a pipe; not inside `$()`/`<()`/`>()`/assignment captures, FuncDecl bodies, or coprocs; no stdout redirect — pure-stderr `2>` redirects allowed) by rewriting the whole command to the no-op `:` (no output, exit 0, surrounding structure and redirects intact) — `echo` when every argument is constant (glob/tilde/expansion args don't count), `printf` only for the literal-print form (exactly one static-string argument with no `%`; a `%` directive, `%%`, extra args, or an expansion keep it as real formatting); the effective-command resolver sees through `command`/`builtin`/leading-`\`/quoting wrappers (`command echo`, `\printf`, `"printf"`, `command command echo` all resolve) while `command -v/-V X` is a lookup that triggers nothing. Strictness settings the user wrote (`set -e` etc.) are never removed. After regeneration the hook re-parses the cleaned command and fails open — emits no rewrite — if it somehow has fewer top-level statements than the original (belt-and-braces guard against ever executing a truncated command). The hook is fully silent by design: every rewrite emits ONLY `updatedInput` + `suppressOutput: true` — never a `systemMessage` or `additionalContext` (a visible hook message lets the model blame the hook for its own command mistakes); only the heredoc and perl denies return a reason (heredoc points at the Write/Edit tools, perl says it is banned). shfmt operator numbers are version-dependent, so the hook probes them at runtime; missing tools or unparseable commands fail open (no pipefail injection into anything unparseable).

- **Hook script**: `plugins/cleanup-bash-cmds/hook.sh` — orchestration (extract command, probe ops, parse, transform, regenerate, emit)
- **AST transform**: `plugins/cleanup-bash-cmds/transform.jq` — all rewrite rules
- **Tests**: `plugins/cleanup-bash-cmds/tests/run-tests.sh` — self-contained runner (bootstraps a pinned shfmt if the machine lacks one); CI runs it via the plugin `justfile` `prebuild` recipe
- **Plugin config**: `plugins/cleanup-bash-cmds/.claude-plugin/plugin.json` — PreToolUse/Bash matcher invoking `hook.sh`

For rewrites the hook emits `hookSpecificOutput.updatedInput` WITHOUT a `permissionDecision`, so the normal permission flow evaluates the rewritten command (it does not auto-allow); only the heredoc and perl bans emit a `permissionDecision` (`"deny"` + reason). The debug channel is `CLEANUP_BASH_CMDS_LOG`: every rewrite is logged with the rules that fired (`REWRITE ... rules="grep,pipefail"`; the narration/sleep rules log as `narration_remove`/`sleep_cap`), denies as `DENY ... reason="heredoc"` or `reason="perl"`, and statement-count fail-opens as `GUARD ... reason="stmt-count"`.

## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It whitelists read-only tools via a PermissionRequest hook.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` — XML-driven Bash command whitelist
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` — Go binary that evaluates permissions
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` — hook matchers for Bash, Read/Glob/Grep, and MCP tools

When adding new whitelisted tools:
- For Bash commands: add entries to `rules.xml`
- For MCP tools or other non-Bash tools: add to the tool name allowlist in `cmd/hook.go` and add a matcher in `plugin.json`

## Focus Please Plugin

The focus-please plugin lives at `plugins/focus-please/`. It enforces "answer the human first": when a user prompt contains a `?`, every tool call is blocked until the assistant replies in plain text. One dependency-free Go binary (`hook.go`) serves three hook events, dispatched on `hook_event_name`: **UserPromptSubmit** arms the block when the prompt contains a `?` (drops a per-session marker file and injects an `additionalContext` note) and disarms it (removes any stale marker) otherwise; **PreToolUse** (matcher `*`) emits `permissionDecision: "deny"` with a reason while the marker exists, else the no-op `{}`; **Stop** clears the marker once the assistant has replied, so the next turn's tools work. The block therefore lasts exactly one turn — ask a question and the model must reply before it can touch a tool again. The marker is `<tempdir>/focus-please/<sha256(session_id)[:16]>.pending`, keyed per session so parallel sessions never interfere; every failure path fails OPEN (no marker written, no denial emitted) so the plugin can never wedge a session shut. All three `plugin.json` hook entries point at the same `build/focus-please` binary.

- **Hook binary**: `plugins/focus-please/hook.go` — event dispatch, the arm/disarm marker lifecycle, and the UserPromptSubmit/PreToolUse output shapes
- **Tests**: `plugins/focus-please/hook_test.go` — arm/disarm, deny/allow, the full-turn cycle, session isolation, and fail-open on bad input
- **Plugin config**: `plugins/focus-please/.claude-plugin/plugin.json` — the three hook registrations (UserPromptSubmit, PreToolUse matcher `*`, Stop)

## Glob Plugin

The glob plugin lives at `plugins/glob/`. It is a Go stdio MCP server restoring the builtin Glob tool that Claude Code disabled by default in 2.1.117, mirroring 2.1.116 byte-for-byte: verbatim description and schema, ripgrep as the engine (`rg --files --glob <pat> --no-ignore --hidden`, so `.gitignore` is NOT respected and dotfiles/`.git` ARE listed — env-overridable via `CLAUDE_CODE_GLOB_NO_IGNORE`/`CLAUDE_CODE_GLOB_HIDDEN`), oldest-first mtime order **sorted in Go, not via `--sort=modified`** (identical order on rg 13/14/15 — rg 13 sorted per-directory; ties break by the localeCompare collator in `collate.go`), 25000-file cap with the verbatim truncation line, 20s (60s WSL) timeout via `CLAUDE_CODE_GLOB_TIMEOUT_SECONDS`, and >50000-char persist-to-temp-file. The `path` argument gets the builtin's Vq preprocessing (whitespace trim, `~`/`~/x` home expansion, null-byte rejection); JSON `null` pattern/path are rejected at -32602. Ripgrep exit 2 with no output (invalid glob) surfaces rg's stderr as a tool error capped at 4000 chars — same deliberate deviation as grep. Requires `rg` (13.0.0+) on PATH (or `RIPGREP_PATH`); a missing ripgrep is a tool error. The MCP tool is `Glob` on server key `glob`, so the model-visible name is `mcp__plugin_glob_glob__Glob`; `.mcp.json` sets `alwaysLoad: true` so the tool is never deferred behind ToolSearch. A version gate hides the tool from `claude-code` clients whose version parses as < 2.1.117 (they still have the builtin); override with `CC_GLOB_PLUGIN=always|never|auto`.

- **Tool logic**: `plugins/glob/globtool.go` — verbatim description/schema consts, rg argv, validation, Go-side mtime sort, result formatting
- **Protocol / gate / runner / persist / collation**: `plugins/glob/server.go`, `gate.go`, `rg.go`, `persist.go`, `collate.go` — shared (verbatim-copied) with the grep plugin
- **Tests**: `plugins/glob/*_test.go` — TestMain bootstraps a pinned ripgrep 14.1.0 when the machine lacks one (CI runners); the suite passes against rg 13.0.0, 14.1.0, and 15.1.0

## Grep Plugin

The grep plugin lives at `plugins/grep/`. It is a Go stdio MCP server restoring the builtin Grep tool that Claude Code disabled by default in 2.1.117, mirroring 2.1.116 (verbatim rg argv: `--hidden`, the six VCS `!` excludes, `.gitignore` respected — the opposite of glob's default; the builtin's `--max-columns 500` line omission is dropped so a matching line is never lost — long lines are shown and clamped in Go (`clamp.go`) to a ~4096-rune window, ellipsis-fenced, centered on the match via rg's JSON submatch column (content mode has no column and anchors the window at the start), replacing rg's `[Omitted long matching line]`; context precedence `context` > `-C` > `-B`/`-A`; `-e` for dash-leading patterns; zod-style input coercions; Vq path preprocessing — whitespace trim, `~`/`~/x` home expansion, null-byte rejection; Q46/l46 pagination with the exact result strings; newest-first mtime sort with the builtin's localeCompare tie-break ported in `collate.go` via x/text ICU root collation; 20s/60s timeout, 20MB caps, >20000-char persist) **except for a deliberately amended output-mode set**: `output_mode` is exactly `content` | `filenames_with_matches` | `filenames` | `count` — the old `files_with_matches` name is gone, no alias. `filenames_with_matches` (the default) is backed by `rg --json` and groups results per file: unindented `path:` headers newest-first, that file's match/context lines indented two spaces (`N:`/`N-` prefixes, `--` separators only when a nonzero context width is set), with `head_limit` paginating the match/context LINES across files. `filenames` returns exactly what the old `files_with_matches` returned; `content` is byte-parity; `count` adds the upstream `-c -H` single-file fix. Ripgrep exit 2 with no output (invalid regex/glob/type) surfaces rg's stderr as a tool error (capped at 4000 chars) instead of the builtin's silent "No matches found". Requires `rg` (13.0.0+) on PATH (or `RIPGREP_PATH`). Model-visible name: `mcp__plugin_grep_grep__Grep` (`alwaysLoad: true`). Version gate: hidden for `claude-code` < 2.1.117; override `CC_GREP_PLUGIN=always|never|auto`.

- **Tool core**: `plugins/grep/greptool.go` — description/schema consts, argument parsing/coercions, rg argv, path validation
- **Mode formatting**: `plugins/grep/grepmodes.go` (content/count/filenames + Q46/l46 ports), `plugins/grep/grepfwm.go` (the grouped default mode over rg --json), `plugins/grep/clamp.go` (the shared long-line clamp/window that replaced `--max-columns 500`)
- **Protocol / gate / runner / persist / collation**: `plugins/grep/server.go`, `gate.go`, `rg.go` (carries the exit-2 stderr deviation, shared verbatim with glob), `persist.go`, `paths.go`, `collate.go`
- **Tests**: `plugins/grep/*_test.go` — byte-exact goldens for every mode, same ripgrep bootstrap as glob; suite passes against rg 13.0.0, 14.1.0, and 15.1.0
