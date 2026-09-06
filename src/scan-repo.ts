// A local run of the hard ste-lint buckets over every markdown file, so a CI
// failure can be reproduced and fixed without pushing. Not part of the plugin.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

import * as steLint from "../vendor/ste-lint/lint.ts";

const HARD = ["hardLong", "contractions", "bannedModals", "semicolons", "commaSplices", "wrappedLines"];
const SKIP = new Set(["node_modules", ".git", "vendor", "build", "server", "reference"]);

function walk(dir: string, out: string[]): string[] {
  for (const name of readdirSync(dir)) {
    if (SKIP.has(name)) continue;
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else if (/\.md$/i.test(path)) out.push(path);
  }
  return out;
}

const root = process.argv[2] ?? ".";
let found = 0;
for (const path of walk(root, [])) {
  const rel = relative(root, path);
  const findings = steLint.lintText(rel, readFileSync(path, "utf8")) as Record<string, string[]>;
  for (const bucket of HARD) {
    for (const entry of findings[bucket] ?? []) {
      found++;
      console.log(`${bucket}: ${entry}`);
    }
  }
}
console.log(found === 0 ? "clean" : `${found} hard finding(s)`);
