#!/usr/bin/env node
const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const repoRoot = execSync("git rev-parse --show-toplevel", {
  encoding: "utf8",
}).trim();
const pluginsDir = path.join(repoRoot, "plugins");

const failed = [];
for (const entry of fs.readdirSync(pluginsDir, { withFileTypes: true })) {
  if (!entry.isDirectory()) continue;
  const pjPath = path.join(
    pluginsDir,
    entry.name,
    ".claude-plugin",
    "plugin.json",
  );
  if (!fs.existsSync(pjPath)) continue;
  try {
    const pj = JSON.parse(fs.readFileSync(pjPath, "utf8"));
    if (!pj.mh?.include_in_marketplace) continue;
  } catch {
    continue;
  }
  try {
    execSync(`node ${path.join(__dirname, "test-plugin.js")} ${entry.name}`, {
      stdio: "inherit",
    });
  } catch {
    failed.push(entry.name);
  }
}

if (failed.length > 0) {
  console.error(`Failed plugins: ${failed.join(", ")}`);
  process.exit(1);
}
