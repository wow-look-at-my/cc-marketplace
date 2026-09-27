## Slopfix Plugin

The slopfix plugin lives at `plugins/slopfix/`. It carries **no rule of its own**. Every verdict comes out of [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). This directory is the manifest and nothing else.

### How a check is wired

A manifest `command` is a shell command with `${CLAUDE_PLUGIN_ROOT}` substituted. It runs the binary directly. **There is no `hooks/` directory and no shell script of any kind.** `--only` takes a comma-separated list, so every write check runs in one process rather than one process each.

What each subcommand DOES is slopfix's own documentation. Read `slopfix --help`, or that repository. A table here is a copy that drifts. The copy nobody updates is this one. The manifest is the list of which events reach which subcommand. It is the only such list.

These replace the plugins. Those are ask-properly, claude-md-budget, cleanup-bash-cmds, common-checks, detect-permission-seeking and enhanced-auto-allow. They are also link-all-refs, no-blame-language, no-busy-poll, no-counts-in-docs, no-tombstones, no-work-loss and recommend-go-toolchain. Each of those directories is gone. A rule with homes drifts. The second copy is the nobody updates.

**There are no `.go` files here, and there must not be.** A manifest that names a subcommand needs no Go. The `plugin-e2e` workflow drives `clean-bash` end to end. That is the only place the manifest and the binary are exercised together.

### The language server half

`.lsp.json` runs `bin/slopfix.ape lsp`. The server publishes, for each open file a build reads. The findings `slopfix report` gives it, on the lines they sit on. The PreToolUse `hook` subcommand judges the write itself. The two are one binary, so neither can drift from the other or from CI.

Scope, ranking, the per-file cap and the diagnostic text are all decided in slopfix's `langserver` package. A file outside every work tree, or under `$HOME/.claude`, gets nothing, because no build reads it. `--max-per-file` sets the cap.

It takes its shape from `css-duplication`. A finding that tracks the file's state disappears when the code is fixed. A hook shouts once per edit, whether or not the finding is still true.

**It differs from every other plugin here in one way. That way decides its whole design. It contains no rules.** Every verdict comes out of [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). That binary owns every prose and YAML rule the org applies. The build fetches it and the plugin ships it.

### common-checks is slopfix's default rule set

That sentence is the whole architecture. It is the owner's ruling. The org's `common-checks` action is a name every repository already calls. Its steps forward to slopfix. Running slopfix with no `--only` IS common-checks. This plugin runs exactly that.

Everything that kept a hand-maintained list in step with upstream is deleted. That was a `checks.json` manifest parser. It was also a vendored copy of each check's TypeScript modules. It was also an `ADAPTED` coverage list, and the build-time drift assertions holding them against each other. All of it guarded one failure: this plugin's list of checks falling out of step with the gate's. A plugin that runs the binary's default set cannot fall out of step. There is nothing left to assert. Do not reintroduce any of it.

`push-excludes-tags` is the gap. It is a stated one. Its rule is an inline script inside a composite action. Nothing imports it and slopfix does not carry it. No diagnostic here reports it. Never close that by writing the rule in this plugin.

### The plugin SHIPS the binary. It never looks on PATH.

`bin/` carries slopfix itself, fetched by `.github/scripts/vendor-slopfix.sh` at build time. The sibling plugins settled this shape and their CLAUDE.md files record why. A plugin that names its binary as a bare word travels on a separate track from that binary. One calling a subcommand its installed binary predates then reports nothing at all. Swallowing that failure is worse. It leaves a guard that installs, reports success and does nothing.

**The fetch is a GATE, not a download.** It checks the APE prologue first. Then it runs `report` on a workflow and on a document built to violate a rule. Each must come back with a verdict. Exit status alone proves only that the subcommand parses.

`report` doubles as the proof that this is a post-rename build. buildhost still serves the old `slopfmt` project name. A fetch of that name succeeds and hands back a binary frozen before the rename. That build has no `report` at all. It cannot pass this gate quietly. `SLOPFIX_URL` points the fetch elsewhere, for a build against a slopfix that has not published.

The same script serves `no-counts-in-docs` and `no-tombstones`. Each names the rules it drives through the `hook` contract. Each rule gets the same treatment. The script runs it on text built to violate the rule and requires a verdict.

**The `prepare` job puts buildhost's published slopfix version into the cache key.** Nothing under these plugins' own directories changes when a rule changes in slopfix. Without it a cached build serves a checker that CI no longer runs.

**A slopfix publish ships here without a push to this repository.** slopfix's own CI dispatches `release.yml` with `publish: true` after each master publish. The new version then misses the cache key, so only this plugin rebuilds. The dispatch token is `CC_MARKETPLACE_DISPATCH_TOKEN` in secret-server, which slopfix's workflow reads.

### Ranking, and the client's budget

Ranking decides what the model actually sees. The client injects only the first handful of diagnostics per file. ste-lint reports hundreds of findings on one document, and `no-all-builds-job` reports one. So slopfix's server puts the YAML rules first and the wrap rule last.

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

The layer below that IS verified. slopfix's `langserver` tests drive the server over a pipe with a real JSON-RPC client. They cover `initialize`, publish on open and change, the cleared list on close, pull diagnostics, and the `null` shutdown result. Run the interactive check before you trust the registration.

### Files

- **Registration**: `plugins/slopfix/.lsp.json` runs `bin/slopfix.ape lsp`
- **Hooks**: `plugins/slopfix/.claude-plugin/plugin.json`
- **Fetching**: `.github/scripts/vendor-slopfix.sh` -- the download, the APE check, the `report` probes, and the per-rule `hook` probes the sibling plugins name. It also requires this binary to define every subcommand `plugin.json` and `.lsp.json` name. That is the check the `plugin-e2e` job cannot make. The job asks slopfix's SOURCE at master, and master is not the build being packaged. `laziness` and `blame-language` passed it while the fetched binary defined neither. Every Stop and every message then answered `unknown command`, and those guards ran nowhere. `SLOPFIX_URL` is how a manifest lands ahead of a publish

Adding a rule to slopfix needs no edit here. That is the point of the arrangement. The plugin runs the default set, so a new rule arrives with the next build's fetch.
