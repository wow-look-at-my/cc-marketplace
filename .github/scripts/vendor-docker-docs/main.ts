// Refreshes the Docker reference text the `docs` plugin's dockerfile and docker-compose skills read from.

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
