import { test } from "node:test";
import assert from "node:assert/strict";

import { addedText, blockingFindings, decide, relativePath } from "./hook.ts";

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
  const reason = decide(
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
  const reason = decide(
    payload({
      new_string: "  # Republish a preview without a commit that only triggers one.\n  workflow_dispatch:",
    }),
  );
  assert.equal(reason, "");
});

// Only the added text is judged, so a violation already in the file cannot
// block an edit that has nothing to do with it.
test("an unrelated edit is not blocked by the rest of the file", () => {
  assert.equal(decide(payload({ new_string: "  timeout-minutes: 10" })), "");
});

test("a non-workflow file is not judged", () => {
  const reason = decide(
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
  const reason = decide(
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
  const reason = decide(
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
  assert.equal(decide("not json"), "");
  assert.equal(decide(JSON.stringify({ hook_event_name: "Stop" })), "");
  assert.equal(decide(JSON.stringify({ hook_event_name: "PreToolUse" })), "");
  assert.equal(
    decide(JSON.stringify({ hook_event_name: "PreToolUse", tool_name: "Edit", tool_input: {} })),
    "",
  );
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
