# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Branch Protection

The `master` branch is protected. All changes require a pull request.

## Documentation

@README.md @.claude-plugin/MARKETPLACE_GUIDE.md @plugins/CREATING_PLUGINS.md @plugins/PLUGIN_REFERENCE.md

## Schemas

@.claude-plugin/marketplace.schema.json

## Plugins

Each plugin documents itself in its own directory. Add a new plugin's notes at `plugins/<name>/CLAUDE.md` and import it here so it loads from the repo root.

@plugins/css-duplication/CLAUDE.md @plugins/docs/CLAUDE.md @plugins/focus-please/CLAUDE.md @plugins/glob/CLAUDE.md @plugins/grep/CLAUDE.md @plugins/misc-skills/CLAUDE.md @plugins/repo-index/CLAUDE.md @plugins/slopfix/CLAUDE.md

The rules that were their own plugins now live in [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix), and `plugins/slopfix/` is the launcher that reaches them. That covers ask-properly, claude-md-budget, cleanup-bash-cmds, common-checks, detect-permission-seeking, enhanced-auto-allow, link-all-refs, no-blame-language, no-busy-poll, no-counts-in-docs, no-tombstones, no-work-loss and recommend-go-toolchain.

