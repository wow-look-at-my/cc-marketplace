---
description: Read before answering any question about how Claude Code itself behaves -- hooks, plugins, LSP servers, settings keys, slash commands, tool schemas, env vars, permission rules, telemetry. Explains how to fetch the prettified source tree (cli.js plus chunks/) for the RIGHT version from claude-docs-gaps and, non-negotiably, how to search it from a Sonnet subagent instead of burning main context on tens of megabytes of bundle.
---

# Reading Claude Code's own source for ground truth

Notes to self. The docs describe the product. The shipped bundle **is** the product. When the two disagree the source wins, and when the docs are silent the source is the only answer that exists. `PazerOP/claude-docs-gaps` keeps a prettified copy of the shipped bundle on **one branch per released version**, which is what makes this practical.

Rules. The second one is the whole point of this skill.

## 1. Get the right version's branch into /tmp

One call does all of it. Run `fetch.sh` from this skill's base directory, the path printed above as "Base directory for this skill":

```bash
sh <base directory>/fetch.sh            # the running version
sh <base directory>/fetch.sh 2.1.220    # a named version
```

It prints the tree's path as its last line. Use that path, called `$D` below, for every search. It reuses a tree already in `/tmp`. It shallow-clones exactly one branch in about 2 s. A version with no branch reads the highest branch below it and says so on stderr. Say that version in your report.

Never paste the fetch as inline shell. Claude Code substitutes skill arguments into this file's text, so a positional parameter written here arrives as a word of the caller's arguments. An inline `awk` field reference therefore breaks. `tar -x` into a variable directory is also refused by a hook. The script has neither problem.

**Run it yourself, before you dispatch the subagent, and put the printed path in the brief.** The subagent then downloads nothing. It cannot run `add_repo` if the fetch fails, and a hook refuses it any search for the script.

**When it exits 1 with "cannot read"**. The repository is not attached to the session. Only the MAIN session can fix that, with `add_repo(owner="PazerOP", repo="claude-docs-gaps", access="read")`. A subagent must stop at once and report exactly that. Every other route gives a proxy 403 or a real 404. Trying tokens, `gh api`, proxy.pazer.ai or the tarball URL wastes the whole run.

**The source is the whole of `$D`, not `cli.js`.** From 2.1.242 the shipped binary splits its JavaScript into chunks. On those branches `cli.js` is a small import stub and the code lives in `$D/chunks/*.js`. `$D/module-graph.json` maps each `/$bunfs/root/<name>` import to its file. Search `cli.js` and `chunks/` together, every time. Branches 2.1.241 and older have no `chunks/`. There the whole bundle is in `cli.js`. The branch's own `CLAUDE.md` describes its layout. Read it first.

- **`master` IS ALMOST NEVER THE RIGHT BRANCH.** It holds the extraction tooling, not the product of the version you are running. Same for the `claude/*`, `doc-js-extraction-*` and `analysis-framework` branches. Reaching for `master` is the default mistake. Name the version explicitly.
- Use the version you are **actually running** unless the question is about a different one ("when did X change?", "does the user's older build have Y?"). Version-specific questions need fetches and a comparison. `git ls-remote --heads https://github.com/PazerOP/claude-docs-gaps '2.*'` lists every version.
- **Cheap first stop before any search**: the `docs/` directory the tarball extracted (`$D/docs`, when the branch has one), and the `docs-aggregate` branch's `INDEX.md`, hold prior investigations. If someone already wrote up the subsystem, read that instead of re-deriving it — then confirm the specific claim you care about in the source.

## 2. Search it ONLY from a Sonnet subagent -- never in main context

**This is not a preference. It never comes back out.

- **Prefer `subagent_type: "claude-code-source"`** (the `Agent` tool's `subagent_type` param) when it is offered. That registered agent already pins `model: sonnet` and preloads this skill, so there is nothing left to get wrong -- just ask it your question.
- **Only if that agent type is not available this session** (the plugin installed after the `claude` process's hook/agent registry was already resolved.
- Either way. The subagent reads the file. **you read its report**. That is the entire arrangement — its context absorbs the searching, yours receives the findings.
- Never Read a file under `/tmp/claude-docs-gaps-*` directly. Never run `rg` on it inline "just to check one thing". Spawn the agent. The one-line exception is a bare match COUNT (`rg -c pattern dir`), which returns a number rather than source.

Ask for what a source answer has to carry, or it is not worth the round trip:

> Search the whole tree at /tmp/claude-docs-gaps-2.1.283 (prettified Claude Code 2.1.283):
> cli.js AND chunks/*.js. Find how X works.
> Report: file and line number for every claim, VERBATIM quotes of the relevant code (schemas,
> string literals, defaults), the config/JSON shapes involved, and anything
> that contradicts the public docs. Label every inference as an inference.
> Do not paste large regions -- quote only what carries the answer.

## Searching it well (put this in the agent's brief)

- **Search the directory, not one file.** Run `rg -n pattern "$D" --glob '*.js'`. A search of `cli.js` alone on a chunked branch finds nothing and reads as "the source does not have it". On 2.1.283 `publishDiagnostics` is in multiple chunk files and absent from `cli.js`.
- **Follow imports through `module-graph.json`.** An import of `"/$bunfs/root/chunk-abc.js"` is the file `chunks/chunk-abc.js`.
- **Search for STRINGS, not identifiers.** Top-level names are mangled and differ between builds (`OHh`, `p7t`, `Cxt`).
- **Beware the long lines** -- and do not mistake them for broken formatting. The files ARE prettified. A formatter simply cannot break a single token. `rg -n pattern` prints the whole matched line, so pipe it (`| cut -c1-200`), prefer `rg -o` with a tight pattern, and keep `-C` small. Then read the interesting region with Read's `offset`/`limit` around the reported line.
- Zod-shaped schemas read as `v.object({...})` / `v.strictObject({...})` with `.describe(...)` on each field. That is where config contracts live. The `describe` text is often better than the published docs.
- Feature gates show up as small guard calls near a subsystem's init. Env-var names and flag strings nearby tell you how a feature is turned off.
- Cross-check a claim in a SECOND place (the schema plus its consumer) before reporting it as fact. A schema that accepts a field proves nothing about the code path honoring it — 2.1.220 accepts `transport: "socket"` for LSP servers and spawns stdio regardless.

## What this is good for

Anything the docs leave vague or unstated: exact plugin manifest schemas, hook event payloads and exit-code semantics. How diagnostics or attachments are injected, and what caps apply to them. Settings keys and their defaults, and which LSP methods the client calls. Tool descriptions and gating, telemetry names, and what a specific error message means.
