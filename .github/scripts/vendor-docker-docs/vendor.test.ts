import assert from "node:assert/strict";
import { mkdtemp, mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import type { Client } from "./github.ts";
import { vendor } from "./vendor.ts";
import {
  absolutizeLinks,
  bundles,
  buildkit,
  NOTICE_FILE,
  notice,
  render,
  resolveIncludes,
  stripFrontmatter,
  stripShortcodes,
  type Bundle,
  type Page,
  type Upstream,
} from "./transform.ts";

const COMMIT = "0123456789abcdef0123456789abcdef01234567";

const composePage = (name: string): Page => ({
  src: bundles[1].pages[0].src,
  path: `content/reference/compose-file/${name}.md`,
  out: `${name}.md`,
});

test("stripFrontmatter takes the title and drops the block", () => {
  const { title, body } = stripFrontmatter(
    "---\ntitle: Services top-level element\nweight: 20\n---\n\nA service is...\n",
  );
  assert.equal(title, "Services top-level element");
  assert.equal(body, "\nA service is...\n");
});

test("stripFrontmatter leaves a file that has none", () => {
  const source = "# Dockerfile reference\n\nText.\n";
  assert.deepEqual(stripFrontmatter(source), { title: "", body: source });
});

// A horizontal rule at the top of a body is not a frontmatter fence. Treating
// it as one would swallow everything up to the next rule.
test("stripFrontmatter leaves an unterminated block alone", () => {
  const source = "---\nnot really frontmatter\n";
  assert.deepEqual(stripFrontmatter(source), { title: "", body: source });
});

test("resolveIncludes inlines the partial", async () => {
  const seen: string[] = [];
  const fetchPath = async (path: string) => {
    seen.push(path);
    return "---\ntitle: ignored\n---\nA service is an abstract definition.\n";
  };

  const out = await resolveIncludes(
    'Intro.\n\n{{% include "compose/services.md" %}}\n\nOutro.\n',
    fetchPath,
  );

  assert.deepEqual(seen, ["content/includes/compose/services.md"]);
  assert.equal(out, "Intro.\n\nA service is an abstract definition.\n\nOutro.\n");
});

// The partial's own title is dropped: it is spliced into a page that already
// has an H1, so keeping it would produce two.
test("resolveIncludes drops the partial's frontmatter", async () => {
  const out = await resolveIncludes('{{% include "compose/x.md" %}}\n', async () =>
    "---\ntitle: Partial\n---\nBody.\n",
  );
  assert.ok(!out.includes("Partial"));
  assert.ok(out.includes("Body."));
});

test("resolveIncludes fails on a cycle", async () => {
  await assert.rejects(
    resolveIncludes('{{% include "compose/loop.md" %}}', async () =>
      '{{% include "compose/loop.md" %}}',
    ),
    /cycle/,
  );
});

test("resolveIncludes names the partial it could not fetch", async () => {
  await assert.rejects(
    resolveIncludes('{{% include "compose/gone.md" %}}', async () => {
      throw new Error("404");
    }),
    /compose\/gone\.md/,
  );
});

// The badge carries the fact that the option is version-gated. Dropping it
// silently would delete the only signal that a field is not universally
// available.
test("stripShortcodes keeps the version-gate signal", () => {
  const out = stripShortcodes(
    '### gpus\n\n{{< summary-bar feature_name="Compose gpus" >}}\n\nText.\n',
  );
  assert.ok(out.includes('Version-gated feature: "Compose gpus"'));
  assert.ok(!out.includes("{{<"));
});

// An upstream shortcode nobody taught this script about must stop the run,
// rather than leaking Hugo syntax into the reference or vanishing untraced.
test("stripShortcodes rejects an unknown shortcode", () => {
  assert.throws(() => stripShortcodes("{{< grid >}}\n"), /grid/);
});

test("absolutizeLinks rewrites only root-relative ones", () => {
  const out = absolutizeLinks(
    "See [cli](/reference/cli/docker/), [anchor](#build), [abs](https://example.com/x), and [sibling](services.md).",
  );
  assert.ok(out.includes("(https://docs.docker.com/reference/cli/docker/)"));
  assert.ok(out.includes("(#build)"));
  assert.ok(out.includes("(https://example.com/x)"));
  assert.ok(out.includes("(services.md)"));
});

test("render produces attributed output", async () => {
  const raw =
    '---\ntitle: Services\n---\n\n{{% include "compose/services.md" %}}\n\n' +
    '{{< summary-bar feature_name="Compose gpus" >}}\n\nSee [cli](/reference/cli/).\n';

  const out = await render(raw, composePage("services"), COMMIT, async () =>
    "A service is an abstract definition.\n",
  );

  assert.ok(out.startsWith("# Services\n"), "title becomes the H1");
  assert.ok(out.includes("Do not edit by hand"));
  assert.ok(out.includes(COMMIT));
  assert.ok(out.includes("Licensed Apache-2.0"));
  assert.ok(out.includes("A service is an abstract definition."));
  assert.ok(out.includes("https://docs.docker.com/reference/cli/"));
  assert.ok(!out.includes("{{"));
});

test("render fails rather than emit Hugo syntax", async () => {
  await assert.rejects(
    render("---\ntitle: X\n---\n{{< grid >}}\n", composePage("x"), COMMIT, async () => ""),
  );
});

// Every destination must be unique, or one page silently overwrites another.
test("the bundle plan has no duplicate outputs", () => {
  for (const bundle of bundles) {
    const seen = new Set<string>();
    for (const page of bundle.pages) {
      assert.ok(!seen.has(page.out), `${bundle.skill}: ${page.out} written twice`);
      assert.notEqual(page.out, NOTICE_FILE, "a page may not claim the notice filename");
      seen.add(page.out);
    }
  }
});

test("notice names every source and its license", () => {
  const bundle: Bundle = {
    skill: "demo",
    pages: [composePage("services"), { src: buildkit, path: "frontend/dockerfile/docs/reference.md", out: "dockerfile.md" }],
  };

  const out = notice(bundle, new Map([["docker/docs", "aaa"], ["moby/buildkit", "bbb"]]));

  assert.ok(out.includes("`docker/docs` at commit `aaa`, licensed Apache-2.0."));
  assert.ok(out.includes("`moby/buildkit` at commit `bbb`, licensed Apache-2.0."));
  // Sorted, so a regenerate that changed nothing produces no diff.
  assert.ok(out.indexOf("docker/docs") < out.indexOf("moby/buildkit"));
});

/** Answers every read from a table, so the pipeline runs with no network. */
class FakeClient implements Client {
  constructor(
    private files: Map<string, string>,
    private failResolve = false,
  ) {}

  async resolve(_src: Upstream): Promise<string> {
    if (this.failResolve) throw new Error("no such ref");
    return COMMIT;
  }

  async get(repo: string, commit: string, path: string): Promise<string> {
    const body = this.files.get(path);
    // Every page in the real plan resolves; an unlisted path is a page the
    // fixture forgot, so say which one rather than returning empty.
    if (body === undefined) throw new Error(`fake client has no ${path} (${repo}@${commit})`);
    return body;
  }
}

function everyPlannedPage(): Map<string, string> {
  const files = new Map<string, string>();
  for (const bundle of bundles) {
    for (const page of bundle.pages) {
      files.set(page.path, `---\ntitle: ${page.out}\n---\n\nBody of ${page.out}.\n`);
    }
  }
  return files;
}

const scratch = () => mkdtemp(join(tmpdir(), "vendor-docker-docs-"));

test("vendor writes every planned page", async () => {
  const root = await scratch();

  await vendor(root, new FakeClient(everyPlannedPage()));

  for (const bundle of bundles) {
    const dir = join(root, "plugins", "docs", "skills", bundle.skill, "reference");
    for (const page of bundle.pages) {
      const body = await readFile(join(dir, page.out), "utf8");
      assert.ok(body.includes(`Body of ${page.out}`), `${bundle.skill}/${page.out}`);
      assert.ok(body.includes(COMMIT), "provenance names the commit");
    }
    assert.ok((await readFile(join(dir, NOTICE_FILE), "utf8")).includes(COMMIT));
  }
});

test("vendor reports a failed ref resolution", async () => {
  await assert.rejects(vendor(await scratch(), new FakeClient(new Map(), true)), /no such ref/);
});

test("vendor reports a page it cannot fetch", async () => {
  await assert.rejects(vendor(await scratch(), new FakeClient(new Map())), /fake client has no/);
});

// A page dropped from the plan must not leave a file behind: nothing would
// regenerate it, and it would still read as current reference.
test("vendor removes a file the plan no longer produces", async () => {
  const root = await scratch();
  const dir = join(root, "plugins", "docs", "skills", bundles[0].skill, "reference");
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, "retired-page.md"), "old");

  await vendor(root, new FakeClient(everyPlannedPage()));

  assert.ok(!(await readdir(dir)).includes("retired-page.md"));
});

test("vendor keeps the notice and the planned pages", async () => {
  const root = await scratch();

  await vendor(root, new FakeClient(everyPlannedPage()));

  const dir = join(root, "plugins", "docs", "skills", bundles[0].skill, "reference");
  const entries = await readdir(dir);
  assert.ok(entries.includes(NOTICE_FILE));
  for (const page of bundles[0].pages) assert.ok(entries.includes(page.out));
});
