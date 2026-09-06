// Where a write's text lands in the file it is written to.
//
// The hook judges the text a write ADDS, and for an Edit that text is a
// FRAGMENT. Judged on its own it carries no fence, no table and no list, so
// the lines of an ASCII diagram inside a ```-block read as an ordinary
// hard-wrapped paragraph -- and the wrap repair joined them into one line,
// flattening the diagram. The same blindness makes a semicolon inside a code
// block read as prose and refuse the write.
//
// So the fragment is put back where it belongs first: the file on disk with the
// edit applied, plus the line span the new text occupies. Every check then runs
// with the fences, tables and lists the file actually has, and only the findings
// inside that span are the write's own.

import { continuationLines } from "./checks.ts";
import { diskContent } from "./ledger.ts";

export interface Placement {
  /** The whole file as this write leaves it. */
  full: string;
  /** 0-based index of the first line the new text lands on. */
  start: number;
  /** 0-based index of the last line it lands on. */
  end: number;
  /** Bytes of the first line that came from before the new text. */
  prefix: number;
  /** Bytes of the last line that come from after the new text. */
  suffix: number;
}

/**
 * The placement of one Edit, or undefined when it cannot be pinned down.
 *
 * An unreadable file, a string that is not in it, and a string that is in it
 * more than once all return undefined. Guessing which occurrence an edit means
 * would put the span on the wrong lines, and a wrong span is worse here than no
 * span: the caller falls back to judging the fragment alone.
 */
export function place(
  filePath: string,
  oldString: string,
  newString: string,
): Placement | undefined {
  if (filePath === "" || oldString === "") return undefined;
  const disk = diskContent(filePath);
  if (disk === undefined) return undefined;
  const at = disk.indexOf(oldString);
  if (at === -1) return undefined;
  if (disk.indexOf(oldString, at + 1) !== -1) return undefined;

  const before = disk.slice(0, at);
  const after = disk.slice(at + oldString.length);
  const start = before.split("\n").length - 1;
  const prefix = before.length - (before.lastIndexOf("\n") + 1);
  const end = start + newString.split("\n").length - 1;
  const newline = after.indexOf("\n");
  const suffix = newline === -1 ? after.length : newline;
  return { full: before + newString + after, start, end, prefix, suffix };
}

/**
 * The write's own text with its hard wraps joined, or undefined when it has
 * none.
 *
 * The lines to join come from the WHOLE file, so a fence, a table, a heading
 * and a list all keep their line breaks even when the fragment shows none of
 * them. Only lines inside the write's own span move, so an unrelated wrap
 * elsewhere in the file is left for whoever edits that part.
 *
 * The last line of the span is never folded when text from the file follows it
 * on the same line: the join trims, and trimming there would eat bytes the
 * write never owned.
 */
export function repairWithin(p: Placement): string | undefined {
  const lines = p.full.split("\n");
  const continuation = continuationLines(p.full);
  const out: string[] = [];
  let changed = false;
  for (let i = p.start; i <= p.end; i++) {
    const foldable = i > p.start && (i < p.end || p.suffix === 0);
    if (foldable && continuation.has(i) && out.length > 0) {
      out[out.length - 1] = `${out[out.length - 1].replace(/\s+$/, "")} ${lines[i].trim()}`;
      changed = true;
      continue;
    }
    out.push(lines[i]);
  }
  if (!changed) return undefined;
  const joined = out.join("\n");
  return joined.slice(p.prefix, joined.length - p.suffix);
}
