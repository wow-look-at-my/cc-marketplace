import { hashFiles } from "@actions/glob";
import { execSync } from "child_process";

const plugins = JSON.parse(process.argv[2] || "[]") as string[];
if (plugins.length === 0) {
  console.log("build_plugins=[]");
  console.log("cached_plugins=[]");
  console.log("has_build=false");
  process.exit(0);
}

const repo = process.env.GITHUB_REPOSITORY;
const ref = process.env.CACHE_REF;

const buildPlugins: string[] = [];
const cachedPlugins: string[] = [];

for (const plugin of plugins) {
  const hash = await hashFiles(`plugins/${plugin}/**`);
  const key = `plugin-${plugin}-${hash}`;

  let cached = false;
  try {
    const out = execSync(
      `gh api "repos/${repo}/actions/caches?key=${encodeURIComponent(key)}&ref=${encodeURIComponent(ref!)}" --jq ".actions_caches | length"`,
      { encoding: "utf8", env: { ...process.env, GH_TOKEN: process.env.GH_TOKEN } },
    ).trim();
    cached = parseInt(out, 10) > 0;
  } catch {
    cached = false;
  }

  if (cached) {
    cachedPlugins.push(plugin);
    console.error(`  ${plugin}: cached (${key})`);
  } else {
    buildPlugins.push(plugin);
    console.error(`  ${plugin}: needs build (${key})`);
  }
}

console.log(`build_plugins=${JSON.stringify(buildPlugins)}`);
console.log(`cached_plugins=${JSON.stringify(cachedPlugins)}`);
console.log(`has_build=${buildPlugins.length > 0}`);
