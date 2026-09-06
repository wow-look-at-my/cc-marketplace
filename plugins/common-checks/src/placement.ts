// Where a write's text lands in the file it is written to.
//
// The hook judges the text a write ADDS, and for an Edit that text is a
// FRAGMENT. Judged on its own it carries no fence, no table and no list, so a
// semicolon inside a code block reads as prose and refuses the write.
//
// So the fragment is put back where it belongs first: the file on disk with the
// edit applied, plus the line span the new text occupies. Every check then runs
// with the fences, tables and lists the file actually has, and only the findings
// inside that span are the write's own.

import { diskContent } from "./ledger.ts";

export interface Placement {
  /** The whole file as this write leaves it. */
  full: string;
  /** The whole file as it was before this write. */
  before: string;
  /** 0-based index of the first line the new text lands on. */
  start: number;
  /** 0-based index of the last line it lands on. */
  end: number;
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
  const end = start + newString.split("\n").length - 1;
<<<<<<< HEAD
  const newline = after.indexOf("\n");
  const suffix = newline === -1 ? after.length : newline;
  return { full: before + newString + after, before: disk, start, end, prefix, suffix };
=======
  return { full: before + newString + after, start, end };
>>>>>>> origin/master
}

