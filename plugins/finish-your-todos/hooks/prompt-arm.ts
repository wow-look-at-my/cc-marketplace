#!/usr/bin/env node
// UserPromptSubmit hook: record that the user just assigned work.
//
// This half only ARMS. It writes a per-session debt marker and injects one
// line of context; the PreToolUse half (todo-gate.ts) is what actually stops
// the session from proceeding without a task. Splitting it this way is
// deliberate: UserPromptSubmit cannot refuse a tool call, and injected context
// is exactly the kind of advice that has been ignored all along.
//
// Fails open on every path -- unparseable stdin, no session id, unwritable tmp
// -- because a broken guard must never wedge a prompt.

import process from "node:process";
import { assignsWork, parseInput, readDebt, readStdin, writeDebt } from "./todo-debt-lib.ts";

function main(): void {
  const input = parseInput(readStdin());
  if (input.sessionId === null || !assignsWork(input.prompt)) return;

  // An outstanding debt stays as it is: the first unfiled assignment is the
  // one to name, and overwriting it would lose it behind a follow-up.
  if (readDebt(input.sessionId) !== null) return;

  const prompt = input.prompt.trim().replace(/\s+/g, " ").slice(0, 400);
  writeDebt(input.sessionId, { prompt, refusals: 0 });

  process.stdout.write(
    JSON.stringify({
      hookSpecificOutput: {
        hookEventName: "UserPromptSubmit",
        additionalContext:
          "This message assigns work. File it with TaskCreate before doing anything else -- " +
          "every other tool is refused until you do. If it maps onto a task that already exists, " +
          "TaskUpdate that one (status in_progress, or an amended description) instead of filing a duplicate.",
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
