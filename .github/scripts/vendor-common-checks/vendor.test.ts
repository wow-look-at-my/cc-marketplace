import assert from "node:assert/strict";
import { test } from "node:test";
import type { Client } from "../vendor-docker-docs/github.ts";
import {
  MANIFEST_PATH,
  assertPlanCoversCheckSet,
  parseCheckSet,
  parseManifest,
} from "./plan.ts";
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

// The real manifest, in the shape upstream publishes it.
const MANIFEST = JSON.stringify({
  checks: [
    { name: "run-once", modules: [], reason: "it claims the workflow run for one job." },
    { name: "no-all-builds-job", modules: ["no-all-builds-job/src/detect.ts"] },
    { name: "yaml-comment-block", modules: ["yaml-comment-block/src/scan.ts"] },
    { name: "no-tests-in-yaml", modules: ["no-tests-in-yaml/src/scan.ts"] },
    { name: "push-excludes-tags", modules: [], reason: "its rule is inline in a composite action." },
    { name: "ste-lint", modules: ["ste-lint/src/lint.ts", "ste-lint/src/guard.ts"] },
  ],
});

const PLAN = parseManifest(MANIFEST);

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

// Only two paths are ever fetched now: the manifest, and the composite it
// ships beside. A fetch of anything else is an error the FakeClient throws on,
// which is what pins that the modules really are no longer downloaded.
function filesFor(composite: string, manifest: string = MANIFEST): Record<string, string> {
  return {
    "common-checks/action.yml": composite,
    [MANIFEST_PATH]: manifest,
  };
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

test("a module path is read relative to its own check", () => {
  const ste = PLAN.find((entry) => entry.name === "ste-lint");
  assert.deepEqual(ste?.files, ["src/lint.ts", "src/guard.ts"]);
});

test("the manifest covers the composite it ships beside", () => {
  assertPlanCoversCheckSet(PLAN, parseCheckSet(COMPOSITE));
});

test("a check added upstream fails the build and names itself", () => {
  const added = `${COMPOSITE}    - uses: wow-look-at-my/actions@brand-new-rule#latest\n`;
  assert.throws(
    () => assertPlanCoversCheckSet(PLAN, parseCheckSet(added)),
    (error: Error) => error.message.includes("brand-new-rule") && error.message.includes("does not name"),
  );
});

test("a check dropped upstream fails the build and names itself", () => {
  const dropped = COMPOSITE.replace("    - if: steps.claim.outputs.first == 'true'\n      uses: wow-look-at-my/actions@ste-lint#latest\n", "");
  assert.throws(
    () => assertPlanCoversCheckSet(PLAN, parseCheckSet(dropped)),
    (error: Error) => error.message.includes("ste-lint") && error.message.includes("no longer runs"),
  );
});

// Two live examples, for the two reasons a check reports nothing. run-once has
// no rule an open file can break. push-excludes-tags has one, inline in a
// composite action, where nothing can import it.
test("a check that reports nothing still has to be named, with its reason", () => {
  for (const name of ["run-once", "push-excludes-tags"]) {
    const entry = PLAN.find((candidate) => candidate.name === name);
    assert.ok(entry, `${name} is missing from the manifest`);
    assert.equal(entry.files.length, 0);
    assert.ok(entry.why && entry.why.length > 0, `${name} declares no reason`);
  }
});

test("a check that vendors nothing and gives no reason is rejected", () => {
  const silent = JSON.stringify({ checks: [{ name: "quiet-rule", modules: [] }] });
  assert.throws(
    () => parseManifest(silent),
    (error: Error) => error.message.includes("quiet-rule") && error.message.includes("no reason"),
  );
});

test("a module belonging to another check is rejected rather than vendored somewhere surprising", () => {
  const crossed = JSON.stringify({ checks: [{ name: "ste-lint", modules: ["run-once/src/main.ts"] }] });
  assert.throws(
    () => parseManifest(crossed),
    (error: Error) => error.message.includes("run-once/src/main.ts") && error.message.includes("ste-lint/"),
  );
});

test("an unreadable or empty manifest fails the build rather than vendoring nothing", () => {
  assert.throws(() => parseManifest("not json"), /not valid JSON/);
  assert.throws(() => parseManifest(JSON.stringify({ checks: [] })), /names no checks/);
});

test("a full run writes the notice and the plan, and downloads no module", async () => {
  const { commit, files } = await vendor(new FakeClient(filesFor(COMPOSITE)), "master");
  const paths = files.map((file) => file.path);
  assert.deepEqual(paths.sort(), ["NOTICE.md", "plan.json"]);
  const noticeFile = files.find((file) => file.path === "NOTICE.md");
  assert.ok(noticeFile?.content.includes(commit));
  // Every check appears, so the notice accounts for the whole gate.
  for (const name of ["run-once", "push-excludes-tags", "ste-lint", "yaml-comment-block"]) {
    assert.ok(noticeFile?.content.includes(name), `${name} is missing from the notice`);
  }
  // The plugin's own tests hold their coverage against this.
  const planFile = files.find((file) => file.path === "plan.json");
  assert.ok(planFile);
  assert.deepEqual(JSON.parse(planFile.content).plan, PLAN);
});

// The negative control for the case above. The FakeClient throws on any path it
// was not given, so a run that still reached for a module would fail here.
test("a manifest naming a module nobody serves still completes", async () => {
  const grown = MANIFEST.replace('"ste-lint/src/guard.ts"', '"ste-lint/src/guard.ts","ste-lint/src/blocks.ts"');
  const run = await vendor(new FakeClient(filesFor(COMPOSITE, grown)), "master");
  const plan = JSON.parse(run.files.find((file) => file.path === "plan.json")!.content).plan as typeof PLAN;
  const ste = plan.find((entry) => entry.name === "ste-lint");
  assert.deepEqual(ste?.files, ["src/lint.ts", "src/guard.ts", "src/blocks.ts"]);
});

test("a drifted composite fails the run before anything is written", async () => {
  const drifted = `${COMPOSITE}    - uses: wow-look-at-my/actions@brand-new-rule#latest\n`;
  await assert.rejects(
    () => vendor(new FakeClient(filesFor(drifted)), "master"),
    (error: Error) => error.message.includes("brand-new-rule"),
  );
});
