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

const DELETED_STE_WORD_FIX: Record<string, string> = {
  "can't": "cannot", "won't": "will not", "don't": "do not", "doesn't": "does not",
  "didn't": "did not", "isn't": "is not", "aren't": "are not", "wasn't": "was not",
  "weren't": "were not", "wouldn't": "will not", "shouldn't": "must not",
  "couldn't": "cannot", "mustn't": "must not", "hasn't": "has not",
  "haven't": "have not", "hadn't": "had not", "it's": "it is", "that's": "that is",
  "there's": "there is", "here's": "here is", "let's": "let us", "we're": "we are",
  "they're": "they are", "you're": "you are", "i'm": "I am", "i've": "I have",
  "we've": "we have", "they've": "they have", "you've": "you have", "i'll": "I will",
  "we'll": "we will", "they'll": "they will", "you'll": "you will", "he's": "he is",
  "she's": "she is", "who's": "who is", "what's": "what is",
  should: "must", shall: "must", could: "can", might: "can", would: "will",
};

/**
 * The replacement to write, when the rule has exactly one.
 *
 * Claude Code cannot accept a fix from a language server: cli.js 2.1.241
 * declares no `codeAction` capability, never sends `textDocument/codeAction`,
 * and has no `workspace/applyEdit` handler. The diagnostic message is the only
 * channel to the model, and it survives as text. So a rule whose repair is one
 * word says that word here, and the reader makes a one-token edit rather than
 * re-deriving it.
 */
function steFix(bucket: string, detail: string): string {
  const quoted = /"([^"]+)"/.exec(detail)?.[1];
  if ((bucket === "contractions" || bucket === "bannedModals") && quoted) {
    const fix = STE_WORD_FIX[quoted.toLowerCase()];
    // A capitalized original keeps its capital, so the replacement is a drop-in.
    if (fix) return ` Write "${/^[A-Z]/.test(quoted) ? fix[0].toUpperCase() + fix.slice(1) : fix}".`;
  }
  if (bucket === "semicolons") return ' Write ". " and capitalize the next word.';
  if (bucket === "commaSplices") return " Write a period in place of the comma and capitalize the next word.";
  return "";
}

/**
 * ste-lint reports `<name>:<line>: <detail>` strings, so the line comes back
 * out by matching on the name it was given.
 */
function parseSteFinding(name: string, entry: string): {line: number; detail: string} | undefined {
  const match = new RegExp(`^${escapeRegExp(name)}:(\\d+):\\s*(.*)$`).exec(entry);
  if (!match) return undefined;
  return {line: Number(match[1]), detail: match[2]};
}

function ste(relativePath: string, content: string): Finding[] {
  const findings = steLint.lintText(relativePath, content);
  const out: Finding[] = [];
  for (const bucket of STE_BUCKETS) {
    const entries = findings[bucket];
    // A hard-wrapped paragraph reports every continuation line, which on an
    // ordinary document is hundreds of findings for one thing to fix. One per
    // paragraph is the same information and leaves room for everything else.
    const collapsed = bucket === "wrappedLines" ? collapseRuns(relativePath, entries) : entries;
    for (const entry of collapsed) {
      const parsed = parseSteFinding(relativePath, entry);
      if (!parsed) continue;
      const detail = parsed.detail === "" ? "" : ` ${parsed.detail}`;
      out.push({
        check: "ste-lint",
        startLine: parsed.line,
        endLine: parsed.line,
        message: `${STE_REMEDIES[bucket]}${detail}${steFix(bucket, parsed.detail)}`,
      });
    }
  }
  return out;
}

/** Keeps the first entry of each run of consecutive lines. */
function collapseRuns(name: string, entries: string[]): string[] {
  const kept: string[] = [];
  let previous = -2;
  for (const entry of entries) {
    const parsed = parseSteFinding(name, entry);
    if (!parsed) continue;
    if (parsed.line !== previous + 1) kept.push(entry);
    previous = parsed.line;
  }
  return kept;
}

/**
 * Every way this file would fail common-checks, ranked so a structural finding
 * is never pushed out of the client's budget by a voluminous one.
 */
export function findings(relativePath: string, content: string): Finding[] {
  const kind = fileKind(relativePath);
  const out: Finding[] = [];

  if (kind === "workflow" || kind === "action") {
    out.push(...yamlCommentBlock(content), ...noTestsInYaml(content));
  }
  if (kind === "workflow") {
    out.push(...noAllBuildsJob(relativePath, content), ...neuteredGate(content));
  }
  if (kind === "markdown") {
    out.push(...ste(relativePath, content));
  }

  return out.sort((a, b) => rank(a.check) - rank(b.check) || a.startLine - b.startLine);
}
