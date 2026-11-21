// A plugin ships text: manifests, skills, scripts. Every binary it runs comes
// from buildhost at run time through scripts/launch. These checks fail the run
// on anything that would put a binary inside a plugin.

import { execFileSync } from "node:child_process";
import { closeSync, existsSync, openSync, readFileSync, readdirSync, readSync, statSync } from "node:fs";
import { join, posix } from "node:path";
import { parse as parseYaml } from "yaml";

export interface Finding {
  file: string;
  message: string;
}

// The longest prefix first, so an APE is named as one rather than as its MZ header.
const MAGICS: Array<[string, number[]]> = [
  ["an APE", [0x4d, 0x5a, 0x71, 0x46, 0x70, 0x44]],
  ["an ELF binary", [0x7f, 0x45, 0x4c, 0x46]],
  ["a 32-bit Mach-O binary", [0xfe, 0xed, 0xfa, 0xce]],
  ["a 64-bit Mach-O binary", [0xfe, 0xed, 0xfa, 0xcf]],
  ["a byte-swapped 32-bit Mach-O binary", [0xce, 0xfa, 0xed, 0xfe]],
  ["a byte-swapped 64-bit Mach-O binary", [0xcf, 0xfa, 0xed, 0xfe]],
  ["a universal Mach-O binary", [0xca, 0xfe, 0xba, 0xbe]],
  ["a 64-bit universal Mach-O binary", [0xca, 0xfe, 0xba, 0xbf]],
  ["a byte-swapped universal Mach-O binary", [0xbe, 0xba, 0xfe, 0xca]],
  ["a byte-swapped 64-bit universal Mach-O binary", [0xbf, 0xba, 0xfe, 0xca]],
  ["a PE executable", [0x4d, 0x5a]],
];

const HEAD_BYTES = Math.max(...MAGICS.map(([, m]) => m.length));

// Names the executable format a file's first bytes announce, or returns undefined for anything else.
export function binaryKind(head: Uint8Array): string | undefined {
  for (const [kind, magic] of MAGICS) {
    if (head.length >= magic.length && magic.every((b, i) => head[i] === b)) return kind;
  }
  return undefined;
}

function readHead(path: string): Uint8Array {
  const fd = openSync(path, "r");
  try {
    const buf = Buffer.alloc(HEAD_BYTES);
    const n = readSync(fd, buf, 0, HEAD_BYTES, 0);
    return buf.subarray(0, n);
  } finally {
    closeSync(fd);
  }
}

// A link is judged by what it points at, because the release cook copies the target's bytes.
function binaryFinding(abs: string, shown: string): Finding | undefined {
  const st = statSync(abs);
  if (!st.isFile()) return undefined;
  const kind = binaryKind(readHead(abs));
  if (!kind) return undefined;
  return { file: shown, message: `${shown} is ${kind}. A plugin ships no binary; run it through scripts/launch instead.` };
}

function walkFiles(dir: string, rel = ""): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(join(dir, rel), { withFileTypes: true })) {
    const child = rel ? posix.join(rel, entry.name) : entry.name;
    if (entry.isDirectory()) out.push(...walkFiles(dir, child));
    else out.push(child);
  }
  return out;
}

// Check one, on the tree the release assembled: every file of every plugin that will be published.
export function binariesInAssembled(dir: string): Finding[] {
  const findings: Finding[] = [];
  for (const rel of walkFiles(dir)) {
    const f = binaryFinding(join(dir, rel), rel);
    if (f) findings.push(f);
  }
  return findings;
}

function gitFiles(root: string, ...pathspec: string[]): string[] {
  const out = execFileSync("git", ["-C", root, "ls-files", "-z", "--", ...pathspec], { encoding: "utf8" });
  return out.split("\0").filter(Boolean);
}

// Check one, on the committed tree.
export function binariesInSource(root: string): Finding[] {
  const findings: Finding[] = [];
  for (const rel of gitFiles(root, "plugins")) {
    const abs = join(root, rel);
    if (!existsSync(abs)) continue;
    const f = binaryFinding(abs, rel);
    if (f) findings.push(f);
  }
  return findings;
}

const ROOT_VAR = "${CLAUDE_PLUGIN_ROOT}";
const ROOT_PATH = /\$\{CLAUDE_PLUGIN_ROOT\}(\/[^\s"'`;|&<>()$]*)?/g;

// Every path under the plugin root that a manifest names, as plugin-relative paths.
export function manifestPaths(doc: unknown): string[] {
  const found = new Set<string>();
  const visit = (node: unknown): void => {
    if (typeof node === "string") {
      for (const m of node.matchAll(ROOT_PATH)) {
        const rel = (m[1] ?? "").replace(/^\/+/, "").replace(/\/+$/, "");
        if (rel) found.add(posix.normalize(rel));
      }
      return;
    }
    if (Array.isArray(node)) {
      node.forEach(visit);
      return;
    }
    if (!node || typeof node !== "object") return;
    const obj = node as Record<string, unknown>;
    // A relative command resolves against cwd, and a cwd under the plugin root makes it a plugin path.
    if (typeof obj.command === "string" && typeof obj.cwd === "string" && obj.cwd.startsWith(ROOT_VAR)) {
      const cmd = obj.command;
      if (cmd.startsWith("./") || cmd.startsWith("../")) {
        const base = obj.cwd.slice(ROOT_VAR.length).replace(/^\/+/, "");
        found.add(posix.normalize(posix.join(base, cmd)));
      }
    }
    Object.values(obj).forEach(visit);
  };
  visit(doc);
  return [...found];
}

const MANIFESTS = [".claude-plugin/plugin.json", ".mcp.json", ".lsp.json", "hooks/hooks.json"];

// The manifest files of one plugin: the fixed names, plus any file plugin.json points a component key at.
function pluginManifests(pluginDir: string): string[] {
  const files = MANIFESTS.filter((m) => existsSync(join(pluginDir, m)));
  const pj = join(pluginDir, ".claude-plugin/plugin.json");
  if (!existsSync(pj)) return files;
  const doc = JSON.parse(readFileSync(pj, "utf8")) as Record<string, unknown>;
  for (const key of ["hooks", "mcpServers", "lspServers"]) {
    const v = doc[key];
    if (typeof v === "string" && v.endsWith(".json")) files.push(posix.normalize(v));
  }
  return [...new Set(files)];
}

export function untrackedManifestPaths(root: string): Finding[] {
  const findings: Finding[] = [];
  const pluginsDir = join(root, "plugins");
  for (const entry of readdirSync(pluginsDir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const plugin = entry.name;
    const pluginDir = join(pluginsDir, plugin);
    const tracked = new Set(gitFiles(root, `plugins/${plugin}`).map((f) => f.slice(`plugins/${plugin}/`.length)));
    for (const manifest of pluginManifests(pluginDir)) {
      const shown = `plugins/${plugin}/${manifest}`;
      const doc = JSON.parse(readFileSync(join(pluginDir, manifest), "utf8"));
      for (const rel of manifestPaths(doc)) {
        const isFile = tracked.has(rel);
        const isDir = [...tracked].some((t) => t.startsWith(`${rel}/`));
        if (isFile || isDir) continue;
        findings.push({
          file: shown,
          message: `${shown} names ${ROOT_VAR}/${rel}, which git does not track in plugins/${plugin}. Something the build fetches or generates would ship there.`,
        });
      }
    }
  }
  return findings;
}

const DOWNLOADER = /\b(curl|wget)\b|\bgh\s+(release|run)\s+download\b|dl\.pazer\.build|\bbuildhost\b/;
const PLUGINS_PATH = /(^|[^\w.-])plugins\//;
const COMMENT = /^\s*(#|\/\/)/;

// Lines joined across backslash continuations, so one command is judged as one line.
function logicalLines(text: string): Array<{ line: number; text: string }> {
  const out: Array<{ line: number; text: string }> = [];
  const raw = text.split("\n");
  for (let i = 0; i < raw.length; i++) {
    const start = i;
    let joined = raw[i];
    while (joined.endsWith("\\") && i + 1 < raw.length) {
      i++;
      joined = joined.slice(0, -1) + " " + raw[i];
    }
    out.push({ line: start + 1, text: joined });
  }
  return out;
}

function isScript(rel: string, text: string): boolean {
  const base = posix.basename(rel);
  if (base === "justfile" || base === "Justfile" || base.endsWith(".just") || base === "Makefile") return true;
  if (/\.(sh|bash|ts|mts|cts|js|mjs|cjs|go|ya?ml|ps1)$/.test(base)) return true;
  return text.startsWith("#!");
}

// A justfile recipe runs in its own directory, so one inside a plugin writes there whatever path it names.
function inPluginDir(rel: string): boolean {
  return rel.startsWith("plugins/") && rel.split("/").length > 2;
}

function workflowSteps(doc: unknown): Array<Record<string, unknown>> {
  const steps: Array<Record<string, unknown>> = [];
  if (!doc || typeof doc !== "object") return steps;
  const d = doc as Record<string, any>;
  const jobs = d.jobs && typeof d.jobs === "object" ? Object.values(d.jobs as Record<string, any>) : [];
  for (const job of jobs) {
    const dir = job?.defaults?.run?.["working-directory"];
    for (const step of (job?.steps ?? []) as Array<Record<string, unknown>>) {
      steps.push({ "working-directory": dir, ...step });
    }
  }
  const runs = d.runs as Record<string, any> | undefined;
  for (const step of (runs?.steps ?? []) as Array<Record<string, unknown>>) steps.push(step);
  return steps;
}

export function downloadsIntoPlugins(root: string): Finding[] {
  const findings: Finding[] = [];
  for (const rel of gitFiles(root)) {
    const abs = join(root, rel);
    if (!existsSync(abs) || !statSync(abs).isFile()) continue;
    if (binaryKind(readHead(abs))) continue;
    const text = readFileSync(abs, "utf8");
    if (!isScript(rel, text)) continue;

    const pluginJustfile = inPluginDir(rel) && /^(justfile|Justfile|.*\.just|Makefile)$/.test(posix.basename(rel));
    for (const { line, text: l } of logicalLines(text)) {
      if (COMMENT.test(l) || !DOWNLOADER.test(l)) continue;
      if (!PLUGINS_PATH.test(l) && !pluginJustfile) continue;
      findings.push({ file: `${rel}:${line}`, message: `${rel}:${line} downloads into a plugin: ${l.trim()}` });
    }

    if (!/^\.github\/(workflows|actions)\/.*\.ya?ml$/.test(rel)) continue;
    for (const step of workflowSteps(parseYaml(text))) {
      const dir = String(step["working-directory"] ?? "");
      const run = String(step.run ?? "");
      if (PLUGINS_PATH.test(`${dir}/`) && DOWNLOADER.test(run)) {
        findings.push({ file: rel, message: `${rel} runs a download in ${dir}: ${run.trim().split("\n")[0]}` });
      }
      const uses = String(step.uses ?? "");
      const target = String((step.with as Record<string, unknown> | undefined)?.path ?? "");
      if (/download-artifact|release-download/.test(uses) && PLUGINS_PATH.test(target)) {
        findings.push({ file: rel, message: `${rel} downloads ${uses} into ${target}` });
      }
    }
  }
  return findings;
}

export function checkSource(root: string): Finding[] {
  return [...binariesInSource(root), ...untrackedManifestPaths(root), ...downloadsIntoPlugins(root)];
}
