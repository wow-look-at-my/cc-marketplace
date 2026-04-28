import { execSync } from "child_process";

const branchName = process.argv[2];
if (!branchName) {
  console.error("Usage: cleanup-branch <branch-name>");
  process.exit(1);
}

const marketplaceTag = `marketplace/${branchName}`;
const allTags = execSync('git tag -l "marketplace/*"', { encoding: "utf8" })
  .trim()
  .split("\n")
  .filter(Boolean);

const tag = allTags.find((t) => t === marketplaceTag);
if (!tag) {
  console.log(`No marketplace tag found for branch ${branchName}`);
  process.exit(0);
}

console.log(`Cleaning up tag ${tag}`);
try {
  execSync(`git push origin :refs/tags/${tag}`, { stdio: "inherit" });
} catch {}
try {
  execSync(`git tag -d ${tag}`, { stdio: "inherit" });
} catch {}
console.log(`Deleted marketplace tag for branch ${branchName}`);
