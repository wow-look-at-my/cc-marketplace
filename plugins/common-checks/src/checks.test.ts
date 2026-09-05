import assert from "node:assert/strict";
import { test } from "node:test";
import { fileKind, findings } from "./checks.ts";

const WORKFLOW = ".github/workflows/ci.yml";

function checks(path: string, content: string): string[] {
  return findings(path, content).map((finding) => finding.check);
}

test("a path decides which checks apply", () => {
  assert.equal(fileKind(".github/workflows/ci.yml"), "workflow");
  assert.equal(fileKind(".github/workflows/ci.yaml"), "workflow");
  assert.equal(fileKind("action.yml"), "action");
  assert.equal(fileKind("ste-lint/action.yaml"), "action");
  assert.equal(fileKind("docs/notes.md"), "markdown");
  // A workflow one directory down is not a workflow, and neither is any other
  // YAML: firing on every .yaml is how a checker gets skimmed past.
  assert.equal(fileKind(".github/workflows/nested/ci.yml"), "other");
  assert.equal(fileKind("compose.yaml"), "other");
  assert.equal(fileKind("src/main.go"), "other");
});

test("a file the checks never read reports nothing", () => {
  assert.deepEqual(findings("compose.yaml", "# a\n# b\n# c\nservices: {}\n"), []);
});

test("a comment wall is reported with the span it covers", () => {
  const content = "name: CI\n# one\n# two\n# three\non:\n  push:\n    branches: ['**']\n";
  const found = findings(WORKFLOW, content).filter((finding) => finding.check === "yaml-comment-block");
  assert.equal(found.length, 1);
  assert.equal(found[0].startLine, 2);
  assert.equal(found[0].endLine, 4);
  assert.match(found[0].message, /the limit is 1/);
});

test("a single comment line is not a wall", () => {
  const content = "name: CI\n# one\non:\n  push:\n    branches: ['**']\n";
  assert.deepEqual(checks(WORKFLOW, content), []);
});

test("an unfiltered push trigger is reported on the push key", () => {
  const found = findings(WORKFLOW, "name: CI\non:\n  push:\njobs: {}\n");
  assert.deepEqual(
    found.map((finding) => [finding.check, finding.startLine]),
    [["push-excludes-tags", 3]],
  );
  assert.match(found[0].message, /names no ref filter/);
});

test("an all-builds job key is reported on its own line", () => {
  const content = "name: CI\non:\n  push:\n    branches: ['**']\njobs:\n  build:\n    runs-on: x\n  all-builds:\n    runs-on: x\n";
  const found = findings(WORKFLOW, content).filter((finding) => finding.check === "no-all-builds-job");
  assert.equal(found.length, 1);
  assert.equal(found[0].startLine, 8);
  assert.match(found[0].message, /known deception attempt/);
});

test("an all-builds job name is reported on the name line", () => {
  const content = "on:\n  push:\n    branches: ['**']\njobs:\n  gate:\n    name: all-builds\n    runs-on: x\n";
  const found = findings(WORKFLOW, content).filter((finding) => finding.check === "no-all-builds-job");
  assert.equal(found.length, 1);
  assert.equal(found[0].startLine, 6);
});

test("an ordinarily named job is clean", () => {
  const content = "on:\n  push:\n    branches: ['**']\njobs:\n  build:\n    runs-on: x\n";
  assert.deepEqual(checks(WORKFLOW, content), []);
});

test("an assertion inside a run block is reported on its own line", () => {
  const content = [
    "on:",
    "  push:",
    "    branches: ['**']",
    "jobs:",
    "  build:",
    "    steps:",
    "      - run: |",
    "          make build",
    '          grep -q hello out.txt || { echo "::error::missing"; exit 1; }',
    "",
  ].join("\n");
  const found = findings(WORKFLOW, content).filter((finding) => finding.check === "no-tests-in-yaml");
  assert.equal(found.length, 1);
  assert.equal(found[0].startLine, 9);
  assert.match(found[0].message, /repository suite/);
});

test("a run block that only runs a command is not a test", () => {
  const content = "on:\n  push:\n    branches: ['**']\njobs:\n  build:\n    steps:\n      - run: make build\n";
  assert.deepEqual(checks(WORKFLOW, content), []);
});

test("the yaml checks also read an action manifest, and the workflow-only ones do not", () => {
  const content = "name: A\n# one\n# two\ndescription: d\nruns:\n  using: composite\n";
  const found = checks("my-action/action.yml", content);
  assert.deepEqual(found, ["yaml-comment-block"]);
});

test("continue-on-error over common-checks is reported", () => {
  const content = [
    "on:",
    "  push:",
    "    branches: ['**']",
    "jobs:",
    "  build:",
    "    steps:",
    "      - uses: wow-look-at-my/actions@common-checks#latest",
    "        continue-on-error: true",
    "",
  ].join("\n");
  const found = findings(WORKFLOW, content).filter((finding) => /not a gate/.test(finding.message));
  assert.equal(found.length, 1);
  assert.equal(found[0].startLine, 7);
});

test("a common-checks step that is allowed to fail nothing is clean", () => {
  const content = [
    "on:",
    "  push:",
    "    branches: ['**']",
    "jobs:",
    "  build:",
    "    steps:",
    "      - uses: wow-look-at-my/actions@common-checks#latest",
    "",
  ].join("\n");
  assert.deepEqual(checks(WORKFLOW, content), []);
});

test("a semicolon in markdown is a finding, and it names the line", () => {
  const found = findings("docs/x.md", "The server reads the file; it then reports.\n");
  assert.equal(found.length, 1);
  assert.equal(found[0].check, "ste-lint");
  assert.equal(found[0].startLine, 1);
  assert.match(found[0].message, /semicolon/);
});

test("a banned modal and a contraction are both reported", () => {
  const found = findings("docs/x.md", "You should not do that.\n\nIt isn't ready.\n");
  const messages = found.map((finding) => finding.message).join("\n");
  assert.match(messages, /should/);
  assert.match(messages, /contractions/i);
});

test("ordinary conforming prose reports nothing", () => {
  assert.deepEqual(findings("docs/x.md", "The server reads the file. It reports what it finds.\n"), []);
});

// The heuristic buckets never fail CI, so a diagnostic for one would spend the
// client's budget on something the gate does not care about.
test("a heuristic-only finding is not reported", () => {
  // Passive voice and a long noun cluster, with nothing that fails.
  const found = findings("docs/x.md", "The value is returned by the handler.\n");
  assert.deepEqual(found, []);
});

test("a hard-wrapped paragraph is one finding, not one per line", () => {
  const wrapped = "This is a paragraph that the author\nwrapped by hand across four\nseparate lines in the source\nfile itself.\n";
  const found = findings("docs/x.md", wrapped).filter((finding) => /one line/.test(finding.message));
  assert.equal(found.length, 1);
  assert.equal(found[0].startLine, 2);
});

test("two wrapped paragraphs are two findings", () => {
  const wrapped = "One paragraph that the author\nwrapped by hand.\n\nAnother paragraph that the author\nwrapped by hand.\n";
  const found = findings("docs/x.md", wrapped).filter((finding) => /one line/.test(finding.message));
  assert.equal(found.length, 2);
});

// ste-lint can report hundreds of findings on one document while a structural
// check reports one. The client injects only the first handful, so the order
// decides what the model actually sees.
test("a structural finding outranks a voluminous one", () => {
  const content = [
    "# one",
    "# two",
    "on:",
    "  push:",
    "jobs:",
    "  all-builds:",
    "    runs-on: x",
    "",
  ].join("\n");
  assert.deepEqual(checks(WORKFLOW, content), ["push-excludes-tags", "no-all-builds-job", "yaml-comment-block"]);
});

test("unparseable YAML is not judged", () => {
  assert.deepEqual(checks(WORKFLOW, "on:\n  push:\n :::not yaml\n\t\tbad\n").includes("push-excludes-tags"), false);
});
