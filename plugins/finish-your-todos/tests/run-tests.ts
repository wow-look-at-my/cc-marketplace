#!/usr/bin/env node
// Behaviour tests for the two entry-side hooks (prompt-arm.ts, todo-gate.ts).
// The Stop-side hook is Go and is covered by go test.
//
// Every case drives the real hook as a subprocess with fabricated stdin, in an
// isolated TMPDIR, so what is proven is the shipped file rather than an
// in-process reimplementation of it.
//
// Runtime contract: node >= 22.18 (type stripping, unflagged), builtins only.

import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const HOOKS = join(dirname(dirname(fileURLToPath(import.meta.url))), "hooks");
const ARM = join(HOOKS, "prompt-arm.ts");
const GATE = join(HOOKS, "todo-gate.ts");

let failures = 0;
let checks = 0;

function fail(msg: string): void {
  failures += 1;
  process.stderr.write(`::error::${msg}\n`);
}

function check(cond: boolean, msg: string): void {
  checks += 1;
  if (!cond) fail(msg);
}

interface Run {
  status: number;
  stdout: string;
}

function run(hook: string, payload: Record<string, unknown>, tmp: string): Run {
  const res = spawnSync(process.execPath, [hook], {
    input: JSON.stringify(payload),
    encoding: "utf8",
    env: { ...process.env, TMPDIR: tmp },
  });
  return { status: res.status ?? 0, stdout: res.stdout ?? "" };
}

function decision(r: Run): string {
  if (r.stdout.trim() === "") return "";
  try {
    const o = JSON.parse(r.stdout) as { hookSpecificOutput?: { permissionDecision?: string } };
    return o.hookSpecificOutput?.permissionDecision ?? "";
  } catch {
    return "unparseable";
  }
}

function scratch(): string {
  return mkdtempSync(join(tmpdir(), "force-todos-test-"));
}

const arm = (tmp: string, prompt: string, session = "s1") =>
  run(ARM, { session_id: session, hook_event_name: "UserPromptSubmit", prompt }, tmp);

const gate = (tmp: string, toolName: string, session = "s1") =>
  run(GATE, { session_id: session, hook_event_name: "PreToolUse", tool_name: toolName, tool_input: {} }, tmp);

// ---- classification -------------------------------------------------------

// A plain instruction arms the gate.
{
  const tmp = scratch();
  arm(tmp, "add deny support to the plugin");
  check(decision(gate(tmp, "Bash")) === "deny", "an assignment must arm the gate");
  rmSync(tmp, { recursive: true, force: true });
}

// A question does not.
for (const q of [
  "why is the budget hook not firing?",
  "what does this rule do?",
  "is jq installed?",
  "how did that get past CI?",
]) {
  const tmp = scratch();
  arm(tmp, q);
  check(decision(gate(tmp, "Bash")) === "", `a question must not arm the gate: ${q}`);
  rmSync(tmp, { recursive: true, force: true });
}

// An auxiliary opener without a "?" is an instruction, not a question. This is
// the case that shipped broken: "do the thing" was read as a question because
// it starts with "do", so the assignment was never filed.
for (const p of [
  "do the thing",
  "can you add a test for that",
  "should we block python entirely",
  "make sure the readme covers it",
]) {
  const tmp = scratch();
  arm(tmp, p);
  check(decision(gate(tmp, "Bash")) === "deny", `an instruction must arm the gate: ${p}`);
  rmSync(tmp, { recursive: true, force: true });
}

// A question carrying an instruction DOES: the instruction is the part that
// gets forgotten.
{
  const tmp = scratch();
  arm(tmp, "why is that failing? fix it please");
  check(decision(gate(tmp, "Bash")) === "deny", "a question with an imperative must arm the gate");
  rmSync(tmp, { recursive: true, force: true });
}

// Acknowledgements and slash commands do not.
for (const p of ["ok", "thanks", "yes", "go ahead", "lgtm", "/goal something", "  "]) {
  const tmp = scratch();
  arm(tmp, p);
  check(decision(gate(tmp, "Bash")) === "", `an ack/command must not arm the gate: ${JSON.stringify(p)}`);
  rmSync(tmp, { recursive: true, force: true });
}

// ---- the gate -------------------------------------------------------------

// Every non-task tool is refused while the debt stands.
{
  const tmp = scratch();
  arm(tmp, "block python everywhere");
  for (const tool of ["Bash", "Edit", "Write", "Read", "Grep", "WebFetch", "mcp__github__get_me"]) {
    check(decision(gate(tmp, tool)) === "deny", `${tool} must be refused while a task is owed`);
  }
  rmSync(tmp, { recursive: true, force: true });
}

// The task tools stay callable, and reading the list does not settle the debt:
// otherwise a TaskList would buy silence without filing anything.
{
  const tmp = scratch();
  arm(tmp, "block python everywhere");
  check(decision(gate(tmp, "TaskList")) === "", "TaskList must not be refused");
  check(decision(gate(tmp, "TaskGet")) === "", "TaskGet must not be refused");
  check(decision(gate(tmp, "Bash")) === "deny", "reading the list must not settle the debt");
  rmSync(tmp, { recursive: true, force: true });
}

// Filing the task settles it, and the next tool goes through unchanged.
{
  const tmp = scratch();
  arm(tmp, "block python everywhere");
  check(decision(gate(tmp, "TaskCreate")) === "", "TaskCreate must not be refused");
  check(decision(gate(tmp, "Bash")) === "", "the gate must open once the task is filed");
  rmSync(tmp, { recursive: true, force: true });
}

// Updating an existing task settles it too: work that maps onto a filed task
// should not force a duplicate.
{
  const tmp = scratch();
  arm(tmp, "also wire that into the config");
  check(decision(gate(tmp, "TaskUpdate")) === "", "TaskUpdate must not be refused");
  check(decision(gate(tmp, "Bash")) === "", "TaskUpdate must settle the debt");
  rmSync(tmp, { recursive: true, force: true });
}

// The refusal names the assignment, so the task can be filed without scrolling.
{
  const tmp = scratch();
  arm(tmp, "add deny support to the enhanced-auto-allow plugin");
  const r = gate(tmp, "Bash");
  check(r.stdout.includes("enhanced-auto-allow"), "the refusal must quote the assignment");
  check(r.stdout.includes("TaskCreate"), "the refusal must name the way out");
  rmSync(tmp, { recursive: true, force: true });
}

// A second assignment before the first is filed does not overwrite it: the
// first unfiled instruction is the one at risk of being lost.
{
  const tmp = scratch();
  arm(tmp, "add deny support to the plugin");
  arm(tmp, "and update the readme");
  const r = gate(tmp, "Bash");
  check(r.stdout.includes("deny support"), "the first unfiled assignment must survive a follow-up");
  rmSync(tmp, { recursive: true, force: true });
}

// Sessions do not leak into each other.
{
  const tmp = scratch();
  arm(tmp, "do the thing", "session-a");
  check(decision(gate(tmp, "Bash", "session-b")) === "", "another session must not inherit the debt");
  check(decision(gate(tmp, "Bash", "session-a")) === "deny", "the arming session must still be gated");
  rmSync(tmp, { recursive: true, force: true });
}

// ---- fail open ------------------------------------------------------------

{
  const tmp = scratch();
  for (const [name, payload] of [
    ["no session id", { hook_event_name: "PreToolUse", tool_name: "Bash" }],
    ["empty object", {}],
  ] as [string, Record<string, unknown>][]) {
    const r = run(GATE, payload, tmp);
    check(r.status === 0, `${name}: the gate must exit 0`);
    check(r.stdout.trim() === "", `${name}: the gate must decide nothing`);
  }

  for (const hook of [ARM, GATE]) {
    const res = spawnSync(process.execPath, [hook], {
      input: "not json at all",
      encoding: "utf8",
      env: { ...process.env, TMPDIR: tmp },
    });
    check((res.status ?? 0) === 0, `${hook}: garbage stdin must exit 0`);
    check((res.stdout ?? "").trim() === "", `${hook}: garbage stdin must produce no decision`);
  }
  rmSync(tmp, { recursive: true, force: true });
}

if (failures > 0) {
  process.stderr.write(`force-todos: ${failures} of ${checks} checks failed\n`);
  process.exit(1);
}
process.stdout.write(`force-todos: all ${checks} checks passed\n`);
