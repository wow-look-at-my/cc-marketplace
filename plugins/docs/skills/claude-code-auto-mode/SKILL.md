---
name: claude-code-auto-mode
description: How auto mode and the autoMode classifier resolve here - the levers, the $defaults rule, why bypassPermissions is inert on the web. Read before editing permissions or autoMode settings.
user-invocable: false
---

# `defaultMode: "auto"`

Sessions launch in **auto mode**, so the `autoMode` section below is the live guardrail, not a fallback. Facts, from the CLI's mode resolver (`tBc`):

- **`bypassPermissions` in settings is inert on the web. That is what used to knock sessions off auto.** With `CLAUDE_CODE_REMOTE` set, the resolver accepts only `acceptEdits`, `plan`, `default`, and `auto` from `permissions.defaultMode`. Any other value is dropped with a warning (`tengu_ccr_unsupported_default_mode_ignored`) and the session falls through to plain `default` -- ask-on-everything. The old `"bypassPermissions"` value here never took effect at all.
- **Auto is only honored from a trusted scope.** `defaultMode: "auto"` counts only when policy, **user**, or flag settings declare it -- project and local settings are repo-controllable and ignored. `setup/settings.sh` merges into `~/.claude/settings.json` (user scope). As a result, it takes effect.
- **Precedence** in the resolver: CLI `--dangerously-skip-permissions` / `--permission-mode`, then agent frontmatter, then `permissions.defaultMode`. The web launcher passes neither flag (only `--settings ~/.claude/launcher-settings.json`, which sets no mode), so this setting wins.
- **Gates that can still force `default`:** the server-side auto-mode circuit breaker (`tengu_auto_mode_config.enabled == "disabled"`) and `disableAutoMode: "disable"` (top level or under `permissions`, neither set here). A model gate can force it too. That gate excludes `claude-3-*`, opus-4-0/4-1/4-5, sonnet-4-0/4-5, and haiku-4-5.
- `skipDangerousModePermissionPrompt: true` (top level, **not** under `permissions`) stays. It records that the bypass-mode dialog was accepted. Harmless under auto, and it prevents a **blocking interactive confirmation** nothing can click if a session is ever launched with the CLI bypass flag. Note auto mode leaves `isBypassPermissionsModeAvailable` false -- `(mode === "bypassPermissions" || cliFlag) && !featureGate && !disableBypassPermissionsMode` -- so a session cannot switch itself to bypass.

**Deny and ask rules fire in every mode.** In the CLI's tool-permission flow `permissions.deny` returns at the top and `permissions.ask` right after it, ahead of both the classifier and the bypass branch. So the `mcp__github__*` deny entries in `setup/config_template.json` (the merge, push, pull-request and issue write tools) and the `mcp__GitHub_API_MCP__*` / `gh` comment-review rules hold regardless of mode. What auto adds back, versus bypass, is the LLM classifier on everything not explicitly allowed or denied -- including Bash, which bypass ran unprompted.

Do **not** try to widen that back out with a rule of the blanket Bash allow. `Bash` (bare) and `Bash(*)` are both detected as broad and suspended by the classifier. As a result, they are silently inert. `Bash(:*)` happens to slip past that detector (it only checks the `*` spelling, not the equivalent `:*`). However, that is a CLI bug, not a supported knob -- do not build on it. The supported lever is `autoMode.environment` prose plus narrow `permissions.allow` patterns.

# `autoMode` (the auto-mode classifier)

`autoMode` tunes the LLM classifier that vets tool calls in auto mode. The CLI reads it **only** from the user scope (`~/.claude/settings.json`), not from project settings. That is exactly where `setup/settings.sh` merges it. The block therefore takes effect. Its arrays (`environment`, `allow`, `soft_deny`, `hard_deny`) hold **prose**, not patterns. Each must keep the literal `"$defaults"` entry.

Omitting it *replaces* the built-in rules for that section, rather than extending them (see `vQt` in the CLI source, which splices the defaults in at the `$defaults` position, once). Note that `init-assets/deep-merge.jq` runs `unique`, which sorts each array. `"$defaults"` happens to sort first (`$` is 0x24), so its required leading position survives. Do not rely on the order of the other entries.

Distinct levers, frequently confused:

- **`permissions.allow`** -- pattern rules (e.g. `Bash(git push:*)`) that resolve *before* the classifier runs, bypassing it for matching commands. Per the docs, narrow rules carry over into auto mode but broad ones (`Bash(*)`, wildcarded interpreters) are suspended, so a blanket `Bash(*)` buys nothing here. `autoMode.classifyAllShell: true` suspends *every* Bash allow rule and routes all shell through the classifier -- leave it unset.
- **`autoMode.environment`** -- prose telling the classifier what this environment *is* (which orgs the user owns, which pushes are routine). This is what stops it misreading ordinary work as an attack.

CLAUDE.md prose does **not** configure the classifier -- it reads these settings keys, not instructions.

Rules worth knowing about because they will stop you mid-task:

- **The `enhanced-auto-allow` plugin rather than by patterns here blocks Python**. It denies the resolved PROCESS name. One rule therefore covers `python3.11`, `/usr/bin/python3`, `env python3`, `sudo -E python` and `uv run python`. That includes compound commands and substitutions a prefix pattern cannot see. The same section denies handing an interpreter an inline script (`node -e`, `perl -pe`, a heredoc into `node`), while it leaves `node script.js` alone. Use `jq` for JSON, and `node` for anything bigger. The `/jq-recipes` skill has the recipes.
- **Shell rewrites of project files are soft-denied.** Anything other than Write/Edit/MultiEdit changes an `autoMode.soft_deny` rule asks before a file the user will review. Heredocs, `sed -i`, `tee`, and equally `node -e` or a throwaway script, since reaching for an interpreter is how the rule gets walked around. The test is intent, not syntax. It is not about danger: such an edit leaves no diff in the transcript and is invisible to PostToolUse hooks that key on `tool_input.file_path`. This is exactly how an over-budget CLAUDE.md was edited a dozen times in silence. Approvable reasons exist (a mechanical sweep over many files). "it was faster to script it" is not one.
