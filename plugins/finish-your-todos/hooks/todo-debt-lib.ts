// Shared state + classification for the force-todos hooks.
//
// The problem this plugin exists for: the model is told, by system reminder,
// on most turns, to keep a task list. It reads the reminder and does not do
// it -- a whole session went by with five separate assignments given and zero
// tasks filed, because a reminder is text and text is skippable. So the debt
// is recorded in a file and collected by a PreToolUse gate: no other tool runs
// until the task exists.
//
// Runtime contract: executed natively by node >= 22.18 (type stripping,
// unflagged), erasable-syntax-only TypeScript, node builtins only, no deps.

import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export interface Debt {
  /** The prompt that assigned work, trimmed, for the block message. */
  prompt: string;
  /** How many tool calls have been refused while this debt stands. */
  refusals: number;
}

export function debtPath(sessionId: string): string {
  const key = createHash("sha256").update(sessionId).digest("hex").slice(0, 16);
  return join(tmpdir(), "force-todos", key + ".json");
}

export function readDebt(sessionId: string): Debt | null {
  try {
    const parsed: unknown = JSON.parse(readFileSync(debtPath(sessionId), "utf8"));
    if (!parsed || typeof parsed !== "object") return null;
    const o = parsed as Record<string, unknown>;
    return {
      prompt: typeof o.prompt === "string" ? o.prompt : "",
      refusals: typeof o.refusals === "number" ? o.refusals : 0,
    };
  } catch {
    return null;
  }
}

export function writeDebt(sessionId: string, debt: Debt): void {
  try {
    const path = debtPath(sessionId);
    mkdirSync(join(path, ".."), { recursive: true });
    writeFileSync(path, JSON.stringify(debt));
  } catch {
    // Losing the marker costs enforcement, not correctness.
  }
}

export function clearDebt(sessionId: string): void {
  try {
    rmSync(debtPath(sessionId), { force: true });
  } catch {
    // Same.
  }
}

/** The task tools, which must stay callable while a debt is outstanding. */
export const TASK_TOOLS = new Set(["TaskCreate", "TaskUpdate", "TaskList", "TaskGet"]);

/** Tools that pay the debt: filing new work, or claiming/retargeting existing work. */
export const SETTLING_TOOLS = new Set(["TaskCreate", "TaskUpdate"]);

// Prompts that are plainly not assignments of work. Kept deliberately narrow:
// the failure mode this plugin fixes is skipping tasks, so an ambiguous prompt
// arms the gate and costs one TaskCreate, while a wrongly-skipped one costs
// the whole point of the plugin.
// A wh-word opens a question whether or not the "?" was typed.
const WH_OPENERS = /^\s*(what|why|how|when|where|which|who|whose)\b/i;

// An auxiliary opens a question only WITH a "?" present. Without one it is
// nearly always an instruction -- "do the thing", "can you add a test",
// "should we block python" -- and reading those as questions is precisely how
// an assignment goes unfiled, which is the failure this plugin exists to stop.
const AUX_OPENERS = /^\s*(is|are|was|were|do|does|did|can|could|should|would|will|has|have|had|am)\b/i;

const ACK_ONLY = /^\s*(ok(ay)?|k|yes|yep|yeah|no|nope|sure|thanks|thank you|ty|nice|cool|great|perfect|good|lgtm|ship it|go ahead|continue|proceed|please do|sounds good|👍|\+1)[\s.!?]*$/i;

// A slash command is the CLI's own control surface, not an assignment.
const SLASH_COMMAND = /^\s*\//;

/**
 * Does this prompt assign work?
 *
 * Deliberately biased toward YES. A false positive costs one TaskCreate call;
 * a false negative is the exact failure this plugin exists to stop. The only
 * things that get a pass are pure questions, bare acknowledgements, and slash
 * commands -- and a question that also contains an imperative ("why is X? fix
 * it") still arms, because the imperative is the part that gets forgotten.
 */
export function assignsWork(prompt: string): boolean {
  const text = prompt.trim();
  if (text === "") return false;
  if (SLASH_COMMAND.test(text)) return false;
  if (ACK_ONLY.test(text)) return false;

  // A question that carries no imperative is a question.
  const isQuestion = WH_OPENERS.test(text) || (AUX_OPENERS.test(text) && text.includes("?"));
  if (isQuestion && !hasImperative(text)) return false;

  return true;
}

// Verbs that mean "do something to the codebase". Matched at the start of any
// line or clause, which is where an instruction actually sits.
const IMPERATIVES = [
  "add", "block", "build", "change", "check", "clean", "commit", "create", "delete", "deny",
  "deploy", "disable", "document", "drop", "enable", "enforce", "extract", "fix", "handle",
  "implement", "install", "make", "merge", "move", "open", "port", "publish", "push", "refactor",
  "remove", "rename", "replace", "revert", "rewrite", "run", "set", "split", "stop", "switch",
  "test", "update", "upgrade", "use", "verify", "wire", "write", "wrap",
];

function hasImperative(text: string): boolean {
  const pattern = new RegExp(
    "(^|[.!?;\\n]\\s*|\\b(?:please|also|then|and|now|just|can you|could you|make sure (?:you|to)|i want you to|i need you to)\\s+)(" +
      IMPERATIVES.join("|") +
      ")\\b",
    "i"
  );
  return pattern.test(text);
}

export function readStdin(): string {
  try {
    return readFileSync(0, "utf8");
  } catch {
    return "";
  }
}

export interface HookInput {
  sessionId: string | null;
  prompt: string;
  toolName: string;
}

export function parseInput(raw: string): HookInput {
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === "object") {
      const o = parsed as Record<string, unknown>;
      return {
        sessionId: typeof o.session_id === "string" && o.session_id !== "" ? o.session_id : null,
        prompt: typeof o.prompt === "string" ? o.prompt : "",
        toolName: typeof o.tool_name === "string" ? o.tool_name : "",
      };
    }
  } catch {
    // Garbage stdin: no session, no enforcement. Fail open.
  }
  return { sessionId: null, prompt: "", toolName: "" };
}
