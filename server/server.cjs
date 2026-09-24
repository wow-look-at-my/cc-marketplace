"use strict";

// plugins/slopfix/src/slopfix.ts
var import_node_child_process = require("node:child_process");
var import_node_fs = require("node:fs");
var import_node_path = require("node:path");
var import_node_url = require("node:url");
var import_meta = {};
var CANDIDATES = ["bin/slopfix.ape"];
function moduleDir() {
  if (typeof __dirname !== "undefined") return __dirname;
  return (0, import_node_path.dirname)((0, import_node_url.fileURLToPath)(import_meta.url));
}
var cached;
function slopfixPath() {
  const named = process.env.COMMON_CHECKS_SLOPFIX;
  if (named) return (0, import_node_fs.existsSync)(named) ? named : void 0;
  if (cached !== void 0) return cached || void 0;
  const root = (0, import_node_path.join)(moduleDir(), "..");
  for (const candidate of CANDIDATES) {
    const path = (0, import_node_path.join)(root, candidate);
    if ((0, import_node_fs.existsSync)(path)) {
      cached = path;
      return path;
    }
  }
  cached = "";
  return void 0;
}
var SlopfixUnavailable = class extends Error {
};
function report(relativePath2, content) {
  const binary = slopfixPath();
  if (!binary) {
    throw new SlopfixUnavailable(
      "no slopfix binary beside this plugin, so nothing can be checked. The plugin ships one under bin/. Set COMMON_CHECKS_SLOPFIX to point at another."
    );
  }
  let out;
  try {
    out = (0, import_node_child_process.execFileSync)(binary, ["report", "--path", relativePath2], {
      input: content,
      encoding: "utf8",
      maxBuffer: 32 * 1024 * 1024
    });
  } catch (error) {
    throw new SlopfixUnavailable(`${binary} could not read ${relativePath2}: ${error.message}`);
  }
  try {
    return JSON.parse(out).findings ?? [];
  } catch (error) {
    throw new SlopfixUnavailable(`${binary} answered with something that is not JSON: ${error.message}`);
  }
}

// plugins/slopfix/src/checks.ts
function fileKind(relativePath2) {
  const path = relativePath2.replace(/\\/g, "/").replace(/^\.\//, "");
  if (/^\.github\/workflows\/[^/]+\.ya?ml$/.test(path)) return "workflow";
  if (/(^|\/)action\.ya?ml$/.test(path)) return "action";
  if (/\.md$/i.test(path)) return "markdown";
  return "other";
}
var FAMILY_ORDER = ["yaml", "ste", "wrap"];
function rank(check) {
  const index = FAMILY_ORDER.indexOf(check.split("/")[0]);
  return index === -1 ? FAMILY_ORDER.length : index;
}
function message(rule, detail, fix) {
  const quoted = detail ? ` ${JSON.stringify(detail)}` : "";
  const repair = fix ? ` ${fix}` : "";
  return `${rule}${quoted}.${repair}`;
}
function findings(relativePath2, content) {
  if (fileKind(relativePath2) === "other") return [];
  const out = report(relativePath2, content).map((finding) => ({
    check: finding.id,
    startLine: finding.line,
    endLine: finding.endLine || finding.line,
    message: message(finding.rule, finding.detail, finding.fix)
  }));
  return out.sort((a, b) => rank(a.check) - rank(b.check) || a.startLine - b.startLine);
}

// plugins/slopfix/src/scope.ts
var import_node_fs2 = require("node:fs");
var import_node_os = require("node:os");
var import_node_path2 = require("node:path");
function claudeConfigDir() {
  const home = process.env.HOME ?? (0, import_node_os.homedir)();
  if (!home) return void 0;
  return (0, import_node_path2.join)((0, import_node_path2.resolve)(home), ".claude");
}
function underClaudeConfig(path) {
  const dir = claudeConfigDir();
  if (dir === void 0) return false;
  return path === dir || path.startsWith(dir + import_node_path2.sep);
}
function insideWorkTree(path) {
  return workTree(path) !== void 0;
}
function workTree(absPath) {
  try {
    if (typeof absPath !== "string" || absPath === "" || !(0, import_node_path2.isAbsolute)(absPath)) return void 0;
    let dir = (0, import_node_path2.dirname)((0, import_node_path2.resolve)(absPath));
    for (; ; ) {
      if ((0, import_node_fs2.existsSync)((0, import_node_path2.join)(dir, ".git"))) return dir;
      const parent = (0, import_node_path2.dirname)(dir);
      if (parent === dir) return void 0;
      dir = parent;
    }
  } catch {
    return void 0;
  }
}
function inScope(absPath) {
  try {
    if (typeof absPath !== "string" || absPath === "" || !(0, import_node_path2.isAbsolute)(absPath)) return false;
    const path = (0, import_node_path2.resolve)(absPath);
    if (underClaudeConfig(path)) return false;
    return insideWorkTree(path);
  } catch {
    return false;
  }
}

// plugins/slopfix/src/lsp.ts
var SOURCE = "common-checks";
var MAX_PER_FILE = Number(process.env.COMMON_CHECKS_LSP_MAX_PER_FILE) || 10;
function toDiagnostic(finding, lines) {
  const start = Math.max(0, finding.startLine - 1);
  const end = Math.max(start, finding.endLine - 1);
  const width = lines[end]?.length ?? 0;
  return {
    range: { start: { line: start, character: 0 }, end: { line: end, character: width } },
    severity: 1,
    source: SOURCE,
    code: finding.check,
    message: finding.message
  };
}
function diagnosticsFor(relativePath2, content) {
  let all;
  try {
    all = findings(relativePath2, content);
  } catch (error) {
    process.stderr.write(
      `common-checks: no diagnostics for ${relativePath2} -- ${error instanceof Error ? error.message : String(error)}
`
    );
    return [];
  }
  const lines = content.split(/\r?\n/);
  const shown = all.slice(0, MAX_PER_FILE).map((finding) => toDiagnostic(finding, lines));
  const hidden = all.length - shown.length;
  if (hidden > 0 && shown.length > 0) {
    shown[shown.length - 1].message += ` (+${hidden} more common-checks findings in this file)`;
  }
  return shown;
}
function pathOf(uri) {
  if (!uri.startsWith("file://")) return uri;
  try {
    return decodeURIComponent(uri.slice("file://".length));
  } catch {
    return uri.slice("file://".length);
  }
}
function relativize(uri, root) {
  const path = pathOf(uri).replace(/\\/g, "/");
  if (root) {
    const base = pathOf(root).replace(/\\/g, "/").replace(/\/+$/, "");
    if (path.startsWith(`${base}/`)) return path.slice(base.length + 1);
  }
  const workflows = path.lastIndexOf("/.github/workflows/");
  if (workflows !== -1) return path.slice(workflows + 1);
  const slash = path.lastIndexOf("/");
  return slash === -1 ? path : path.slice(slash + 1);
}
var Server = class {
  constructor(write) {
    this.write = write;
  }
  write;
  #root;
  #open = /* @__PURE__ */ new Map();
  /** Frames one JSON-RPC message the way the base protocol requires. */
  #send(payload) {
    const body = JSON.stringify(payload);
    this.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r
\r
${body}`);
  }
  // Assembled by hand so a null result marshals as an explicit `null`. A reply
  // carrying neither `result` nor `error` is malformed JSON-RPC, and the
  // client's reader rejects it -- which is how a clean session still fails at
  // shutdown.
  #respond(id, result) {
    this.#send({ jsonrpc: "2.0", id, result: result === void 0 ? null : result });
  }
  /**
   * Whether a document is one the checks judge at all.
   *
   * Scope is asked of the URI's own path, which is absolute. The relative form
   * below is for the checks, and it cannot answer where the file lives.
   */
  #judged(uri, relative) {
    return inScope(pathOf(uri)) && fileKind(relative) !== "other";
  }
  #publish(uri) {
    const content = this.#open.get(uri);
    if (content === void 0) return;
    const relative = relativize(uri, this.#root);
    const diagnostics = this.#judged(uri, relative) ? diagnosticsFor(relative, content) : [];
    this.#send({
      jsonrpc: "2.0",
      method: "textDocument/publishDiagnostics",
      params: { uri, diagnostics }
    });
  }
  handle(message2) {
    const { id, method, params } = message2;
    switch (method) {
      case "initialize": {
        const folders = params?.workspaceFolders;
        this.#root = params?.rootUri ?? folders?.[0]?.uri;
        this.#respond(id, {
          // Full sync: a finding is a property of the whole document, and an
          // incremental edit would need the server to rebuild it anyway.
          capabilities: { textDocumentSync: 1, diagnosticProvider: { interFileDependencies: false, workspaceDiagnostics: false } },
          serverInfo: { name: SOURCE, version: "1" }
        });
        return;
      }
      case "initialized":
        return;
      case "shutdown":
        this.#respond(id, null);
        return;
      case "exit":
        return;
      case "textDocument/didOpen": {
        const doc = params?.textDocument;
        if (!doc?.uri) return;
        this.#open.set(doc.uri, doc.text ?? "");
        this.#publish(doc.uri);
        return;
      }
      case "textDocument/didChange": {
        const doc = params?.textDocument;
        const changes = params?.contentChanges;
        const text = changes?.[changes.length - 1]?.text;
        if (!doc?.uri || text === void 0) return;
        this.#open.set(doc.uri, text);
        this.#publish(doc.uri);
        return;
      }
      case "textDocument/didSave": {
        const doc = params?.textDocument;
        if (doc?.uri) this.#publish(doc.uri);
        return;
      }
      case "textDocument/didClose": {
        const doc = params?.textDocument;
        if (!doc?.uri) return;
        this.#open.delete(doc.uri);
        this.#send({ jsonrpc: "2.0", method: "textDocument/publishDiagnostics", params: { uri: doc.uri, diagnostics: [] } });
        return;
      }
      // Claude Code never calls this -- it registers a publishDiagnostics
      // handler and nothing else. It is here so the server is correct for an
      // editor that pulls.
      case "textDocument/diagnostic": {
        const doc = params?.textDocument;
        const content = doc?.uri ? this.#open.get(doc.uri) : void 0;
        const relative = doc?.uri ? relativize(doc.uri, this.#root) : "";
        const items = content === void 0 || !doc?.uri || !this.#judged(doc.uri, relative) ? [] : diagnosticsFor(relative, content);
        this.#respond(id, { kind: "full", items });
        return;
      }
      default:
        if (id !== void 0 && id !== null) {
          this.#send({ jsonrpc: "2.0", id, error: { code: -32601, message: `unknown method: ${method}` } });
        }
    }
  }
};
var Framing = class {
  #buffer = Buffer.alloc(0);
  push(chunk) {
    this.#buffer = Buffer.concat([this.#buffer, chunk]);
    const out = [];
    for (; ; ) {
      const split = this.#buffer.indexOf("\r\n\r\n");
      if (split === -1) return out;
      const header = this.#buffer.subarray(0, split).toString("ascii");
      const length = /content-length:\s*(\d+)/i.exec(header);
      if (!length) {
        this.#buffer = this.#buffer.subarray(split + 4);
        continue;
      }
      const size = Number(length[1]);
      const start = split + 4;
      if (this.#buffer.length < start + size) return out;
      const body = this.#buffer.subarray(start, start + size).toString("utf8");
      this.#buffer = this.#buffer.subarray(start + size);
      try {
        out.push(JSON.parse(body));
      } catch {
      }
    }
  }
};
function serve(input, write) {
  const framing = new Framing();
  const server = new Server(write);
  input.on("data", (chunk) => {
    for (const message2 of framing.push(chunk)) {
      try {
        server.handle(message2);
      } catch (error) {
        process.stderr.write(`common-checks: ${error instanceof Error ? error.stack : String(error)}
`);
      }
    }
  });
}

// plugins/slopfix/src/ledger.ts
var import_node_crypto = require("node:crypto");
var import_node_fs3 = require("node:fs");
var import_node_path3 = require("node:path");
var import_node_os2 = require("node:os");
function ledgerDir(sessionId) {
  if (sessionId === "") return void 0;
  const key = (0, import_node_crypto.createHash)("sha256").update(sessionId).digest("hex").slice(0, 16);
  const dir = (0, import_node_path3.join)((0, import_node_os2.tmpdir)(), "common-checks", key);
  try {
    (0, import_node_fs3.mkdirSync)(dir, { recursive: true });
    return dir;
  } catch {
    return void 0;
  }
}
function entryName(filePath) {
  return (0, import_node_crypto.createHash)("sha256").update(filePath).digest("hex").slice(0, 32);
}
function record(sessionId, filePath, ids) {
  const dir = ledgerDir(sessionId);
  if (dir === void 0) return;
  const entry = { path: filePath, ids };
  try {
    (0, import_node_fs3.writeFileSync)((0, import_node_path3.join)(dir, entryName(filePath)), JSON.stringify(entry), "utf8");
  } catch {
  }
}
function forget(sessionId, filePath) {
  const dir = ledgerDir(sessionId);
  if (dir === void 0) return;
  try {
    (0, import_node_fs3.rmSync)((0, import_node_path3.join)(dir, entryName(filePath)), { force: true });
  } catch {
  }
}
function outstanding(sessionId) {
  const dir = ledgerDir(sessionId);
  if (dir === void 0) return [];
  try {
    return (0, import_node_fs3.readdirSync)(dir).map((name) => read((0, import_node_path3.join)(dir, name))).filter((entry) => entry !== void 0 && (0, import_node_fs3.existsSync)(entry.path));
  } catch {
    return [];
  }
}
function read(file) {
  let raw;
  try {
    raw = (0, import_node_fs3.readFileSync)(file, "utf8");
  } catch {
    return void 0;
  }
  if (raw === "") return void 0;
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return void 0;
    const { path, ids } = parsed;
    if (typeof path !== "string" || path === "") return void 0;
    return { path, ids: Array.isArray(ids) ? ids.filter((id) => typeof id === "string") : [] };
  } catch {
    return void 0;
  }
}
function diskContent(filePath) {
  try {
    return (0, import_node_fs3.readFileSync)(filePath, "utf8");
  } catch {
    return void 0;
  }
}

// plugins/slopfix/src/placement.ts
function place(filePath, oldString, newString) {
  if (filePath === "" || oldString === "") return void 0;
  const disk = diskContent(filePath);
  if (disk === void 0) return void 0;
  const at = disk.indexOf(oldString);
  if (at === -1) return void 0;
  if (disk.indexOf(oldString, at + 1) !== -1) return void 0;
  const prefix = disk.slice(0, at);
  const after = disk.slice(at + oldString.length);
  const start = prefix.split("\n").length - 1;
  const end = start + newString.split("\n").length - 1;
  return { full: prefix + newString + after, before: disk, start, end };
}

// plugins/slopfix/src/hook.ts
function unitsOf(toolName, input) {
  if (!WRITE_TOOLS.has(toolName)) return [];
  const filePath = typeof input.file_path === "string" ? input.file_path : "";
  const units = [];
  if (typeof input.content === "string") units.push({ text: input.content });
  if (typeof input.new_string === "string") {
    const old = typeof input.old_string === "string" ? input.old_string : "";
    units.push({ text: input.new_string, placement: place(filePath, old, input.new_string) });
  }
  if (Array.isArray(input.edits)) {
    for (const edit of input.edits) {
      const next = edit?.new_string;
      if (typeof next !== "string") continue;
      const old = edit?.old_string;
      units.push({
        text: next,
        placement: place(filePath, typeof old === "string" ? old : "", next)
      });
    }
  }
  return units;
}
function findingsFor(rel, unit) {
  if (unit.placement === void 0) return findings(rel, unit.text);
  const { full, before, start, end } = unit.placement;
  const carried = /* @__PURE__ */ new Map();
  for (const finding of findings(rel, before)) {
    const key = identity(finding);
    carried.set(key, (carried.get(key) ?? 0) + 1);
  }
  return findings(rel, full).filter((f) => f.startLine - 1 >= start && f.startLine - 1 <= end).filter((f) => {
    const left = carried.get(identity(f)) ?? 0;
    if (left === 0) return true;
    carried.set(identity(f), left - 1);
    return false;
  });
}
var WRITE_TOOLS = /* @__PURE__ */ new Set(["Write", "Edit", "MultiEdit", "NotebookEdit"]);
function relativePath(filePath, cwd) {
  if (cwd !== "" && filePath.startsWith(cwd)) {
    return filePath.slice(cwd.length).replace(/^[/\\]+/, "");
  }
  return filePath;
}
function blockingFindings(toolName, input, cwd) {
  const filePath = typeof input.file_path === "string" ? input.file_path : "";
  if (filePath === "") return [];
  if (!inScope(filePath)) return [];
  const rel = relativePath(filePath, cwd);
  if (fileKind(rel) === "other") return [];
  const found = [];
  for (const unit of unitsOf(toolName, input)) {
    found.push(...findingsFor(rel, unit));
  }
  return found;
}
function denyReason(found) {
  const lines = found.map((f) => `  ${f.check}: ${f.message}`);
  return "blocked: this write fails a check that gates every build in the org, so it would fail CI rather than the edit.\n" + lines.join("\n");
}
function describe(error) {
  return error instanceof Error ? error.message : String(error);
}
function identity(f) {
  return `${f.check} ${f.message}`;
}
function sweep(sessionId, cwd, project) {
  const still = [];
  for (const entry of outstanding(sessionId)) {
    if (project !== void 0 && workTree(entry.path) !== project) continue;
    if (!inScope(entry.path)) {
      forget(sessionId, entry.path);
      continue;
    }
    const content = diskContent(entry.path);
    if (content === void 0) {
      forget(sessionId, entry.path);
      continue;
    }
    const rel = relativePath(entry.path, cwd);
    const left = new Set(findings(rel, content).map(identity));
    if (!entry.ids.some((id) => left.has(id))) {
      forget(sessionId, entry.path);
      continue;
    }
    still.push(entry.path);
  }
  return still;
}
function otherFileReason(paths) {
  return "blocked: a file already carries a violation that fails CI, and this write is to a different file.\n" + paths.map((p) => `  ${p}`).join("\n") + "\nFix that first. This clears itself once the file is clean.";
}
function decide(raw) {
  let payload;
  try {
    payload = JSON.parse(raw);
  } catch {
    return "";
  }
  if (payload.hook_event_name !== "PreToolUse") return "";
  if (typeof payload.tool_name !== "string") return "";
  const input = payload.tool_input ?? {};
  if (typeof input !== "object" || input === null) return "";
  const cwd = typeof payload.cwd === "string" ? payload.cwd : "";
  const sessionId = typeof payload.session_id === "string" ? payload.session_id : "";
  const filePath = typeof input.file_path === "string" ? input.file_path : "";
  const project = workTree(filePath);
  const stuck = project === void 0 ? [] : sweep(sessionId, cwd, project).filter((p) => p !== filePath);
  if (stuck.length > 0) return otherFileReason(stuck);
  const found = blockingFindings(payload.tool_name, input, cwd);
  if (found.length === 0) return "";
  if (filePath !== "") record(sessionId, filePath, found.map(identity));
  return denyReason(found);
}
async function runHook(stdin) {
  const chunks = [];
  for await (const chunk of stdin) chunks.push(Buffer.from(chunk));
  let reason = "";
  try {
    reason = decide(Buffer.concat(chunks).toString("utf8"));
  } catch (error) {
    process.stderr.write(`common-checks: allowing this write unchecked -- ${describe(error)}
`);
    reason = "";
  }
  if (reason === "") return;
  process.stdout.write(
    JSON.stringify({
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: reason
      }
    })
  );
}

// plugins/slopfix/src/server.ts
if (process.argv.includes("--hook")) {
  void runHook(process.stdin);
} else {
  serve(process.stdin, (chunk) => process.stdout.write(chunk));
}
