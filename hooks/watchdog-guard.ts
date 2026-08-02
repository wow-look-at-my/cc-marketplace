#!/usr/bin/env node
// PreToolUse hook: coordinator watchdog dispatch guard (Claude Code on the
// web coordinator sessions only -- the matched tools exist nowhere else).
//
// Fires on the dispatch tools (matcher in the plugin manifest):
//   mcp__webagent__start_project_session
//   mcp__claude-code-remote__send_message / mcp__Claude_Code_Remote__send_message
//
// Enforces the coordinator watchdog law: while any worker holds assigned
// work the coordinator must have a live ~15-minute check-in timer armed.
// The verdict comes from the LOCAL arm marker watchdog-state.ts records --
// the bridge hook's frozen contract (heartbeat + list only) has no arm-state
// verbs, so there is deliberately no off-VM read and this hook performs
// ZERO network I/O. After a VM reset the marker is gone and this guard nags
// until the coordinator re-arms; that is the designed behavior, and
// re-arming (send_later, +15 minutes) is idempotent.
//
// Verdicts:
//   live marker  -> silence (exit 0, no output).
//   stale/absent/unreadable -> soft mode (default): factual re-arm directive
//     via hookSpecificOutput.additionalContext + a user-visible
//     systemMessage; hard mode (CC_WATCHDOG_MODE=hard): permissionDecision
//     "deny" (blocks the dispatch even under bypassPermissions) -- hard mode
//     denies on EVERY non-live verdict, including an unreadable marker.
//
// Env gate: WATCHDOG_STATE_HOOK_API_KEY unset/empty -> the whole watchdog
// feature is unconfigured -> clean silent no-op (exit 0, no output).
//
// Runtime contract: executed natively by node >= 22.18 (type stripping,
// unflagged), erasable-syntax-only TypeScript, node builtins only, no deps.
//
// Fail-open by design: ANY unexpected error exits 0 with no stdout, and
// stdout stays pure JSON (or empty) in every path.
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import process from "node:process";

const ARM_FRESH_SECS = 1200; // a marker armed within 20 min counts as live
const WATCHDOG_HORIZON_SECS = 2700; // a fire_at more than 45 min out is not a +15 check-in

const GUARDED_TOOLS: readonly string[] = [
  "mcp__webagent__start_project_session",
  "mcp__claude-code-remote__send_message",
  "mcp__Claude_Code_Remote__send_message",
];

function num(v: unknown): number | null {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v !== "" && !Number.isNaN(Number(v))) return Number(v);
  return null;
}

// True iff the marker proves a live check-in timer:
//   (fire_at parses AND now < fire_at AND fire_at - now <= HORIZON)
//   OR (armed_at within ARM_FRESH_SECS of now).
function markerOk(markerText: string, now: number): boolean {
  try {
    const m: unknown = JSON.parse(markerText);
    if (typeof m !== "object" || m === null || Array.isArray(m)) return false;
    const rec = m as Record<string, unknown>;
    const fireAt = num(rec.fire_at);
    const armedAt = num(rec.armed_at);
    return (
      (fireAt !== null && now < fireAt && fireAt - now <= WATCHDOG_HORIZON_SECS) ||
      (armedAt !== null && now - armedAt <= ARM_FRESH_SECS)
    );
  } catch {
    return false;
  }
}

function emitNag(reason: string): void {
  const directive =
    `No live coordinator check-in timer is on record for this session: ${reason}. ` +
    "The arm record is a local marker only -- the liveness bridge deliberately stores heartbeats, not arm state -- so after a VM reset the record is gone until the coordinator re-arms, while the server-side timers themselves (send_later one-shots, triggers) survive resets. " +
    "The coordinator watchdog law requires a live ~15-minute check-in timer whenever any worker holds assigned work, armed as part of dispatching. " +
    "Re-arming via mcp__claude-code-remote__send_later (a +15 minute watchdog check-in message) is idempotent with respect to this check, and stray or duplicate timers are removable via list_triggers/delete_trigger.";
  if ((process.env.CC_WATCHDOG_MODE ?? "") === "hard") {
    const hard =
      directive +
      " This dispatch was blocked because CC_WATCHDOG_MODE=hard is set; the same dispatch can be retried immediately after a check-in timer is armed.";
    process.stdout.write(
      JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: "deny",
          permissionDecisionReason: hard,
        },
      }) + "\n"
    );
  } else {
    process.stdout.write(
      JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          additionalContext: directive,
        },
        systemMessage:
          "watchdog: no live +15 check-in timer on record -- re-arm directive injected",
      }) + "\n"
    );
  }
}

function main(): void {
  // Unconfigured -> documented clean no-op.
  if ((process.env.WATCHDOG_STATE_HOOK_API_KEY ?? "") === "") return;

  const raw = readFileSync(0, "utf8");
  const parsed: unknown = JSON.parse(raw);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return;
  const input = parsed as Record<string, unknown>;
  const tool = typeof input.tool_name === "string" ? input.tool_name : "";
  // Belt: only guard the dispatch tools, whatever the matcher fired on.
  if (!GUARDED_TOOLS.includes(tool)) return;

  const now = Math.floor(Date.now() / 1000);
  const stateFile =
    process.env.CC_WATCHDOG_STATE || join(process.env.HOME ?? "", ".claude", "watchdog-state");

  if (existsSync(stateFile)) {
    let markerText: string | null = null;
    try {
      markerText = readFileSync(stateFile, "utf8");
    } catch {
      markerText = null;
    }
    if (markerText !== null && markerOk(markerText, now)) return;
    emitNag(
      markerText === null
        ? "the local watchdog arm marker on this VM is unreadable"
        : "the local watchdog arm marker on this VM is stale"
    );
    return;
  }
  emitNag("no local watchdog arm marker exists on this VM");
}

try {
  main();
} catch {
  // Fail-open: a broken watchdog must never break the tool call it rides on.
}
process.exit(0);
