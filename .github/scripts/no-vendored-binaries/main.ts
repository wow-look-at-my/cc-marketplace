// Usage: main.ts source [repo-root] the committed tree: binaries under plugins/, manifest paths, downloads main.ts assembled <dir>... the release output.

import { execSync } from "node:child_process";

import { binariesInAssembled, checkSource, type Finding } from "./check.ts";

function repoRoot(given: string | undefined): string {
  if (given) return given;
  return execSync("git rev-parse --show-toplevel", { encoding: "utf8" }).trim();
}

const [mode, ...rest] = process.argv.slice(2);
let findings: Finding[];
if (mode === "source") {
  findings = checkSource(repoRoot(rest[0]));
} else if (mode === "assembled" && rest.length > 0) {
  findings = rest.flatMap((dir) => binariesInAssembled(dir).map((f) => ({ ...f, file: `${dir}/${f.file}` })));
} else {
  console.error("usage: main.ts source [repo-root] | main.ts assembled <dir>...");
  process.exit(2);
}

for (const f of findings) console.log(`::error file=${f.file}::${f.message}`);
if (findings.length > 0) {
  console.error(`no-vendored-binaries: ${findings.length} finding(s)`);
  process.exit(1);
}
console.log(`no-vendored-binaries: clean (${mode})`);
