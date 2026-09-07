import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { decide, otherFileReason, sweep } from "./hook.ts";
import { outstanding, record } from "./ledger.ts";

const BAD_YAML = "on:\n  # one\n  # two\n  # three\n  push:\n";
const GOOD_YAML = "on:\n  # one\n  push:\n";

function session(): string {
  return `test-${Math.random().toString(36).slice(2)}-${Date.now()}`;
}

function repo(): string {
  return mkdtempSync(join(tmpdir(), "common-checks-block-"));
}

// fileKind only judges a workflow, an action manifest or markdown, so the
// fixtures live where a workflow really lives.
function write(cwd: string, name: string, body: string): string {
  const dir = join(cwd, ".github", "workflows");
  mkdirSync(dir, { recursive: true });
  const path = join(dir, name);
  writeFileSync(path, body, "utf8");
  return path;
}

function payload(sessionId: string, cwd: string, filePath: string, text: string): string {
  return JSON.stringify({
    hook_event_name: "PreToolUse",
    tool_name: "Write",
    session_id: sessionId,
    cwd,
    tool_input: { file_path: filePath, content: text },
  });
}

// The escape this closes: a refusal on one file, then move to another and the
// finding is behind you.
test("a write to another file is refused while a known file is bad", () => {
  const id = session();
  const cwd = repo();
  const bad = write(cwd, "bad.yml", BAD_YAML);
  const other = write(cwd, "other.yml", GOOD_YAML);

  const first = decide(payload(id, cwd, bad, BAD_YAML));
  assert.match(first, /yaml-comment-block/);

  const second = decide(payload(id, cwd, other, GOOD_YAML));
  assert.match(second, /already carries a violation/);
  assert.match(second, /bad\.yml/);
});

// The block has to clear by itself, or it is a wedge.
test("the block clears once the file is clean on disk", () => {
  const id = session();
  const cwd = repo();
  const bad = write(cwd, "bad.yml", BAD_YAML);
  const other = write(cwd, "other.yml", GOOD_YAML);

  assert.match(decide(payload(id, cwd, bad, BAD_YAML)), /yaml-comment-block/);
  assert.match(decide(payload(id, cwd, other, GOOD_YAML)), /already carries/);

  // The repair lands on disk, exactly as a real write would.
  writeFileSync(bad, GOOD_YAML, "utf8");

  assert.equal(decide(payload(id, cwd, other, GOOD_YAML)), "");
  assert.deepEqual(sweep(id, cwd), []);
});

// Working ON the bad file is the one thing that must stay possible.
test("the file that is bad can still be edited", () => {
  const id = session();
  const cwd = repo();
  const bad = write(cwd, "bad.yml", BAD_YAML);

  assert.match(decide(payload(id, cwd, bad, BAD_YAML)), /yaml-comment-block/);
  assert.equal(decide(payload(id, cwd, bad, GOOD_YAML)), "");
});

// A file that vanished cannot be fixed, so holding the session on it is a wedge.
test("a deleted file drops out of the ledger", () => {
  const id = session();
  const cwd = repo();
  record(id, join(cwd, "gone.yml"));
  assert.deepEqual(sweep(id, cwd), []);
});

test("sessions do not block each other", () => {
  const cwd = repo();
  const mine = session();
  const theirs = session();
  const bad = write(cwd, "bad.yml", BAD_YAML);
  const other = write(cwd, "other.yml", GOOD_YAML);

  assert.match(decide(payload(mine, cwd, bad, BAD_YAML)), /yaml-comment-block/);
  assert.equal(decide(payload(theirs, cwd, other, GOOD_YAML)), "");
});

// No session id means no ledger, which is the behaviour before this existed.
test("a payload with no session id still checks the write itself", () => {
  const cwd = repo();
  const bad = write(cwd, "bad.yml", BAD_YAML);
  assert.match(decide(payload("", cwd, bad, BAD_YAML)), /yaml-comment-block/);
  assert.deepEqual(outstanding(""), []);
});

test("the refusal names every outstanding file", () => {
  const reason = otherFileReason(["/w/a.yml", "/w/b.yml"]);
  assert.match(reason, /a\.yml/);
  assert.match(reason, /b\.yml/);
  assert.match(reason, /clears itself/);
});
