#!/usr/bin/env node
// PreToolUse hook (matcher "*"): collect the debt.
//
// While a session owes a task, every tool except the task tools is DENIED.
// This is the enforcement half, and it is a hard permissionDecision rather
// than injected advice on purpose: the model already receives a system
// reminder about the task list on most turns and reads past it. A refusal is
// not skippable.
//
// The debt is settled by TaskCreate (new work) or TaskUpdate (work that maps
// onto a task already filed). TaskList and TaskGet stay callable while blocked
// so the settling call can check for duplicates first. There is deliberately
// no "declare that there is no task" escape: an escape hatch is the hole the
// whole plugin exists to close.
//
// Fails open on every path -- unparseable stdin, no session id, unreadable
// marker -- because a broken guard must never wedge a session.

import process from "node:process";
import {
  clearDebt,
  parseInput,
  readDebt,
  readStdin,
  SETTLING_TOOLS,
  TASK_TOOLS,
  writeDebt,
} from "./todo-debt-lib.ts";

function main(): void {
  const input = parseInput(readStdin());
  if (input.sessionId === null) return;

  const debt = readDebt(input.sessionId);
  if (debt === null) return;

  if (SETTLING_TOOLS.has(input.toolName)) {
    clearDebt(input.sessionId);
    return;
  }
  if (TASK_TOOLS.has(input.toolName)) return;

  writeDebt(input.sessionId, { prompt: debt.prompt, refusals: debt.refusals + 1 });

  const nagged =
    debt.refusals === 0
      ? ""
      : " This is refusal " + (debt.refusals + 1) + " for the same outstanding task; nothing else will run until it is filed.";

  process.stdout.write(
    JSON.stringify({
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason:
          "Blocked: the user assigned work and no task was filed for it. Call TaskCreate now, with a subject naming " +
          "the outcome, then retry this tool call -- it will go through unchanged. If the work maps onto a task that " +
          "already exists, TaskUpdate that one instead. The assignment was: “" +
          debt.prompt +
          "”." +
          nagged,
      },
    }) + "\n"
  );
}

try {
  main();
} catch {
  // Fail open, always.
}
process.exit(0);
