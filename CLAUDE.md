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

## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It whitelists read-only tools via a PermissionRequest hook.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` — XML-driven Bash command whitelist
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` — Go binary that evaluates permissions
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` — hook matchers for Bash, Read/Glob/Grep, and MCP tools

When adding new whitelisted tools:
- For Bash commands: add entries to `rules.xml`
- For MCP tools or other non-Bash tools: add to the tool name allowlist in `cmd/hook.go` and add a matcher in `plugin.json`
