import { execSync } from "child_process";
import { readdirSync, existsSync, readFileSync } from "fs";
import { join, dirname } from "path";
import { fileURLToPath } from "url";

const repoRoot = execSync("git rev-parse --show-toplevel", {
  encoding: "utf8",
}).trim();
const pluginsDir = join(repoRoot, "plugins");
const scriptDir = dirname(fileURLToPath(import.meta.url));

interface PluginJson {
  mh?: { include_in_marketplace?: boolean };
}

const failed: string[] = [];
for (const entry of readdirSync(pluginsDir, { withFileTypes: true })) {
  if (!entry.isDirectory()) continue;
  const pjPath = join(pluginsDir, entry.name, ".claude-plugin", "plugin.json");
  if (!existsSync(pjPath)) continue;
  try {
    const pj: PluginJson = JSON.parse(readFileSync(pjPath, "utf8"));
    if (!pj.mh?.include_in_marketplace) continue;
  } catch {
    continue;
  }
  try {
    execSync(`npx tsx ${join(scriptDir, "test-plugin.ts")} ${entry.name}`, {
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
