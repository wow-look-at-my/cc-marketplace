---
# Agent/Subagent Template
# Rename this file to your-agent.md (removing .template.)
#
# Agents are specialized assistants that Claude can delegate tasks to.
# Define when this agent should be used in the description.

name: your-agent-name
description: When to use this agent. Claude reads this to decide whether to delegate tasks to it.

# Tools this agent can use (comma-separated or array)
# Omit to inherit all tools from parent
tools: Read, Glob, Grep, Bash

# Model to use: sonnet, opus, haiku, or inherit (default)
model: inherit

# Permission handling:
#   default        - Normal permission prompts
#   acceptEdits    - Auto-accept file edits
#   dontAsk        - Auto-deny permission prompts
#   bypassPermissions - Skip all permission checks
#   plan           - Read-only planning mode
permissionMode: default

# Skills to preload into this agent's context (optional)
# skills:
#   - skill-name-1
#   - skill-name-2
---

# Agent System Prompt

You are a specialized agent for [describe purpose].

## Your Role

[Describe what this agent does and when it should be used]

## Guidelines

- [Guideline 1]
- [Guideline 2]
- [Guideline 3]

## Output Format

[Describe how the agent should format its output]
