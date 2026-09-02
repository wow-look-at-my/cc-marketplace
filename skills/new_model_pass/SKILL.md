---
description: Do a full improvement pass over an existing codebase - read it end to end, find everything worth fixing, and fix it. Written for pointing a newly released model at a repo that older models have already been over, to see what it can find that they could not.
argument-hint: [path or subsystem, default the whole repo]
disable-model-invocation: true
---

# Full pass

Scope is `$ARGUMENTS`. Empty means the whole repository.

This code has already been read, reviewed, and edited by earlier models. Assume the
easy findings are gone. What is left is what needs more of the codebase held in mind
at once than the last pass managed, or a fact the last model did not know. That is
what you are here for, and it is also the measurement: the value of this pass is the
findings a weaker pass could not have produced.

## 1. Map it before you change anything

Read the build files, the entry points, the test layout, `CLAUDE.md` and `docs/`.
Learn what the thing is supposed to do before judging what it does. Then read the
actual source -- whole files, not the hits from a grep. A defect that lives in the
relationship between two files is invisible to a search for a pattern.

Cover everything in scope. If it is too large for that, split it into passes and say
in the final report which parts you covered and which you did not. Never let partial
coverage read as complete.

## 2. Hunt where context is required

Rank effort toward defects that only show up when you are holding several files at
once. In rough order of what tends to be left behind:

- **Contract mismatches.** A producer and a consumer that disagree about a field,
  a unit, a nil case, an ordering guarantee, an error value. Each side reads fine
  alone.
- **Invariants stated in one place and broken in another.** A comment, a doc, or a
  validator says X holds; some other path constructs a value where it does not.
- **Wrong edge-case behavior.** Empty input, one element, duplicate keys, unicode,
  timezone and DST, integer overflow, concurrent access, retry that is not
  idempotent, cancellation mid-write.
- **Silent failure.** Swallowed errors, defaulted-away missing config, a fallback
  nobody is told fired, a truncation with no log, a catch that continues into code
  that needed the thing that failed. A green run that did nothing is the worst
  defect class in any repo.
- **Tests that assert the easy half.** A test that would still pass with the bug
  present. Reproduce it: break the code deliberately and see whether the suite
  notices.
- **Stale knowledge.** APIs, language features, tool flags, and library idioms that
  moved after the last pass was written. You know things earlier models did not --
  check the current docs before rewriting on a hunch, then use what is actually
  available now instead of the workaround built to avoid its absence.
- **Duplication with drift.** The same logic in three places, two of which have been
  fixed. Hoist it, or at minimum fix the stragglers.
- **Dead code and dead branches.** A guard for a state the system is never in, a
  flag nothing sets, an exported function nobody calls.
- **Performance that matters.** Quadratic behavior on input that grows, work
  repeated inside a loop, a query per row. Ignore micro-optimizations nobody can
  measure.
- **Docs and comments that lie.** Every claim in them is checked or it goes. A
  comment naming a function or a number that no longer exists is a defect, not a
  cosmetic issue.

## 3. Fix, do not file

Finding it creates the obligation to repair it. You have the repo open and the
diagnosis in hand -- a report handed back unfixed makes the reader pay for it twice.
Fix it in this pass.

For a behavior bug: write the failing test first, watch it go red against the
unfixed code, then fix it. A test that never went red proves nothing.

Defer only what genuinely needs a human decision -- a design fork with more than one
defensible answer, an irreversible action, access you do not have. Then name it
exactly, say what the fix would be, and put it in the report.

## 4. This is not a security audit

Hardening here means the code fails loudly, handles its real inputs correctly, and
does not corrupt state when something goes wrong. It does not mean bolting auth onto
a local script, validating inputs that come from your own code, adding defensive
`try`/`catch` around things that cannot throw, or rewriting a working parser because
a hostile input could theoretically exist.

If you find a real exploitable bug in something that actually faces untrusted input,
fix it and say so. Do not go looking for a threat model the project does not have.

## 5. Do not churn

The failure mode of this skill is a huge diff of taste changes that buries three real
fixes. Every change needs a reason the owner would agree with out loud.

- No reformatting, no renaming for style, no reordering, no reshuffling files.
- No rewriting working code into your preferred idiom.
- No new abstraction with one caller.
- No new dependency where a few lines of the standard library do it.
- Match the surrounding code's conventions, including the ones you would not have
  chosen.
- Dependency and toolchain upgrades are in scope when they fix something or unlock
  something you then use; a version bump for its own sake is not.

## 6. Verify

Run the build. Run the tests. Run the repo's own toolchain command if it has one --
check `CLAUDE.md`, the `justfile`, or the CI workflow for what that is, and use it
rather than the raw language tool. If a change touches something rendered, look at it
rendered. Report what you ran and what it said, including anything still failing.

Commit in coherent pieces, one concern per commit, with messages that say why.

## 7. The report

The report is the deliverable of the pass, because it is what gets compared against
the last model's. Write it as:

- **Fixed** -- each defect, where it was, why it was wrong, how you verified.
- **Found and left** -- what it is, where, what the fix would be, why it needs a
  decision.
- **Checked and fine** -- the areas you read carefully and found genuinely healthy.
  This is not filler; without it, silence about a subsystem is ambiguous between
  "clean" and "never opened".
- **Not covered** -- anything in scope you did not get to.

Rank the fixed list by how much of the codebase you had to hold at once to see the
problem. That ordering is the actual answer to "what can this model find that the
last one could not".
