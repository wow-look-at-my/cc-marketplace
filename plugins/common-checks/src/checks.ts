// The adapter. It asks slopfix the question CI asks it, and turns the answer
// into something a diagnostic can point at.
//
// A rule is never restated here. Every verdict comes out of the slopfix binary
// this plugin ships, which is the binary the org's common-checks gate runs.
// What this file adds is the one thing a check invoked from a workflow has no
// reason to produce: a file kind that says which checks even apply.

import {report} from "./slopfix.ts";

export interface Finding {
  /** The check that produced this, used as the diagnostic's `code`. */
  check: string;
  /** 1-based, inclusive. */
  startLine: number;
  endLine: number;
  message: string;
}

export type FileKind = "workflow" | "action" | "markdown" | "other";

/**
 * Which checks an open file is subject to. common-checks passes no path
 * filters of its own, so this mirrors what each check reads: workflow files
 * and action manifests for the YAML checks, and every markdown file for
 * ste-lint.
 */
export function fileKind(relativePath: string): FileKind {
  const path = relativePath.replace(/\\/g, "/").replace(/^\.\//, "");
  if (/^\.github\/workflows\/[^/]+\.ya?ml$/.test(path)) return "workflow";
  if (/(^|\/)action\.ya?ml$/.test(path)) return "action";
  if (/\.md$/i.test(path)) return "markdown";
  return "other";
}

// Ranking. Every finding here fails CI, so severity cannot separate them; what
// separates them is that ste-lint can produce hundreds of findings on one
// document and the structural checks produce one or two. The client injects
// only the first handful, so a voluminous check must never crowd out a
// structural one.
// This doubles as the coverage claim: every check named here is one this
// plugin reports, and `checks.test.ts` holds it against the manifest the build
// read from upstream. A check upstream runs and this plugin neither reports
// nor declares uncovered would otherwise ship as silent non-coverage.
export const ADAPTED = ["no-all-builds-job", "yaml-comment-block", "no-tests-in-yaml", "ste-lint"];

/**
 * The checks upstream runs that no open file can violate, and why.
 *
 * Together with ADAPTED this is the whole claim: every check the manifest names
 * sits in exactly one of the two lists. Naming one here is a declared gap,
 * which is the honest alternative to reimplementing a rule this plugin must
 * not own.
 */
export const UNCOVERED: Record<string, string> = {
  "run-once": "it claims the workflow run for one job. There is no rule a file can break.",
  "push-excludes-tags":
    "its rule is an inline script inside a composite action, which nothing imports and slopfix does not " +
    "carry. Reimplementing it here is the one thing this plugin must never do.",
};

/**
 * Each slopfix rule the org's gate enforces, and the common-checks step that
 * runs it. The step name is what a diagnostic carries as its `code`.
 *
 * This is a selection rather than a rule. slopfix reports more than the gate
 * fails on -- `ste/count` is repaired by a different plugin and never fails
 * common-checks -- and a diagnostic for a rule the merge gate ignores spends
 * the client's budget on something nobody has to fix.
 */
const GATE_RULES: Record<string, string> = {
  "yaml/all-builds-job": "no-all-builds-job",
  "yaml/comment-block": "yaml-comment-block",
  "yaml/test-in-workflow": "no-tests-in-yaml",
  // ste-lint's own step carries the continue-on-error guard, so a finding
  // there is reported under that step's name.
  "yaml/neutered-gate": "ste-lint",
  "ste/contraction": "ste-lint",
  "ste/modal": "ste-lint",
  "ste/semicolon": "ste-lint",
  "ste/sentence-length": "ste-lint",
  "ste/comma-splice": "ste-lint",
  "wrap/hard-wrap": "ste-lint",
};

function rank(check: string): number {
  const index = ADAPTED.indexOf(check);
  return index === -1 ? ADAPTED.length : index;
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
 * A rule slopfix reports that the gate does not fail on is dropped here. The
 * whole promise of a diagnostic from this plugin is that it fails the merge
 * gate, and a finding nobody has to act on spends the budget for one that does.
 */
export function findings(relativePath: string, content: string): Finding[] {
  if (fileKind(relativePath) === "other") return [];

  const out: Finding[] = [];
  for (const finding of report(relativePath, content)) {
    const check = GATE_RULES[finding.id];
    if (!check) continue;
    out.push({
      check,
      startLine: finding.line,
      endLine: finding.endLine || finding.line,
      message: message(finding.rule, finding.detail, finding.fix),
    });
  }
  return out.sort((a, b) => rank(a.check) - rank(b.check) || a.startLine - b.startLine);
}
