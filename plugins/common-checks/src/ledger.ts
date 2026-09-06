// The set of files known to carry a violation right now.
//
// A refusal on one write is escapable: move to another file and the finding
// is behind you. This makes it not escapable. Once a file is known bad, a
// write to any OTHER judged file is refused until that file is clean again.
//
// It is per session, so two sessions never block each other, and it lives
// under the temp directory because it describes this moment rather than the
// repository. An unwritable temp directory means no ledger, which is the
// behaviour before this existed -- never a session that cannot write.

import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

/** One directory per session, named by a hash so a session id is never a path. */
function ledgerDir(sessionId: string): string | undefined {
  if (sessionId === "") return undefined;
  const key = createHash("sha256").update(sessionId).digest("hex").slice(0, 16);
  const dir = join(tmpdir(), "common-checks", key);
  try {
    mkdirSync(dir, { recursive: true });
    return dir;
  } catch {
    return undefined;
  }
}

/** A file name that cannot escape the ledger directory. */
function entryName(filePath: string): string {
  return createHash("sha256").update(filePath).digest("hex").slice(0, 32);
}

export function record(sessionId: string, filePath: string): void {
  const dir = ledgerDir(sessionId);
  if (dir === undefined) return;
  try {
    writeFileSync(join(dir, entryName(filePath)), filePath, "utf8");
  } catch {
    // A ledger that cannot be written leaves the guard exactly as strong as
    // it was without one.
  }
}

export function forget(sessionId: string, filePath: string): void {
  const dir = ledgerDir(sessionId);
  if (dir === undefined) return;
  try {
    rmSync(join(dir, entryName(filePath)), { force: true });
  } catch {
    // Nothing to do: a stale entry is re-checked against disk on every write.
  }
}

/** Every path the ledger still holds. */
export function outstanding(sessionId: string): string[] {
  const dir = ledgerDir(sessionId);
  if (dir === undefined) return [];
  try {
    return readdirSync(dir)
      .map((name) => {
        try {
          return readFileSync(join(dir, name), "utf8");
        } catch {
          return "";
        }
      })
      .filter((path) => path !== "" && existsSync(path));
  } catch {
    return [];
  }
}

/** The file's content on disk, or undefined when it cannot be read. */
export function diskContent(filePath: string): string | undefined {
  try {
    return readFileSync(filePath, "utf8");
  } catch {
    return undefined;
  }
}
