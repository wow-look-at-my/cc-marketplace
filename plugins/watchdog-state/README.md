# watchdog-state

**This plugin is ONLY for Claude Code on the web sessions running this org's session infrastructure — it depends on the private coordinator-watchdog-state webhook bridge and its API key, refuses to start without `WATCHDOG_STATE_HOOK_API_KEY`, and is useless outside that environment.**

MCP server giving those web-session coordinators a direct read path to the session-liveness map stored in the webhook-state KV, over the coordinator-watchdog-state bridge hook.

## Installation

In practice you do not install this by hand: claude-code-web-config's `install-plugins.sh` installs it automatically into Claude Code web sessions whose environment provisions the bridge key. For manual installs (only meaningful inside such a session):

```bash
# Add the marketplace (if not already added)
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json

# Install
claude plugin install watchdog-state
```

## Requirements

- `node` >= 22.18 on `PATH` — the server is a single TypeScript file run natively by node's type stripping (no build step, no dependencies)
- `curl` on `PATH` — bridge HTTPS rides the session's proxy environment
- `WATCHDOG_STATE_HOOK_API_KEY` set in the environment. The server refuses to start without it (immediate exit with a stderr line naming the variable), and every tool call requires it.
- Optional: `WATCHDOG_STATE_HOOK_URL` overrides the bridge endpoint (default `https://hooks.pazer.io/hook/coordinator-watchdog-state`).

## Tools

### `liveness_map`

Map every session heartbeat (`session:<id>:last_seen`) in the watchdog KV to an alive/stale verdict based on the heartbeat value's own timestamp. Stale entries sort first.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `stale_after_secs` | number | No | Age in seconds beyond which a heartbeat counts as stale (default 300) |

## Behavior notes

- `liveness_map` is deliberately the **only** tool — the surface is read-only and never touches the watchdog arm markers, so a session cannot forge its own compliance; the only writer of guard-trusted state is the PostToolUse arm hook, which fires only on real `send_later`/`create_trigger` calls.
- Startup is gated on `WATCHDOG_STATE_HOOK_API_KEY`: when the variable is missing the server exits immediately with a stderr message naming it, before serving `initialize`. Provision the variable by name in the environment; never embed its value anywhere.
- Runtime failures on tool calls (bridge unreachable, malformed bridge response) return `isError` tool results — never a crash; the serve loop survives bad requests and unparseable input lines.
- The liveness verdict comes from each heartbeat value's own `ts` field, not from KV TTLs — TTLs are garbage collection, not a liveness signal.
