// The pure half of the vendoring: everything that turns one upstream Hugo page
// into the file written beside a skill. No network and no filesystem, so it is
// testable without either.

export interface Upstream {
  repo: string;
  ref: string;
  /** Blob URL for a human to open, given a commit and a path. */
  blob: (commit: string, path: string) => string;
  license: string;
}

export const dockerDocs: Upstream = {
  repo: "docker/docs",
  ref: "main",
  blob: (commit, path) => `https://github.com/docker/docs/blob/${commit}/${path}`,
  license: "Apache-2.0",
};

export const buildkit: Upstream = {
  repo: "moby/buildkit",
  ref: "master",
  blob: (commit, path) => `https://github.com/moby/buildkit/blob/${commit}/${path}`,
  license: "Apache-2.0",
};

export interface Page {
  src: Upstream;
  path: string;
  out: string;
}

export interface Bundle {
  skill: string;
  pages: Page[];
}

const composePage = (name: string): Page => ({
  src: dockerDocs,
  path: `content/reference/compose-file/${name}.md`,
  out: `${name}.md`,
});

/**
 * The whole vendoring plan. Adding a reference file is one entry here.
 *
 * The Compose landing page (_index.md) is deliberately absent: it is a
 * navigation grid with no specification text in it.
 */
export const bundles: Bundle[] = [
  {
    skill: "dockerfile",
    pages: [
      { src: buildkit, path: "frontend/dockerfile/docs/reference.md", out: "dockerfile.md" },
    ],
  },
  {
    skill: "docker-compose",
    pages: [
      "services",
      "build",
      "deploy",
      "develop",
      "networks",
      "volumes",
      "configs",
      "secrets",
      "models",
      "profiles",
      "include",
      "merge",
      "interpolation",
      "fragments",
      "extension",
      "version-and-name",
    ].map(composePage),
  },
];

/** Where a Hugo `include` resolves from. Only docker/docs uses them. */
export const INCLUDE_ROOT = "content/includes/";

export const NOTICE_FILE = "NOTICE.md";

const DOCS_DOCKER_URL = "https://docs.docker.com";

/**
 * Removes a leading YAML frontmatter block, returning the body and the block's
 * `title`. Hugo renders the title from frontmatter, so a page whose block is
 * dropped otherwise arrives with no heading at all.
 */
export function stripFrontmatter(source: string): { title: string; body: string } {
  const FENCE = "---\n";
  if (!source.startsWith(FENCE)) return { title: "", body: source };

  const rest = source.slice(FENCE.length);
  const end = rest.indexOf(`\n${FENCE}`);
  // An unterminated block is a horizontal rule, not frontmatter. Treating it as
  // frontmatter would swallow everything up to the next rule.
  if (end < 0) return { title: "", body: source };

  const block = rest.slice(0, end);
  const body = rest.slice(end + 1 + FENCE.length);

  let title = "";
  for (const line of block.split("\n")) {
    const at = line.indexOf(":");
    if (at < 0 || line.slice(0, at).trim() !== "title") continue;
    title = line.slice(at + 1).trim().replace(/^["']|["']$/g, "");
    break;
  }
  return { title, body };
}

const INCLUDE = /\{\{%\s*include\s+"([^"]+)"\s*%\}\}/g;

/**
 * Inlines every Hugo include. The partial's own frontmatter is dropped: it is
 * spliced into a page that already has an H1, so keeping its title makes two.
 *
 * A partial may include another, so this recurses. Depth is bounded because a
 * cycle would otherwise hang.
 */
export async function resolveIncludes(
  body: string,
  fetchPath: (path: string) => Promise<string>,
  depth = 0,
): Promise<string> {
  if (depth > 8) throw new Error("include nesting deeper than 8 levels; likely a cycle");

  const names = [...body.matchAll(INCLUDE)].map((m) => m[1]);
  if (names.length === 0) return body;

  const resolved = new Map<string, string>();
  for (const name of names) {
    if (resolved.has(name)) continue;
    let raw: string;
    try {
      raw = await fetchPath(INCLUDE_ROOT + name);
    } catch (cause) {
      throw new Error(`include "${name}": ${(cause as Error).message}`);
    }
    const { body: partial } = stripFrontmatter(raw);
    resolved.set(name, (await resolveIncludes(partial, fetchPath, depth + 1)).replace(/\n+$/, ""));
  }

  return body.replace(INCLUDE, (_match, name: string) => resolved.get(name)!);
}

const SUMMARY_BAR = /\{\{<\s*summary-bar\s+feature_name="([^"]*)"\s*>\}\}/g;
const ANY_SHORTCODE = /\{\{[<%][^}]*[>%]\}\}/;

/**
 * Removes the Hugo shortcodes that survive include resolution.
 *
 * A summary-bar renders a badge saying which product version first shipped the
 * feature. The version itself lives in a Hugo data file this script does not
 * read, so the badge becomes a line naming the feature: dropping it silently
 * would delete the only signal that the option is version-gated at all.
 *
 * Any OTHER shortcode throws. Upstream adding one is exactly the change that
 * must not pass through unnoticed, either as literal Hugo syntax in the output
 * or as a silent deletion.
 */
export function stripShortcodes(body: string): string {
  const out = body.replace(
    SUMMARY_BAR,
    (_m, feature: string) =>
      `> Version-gated feature: "${feature}". Check upstream for the minimum version.`,
  );

  const leftover = ANY_SHORTCODE.exec(out);
  if (leftover) {
    throw new Error(`unhandled Hugo shortcode ${JSON.stringify(leftover[0])}: teach stripShortcodes about it`);
  }
  return out;
}

/**
 * Rewrites Hugo's root-relative links to absolute docs.docker.com URLs. Vendored
 * out of the site, `](/reference/cli/docker/)` resolves against whatever host
 * reads the file, which is never the right one.
 *
 * An anchor, an absolute URL and a same-directory relative link are all left
 * alone: the first two already work, and the third points at a sibling file
 * vendored next to it.
 */
export function absolutizeLinks(body: string): string {
  return body.replace(/\]\((\/[^)]*)\)/g, (_m, path: string) => `](${DOCS_DOCKER_URL}${path})`);
}

/**
 * The provenance block prepended to every vendored file. It names the exact
 * commit read, so the copy can be diffed against upstream, and carries the
 * license the content is used under.
 */
export function header(title: string, page: Page, commit: string): string {
  const lines = [];
  if (title) lines.push(`# ${title}`, "");
  lines.push(
    "<!-- Vendored file. Do not edit by hand. -->",
    `> Vendored verbatim from [\`${page.src.repo}/${page.path}\`](${page.src.blob(commit, page.path)}) at commit \`${commit}\`.`,
    `> Licensed ${page.src.license}. Regenerate with \`npx tsx .github/scripts/vendor-docker-docs/main.ts\`.`,
    "",
    "",
  );
  return lines.join("\n");
}

/** Turns one fetched page into the file written to disk. */
export async function render(
  raw: string,
  page: Page,
  commit: string,
  fetchPath: (path: string) => Promise<string>,
): Promise<string> {
  const { title, body } = stripFrontmatter(raw);
  const inlined = await resolveIncludes(body, fetchPath);
  const plain = stripShortcodes(inlined);
  return header(title, page, commit) + absolutizeLinks(plain).replace(/^\n+/, "");
}

/**
 * The provenance file beside a skill's vendored reference, naming every upstream
 * commit the directory was built from.
 */
export function notice(bundle: Bundle, commits: Map<string, string>): string {
  const sources = new Map<string, Upstream>();
  for (const page of bundle.pages) sources.set(page.src.repo, page.src);

  const rows = [...sources.entries()]
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([repo, src]) => `- \`${repo}\` at commit \`${commits.get(repo)}\`, licensed ${src.license}.`);

  return [
    "# Vendored reference",
    "",
    "Generated by `npx tsx .github/scripts/vendor-docker-docs/main.ts`, and again on every",
    "CI build of this plugin. Do not edit these files by hand -- a regenerate overwrites them.",
    "",
    "## Sources",
    "",
    ...rows,
    "",
    "The text is reproduced verbatim under those licenses. Hugo frontmatter and",
    "shortcodes are stripped, include partials are inlined, and root-relative links",
    "are rewritten to absolute `docs.docker.com` URLs. Nothing else is altered.",
    "",
  ].join("\n");
}
