import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer, type Server } from "node:http";
import { chmod, mkdir, mkdtemp, readdir, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import type { AddressInfo } from "node:net";

const launcher = resolve("plugins/slopfix/scripts/slopfix");
const published = "839";

const downloadedBody = '#!/bin/sh\necho "downloaded $*"\ncat\n';

interface Fake {
	url: string;
	heads: number;
	downloads: number;
	close: () => Promise<void>;
}

async function listen(server: Server): Promise<number> {
	await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
	return (server.address() as AddressInfo).port;
}

async function buildhost(): Promise<Fake> {
	const fake = { heads: 0, downloads: 0 } as Fake;
	const server = createServer((req, res) => {
		if (req.url?.startsWith("/slopfix")) {
			if (req.method === "HEAD") fake.heads++;
			res.writeHead(302, { location: `http://127.0.0.1:${port}/file?project=slopfix&v=${published}` });
			res.end();
			return;
		}
		if (req.url?.startsWith("/file") && req.url.includes(`v=${published}`)) {
			fake.downloads++;
			res.writeHead(200, { "content-type": "application/octet-stream" });
			res.end(downloadedBody);
			return;
		}
		res.writeHead(404);
		res.end();
	});
	const port = await listen(server);
	fake.url = `http://127.0.0.1:${port}/slopfix?os=linux&arch=amd64`;
	fake.close = () => new Promise<void>((done) => server.close(() => done()));
	return fake;
}

async function unreachableUrl(): Promise<string> {
	const server = createServer();
	const port = await listen(server);
	await new Promise<void>((done) => server.close(() => done()));
	return `http://127.0.0.1:${port}/slopfix?os=linux&arch=amd64`;
}

async function executable(path: string, body: string): Promise<void> {
	await writeFile(path, body);
	await chmod(path, 0o755);
}

const systemWithVersion = (v: string) =>
	`#!/bin/sh\nif [ "$1" = version ]; then echo "slopfix ${v} 0a1b2c3"; exit 0; fi\necho "system $*"\ncat\n`;
const systemWithoutVersion =
	'#!/bin/sh\nif [ "$1" = version ]; then echo \'Error: unknown command "version" for "slopfix"\' >&2; exit 1; fi\necho "system $*"\ncat\n';

interface Setup {
	data: string;
	path: string;
}

async function setup(system?: string): Promise<Setup> {
	const root = await mkdtemp(join(tmpdir(), "slopfix-launcher-"));
	const fakeBin = join(root, "bin");
	const data = join(root, "data");
	await mkdir(fakeBin);
	if (system !== undefined) await executable(join(fakeBin, "slopfix"), system);
	return { data, path: `${fakeBin}:/usr/bin:/bin` };
}

interface Run {
	code: number | null;
	stdout: string;
	stderr: string;
}

function run(s: Setup, url: string, args: string[], stdin: string): Promise<Run> {
	return new Promise((done, fail) => {
		const child = spawn(launcher, args, {
			env: { PATH: s.path, HOME: s.data, CLAUDE_PLUGIN_DATA: s.data, SLOPFIX_URL: url },
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

test("a system slopfix at or above the published release runs, and nothing downloads", async () => {
	const host = await buildhost();
	try {
		const s = await setup(systemWithVersion("900"));
		const r = await run(s, host.url, ["hook", "--max-comment-lines", "14"], "payload\n");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, "system hook --max-comment-lines 14\npayload\n");
		assert.equal(host.downloads, 0);
	} finally {
		await host.close();
	}
});

test("a system slopfix that prints no release number counts as older, so the published build downloads and runs", async () => {
	const host = await buildhost();
	try {
		const s = await setup(systemWithoutVersion);
		const r = await run(s, host.url, ["hook"], "payload\n");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, "downloaded hook\npayload\n");
		assert.equal(host.downloads, 1);
		assert.ok(existsSync(join(s.data, `slopfix.${published}`)));
	} finally {
		await host.close();
	}
});

test("a system slopfix below the published release is passed over for the download", async () => {
	const host = await buildhost();
	try {
		const s = await setup(systemWithVersion("700"));
		const r = await run(s, host.url, ["lsp"], "");
		assert.equal(r.code, 0, r.stderr);
		assert.equal(r.stdout, "downloaded lsp\n");
	} finally {
		await host.close();
	}
});

test("with no system slopfix the published build downloads once, older downloads go, and the choice is reused", async () => {
	const host = await buildhost();
	try {
		const s = await setup();
		await mkdir(s.data, { recursive: true });
		await executable(join(s.data, "slopfix.700"), "#!/bin/sh\necho stale\n");

		const first = await run(s, host.url, ["hook"], "one\n");
		assert.equal(first.code, 0, first.stderr);
		assert.equal(first.stdout, "downloaded hook\none\n");
		assert.deepEqual((await readdir(s.data)).filter((f) => f.startsWith("slopfix.")), [`slopfix.${published}`]);

		const headsAfterFirst = host.heads;
		const second = await run(s, host.url, ["hook"], "two\n");
		assert.equal(second.code, 0, second.stderr);
		assert.equal(second.stdout, "downloaded hook\ntwo\n");
		assert.equal(host.heads, headsAfterFirst, "a fresh choice must not ask buildhost again");
		assert.equal(host.downloads, 1);
	} finally {
		await host.close();
	}
});

test("when buildhost is unreachable the newest download already present runs, and stderr says so", async () => {
	const s = await setup(systemWithoutVersion);
	await mkdir(s.data, { recursive: true });
	await executable(join(s.data, "slopfix.700"), '#!/bin/sh\necho "cached $*"\n');
	const r = await run(s, await unreachableUrl(), ["hook"], "");
	assert.equal(r.code, 0, r.stderr);
	assert.equal(r.stdout, "cached hook\n");
	assert.match(r.stderr, /slopfix\.700/);
});

test("when buildhost is unreachable and no slopfix exists anywhere, the launcher fails and names the URL", async () => {
	const s = await setup();
	const url = await unreachableUrl();
	const r = await run(s, url, ["hook"], "");
	assert.notEqual(r.code, 0);
	assert.equal(r.stdout, "");
	assert.ok(r.stderr.includes(url), r.stderr);
});
