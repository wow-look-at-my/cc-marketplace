import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { addedText, blockingFindings, decide, relativePath, repairInput } from "./hook.ts";

const CWD = "/home/user/js-snippets";
const WORKFLOW = `${CWD}/.github/workflows/deploy.yml`;

function payload(extra: Record<string, unknown>): string {
  return JSON.stringify({
    hook_event_name: "PreToolUse",
    tool_name: "Edit",
    cwd: CWD,
    tool_input: { file_path: WORKFLOW, ...extra },
  });
}

// The incident, twice in one session: three comment lines above a trigger.
// It failed common-checks in CI and took the whole pull request red, while
// the language server was already adapting this very rule.
test("the three-line comment that took a pull request red is refused", () => {
  const { reason } = decide(
    payload({
      new_string: [
        "  # A preview is republished by pushing, so a branch whose last run predates a",
        "  # change in the PUBLISHING side had no way to pick it up without a commit",
        "  # that exists only to trigger one.",
        "  workflow_dispatch:",
      ].join("\n"),
    }),
  );
  assert.match(reason, /yaml-comment-block/);
  assert.match(reason, /comment lines in a row/);
});

test("one comment line above the same trigger is allowed", () => {
  const { reason } = decide(
    payload({
      new_string: "  # Republish a preview without a commit that only triggers one.\n  workflow_dispatch:",
    }),
  );
  assert.equal(reason, "");
});

// Only the added text is judged, so a violation already in the file cannot
// block an edit that has nothing to do with it.
test("an unrelated edit is not blocked by the rest of the file", () => {
  assert.equal(decide(payload({ new_string: "  timeout-minutes: 10" })).reason, "");
});

test("a non-workflow file is not judged", () => {
  const { reason } = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: CWD,
      tool_input: {
        file_path: `${CWD}/README.md`,
        content: "# one\n# two\n# three\n",
      },
    }),
  );
  assert.equal(reason, "");
});

test("an action manifest is judged like a workflow", () => {
  const { reason } = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: CWD,
      tool_input: {
        file_path: `${CWD}/.github/actions/thing/action.yml`,
        content: "runs:\n  # one\n  # two\n  using: composite\n",
      },
    }),
  );
  assert.match(reason, /yaml-comment-block/);
});

test("every write shape contributes its added text", () => {
  assert.deepEqual(addedText("Write", { content: "a" }), ["a"]);
  assert.deepEqual(addedText("Edit", { new_string: "b" }), ["b"]);
  assert.deepEqual(
    addedText("MultiEdit", { edits: [{ new_string: "c" }, { new_string: "d" }] }),
    ["c", "d"],
  );
  assert.deepEqual(addedText("Bash", { content: "a" }), []);
});

test("a MultiEdit is refused when any one of its edits violates", () => {
  const { reason } = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "MultiEdit",
      cwd: CWD,
      tool_input: {
        file_path: WORKFLOW,
        edits: [{ new_string: "  timeout-minutes: 5" }, { new_string: "  # one\n  # two\n  on: push" }],
      },
    }),
  );
  assert.match(reason, /yaml-comment-block/);
});

test("paths resolve relative to the working directory, and absolute ones survive", () => {
  assert.equal(relativePath(WORKFLOW, CWD), ".github/workflows/deploy.yml");
  assert.equal(relativePath("/elsewhere/x.yml", CWD), "/elsewhere/x.yml");
});

// A guard that wedges a session on a surprise is worse than no guard.
test("every unreadable or unrelated payload is allowed", () => {
  assert.equal(decide("not json").reason, "");
  assert.equal(decide(JSON.stringify({ hook_event_name: "Stop" })).reason, "");
  assert.equal(decide(JSON.stringify({ hook_event_name: "PreToolUse" })).reason, "");
  assert.equal(
    decide(JSON.stringify({ hook_event_name: "PreToolUse", tool_name: "Edit", tool_input: {} })).reason,
    "",
  );
});

// The whole point of the repair: a wrap is whitespace, the lines to join are
// the ones the check already names, and a round trip over that buys nothing.
test("a hard-wrapped paragraph is joined rather than refused", () => {
  const decision = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: CWD,
      tool_input: {
        file_path: `${CWD}/docs/thing.md`,
        content: "# Title\n\nA paragraph that the author wrapped\nacross two lines by hand.\n",
      },
    }),
  );
  assert.equal(decision.reason, "");
  assert.equal(
    decision.updatedInput?.content,
    "# Title\n\nA paragraph that the author wrapped across two lines by hand.\n",
  );
});

test("an already unwrapped document is passed through untouched", () => {
  const decision = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: CWD,
      tool_input: {
        file_path: `${CWD}/docs/thing.md`,
        content: "# Title\n\nOne line, one paragraph.\n",
      },
    }),
  );
  assert.equal(decision.reason, "");
  assert.equal(decision.updatedInput, undefined);
});

// A fence is not prose. Joining its lines changes what the code says.
test("a fenced code block keeps its own line breaks", () => {
  const content = "# Title\n\n```sh\nfirst\nsecond\n```\n";
  assert.equal(repairInput("Write", { content }), undefined);
});

test("every edit of a MultiEdit is repaired", () => {
  const repaired = repairInput("MultiEdit", {
    edits: [{ new_string: "One line.\n" }, { new_string: "Wrapped over\ntwo lines.\n" }],
  });
  assert.deepEqual(repaired?.edits, [
    { new_string: "One line.\n" },
    { new_string: "Wrapped over two lines.\n" },
  ]);
});

// A wrap is repaired, so it must never reach the refusal. What is left in the
// same write still does.
test("a wrap beside a real violation leaves only the real violation", () => {
  const { reason } = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: CWD,
      tool_input: {
        file_path: `${CWD}/docs/thing.md`,
        content: "A sentence that is wrapped\nby hand and that doesn't expand its contraction.\n",
      },
    }),
  );
  assert.match(reason, /contractions/);
  assert.doesNotMatch(reason, /Join it back up/);
});

// The incident this placement exists for: an Edit whose fragment sits inside a
// fenced block. Judged alone the fragment shows no fence, so its lines read as
// a hand-wrapped paragraph and the repair flattened a diagram into one line.
test("an edit inside a fenced block keeps its line breaks", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "spec.md");
  const before = "Input: ls -la\nFlow:  Shell then Parser then Command";
  writeFileSync(file, `# Title\n\nOne line, one paragraph.\n\n\`\`\`\n${before}\n\`\`\`\n`, "utf8");

  const after = "Input: ls -la\nFlow:  Shell then Parser then external execution";
  const decision = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: { file_path: file, old_string: before, new_string: after },
    }),
  );
  assert.equal(decision.reason, "");
  assert.equal(decision.updatedInput, undefined);
});

// The control: the same two lines as prose in the same file are still joined,
// so placement did not simply switch the repair off.
test("an edit outside a fence is still repaired", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "spec.md");
  const before = "A paragraph the author wrapped\nacross two lines by hand.";
  writeFileSync(file, `# Title\n\n${before}\n`, "utf8");

  const after = "A paragraph the author rewrapped\nacross two lines by hand.";
  const decision = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: { file_path: file, old_string: before, new_string: after },
    }),
  );
  assert.equal(decision.reason, "");
  assert.equal(
    decision.updatedInput?.new_string,
    "A paragraph the author rewrapped across two lines by hand.",
  );
});

// A semicolon inside a fenced block is code, not the document's own prose. The
// fragment alone shows no fence, and the refusal that followed is what taught a
// session to delete code blocks out of a spec.
test("a fenced fragment is not refused for its punctuation", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "spec.md");
  const before = "const a = 1;";
  writeFileSync(file, `# Title\n\n\`\`\`go\n${before}\n\`\`\`\n`, "utf8");

  const decision = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: { file_path: file, old_string: before, new_string: "const a = 2;" },
    }),
  );
  assert.equal(decision.reason, "");
});

// A line break in YAML is syntax, not a wrap. This repair joined a two-line
// `concurrency:` block into one line and GitHub rejected the whole workflow
// before a job started.
test("a workflow file is never rewrapped", () => {
  const decision = decide(
    payload({
      old_string: "concurrency:\n  group: release-x",
      new_string: "concurrency:\n  group: release",
    }),
  );
  assert.equal(decision.reason, "");
  assert.equal(decision.updatedInput, undefined);
});

test("blockingFindings reports the check by name", () => {
  const found = blockingFindings(
    "Write",
    { file_path: WORKFLOW, content: "# a\n# b\non: push\n" },
    CWD,
  );
  assert.equal(found.length > 0, true);
  assert.equal(found[0]?.check, "yaml-comment-block");
});
