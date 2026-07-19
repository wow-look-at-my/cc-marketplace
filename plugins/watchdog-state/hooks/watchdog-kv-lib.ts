// Shared transport for the coordinator-watchdog plugin hooks: POSTing domain
// envelopes to the coordinator-watchdog-state hook on hooks.pazer.io
// (wow-look-at-my/webhooks, the two-action liveness registry) and parsing
// its synchronous run-snapshot response.
//
// Imported (with the explicit .ts extension, as native type stripping
// requires) by the sibling hooks; the MCP server at the plugin root carries
// its own copy on purpose (single-file server).
//
// Wire contract (from the hook's own source of truth -- gate.ts/kv.ts):
//   POST $WATCHDOG_STATE_HOOK_URL with header X-API-Key; body is exactly one
//   of the two domain envelopes:
//     {"action":"heartbeat","session":"<id>","cc"?,"ccr"?,"cwd"?,"tool"?}
//     {"action":"list"}
//   The synchronous reply is a webhook-runner run snapshot whose output[]
//   carries the hook's ONE JSON line; callers take the LAST line parsing as
//   JSON with ok === true. Everything else is a failure.
//
// NETWORK GOES THROUGH curl, DELIBERATELY: the child inherits HTTPS_PROXY /
// CA-bundle env and honors them exactly as a shell caller would (node's
// fetch does not on this runtime path), and curl resolves via PATH, which is
// what the test suite's poisoned-PATH no-network proofs rely on.
//
// Fail-soft: functions return null instead of throwing and print nothing.
// Callers own the exit-0 guarantee and the loudness policy (a hook with the
// key set surfaces bridge failures via systemMessage; without the key the
// hooks never call this at all).
//
// Env contract:
//   WATCHDOG_STATE_HOOK_API_KEY  bridge api key (sent as X-API-Key).
//   WATCHDOG_STATE_HOOK_URL      endpoint override; default below.

import { execFileSync } from "node:child_process";
import process from "node:process";

export const WATCHDOG_STATE_HOOK_URL_DEFAULT =
  "https://hooks.pazer.io/hook/coordinator-watchdog-state";

export function wdBridgeUrl(): string {
  return process.env.WATCHDOG_STATE_HOOK_URL || WATCHDOG_STATE_HOOK_URL_DEFAULT;
}

// POST one domain envelope. Returns the raw HTTP response body (a run
// snapshot JSON string), or null when the api key is unset or the transport
// fails. Never throws; never writes to stdout.
export function wdBridgePost(envelope: string, maxTimeSecs: number = 3): string | null {
  const apiKey = process.env.WATCHDOG_STATE_HOOK_API_KEY ?? "";
  if (apiKey === "") return null;
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
        wdBridgeUrl(),
      ],
      {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
        // Belt on top of curl's own --max-time, so a wedged curl can never
        // hold the hook until the hook-level timeout.
        timeout: (maxTimeSecs + 2) * 1000,
      }
    );
  } catch {
    return null;
  }
}

// Extract the bridge's own reply from a synchronous run snapshot: the LAST
// output[] line that parses as JSON with ok === true. Returns that object,
// or null when no such line exists (denied envelope, failed run, sync-
// timeout 202 body, garbage).
export function wdBridgeResult(snapshot: string): Record<string, unknown> | null {
  try {
    const snap = JSON.parse(snapshot) as { output?: unknown };
    if (!Array.isArray(snap.output)) return null;
    let last: Record<string, unknown> | null = null;
    for (const line of snap.output) {
      if (typeof line !== "string") continue;
      try {
        const parsed: unknown = JSON.parse(line);
        if (
          typeof parsed === "object" &&
          parsed !== null &&
          !Array.isArray(parsed) &&
          (parsed as Record<string, unknown>).ok === true
        ) {
          last = parsed as Record<string, unknown>;
        }
      } catch {
        // not JSON -- skip, exactly like the server-side contract describes
      }
    }
    return last;
  } catch {
    return null;
  }
}
