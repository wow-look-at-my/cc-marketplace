import { restoreCache } from "@actions/cache";
import { hashFiles } from "@actions/glob";

const plugins = JSON.parse(process.argv[2] || "[]") as string[];
if (plugins.length === 0) {
  process.exit(0);
}

let failed = 0;
for (const plugin of plugins) {
  const hash = await hashFiles(`plugins/${plugin}/**`);
  const key = `plugin-${plugin}-${hash}`;
  const path = `/tmp/packaged-${plugin}`;

  console.log(`Restoring ${plugin} (${key})`);
  const hit = await restoreCache([path], key);
  if (hit) {
    console.log(`  OK`);
  } else {
    console.error(`  MISS — not found`);
    failed++;
  }
}

if (failed > 0) {
  process.exit(1);
}
