## Slopfix Plugin

The slopfix plugin lives at `plugins/slopfix/`. It carries **no rule of its own**. Every verdict comes out of [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). This directory is the manifest and nothing else.

### The checks, and where each is named

A manifest `command` is a shell command with `${CLAUDE_PLUGIN_ROOT}` substituted. It runs the binary directly. **There is no `hooks/` directory and no shell script of any kind.** `--only` takes a comma-separated list, so every write check runs in one process rather than one process each.

| Subcommand | Event | What it does |
| --- | --- | --- |
| `hook --only counts,tombstones` | PreToolUse | Cuts a cardinal out of a document, and strips a tombstone or refuses what it cannot excise |
| `md-budget` | SessionStart, PostToolUse, Stop | Reports an instruction file over budget |
| `no-work-loss` | PreToolUse | Refuses a loss, and a write that skips the edit tools |
| `auto-allow` | PermissionRequest, PreToolUse | Approves read-only work, refuses a banned program |
| `clean-bash` | PreToolUse | Rewrites a Bash command instead of refusing it |
| `busy-poll` | Stop, PreToolUse | Refuses a repeat that cannot learn anything |
| `link-refs` | MessageDisplay | Renders a reference as a markdown link |
| `ask-properly` | MessageDisplay | Marks a decision put to the reader in prose |
| `laziness` | Stop | Refuses a turn that reports a defect it left alone |
| `blame-language` | MessageDisplay | Marks deflecting language |

`laziness` and `blame-language` need the message assembled before a rule reads it. slopfix does that itself. The Stop payload does not always carry `last_assistant_message`, so `laziness` falls back to the transcript. A MessageDisplay payload arrives as deltas. So `blame-language` accumulates them and judges the message whole, the way `link-refs` carries its fence state.

These replace the plugins that used to hold the same rules. Those are ask-properly, claude-md-budget, cleanup-bash-cmds, common-checks, detect-permission-seeking, enhanced-auto-allow, link-all-refs, no-blame-language, no-busy-poll, no-counts-in-docs, no-tombstones, no-work-loss and recommend-go-toolchain. Each of those directories is gone. A rule with two homes drifts, and the second copy is the one nobody updates.

**There are no `.go` files here, and there must not be.** A manifest that names a subcommand needs no Go. The `plugin-e2e` workflow drives `clean-bash` end to end. That is the only place the manifest and the binary are exercised together.

### The language server half

The server is one **language server** plus one **PreToolUse hook**, sharing `checks.ts`. The server reports every violation that fails the org's `common-checks` CI gate, on the line it sits on, while the file is open. The hook refuses a write that introduces one.

**Both exist because a diagnostic is advice and a write is a decision.** A session put a three-line YAML comment above a trigger. That failed `yaml-comment-block` and took a pull request red. The same three lines went into a second repository an hour later. The server was adapting that exact rule the whole time. A finding nobody reads stops nothing.

The hook judges only the text a write ADDS. A violation already in the file therefore never blocks an unrelated edit to it.

**Scope is the repository.** It is decided before any check runs. Writing a plan under `~/.claude` was refused with a wall of ste-lint findings. The message claimed the write fails CI. That claim was false. A plan sits in no repository. It never ships, and no build reads it. The hard-wrap rule is wrong there for a second reason. A plan is read in a terminal pane, where one-line paragraphs read worse. So `scope.ts` walks up from the file looking for a `.git` entry. A worktree and a submodule carry a `.git` FILE rather than a directory, so it asks only whether the name exists. A path with none in any ancestor is judged by nothing at all. `$HOME/.claude` is excluded on top of that, because a versioned dotfiles tree puts the user's own configuration inside a real work tree. That exclusion matches the resolved home prefix and nothing wider. A repository that keeps a `.claude/` of its own is a directory CI really does read. Both callers ask the ABSOLUTE path. The hook asks before it makes the path relative to the working directory, and the server asks the URI's own path. A relative path cannot be walked upward. The answer there is the working directory's rather than the file's. Every failure answers out of scope, which allows the write and publishes nothing. The ledger drops an out-of-scope entry for the same reason. Nothing re-checks such a file, so an entry naming one can never clear on its own. What survives is the refusal's own sentence. It claims the write fails CI. That claim is now only ever made about a file inside a work tree.

**A fragment is judged where it LANDS, not on its own.** An Edit's text carries no fence, no table and no list of its own. Judged alone, a semicolon inside a fenced block reads as prose and refuses the write. The lines of an ASCII diagram in that same block read as a hand-wrapped paragraph. A session answered both by deleting the code blocks out of a specification. `placement.ts` puts the fragment back first. It reads the file from disk, applies the edit, and records the line span the new text occupies. Every check then runs against the whole file, and only the findings inside that span are the write's own.

A placement needs the file to be readable and the replaced string to appear in it exactly once. When it cannot be pinned down, the fragment is judged alone, which is what this hook always did.

**The span alone is not enough, and the gap wedged a whole session.** An edit anchors on text the file already has. A `new_string` that repeats any of it puts those lines inside the span. A sentence the write never touched is then reported as its own. That refusal records the file, and the record never clears. Every later write in the session is then refused against a file nothing can repair. So `findingsFor` subtracts what the file carried BEFORE the edit. A finding is matched by check and message rather than by line, because every line below an edit moves. Each pre-edit finding cancels one match. A second copy of a sentence the file already breaks is still the write's own.

**That subtraction was documented here and never wired up.** `Placement` declared a `before` field for it. `place` never filled the field in, so the subtraction read an empty document and cancelled nothing. It is a real fix rather than a described one now, and `hook.test.ts` pins both halves. The cost is one more slopfix run per edit unit.

One consequence is deliberate and worth stating. Rewording a paragraph the file already hard-wrapped, and leaving it wrapped, is no longer refused. The write did not make the file worse. Adding a wrapped paragraph to a clean file still is.

**It filters nothing else.** `findings()` already returns only what fails CI. A second list of enforced checks is a list that drifts from the first. An earlier draft kept one and left ste-lint out of it. The file documenting that choice then failed ste-lint in CI.

**A hard wrap is refused, like every other finding.** The hook once repaired it instead. It joined the lines and let the write through on `updatedInput`. That was reverted at the operator's request. A different tool is being built for the job. Placement is what survives from that change. Judging a fragment in its own file is what stops a fenced block reading as prose. That half was never about the repair.

**A refusal on one write is escapable. Moving to another file leaves the finding behind.** So a write to any OTHER judged file is refused once a file is known bad. It stays refused until that file is clean. `ledger.ts` holds the set, one directory per session under the temp directory.

**That block stops at the work tree's edge. It did not before.** The ledger is keyed by session and holds absolute paths. A session here normally holds several checkouts. So an entry recorded while working in one repository refused every write in an unrelated one. A sibling agent was blocked from editing this repository by a file in another. `sweep` now takes the write's own work tree and skips every entry outside it. `scope.ts` exports `workTree` for both callers. The two cannot then disagree about which project a path belongs to. A write with no path, or one outside every work tree, has no project and blocks on nothing.

It clears itself. Every write re-reads each recorded file from disk and drops the ones whose finding is gone. The repair therefore needs no announcement. Editing the bad file is always allowed, or nothing can ever fix it. A file that was deleted drops out too. No session id means no ledger, which is the behaviour before this existed.

**An entry names the FINDINGS, never the file alone.** The earlier sweep asked whether the whole file passed. A file carries findings no write here introduced, and a hard-wrapped document carries one per paragraph. The entry then never cleared. Every later write in the session was refused against a file nothing was able to clean. That is a wedge whose only way out is deleting the entry by hand, which is what happened. So `record` stores each finding's identity, and `sweep` drops the entry once none of them is on disk any more. A different finding the file already had holds nothing.

Every failure path allows the write: an unparseable payload, an unjudged path, a tool that does not write. **A missing Node allows it too, in `launcher.sh`.** A non-zero exit from a PreToolUse hook blocks the tool. The server's `exit 127` there refuses every write in the session rather than none.

**An entry recorded before the slopfix migration clears itself.** `sweep` drops an entry once none of its recorded findings is on disk any more. An entry made under the old vendored checks carries their wording. Nothing in a slopfix finding matches that wording. So the entry reads as repaired and goes, rather than wedging the session against a file nothing can clean.

One bundle serves both. `server.ts` dispatches on `--hook`, so neither half can drift from the other or from CI.

It takes its shape from `css-duplication`, for the same reason. A finding that tracks the file's state disappears when the code is fixed. A hook shouts once per edit, whether or not the finding is still true.

**It differs from every other plugin here in one way. That way decides its whole design. It contains no rules.** Every verdict comes out of [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). That binary owns every prose and YAML rule the org applies. The build fetches it and the plugin ships it.

### common-checks is slopfix's default rule set

That sentence is the whole architecture. It is the owner's ruling. The org's `common-checks` action is a name every repository already calls. Its steps forward to slopfix. Running slopfix with no `--only` IS common-checks. This plugin runs exactly that.

Everything that kept a hand-maintained list in step with upstream is deleted. That was a `checks.json` manifest parser. It was also a vendored copy of each check's TypeScript modules. It was also an `ADAPTED` coverage list, and the build-time drift assertions holding them against each other. All of it guarded one failure: this plugin's list of checks falling out of step with the gate's. A plugin that runs the binary's default set cannot fall out of step. There is nothing left to assert. Do not reintroduce any of it.

`push-excludes-tags` is the one gap. It is a stated one. Its rule is an inline script inside a composite action. Nothing imports it and slopfix does not carry it. No diagnostic here reports it. Never close that by writing the rule in TypeScript.

### Why this plugin is TypeScript in a marketplace of Go binaries

The server is TypeScript, and `esbuild` bundles it into one file. A `/bin/sh` launcher finds Node and starts it. The cost is a Node runtime that the other plugins do not need. The launcher states a missing Node out loud on stderr. A language server that fails to start reports nothing either way.

The rules are Go now. The plugin reaches them by running the binary rather than by importing anything.

### The plugin SHIPS the binary. It never looks on PATH.

`build/` carries slopfix itself, fetched by `.github/scripts/vendor-slopfix.sh` at build time. The sibling plugins settled this shape and their CLAUDE.md files record why. A plugin that names its binary as a bare word travels on a separate track from that binary. One calling a subcommand its installed binary predates then reports nothing at all. Swallowing that failure is worse. It leaves a guard that installs, reports success and does nothing.

**The fetch is a GATE, not a download.** It checks the APE prologue first. Then it runs `report` on a workflow and on a document built to violate a rule. Each must come back with a verdict. Exit status alone proves only that the subcommand parses.

`report` doubles as the proof that this is a post-rename build. buildhost still serves the old `slopfmt` project name. A fetch of that name succeeds and hands back a binary frozen before the rename. That build has no `report` at all. It cannot pass this gate quietly. `SLOPFIX_URL` points the fetch elsewhere, for a build against a slopfix that has not published.

The same script serves `no-counts-in-docs` and `no-tombstones`. Each names the rules it drives through the `hook` contract. Each rule gets the same treatment. The script runs it on text built to violate the rule and requires a verdict.

**The `prepare` job resolves slopfix's head commit into the cache key.** Nothing under these plugins' own directories changes when a rule changes in slopfix. Without it a cached build serves a checker that CI no longer runs.

### What `src/checks.ts` adds

It adds what slopfix does not answer, and nothing more.

A **file kind**. slopfix decides which rules read a path too. It has to: a server handed one open buffer knows nothing else about it. What slopfix does not answer is whether the file is one the gate reads at ALL. Its `check` command judges any file it is named as prose, because naming it is the request. The gate instead walks a tree. That walk selects workflow files, action manifests and markdown. `fileKind` mirrors the walk. Firing on every `.yaml` and every `.go` is how a checker earns the reputation that gets it uninstalled.

A **ranking**, for the client's diagnostic budget. slopfix sorts by line, which is right for a report a person reads top to bottom. It is wrong for a channel that carries only the first handful. `FAMILY_ORDER` puts the YAML rules first and the wrap rule last. A structural finding is then never crowded out by a voluminous one.

### The bridge, and what a missing binary means

`src/slopfix.ts` finds the binary. It runs `report --path <p>` with the content on stdin, and reads the findings back. It THROWS when the binary is absent or cannot answer. It never returns an empty list there. An empty list reads exactly like a clean file.

Each caller then decides, and only one of them swallows it.

`scan-file.ts` and every CI-facing path fail hard. The failure this replaced was a run that printed `no slopfix binary` and then `clean`, and exited zero. Those two lines contradict each other. A caller reading the exit code was told the file passed.

The language server logs the failure and keeps serving. A server that dies on one document stops answering for every other one.

The PreToolUse hook is the one place that still allows the write. A non-zero exit from a PreToolUse hook blocks the tool. Failing hard there refuses every write in the session rather than none. It says so loudly on stderr instead.

### Ranking, and the client's budget

Ranking decides what the model actually sees. The client injects only the first handful of diagnostics per file. ste-lint reports hundreds of findings on one document, and `no-all-builds-job` reports one. So the order puts the structural checks first and ste-lint last.

More follows from the same budget. slopfix reports a hard-wrapped paragraph ONCE, where it starts, rather than one finding per line. That is the same information. It leaves room for everything else. A file with more findings than the cap says so on the last diagnostic it sends. Dropping the tail in silence reads as a claim that the list is complete.

### Every diagnostic is severity Error

The heuristic findings are not published at all. ste-lint's warn buckets are passive voice, noun clusters, complex tense, dictionary word choice and long paragraphs. None of them fails CI, by their author's own deliberate design. A diagnostic for one spends the budget on something the gate does not care about. What this plugin publishes means one thing: this fails the merge gate.

### The message carries the repair, because nothing else can

Claude Code cannot accept a fix from a language server. In 2.1.241 there is no `codeAction` client capability. `textDocument/codeAction` has zero occurrences in the whole bundle, and so does `workspace/applyEdit`. There is no formatting or `willSaveWaitUntil` path either.

The diagnostic is the only channel. It is narrow. Only message, severity, line and character, code and source survive into the model's context. The client announces support for `relatedInformation`, `tagSupport` and `codeDescriptionSupport`, then discards all three in the mapper.

So a rule whose repair is one word says that word in the message. A contraction names its expansion. A banned modal names the approved word, and keeps a leading capital so the replacement drops straight in. A semicolon and a comma splice each name the punctuation to write.

A sentence over the cap gets no such text. It has no single replacement. Inventing one puts a guess in a message the reader trusts.

### The registration, and what was checked rather than assumed

These facts were read out of the shipped bundle, per `/docs:claude-code-source`. The highest version carrying an extractable `cli.js` is 2.1.241. From 2.1.242 the npm package ships a native binary and `cli.js` is a stub. So these are 2.1.241's rules. A later change does not show up here.

- **`.lsp.json` at the plugin root is auto-discovered.** The manifest's `lspServers` key is read separately and merged into the same map. Either one works, and both together merge by server name. This plugin ships only the file, matching `css-duplication`.
- **`command` and `extensionToLanguage` are both required**, and every extension must start with a dot. An entry missing either is dropped with an error rather than started. `diagnostics` defaults to true. That default is what pushes `publishDiagnostics` into the agent's context after an edit. This plugin leaves it alone.
- **Three gates stop a server before it starts.** The plugin must be enabled. A `--plugin-dir` plugin counts as enabled unless somebody disables it. Safe mode disables `lspServers` outright. Bare mode and simple mode disable it unless a caller explicitly requests it. There is no print-mode gate in 2.1.241.
- A session without one gets no findings, silently. This plugin cannot detect that.
- **One server per extension, and the first registered wins**, with a warning that names the loser. This plugin claims `.yml`, `.yaml` and `.md`. That is a wide claim. Another plugin that wants full YAML or markdown diagnostics cannot run beside it. That is a real tradeoff, not a gap to fix here.

**Live verification did not complete. The control was a plugin whose entire LSP command appends one line to a file. It never ran either, on a real `Edit` to a file with its registered extension. The diagnostics log carried `load_plugin_hooks_completed` and no LSP event of any kind.

The layer below that IS verified. A real LSP client drove the bundled `build/server.cjs` over stdio. It answered `initialize`, `didOpen` and `shutdown` correctly, and published every workflow finding on the right line. Run the interactive check before you trust the registration.

### Files

- **Adapter**: `plugins/slopfix/src/checks.ts` -- the file kind, the family ranking, and the sentence a diagnostic carries. Nothing else
- **Bridge**: `plugins/slopfix/src/slopfix.ts` -- finding the shipped binary, running `report`, and `SlopfixUnavailable`
- **Hook**: `plugins/slopfix/src/hook.ts` -- the PreToolUse payload, the units a write adds, the ledger sweep, and the refusal
- **Scope**: `plugins/slopfix/src/scope.ts` -- `inScope` walks for a `.git` entry, excludes `$HOME/.claude`, and answers out of scope on every failure. `workTree` returns the root it found, which is what keeps one project's ledger out of another's. The hook and the server share both
- **Placement**: `plugins/slopfix/src/placement.ts` -- `place` pins an edit to its line span in the file on disk
- **Local scan**: `plugins/slopfix/src/scan-repo.ts` -- `npx tsx plugins/slopfix/src/scan-repo.ts .` reports every hard ste-lint finding in the repository. CI lints only what a push changed, so a file nothing touches keeps its findings until somebody edits it. This finds them first
- **Tests**: `src/hook.test.ts` covers the refusal and every fail-open path. Then the placement cases, against a real file on disk. An edit inside a fence is not refused for its line breaks. The same edit is not refused for its punctuation. The control edit outside a fence is still refused for its wrap. The scope cases run against real directories on disk. A hard-wrapped document under a directory with no `.git` is allowed. The same bytes one `.git` away are still refused, which is the control that proves the case can fail. A plan under `$HOME/.claude` is allowed while `$HOME` itself is a work tree
- Document sync is full, because a finding is a property of the whole document
- **Entry point**: `plugins/slopfix/src/server.ts` -- serve stdio, nothing else
- **Launcher**: `plugins/slopfix/launcher.sh` -- staged into `server/` as `slopfix-lsp`. The client execve()s the path in `.lsp.json`, and a bundled `.js` file is not executable on its own. The directory is `server/` and not `build/` because `release-plugin` requires every file under `build/` to be a fat APE, and this plugin ships no Go
- **Fetching**: `.github/scripts/vendor-slopfix.sh` -- the download, the APE check, the `report` probes, and the per-rule `hook` probes the sibling plugins name
- **Tests**: `src/checks.test.ts` fires each rule on the right line, with a clean control beside it. It also covers the file-kind boundaries, the ranking, and a heuristic-only finding staying unreported. Every case drives the real binary. A fake here is the second source of truth this plugin exists to avoid
- **Tests**: `src/slopfix.test.ts` covers the missing binary. `report` and `findings` both throw, and `scan-file` exits non-zero without printing `clean`. The control proves the same command reports normally when the binary is there
- **Tests**: `src/ledger.test.ts` covers the cross-checkout case. An entry in another work tree blocks nothing, and the same entry in this one still blocks
- **Tests**: `src/lsp.test.ts` covers the handshake, publish and clear, pull and push agreeing, and the cap's overflow note. It also covers path resolution with and without a root, and the framing edge cases. It asserts the explicit `null` shutdown result on the RAW JSON keys
- **Registration**: `plugins/slopfix/.lsp.json`

Adding a rule to slopfix needs no edit here. That is the point of the arrangement. The plugin runs the default set, so a new rule arrives with the next build's fetch.
