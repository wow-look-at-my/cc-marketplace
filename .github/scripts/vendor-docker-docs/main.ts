// Refreshes the Docker reference text the `docs` plugin's dockerfile and
// docker-compose skills read from.
//
// The reference is vendored rather than summarized. A paraphrase of a
// specification is a second source of truth that goes stale with nothing to
// signal it; a verbatim copy pinned to a commit can be diffed against upstream
// and regenerated in one command.
//
// This runs on every CI build of the docs plugin, from its justfile `prebuild`
// recipe, so a published plugin always carries current reference text.
//
// Usage: npx tsx .github/scripts/vendor-docker-docs/main.ts [repo-root]

import { execSync } from "child_process";

import { GitHubClient } from "./github.ts";
import { vendor } from "./vendor.ts";

function repoRoot(): string {
  const given = process.argv[2];
  if (given) return given;
  return execSync("git rev-parse --show-toplevel", { encoding: "utf8" }).trim();
}

try {
  await vendor(repoRoot(), new GitHubClient(), (line) => console.log(line));
} catch (error) {
  console.error(`vendor-docker-docs: ${(error as Error).message}`);
  process.exit(1);
}
