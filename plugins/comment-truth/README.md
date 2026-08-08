# comment-truth

A Stop hook that asks whether the comments this session wrote are **true**.

Comments are the one artifact nothing verifies. Compilers ignore them, tests
ignore them, CI ignores them — the reader is the only check, and by then the
author is not there to correct it. So a wrong comment is read as authority and
believed for years, which makes it worse than no comment at all.

## How it stays cheap

Three passes, cheapest first, each narrowing what the next has to look at.

**1. Scope — free.** Only comments this session's diff added or changed, across
committed, staged, unstaged and untracked files. A repository holds thousands of
comments; a session writes a handful. Cost tracks the size of the change, not
the size of the repo, and a session that wrote no comments does no work.

**2. Mechanical verdicts — free.** Of the comments that survive, most claim
nothing checkable (`// bump the counter`). The rest are sorted by what kind of
claim they make, and two kinds are settled by looking:

| claim | reported when |
|---|---|
| **reference** — names a symbol, test or file | the name does not resolve anywhere in the repo, a sibling checkout, or `.gitignore` |
| **quantity** — a figure with a unit | the document the comment itself cites has no figure in that unit which rounds to it |
| **hedge** — "probably", "presumably", "I believe" | always: a hedge is an unverified claim with an escape hatch |
| **bloat** — length against what it annotates | a long comment sits on a one-line declaration |
| **tombstone** — a date, a PR number, the past tense | always: git already stores when it changed and who asked |

**3. Judgment — one bounded model call.** What is left needs reading
comprehension: is a causal claim supported, is a measurement scoped to what was
measured, is the comment even about the thing it sits on. The hook resolves
every piece of evidence **mechanically first** — the annotated code, the
definitions of the names cited, the relevant lines of the cited document — and
sends comment plus evidence in one batched request. The model never explores the
repository: no agentic loop, no tool calls, a bounded prompt.

That inversion is the design. An agent turned loose on "is this comment true"
spends its budget searching. Here the searching is grep, which is free, and the
model only does the part that actually needs a reader.

## Accuracy

A false alarm costs more than a miss: it teaches people to switch the check off.
So each rule is written to survive real prose, and the tests are the real cases.

- **Rounding and truncation both pass.** A document measuring 23.8 B/event is
  honestly written "~24" *or* "~23"; demanding a literal match would make the
  check unusable. The tolerance is the comment's own precision, so "~10" against
  7.5 still fails — which is the defect this was built after.
- **Figures are matched within their unit family**, not against every number on
  the page. Checking a bare number against a whole document is so permissive
  that a wrong figure passes on a stray match.
- **A bare integer is not a measurement.** "step 2", "v1", "RFC 8628" are not
  claims.
- **Quoted text is an example, not a citation.** Backticked and bare names are
  checked; `"quoted"` ones are specimens being shown. Without that rule the tool
  reports its own documentation — it did, before the rule existed.
- **URLs and absolute paths are stripped** before paths are read out of prose.
- **Build outputs resolve** by asking git whether a path is *ignored*, not
  whether it happens to be built right now.
- **"should" is not a hedge.** "callers should drain the manager" is a contract,
  not uncertainty.
- **Length is judged as PROPORTION.** A page of prose over a subtle function is
  fine and truncating it would be the same defect in reverse; the same page over
  `const X = "..."` is a document that has not been written yet. So a long
  comment is reported only when the thing it annotates is a one-line
  declaration.
- **Tombstones include the past tense**, not just dates and PR numbers: "used
  to", "no longer", "formerly", "was removed". Naming one of those phrases as
  an *example* does not count as using it — quoted spans are stripped first, so
  a comment documenting the rule is not reported by it. (This tool's own source
  is the pathological case, being the one file whose subject matter is these
  phrases; that is a property of writing about tombstones, not a reason to stop
  detecting them.)
- **A placeholder is not a name.** `vX.Y.Z`, a glob, an `<angle-bracket>` slot —
  naming the shape of a thing is not a claim that a thing with that literal name
  exists.

## Configuration

| variable | meaning |
|---|---|
| `COMMENT_TRUTH_API_KEY` | enables pass 3. **Unset is a clean no-op** — the mechanical passes still block. |
| `COMMENT_TRUTH_API_URL` | OpenAI-compatible chat-completions endpoint. Default `https://api.openai.com/v1/chat/completions`. |
| `COMMENT_TRUTH_MODEL` | default `gpt-4o-mini`. |

A configured endpoint that cannot be reached is reported **loudly**: a checker
that goes quiet when its backend fails is claiming the comments were checked
when they were not.

## Behaviour

Exit 2 blocks the stop and puts the findings in front of the model as work to
do. `stop_hook_active` short-circuits, so a block never loops. Garbage on stdin,
a missing repo, or a detached checkout all exit 0 silently — this hook never
becomes the reason a session cannot end.
