# watchdog-state

**This plugin is ONLY for Claude Code on the web sessions running this org's session infrastructure — it depends on the private coordinator-watchdog-state liveness bridge (wow-look-at-my/webhooks) and its API key, no-ops or refuses to start without `WATCHDOG_STATE_HOOK_API_KEY`, and is useless outside that environment.**

The complete coordinator-watchdog unit: three Claude Code hooks plus a read-only MCP tool, all speaking the bridge hook's frozen two-action contract (`{"action":"heartbeat",...}` from workers, `{"action":"list"}` from coordinators; the server owns keys, timestamps, and TTLs).

## What ships

### Hooks (registered by the plugin manifest)

- **`hooks/watchdog-guard.ts`** — PreToolUse on the coordinator dispatch tools (`mcp__webagent__start_project_session`, `mcp__claude-code-remote__send_message`, both CCR spellings). Enforces the watchdog law from the LOCAL arm marker: a live ~15-minute check-in timer means silence; stale/absent/unreadable means a factual re-arm directive via `additionalContext` + a user-visible `systemMessage` (soft, default) or `permissionDecision: "deny"` (hard, `CC_WATCHDOG_MODE=hard` — blocks even under `bypassPermissions`, and denies on every non-live verdict). The bridge deliberately stores no arm state (heartbeat + list only), so the guard performs zero network I/O and a VM reset means nagging until re-arm — by design; re-arming is idempotent.
- **`hooks/watchdog-state.ts`** — PostToolUse on `send_later`/`create_trigger`/`delete_trigger` (both CCR spellings). Records `{armed_at, fire_at, tool, cc_session_id}` to `~/.claude/watchdog-state` (atomic tmp+rename, 0600; `fire_at` parsed defensively from the tool response/input — the live `send_later` reply shape is covered). `delete_trigger` clears it. Local-only; zero network. This hook is the SOLE writer of guard-trusted state — a session cannot forge compliance through the MCP surface.
- **`hooks/session-heartbeat.ts`** — PostToolUse `*`, async, web sessions only (`CLAUDE_CODE_REMOTE=true`). Sends the bridge's heartbeat action (`session`/`cc`/`ccr`/`cwd`/`tool`, each clamped to the gate's 200-char bound); the server stamps `ts` and applies the fixed 1800s (30 min) TTL (garbage collection only — liveness is judged from `ts` by the reader). Throttled to one POST per 60s via a marker touched before the network call.

### MCP tool

- **`liveness_map`** `{stale_after_secs? = 300}` — POSTs `{"action":"list"}` and maps every session's last-seen record (server-computed `age_seconds`, server-stamped `ts`) to an alive/stale verdict, stale-first. The single tool; the surface is read-only.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `stale_after_secs` | number | No | Age in seconds beyond which a heartbeat counts as stale (default 300) |

## Loudness contract

- `WATCHDOG_STATE_HOOK_API_KEY` **unset/empty**: the feature is unconfigured — every hook is a clean, silent no-op (exit 0, no output, no writes), and the MCP server refuses to start (one stderr line naming the variable, exit 1, before serving `initialize`).
- Key **set** but the bridge unreachable/denying/erroring: **loud** — the heartbeat emits a user-visible `systemMessage` naming the failure (still non-blocking exit 0), and MCP tool calls answer `isError: true` (never a crash; the serve loop survives bad requests and unparseable lines). Never silent degradation with the key present.

## Behavior notes

- `liveness_map` is deliberately the **only** tool — the surface is read-only, so a session cannot forge its own compliance; the only writer of guard-trusted state is the PostToolUse arm hook, which fires only on real `send_later`/`create_trigger` calls.
- The liveness verdict comes from each record's server-computed age (`age_seconds` against `stale_after_secs`), never from KV TTLs — TTLs are garbage collection, not a liveness signal.
- Provision `WATCHDOG_STATE_HOOK_API_KEY` by name in the environment; never embed its value anywhere.

## Requirements

- `node` >= 22.18 on `PATH` — hooks and server are TypeScript run natively by node's type stripping (no build step, no dependencies). Never `/usr/local/bin/node` (a v20 trap on session VMs).
- `curl` on `PATH` — bridge HTTPS deliberately rides curl (child_process) so the session's `HTTPS_PROXY`/CA env is honored; node `fetch` is not used for network.
- `WATCHDOG_STATE_HOOK_API_KEY` in the environment (the bridge hook's api key, sent as `X-API-Key`). Optional `WATCHDOG_STATE_HOOK_URL` overrides the endpoint (default `https://hooks.pazer.io/hook/coordinator-watchdog-state`).

## Installation

In practice you do not install this by hand: claude-code-web-config's `install-plugins.sh` installs it automatically into Claude Code web sessions. For manual installs (only meaningful inside such a session):

```bash
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json
claude plugin install watchdog-state
```

## Tests

`just prebuild` (run per-plugin by the marketplace build) scrubs the tree for bridge-key-shaped strings, then runs `tests/run-tests.ts`: the full hook + MCP behavior matrix as real node child processes with fabricated hook stdin — poisoned-PATH curls prove the zero-network claims (guard, recorder, throttled heartbeat), a capturing curl pins the exact heartbeat envelope (server owns `ts`/TTL), a canned-snapshot curl drives the happy paths, and the keyless/loud splits are asserted on every entrypoint. No test ever touches the network.
