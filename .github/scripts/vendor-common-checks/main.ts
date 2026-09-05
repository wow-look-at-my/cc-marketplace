// Re-fetches the check modules the common-checks language server runs, so a
// released plugin enforces what CI enforces today rather than what it enforced
// the day somebody last copied the files by hand.
//
// A fetch failure fails the build. Packaging a silently stale checker is the
// outcome this whole arrangement exists to avoid.

import { mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
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

/**
 * Names the vendored files that no longer match upstream, ignoring the
 * provenance header: a header differs on every commit, and reporting that as
 * drift would make the check cry wolf on every unrelated push.
 */
export function drifted(files: Written[], committed: Map<string, string | undefined>): string[] {
  const out: string[] = [];
  for (const file of files) {
    if (file.path === "NOTICE.md") continue;
    if (stripHeader(committed.get(file.path)) !== stripHeader(file.content)) out.push(file.path);
  }
  for (const path of committed.keys()) {
    if (!files.some((file) => file.path === path)) out.push(`${path} (no longer vendored)`);
  }
  return out.sort();
}

function stripHeader(content: string | undefined): string | undefined {
  if (content === undefined) return undefined;
  return content.replace(/^(\/\/ [^\n]*\n)+\n/, "");
}

async function committedFiles(): Promise<Map<string, string | undefined>> {
  const out = new Map<string, string | undefined>();
  let entries: string[];
  try {
    entries = await readdir(VENDOR_DIR, { recursive: true });
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (!entry.endsWith(".ts")) continue;
    out.set(entry.replace(/\\/g, "/"), await readFile(join(VENDOR_DIR, entry), "utf8"));
  }
  return out;
}

async function main(): Promise<void> {
  const ref = upstreamRef();
  const check = process.argv.includes("--check");
  const { commit, files } = await vendor(new GitHubClient(), ref);

  // The plugin build is cached on the hash of its own directory, so a change
  // made only upstream never invalidates it and prebuild never re-runs. This
  // mode runs where nothing is cached, so drift is red on every push rather
  // than on whichever push happens to touch this plugin.
  if (check) {
    const stale = drifted(files, await committedFiles());
    if (stale.length > 0) {
      throw new Error(
        `the vendored checks have drifted from ${UPSTREAM_REPO}@${ref} (${commit}):\n  ${stale.join("\n  ")}\n` +
          "Re-run `just prebuild` in plugins/common-checks and commit the result.",
      );
    }
    process.stderr.write(`vendored checks match ${UPSTREAM_REPO}@${ref} (${commit})\n`);
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
