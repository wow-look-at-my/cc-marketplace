import assert from "node:assert/strict";
import { test } from "node:test";
import type { Client } from "../vendor-docker-docs/github.ts";
import { PLAN, assertPlanCoversCheckSet, parseCheckSet, stampHeader, vendorPath } from "./plan.ts";
import { vendor } from "./main.ts";

// The real composite, trimmed to its `uses:` lines and the shapes around them.
const COMPOSITE = `
runs:
  using: composite
  steps:
    - id: claim
      uses: wow-look-at-my/actions@run-once#latest
      with:
        name: common-checks
    - id: gate
      shell: bash
      run: echo hi
    - if: steps.claim.outputs.first == 'true'
      uses: wow-look-at-my/actions@no-all-builds-job#latest
    - if: steps.claim.outputs.first == 'true'
      uses: wow-look-at-my/actions@yaml-comment-block#latest
    - if: steps.claim.outputs.first == 'true'
      uses: wow-look-at-my/actions@no-tests-in-yaml#latest
    - if: steps.claim.outputs.first == 'true'
      uses: wow-look-at-my/actions@push-excludes-tags#latest
    - if: steps.claim.outputs.first == 'true'
      uses: wow-look-at-my/actions@ste-lint#latest
`;

class FakeClient implements Client {
  constructor(private readonly files: Record<string, string>) {}
  async resolve(): Promise<string> {
    return "a".repeat(40);
  }
  async get(_repo: string, _commit: string, path: string): Promise<string> {
    const hit = this.files[path];
    if (hit === undefined) throw new Error(`unexpected fetch: ${path}`);
    return hit;
  }
}

function filesFor(composite: string): Record<string, string> {
  const files: Record<string, string> = { "common-checks/action.yml": composite };
  for (const entry of PLAN) {
    for (const path of entry.files) files[`${entry.name}/${path}`] = `// body of ${entry.name}/${path}\n`;
  }
  return files;
}

test("the check set is read out of the composite in order", () => {
  assert.deepEqual(parseCheckSet(COMPOSITE), [
    "run-once",
    "no-all-builds-job",
    "yaml-comment-block",
    "no-tests-in-yaml",
    "push-excludes-tags",
    "ste-lint",
  ]);
});

test("a uses value from another repository is not a check of this composite", () => {
  assert.deepEqual(parseCheckSet("    - uses: actions/checkout@v4\n"), []);
});

test("the shipped plan covers the shipped composite", () => {
  assertPlanCoversCheckSet(PLAN, parseCheckSet(COMPOSITE));
});

test("a check added upstream fails the build and names itself", () => {
  const added = `${COMPOSITE}    - uses: wow-look-at-my/actions@brand-new-rule#latest\n`;
  assert.throws(
    () => assertPlanCoversCheckSet(PLAN, parseCheckSet(added)),
    (error: Error) => error.message.includes("brand-new-rule") && error.message.includes("does not cover"),
  );
});

test("a check dropped upstream fails the build and names itself", () => {
  const dropped = COMPOSITE.replace("    - if: steps.claim.outputs.first == 'true'\n      uses: wow-look-at-my/actions@ste-lint#latest\n", "");
  assert.throws(
    () => assertPlanCoversCheckSet(PLAN, parseCheckSet(dropped)),
    (error: Error) => error.message.includes("ste-lint") && error.message.includes("no longer runs"),
  );
});

test("a check that reports nothing still has to be named", () => {
  // run-once is the live example: it runs, and no open file can violate it.
  const runOnce = PLAN.find((entry) => entry.name === "run-once");
  assert.ok(runOnce);
  assert.equal(runOnce.files.length, 0);
  assert.ok(runOnce.why && runOnce.why.length > 0);
});

test("the header names the commit and the upstream path, and keeps the body verbatim", () => {
  const commit = "b".repeat(40);
  const stamped = stampHeader(commit, "ste-lint/src/lint.ts", "export const x = 1;\n");
  assert.ok(stamped.includes(commit));
  assert.ok(stamped.includes("ste-lint/src/lint.ts"));
  assert.ok(stamped.endsWith("export const x = 1;\n"));
});

test("a vendored path drops the src segment so relative imports still resolve", () => {
  assert.equal(vendorPath("ste-lint", "src/lint.ts"), "ste-lint/lint.ts");
  assert.equal(vendorPath("ste-lint", "src/blocks.ts"), "ste-lint/blocks.ts");
});

test("a full run writes every planned file plus the notice", async () => {
  const { commit, files } = await vendor(new FakeClient(filesFor(COMPOSITE)), "master");
  const paths = files.map((file) => file.path);
  assert.ok(paths.includes("ste-lint/lint.ts"));
  assert.ok(paths.includes("push-excludes-tags/scan.ts"));
  assert.ok(paths.includes("NOTICE.md"));
  const noticeFile = files.find((file) => file.path === "NOTICE.md");
  assert.ok(noticeFile?.content.includes(commit));
  // run-once has to appear, so the notice explains its absence from the covered list.
  assert.ok(noticeFile?.content.includes("run-once"));
});

test("a drifted composite fails the run before anything is written", async () => {
  const drifted = `${COMPOSITE}    - uses: wow-look-at-my/actions@brand-new-rule#latest\n`;
  await assert.rejects(
    () => vendor(new FakeClient(filesFor(drifted)), "master"),
    (error: Error) => error.message.includes("brand-new-rule"),
  );
});
