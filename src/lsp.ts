// A stdio language server whose only job is to say, while a file is open, what
// common-checks would say about it in CI.

import { findings, fileKind, type Finding } from "./checks.ts";

export const SOURCE = "common-checks";

// The client injects only the first handful of diagnostics per file, so a file
// with more findings than that says so on the last one it does send. Silently
// dropping the tail would read as "that is all of it".
export const MAX_PER_FILE = Number(process.env.COMMON_CHECKS_LSP_MAX_PER_FILE) || 10;

export interface Diagnostic {
  range: { start: { line: number; character: number }; end: { line: number; character: number } };
  severity: number;
  source: string;
  code: string;
  message: string;
}

/**
 * Every diagnostic is severity 1 (Error): a finding here is a finding that
 * fails the merge gate. ste-lint's heuristic buckets never reach this point --
 * they do not fail CI, and spending the client's budget on them would push out
 * findings that do.
 */
export function toDiagnostic(finding: Finding, lines: string[]): Diagnostic {
  const start = Math.max(0, finding.startLine - 1);
  const end = Math.max(start, finding.endLine - 1);
  const width = lines[end]?.length ?? 0;
  return {
    range: { start: { line: start, character: 0 }, end: { line: end, character: width } },
    severity: 1,
    source: SOURCE,
    code: finding.check,
    message: finding.message,
  };
}

export function diagnosticsFor(relativePath: string, content: string): Diagnostic[] {
  const all = findings(relativePath, content);
  const lines = content.split(/\r?\n/);
  const shown = all.slice(0, MAX_PER_FILE).map((finding) => toDiagnostic(finding, lines));
  const hidden = all.length - shown.length;
  if (hidden > 0 && shown.length > 0) {
    shown[shown.length - 1].message += ` (+${hidden} more common-checks findings in this file)`;
  }
  return shown;
}

/** Decodes a `file://` URI to a filesystem path. */
export function pathOf(uri: string): string {
  if (!uri.startsWith("file://")) return uri;
  try {
    return decodeURIComponent(uri.slice("file://".length));
  } catch {
    return uri.slice("file://".length);
  }
}

/**
 * The path the checks reason about, which is always repository-relative. When
 * the client named a root, the path is taken against it. Otherwise the
 * `.github/workflows/` segment locates it, and everything else falls back to a
 * basename -- enough for an `action.yml` or a `.md`, which are recognised by
 * their name alone.
 */
export function relativize(uri: string, root: string | undefined): string {
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

interface Message {
  id?: number | string | null;
  method?: string;
  params?: Record<string, unknown>;
}

type Write = (chunk: string) => void;

export class Server {
  #root: string | undefined;
  #open = new Map<string, string>();

  constructor(private readonly write: Write) {}

  /** Frames one JSON-RPC message the way the base protocol requires. */
  #send(payload: Record<string, unknown>): void {
    const body = JSON.stringify(payload);
    this.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n\r\n${body}`);
  }

  // Assembled by hand so a null result marshals as an explicit `null`. A reply
  // carrying neither `result` nor `error` is malformed JSON-RPC, and the
  // client's reader rejects it -- which is how a clean session still fails at
  // shutdown.
  #respond(id: number | string, result: unknown): void {
    this.#send({ jsonrpc: "2.0", id, result: result === undefined ? null : result });
  }

  #publish(uri: string): void {
    const content = this.#open.get(uri);
    if (content === undefined) return;
    const relative = relativize(uri, this.#root);
    const diagnostics = fileKind(relative) === "other" ? [] : diagnosticsFor(relative, content);
    this.#send({
      jsonrpc: "2.0",
      method: "textDocument/publishDiagnostics",
      params: { uri, diagnostics },
    });
  }

  handle(message: Message): void {
    const { id, method, params } = message;
    switch (method) {
      case "initialize": {
        const folders = params?.workspaceFolders as { uri?: string }[] | undefined;
        this.#root = (params?.rootUri as string | undefined) ?? folders?.[0]?.uri;
        this.#respond(id as number, {
          // Full sync: a finding is a property of the whole document, and an
          // incremental edit would need the server to rebuild it anyway.
          capabilities: { textDocumentSync: 1, diagnosticProvider: { interFileDependencies: false, workspaceDiagnostics: false } },
          serverInfo: { name: SOURCE, version: "1" },
        });
        return;
      }
      case "initialized":
        return;
      case "shutdown":
        this.#respond(id as number, null);
        return;
      case "exit":
        return;
      case "textDocument/didOpen": {
        const doc = params?.textDocument as { uri?: string; text?: string } | undefined;
        if (!doc?.uri) return;
        this.#open.set(doc.uri, doc.text ?? "");
        this.#publish(doc.uri);
        return;
      }
      case "textDocument/didChange": {
        const doc = params?.textDocument as { uri?: string } | undefined;
        const changes = params?.contentChanges as { text?: string }[] | undefined;
        const text = changes?.[changes.length - 1]?.text;
        if (!doc?.uri || text === undefined) return;
        this.#open.set(doc.uri, text);
        this.#publish(doc.uri);
        return;
      }
      case "textDocument/didSave": {
        const doc = params?.textDocument as { uri?: string } | undefined;
        if (doc?.uri) this.#publish(doc.uri);
        return;
      }
      case "textDocument/didClose": {
        const doc = params?.textDocument as { uri?: string } | undefined;
        if (!doc?.uri) return;
        this.#open.delete(doc.uri);
        // Clearing is what removes the findings from a file nobody is editing.
        this.#send({ jsonrpc: "2.0", method: "textDocument/publishDiagnostics", params: { uri: doc.uri, diagnostics: [] } });
        return;
      }
      // Claude Code never calls this -- it registers a publishDiagnostics
      // handler and nothing else. It is here so the server is correct for an
      // editor that pulls.
      case "textDocument/diagnostic": {
        const doc = params?.textDocument as { uri?: string } | undefined;
        const content = doc?.uri ? this.#open.get(doc.uri) : undefined;
        const relative = doc?.uri ? relativize(doc.uri, this.#root) : "";
        const items =
          content === undefined || fileKind(relative) === "other" ? [] : diagnosticsFor(relative, content);
        this.#respond(id as number, { kind: "full", items });
        return;
      }
      default:
        // A request must always be answered; a notification must not be.
        if (id !== undefined && id !== null) {
          this.#send({ jsonrpc: "2.0", id, error: { code: -32601, message: `unknown method: ${method}` } });
        }
    }
  }
}

/** Splits the base protocol's header-framed stream into messages. */
export class Framing {
  #buffer = Buffer.alloc(0);

  push(chunk: Buffer): Message[] {
    this.#buffer = Buffer.concat([this.#buffer, chunk]);
    const out: Message[] = [];
    for (;;) {
      const split = this.#buffer.indexOf("\r\n\r\n");
      if (split === -1) return out;
      const header = this.#buffer.subarray(0, split).toString("ascii");
      const length = /content-length:\s*(\d+)/i.exec(header);
      if (!length) {
        // A frame with no length cannot be skipped safely, so drop the header
        // and resynchronise rather than blocking on a stream that will never
        // produce one.
        this.#buffer = this.#buffer.subarray(split + 4);
        continue;
      }
      const size = Number(length[1]);
      const start = split + 4;
      if (this.#buffer.length < start + size) return out;
      const body = this.#buffer.subarray(start, start + size).toString("utf8");
      this.#buffer = this.#buffer.subarray(start + size);
      try {
        out.push(JSON.parse(body) as Message);
      } catch {
        // A frame that is not JSON is one message lost, never the session.
      }
    }
  }
}

export function serve(input: NodeJS.ReadableStream, write: Write): void {
  const framing = new Framing();
  const server = new Server(write);
  input.on("data", (chunk: Buffer) => {
    for (const message of framing.push(chunk)) {
      try {
        server.handle(message);
      } catch (error) {
        // One bad document must not take the session down with it: the editor
        // would lose every later finding and say nothing about why.
        process.stderr.write(`common-checks: ${error instanceof Error ? error.stack : String(error)}\n`);
      }
    }
  });
}
