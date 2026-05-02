import { restoreCache } from "@actions/cache";
import { createHash } from "crypto";
import { readdirSync, readFileSync, statSync } from "fs";
import { join } from "path";

const plugins = JSON.parse(process.argv[2] || "[]") as string[];
if (plugins.length === 0) {
  console.log("No cached plugins to restore");
  process.exit(0);
}

function hashFiles(dir: string): string {
  const files: string[] = [];
  function walk(d: string) {
    for (const entry of readdirSync(d, { withFileTypes: true })) {
      const full = join(d, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.isFile()) files.push(full);
    }
  }
  walk(dir);
  files.sort();

  let concat = "";
  for (const file of files) {
    const hash = createHash("sha256").update(readFileSync(file)).digest("hex");
    concat += hash;
  }
  return createHash("sha256").update(concat).digest("hex");
}

let failed = 0;

for (const plugin of plugins) {
  const dir = join("plugins", plugin);
  const hash = hashFiles(dir);
  const key = `plugin-${plugin}-${hash}`;
  const path = `/tmp/packaged-${plugin}`;

  console.log(`Restoring ${plugin} (key: ${key})`);
  const hit = await restoreCache([path], key);
  if (hit) {
    console.log(`  restored from cache`);
  } else {
    console.error(`  MISS — cache not found for ${plugin}`);
    failed++;
  }
}

if (failed > 0) {
  console.error(`${failed} plugin(s) failed to restore from cache`);
  process.exit(1);
}
