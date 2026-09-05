// Fetches the check modules the common-checks language server runs, so a
// released plugin enforces what CI enforces today.
//
// The output is gitignored on purpose. A copy in the tree is a second source of
// truth: it goes stale in silence, and it puts prose the repository does not
// author in front of every check that reads the repository.
//
// A fetch failure fails the build. Packaging a silently stale checker is the
// outcome this whole arrangement exists to avoid.

import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { GitHubClient, type Client } from "../vendor-docker-docs/github.ts";
import {
  PLAN,
  UPSTREAM_REPO,
  assertPlanCoversCheckSet,
  notice,
  parseCheckSet,
  stampHeader,
  upstreamRef,
  vendorPath,
} from "./plan.ts";

const VENDOR_DIR = "plugins/common-checks/vendor";

export interface Written {
  path: string;
  content: string;
}

/** The whole pipeline, with the network behind a Client so a test can drive it. */
export async function vendor(client: Client, ref: string): Promise<{ commit: string; files: Written[] }> {
  const commit = await client.resolve({ repo: UPSTREAM_REPO, ref });

  const composite = await client.get(UPSTREAM_REPO, commit, "common-checks/action.yml");
  assertPlanCoversCheckSet(PLAN, parseCheckSet(composite));

  const files: Written[] = [];
  for (const entry of PLAN) {
    for (const path of entry.files) {
      const body = await client.get(UPSTREAM_REPO, commit, `${entry.name}/${path}`);
      files.push({ path: vendorPath(entry.name, path), content: stampHeader(commit, `${entry.name}/${path}`, body) });
    }
  }
  files.push({ path: "NOTICE.md", content: notice(commit, PLAN) });
  return { commit, files };
}

async function main(): Promise<void> {
  const ref = upstreamRef();
  const { commit, files } = await vendor(new GitHubClient(), ref);

  // `--commit` prints the resolved commit and writes nothing. The plugin build
  // is cached on the hash of its own directory, and nothing in that directory
  // changes when a rule changes upstream, so the release workflow folds this
  // into the cache key. Without it a cached build serves check code that no
  // longer matches CI.
  if (process.argv.includes("--commit")) {
    process.stdout.write(`${commit}\n`);
    return;
  }

  // A whole-directory replace, so a file the plan stopped producing goes away
  // instead of lingering as a module nothing imports and nothing refreshes.
  await rm(VENDOR_DIR, { recursive: true, force: true });
  for (const file of files) {
    const target = join(VENDOR_DIR, file.path);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, file.content, "utf8");
  }
  process.stderr.write(`vendored ${files.length} file(s) from ${UPSTREAM_REPO}@${ref} (${commit})\n`);
}

if (process.argv[1] && import.meta.url === `file://${process.argv[1]}`) {
  main().catch((error: unknown) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exit(1);
  });
}
