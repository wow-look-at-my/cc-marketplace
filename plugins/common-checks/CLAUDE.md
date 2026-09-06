## Common Checks Plugin

The common-checks plugin lives at `plugins/common-checks/`. It is one **language server** plus one **PreToolUse hook**, sharing `checks.ts`. The server reports every violation that fails the org's `common-checks` CI gate, on the line it sits on, while the file is open. The hook refuses a write that introduces one.

**Both exist because a diagnostic is advice and a write is a decision.** A session put a three-line YAML comment above a trigger. That failed `yaml-comment-block` and took a pull request red. The same three lines went into a second repository an hour later. The server was adapting that exact rule the whole time. A finding nobody reads stops nothing.

The hook judges only the text a write ADDS. A violation already in the file therefore never blocks an unrelated edit to it.

**A fragment is judged where it LANDS, not on its own.** An Edit's text carries no fence, no table and no list of its own. Judged alone, an ASCII diagram inside a fenced block reads as a hand-wrapped paragraph. The wrap repair then joined its lines into one. A semicolon inside that same block read as prose and refused the write. A session answered both by deleting the code blocks out of a specification. `placement.ts` puts the fragment back first. It reads the file from disk, applies the edit, and records the line span the new text occupies. Every check then runs against the whole file, and only the findings inside that span are the write's own. The repair moves only lines in that span, so a wrap elsewhere is left for whoever edits that part.

A placement needs the file to be readable and the replaced string to appear in it exactly once. When it cannot be pinned down, the fragment is judged alone, which is what this hook always did.

**It filters nothing else.** `findings()` already returns only what fails CI, so a second list of enforced checks is a list that drifts from the first. An earlier draft kept one and left ste-lint out of it. The file documenting that choice then failed ste-lint in CI.

**A hard wrap is REPAIRED, never refused.** The wrap rule is the one check here whose repair needs no judgement. The lines to join are the ones the check itself names. The text reads the same afterwards. So `unwrapParagraphs` joins them. The hook then emits `hookSpecificOutput.updatedInput` with `suppressOutput`, which is the standing rule in this marketplace. A guard that already knows the answer must not spend a round trip asking for it. The repair reads the same stripped view the linter reads. A fence, a table, a heading and a quotation all keep their own line breaks. The JOIN lands on the raw lines, so the file keeps its own text. A `fixable` finding therefore never reaches the refusal, and never holds a file in the ledger. What the repair cannot reach still denies.

**A refusal on one write is escapable. Moving to another file leaves the finding behind.** So once a file is known bad, a write to any OTHER judged file is refused until that file is clean. `ledger.ts` holds the set, one directory per session under the temp directory.

It clears itself. Every write re-reads each recorded file from disk, and drops the ones that now pass, so the repair needs no announcement. Editing the bad file is always allowed, or nothing can ever fix it. A file that was deleted drops out too. No session id means no ledger, which is the behaviour before this existed.

Every failure path allows the write: an unparseable payload, an unjudged path, a tool that does not write. **A missing Node allows it too, in `launcher.sh`.** A non-zero exit from a PreToolUse hook blocks the tool. The server's `exit 127` there refuses every write in the session rather than none.

One bundle serves both. `server.ts` dispatches on `--hook`, so neither half can drift from the other or from CI.

It takes its shape from `css-duplication`, for the same reason. A finding that tracks the file's state disappears when the code is fixed. A hook shouts once per edit, whether or not the finding is still true.

**It differs from every other plugin here in one way, and that way decides its whole design. It contains no rules.** Every verdict comes out of the check's own module. The build fetches each module from [wow-look-at-my/actions](https://github.com/wow-look-at-my/actions).

### Why this plugin is TypeScript in a marketplace of Go binaries

Reuse of the real check code is the point. A Go reimplementation of five checkers is a second source of truth. It is correct on the day somebody writes it. It is wrong the first time a regex changes upstream. Nothing says so.

The checks are TypeScript. The server is TypeScript too. `esbuild` bundles it into one file. A `/bin/sh` launcher finds Node and starts it. The cost is a Node runtime that the other plugins do not need. The launcher states a missing Node out loud on stderr, because a language server that fails to start reports nothing either way. The only thing a message changes is whether `--debug lsp` explains the silence.

### Upstream names its own checks. Nothing here keeps a copy of that list.

`common-checks/checks.json` is the manifest, published beside the composite in [wow-look-at-my/actions](https://github.com/wow-look-at-my/actions). It names every check the composite runs and, for each, the modules that carry its rules. `.github/scripts/vendor-common-checks/` resolves the upstream branch to one commit, reads the manifest, and fetches exactly the modules it names.

A list maintained on this side describes only what upstream looked like when somebody last read it. The manifest replaced one for that reason. A rule that moves into a new module now arrives with no edit here. The `uses:` list can never show that move at all.

**Three assertions run on every build, and each catches a different drift.**

- The manifest against the composite's `uses:` list. A check the composite runs and the manifest does not name fails the build. So does a manifest entry for a check the composite dropped. Both sides are upstream, so this catches a step added without a manifest entry.
- The manifest against this plugin's adapters. The vendor step writes the parsed plan to `vendor/plan.json`, and `src/checks.test.ts` holds `ADAPTED` against it. A check whose modules are fetched and which nothing calls fails here. Vendoring alone leaves the plugin quietly enforcing four fifths of the gate while every surface reports success.
- The plan's own shape. A module path under the wrong check fails. So does an entry with no name. So does a manifest that is empty or unparseable. Each of them fails rather than vendoring a subset.

An entry may declare `modules: []` plus a `reason`. The reason is required rather than optional, because the assertion cannot tell a decision from an omission. Two checks use it, for the two different reasons a check reports nothing.

`run-once` has no rule an open file can break. It claims the workflow run for one job.

`push-excludes-tags` has a real rule, inline in a composite action, where nothing can import it. Making it importable meant converting it to a node action. That cost 642 lines against 34, on an action every repository in the org runs. 386 of those lines were a lockfile, for an eight-line rule. The gap is declared rather than paid for. Never close it by reimplementing the rule here.

The vendor step runs from the plugin's `justfile` `prebuild` recipe on every CI build. A fetch failure fails the build, which matches the `docs` plugin's Docker reference. Packaging a silently stale checker is the outcome this arrangement exists to avoid. `COMMON_CHECKS_REF` points the fetch at a branch, for a build against a check that has not merged yet.

**The `prepare` job resolves the upstream commit. That resolution is load-bearing twice over.** It runs the same manifest assertion, so a check added upstream fails the run before any plugin builds. It also feeds the plugin's cache key. Nothing under `plugins/common-checks/` changes when a rule changes upstream, so without it a cached build serves check code that CI no longer runs.

**`vendor/` is gitignored. That is the design rather than an oversight.** A copy of the checks in the tree is a second source of truth. It goes stale in silence, and nothing marks the moment it stops matching CI. It also puts prose the repository does not author in front of every check that reads the repository. The build fetches the modules and bundles them. Nothing is committed. Nothing can drift.

The whole directory is replaced on each fetch rather than merged. A file the manifest stopped naming then goes away. It does not linger as a module nothing imports and nothing refreshes.

### What `src/checks.ts` adds

It adds exactly two things that a check written for CI has no reason to produce.

A **file kind**, because a check invoked from a workflow already knows what it reads, and a server handed one open document does not. Workflow files and action manifests go to the YAML checks. Every markdown file goes to ste-lint. Nothing else is judged. Firing on every `.yaml` is how a checker earns the reputation that gets it uninstalled.

A **line** for `no-all-builds-job`, whose CI form names the job and never the line. A CI annotation only ever had a file to attach to. The verdict there is still upstream's. Only the cursor position is worked out locally.

### Ranking, and the client's budget

Ranking decides what the model actually sees. The client injects only the first handful of diagnostics per file. ste-lint reports hundreds of findings on one document, and `no-all-builds-job` reports one. So the order puts the structural checks first and ste-lint last.

Two more things follow from the same budget. A hard-wrapped paragraph reports ONE finding at its first continuation line, rather than one per line. That is the same information. It leaves room for everything else. A file with more findings than the cap says so on the last diagnostic it sends. Dropping the tail in silence reads as a claim that the list is complete.

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
- **`command` and `extensionToLanguage` are the two required keys**, and every extension must start with a dot. An entry missing either is dropped with an error rather than started. `diagnostics` defaults to true. That default is what pushes `publishDiagnostics` into the agent's context after an edit. This plugin leaves it alone.
- **Three gates stop a server before it starts.** The plugin must be enabled. A `--plugin-dir` plugin counts as enabled unless somebody disables it. Safe mode disables `lspServers` outright. Bare mode and simple mode disable it unless a caller explicitly requests it. There is no print-mode gate in 2.1.241.
- A session without one gets no findings, silently. This plugin cannot detect that.
- **One server per extension, and the first registered wins**, with a warning that names the loser. This plugin claims `.yml`, `.yaml` and `.md`. That is a wide claim. Another plugin that wants full YAML or markdown diagnostics cannot run beside it. That is a real tradeoff, not a gap to fix here.

**Live verification did not complete. The control was a plugin whose entire LSP command appends one line to a file. It never ran either, on a real `Edit` to a file with its registered extension. The diagnostics log carried `load_plugin_hooks_completed` and no LSP event of any kind.

The layer below that IS verified. A real LSP client drove the bundled `build/server.cjs` over stdio. It answered `initialize`, `didOpen` and `shutdown` correctly, and published all four workflow findings on the right lines. Run the interactive check before you trust the registration.

### Files

- **Adapters**: `plugins/common-checks/src/checks.ts` -- file kind, the per-check calls into `vendor/`, the `no-all-builds-job` anchor, the ste-lint bucket wording, and the ranking. Also the wrapped-paragraph collapse, the `fixable` tag, and `unwrapParagraphs`
- **Hook**: `plugins/common-checks/src/hook.ts` -- the PreToolUse payload, the units a write adds, `repairInput`, the ledger sweep, and the two output shapes
- **Placement**: `plugins/common-checks/src/placement.ts` -- `place` pins an edit to its line span in the file on disk, and `repairWithin` joins only the wraps inside that span
- **Tests**: `src/hook.test.ts` covers the refusal and every fail-open path. Also the wrap repair on each write shape, a fence keeping its line breaks, and a wrap beside a real violation. Then the placement cases, against a real file on disk. An edit inside a fence keeps its line breaks. The same edit is not refused for its punctuation. The control edit outside a fence is still joined
- Document sync is full, because a finding is a property of the whole document
- **Entry point**: `plugins/common-checks/src/server.ts` -- serve stdio, nothing else
- **Launcher**: `plugins/common-checks/launcher.sh` -- staged into `server/` as `common-checks-lsp`. The client execve()s the path in `.lsp.json`, and a bundled `.js` file is not executable on its own. The directory is `server/` and not `build/` because `release-plugin` requires every file under `build/` to be a fat APE, and this plugin ships no Go
- **Fetching**: `.github/scripts/vendor-common-checks/plan.ts` holds the manifest parser, the drift assertion and the provenance header. `main.ts` holds the network and disk half, plus `--commit`. The GitHub client is shared with the `docs` plugin's fetcher rather than written twice
- **Tests**: `src/checks.test.ts` fires each check on the right line, with a clean control beside it. It also covers the file-kind boundaries, the ranking, the wrapped-paragraph collapse, and a heuristic-only finding staying unreported
- **Tests**: `src/lsp.test.ts` covers the handshake, publish and clear, pull and push agreeing, and the cap's overflow note. It also covers path resolution with and without a root, and the framing edge cases. It asserts the explicit `null` shutdown result on the RAW JSON keys
- **Tests**: `vendor.test.ts` drives a fake client that never touches the network. A check added upstream and a check dropped upstream each fail the build by name. A module added to the manifest is fetched with no edit here, and every malformed manifest is rejected rather than vendoring a subset
- **Registration**: `plugins/common-checks/.lsp.json`

Adding a check upstream is meant to break this build. The failure names the check and the ways out. List its modules in `common-checks/checks.json` and write the adapter here, or give the entry no modules and say why no open file can violate it. Do not silence it by deleting an assertion. The assertions are the feature.
