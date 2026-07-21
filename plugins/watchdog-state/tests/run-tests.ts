#!/usr/bin/env node
// Behavior tests for the watchdog-state plugin: the three hooks (guard,
// arm-state recorder, heartbeat) and the MCP server, exercised as real node
// child processes with fabricated hook stdin -- no network ever (poisoned
// PATH curls prove the no-network claims; a canned-snapshot curl stub drives
// the happy paths). Run by `just prebuild` (which the marketplace build
// invokes per plugin).
//
// The wire contract under test is the coordinator-watchdog-state hook's
// frozen two-action protocol (wow-look-at-my/webhooks): heartbeat envelopes
// {"action":"heartbeat","session",...} answered {"ok":true}; list answered
// {"ok":true,"sessions":[...]}; callers parse the LAST run-snapshot output
// line with ok === true.
//
// Loudness split under test: unset WATCHDOG_STATE_HOOK_API_KEY -> every
// entrypoint is a clean silent no-op (the MCP server refuses to start);
// key set + failing bridge -> the heartbeat emits a user-visible
// systemMessage (non-blocking), the server answers isError.
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import process from "node:process";
import { fileURLToPath } from "node:url";

const PLUGIN_ROOT = dirname(dirname(fileURLToPath(import.meta.url)));
const HOOKS = join(PLUGIN_ROOT, "hooks");
const MCP = join(PLUGIN_ROOT, "watchdog-state-mcp.ts");
const SCRATCH = mkdtempSync(join(tmpdir(), "watchdog-plugin-tests-"));

let failures = 0;
function pass(msg: string): void {
  process.stdout.write(`PASS: ${msg}\n`);
}
function fail(msg: string): void {
  failures += 1;
  process.stderr.write(`FAIL: ${msg}\n`);
}
function check(cond: boolean, msg: string): void {
  if (cond) pass(msg);
  else fail(msg);
}

type Env = Record<string, string | undefined>;

interface RunResult {
  status: number | null;
  stdout: string;
  stderr: string;
}

function run(script: string, stdin: string, extraEnv: Env): RunResult {
  const env: Env = { ...process.env, HOME: SCRATCH };
  // Never inherit a live bridge config from the machine running the tests.
  delete env.WATCHDOG_STATE_HOOK_API_KEY;
  delete env.WATCHDOG_STATE_HOOK_URL;
  delete env.CC_WATCHDOG_MODE;
  delete env.CLAUDE_CODE_SESSION_ID;
  delete env.CLAUDE_CODE_REMOTE;
  delete env.CLAUDE_CODE_REMOTE_SESSION_ID;
  for (const [k, v] of Object.entries(extraEnv)) {
    if (v === undefined) delete env[k];
    else env[k] = v;
  }
  const res = spawnSync("node", [script], { input: stdin, encoding: "utf8", env, timeout: 30_000 });
  return { status: res.status, stdout: res.stdout ?? "", stderr: res.stderr ?? "" };
}

function silentOk(r: RunResult): boolean {
  return r.status === 0 && r.stdout === "";
}

function parseJson(s: string): Record<string, unknown> | null {
  try {
    const v: unknown = JSON.parse(s);
    return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

// Poisoned curls. captureCurl records argv (one line per arg) then exits 7;
// cannedCurl prints a fixed run-snapshot and exits 0.
function makeCurlStub(dir: string, body: string): string {
  mkdirSync(dir, { recursive: true });
  const p = join(dir, "curl");
  writeFileSync(p, body);
  chmodSync(p, 0o755);
  return dir;
}
const CAPTURE_FILE = join(SCRATCH, "curl-args");
const capturePath = makeCurlStub(
  join(SCRATCH, "capture-bin"),
  `#!/bin/sh\nprintf '%s\\n' "$@" > "${CAPTURE_FILE}"\nexit 7\n`
);
const SNAPSHOT = JSON.stringify({
  status: "success",
  output: [
    "coordinator-watchdog-state: ok",
    JSON.stringify({
      ok: true,
      sessions: [
        { session: "s-old", ts: "2026-01-01T00:00:00.000Z", age_seconds: 9999, cc: "s-old" },
        { session: "s-new", ts: "2026-01-01T02:00:00.000Z", age_seconds: 10, cc: "s-new", tool: "Bash" },
      ],
    }),
  ],
});
const cannedPath = makeCurlStub(
  join(SCRATCH, "canned-bin"),
  `#!/bin/sh\nprintf '%s' '${SNAPSHOT.replace(/'/g, "'\\''")}'\nexit 0\n`
);
const okPath = makeCurlStub(
  join(SCRATCH, "ok-bin"),
  `#!/bin/sh\nprintf '%s' '{"status":"success","output":["{\\"ok\\":true}"]}'\nexit 0\n`
);

const DISPATCH = JSON.stringify({
  hook_event_name: "PreToolUse",
  tool_name: "mcp__claude-code-remote__send_message",
  tool_input: {},
});
const GUARD = join(HOOKS, "watchdog-guard.ts");
const STATE = join(HOOKS, "watchdog-state.ts");
const HB = join(HOOKS, "session-heartbeat.ts");

// ------------------------------------------------------------------- guard
{
  const marker = join(SCRATCH, "guard-marker");
  const base: Env = { CC_WATCHDOG_STATE: marker, WATCHDOG_STATE_HOOK_API_KEY: "test-key" };
  const now = Math.floor(Date.now() / 1000);

  // g0: unset key -> clean no-op even with no marker.
  check(silentOk(run(GUARD, DISPATCH, { CC_WATCHDOG_STATE: marker })), "guard: unset key -> silent no-op");

  // g1: no marker, soft -> additionalContext + systemMessage, no decision.
  let r = run(GUARD, DISPATCH, base);
  let out = parseJson(r.stdout);
  const hso = out === null ? null : (out.hookSpecificOutput as Record<string, unknown> | undefined);
  check(
    r.status === 0 &&
      hso !== null &&
      hso !== undefined &&
      typeof hso.additionalContext === "string" &&
      !Object.hasOwn(hso, "permissionDecision") &&
      typeof out?.systemMessage === "string",
    "guard: absent marker + soft -> additionalContext nag with systemMessage"
  );

  // g2: hard mode denies.
  r = run(GUARD, DISPATCH, { ...base, CC_WATCHDOG_MODE: "hard" });
  out = parseJson(r.stdout);
  const hso2 = out?.hookSpecificOutput as Record<string, unknown> | undefined;
  check(
    hso2?.permissionDecision === "deny" &&
      typeof hso2?.permissionDecisionReason === "string" &&
      hso2.permissionDecisionReason.includes("CC_WATCHDOG_MODE=hard"),
    "guard: absent marker + hard -> deny"
  );

  // g3-g6: marker freshness matrix.
  writeFileSync(marker, JSON.stringify({ armed_at: now, fire_at: null }) + "\n");
  check(silentOk(run(GUARD, DISPATCH, base)), "guard: fresh armed_at -> silent");
  writeFileSync(marker, JSON.stringify({ armed_at: now - 3000, fire_at: now + 600 }) + "\n");
  check(silentOk(run(GUARD, DISPATCH, base)), "guard: fire_at 10 min out -> silent");
  writeFileSync(marker, JSON.stringify({ armed_at: now - 3000, fire_at: now + 7200 }) + "\n");
  check(parseJson(run(GUARD, DISPATCH, base).stdout) !== null, "guard: fire_at beyond horizon -> nag");
  writeFileSync(marker, JSON.stringify({ armed_at: now - 7200, fire_at: null }) + "\n");
  check(parseJson(run(GUARD, DISPATCH, base).stdout) !== null, "guard: stale armed_at -> nag");

  // g7: garbage stdin -> fail-open silence.
  check(silentOk(run(GUARD, "not json {{{", base)), "guard: garbage stdin -> fail-open silence");

  // g8: the guard performs ZERO network I/O (frozen contract has no
  // arm-state verbs) -- a poisoned PATH curl must never be invoked.
  rmSync(CAPTURE_FILE, { force: true });
  rmSync(marker, { force: true });
  r = run(GUARD, DISPATCH, { ...base, PATH: `${capturePath}:${process.env.PATH ?? ""}` });
  check(
    r.status === 0 && !existsSync(CAPTURE_FILE),
    "guard: no network ever (poisoned curl not invoked)"
  );
}

// ---------------------------------------------------------- state recorder
{
  const marker = join(SCRATCH, "state-marker");
  const base: Env = { CC_WATCHDOG_STATE: marker, WATCHDOG_STATE_HOOK_API_KEY: "test-key" };
  const now = Math.floor(Date.now() / 1000);
  const fa = now + 900;

  // s0: unset key -> no marker written.
  rmSync(marker, { force: true });
  const r0 = run(
    STATE,
    JSON.stringify({ tool_name: "mcp__claude-code-remote__send_later", tool_response: { fire_at: fa } }),
    { CC_WATCHDOG_STATE: marker }
  );
  check(silentOk(r0) && !existsSync(marker), "state: unset key -> no-op, no marker");

  // s1: direct epoch fire_at; marker 0600.
  let r = run(
    STATE,
    JSON.stringify({
      tool_name: "mcp__claude-code-remote__send_later",
      tool_input: { delay_minutes: 15 },
      tool_response: { fire_at: fa },
    }),
    base
  );
  let m = parseJson(readFileSync(marker, "utf8"));
  check(
    silentOk(r) && m?.fire_at === fa && typeof m?.armed_at === "number" && (statSync(marker).mode & 0o777) === 0o600,
    "state: direct fire_at recorded, marker 0600"
  );

  // s2: the LIVE send_later reply shape (RFC3339 fire_at in an MCP text block).
  const rfc = new Date(fa * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
  run(
    STATE,
    JSON.stringify({
      tool_name: "mcp__Claude_Code_Remote__send_later",
      tool_response: { content: [{ type: "text", text: JSON.stringify({ fire_at: rfc, trigger_id: "trig_x" }) }] },
    }),
    base
  );
  m = parseJson(readFileSync(marker, "utf8"));
  check(m?.fire_at === fa, "state: MCP text-embedded RFC3339 fire_at parsed to epoch");

  // s3: delay_minutes fallback; s4: cron -> null.
  run(
    STATE,
    JSON.stringify({
      tool_name: "mcp__claude-code-remote__create_trigger",
      tool_input: { delay_minutes: 20 },
      tool_response: { content: [{ type: "text", text: "Scheduled." }] },
    }),
    base
  );
  m = parseJson(readFileSync(marker, "utf8"));
  const dmFa = typeof m?.fire_at === "number" ? (m.fire_at as number) : 0;
  check(dmFa >= now + 1195 && dmFa <= now + 1215, "state: delay_minutes fallback");
  run(
    STATE,
    JSON.stringify({
      tool_name: "mcp__claude-code-remote__create_trigger",
      tool_input: { cron_expression: "0 * * * *" },
      tool_response: { content: [{ type: "text", text: "ok" }] },
    }),
    base
  );
  m = parseJson(readFileSync(marker, "utf8"));
  check(m !== null && m.fire_at === null, "state: cron-only trigger -> fire_at null");

  // s5: delete_trigger clears.
  run(STATE, JSON.stringify({ tool_name: "mcp__claude-code-remote__delete_trigger", tool_input: {} }), base);
  check(!existsSync(marker), "state: delete_trigger removes the marker");

  // s6: zero network (poisoned curl never invoked).
  rmSync(CAPTURE_FILE, { force: true });
  r = run(
    STATE,
    JSON.stringify({ tool_name: "mcp__claude-code-remote__send_later", tool_response: { fire_at: fa } }),
    { ...base, PATH: `${capturePath}:${process.env.PATH ?? ""}` }
  );
  check(r.status === 0 && !existsSync(CAPTURE_FILE), "state: no network ever (poisoned curl not invoked)");
}

// ---------------------------------------------------------------- heartbeat
{
  const throttle = join(SCRATCH, "hb-throttle");
  const stdin = JSON.stringify({ tool_name: "Bash" });
  const base: Env = {
    CLAUDE_CODE_REMOTE: "true",
    CLAUDE_CODE_SESSION_ID: "ci-sess",
    CLAUDE_CODE_REMOTE_SESSION_ID: "cse_ci",
    WATCHDOG_STATE_HOOK_API_KEY: "test-key",
    CC_HEARTBEAT_THROTTLE_FILE: throttle,
  };

  // h0: unset key -> full no-op (no throttle file, silence).
  rmSync(throttle, { force: true });
  let r = run(HB, stdin, { ...base, WATCHDOG_STATE_HOOK_API_KEY: undefined });
  check(silentOk(r) && !existsSync(throttle), "heartbeat: unset key -> full no-op");

  // h1: non-web session -> no-op even with the key.
  r = run(HB, stdin, { ...base, CLAUDE_CODE_REMOTE: undefined });
  check(silentOk(r) && !existsSync(throttle), "heartbeat: non-web session -> no-op");

  // h2: envelope shape (captured from the poisoned curl argv) + LOUD failure.
  rmSync(CAPTURE_FILE, { force: true });
  r = run(HB, stdin, { ...base, PATH: `${capturePath}:${process.env.PATH ?? ""}` });
  const argv = existsSync(CAPTURE_FILE) ? readFileSync(CAPTURE_FILE, "utf8").split("\n") : [];
  const dataIdx = argv.indexOf("--data-binary");
  const envelope = dataIdx >= 0 ? parseJson(argv[dataIdx + 1] ?? "") : null;
  check(
    envelope !== null &&
      envelope.action === "heartbeat" &&
      envelope.session === "ci-sess" &&
      envelope.cc === "ci-sess" &&
      envelope.ccr === "cse_ci" &&
      typeof envelope.cwd === "string" &&
      envelope.tool === "Bash" &&
      !Object.hasOwn(envelope, "ts") &&
      !Object.hasOwn(envelope, "ttl"),
    "heartbeat: sends the bridge heartbeat action (server owns ts/ttl)"
  );
  const loud = parseJson(r.stdout);
  check(
    r.status === 0 &&
      existsSync(throttle) &&
      typeof loud?.systemMessage === "string" &&
      (loud.systemMessage as string).includes("heartbeat POST") &&
      (loud.systemMessage as string).includes("failed"),
    "heartbeat: key set + failing bridge -> LOUD systemMessage, non-blocking"
  );

  // h3: 60s throttle short-circuits BEFORE any network attempt, silently.
  rmSync(CAPTURE_FILE, { force: true });
  r = run(HB, stdin, { ...base, PATH: `${capturePath}:${process.env.PATH ?? ""}` });
  check(silentOk(r) && !existsSync(CAPTURE_FILE), "heartbeat: throttle short-circuits before the network");

  // h4: success (canned ok snapshot) -> completely silent.
  rmSync(throttle, { force: true });
  r = run(HB, stdin, { ...base, PATH: `${okPath}:${process.env.PATH ?? ""}` });
  check(silentOk(r), "heartbeat: bridge success -> silent");

  // h5: field clamping to the gate's 200-char bound.
  rmSync(throttle, { force: true });
  rmSync(CAPTURE_FILE, { force: true });
  // >200 chars of path built from filesystem-legal (<255 byte) segments.
  const longCwd = join(SCRATCH, "x".repeat(120), "y".repeat(120));
  mkdirSync(longCwd, { recursive: true });
  r = spawnSync("node", [HB], {
    input: stdin,
    encoding: "utf8",
    cwd: longCwd,
    env: (() => {
      const env: Env = { ...process.env, HOME: SCRATCH, ...base, PATH: `${capturePath}:${process.env.PATH ?? ""}` };
      return env as Record<string, string>;
    })(),
    timeout: 30_000,
  }) as unknown as RunResult;
  const argv2 = existsSync(CAPTURE_FILE) ? readFileSync(CAPTURE_FILE, "utf8").split("\n") : [];
  const di2 = argv2.indexOf("--data-binary");
  const env2 = di2 >= 0 ? parseJson(argv2[di2 + 1] ?? "") : null;
  check(
    typeof env2?.cwd === "string" && (env2.cwd as string).length === 200,
    "heartbeat: overlong optional fields clamped to 200 chars"
  );
}

// --------------------------------------------------------------- MCP server
{
  // m0: startup hard-gate.
  const r0 = run(MCP, "", {});
  check(
    r0.status === 1 &&
      r0.stdout === "" &&
      r0.stderr.includes("missing required env var WATCHDOG_STATE_HOOK_API_KEY"),
    "mcp: keyless startup -> refusal (exit 1, stderr line, no stdout)"
  );

  const lines = [
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}',
    '{"jsonrpc":"2.0","method":"notifications/initialized"}',
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"liveness_map","arguments":{"stale_after_secs":300}}}',
    '{"jsonrpc":"2.0","id":4,"method":"ping"}',
    'garbage not json',
    '{"jsonrpc":"2.0","id":5,"method":"nope"}',
  ].join("\n") + "\n";

  // m1: happy path against the canned list snapshot.
  const r1 = run(MCP, lines, {
    WATCHDOG_STATE_HOOK_API_KEY: "test-key",
    PATH: `${cannedPath}:${process.env.PATH ?? ""}`,
  });
  const outs = r1.stdout.trim().split("\n").map(parseJson);
  const init = outs[0]?.result as Record<string, unknown> | undefined;
  const tools = (outs[1]?.result as { tools?: unknown[] } | undefined)?.tools;
  const callRes = outs[2]?.result as { isError?: boolean; content?: Array<{ text?: string }> } | undefined;
  const mapText = callRes?.content?.[0]?.text ?? "";
  let mapEntries: Array<Record<string, unknown>> = [];
  try {
    mapEntries = JSON.parse(mapText) as Array<Record<string, unknown>>;
  } catch {
    /* asserted below */
  }
  check(
    r1.status === 0 &&
      outs.length === 5 &&
      (init?.serverInfo as { name?: string } | undefined)?.name === "watchdog-state" &&
      Array.isArray(tools) &&
      tools.length === 1,
    "mcp: initialize + single-tool tools/list"
  );
  check(
    callRes?.isError === false &&
      mapEntries.length === 2 &&
      mapEntries[0].session === "s-old" &&
      mapEntries[0].verdict === "stale" &&
      mapEntries[1].session === "s-new" &&
      mapEntries[1].verdict === "alive" &&
      mapEntries[1].age_seconds === 10,
    "mcp: liveness_map adapts the list action (verdicts from age_seconds, stale-first)"
  );
  check(
    JSON.stringify(outs[3]) === JSON.stringify({ jsonrpc: "2.0", id: 4, result: {} }) &&
      (outs[4]?.error as { code?: number } | undefined)?.code === -32601,
    "mcp: ping + unknown-method behavior intact (garbage line ignored)"
  );

  // m2: dead bridge -> isError.
  const r2 = run(MCP, lines, {
    WATCHDOG_STATE_HOOK_API_KEY: "test-key",
    WATCHDOG_STATE_HOOK_URL: "http://127.0.0.1:1",
  });
  const outs2 = r2.stdout.trim().split("\n").map(parseJson);
  const call2 = outs2[2]?.result as { isError?: boolean; content?: Array<{ text?: string }> } | undefined;
  check(
    call2?.isError === true && (call2?.content?.[0]?.text ?? "").includes("bridge unreachable"),
    "mcp: dead bridge -> isError tool result"
  );
}

rmSync(SCRATCH, { recursive: true, force: true });
if (failures > 0) {
  process.stderr.write(`${failures} test(s) FAILED\n`);
  process.exit(1);
}
process.stdout.write("ALL WATCHDOG PLUGIN TESTS PASS\n");
