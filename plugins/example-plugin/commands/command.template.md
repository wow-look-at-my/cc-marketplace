---
# Slash Command Template
# Rename this file to your-command.md (removing .template.)
# The filename becomes the command name: /your-command
#
# Note: Commands and Skills use the same format. Commands in commands/ are
# automatically user-invocable. For more control, use skills/ instead.

description: Brief explanation shown in command autocomplete
argument-hint: [optional-args]
allowed-tools: Read, Glob, Grep
---

# Command Instructions

These instructions are followed when the user runs `/your-command`.

Use `$ARGUMENTS` to reference what the user passed after the command name.

## Steps

1. First, do this
2. Then, do that
3. Finally, complete the task

## Guidelines

- Keep instructions clear and actionable
- Include example output formats if relevant
- Specify any constraints or requirements
