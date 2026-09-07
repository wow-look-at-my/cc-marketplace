import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { addedText, blockingFindings, decide, relativePath } from "./hook.ts";
import { inScope } from "./scope.ts";

/**
 * A real directory carrying a real `.git`, because scope is now decided by
 * walking the path on disk. A fixture under a bare temp directory is out of
 * scope by design, which is what the scope tests at the bottom pin.
 */
function repoDir(): string {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-hook-"));
  mkdirSync(join(dir, ".git"));
  return dir;
}

const CWD = repoDir();
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
  assert.match(reason, /yaml\/comment-block/);
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
  assert.match(reason, /yaml\/comment-block/);
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
  assert.match(reason, /yaml\/comment-block/);
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
  const dir = repoDir();
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
  const dir = repoDir();
  const file = join(dir, "spec.md");
  // The file starts clean, so the wrap the edit adds is unambiguously its own.
  // Rewording a paragraph the file already wrapped is a different case, and the
  // subtraction above deliberately leaves that one alone.
  const before = "One line, one paragraph.";
  writeFileSync(file, `# Title\n\n${before}\n`, "utf8");

  const after = "A paragraph the author wrapped\nacross two lines by hand.";
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

// The span alone is not enough. An edit anchors on text the file already has,
// and a new_string that repeats any of it puts those lines inside the span. The
// file below already breaks the rule on a paragraph the edit only re-anchors on.
// Without the subtraction that sentence is reported as this write's own, and the
// ledger records a file no write here made worse.
test("a finding the write only re-anchored on is not its own", () => {
  const dir = repoDir();
  const file = join(dir, "spec.md");
  const carried = "A paragraph the author wrapped\nacross two lines by hand.";
  writeFileSync(file, `# Title\n\n${carried}\n\nOne line, one paragraph.\n`, "utf8");

  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: {
        file_path: file,
        // The edit anchors on the wrapped paragraph and writes it back
        // unchanged, so it lands inside the span without being this write's.
        old_string: carried,
        new_string: `${carried}\n\nA sentence this write really adds.`,
      },
    }),
  );
  assert.equal(reason, "");
});

// The control. A SECOND copy of a sentence the file already breaks is still the
// write's own, so each pre-edit finding cancels exactly one match.
test("a second copy of a carried finding is still the write's own", () => {
  const dir = repoDir();
  const file = join(dir, "spec.md");
  const carried = "A paragraph the author wrapped\nacross two lines by hand.";
  writeFileSync(file, `# Title\n\n${carried}\n\nOne line, one paragraph.\n`, "utf8");

  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Edit",
      cwd: dir,
      tool_input: {
        file_path: file,
        old_string: carried,
        new_string: `${carried}\n\n${carried}`,
      },
    }),
  );
  assert.match(reason, /Join it back up/);
});

// A semicolon inside a fenced block is code, not the document's own prose. The
// fragment alone shows no fence, and the refusal that followed is what taught a
// session to delete code blocks out of a spec.
test("a fenced fragment is not refused for its punctuation", () => {
  const dir = repoDir();
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

// A line break in YAML is syntax, not a wrap. Only markdown reaches the wrap
// check, so a two-line `concurrency:` block is neither refused nor joined.
test("a workflow file is never wrap-checked", () => {
  const reason = decide(
    payload({
      old_string: "concurrency:\n  group: release-x",
      new_string: "concurrency:\n  group: release",
    }),
  );
  assert.equal(reason, "");
});

// The incident: writing a plan file under ~/.claude was refused with 29
// ste-lint findings and told it would fail CI. A plan is in no repository, it
// never ships, and no build reads it, so that sentence was false. The hard-wrap
// rule is wrong there too, because a plan is read in a terminal pane.
const WRAPPED = "# Plan\n\nA paragraph that the author wrapped\nacross two lines by hand.\n";

test("a document outside any work tree is not judged", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-loose-"));
  const file = join(dir, "notes.md");
  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: dir,
      tool_input: { file_path: file, content: WRAPPED },
    }),
  );
  assert.equal(reason, "");
});

// The control that proves the case above can fail: the same bytes, one `.git`
// away, are refused exactly as before.
test("the same document inside a work tree is still refused", () => {
  const dir = repoDir();
  const file = join(dir, "notes.md");
  const reason = decide(
    JSON.stringify({
      hook_event_name: "PreToolUse",
      tool_name: "Write",
      cwd: dir,
      tool_input: { file_path: file, content: WRAPPED },
    }),
  );
  assert.match(reason, /Join it back up/);
});

// The user's own configuration directory is out of scope even when it sits
// inside a work tree, which it does whenever somebody versions their dotfiles.
test("a plan under the Claude configuration directory is not judged", () => {
  const home = repoDir();
  const plans = join(home, ".claude", "plans");
  mkdirSync(plans, { recursive: true });
  const file = join(plans, "x.md");

  const previous = process.env.HOME;
  process.env.HOME = home;
  try {
    assert.equal(inScope(join(home, "src", "real.md")), true, "the work tree itself is still in scope");
    const reason = decide(
      JSON.stringify({
        hook_event_name: "PreToolUse",
        tool_name: "Write",
        cwd: home,
        tool_input: { file_path: file, content: WRAPPED },
      }),
    );
    assert.equal(reason, "");
  } finally {
    if (previous === undefined) delete process.env.HOME;
    else process.env.HOME = previous;
  }
});

// A worktree and a submodule carry a `.git` FILE rather than a directory, so
// asking whether the name exists is what covers both.
test("a work tree whose .git is a file is in scope", () => {
  const dir = mkdtempSync(join(tmpdir(), "common-checks-worktree-"));
  writeFileSync(join(dir, ".git"), "gitdir: /elsewhere/.git/worktrees/x\n", "utf8");
  assert.equal(inScope(join(dir, "docs", "notes.md")), true);
});

// Fail safe: a path the predicate cannot reason about is out of scope, so the
// write goes through rather than wedging on a guard that cannot read it.
test("a path that is not absolute is out of scope", () => {
  assert.equal(inScope("docs/notes.md"), false);
  assert.equal(inScope(""), false);
});

test("blockingFindings reports the check by name", () => {
  const found = blockingFindings(
    "Write",
    { file_path: WORKFLOW, content: "# a\n# b\non: push\n" },
    CWD,
  );
  assert.equal(found.length > 0, true);
  assert.equal(found[0]?.check, "yaml/comment-block");
});
