// Which files this plugin is allowed to judge at all.
//
// Every message this plugin sends makes one claim: this would fail the org's
// merge gate. That claim is only true of a file some repository builds. A plan
// file under ~/.claude is scratch state for one session. It sits in no
// repository, it never ships, and no CI job reads it. Refusing a write there
// spends the session's turns on a repair nothing was ever going to ask for, and
// the hard-wrap rule is actively wrong on a document read in a terminal pane.
//
// So the scope test is the repository itself: a file with no `.git` in any
// ancestor is out of scope, and so is the user's own configuration directory
// even when it happens to sit inside a work tree.
//
// Every failure answers "out of scope". A guard that cannot read a path must
// allow the write rather than wedge the session.

import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, isAbsolute, join, resolve, sep } from "node:path";

/** The user's Claude configuration directory, or undefined when there is none. */
function claudeConfigDir(): string | undefined {
  const home = process.env.HOME ?? homedir();
  if (!home) return undefined;
  return join(resolve(home), ".claude");
}

/**
 * Whether the path sits in `$HOME/.claude`.
 *
 * Deliberately narrow: it matches the resolved home prefix and nothing else. A
 * rule that excused any `.claude/` segment anywhere would excuse a repository
 * that keeps one of its own, which is a real directory CI does read.
 */
function underClaudeConfig(path: string): boolean {
  const dir = claudeConfigDir();
  if (dir === undefined) return false;
  return path === dir || path.startsWith(dir + sep);
}

/**
 * Whether any ancestor directory carries a `.git` entry.
 *
 * The entry is a directory in an ordinary clone and a FILE in a worktree or a
 * submodule, so this asks only whether the name exists.
 */
function insideWorkTree(path: string): boolean {
  return workTree(path) !== undefined;
}

/**
 * The root of the work tree the path sits in, or undefined when it is in none.
 *
 * The `.git` entry is a directory in an ordinary clone and a FILE in a worktree
 * or a submodule, so this asks only whether the name exists.
 *
 * A session in this environment normally holds several checkouts at once, and
 * anything that reasons about "the other file" has to know which project that
 * other file belongs to.
 */
export function workTree(absPath: string): string | undefined {
  try {
    if (typeof absPath !== "string" || absPath === "" || !isAbsolute(absPath)) return undefined;
    let dir = dirname(resolve(absPath));
    for (;;) {
      if (existsSync(join(dir, ".git"))) return dir;
      const parent = dirname(dir);
      if (parent === dir) return undefined;
      dir = parent;
    }
  } catch {
    return undefined;
  }
}

/**
 * Whether the checks apply to this file. The path must be ABSOLUTE: a path
 * already made relative to the working directory cannot be walked upward, and
 * the answer would be the working directory's rather than the file's.
 */
export function inScope(absPath: string): boolean {
  try {
    if (typeof absPath !== "string" || absPath === "" || !isAbsolute(absPath)) return false;
    const path = resolve(absPath);
    if (underClaudeConfig(path)) return false;
    return insideWorkTree(path);
  } catch {
    return false;
  }
}
