## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It whitelists read-only tools via a PermissionRequest hook.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` -- XML-driven Bash command whitelist
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` -- Go binary that evaluates permissions
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` -- hook matchers for Bash, Read/Glob/Grep, and MCP tools

When adding new whitelisted tools:
- For Bash commands: add entries to `rules.xml`
- For MCP tools or other non-Bash tools: add to the tool name allowlist in `cmd/hook.go` and add a matcher in `plugin.json`
