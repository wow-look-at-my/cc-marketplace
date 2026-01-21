---
# Skill Template
# Rename this file to SKILL.md (removing .template.)
# The folder name becomes the skill/command name: /example-skill
#
# Skills are the most flexible way to extend Claude Code. They can be:
# - User-invocable commands (/skill-name)
# - Auto-loaded based on context
# - Run in subagents with restricted tools

name: example-skill
description: When this skill should be used. Claude reads this to decide when to auto-load it.

# Show hint during autocomplete (e.g., "[file-path]")
argument-hint: [arguments]

# Set true to prevent auto-loading (manual /skill-name only)
disable-model-invocation: false

# Set false to hide from / menu (internal skill)
user-invocable: true

# Tools available when this skill is active
allowed-tools: Read, Glob, Grep

# Run in a subagent instead of main context
# context: fork
# agent: Explore
---

# Skill Instructions

Instructions Claude follows when this skill is active.

Use `$ARGUMENTS` to reference arguments passed to the skill.
Use `${CLAUDE_SESSION_ID}` for the current session ID.

## Steps

1. First step
2. Second step
3. Final step

## Guidelines

- Keep instructions clear and specific
- Skills under 500 lines perform best
- Put detailed reference material in separate files in this folder

## Additional Files (Optional)

You can add supporting files in this skill's folder:
- `template.md` - Template for Claude to fill in
- `examples/` - Example outputs
- `scripts/` - Scripts Claude can run
