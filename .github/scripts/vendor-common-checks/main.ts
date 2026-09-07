// Reads upstream's own manifest of the checks common-checks runs, and fails the
// build when the plugin no longer covers every one of them.
//
// It used to fetch each check's TypeScript modules too, and the plugin imported
// them. Upstream has since moved the rules into wow-look-at-my/slopfix, and the
// plugin runs that binary instead. What is left here is the coverage question,
// which no binary answers: does the plugin still report every check the gate
// runs, or say why it cannot.
//
// The output is gitignored on purpose. A copy in the tree is a second source of
// truth: it goes stale in silence.
//
// A fetch failure fails the build. Packaging a silently stale checker is the
// outcome this whole arrangement exists to avoid.

import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { GitHubClient, type Client } from "../vendor-docker-docs/github.ts";
import {
  MANIFEST_PATH,
  UPSTREAM_REPO,
  assertPlanCoversCheckSet,
  notice,
  parseCheckSet,
  parseManifest,
  upstreamRef,
} from "./plan.ts";

const VENDOR_DIR = "plugins/common-checks/vendor";

export interface Written {
  path: string;
  content: string;
}

/** The whole pipeline, with the network behind a Client so a test can drive it. */
export async function vendor(client: Client, ref: string): Promise<{ commit: string; files: Written[] }> {
  const commit = await client.resolve({ repo: UPSTREAM_REPO, ref });

  const plan = parseManifest(await client.get(UPSTREAM_REPO, commit, MANIFEST_PATH));
  const composite = await client.get(UPSTREAM_REPO, commit, "common-checks/action.yml");
  assertPlanCoversCheckSet(plan, parseCheckSet(composite));

  const files: Written[] = [];
  files.push({ path: "NOTICE.md", content: notice(commit, plan) });
  // The plan is what the plugin's own tests hold their coverage against. A
  // check upstream runs that the plugin neither reports nor declares uncovered
  // is silent non-coverage, and the composite's `uses:` list cannot show it.
  files.push({ path: "plan.json", content: `${JSON.stringify({ commit, plan }, null, 2)}\n` });
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
