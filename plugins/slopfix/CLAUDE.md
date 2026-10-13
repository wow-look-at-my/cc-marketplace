## Slopfix Plugin

The slopfix plugin lives at `plugins/slopfix/`. It carries **no rule of its own**. Every verdict comes out of [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). This directory is the manifest and nothing else.

### How a check is wired

A manifest `command` is a shell command with `${CLAUDE_PLUGIN_ROOT}` substituted. It runs `scripts/launch slopfix`, which picks a slopfix binary and execs it with the remaining arguments. The launcher holds no rule. `--only` takes a comma-separated list, so every write check runs in one process rather than one process each.

Every event execs `scripts/launch slopfix hook`. The binary reads the event off the payload and runs each guard that serves it, so the manifest only decides which events reach slopfix. What each guard DOES is slopfix's own documentation. Read `slopfix hook --help`, or that repository. A table here is a copy that drifts.

These replace the plugins. Those are `ask-properly`, `claude-md-budget`, `cleanup-bash-cmds`, `common-checks`, `detect-permission-seeking` and `enhanced-auto-allow`. They are also `link-all-refs`, `no-blame-language`, `no-busy-poll`, `no-counts-in-docs`, `no-tombstones`, `no-work-loss`, `recommend-go-toolchain` and `rustfmt-enforce`. Each of those directories is gone. A rule with homes drifts. The second copy is the one nobody updates.

**There are no `.go` files here, and there must not be.** A manifest that names a subcommand needs no Go. The `plugin-e2e` workflow checks each subcommand the manifest names against slopfix's source.


### The language server half

`.lsp.json` runs `scripts/launch slopfix lsp`. The server publishes, for each open file a build reads. The findings `slopfix check` gives it, on the lines they sit on. The PreToolUse `hook` judges the write itself. The two are one binary, so neither can drift from the other or from CI.

Scope, ranking, the per-file cap and the diagnostic text are all decided in slopfix's `langserver` package. A file outside every work tree, or under `$HOME/.claude`, gets nothing, because no build reads it. `--max-per-file` sets the cap.

It takes its shape from `css-duplication`. A finding that tracks the file's state disappears when the code is fixed. A hook shouts once per edit, whether or not the finding is still true.

**It differs from every other plugin here in one way. That way decides its whole design. It contains no rules.** Every verdict comes out of [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). That binary owns every prose and YAML rule the org applies. The plugin ships no copy of it.

### common-checks is slopfix's default rule set

That sentence is the whole architecture. It is the owner's ruling. The org's `common-checks` action is a name every repository already calls. Its steps forward to slopfix. Running slopfix with no `--only` IS common-checks. This plugin runs exactly that.

Everything that kept a hand-maintained list in step with upstream is deleted. That was a `checks.json` manifest parser. It was also a vendored copy of each check's TypeScript modules. It was also an `ADAPTED` coverage list, and the build-time drift assertions holding them against each other. All of it guarded one failure: this plugin's list of checks falling out of step with the gate's. A plugin that runs the binary's default set cannot fall out of step. There is nothing left to assert. Do not reintroduce any of it.

`push-excludes-tags` is the gap. It is a stated one. Its rule is an inline script inside a composite action. Nothing imports it and slopfix does not carry it. No diagnostic here reports it. Never close that by writing the rule in this plugin.

### The plugin ships no binary. The launcher picks one.

A copy inside the plugin goes stale in the user's plugin cache. A fix merged to slopfix master then stays refused by the installed hook until the plugin itself rebuilds and updates. So the build vendors nothing, and the user installs nothing.

`scripts/launch slopfix` runs the slopfix on PATH when its `version` is at least the release buildhost publishes. Otherwise it downloads that release and runs the download. A slopfix that prints no release number counts as older. [CREATING_PLUGINS.md](../CREATING_PLUGINS.md#a-plugin-ships-no-binary) describes the launcher every plugin shares.

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

- **Registration**: `plugins/slopfix/.lsp.json` runs `scripts/launch slopfix lsp`
- **Hooks**: `plugins/slopfix/.claude-plugin/plugin.json`
- **Launcher**: `plugins/slopfix/scripts/launch`, a link to `tools/plugin-launch/launch`

Adding a rule to slopfix needs no edit here. That is the point of the arrangement. The plugin runs the default set, so a new rule arrives once buildhost publishes it.
