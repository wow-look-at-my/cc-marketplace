// A PreToolUse guard over the same checks the language server reports.
//
// Why both: a diagnostic describes a file, and the model is free to not look
// at it. Twice in one session a three-line YAML comment went in, failed
// `yaml-comment-block` in CI, and took a whole pull request red with it --
// while this plugin's server was already adapting that exact rule. A finding
// nobody reads stops nothing. A refusal at the write does.
//
// It judges only the text a write ADDS, so a violation already in the file
// never blocks an unrelated edit to it. It filters nothing else: findings()
// already returns only what fails CI, and a second list of which checks to
// enforce is a list that drifts from the first. An earlier draft kept one,
// left ste-lint out of it as "prose judgement", and the very file documenting
// that choice then failed ste-lint in CI.

import { fileKind, findings, type Finding } from "./checks.ts";
import { diskContent, forget, outstanding, record } from "./ledger.ts";

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
  session_id?: unknown;
}

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
  if (fileKind(rel) === "other") return [];

  const found: Finding[] = [];
  for (const text of addedText(toolName, input)) {
    found.push(...findings(rel, text));
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

/**
 * Drops every ledger entry whose file is clean on disk now, and returns the
 * paths still carrying a violation.
 *
 * Re-reading disk is what clears the block on its own: the write that
 * fixed the file has already landed by the time the next one is judged, so
 * nothing has to be told the repair happened.
 */
export function sweep(sessionId: string, cwd: string): string[] {
  const still: string[] = [];
  for (const filePath of outstanding(sessionId)) {
    const content = diskContent(filePath);
    const rel = relativePath(filePath, cwd);
    if (content === undefined || findings(rel, content).length === 0) {
      forget(sessionId, filePath);
      continue;
    }
    still.push(filePath);
  }
  return still;
}

/** The refusal for a write aimed away from a file already known to be bad. */
export function otherFileReason(paths: string[]): string {
  return (
    "blocked: a file already carries a violation that fails CI, and this write is to a different file.\n" +
    paths.map((p) => `  ${p}`).join("\n") +
    "\nFix that first. This clears itself once the file is clean."
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
  const sessionId = typeof payload.session_id === "string" ? payload.session_id : "";
  const filePath = typeof input.file_path === "string" ? input.file_path : "";

  // Checked before anything about this write: a known-bad file elsewhere
  // outranks whatever is being written now. Walking away to another file is
  // the escape this exists to close.
  const stuck = sweep(sessionId, cwd).filter((p) => p !== filePath);
  if (stuck.length > 0) return otherFileReason(stuck);

  const found = blockingFindings(payload.tool_name, input, cwd);
  if (found.length === 0) return "";
  if (filePath !== "") record(sessionId, filePath);
  return denyReason(found);
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
