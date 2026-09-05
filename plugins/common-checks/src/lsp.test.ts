import assert from "node:assert/strict";
import { test } from "node:test";
import { Framing, Server, diagnosticsFor, relativize } from "./lsp.ts";

const ROOT = "file:///repo";
const CI = "file:///repo/.github/workflows/ci.yml";

// A workflow that breaks three rules at once.
const DIRTY = ["# one", "# two", "on:", "  push:", "jobs:", "  all-builds:", "    runs-on: x", ""].join("\n");
const CLEAN = ["on:", "  push:", "    branches: ['**']", "jobs:", "  build:", "    runs-on: x", ""].join("\n");

function frame(payload: Record<string, unknown>): Buffer {
  const body = JSON.stringify(payload);
  return Buffer.from(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n\r\n${body}`, "utf8");
}

interface Sent {
  jsonrpc: string;
  id?: number;
  method?: string;
  result?: unknown;
  error?: unknown;
  params?: { uri: string; diagnostics: { message: string; code: string; severity: number; range: unknown }[] };
}

class Harness {
  readonly raw: string[] = [];
  readonly server: Server;
  constructor() {
    this.server = new Server((chunk) => this.raw.push(chunk));
  }
  send(method: string, params?: Record<string, unknown>, id?: number): void {
    this.server.handle({ id, method, params });
  }
  /** Every message the server wrote, with its framing removed. */
  get sent(): Sent[] {
    return this.raw.map((chunk) => JSON.parse(chunk.slice(chunk.indexOf("\r\n\r\n") + 4)) as Sent);
  }
  get published(): Sent["params"][] {
    return this.sent.filter((message) => message.method === "textDocument/publishDiagnostics").map((m) => m.params!);
  }
  open(uri: string, text: string): void {
    this.send("initialize", { rootUri: ROOT }, 1);
    this.send("textDocument/didOpen", { textDocument: { uri, text } });
  }
}

test("initialize announces full document sync", () => {
  const harness = new Harness();
  harness.send("initialize", { rootUri: ROOT }, 1);
  const result = harness.sent[0].result as { capabilities: { textDocumentSync: number } };
  assert.equal(result.capabilities.textDocumentSync, 1);
});

test("opening a dirty workflow publishes one diagnostic per finding", () => {
  const harness = new Harness();
  harness.open(CI, DIRTY);
  const published = harness.published;
  assert.equal(published.length, 1);
  assert.equal(published[0]!.uri, CI);
  assert.deepEqual(
    published[0]!.diagnostics.map((diagnostic) => diagnostic.code),
    ["push-excludes-tags", "no-all-builds-job", "yaml-comment-block"],
  );
});

test("every diagnostic is an error, because every finding fails the gate", () => {
  const harness = new Harness();
  harness.open(CI, DIRTY);
  for (const diagnostic of harness.published[0]!.diagnostics) assert.equal(diagnostic.severity, 1);
});

test("a clean workflow publishes an empty list rather than nothing", () => {
  const harness = new Harness();
  harness.open(CI, CLEAN);
  assert.deepEqual(harness.published[0]!.diagnostics, []);
});

test("fixing the file clears the diagnostic", () => {
  const harness = new Harness();
  harness.open(CI, DIRTY);
  harness.send("textDocument/didChange", { textDocument: { uri: CI }, contentChanges: [{ text: CLEAN }] });
  const published = harness.published;
  assert.equal(published.length, 2);
  assert.ok(published[0]!.diagnostics.length > 0);
  assert.deepEqual(published[1]!.diagnostics, []);
});

test("closing a document clears its diagnostics", () => {
  const harness = new Harness();
  harness.open(CI, DIRTY);
  harness.send("textDocument/didClose", { textDocument: { uri: CI } });
  assert.deepEqual(harness.published.at(-1)!.diagnostics, []);
});

test("a file none of the checks read publishes an empty list", () => {
  const harness = new Harness();
  harness.open("file:///repo/compose.yaml", "# a\n# b\n# c\nservices: {}\n");
  assert.deepEqual(harness.published[0]!.diagnostics, []);
});

// A reply carrying neither `result` nor `error` is malformed JSON-RPC, and the
// client's reader rejects it -- which fails shutdown after an otherwise clean
// session. Asserting on the RAW keys is the point: unmarshalling hides it,
// because a missing key and a null both read as the zero value.
test("shutdown replies with an explicit null result", () => {
  const harness = new Harness();
  harness.send("shutdown", undefined, 7);
  const body = JSON.parse(harness.raw[0].slice(harness.raw[0].indexOf("\r\n\r\n") + 4)) as Record<string, unknown>;
  assert.ok("result" in body);
  assert.equal(body.result, null);
});

test("an unknown request is answered and an unknown notification is not", () => {
  const harness = new Harness();
  harness.send("textDocument/hover", {}, 9);
  harness.send("$/setTrace", {});
  assert.equal(harness.sent.length, 1);
  assert.equal((harness.sent[0].error as { code: number }).code, -32601);
});

test("the pull path answers with the same findings the push path published", () => {
  const harness = new Harness();
  harness.open(CI, DIRTY);
  harness.send("textDocument/diagnostic", { textDocument: { uri: CI } }, 4);
  const pulled = harness.sent.at(-1)!.result as { kind: string; items: { code: string }[] };
  assert.equal(pulled.kind, "full");
  assert.deepEqual(
    pulled.items.map((item) => item.code),
    harness.published[0]!.diagnostics.map((diagnostic) => diagnostic.code),
  );
});

test("a truncated file says how many findings it did not send", () => {
  // Twelve semicolons: more findings than the per-file cap.
  const lines = Array.from({ length: 12 }, (_, index) => `Line ${index} reads; it then stops.`);
  const diagnostics = diagnosticsFor("docs/x.md", `${lines.join("\n\n")}\n`);
  assert.equal(diagnostics.length, 10);
  assert.match(diagnostics.at(-1)!.message, /\+2 more common-checks findings/);
});

test("a file inside the cap says nothing about a tail", () => {
  const diagnostics = diagnosticsFor("docs/x.md", "It reads the file; it stops.\n");
  assert.equal(diagnostics.length, 1);
  assert.doesNotMatch(diagnostics[0].message, /more common-checks findings/);
});

test("a path is taken against the root the client named", () => {
  assert.equal(relativize("file:///repo/.github/workflows/ci.yml", "file:///repo"), ".github/workflows/ci.yml");
  assert.equal(relativize("file:///repo/docs/a%20b.md", "file:///repo"), "docs/a b.md");
});

// Without a root, a workflow still has to be recognised as one: the checks that
// matter most are the ones scoped to .github/workflows.
test("a workflow is recognised with no root at all", () => {
  assert.equal(relativize("file:///anywhere/deep/.github/workflows/ci.yml", undefined), ".github/workflows/ci.yml");
  assert.equal(relativize("file:///anywhere/my-action/action.yml", undefined), "action.yml");
});

test("a path outside the named root still resolves rather than reporting nothing", () => {
  assert.equal(relativize("file:///elsewhere/notes.md", "file:///repo"), "notes.md");
});

test("framing splits two messages arriving in one chunk", () => {
  const framing = new Framing();
  const messages = framing.push(Buffer.concat([frame({ method: "a" }), frame({ method: "b" })]));
  assert.deepEqual(
    messages.map((message) => message.method),
    ["a", "b"],
  );
});

test("framing waits for a message split across chunks", () => {
  const framing = new Framing();
  const whole = frame({ method: "a" });
  assert.deepEqual(framing.push(whole.subarray(0, 20)), []);
  assert.deepEqual(
    framing.push(whole.subarray(20)).map((message) => message.method),
    ["a"],
  );
});

test("a frame that is not JSON costs one message, not the session", () => {
  const framing = new Framing();
  const garbage = Buffer.from("Content-Length: 3\r\n\r\nnot", "utf8");
  assert.deepEqual(framing.push(garbage), []);
  assert.deepEqual(
    framing.push(frame({ method: "a" })).map((message) => message.method),
    ["a"],
  );
});

test("a header with no content-length resynchronises", () => {
  const framing = new Framing();
  const messages = framing.push(Buffer.concat([Buffer.from("Nonsense: 1\r\n\r\n", "utf8"), frame({ method: "a" })]));
  assert.deepEqual(
    messages.map((message) => message.method),
    ["a"],
  );
});
