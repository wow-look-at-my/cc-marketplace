import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { findings } from "./checks.ts";
import { SlopfixUnavailable, report } from "./slopfix.ts";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCAN = join(HERE, "scan-file.ts");
// A markdown file, because a path no rule reads never reaches slopfix at all
// and would report `clean` whether the binary was there or not.
const TARGET = join(HERE, "..", "README.md");
const WORKFLOW = ".github/workflows/ci.yml";

/** An environment whose slopfix cannot be found or cannot run. */
function without(binary: string): NodeJS.ProcessEnv {
  return { ...process.env, COMMON_CHECKS_SLOPFIX: binary };
}

// The failure this exists to prevent: a run that checked nothing must never
// report a pass. Answering with an empty list reads exactly like a clean file.
test("an unusable slopfix throws rather than reporting nothing", () => {
  const previous = process.env.COMMON_CHECKS_SLOPFIX;
  process.env.COMMON_CHECKS_SLOPFIX = join(HERE, "no-such-slopfix");
  try {
    assert.throws(() => report(WORKFLOW, "name: CI\n"), SlopfixUnavailable);
    assert.throws(() => findings(WORKFLOW, "name: CI\n"), SlopfixUnavailable);
  } finally {
    if (previous === undefined) delete process.env.COMMON_CHECKS_SLOPFIX;
    else process.env.COMMON_CHECKS_SLOPFIX = previous;
  }
});

// The same failure at the surface a person reads. It printed `clean` and exited
// zero after checking nothing, which told a caller the file passed.
test("scan-file fails loudly when it cannot check anything", () => {
  let out = "";
  let code = 0;
  try {
    execFileSync("npx", ["tsx", SCAN, TARGET], {
      encoding: "utf8",
      env: without(join(HERE, "no-such-slopfix")),
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (error) {
    const failure = error as { status?: number; stdout?: string; stderr?: string };
    code = failure.status ?? 0;
    out = `${failure.stdout ?? ""}${failure.stderr ?? ""}`;
  }
  assert.notEqual(code, 0, "a run that checked nothing exited zero");
  assert.doesNotMatch(out, /^clean$/m, "a run that checked nothing printed clean");
  assert.match(out, /no slopfix binary/);
});

// The negative control. With a working slopfix the same command reports the
// file and exits on its findings, so the case above can really fail.
test("scan-file reports normally when slopfix is there", () => {
  let out = "";
  try {
    out = execFileSync("npx", ["tsx", SCAN, TARGET], { encoding: "utf8" });
  } catch (error) {
    out = (error as { stdout?: string }).stdout ?? "";
  }
  assert.match(out, /clean|finding\(s\)/);
  assert.doesNotMatch(out, /no slopfix binary/);
});
