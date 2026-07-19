#!/usr/bin/env node
// watchdog-state: a single-file stdio MCP server (JSON-RPC 2.0, one message
// per line) giving coordinator sessions direct access to the off-VM
// webhook-runner KV through the coordinator-watchdog-state bridge hook.
//
// Destined for the wow-look-at-my/cc-marketplace `watchdog-state` plugin
// (installed into sessions via install-plugins.sh); self-contained on
// purpose -- no imports beyond node builtins, no deps, no build step.
//
// Tools:
//   liveness_map       {stale_after_secs?=300} -> per-session alive/stale map
//                      built from session:<id>:last_seen heartbeat values
//                      (verdict from each value's own ts; sorted stale-first).
//   watchdog_state_get {session_id?} -> the recorded watchdog:<id>:state
//                      marker (defaults to this session's own id).
//
// The surface is deliberately READ-ONLY: there is no watchdog_state_set.
// The PostToolUse arm hook -- which fires only on real send_later /
// create_trigger calls -- stays the sole writer of guard-trusted state, so
// a session cannot forge its own compliance through this server.
//
// Env contract, checked AT STARTUP: WATCHDOG_STATE_HOOK_API_KEY must be
// present and non-empty or the server refuses to start (one stderr line,
// exit 1) before reading stdin or serving anything -- absence of the key is
// a configuration error, not a degraded mode. WATCHDOG_STATE_HOOK_URL stays
// optional (default below). Tool-call-level isError covers RUNTIME failures
// only: bridge unreachable, malformed bridge response.
//
// NETWORK GOES THROUGH curl (child_process), DELIBERATELY: the child inherits
// HTTPS_PROXY / CA-bundle env and honors them exactly as a shell caller
// would; node's fetch does not on this runtime path.
//
// Runtime contract: executed natively by node >= 22.18 (type stripping,
// unflagged), erasable-syntax-only TypeScript, node builtins only. A serve
// loop must never die on a bad request: every per-request failure becomes a
// JSON-RPC error or an isError tool result.
import { execFileSync } from "node:child_process";
import { createInterface } from "node:readline";
import process from "node:process";

const SERVER_NAME = "watchdog-state";
const SERVER_VERSION = "1.0.0";
const WATCHDOG_STATE_HOOK_URL_DEFAULT =
  "https://hooks.pazer.io/hook/coordinator-watchdog-state";

// Startup hard-gate: refuse to serve without the bridge api key.
if ((process.env.WATCHDOG_STATE_HOOK_API_KEY ?? "") === "") {
  process.stderr.write(
    "watchdog-state: missing required env var WATCHDOG_STATE_HOOK_API_KEY; refusing to start\n"
  );
  process.exit(1);
}

type Json = Record<string, unknown>;

function asObject(v: unknown): Json | null {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Json) : null;
}

// ---------------------------------------------------------------- KV bridge

function bridgePost(envelope: string, maxTimeSecs: number): string | null {
  const apiKey = process.env.WATCHDOG_STATE_HOOK_API_KEY ?? "";
  if (apiKey === "") return null;
  const url = process.env.WATCHDOG_STATE_HOOK_URL || WATCHDOG_STATE_HOOK_URL_DEFAULT;
  try {
    return execFileSync(
      "curl",
      [
        "-sS",
        "--max-time",
        String(maxTimeSecs),
        "-H",
        `X-API-Key: ${apiKey}`,
        "-H",
        "Content-Type: application/json",
        "--data-binary",
        envelope,
        url,
      ],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"], timeout: (maxTimeSecs + 2) * 1000 }
    );
  } catch {
    return null;
  }
}

// The bridge's own response: the LAST snapshot output[] line parsing as JSON
// with ok === true.
function bridgeResult(snapshot: string): Json | null {
  try {
    const snap = JSON.parse(snapshot) as { output?: unknown };
    if (!Array.isArray(snap.output)) return null;
    let last: Json | null = null;
    for (const line of snap.output) {
      if (typeof line !== "string") continue;
      try {
        const parsed = asObject(JSON.parse(line));
        if (parsed !== null && parsed.ok === true) last = parsed;
      } catch {
        /* not JSON -- skip */
      }
    }
    return last;
  } catch {
    return null;
  }
}

// Returns the ok:true result object, or a human-readable failure string.
// The api key is guaranteed present here (startup hard-gate), so failures
// are runtime-only: unreachable bridge or malformed response.
function bridgeCall(envelope: string, maxTimeSecs: number): Json | string {
  const snapshot = bridgePost(envelope, maxTimeSecs);
  if (snapshot === null) {
    const url = process.env.WATCHDOG_STATE_HOOK_URL || WATCHDOG_STATE_HOOK_URL_DEFAULT;
    return `bridge unreachable: POST ${url} failed`;
  }
  const result = bridgeResult(snapshot);
  if (result === null) return "bridge response carried no ok:true result line";
  return result;
}

// ------------------------------------------------------------------ JSON-RPC

function respond(obj: unknown): void {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

function rpcResult(id: unknown, result: unknown): Json {
  return { jsonrpc: "2.0", id, result };
}

function rpcError(id: unknown, code: number, message: string): Json {
  return { jsonrpc: "2.0", id, error: { code, message } };
}

function toolText(text: string, isError: boolean): Json {
  return { content: [{ type: "text", text }], isError };
}

const TOOLS = {
  tools: [
    {
      name: "liveness_map",
      description:
        "Map every session heartbeat (session:<id>:last_seen) in the watchdog KV to an alive/stale verdict based on the heartbeat value timestamp. Stale entries sort first.",
      inputSchema: {
        type: "object",
        properties: {
          stale_after_secs: {
            type: "number",
            description: "Age in seconds beyond which a heartbeat counts as stale (default 300).",
          },
        },
        required: [],
      },
    },
    {
      name: "watchdog_state_get",
      description:
        "Fetch the recorded coordinator watchdog arm state (watchdog:<session>:state) for a session; defaults to this session.",
      inputSchema: {
        type: "object",
        properties: {
          session_id: {
            type: "string",
            description: "CLAUDE_CODE_SESSION_ID of the session to inspect (default: this session).",
          },
        },
        required: [],
      },
    },
  ],
};

// --------------------------------------------------------------------- tools

function toolLivenessMap(args: Json): Json {
  let stale = 300;
  const s = args.stale_after_secs;
  if (typeof s === "number" && Number.isFinite(s) && s >= 0) stale = s;
  const out = bridgeCall('{"op":"map","prefix":"session:"}', 5);
  if (typeof out === "string") return toolText(out, true);
  const map = asObject(out.value) ?? {};
  const now = Math.floor(Date.now() / 1000);
  const entries: Json[] = [];
  for (const [key, rawValue] of Object.entries(map)) {
    let v: Json;
    if (typeof rawValue === "string") {
      try {
        v = asObject(JSON.parse(rawValue)) ?? { raw: rawValue };
      } catch {
        v = { raw: rawValue };
      }
    } else {
      v = asObject(rawValue) ?? { raw: rawValue };
    }
    const ts = typeof v.ts === "number" ? v.ts : null;
    const age = ts === null ? null : now - ts;
    entries.push({
      key,
      verdict: age === null ? "unknown" : age < stale ? "alive" : "stale",
      age_secs: age,
      cc: v.cc ?? null,
      ccr: v.ccr ?? null,
      cwd: v.cwd ?? null,
      ts,
      tool: v.tool ?? null,
    });
  }
  entries.sort(
    (a, b) => Number(a.verdict !== "stale") - Number(b.verdict !== "stale")
  );
  return toolText(JSON.stringify(entries, null, 2), false);
}

function toolWatchdogStateGet(args: Json): Json {
  let sid = typeof args.session_id === "string" ? args.session_id : "";
  if (sid === "") sid = process.env.CLAUDE_CODE_SESSION_ID ?? "";
  if (sid === "") {
    return toolText("no session_id given and CLAUDE_CODE_SESSION_ID is not set", true);
  }
  const out = bridgeCall(JSON.stringify({ op: "get", key: `watchdog:${sid}:state` }), 5);
  if (typeof out === "string") return toolText(out, true);
  const value = typeof out.value === "string" ? out.value : "";
  return toolText(value === "" ? "absent" : value, false);
}

function handleToolsCall(id: unknown, params: Json): Json {
  const name = typeof params.name === "string" ? params.name : "";
  const args = asObject(params.arguments) ?? {};
  let out: Json;
  try {
    switch (name) {
      case "liveness_map":
        out = toolLivenessMap(args);
        break;
      case "watchdog_state_get":
        out = toolWatchdogStateGet(args);
        break;
      default:
        out = toolText(`unknown tool: ${name === "" ? "<none>" : name}`, true);
    }
  } catch (err) {
    out = toolText(`internal error handling tool ${name === "" ? "<none>" : name}: ${String(err)}`, true);
  }
  return rpcResult(id, out);
}

// ---------------------------------------------------------------- serve loop

const rl = createInterface({ input: process.stdin, terminal: false });

rl.on("line", (line: string) => {
  if (line.trim() === "") return;
  let msg: Json;
  try {
    const parsed = asObject(JSON.parse(line));
    if (parsed === null) return;
    msg = parsed;
  } catch {
    return; // unparseable line -- never crash the loop
  }
  const method = typeof msg.method === "string" ? msg.method : "";
  if (method === "") return;
  const hasId = Object.hasOwn(msg, "id");
  const id = msg.id;
  const params = asObject(msg.params) ?? {};

  try {
    if (method.startsWith("notifications/")) return;
    if (method === "initialize") {
      const pv = typeof params.protocolVersion === "string" ? params.protocolVersion : "2025-06-18";
      if (hasId) {
        respond(
          rpcResult(id, {
            protocolVersion: pv,
            capabilities: { tools: {} },
            serverInfo: { name: SERVER_NAME, version: SERVER_VERSION },
          })
        );
      }
      return;
    }
    if (method === "ping") {
      if (hasId) respond(rpcResult(id, {}));
      return;
    }
    if (method === "tools/list") {
      if (hasId) respond(rpcResult(id, TOOLS));
      return;
    }
    if (method === "tools/call") {
      if (hasId) respond(handleToolsCall(id, params));
      return;
    }
    if (hasId) respond(rpcError(id, -32601, `method not found: ${method}`));
  } catch (err) {
    // Absolute belt: even a respond() failure must not kill the loop.
    try {
      if (hasId) respond(rpcError(id, -32603, `internal error: ${String(err)}`));
    } catch {
      /* give up on this one message */
    }
  }
});

rl.on("close", () => {
  process.exit(0);
});
