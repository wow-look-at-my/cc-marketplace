// Report what the org's checks say about a file on disk, using the same
// findings() the hook and the server read. Run it before pushing prose:
//   npx tsx plugins/common-checks/src/scan-file.ts <path>...
import { readFileSync } from "node:fs";
import { relative } from "node:path";

import { findings } from "./checks.ts";

// A run that could not check anything must never print `clean`. An unavailable
// slopfix ends the run non-zero, so the exit code and the last line agree.
let bad = 0;
try {
  for (const path of process.argv.slice(2)) {
    const rel = relative(process.cwd(), path);
    const found = findings(rel, readFileSync(path, "utf8"));
    for (const f of found) {
      bad++;
      console.log(`${rel}:${f.startLine}: ${f.check}: ${f.message}`);
    }
  }
} catch (error) {
  console.error(`common-checks: ${error instanceof Error ? error.message : String(error)}`);
  process.exit(2);
}
console.log(bad === 0 ? "clean" : `${bad} finding(s)`);
process.exit(bad === 0 ? 0 : 1);
