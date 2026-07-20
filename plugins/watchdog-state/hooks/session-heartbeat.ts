#!/usr/bin/env node
// PostToolUse hook (matcher "*", registered async): all-web-sessions
// liveness beacon (Claude Code on the web only -- gates on
// CLAUDE_CODE_REMOTE=true, which the local CLI never sets).
//
// After any successful tool call in a web session it sends the bridge
// hook's heartbeat action:
//   {"action":"heartbeat","session":"<id>","cc":"…","ccr":"…","cwd":"…","tool":"…"}
// The SERVER owns the stored key (session:<id>:last_seen), the timestamp
// (ts is server-stamped UTC ISO), and the TTL (fixed 1800s -- garbage
// collection only; liveness is judged from ts by the reader). Optional
// fields are clamped to the gate's 200-char bound client-side.
//
// Gates, in order: web session; WATCHDOG_STATE_HOOK_API_KEY configured
// (unset -> documented clean no-op); session id known. Then a 60s mtime
// throttle (touched BEFORE the network call, so a hung bridge can never
// make later hook firings re-attempt in a stampede).
//
// Loudness split (key set): a bridge failure -- transport error, denied
// envelope, failed run -- is LOUD: the hook stays non-blocking (exit 0) but
// emits a user-visible systemMessage naming the failure, so a broken
// liveness pipeline is never silent. Success emits nothing.
//
// Runtime contract: executed natively by node >= 22.18 (type stripping,
// unflagged), erasable-syntax-only TypeScript, node builtins only, no deps.
// The POST rides curl via PATH (which the test suite's poisoned-PATH
// no-network throttle proof relies on).
import { closeSync, mkdirSync, openSync, readFileSync, statSync, utimesSync } from "node:fs";
import { dirname, join } from "node:path";
import process from "node:process";
import { wdBridgePost, wdBridgeResult, wdBridgeUrl } from "./watchdog-kv-lib.ts";

// The bridge gate bounds every optional heartbeat field at 200 chars.
const MAX_FIELD_CHARS = 200;

function clamp(s: string): string {
  return s.length > MAX_FIELD_CHARS ? s.slice(0, MAX_FIELD_CHARS) : s;
}

function main(): void {
  if ((process.env.CLAUDE_CODE_REMOTE ?? "") !== "true") return;
  // Unconfigured -> documented clean no-op.
  if ((process.env.WATCHDOG_STATE_HOOK_API_KEY ?? "") === "") return;
  const cc = process.env.CLAUDE_CODE_SESSION_ID ?? "";
  if (cc === "") return;

  // Read stdin fully; tolerate garbage (tool name is best-effort metadata).
  let tool = "";
  try {
    const parsed: unknown = JSON.parse(readFileSync(0, "utf8"));
    if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) {
      const t = (parsed as Record<string, unknown>).tool_name;
      if (typeof t === "string") tool = t;
    }
  } catch {
    // garbage stdin -- proceed with an empty tool name
  }

  // Throttle: at most one beacon per 60s.
  const throttle =
    process.env.CC_HEARTBEAT_THROTTLE_FILE || join(process.env.HOME ?? "", ".claude", ".heartbeat-ts");
  const now = Math.floor(Date.now() / 1000);
  try {
    const mtime = Math.floor(statSync(throttle).mtimeMs / 1000);
    if (now - mtime < 60) return;
  } catch {
    // absent throttle file -- first beacon
  }
  mkdirSync(dirname(throttle), { recursive: true });
  closeSync(openSync(throttle, "a")); // BEFORE the network call
  utimesSync(throttle, now, now);

  const envelope: Record<string, string> = { action: "heartbeat", session: cc };
  envelope.cc = clamp(cc);
  const ccr = process.env.CLAUDE_CODE_REMOTE_SESSION_ID ?? "";
  if (ccr !== "") envelope.ccr = clamp(ccr);
  envelope.cwd = clamp(process.cwd());
  if (tool !== "") envelope.tool = clamp(tool);

  const snapshot = wdBridgePost(JSON.stringify(envelope), 3);
  const result = snapshot === null ? null : wdBridgeResult(snapshot);
  if (result === null) {
    // LOUD failure: key is configured, so a dead liveness pipeline must be
    // visible. Non-blocking -- the tool call already succeeded.
    process.stdout.write(
      JSON.stringify({
        systemMessage: `watchdog-state: heartbeat POST to ${wdBridgeUrl()} failed (transport error or the bridge denied/errored); session liveness is not being recorded`,
      }) + "\n"
    );
  }
}

try {
  main();
} catch {
  // Fail-open: a broken beacon must never break the tool call it rides on.
}
process.exit(0);
