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
import { place, type Placement } from "./placement.ts";

interface ToolInput {
  file_path?: unknown;
  content?: unknown;
  old_string?: unknown;
  new_string?: unknown;
  edits?: unknown;
}

/**
 * One piece of text a write adds, with where it lands in the file.
 *
 * A `content` unit is the whole file already, so it needs no placement. An
 * edit's fragment gets one when the file can be read and the string it replaces
 * appears exactly once. Without a placement the fragment is judged alone, which
 * is what this hook did before and is still the safe answer.
 */
interface Unit {
  text: string;
  placement?: Placement;
}

function unitsOf(toolName: string, input: ToolInput): Unit[] {
  if (!WRITE_TOOLS.has(toolName)) return [];
  const filePath = typeof input.file_path === "string" ? input.file_path : "";
  const units: Unit[] = [];
  if (typeof input.content === "string") units.push({ text: input.content });
  if (typeof input.new_string === "string") {
    const old = typeof input.old_string === "string" ? input.old_string : "";
    units.push({ text: input.new_string, placement: place(filePath, old, input.new_string) });
  }
  if (Array.isArray(input.edits)) {
    for (const edit of input.edits) {
      const next = (edit as { new_string?: unknown } | null)?.new_string;
      if (typeof next !== "string") continue;
      const old = (edit as { old_string?: unknown } | null)?.old_string;
      units.push({
        text: next,
        placement: place(filePath, typeof old === "string" ? old : "", next),
      });
    }
  }
  return units;
}

/** The findings this unit is answerable for. */
function findingsFor(rel: string, unit: Unit): Finding[] {
  if (unit.placement === undefined) return findings(rel, unit.text);
  const { full, start, end } = unit.placement;
  return findings(rel, full).filter((f) => f.startLine - 1 >= start && f.startLine - 1 <= end);
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
 * A fragment is checked in the file it lands in, and only the findings on the
 * lines it adds are its own. That keeps an existing violation elsewhere from
 * blocking an unrelated edit, while a fence, a table or a list the fragment
 * sits inside still counts. A fragment that cannot be placed is checked on its
 * own, as it always was.
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
  for (const unit of unitsOf(toolName, input)) {
    found.push(...findingsFor(rel, unit));
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

/** What makes two findings the same one, across the lines an edit moves. */
function identity(f: Finding): string {
  return `${f.check} ${f.message}`;
}

/**
 * Drops every ledger entry whose recorded findings are gone from disk, and
 * returns the paths still carrying one.
 *
 * Re-reading disk is what clears the block on its own: the write that fixed
 * the file has already landed by the time the next one is judged, so nothing
 * has to be told the repair happened.
 *
 * It asks after the RECORDED findings rather than the file's own. A file the
 * write never made worse is one the session cannot be asked to repair. This
 * repository's own CLAUDE.md is hard-wrapped throughout, so a whole-file test
 * on it can never pass. An entry made under that test wedged every later write
 * in the session against a file nothing could clean.
 */
export function sweep(sessionId: string, cwd: string): string[] {
  const still: string[] = [];
  for (const entry of outstanding(sessionId)) {
    const content = diskContent(entry.path);
    if (content === undefined) {
      forget(sessionId, entry.path);
      continue;
    }
    const rel = relativePath(entry.path, cwd);
    const left = new Set(findings(rel, content).map(identity));
    if (!entry.ids.some((id) => left.has(id))) {
      forget(sessionId, entry.path);
      continue;
    }
    still.push(entry.path);
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
  if (filePath !== "") record(sessionId, filePath, found.map(identity));
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
