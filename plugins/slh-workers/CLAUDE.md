## slh Workers Plugin

The slh-workers plugin lives at `plugins/slh-workers/`. It registers `slh mcp` as the MCP server `slh`. With that server, a Claude Code session hands implementation to slh workers on a fast model and reviews what they did. The server itself is [wow-look-at-my/simple-llm-harness](https://github.com/wow-look-at-my/simple-llm-harness)'s `slh mcp` command. Its `docs/mcp-server.md` is the contract for the tools.

- **The plugin carries no slh binary and no slh source.** `scripts/slh-mcp` downloads slh from buildhost (`simple-llm-harness/slh`) into `$CLAUDE_PLUGIN_DATA`, else `$XDG_CACHE_HOME/slh-workers`. It refreshes a copy older than a day. A failed refresh runs the older copy and says so on stderr. A failed first download exits non-zero and names the URL and the missing token.
- **The project is private.** The download sends `BUILDHOST_READ_TOKEN` when it is set. `SLH_WORKERS_URL` points the download elsewhere. `SLH_WORKERS_CONFIG` passes a `--config` to slh.
- **The launcher lives in `scripts/`, not `bin/`.** The repository `.gitignore` drops `plugins/*/bin/`, where builds stage binaries, so a launcher there is never committed. `.mcp.json` runs it directly and git carries its executable bit. A wrapper such as `sh` in front of it fails web-config's bundle check. This wants every MCP command to be a file of the plugin.
- **The worker's model is slh's `main` role.** With no slh config the server still starts, and every `start_task` fails with the reason. That keeps the reason in front of the model rather than in a log.
- **No `alwaysLoad`.** The tools defer behind ToolSearch. `skills/delegate/SKILL.md` names the exact `select:` query. A first start downloads many megabytes, and `alwaysLoad` will put that in front of every session's first turn.
- **`skills/delegate/SKILL.md` is the coordinator's method**: plan, brief, start in parallel, wait, review the diff rather than the report, continue with fixes.

The release smoke test installs the plugin but never starts its MCP server. Start it by hand after a change to the launcher: `printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | plugins/slh-workers/scripts/slh-mcp`.
