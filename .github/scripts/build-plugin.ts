import { execSync } from "child_process";
import { existsSync, readFileSync } from "fs";
import { join } from "path";

const pluginName = process.argv[2];
if (!pluginName) {
  console.error("Usage: build-plugin <plugin-name>");
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

console.log(`Building ${pluginName}`);

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function containsForbiddenCommand(line: string, pattern: string): boolean {
  const escapedPattern = escapeRegExp(pattern);
  return new RegExp(`(?:^|\\s)${escapedPattern}(?:\\s|$)`).test(line);
}

const justfilePath = join(pluginPath, "justfile");
if (existsSync(justfilePath)) {
  const forbidden = ["go build", "go test", "go-toolchain", "go-safe-build"];
  const lines = readFileSync(justfilePath, "utf8").split("\n");
  for (let i = 0; i < lines.length; i++) {
    const trimmedLine = lines[i].trim();
    if (trimmedLine === "" || trimmedLine.startsWith("#")) continue;
    for (const pat of forbidden) {
      if (containsForbiddenCommand(trimmedLine, pat)) {
        console.error(`justfile:${i + 1}: forbidden command "${pat}"`);
        process.exit(1);
      }
    }
  }
}

function hasRecipe(recipe: string): boolean {
  if (!existsSync(justfilePath)) return false;
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

interface Hook {
  command?: string;
}

interface HookMatcher {
  hooks?: Hook[];
}

interface PluginJson {
  hooks?: Record<string, HookMatcher[]>;
}

const pluginJsonPath = join(pluginPath, ".claude-plugin", "plugin.json");
if (existsSync(pluginJsonPath)) {
  const pj: PluginJson = JSON.parse(readFileSync(pluginJsonPath, "utf8"));
  if (pj.hooks && typeof pj.hooks === "object") {
    const prefix = "${CLAUDE_PLUGIN_ROOT}/";
    const missing: string[] = [];
    for (const matchers of Object.values(pj.hooks)) {
      if (!Array.isArray(matchers)) continue;
      for (const matcher of matchers) {
        for (const hook of matcher.hooks ?? []) {
          const cmd = hook.command;
          if (!cmd || !cmd.startsWith(prefix)) continue;
          let rel = cmd.slice(prefix.length);
          const sp = rel.indexOf(" ");
          if (sp !== -1) rel = rel.slice(0, sp);
          if (!existsSync(join(pluginPath, rel))) {
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
