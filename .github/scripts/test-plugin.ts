import { execSync } from "child_process";
import { existsSync } from "fs";
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
console.log("  (no additional tests to run)");
