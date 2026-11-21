import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer, type Server } from "node:http";
import { copyFile, chmod, mkdir, mkdtemp, readdir, readlink, writeFile } from "node:fs/promises";
import { existsSync, lstatSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import type { AddressInfo } from "node:net";

const launcher = resolve("tools/plugin-launch/launch");
const latest = "839";
const onBranch = "900";

interface Host {
	base: string;
	heads: number;
	downloads: string[];
	close: () => Promise<void>;
}

async function listen(server: Server): Promise<number> {
	await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
	return (server.address() as AddressInfo).port;
}

// Serves every project the way dl.pazer.build does: a redirect naming the release, then its bytes.
async function fakeHost(): Promise<Host> {
	const host = { heads: 0, downloads: [] as string[] } as Host;
	const server = createServer((req, res) => {
		const url = new URL(req.url ?? "/", "http://x");
		if (url.pathname === "/file") {
			const v = url.searchParams.get("v");
			host.downloads.push(`${url.searchParams.get("project")}@${v}`);
			res.writeHead(200);
			res.end(`#!/bin/sh\necho "downloaded ${v} $*"\ncat\n`);
			return;
		}
		if (req.method === "HEAD") host.heads++;
		const branch = url.searchParams.get("branch");
		if (branch && branch !== "feature") {
			res.writeHead(404);
			res.end();
			return;
		}
		const v = branch ? onBranch : latest;
		const project = encodeURIComponent(url.pathname.slice(1));
		res.writeHead(302, { location: `http://127.0.0.1:${port}/file?project=${project}&v=${v}` });
		res.end();
	});
	const port = await listen(server);
	host.base = `http://127.0.0.1:${port}`;
	host.close = () => new Promise<void>((done) => server.close(() => done()));
	return host;
}

async function unreachableBase(): Promise<string> {
	const server = createServer();
	const port = await listen(server);
	await new Promise<void>((done) => server.close(() => done()));
	return `http://127.0.0.1:${port}`;
}

async function executable(path: string, body: string): Promise<void> {
	await writeFile(path, body);
	await chmod(path, 0o755);
}

const withVersion = (v: string) =>
	`#!/bin/sh\nif [ "$1" = version ]; then echo "slopfix ${v} 0a1b2c3"; exit 0; fi\necho "system $*"\ncat\n`;
const withoutVersion =
	'#!/bin/sh\nif [ "$1" = version ]; then echo \'Error: unknown command "version"\' >&2; exit 1; fi\necho "system $*"\ncat\n';

interface Plugin {
	launch: string;
	data: string;
	path: string;
}

// A plugin tree holding the launcher, an optional system command of the project's name, and an optional branch record.
async function plugin(opts: { system?: string; name?: string; branch?: string } = {}): Promise<Plugin> {
	const root = await mkdtemp(join(tmpdir(), "plugin-launch-"));
	const bin = join(root, "bin");
	const data = join(root, "data");
	await mkdir(join(root, "plugin", "scripts"), { recursive: true });
	await mkdir(bin);
	const launch = join(root, "plugin", "scripts", "launch");
	await copyFile(launcher, launch);
	await chmod(launch, 0o755);
	if (opts.system !== undefined) await executable(join(bin, opts.name ?? "slopfix"), opts.system);
	if (opts.branch !== undefined) await writeFile(join(root, "plugin", "mh.plugin.json"), JSON.stringify({ branch: opts.branch }, null, "\t"));
	return { launch, data, path: `${bin}:/usr/bin:/bin` };
}

interface Run {
	code: number | null;
	stdout: string;
	stderr: string;
}

function run(p: Plugin, base: string, args: string[], stdin: string): Promise<Run> {
	return new Promise((done, fail) => {
		const child = spawn(p.launch, args, {
			env: { PATH: p.path, HOME: p.data, CLAUDE_PLUGIN_DATA: p.data, CC_PLUGIN_LAUNCH_BASE: base },
		});
		let stdout = "";
		let stderr = "";
		child.stdout.on("data", (d) => (stdout += d));
		child.stderr.on("data", (d) => (stderr += d));
		child.on("error", fail);
		child.on("close", (code) => done({ code, stdout, stderr }));
		child.stdin.end(stdin);
	});
}

test("a system command at or above the published release runs, with the arguments and stdin, and nothing downloads", async () => {
	const host = await fakeHost();
	try {
		const p = await plugin({ system: withVersion("900") });
		const r = await run(p, host.base, ["slopfix", "hook", "--max-comment-lines", "14"], "payload\n");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, "system hook --max-comment-lines 14\npayload\n");
		assert.deepEqual(host.downloads, []);
	} finally {
		await host.close();
	}
});

test("a system command that prints no release number counts as older, so the published release downloads and runs", async () => {
	const host = await fakeHost();
	try {
		const p = await plugin({ system: withoutVersion });
		const r = await run(p, host.base, ["slopfix", "hook"], "payload\n");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, `downloaded ${latest} hook\npayload\n`);
		assert.deepEqual(host.downloads, [`slopfix@${latest}`]);
		assert.ok(existsSync(join(p.data, `slopfix.${latest}`)));
	} finally {
		await host.close();
	}
});

test("a system command below the published release is passed over for the download", async () => {
	const host = await fakeHost();
	try {
		const p = await plugin({ system: withVersion("700") });
		const r = await run(p, host.base, ["slopfix", "lsp"], "");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, `downloaded ${latest} lsp\n`);
	} finally {
		await host.close();
	}
});

test("a nested project downloads under its last path element", async () => {
	const host = await fakeHost();
	try {
		const p = await plugin();
		const r = await run(p, host.base, ["cc-marketplace/glob"], "");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, `downloaded ${latest} \n`);
		assert.deepEqual(host.downloads, [`cc-marketplace/glob@${latest}`]);
		assert.ok(existsSync(join(p.data, `glob.${latest}`)));
	} finally {
		await host.close();
	}
});

test("a released plugin runs the build from its own branch", async () => {
	const host = await fakeHost();
	try {
		const p = await plugin({ branch: "feature" });
		const r = await run(p, host.base, ["cc-marketplace/glob", "x"], "");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, `downloaded ${onBranch} x\n`);
	} finally {
		await host.close();
	}
});

test("a branch with no build of the project falls back to the default branch's build and says so", async () => {
	const host = await fakeHost();
	try {
		const p = await plugin({ branch: "never-built" });
		const r = await run(p, host.base, ["cc-marketplace/glob", "x"], "");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, `downloaded ${latest} x\n`);
		assert.match(r.stderr, /no build from branch never-built/);
	} finally {
		await host.close();
	}
});

test("the published release downloads once, older downloads go, and the choice is reused without asking again", async () => {
	const host = await fakeHost();
	try {
		const p = await plugin();
		await mkdir(p.data, { recursive: true });
		await executable(join(p.data, "slopfix.700"), "#!/bin/sh\necho stale\n");

		const first = await run(p, host.base, ["slopfix", "hook"], "one\n");
		assert.equal(first.code, 0, first.stderr);
		assert.equal(first.stdout, `downloaded ${latest} hook\none\n`);
		assert.deepEqual((await readdir(p.data)).filter((f) => /^slopfix\.\d+$/.test(f)), [`slopfix.${latest}`]);

		const heads = host.heads;
		const second = await run(p, host.base, ["slopfix", "hook"], "two\n");
		assert.equal(second.code, 0, second.stderr);
		assert.equal(second.stdout, `downloaded ${latest} hook\ntwo\n`);
		assert.equal(host.heads, heads, "a fresh choice must not ask the server again");
		assert.equal(host.downloads.length, 1);
	} finally {
		await host.close();
	}
});

test("with the server unreachable, the newest download already present runs and stderr names it", async () => {
	const p = await plugin({ system: withoutVersion });
	await mkdir(p.data, { recursive: true });
	await executable(join(p.data, "slopfix.700"), '#!/bin/sh\necho "cached $*"\n');
	const r = await run(p, await unreachableBase(), ["slopfix", "hook"], "");
	assert.equal(r.code, 0, r.stderr);
	assert.equal(r.stdout, "cached hook\n");
	assert.match(r.stderr, /slopfix\.700/);
});

test("with the server unreachable and nothing present, the launcher fails, writes nothing to stdout, and names the URL", async () => {
	const p = await plugin();
	const base = await unreachableBase();
	const r = await run(p, base, ["slopfix", "hook"], "");
	assert.notEqual(r.code, 0);
	assert.equal(r.stdout, "");
	assert.ok(r.stderr.includes(base), r.stderr);
});

test("every plugin manifest that runs scripts/launch has it as a link to the one launcher", async () => {
	const pluginsDir = resolve("plugins");
	const target = "../../../tools/plugin-launch/launch";
	for (const name of await readdir(pluginsDir)) {
		const manifests = [".claude-plugin/plugin.json", ".mcp.json", ".lsp.json"]
			.map((m) => join(pluginsDir, name, m))
			.filter((m) => existsSync(m));
		const uses = manifests.some((m) => readFileSync(m, "utf8").includes("${CLAUDE_PLUGIN_ROOT}/scripts/launch"));
		if (!uses) continue;
		const link = join(pluginsDir, name, "scripts", "launch");
		assert.ok(lstatSync(link).isSymbolicLink(), `${link} must link to tools/plugin-launch/launch, not copy it`);
		assert.equal(await readlink(link), target, `${link} points elsewhere`);
	}
});
