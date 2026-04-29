import { execSync } from "child_process";

const branchName = process.argv[2];
if (!branchName) {
  console.error("Usage: cleanup-branch <branch-name>");
  process.exit(1);
}

const marketplaceTag = `marketplace/${branchName}`;

console.log(`Cleaning up tag ${marketplaceTag}`);
try {
  execSync(`git push origin :refs/tags/${marketplaceTag}`, { stdio: "inherit" });
} catch {}
try {
  execSync(`git tag -d ${marketplaceTag}`, { stdio: "inherit" });
} catch {}
console.log(`Deleted marketplace tag for branch ${branchName}`);
