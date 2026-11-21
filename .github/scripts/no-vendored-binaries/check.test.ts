import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

import {
  binariesInAssembled,
  binariesInSource,
  binaryKind,
  checkSource,
  downloadsIntoPlugins,
  manifestPaths,
  untrackedManifestPaths,
} from "./check.ts";

const FORMATS: Array<[string, number[]]> = [
  ["ELF", [0x7f, 0x45, 0x4c, 0x46, 2, 1, 1, 0]],
  ["Mach-O 32", [0xfe, 0xed, 0xfa, 0xce, 0, 0]],
  ["Mach-O 64", [0xfe, 0xed, 0xfa, 0xcf, 0, 0]],
  ["Mach-O 32 swapped", [0xce, 0xfa, 0xed, 0xfe, 0, 0]],
  ["Mach-O 64 swapped", [0xcf, 0xfa, 0xed, 0xfe, 0, 0]],
  ["universal", [0xca, 0xfe, 0xba, 0xbe, 0, 0]],
  ["universal 64", [0xca, 0xfe, 0xba, 0xbf, 0, 0]],
  ["universal swapped", [0xbe, 0xba, 0xfe, 0xca, 0, 0]],
  ["universal 64 swapped", [0xbf, 0xba, 0xfe, 0xca, 0, 0]],
  ["PE", [0x4d, 0x5a, 0x90, 0x00, 3, 0]],
  ["APE", [...Buffer.from("MZqFpD='\n")]],
];

function scratch(): string {
  return mkdtempSync(join(tmpdir(), "no-vendored-binaries-"));
}

function put(root: string, rel: string, body: string | Uint8Array, mode = 0o644): void {
  const abs = join(root, rel);
  mkdirSync(dirname(abs), { recursive: true });
  writeFileSync(abs, body, { mode });
}

// A repository whose index holds the given files, which is all `git ls-files` reads.
function repo(files: Record<string, string | Uint8Array>): string {
  const root = scratch();
  execFileSync("git", ["init", "-q", root]);
  for (const [rel, body] of Object.entries(files)) put(root, rel, body);
  execFileSync("git", ["-C", root, "add", "-A"]);
  return root;
}

function stage(root: string): void {
  execFileSync("git", ["-C", root, "add", "-A"]);
}

const cleanPlugin = {
  "plugins/p/.claude-plugin/plugin.json": JSON.stringify({
    name: "p",
    hooks: { Stop: [{ hooks: [{ type: "command", command: "${CLAUDE_PLUGIN_ROOT}/scripts/launch cc-marketplace/p hook" }] }] },
  }),
  "plugins/p/.mcp.json": JSON.stringify({
    mcpServers: { p: { command: "${CLAUDE_PLUGIN_ROOT}/scripts/launch", args: ["cc-marketplace/p"], cwd: "${CLAUDE_PLUGIN_ROOT}" } },
  }),
  "plugins/p/scripts/launch": "#!/bin/sh\nexec true\n",
  "plugins/p/README.md": "# p\n",
};

test("every executable format is recognized by its first bytes", () => {
  for (const [name, bytes] of FORMATS) {
    assert.ok(binaryKind(Uint8Array.from(bytes)), `${name} must be recognized`);
  }
  assert.equal(binaryKind(Buffer.from("MZqFpD='\n")), "an APE");
  assert.equal(binaryKind(Buffer.from("#!/bin/sh\n")), undefined);
  assert.equal(binaryKind(Buffer.from("{\"name\":\"p\"}")), undefined);
  assert.equal(binaryKind(new Uint8Array()), undefined);
});

test("check one fails on each binary format planted in the assembled release, whatever its name", () => {
  for (const [name, bytes] of FORMATS) {
    const cooked = scratch();
    put(cooked, "p/.claude-plugin/plugin.json", "{}");
    put(cooked, "p/skills/x/notes.txt", Uint8Array.from(bytes));
    const findings = binariesInAssembled(cooked);
    assert.equal(findings.length, 1, `${name} under a harmless name must still be caught`);
    assert.equal(findings[0].file, "p/skills/x/notes.txt");
  }
});

test("check one passes on an assembled release of text", () => {
  const cooked = scratch();
  put(cooked, "p/.claude-plugin/plugin.json", "{}");
  put(cooked, "p/scripts/launch", "#!/bin/sh\nexec true\n", 0o755);
  put(cooked, "p/mh.plugin.json", "{\"branch\":\"master\"}");
  assert.deepEqual(binariesInAssembled(cooked), []);
});

test("check one fails on a binary committed under plugins/, and on a link that points at one", () => {
  const root = repo({ ...cleanPlugin, "plugins/p/build/p": Uint8Array.from(FORMATS[0][1]) });
  assert.deepEqual(binariesInSource(root).map((f) => f.file), ["plugins/p/build/p"]);

  const linked = repo(cleanPlugin);
  put(linked, "tools/p", Uint8Array.from(FORMATS[10][1]));
  symlinkSync("../../../tools/p", join(linked, "plugins/p/scripts/p"));
  stage(linked);
  assert.deepEqual(binariesInSource(linked).map((f) => f.file), ["plugins/p/scripts/p"]);
});

test("check one passes on a clean plugin", () => {
  assert.deepEqual(binariesInSource(repo(cleanPlugin)), []);
});

test("manifestPaths reads root paths out of commands, args and relative commands under a root cwd", () => {
  const paths = manifestPaths({
    hooks: { Stop: [{ hooks: [{ command: "${CLAUDE_PLUGIN_ROOT}/build/p --flag ${OTHER:-1}" }] }] },
    servers: { a: { command: "node", args: ["${CLAUDE_PLUGIN_ROOT}/dist/server.js"] }, b: { command: "./run", cwd: "${CLAUDE_PLUGIN_ROOT}" } },
  });
  assert.deepEqual(paths.sort(), ["build/p", "dist/server.js", "run"]);
});

test("check two fails on a hook, an MCP server and an LSP server that name build output", () => {
  const root = repo({
    ...cleanPlugin,
    "plugins/p/.claude-plugin/plugin.json": JSON.stringify({
      name: "p",
      hooks: { Stop: [{ hooks: [{ type: "command", command: "${CLAUDE_PLUGIN_ROOT}/build/p" }] }] },
    }),
    "plugins/p/.mcp.json": JSON.stringify({ mcpServers: { p: { command: "./run", cwd: "${CLAUDE_PLUGIN_ROOT}" } } }),
    "plugins/p/.lsp.json": JSON.stringify({ p: { command: "${CLAUDE_PLUGIN_ROOT}/bin/p.ape", extensionToLanguage: { ".x": "x" } } }),
  });
  const files = untrackedManifestPaths(root).map((f) => f.file).sort();
  assert.deepEqual(files, ["plugins/p/.claude-plugin/plugin.json", "plugins/p/.lsp.json", "plugins/p/.mcp.json"]);
});

test("check two still fails when the untracked file exists on disk, as a build would leave it", () => {
  const root = repo({
    ...cleanPlugin,
    "plugins/p/.claude-plugin/plugin.json": JSON.stringify({
      name: "p",
      hooks: { Stop: [{ hooks: [{ type: "command", command: "${CLAUDE_PLUGIN_ROOT}/build/p" }] }] },
    }),
  });
  put(root, "plugins/p/build/p", "#!/bin/sh\n", 0o755);
  assert.equal(untrackedManifestPaths(root).length, 1);
});

test("check two passes when every named path is tracked", () => {
  assert.deepEqual(untrackedManifestPaths(repo(cleanPlugin)), []);
});

const tool = "curl";
const other = "wget";

test("check three fails on a download into plugins/ in a workflow, a script and a plugin justfile", () => {
  const cases: Record<string, string> = {
    ".github/workflows/w.yml": [
      "on: push",
      "jobs:",
      "  j:",
      "    runs-on: ubuntu-latest",
      "    steps:",
      `      - run: ${tool} -fsSL -o plugins/p/bin/p https://example.com/p`,
    ].join("\n"),
    ".github/scripts/fetch.sh": `#!/bin/sh\n${other} -O plugins/p/bin/p https://example.com/p\n`,
    ".github/scripts/continued.sh": `#!/bin/sh\n${tool} -fsSL \\\n  -o plugins/p/bin/p https://example.com/p\n`,
    "plugins/p/justfile": `prebuild:\n\t${tool} -fsSL -o bin/p https://example.com/p\n`,
    ".github/workflows/wd.yml": [
      "on: push",
      "jobs:",
      "  j:",
      "    runs-on: ubuntu-latest",
      "    steps:",
      "      - working-directory: plugins/p",
      `        run: ${other} https://example.com/p`,
    ].join("\n"),
    ".github/workflows/artifact.yml": [
      "on: push",
      "jobs:",
      "  j:",
      "    runs-on: ubuntu-latest",
      "    steps:",
      "      - uses: actions/download-artifact@v4",
      "        with:",
      "          path: plugins/p/bin",
    ].join("\n"),
  };
  for (const [rel, body] of Object.entries(cases)) {
    const root = repo({ ...cleanPlugin, [rel]: body });
    const findings = downloadsIntoPlugins(root);
    assert.ok(findings.length > 0, `${rel} must be caught`);
    assert.ok(findings.every((f) => f.file.startsWith(rel)), `${rel}: ${JSON.stringify(findings)}`);
  }
});

test("check three passes on downloads that write elsewhere and on comments", () => {
  const root = repo({
    ...cleanPlugin,
    ".github/scripts/fetch.sh": `#!/bin/sh\n${tool} -fsSL -o /tmp/p https://example.com/p\n# the plugins/ tree never holds a ${tool} download\n`,
  });
  assert.deepEqual(downloadsIntoPlugins(root), []);
});

test("the committed tree is clean", () => {
  const root = execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
  assert.deepEqual(checkSource(root), []);
});
