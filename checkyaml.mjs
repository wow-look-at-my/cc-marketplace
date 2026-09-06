import { readFileSync } from "node:fs";
import { parse } from "yaml";
const path = process.argv[2];
const doc = parse(readFileSync(path, "utf8"));
console.log("parsed:", path);
console.log("concurrency:", JSON.stringify(doc.concurrency));
console.log("jobs:", Object.keys(doc.jobs ?? {}).length > 0 ? "present" : "MISSING");
