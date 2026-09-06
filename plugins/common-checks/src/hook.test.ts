import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

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

// A hard wrap is a finding like any other again: the write is refused and the
// model rewrites it.
test("a hard-wrapped paragraph is refused", () => {
  const reason = decide(
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
  assert.match(reason, /Join it back up/);
});

test("an already unwrapped document is allowed", () => {
  const reason = decide(
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
  assert.equal(reason, "");
});

// A fence is not prose. Its line breaks are not a hard wrap, so a fenced block
// is allowed on its own.
test("a fenced code block is not read as a wrapped paragraph", () => {
  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: CWD,
      tool_input: {
        file_path: `${CWD}/docs/thing.md`,
        content: "# Title\n\n```sh\nfirst\nsecond\n```\n",
      },
    }),
  );
  assert.equal(reason, "");
});

// The incident placement exists for: an Edit whose fragment sits inside a
// fenced block. Judged alone the fragment shows no fence, so its lines read as
// a hand-wrapped paragraph and the write was refused for prose it never wrote.
test("an edit inside a fenced block is not refused for its line breaks", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "spec.md");
  const before = "Input: ls -la\nFlow:  Shell then Parser then Command";
  writeFileSync(file, `# Title\n\nOne line, one paragraph.\n\n\`\`\`\n${before}\n\`\`\`\n`, "utf8");

  const after = "Input: ls -la\nFlow:  Shell then Parser then external execution";
  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: { file_path: file, old_string: before, new_string: after },
    }),
  );
  assert.equal(reason, "");
});

// The control: the same two lines as prose in the same file are still refused,
// so placement did not simply switch the wrap check off.
test("an edit outside a fence is still refused for its wrap", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "spec.md");
  const before = "A paragraph the author wrapped\nacross two lines by hand.";
  writeFileSync(file, `# Title\n\n${before}\n`, "utf8");

  const after = "A paragraph the author rewrapped\nacross two lines by hand.";
  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: { file_path: file, old_string: before, new_string: after },
    }),
  );
  assert.match(reason, /Join it back up/);
});

// A semicolon inside a fenced block is code, not the document's own prose. The
// fragment alone shows no fence, and the refusal that followed is what taught a
// session to delete code blocks out of a spec.
test("a fenced fragment is not refused for its punctuation", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "spec.md");
  const before = "const a = 1;";
  writeFileSync(file, `# Title\n\n\`\`\`go\n${before}\n\`\`\`\n`, "utf8");

  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: { file_path: file, old_string: before, new_string: "const a = 2;" },
    }),
  );
  assert.equal(reason, "");
});

<<<<<<< HEAD
// An edit anchors on text the file already has, so a `new_string` that repeats
// any of it puts those lines in the span. The sentence below breaks the word
// cap and no edit here wrote it. Reporting it refused the write AND recorded
// the file, and that record never cleared: the finding naming it was one no
// edit could remove, so every later write in the session was refused too.
test("a finding the file already carried is not the write's own", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "notes.md");
  const long =
    "Every GitHub Actions read goes through the extension this config installs, " +
    "and the raw paths are denied outright, so a session without it cannot read CI at all.";
  writeFileSync(file, `# Title\n\n${long}\n`, "utf8");

  // The anchor carries the long sentence, so the span covers it.
  const decision = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: {
        file_path: file,
        old_string: long,
        new_string: `A short line.\n\n${long}`,
      },
    }),
  );
  assert.equal(decision.reason, "");
});

// The same sentence written a SECOND time is text this write really adds, so
// subtracting what the file carried must not swallow it.
test("a second copy of an existing violation is still refused", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  const file = join(dir, "notes.md");
  const long =
    "Every GitHub Actions read goes through the extension this config installs, " +
    "and the raw paths are denied outright, so a session without it cannot read CI at all.";
  writeFileSync(file, `# Title\n\n${long}\n`, "utf8");

  const decision = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: { file_path: file, old_string: long, new_string: `${long}\n\n${long}` },
    }),
  );
  assert.notEqual(decision.reason, "");
});

// A line break in YAML is syntax, not a wrap. This repair joined a two-line
// `concurrency:` block into one line and GitHub rejected the whole workflow
// before a job started.
test("a workflow file is never rewrapped", () => {
  const decision = decide(
=======
// A line break in YAML is syntax, not a wrap. Only markdown reaches the wrap
// check, so a two-line `concurrency:` block is neither refused nor joined.
test("a workflow file is never wrap-checked", () => {
  const reason = decide(
>>>>>>> origin/master
    payload({
      old_string: "concurrency:\n  group: release-x",
      new_string: "concurrency:\n  group: release",
    }),
  );
  assert.equal(reason, "");
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
