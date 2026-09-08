// The adapter. It asks slopfix the question CI asks it, and turns the answer
// into something a diagnostic can point at.
//
// A rule is never restated here. Every verdict comes out of the slopfix binary
// this plugin ships, which is the binary the org's common-checks gate runs.
// What this file adds is the one thing a check invoked from a workflow has no
// reason to produce: a file kind that says which checks even apply.

import {report} from "./slopfix.ts";

export interface Finding {
  /** The slopfix rule ID, used as the diagnostic's `code`. */
  check: string;
  /** 1-based, inclusive. */
  startLine: number;
  endLine: number;
  message: string;
}

export type FileKind = "workflow" | "action" | "markdown" | "other";

/**
 * Which rules an open file is subject to.
 *
 * slopfix decides this from the path too, and it has to: a server handed one
 * open buffer knows nothing else about it. What slopfix does not answer is
 * whether the file is one the gate reads at ALL. Its `check` command judges any
 * file it is named as prose, because naming it is the request. The gate instead
 * walks a tree, and that walk selects workflow files, action manifests and
 * markdown. This mirrors the walk. Firing on every `.yaml` and every `.go` is
 * how a checker earns the reputation that gets it uninstalled.
 */
export function fileKind(relativePath: string): FileKind {
  const path = relativePath.replace(/\\/g, "/").replace(/^\.\//, "");
  if (/^\.github\/workflows\/[^/]+\.ya?ml$/.test(path)) return "workflow";
  if (/(^|\/)action\.ya?ml$/.test(path)) return "action";
  if (/\.md$/i.test(path)) return "markdown";
  return "other";
}

/**
 * Rule families, most structural first.
 *
 * Every finding fails CI, so severity cannot separate them. What separates them
 * is volume: a prose rule reports hundreds on one document and a YAML rule
 * reports one or two. The client injects only the first handful of diagnostics
 * per file, so a voluminous family must never crowd out a structural one. The
 * wrap rule sits last of all, being both the most numerous and the most
 * mechanical to repair.
 *
 * This is not a list of checks. slopfix's default set IS common-checks, so no
 * list here can fall out of step with it, and there is nothing to keep in sync.
 */
const FAMILY_ORDER = ["yaml", "ste", "wrap"];

function rank(check: string): number {
  const index = FAMILY_ORDER.indexOf(check.split("/")[0]);
  return index === -1 ? FAMILY_ORDER.length : index;
}

/** The sentence a diagnostic carries, assembled from what slopfix reported. */
function message(rule: string, detail?: string, fix?: string): string {
  const quoted = detail ? ` ${JSON.stringify(detail)}` : "";
  const repair = fix ? ` ${fix}` : "";
  return `${rule}${quoted}.${repair}`;
}

/**
 * Every way this file would fail common-checks, ranked so a structural finding
 * is never pushed out of the client's budget by a voluminous one.
 *
 * Nothing is filtered. slopfix's default rule set is what common-checks runs,
 * so a rule this dropped would be one the gate fails on and the reader never
 * heard about.
 */
export function findings(relativePath: string, content: string): Finding[] {
  if (fileKind(relativePath) === "other") return [];

  const out = report(relativePath, content).map((finding) => ({
    check: finding.id,
    startLine: finding.line,
    endLine: finding.endLine || finding.line,
    message: message(finding.rule, finding.detail, finding.fix),
  }));
  return out.sort((a, b) => rank(a.check) - rank(b.check) || a.startLine - b.startLine);
}
