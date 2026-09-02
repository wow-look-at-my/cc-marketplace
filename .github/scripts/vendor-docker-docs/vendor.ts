// Orchestration: resolve one commit per upstream, render every page, write the
// bundle, and drop a file the plan no longer produces.

import { mkdir, readdir, rm, writeFile } from "fs/promises";
import { join } from "path";

import type { Client } from "./github.ts";
import { bundles, NOTICE_FILE, notice, render, type Bundle } from "./transform.ts";

export interface Reporter {
  (line: string): void;
}

/**
 * Runs the whole plan against `root`. `log` exists so a test can assert what
 * happened without reading stdout.
 */
export async function vendor(root: string, client: Client, log: Reporter = () => {}): Promise<void> {
  // One commit per upstream, resolved once, so every file in a run comes from
  // the same tree.
  const commits = new Map<string, string>();
  for (const bundle of bundles) {
    for (const page of bundle.pages) {
      if (commits.has(page.src.repo)) continue;
      const commit = await client.resolve(page.src);
      commits.set(page.src.repo, commit);
      log(`${page.src.repo}@${page.src.ref} -> ${commit}`);
    }
  }

  for (const bundle of bundles) {
    const dir = join(root, "plugins", "docs", "skills", bundle.skill, "reference");
    await mkdir(dir, { recursive: true });
    await writeBundle(dir, bundle, commits, client, log);
  }
}

async function writeBundle(
  dir: string,
  bundle: Bundle,
  commits: Map<string, string>,
  client: Client,
  log: Reporter,
): Promise<void> {
  const written = new Set<string>();

  for (const page of bundle.pages) {
    const commit = commits.get(page.src.repo)!;
    // Includes resolve against the page's own repository and commit.
    const fetchPath = (path: string) => client.get(page.src.repo, commit, path);

    let body: string;
    try {
      body = await render(await fetchPath(page.path), page, commit, fetchPath);
    } catch (cause) {
      throw new Error(`${page.path}: ${(cause as Error).message}`);
    }

    await writeFile(join(dir, page.out), body);
    written.add(page.out);
    log(`  ${bundle.skill}/${page.out} (${body.length} bytes)`);
  }

  await pruneStale(dir, bundle.skill, written, log);
  await writeFile(join(dir, NOTICE_FILE), notice(bundle, commits));
}

/**
 * Deletes a file the plan no longer produces. Left behind, it reads as current
 * reference while nothing regenerates it.
 */
async function pruneStale(
  dir: string,
  skill: string,
  written: Set<string>,
  log: Reporter,
): Promise<void> {
  for (const entry of await readdir(dir)) {
    if (entry === NOTICE_FILE || written.has(entry)) continue;
    await rm(join(dir, entry), { recursive: true });
    log(`  ${skill}/${entry} removed (no longer in the plan)`);
  }
}
