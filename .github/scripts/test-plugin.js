#!/usr/bin/env node
const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const pluginName = process.argv[2];
if (!pluginName) {
  console.error("Usage: test-plugin <plugin-name>");
  process.exit(1);
}

const repoRoot = execSync("git rev-parse --show-toplevel", {
  encoding: "utf8",
}).trim();
const pluginPath = path.join(repoRoot, "plugins", pluginName);

if (!fs.existsSync(pluginPath)) {
  console.error(`plugin not found: ${pluginPath}`);
  process.exit(1);
}

console.log(`Testing ${pluginName}`);
console.log("  (no additional tests to run)");
