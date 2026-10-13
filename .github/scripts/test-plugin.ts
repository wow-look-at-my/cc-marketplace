import { execSync } from "child_process";
import { existsSync, readFileSync, statSync } from "fs";
import { join } from "path";

const pluginName = process.argv[2];
if (!pluginName) {
  console.error("Usage: test-plugin <plugin-name>");
  process.exit(1);
}

const repoRoot = execSync("git rev-parse --show-toplevel", {
  encoding: "utf8",
}).trim();
const pluginPath = join(repoRoot, "plugins", pluginName);

if (!existsSync(pluginPath)) {
  console.error(`plugin not found: ${pluginPath}`);
  process.exit(1);
}

console.log(`Testing ${pluginName}`);

// A server whose command names a file the plugin does not ship installs fine and fails only when a session starts it.
const ROOT = "${CLAUDE_PLUGIN_ROOT}/";
const problems: string[] = [];
for (const file of [".mcp.json", ".lsp.json"]) {
  const path = join(pluginPath, file);
  if (!existsSync(path)) continue;
  const doc = JSON.parse(readFileSync(path, "utf8"));
  for (const [name, server] of Object.entries(doc.mcpServers ?? doc) as [string, any][]) {
    if (typeof server?.command !== "string") continue;
    for (const [i, word] of [server.command, ...(server.args ?? [])].entries()) {
      if (typeof word !== "string" || !word.startsWith(ROOT)) continue;
      const target = join(pluginPath, word.slice(ROOT.length));
      if (!existsSync(target)) {
        problems.push(`${file} '${name}' names ${word}, and the built plugin has no such file`);
      } else if (i === 0 && (statSync(target).mode & 0o111) === 0) {
        problems.push(`${file} '${name}' runs ${word}, which is not executable`);
      }
    }
  }
}
if (problems.length > 0) {
  for (const p of problems) console.error(`  FAIL ${p}`);
  process.exit(1);
}
console.log("  every server command names a file the plugin ships");
