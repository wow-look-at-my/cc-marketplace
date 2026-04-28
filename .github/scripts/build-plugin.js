#!/usr/bin/env node
const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const pluginName = process.argv[2];
if (!pluginName) {
  console.error("Usage: build-plugin <plugin-name>");
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

console.log(`Building ${pluginName}`);

const justfilePath = path.join(pluginPath, "justfile");
if (fs.existsSync(justfilePath)) {
  const forbidden = ["go build", "go test", "go-toolchain", "go-safe-build"];
  const lines = fs.readFileSync(justfilePath, "utf8").split("\n");
  for (let i = 0; i < lines.length; i++) {
    for (const pat of forbidden) {
      if (lines[i].includes(pat)) {
        console.error(`justfile:${i + 1}: forbidden command "${pat}"`);
        process.exit(1);
      }
    }
  }
}

function hasRecipe(recipe) {
  if (!fs.existsSync(justfilePath)) return false;
  try {
    const summary = execSync("just --summary", {
      cwd: pluginPath,
      encoding: "utf8",
    });
    return summary.split(/\s+/).includes(recipe);
  } catch {
    return false;
  }
}

for (const recipe of ["prebuild", "postbuild"]) {
  if (hasRecipe(recipe)) {
    console.log(`  Running just ${recipe}`);
    execSync(`just ${recipe}`, { cwd: pluginPath, stdio: "inherit" });
  }
}

const pluginJsonPath = path.join(
  pluginPath,
  ".claude-plugin",
  "plugin.json",
);
if (fs.existsSync(pluginJsonPath)) {
  const hooks = JSON.parse(fs.readFileSync(pluginJsonPath, "utf8")).hooks;
  if (hooks && typeof hooks === "object") {
    const prefix = "${CLAUDE_PLUGIN_ROOT}/";
    const missing = [];
    for (const matchers of Object.values(hooks)) {
      if (!Array.isArray(matchers)) continue;
      for (const matcher of matchers) {
        for (const hook of matcher.hooks || []) {
          const cmd = hook.command;
          if (!cmd || !cmd.startsWith(prefix)) continue;
          let rel = cmd.slice(prefix.length);
          const sp = rel.indexOf(" ");
          if (sp !== -1) rel = rel.slice(0, sp);
          if (!fs.existsSync(path.join(pluginPath, rel))) {
            missing.push(`  ${rel} (from: ${cmd})`);
          }
        }
      }
    }
    if (missing.length > 0) {
      console.error(`Missing hook binaries:\n${missing.join("\n")}`);
      process.exit(1);
    }
  }
}
