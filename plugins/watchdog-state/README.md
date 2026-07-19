# watchdog-state

MCP server giving coordinator sessions a direct read path to the session-liveness map and coordinator watchdog arm-state stored in the webhook-state KV, over the coordinator-watchdog-state bridge hook.

## Installation

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

### `watchdog_state_get`

Fetch the recorded coordinator watchdog arm state (`watchdog:<session>:state`) for a session.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | No | Session ID to inspect (default: this session's `CLAUDE_CODE_SESSION_ID`) |

### `watchdog_state_set`

Record this session's coordinator watchdog arm state (`watchdog:<own>:state`) with `armed_at` = now, an optional `fire_at` epoch, and an optional note.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `fire_at_epoch` | number | No | Epoch seconds the armed check-in timer fires at |
| `note` | string | No | Free-form note stored with the marker |

## Behavior notes

- Startup is gated on `WATCHDOG_STATE_HOOK_API_KEY`: when the variable is missing the server exits immediately with a stderr message naming it, before serving `initialize`. Provision the variable by name in the environment; never embed its value anywhere.
- Runtime failures on tool calls (bridge unreachable, malformed bridge response) return `isError` tool results — never a crash; the serve loop survives bad requests and unparseable input lines.
- The liveness verdict comes from each heartbeat value's own `ts` field, not from KV TTLs — TTLs are garbage collection, not a liveness signal.
