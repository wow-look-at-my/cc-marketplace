#!/usr/bin/env node
// PostToolUse hook: coordinator watchdog arm-state recorder (Claude Code on
// the web coordinator sessions only -- the matched tools exist nowhere else).
//
// Fires on the CCR timer tools (matcher in the plugin manifest):
//   mcp__claude-code-remote__send_later / create_trigger  -> record "armed"
//   mcp__claude-code-remote__delete_trigger               -> clear the record
// (plus the mcp__Claude_Code_Remote__* server-name spelling of each).
//
// The record is a LOCAL marker file only (JSON: armed_at, fire_at, tool,
// cc_session_id; 0600, atomic tmp+rename), read back by watchdog-guard.ts.
// There is deliberately no off-VM copy: the bridge hook's frozen contract
// (wow-look-at-my/webhooks, coordinator-watchdog-state) is heartbeat + list
// ONLY -- arm state has no server-side verb, so this hook performs ZERO
// network I/O. A VM reset therefore loses the marker and the guard nags
// until the coordinator re-arms; re-arming is idempotent and cheap.
//
// Env gate: WATCHDOG_STATE_HOOK_API_KEY unset/empty -> the whole watchdog
// feature is unconfigured -> clean silent no-op (exit 0, no output, no
// marker writes).
//
// Runtime contract: executed natively by node >= 22.18 (type stripping,
// unflagged), erasable-syntax-only TypeScript, node builtins only, no deps.
//
// Fail-open by design: this is a hook -- ANY error exits 0 with no stdout
// (PostToolUse stdout on exit 0 is parsed as hook JSON; we never emit any).
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { randomBytes } from "node:crypto";
import process from "node:process";

const ARM_TOOLS: readonly string[] = [
  "mcp__claude-code-remote__send_later",
  "mcp__Claude_Code_Remote__send_later",
  "mcp__claude-code-remote__create_trigger",
  "mcp__Claude_Code_Remote__create_trigger",
];
const DISARM_TOOLS: readonly string[] = [
  "mcp__claude-code-remote__delete_trigger",
  "mcp__Claude_Code_Remote__delete_trigger",
];

type Json = Record<string, unknown>;

function asObject(v: unknown): Json | null {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Json) : null;
}

// One candidate fire-time field set: direct fields first, then the same
// nested under .trigger.
function fieldCandidates(obj: Json | null): unknown[] {
  if (obj === null) return [];
  const out: unknown[] = [
    obj.fire_at,
    obj.fires_at,
    obj.scheduled_for,
    obj.next_run_at,
    obj.run_once_at,
  ];
  const trigger = asObject(obj.trigger);
  if (trigger !== null) {
    out.push(
      trigger.fire_at,
      trigger.fires_at,
      trigger.scheduled_for,
      trigger.next_run_at,
      trigger.run_once_at
    );
  }
  return out;
}

// Normalize one candidate to epoch seconds, or null when it does not parse.
// Numeric (number or all-digits string): millisecond epochs (>= 1e12, i.e.
// 13+ digits) fold to seconds. Otherwise RFC3339/ISO via Date.parse.
function normalizeEpoch(cand: unknown): number | null {
  if (typeof cand === "number" && Number.isFinite(cand)) {
    let n = Math.floor(cand);
    if (n >= 1e12) n = Math.floor(n / 1000);
    return n;
  }
  if (typeof cand === "string" && cand !== "") {
    if (/^[0-9]+(\.[0-9]+)?$/.test(cand)) {
      let n = Math.floor(Number(cand));
      if (n >= 1e12) n = Math.floor(n / 1000);
      return n;
    }
    const ms = Date.parse(cand);
    if (!Number.isNaN(ms)) return Math.floor(ms / 1000);
  }
  return null;
}

// Best-effort extraction of the armed timer's fire time as epoch seconds.
// Sources, in order: tool_response direct/trigger fields; MCP-shaped
// tool_response content[].text blocks that are themselves JSON (the live
// send_later reply is {"fire_at":"<RFC3339>","trigger_id":"..."} in a text
// block); tool_input at / run_once_at; tool_input delay_minutes.
function parseFireAt(input: Json): number | null {
  const response = asObject(input.tool_response);
  const toolInput = asObject(input.tool_input);

  const candidates: unknown[] = [...fieldCandidates(response)];
  const content = response?.content;
  if (Array.isArray(content)) {
    for (const block of content) {
      const b = asObject(block);
      if (b === null || b.type !== "text" || typeof b.text !== "string") continue;
      try {
        candidates.push(...fieldCandidates(asObject(JSON.parse(b.text))));
      } catch {
        // text is not JSON -- skip
      }
    }
  }
  if (toolInput !== null) candidates.push(toolInput.at, toolInput.run_once_at);

  for (const cand of candidates) {
    if (cand === null || cand === undefined || cand === "") continue;
    const epoch = normalizeEpoch(cand);
    if (epoch !== null) return epoch;
  }

  const dm = toolInput?.delay_minutes;
  const dmNum =
    typeof dm === "number" && Number.isInteger(dm) && dm >= 0
      ? dm
      : typeof dm === "string" && /^[0-9]+$/.test(dm)
        ? Number(dm)
        : null;
  if (dmNum !== null) return Math.floor(Date.now() / 1000) + dmNum * 60;

  return null;
}

function atomicWrite0600(file: string, contents: string): void {
  const dir = dirname(file);
  mkdirSync(dir, { recursive: true });
  const tmp = join(dir, `.watchdog-state.${process.pid}.${randomBytes(4).toString("hex")}`);
  writeFileSync(tmp, contents, { mode: 0o600 });
  renameSync(tmp, file);
}

function main(): void {
  // Unconfigured -> documented clean no-op.
  if ((process.env.WATCHDOG_STATE_HOOK_API_KEY ?? "") === "") return;

  const raw = readFileSync(0, "utf8");
  const input = asObject(JSON.parse(raw));
  if (input === null) return;
  const tool = typeof input.tool_name === "string" ? input.tool_name : "";
  if (tool === "") return;

  const stateFile =
    process.env.CC_WATCHDOG_STATE || join(process.env.HOME ?? "", ".claude", "watchdog-state");
  let cc = process.env.CLAUDE_CODE_SESSION_ID ?? "";
  if (cc === "" && typeof input.session_id === "string") cc = input.session_id;

  if (ARM_TOOLS.includes(tool)) {
    const marker = JSON.stringify({
      armed_at: Math.floor(Date.now() / 1000),
      fire_at: parseFireAt(input),
      tool,
      cc_session_id: cc,
    });
    atomicWrite0600(stateFile, marker + "\n");
  } else if (DISARM_TOOLS.includes(tool)) {
    try {
      rmSync(stateFile, { force: true });
    } catch {
      // fail-open
    }
  }
}

try {
  main();
} catch {
  // Fail-open: a broken watchdog must never break the tool call it rides on.
}
process.exit(0);
