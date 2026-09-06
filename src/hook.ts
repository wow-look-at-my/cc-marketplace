// A PreToolUse guard over the same checks the language server reports.
//
// Why both: a diagnostic describes a file, and the model is free to not look
// at it. Twice in one session a three-line YAML comment went in, failed
// `yaml-comment-block` in CI, and took a whole pull request red with it --
// while this plugin's server was already adapting that exact rule. A finding
// nobody reads stops nothing. A refusal at the write does.
//
// It judges only the text a write ADDS, so a violation already in the file
// never blocks an unrelated edit to it, and only the checks whose repair is a
// mechanical edit of that text -- see BLOCKING.

import { fileKind, findings, type Finding } from "./checks.ts";

interface ToolInput {
  file_path?: unknown;
  content?: unknown;
  new_string?: unknown;
  edits?: unknown;
}

interface HookPayload {
  hook_event_name?: unknown;
  tool_name?: unknown;
  tool_input?: unknown;
  cwd?: unknown;
}

// ste-lint is deliberately absent: its findings are prose judgements over a
// whole document, and refusing a write over one would block ordinary writing.
// These two are structural, and each names its own repair.
const BLOCKING = new Set(["yaml-comment-block", "no-tests-in-yaml"]);

const WRITE_TOOLS = new Set(["Write", "Edit", "MultiEdit", "NotebookEdit"]);

/** The text a write adds, per tool shape. Nothing else is judged. */
export function addedText(toolName: string, input: ToolInput): string[] {
  if (!WRITE_TOOLS.has(toolName)) return [];
  const texts: string[] = [];
  if (typeof input.content === "string") texts.push(input.content);
  if (typeof input.new_string === "string") texts.push(input.new_string);
  if (Array.isArray(input.edits)) {
    for (const edit of input.edits) {
      const next = (edit as { new_string?: unknown } | null)?.new_string;
      if (typeof next === "string") texts.push(next);
    }
  }
  return texts;
}

/** Path relative to the working directory, which is what fileKind reads. */
export function relativePath(filePath: string, cwd: string): string {
  if (cwd !== "" && filePath.startsWith(cwd)) {
    return filePath.slice(cwd.length).replace(/^[/\\]+/, "");
  }
  return filePath;
}

/**
 * Findings that should refuse this write, or [] to stay out of the way.
 *
 * A fragment is checked as if it were the file: a comment run is a comment
 * run wherever it sits, and judging the fragment is what keeps an existing
 * violation elsewhere in the file from blocking an unrelated edit.
 */
export function blockingFindings(
  toolName: string,
  input: ToolInput,
  cwd: string,
): Finding[] {
  const filePath = typeof input.file_path === "string" ? input.file_path : "";
  if (filePath === "") return [];
  const rel = relativePath(filePath, cwd);
  const kind = fileKind(rel);
  if (kind !== "workflow" && kind !== "action") return [];

  const found: Finding[] = [];
  for (const text of addedText(toolName, input)) {
    for (const finding of findings(rel, text)) {
      if (BLOCKING.has(finding.check)) found.push(finding);
    }
  }
  return found;
}

/** The refusal text: every finding, and the repair its own check names. */
export function denyReason(found: Finding[]): string {
  const lines = found.map((f) => `  ${f.check}: ${f.message}`);
  return (
    "blocked: this write fails a check that gates every build in the org, " +
    "so it would fail CI rather than the edit.\n" +
    lines.join("\n")
  );
}

export function decide(raw: string): string {
  let payload: HookPayload;
  try {
    payload = JSON.parse(raw) as HookPayload;
  } catch {
    return "";
  }
  if (payload.hook_event_name !== "PreToolUse") return "";
  if (typeof payload.tool_name !== "string") return "";
  const input = (payload.tool_input ?? {}) as ToolInput;
  if (typeof input !== "object" || input === null) return "";
  const cwd = typeof payload.cwd === "string" ? payload.cwd : "";

  const found = blockingFindings(payload.tool_name, input, cwd);
  return found.length === 0 ? "" : denyReason(found);
}

export async function runHook(stdin: NodeJS.ReadableStream): Promise<void> {
  const chunks: Buffer[] = [];
  for await (const chunk of stdin) chunks.push(Buffer.from(chunk));
  let reason = "";
  try {
    reason = decide(Buffer.concat(chunks).toString("utf8"));
  } catch {
    // A guard that cannot read its own payload must not wedge the session.
    reason = "";
  }
  if (reason === "") return;
  process.stdout.write(
    JSON.stringify({
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: reason,
      },
    }),
  );
}
