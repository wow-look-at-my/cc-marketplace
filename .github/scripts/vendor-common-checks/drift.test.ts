import assert from "node:assert/strict";
import { test } from "node:test";
import { drifted, type Written } from "./main.ts";
import { stampHeader } from "./plan.ts";

const COMMIT_A = "a".repeat(40);
const COMMIT_B = "b".repeat(40);

function fetched(body: string, commit = COMMIT_A): Written[] {
  return [
    { path: "ste-lint/lint.ts", content: stampHeader(commit, "ste-lint/src/lint.ts", body) },
    { path: "NOTICE.md", content: `# whatever ${commit}\n` },
  ];
}

test("an unchanged check reports no drift", () => {
  const body = "export const x = 1;\n";
  const committed = new Map([["ste-lint/lint.ts", stampHeader(COMMIT_A, "ste-lint/src/lint.ts", body)]]);
  assert.deepEqual(drifted(fetched(body), committed), []);
});

// The header carries the commit, so it differs on every unrelated upstream
// push. Reporting that as drift would make the check cry wolf, and a check that
// cries wolf gets ignored on the run where the number is real.
test("a new commit alone is not drift", () => {
  const body = "export const x = 1;\n";
  const committed = new Map([["ste-lint/lint.ts", stampHeader(COMMIT_A, "ste-lint/src/lint.ts", body)]]);
  assert.deepEqual(drifted(fetched(body, COMMIT_B), committed), []);
});

test("a changed rule is drift, and it names the file", () => {
  const committed = new Map([["ste-lint/lint.ts", stampHeader(COMMIT_A, "ste-lint/src/lint.ts", "export const x = 1;\n")]]);
  assert.deepEqual(drifted(fetched("export const x = 2;\n"), committed), ["ste-lint/lint.ts"]);
});

test("a check the plan added but nobody vendored is drift", () => {
  assert.deepEqual(drifted(fetched("export const x = 1;\n"), new Map()), ["ste-lint/lint.ts"]);
});

test("a file the plan no longer produces is drift", () => {
  const body = "export const x = 1;\n";
  const committed = new Map([
    ["ste-lint/lint.ts", stampHeader(COMMIT_A, "ste-lint/src/lint.ts", body)],
    ["gone/scan.ts", "// leftover\n"],
  ]);
  assert.deepEqual(drifted(fetched(body), committed), ["gone/scan.ts (no longer vendored)"]);
});
