---
description: Read before answering any question about how Claude Code itself behaves -- hooks, plugins, LSP servers, settings keys, slash commands, tool schemas, env vars, permission rules, telemetry. Explains how to fetch the prettified cli.js for the RIGHT version from claude-docs-gaps and, non-negotiably, how to search it from a Sonnet or Opus subagent instead of burning main context on a 27 MB file.
---

# Reading Claude Code's own source for ground truth

Notes to self. The docs describe the product; `cli.js` **is** the product. When
the two disagree the source wins, and when the docs are silent the source is
the only answer that exists. `PazerOP/claude-docs-gaps` keeps a prettified copy
of the shipped bundle on **one branch per released version**, which is what
makes this practical.

Two rules, and the second one is the whole point of this skill.

## 1. Get the right version's cli.js into /tmp

The branch name is EXACTLY the version string. Take it from the running CLI:

```bash
V=$(claude --version | awk '{print $1}')        # e.g. 2.1.220
gh api "repos/PazerOP/claude-docs-gaps/branches/$V" --jq .name   # confirm it exists
gh api -H "Accept: application/vnd.github.raw" \
  "repos/PazerOP/claude-docs-gaps/contents/cli.js?ref=$V" > "/tmp/cli-$V.js"
```

Measured on 2.1.220: 26,975,385 bytes, 720,910 lines, ~1.2 s to download.

- **`master` IS ALMOST NEVER THE RIGHT BRANCH.** It holds the extraction
  tooling, not the product — no `cli.js` of the version you are running. Same
  for the `claude/*`, `doc-js-extraction-*` and `analysis-framework` branches.
  Reaching for `master` is the default mistake; name the version explicitly.
- Use the version you are **actually running** unless the question is about a
  different one ("when did X change?", "does the user's older build have Y?").
  Version-specific questions need two downloads and a comparison — the branch
  list (`gh api repos/PazerOP/claude-docs-gaps/branches --paginate --jq '.[].name'`)
  is the changelog you diff against.
- No branch for your exact version (a build newer than the last extraction)?
  Take the highest branch below it and SAY which version you actually read.
  Never silently answer from a different build.
- **Cheap first stop before any of this**: the `docs/` directory on a version
  branch, and the `docs-aggregate` branch's `INDEX.md`, hold prior
  investigations. If someone already wrote up the subsystem, read that instead
  of re-deriving it — then confirm the specific claim you care about in the
  source.

## 2. Search it ONLY from a Sonnet or Opus subagent -- never in main context

**This is not a preference. A 27 MB, 720k-line bundle must never be read,
`cat`ted, `head`ed, or grepped with wide context from the main conversation.**
One careless command pastes tens of thousands of tokens of minified JavaScript
into the context you need for the actual task, and it never comes back out.

- **Delegate to a subagent** (`Agent` tool) with **`model: "sonnet"` or
  `"opus"`**. Nothing smaller: the work is needle-hunting through mangled
  identifiers across hundreds of thousands of lines, and a weaker model
  reliably answers with something that sounds right and is not there.
- The subagent reads the file; **you read its report**. That is the entire
  arrangement — its context absorbs the searching, yours receives the findings.
- If you catch yourself about to Read `/tmp/cli-*.js` directly, or to run
  `rg` on it inline "just to check one thing", stop and spawn the agent. The
  one-line exception is a bare match COUNT (`rg -c pattern file`), which
  returns a number rather than source.

Ask for what a source answer has to carry, or it is not worth the round trip:

> Search /tmp/cli-2.1.220.js (prettified Claude Code 2.1.220). Find how X works.
> Report: exact line numbers, VERBATIM quotes of the relevant code (schemas,
> string literals, defaults), the config/JSON shapes involved, and anything
> that contradicts the public docs. Label every inference as an inference.
> Do not paste large regions -- quote only what carries the answer.

## Searching it well (put this in the agent's brief)

- **Search for STRINGS, not identifiers.** Top-level names are mangled and
  differ between builds (`OHh`, `p7t`, `Cxt`); string literals are stable and
  are what the product actually keys on: user-visible messages, telemetry event
  names (`tengu_*`), settings keys, `.describe("...")` text on schema fields,
  env var names, error text the user reported.
- **Beware the long lines** -- and do not mistake them for broken formatting.
  The file IS prettified; a formatter simply cannot break a single token. In
  2.1.220, 41 of the 720,910 lines exceed 5,000 characters and every one of
  them is one string or regex literal: embedded skill/prompt documents shipped
  as `\n`-escaped strings (the longest, line 658136, is 71,865 characters of
  markdown), a `\uXXXX`-escaped binary table, and the emoji regex. `rg -n
  pattern` prints the whole matched line, so pipe it (`| cut -c1-200`), prefer
  `rg -o` with a tight pattern, and keep `-C` small. Then read the interesting
  region with Read's `offset`/`limit` around the reported line.
  Corollary: when the answer you want IS one of those embedded documents,
  extract it rather than printing the line -- `awk 'NR==658136' file | cut
  -c1-4000`, or slice it with Read once you know where it starts.
- Zod-shaped schemas read as `v.object({...})` / `v.strictObject({...})` with
  `.describe(...)` on each field — that is where config contracts live, and the
  `describe` text is often better than the published docs.
- Feature gates show up as small guard calls near a subsystem's init; env-var
  names and flag strings nearby tell you how a feature is turned off.
- Cross-check a claim in a SECOND place (the schema plus its consumer) before
  reporting it as fact. A schema that accepts a field proves nothing about the
  code path honoring it — 2.1.220 accepts `transport: "socket"` for LSP servers
  and spawns stdio regardless.

## What this is good for

Anything the docs leave vague or unstated: exact plugin manifest schemas,
hook event payloads and exit-code semantics, how diagnostics or attachments are
injected and what caps apply to them, settings keys and their defaults, which
LSP methods the client actually calls, tool descriptions and gating, telemetry
names, what a specific error message means and what emits it.
