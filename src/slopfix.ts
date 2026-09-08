// The bridge to slopfix, which owns every rule this plugin reports.
//
// Nothing here decides anything. It finds the binary the plugin ships, hands it
// the text and the path, and reads the findings back. A rule restated on this
// side is a second source of truth that is correct on the day somebody writes
// it and wrong the first time the rule changes, with nothing to say so.

import {execFileSync} from "node:child_process";
import {existsSync} from "node:fs";
import {dirname, join} from "node:path";
import {fileURLToPath} from "node:url";

/** One finding, as `slopfix report` writes it. */
export interface SlopfixFinding {
  /** The rule ID, which is also the name slopfix prints and accepts. */
  id: string;
  line: number;
  endLine: number;
  rule: string;
  detail?: string;
  fix?: string;
}

/**
 * Where the shipped binary sits, relative to the plugin root.
 *
 * `release-plugin` rewrites `build/` into the APE plus a launcher named after
 * the plugin, so a published plugin carries the first name. A local `just
 * prebuild` leaves the fetched file under its own name, so the tests that run
 * inside that same recipe find the second.
 */
const CANDIDATES = ["build/common-checks", "build/slopfix_cosmo_fat"];

/** The directory this module was loaded from, under tsx and under the bundle. */
function moduleDir(): string {
  // The bundle is CommonJS and has __dirname. tsx runs the source as ESM and
  // does not, so referencing it unguarded throws rather than reading undefined.
  if (typeof __dirname !== "undefined") return __dirname;
  return dirname(fileURLToPath(import.meta.url));
}

let cached: string | undefined;

/**
 * The slopfix the plugin ships. It never looks on PATH: the plugin and the
 * binary would then travel on separate tracks, and a plugin naming a
 * subcommand its installed binary predated reports nothing at all.
 *
 * COMMON_CHECKS_SLOPFIX overrides it, which is how a build tests against a
 * slopfix that has not published.
 */
export function slopfixPath(): string | undefined {
  // An override that names nothing is the same as no binary at all, and it must
  // read that way rather than as a spawn failure further down.
  const named = process.env.COMMON_CHECKS_SLOPFIX;
  if (named) return existsSync(named) ? named : undefined;
  if (cached !== undefined) return cached || undefined;
  // The module sits one directory under the plugin root, whether that is
  // `src/` under tsx or `server/` in the bundle.
  const root = join(moduleDir(), "..");
  for (const candidate of CANDIDATES) {
    const path = join(root, candidate);
    if (existsSync(path)) {
      cached = path;
      return path;
    }
  }
  cached = "";
  return undefined;
}

/** Reset the memoized lookup. Tests move the binary between cases. */
export function forgetSlopfixPath(): void {
  cached = undefined;
}

/**
 * A slopfix that is absent, or that cannot answer. Named so a caller tells it
 * apart from a finding and decides what to do about it.
 */
export class SlopfixUnavailable extends Error {}

/**
 * Every finding slopfix reports for text headed for `relativePath`.
 *
 * A failure THROWS. It does not answer with an empty list, which reads exactly
 * like a clean file: a run that checked nothing must never report a pass. Every
 * caller decides for itself, and only the PreToolUse hook swallows this. The
 * build gate is what stops a binary that cannot answer from shipping at all.
 */
export function report(relativePath: string, content: string): SlopfixFinding[] {
  const binary = slopfixPath();
  if (!binary) {
    throw new SlopfixUnavailable(
      "no slopfix binary beside this plugin, so nothing can be checked. " +
        "The plugin ships one under build/. Set COMMON_CHECKS_SLOPFIX to point at another.",
    );
  }
  let out: string;
  try {
    out = execFileSync(binary, ["report", "--path", relativePath], {
      input: content,
      encoding: "utf8",
      maxBuffer: 32 * 1024 * 1024,
    });
  } catch (error) {
    throw new SlopfixUnavailable(`${binary} could not read ${relativePath}: ${(error as Error).message}`);
  }
  try {
    return (JSON.parse(out) as {findings?: SlopfixFinding[]}).findings ?? [];
  } catch (error) {
    throw new SlopfixUnavailable(`${binary} answered with something that is not JSON: ${(error as Error).message}`);
  }
}
