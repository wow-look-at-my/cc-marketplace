## CSS Duplication Plugin

The css-duplication plugin lives at `plugins/css-duplication/`. The **`docs` plugin's `/docs:css-cascade` skill** is the reasoning half (specificity math, inheritance, `:where()`, layers, how to hoist safely). This is the mechanical half that fires whether or not the skill was loaded. Keep the two in sync if either changes.

**LSP, not a hook. That was a deliberate replacement.** The first version was a PostToolUse hook that exited 2 with a report. It was deleted. A hook shouts once per edit whether or not the finding is still true, cannot anchor to a line. Diagnostics arrive on their own, sit on the exact selector, and *disappear when the block is hoisted*.

`.lsp.json` is the whole registration (a plugin's `.lsp.json` is auto-loaded. The manifest's `lspServers` key will do the same). An APE is neither ELF nor a `#!` script (see `tools/marketplace-build/ape_package.go`). It must be the staged file and not the raw build output. It works because plugin LSP configs substitute `CLAUDE_PLUGIN_ROOT`/`CLAUDE_PLUGIN_DATA`/`CLAUDE_PROJECT_DIR`/`${user_config.KEY}` into `command`, `args`, `env` and `workspaceFolder`.

Everything below was established by reading the shipped `cli.js` for the running version, per `/docs:claude-code-source` -- the public docs do not state any of it:

- **Push only.** Claude Code registers a `textDocument/publishDiagnostics` handler and never calls `textDocument/diagnostic`. Pull diagnostics have zero occurrences in the bundle. The server implements the pull path anyway (it is ~20 lines and makes it a correct server for other editors), but nothing in Claude Code exercises it.
- **`relatedInformation` is dropped.** The client announces support, then normalizes each diagnostic down to message/severity/range/source/code. Hence `groupMessage`. The message itself names the sibling selectors, because the structured links never reach the model. They are still emitted for real editors.
- **The budget is hard and shared**: 10 diagnostics per file, 30 overall, whole block truncated at 4000 characters, deduped against what was already delivered. `TestDiagnosticMessagesStayInsideTheInjectionBudget` pins it.
- **Diagnostics ride on an unrelated gate**. The attachment is skipped entirely unless a Bash or PowerShell tool is available in the session. A session without them silently gets no findings -- not a bug in this plugin, and not something it can detect.
- **One server per extension, first registered wins.** Installing this alongside another plugin that claims `.css` means one of them never starts. This server only reports duplication. A user who wants full CSS diagnostics has to choose -- a real tradeoff, not a gap to fix here.

Responses are now assembled by hand in `respond()` so a nil result marshals to an explicit `null`. The regression test asserts on the RAW JSON keys. Unmarshalling into a struct is exactly what hid it, because a missing key and a null both arrive as the zero value.

What counts as a finding: identical NORMALIZED bodies (whitespace collapsed, standard property names lowercased -- a `--custom-property` name stays case-sensitive, because CSS treats it that way) within the SAME at-rule context. Two copies is enough when the body has 2+ declarations. A single-declaration body needs three, because one repeated `display: none` is noise. Cross-context duplication (`@media`, `@supports`), `@keyframes` `from`/`to`, and vendor-prefixed pairs are never reported. Preprocessor sources are excluded for the same reason: under nesting, `&:hover` beneath two different parents is not a repeated rule.

- **Server**: `plugins/css-duplication/lsp.go` -- stdio JSON-RPC framing, the handshake (full-document sync. The detector needs the whole file, since a duplicate is a relationship between distant rules), didOpen/didChange/didSave/didClose, push + pull diagnostics, `groupMessage`, and selector-precise ranges
- **Parser + detector**: `plugins/css-duplication/css.go` -- comment stripping that preserves line numbers, string/`url()`-aware declaration splitting, at-rule contexts (containers recursed, `@font-face`-style declaration blocks parsed, `@keyframes` skipped), grouping and ranking
- **Entry point**: `plugins/css-duplication/main.go` -- serve stdio, nothing else
- **Tests**: `plugins/css-duplication/css_test.go` (the real stylesheet as fixture, thresholds, legitimate-duplication cases, normalization, ranking, line numbers through comments and nesting, gnarly syntax) and `plugins/css-duplication/lsp_test.go` (handshake, one diagnostic per copy with the detail said once, the injection budget, ranking, selector ranges, recompute-and-clear, pull/push agreement, didClose clearing, garbage frames, non-CSS URIs)
- **Registration**: `plugins/css-duplication/.lsp.json`
