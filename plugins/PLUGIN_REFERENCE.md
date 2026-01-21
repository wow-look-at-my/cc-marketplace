# Plugin Reference

Complete reference for all Claude Code plugin components.

## Table of Contents

- [plugin.json](#pluginjson)
- [Slash Commands](#slash-commands)
- [Agents](#agents)
- [Skills](#skills)
- [MCP Servers](#mcp-servers)
- [Hooks](#hooks)
- [Environment Variables](#environment-variables)

---

## plugin.json

Located at `.claude-plugin/plugin.json` in your plugin directory.

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique identifier. Kebab-case, no spaces. |

### Optional Metadata Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Semantic version (e.g., `"1.0.0"`) |
| `description` | string | Brief explanation of purpose |
| `author` | object | See Author Object below |
| `homepage` | string | Documentation URL |
| `repository` | string | Source code URL |
| `license` | string | SPDX identifier (e.g., `"MIT"`, `"Apache-2.0"`) |
| `keywords` | array | Tags for discovery |

### Author Object

```json
{
  "author": {
    "name": "Author Name",
    "email": "author@example.com",
    "url": "https://author.example.com"
  }
}
```

Only `name` is commonly used. All fields are optional.

### Component Path Overrides

Override default component locations:

| Field | Type | Description |
|-------|------|-------------|
| `commands` | string\|array | Custom path(s) to command files |
| `agents` | string\|array | Custom path(s) to agent files |
| `skills` | string\|array | Custom path(s) to skill directories |
| `hooks` | string\|object | Hook config path or inline config |
| `mcpServers` | string\|object | MCP config path or inline config |
| `outputStyles` | string\|array | Custom path(s) to output style files |

Paths are relative to plugin root and must start with `./`.

### Complete Example

```json
{
  "name": "code-quality",
  "version": "2.0.0",
  "description": "Code quality tools",
  "author": {
    "name": "DevTools Team",
    "email": "devtools@company.com"
  },
  "homepage": "https://docs.company.com/code-quality",
  "repository": "https://github.com/company/code-quality-plugin",
  "license": "MIT",
  "keywords": ["linting", "formatting", "code-quality"],
  "commands": "./custom/commands/",
  "agents": ["./agents/", "./custom/agents/"],
  "mcpServers": "./.mcp.json"
}
```

---

## Slash Commands

Located in `commands/` directory. Each `.md` file becomes a command.

### Filename Convention

`command-name.md` creates `/plugin-name:command-name` (namespaced by plugin).

### Frontmatter Fields

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `description` | Recommended | string | Shown in autocomplete |
| `argument-hint` | No | string | Hint shown after command name (e.g., `[file]`) |
| `allowed-tools` | No | string | Tools available during command execution |
| `disable-model-invocation` | No | boolean | If `true`, manual `/command` only |

### Body Format

Markdown instructions Claude follows when the command is invoked.

### Variable Substitution

| Variable | Description |
|----------|-------------|
| `$ARGUMENTS` | Everything after the command name |
| `${CLAUDE_SESSION_ID}` | Current session ID |

### Example

```yaml
---
description: Format code in the current file
argument-hint: [file-path]
allowed-tools: Read, Write, Bash
---

Format the code in $ARGUMENTS:

1. Read the file
2. Determine the language
3. Apply appropriate formatting
4. Write the formatted code back
```

---

## Agents

Located in `agents/` directory. Each `.md` file defines a subagent.

### Frontmatter Fields

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `name` | Yes | string | Unique identifier (lowercase, hyphens) |
| `description` | Yes | string | When Claude should use this agent |
| `tools` | No | string\|array | Available tools (inherits all if omitted) |
| `disallowedTools` | No | string\|array | Tools to remove from available set |
| `model` | No | string | `sonnet`, `opus`, `haiku`, or `inherit` |
| `permissionMode` | No | string | See Permission Modes below |
| `skills` | No | array | Skills to preload at startup |

### Permission Modes

| Mode | Behavior |
|------|----------|
| `default` | Normal permission prompts |
| `acceptEdits` | Auto-accept file edits |
| `dontAsk` | Auto-deny permission prompts |
| `bypassPermissions` | Skip all permission checks |
| `plan` | Read-only planning mode |

### Body Format

Markdown system prompt for the subagent.

### Example

```yaml
---
name: security-reviewer
description: Reviews code for security vulnerabilities. Use after implementing auth or handling user input.
tools: Read, Glob, Grep
model: sonnet
permissionMode: default
skills:
  - owasp-top-10
---

You are a security-focused code reviewer. Analyze code for:

- Injection vulnerabilities (SQL, command, XSS)
- Authentication and authorization issues
- Sensitive data exposure
- Security misconfigurations

Provide specific line numbers and remediation steps.
```

---

## Skills

Located in `skills/` directory. Each subdirectory with `SKILL.md` is a skill.

### Directory Structure

```
skills/
└── skill-name/
    ├── SKILL.md           # Required: Main instructions
    ├── template.md        # Optional: Template for output
    ├── examples/          # Optional: Example outputs
    │   └── example.md
    └── scripts/           # Optional: Scripts to run
        └── validate.sh
```

### Frontmatter Fields

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `name` | No | string | Override directory name. Max 64 chars, lowercase/numbers/hyphens. |
| `description` | Recommended | string | When to use this skill (used for auto-loading) |
| `argument-hint` | No | string | Hint shown during autocomplete |
| `disable-model-invocation` | No | boolean | If `true`, manual `/skill` only (no auto-loading) |
| `user-invocable` | No | boolean | If `false`, hidden from `/` menu |
| `allowed-tools` | No | string | Tools available when skill is active |
| `model` | No | string | Model to use when skill is active |
| `context` | No | string | Set to `fork` to run in a subagent |
| `agent` | No | string | Subagent type when `context: fork` |

### Validation Rules

- **Name**: Max 64 characters
- **Name characters**: Lowercase letters, numbers, hyphens only
- **Body length**: Keep under 500 lines for best performance

### Variable Substitution

| Variable | Description |
|----------|-------------|
| `$ARGUMENTS` | Arguments passed to the skill |
| `${CLAUDE_SESSION_ID}` | Current session ID |

### Example

```yaml
---
name: api-docs
description: Generate API documentation from code. Use when asked to document an API.
argument-hint: [source-directory]
allowed-tools: Read, Glob, Grep, Write
disable-model-invocation: false
---

Generate comprehensive API documentation:

1. Scan $ARGUMENTS for API endpoints
2. Extract function signatures, parameters, return types
3. Generate markdown documentation
4. Include usage examples

## Output Format

Create `API.md` with:
- Overview section
- Authentication details
- Endpoint reference
- Error codes
- Examples
```

---

## MCP Servers

Located at `.mcp.json` in the plugin root.

### Structure

```json
{
  "mcpServers": {
    "server-name": {
      // Server configuration
    }
  }
}
```

### stdio Server (Local Process)

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `command` | Yes | string | Executable to run |
| `args` | No | array | Command-line arguments |
| `env` | No | object | Environment variables |
| `cwd` | No | string | Working directory |

```json
{
  "mcpServers": {
    "database": {
      "command": "npx",
      "args": ["-y", "@example/db-server"],
      "env": {
        "DB_URL": "${DATABASE_URL}"
      }
    }
  }
}
```

### HTTP Server (Remote)

| Field | Required | Type | Description |
|-------|----------|------|-------------|
| `type` | Yes | string | `"http"` |
| `url` | Yes | string | Server URL |
| `headers` | No | object | HTTP headers (e.g., auth tokens) |

```json
{
  "mcpServers": {
    "api": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${API_TOKEN}"
      }
    }
  }
}
```

### Plugin-Local Server

Use `${CLAUDE_PLUGIN_ROOT}` for paths within your plugin:

```json
{
  "mcpServers": {
    "local-server": {
      "command": "${CLAUDE_PLUGIN_ROOT}/bin/server",
      "args": ["--config", "${CLAUDE_PLUGIN_ROOT}/config.json"],
      "env": {
        "DATA_DIR": "${CLAUDE_PLUGIN_ROOT}/data"
      }
    }
  }
}
```

---

## Hooks

Hooks run shell commands in response to events. Configure inline in `plugin.json` or in a separate file.

### Hook Events

| Event | When | Can Block |
|-------|------|-----------|
| `SessionStart` | Claude Code starts | No |
| `PreToolUse` | Before a tool runs | Yes |
| `PostToolUse` | After a tool runs | No |
| `Stop` | Before Claude Code exits | No |

### Hook Configuration

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write",
        "hooks": [
          {
            "type": "command",
            "command": "${CLAUDE_PLUGIN_ROOT}/scripts/validate.sh"
          }
        ]
      }
    ]
  }
}
```

### Matcher Patterns

| Pattern | Matches |
|---------|---------|
| `"Write"` | Exact tool name |
| `"Bash(npm:*)"` | Bash commands starting with `npm` |
| `"*"` | All tools |

### Hook Types

| Type | Description |
|------|-------------|
| `command` | Run a shell command |

### Blocking Behavior

`PreToolUse` hooks can block tool execution:
- Exit code 0: Allow tool to run
- Exit code 2: Block tool, show stderr as reason
- Other exit codes: Block tool silently

---

## Environment Variables

Available in MCP configs, hooks, and scripts:

| Variable | Description |
|----------|-------------|
| `${CLAUDE_PLUGIN_ROOT}` | Absolute path to plugin directory |
| `${VAR}` | Value of environment variable `VAR` |
| `${VAR:-default}` | Value of `VAR`, or `default` if unset |

### Example Usage

```json
{
  "command": "${CLAUDE_PLUGIN_ROOT}/bin/server",
  "env": {
    "API_KEY": "${MY_API_KEY}",
    "LOG_LEVEL": "${LOG_LEVEL:-info}"
  }
}
```

---

## Further Reading

- [Official Plugin Documentation](https://docs.anthropic.com/en/docs/claude-code/plugins)
- [MCP Specification](https://modelcontextprotocol.io/)
- [Official Plugin Examples](https://github.com/anthropics/claude-plugins-official)
