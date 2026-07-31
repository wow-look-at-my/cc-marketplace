## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It whitelists read-only tools via a PermissionRequest hook.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` -- XML-driven Bash command whitelist plus the `<mcpServer>` blocks
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` -- Go binary that evaluates permissions
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` -- one PermissionRequest hook on matcher `*`

The trust unit for MCP is the **server**, never a tool-name pattern. Tool names
are author-chosen, so `get_`/`query_` guarantee nothing, and a server-name glob
would extend the allowlist to servers nobody has connected yet. A glob inside a
server is used only where every tool it matches on that server's real surface
was checked read-only (hence `actions_get`/`actions_list` spelled out on
`github`, since `actions_*` would swallow `actions_run_trigger`).

When adding new whitelisted tools:
- For Bash commands: add entries to `rules.xml`
- For MCP tools: add an `<mcpServer name="...">` block to `rules.xml`. No
  `plugin.json` change is needed -- the `*` matcher already routes every tool
  through the binary
- For other non-Bash tools: add to the tool name allowlist in `cmd/hook.go`
