// The adapters. Each one asks a vendored check the same question CI asks it,
// then turns the answer into something a diagnostic can point at.
//
// A rule is never restated here. Every verdict comes out of `../vendor`, which
// a build re-fetches from wow-look-at-my/actions. What this file adds is the
// two things a check written for CI has no reason to produce: a file kind that
// says which checks even apply, and a LINE for a finding whose CI form only
// ever named a file.

import * as commentBlock from "../vendor/yaml-comment-block/scan.ts";
import * as testsInYaml from "../vendor/no-tests-in-yaml/scan.ts";
import * as allBuildsJob from "../vendor/no-all-builds-job/detect.ts";
import * as pushExcludesTags from "../vendor/push-excludes-tags/scan.ts";
import * as steLint from "../vendor/ste-lint/lint.ts";
import * as steGuard from "../vendor/ste-lint/guard.ts";

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
const ORDER = ["push-excludes-tags", "no-all-builds-job", "yaml-comment-block", "no-tests-in-yaml", "ste-lint"];

function rank(check: string): number {
  const index = ORDER.indexOf(check);
  return index === -1 ? ORDER.length : index;
}

/** 1-based line of the first source line matching `pattern`, or `fallback`. */
function lineOf(lines: string[], pattern: RegExp, fallback: number): number {
  const index = lines.findIndex((line) => pattern.test(line));
  return index === -1 ? fallback : index + 1;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function yamlCommentBlock(content: string): Finding[] {
  return commentBlock.findCommentBlocks(content).map((block) => ({
    check: "yaml-comment-block",
    startLine: block.startLine,
    endLine: block.endLine,
    message:
      `${block.lines} comment lines in a row -- the limit is ${commentBlock.MAX_COMMENT_LINES}. ` +
      "Shorten this to one line. Say only what a reader needs right here.",
  }));
}

function noTestsInYaml(content: string): Finding[] {
  const findings: Finding[] = [];
  for (const block of testsInYaml.findRunBlocks(content)) {
    for (const finding of testsInYaml.findFindings(block)) {
      findings.push({
        check: "no-tests-in-yaml",
        startLine: finding.line,
        endLine: finding.line,
        message: `a test in a workflow file [${finding.rule}] -- ${finding.remedy}`,
      });
    }
  }
  return findings;
}

// The upstream detector names the job, not the line it sits on: a CI
// annotation only ever had a file to attach to. The verdict stays upstream and
// only the cursor position is worked out here.
function noAllBuildsJob(relativePath: string, content: string): Finding[] {
  const lines = content.split(/\r?\n/);
  return allBuildsJob.scanWorkflowYaml(relativePath, content).map((violation) => {
    const key = escapeRegExp(violation.jobKey);
    const anchor =
      violation.via === "key"
        ? new RegExp(`^\\s+${key}\\s*:`)
        : new RegExp(`^\\s+name\\s*:\\s*['"]?${escapeRegExp(allBuildsJob.GUARDED_NAME)}`);
    const line = lineOf(lines, anchor, 1);
    return {
      check: "no-all-builds-job",
      startLine: line,
      endLine: line,
      message: allBuildsJob.formatViolation(`job ${violation.jobKey}`),
    };
  });
}

function pushTrigger(content: string): Finding[] {
  return pushExcludesTags.scanWorkflowYaml(content).map((finding) => ({
    check: "push-excludes-tags",
    startLine: finding.line,
    endLine: finding.line,
    message: pushExcludesTags.REMEDY,
  }));
}

// A step allowed to fail is not a gate. The guard reports `uses: ... (line N)`,
// so the line comes back out of its own text.
function neuteredGate(content: string): Finding[] {
  return steGuard.neuteredSteps(content).map((step) => {
    const at = /\(line (\d+)\)\s*$/.exec(step);
    const line = at ? Number(at[1]) : 1;
    return {
      check: "ste-lint",
      startLine: line,
      endLine: line,
      message:
        "this step runs common-checks under continue-on-error. A step allowed to fail is not a gate. " +
        "Remove continue-on-error, or remove the step and say so out loud.",
    };
  });
}

// The one sentence each failing ste-lint bucket needs. The rule and the finding
// are upstream; this is the wording a single diagnostic carries, where the CI
// report groups every finding of a kind under one heading.
const STE_REMEDIES: Record<string, string> = {
  hardLong: "over the sentence-length cap. Split it.",
  contractions: "STE bans contractions. Write the words out.",
  bannedModals: 'STE bans "should"/"shall"/"could"/"might"/"would" -- use "must"/"must not", or "can".',
  semicolons: "STE bans the semicolon. Use a period and start a new sentence.",
  commaSplices: "a comma joining two clauses is the semicolon STE bans, spelled differently. Use a period.",
  wrappedLines: "a paragraph is one line. Join it back up and let the reader's window wrap it.",
};

// Each failing bucket, in the order failureReport prints them.
const STE_BUCKETS = ["hardLong", "contractions", "bannedModals", "semicolons", "commaSplices", "wrappedLines"] as const;

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
        message: `${STE_REMEDIES[bucket]}${detail}`,
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
    out.push(...noAllBuildsJob(relativePath, content), ...pushTrigger(content), ...neuteredGate(content));
  }
  if (kind === "markdown") {
    out.push(...ste(relativePath, content));
  }

  return out.sort((a, b) => rank(a.check) - rank(b.check) || a.startLine - b.startLine);
}
