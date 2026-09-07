// The pure half of the vendoring: what to fetch, and the assertion that the
// plugin still covers every check common-checks runs. No network, no disk.

export const UPSTREAM_REPO = "wow-look-at-my/actions";

/** The branch to vendor from. Overridable so a branch can be built before it merges. */
export function upstreamRef(env: NodeJS.ProcessEnv = process.env): string {
  return env.COMMON_CHECKS_REF || "master";
}

export interface CheckPlan {
  /** The action directory, which is also its `uses:` slug. */
  name: string;
  /**
   * Paths under the action directory to vendor. Empty when the check is not a
   * diagnostic source, in which case `why` says so.
   */
  files: string[];
  /** Why this entry contributes no diagnostics. Present only when files is empty. */
  why?: string;
}

/** The manifest common-checks publishes beside its composite. */
export const MANIFEST_PATH = "common-checks/checks.json";

/**
 * The plan, read from upstream's own manifest rather than kept here.
 *
 * A list maintained on this side can only ever describe what upstream looked
 * like when somebody last looked. It stays green while a rule moves to a module
 * nothing vendors, which is the one drift the `uses:` list cannot show. The
 * manifest names the modules, so the plugin's coverage is upstream's statement
 * about itself.
 *
 * Paths are repo-relative there and check-relative here, so the prefix is
 * checked rather than trimmed blindly: a path under the wrong check is a
 * manifest error, not a file to vendor somewhere surprising.
 */
export function parseManifest(source: string): CheckPlan[] {
  let data: unknown;
  try {
    data = JSON.parse(source);
  } catch (error) {
    throw new Error(`${MANIFEST_PATH} is not valid JSON: ${(error as Error).message}`);
  }
  const checks = (data as {checks?: unknown}).checks;
  if (!Array.isArray(checks) || checks.length === 0) {
    throw new Error(`${MANIFEST_PATH} names no checks. It must carry a non-empty "checks" array.`);
  }
  return checks.map((raw, index) => {
    const entry = raw as {name?: unknown; modules?: unknown; reason?: unknown};
    const name = entry.name;
    if (typeof name !== "string" || name === "") {
      throw new Error(`${MANIFEST_PATH} check ${index} has no name.`);
    }
    const modules = entry.modules;
    if (!Array.isArray(modules)) {
      throw new Error(`${MANIFEST_PATH} check ${name} has no modules array. Use [] plus a reason when nothing importable carries its rule.`);
    }
    const files = modules.map((module) => {
      if (typeof module !== "string") {
        throw new Error(`${MANIFEST_PATH} check ${name} lists a module that is not a path.`);
      }
      const prefix = `${name}/`;
      if (!module.startsWith(prefix)) {
        throw new Error(`${MANIFEST_PATH} check ${name} lists ${module}, which is not under ${prefix}.`);
      }
      return module.slice(prefix.length);
    });
    if (files.length === 0 && typeof entry.reason !== "string") {
      throw new Error(`${MANIFEST_PATH} check ${name} vendors nothing and gives no reason. A silent omission and a decision look the same without one.`);
    }
    return files.length === 0 ? {name, files, why: entry.reason as string} : {name, files};
  });
}

/**
 * Every action this repository's own common-checks composite runs, in the order
 * it runs them. Read with a regex rather than a YAML walk, matching how the
 * checks themselves read `uses:`: the key means the same thing at every depth.
 */
export function parseCheckSet(actionYml: string): string[] {
  const slugs: string[] = [];
  for (const line of actionYml.split(/\r?\n/)) {
    const match = /^\s*(?:-\s+)?uses:\s*(?:'([^']*)'|"([^"]*)"|(\S+))/.exec(line);
    if (!match) continue;
    const value = match[1] ?? match[2] ?? match[3] ?? "";
    // `owner/repo@slug#ref` is this org's action-reference spelling.
    const ref = new RegExp(`^${UPSTREAM_REPO}@([A-Za-z0-9._-]+)#`).exec(value);
    if (ref) slugs.push(ref[1]);
  }
  return slugs;
}

/**
 * Fails when the plugin's plan and the composite's step list have drifted.
 * This is the whole sync guarantee: adding a check to common-checks turns this
 * build red until the plugin either covers it or states why it cannot.
 */
export function assertPlanCoversCheckSet(plan: CheckPlan[], checkSet: string[]): void {
  const planned = new Set(plan.map((entry) => entry.name));
  const upstream = new Set(checkSet);

  const missing = [...upstream].filter((name) => !planned.has(name)).sort();
  const stale = [...planned].filter((name) => !upstream.has(name)).sort();
  if (missing.length === 0 && stale.length === 0) return;

  const problems: string[] = [];
  if (missing.length > 0) {
    problems.push(
      `common-checks now runs ${missing.join(", ")}, which ${MANIFEST_PATH} does not name. ` +
        "Add an entry there listing the modules that carry its rule, then write the adapter that " +
        "turns its findings into diagnostics. An entry with no modules needs a reason saying why " +
        "nothing in an open file can violate it.",
    );
  }
  if (stale.length > 0) {
    problems.push(
      `${MANIFEST_PATH} still names ${stale.join(", ")}, which common-checks no longer runs. ` +
        "Drop the entry there, and drop its adapter here.",
    );
  }
  throw new Error(`the plugin and common-checks have drifted:\n  ${problems.join("\n  ")}`);
}

export function notice(commit: string, plan: CheckPlan[]): string {
  const importable = plan.filter((entry) => entry.files.length > 0);
  const uncovered = plan.filter((entry) => entry.files.length === 0);
  const lines = [
    "# The checks common-checks runs",
    "",
    `Read from [${UPSTREAM_REPO}](https://github.com/${UPSTREAM_REPO})'s own manifest at commit \`${commit}\`.`,
    "",
    "This file is generated. The plugin's build re-reads the manifest on every",
    "run and overwrites any local change.",
    "",
    "The plugin gets every verdict from the slopfix binary it ships. Nothing here",
    "is imported. What the manifest still settles is coverage: the plugin has to",
    "report each check below, or say in `src/checks.ts` why it cannot.",
    "",
    "## Upstream still carries an importable module for these",
    "",
    ...importable.map((entry) => `- \`${entry.name}\` -- ${entry.files.join(", ")}`),
    "",
    "## Upstream states that nothing importable carries the rule",
    "",
    ...uncovered.map((entry) => `- \`${entry.name}\` -- ${entry.why}`),
    "",
  ];
  return lines.join("\n");
}
