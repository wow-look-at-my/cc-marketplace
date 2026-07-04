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

The cleanup-bash-cmds plugin lives at `plugins/cleanup-bash-cmds/`. It is a PreToolUse hook in pure bash + jq (cross-platform, no compiled binary) that rewrites Bash tool commands before execution: it strips ALL `2>/dev/null` stderr suppression anywhere in the command (including `2> /dev/null`, `2>>/dev/null`, and quoted `/dev/null` targets, while leaving multi-digit fds like `12>/dev/null` and distinct paths like `/dev/null.log` intact), and removes legacy noise (trailing `2>&1`, `|| true`, `| head/tail/grep`, leading `set -e`).

- **Hook script**: `plugins/cleanup-bash-cmds/hook.sh` — all scrub/cleanup logic (bash regex; jq only parses/builds the hook JSON)
- **Tests**: `plugins/cleanup-bash-cmds/tests/run-tests.sh` — self-contained runner; CI runs it via the plugin `justfile` `prebuild` recipe
- **Plugin config**: `plugins/cleanup-bash-cmds/.claude-plugin/plugin.json` — PreToolUse/Bash matcher invoking `hook.sh`

The hook emits `hookSpecificOutput.updatedInput` WITHOUT a `permissionDecision`, so the normal permission flow evaluates the rewritten command (it does not auto-allow). Rewrites can be logged by setting `CLEANUP_BASH_CMDS_LOG`.

## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It whitelists read-only tools via a PermissionRequest hook.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` — XML-driven Bash command whitelist
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` — Go binary that evaluates permissions
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` — hook matchers for Bash, Read/Glob/Grep, and MCP tools

When adding new whitelisted tools:
- For Bash commands: add entries to `rules.xml`
- For MCP tools or other non-Bash tools: add to the tool name allowlist in `cmd/hook.go` and add a matcher in `plugin.json`
