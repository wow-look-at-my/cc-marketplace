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

The cleanup-bash-cmds plugin lives at `plugins/cleanup-bash-cmds/`. It is a PreToolUse hook in bash + jq + shfmt (cross-platform, no compiled binary) that inspects and rewrites Bash tool commands before execution by parsing them: `shfmt --to-json` produces the syntax tree, a jq program transforms it, and `shfmt --from-json` regenerates the command. Commands containing a heredoc (`<<`/`<<-`, anywhere in the tree, including inside `$()`) are DENIED outright via `permissionDecision: "deny"` — herestrings (`<<<`), strings containing `<<EOF`, and `$((x << 2))` bit shifts are not affected. Otherwise it strips ALL `2>/dev/null` stderr suppression anywhere in the tree (Redirect nodes only — quoted strings and fds like `12>/dev/null` are structurally safe), kills any chain of trailing `| head ...` / `| tail ...` pipeline stages on top-level statements (never inside `$(...)`/`<(...)` captures), rewrites a trailing stdout file redirect into `| tee file` (`>> file` → `| tee -a file`; `/dev/` targets, process substitutions, and multi-stdout-redirect statements excluded), silently prepends `set -o pipefail` unless the command already enables it, and removes trailing `2>&1` (silent) plus trailing `|| true` and trailing `| grep` (announced). Strictness settings the user wrote (`set -e` etc.) are never removed. Announced rewrites carry a systemMessage listing each actual change (e.g. "removed 2>/dev/null; replaced > f with | tee f"); silent-only rewrites are emitted with `suppressOutput: true` and no message. shfmt operator numbers are version-dependent, so the hook probes them at runtime; missing tools or unparseable commands fail open (no pipefail injection into anything unparseable).

- **Hook script**: `plugins/cleanup-bash-cmds/hook.sh` — orchestration (extract command, probe ops, parse, transform, regenerate, emit)
- **AST transform**: `plugins/cleanup-bash-cmds/transform.jq` — all rewrite rules
- **Tests**: `plugins/cleanup-bash-cmds/tests/run-tests.sh` — self-contained runner (bootstraps a pinned shfmt if the machine lacks one); CI runs it via the plugin `justfile` `prebuild` recipe
- **Plugin config**: `plugins/cleanup-bash-cmds/.claude-plugin/plugin.json` — PreToolUse/Bash matcher invoking `hook.sh`

For rewrites the hook emits `hookSpecificOutput.updatedInput` WITHOUT a `permissionDecision`, so the normal permission flow evaluates the rewritten command (it does not auto-allow); only the heredoc ban emits a `permissionDecision` (`"deny"` + reason). Rewrites (`REWRITE` lines, silent ones tagged `reason="silent"`) and denies (`DENY` lines) can be logged by setting `CLEANUP_BASH_CMDS_LOG`.

## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It whitelists read-only tools via a PermissionRequest hook.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` — XML-driven Bash command whitelist
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` — Go binary that evaluates permissions
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` — hook matchers for Bash, Read/Glob/Grep, and MCP tools

When adding new whitelisted tools:
- For Bash commands: add entries to `rules.xml`
- For MCP tools or other non-Bash tools: add to the tool name allowlist in `cmd/hook.go` and add a matcher in `plugin.json`
