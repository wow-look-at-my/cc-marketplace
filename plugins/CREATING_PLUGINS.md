# Creating Plugins

This guide walks through creating a Claude Code plugin from scratch.

## Quick Start

1. Copy the `example-plugin/` folder and rename it to your plugin name
2. Rename all `.template.` files (remove `.template.` from names)
3. Edit `plugin.json` with your plugin's metadata
4. Add your commands, agents, or skills
5. Add the plugin to `../.claude-plugin/marketplace.json`

## Plugin Directory Structure

```
your-plugin/
├── .claude-plugin/
│   └── plugin.json          # Required: Plugin metadata
├── commands/                # Slash commands (optional)
│   └── your-command.md
├── agents/                  # Subagent definitions (optional)
│   └── your-agent.md
├── skills/                  # Skills (optional)
│   └── your-skill/
│       └── SKILL.md
├── .mcp.json               # MCP server configs (optional)
└── README.md               # Plugin documentation (recommended)
```

## Step 1: Create plugin.json

Every plugin needs a `.claude-plugin/plugin.json` file:

```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "description": "What this plugin does",
  "author": {
    "name": "Your Name",
    "email": "you@example.com"
  },
  "keywords": ["tag1", "tag2"]
}
```

**Required fields:**
- `name` - Unique identifier (kebab-case, no spaces)

**Optional fields:**
- `version` - Semantic version
- `description` - Brief explanation
- `author` - Object with `name`, `email`, `url`
- `keywords` - Tags for discovery
- `homepage` - Documentation URL
- `repository` - Source code URL
- `license` - SPDX identifier (e.g., `"MIT"`)

## Step 2: Add Components

Add any combination of:

### Commands (`commands/`)

Simple slash commands. Create `.md` files with YAML frontmatter:

```yaml
---
description: Brief explanation for autocomplete
argument-hint: [args]
---

Instructions Claude follows when user runs /command-name.
```

See `example-plugin/commands/command.template.md` for a full example.

### Agents (`agents/`)

Specialized subagents for complex tasks:

```yaml
---
name: reviewer
description: Reviews code for quality issues
tools: Read, Glob, Grep
model: sonnet
---

You are a code reviewer. Analyze code and provide feedback.
```

See `example-plugin/agents/agent.template.md` for a full example.

### Skills (`skills/`)

The most flexible option. Each skill is a folder with `SKILL.md`:

```
skills/
└── my-skill/
    ├── SKILL.md         # Main instructions
    ├── template.md      # Template file (optional)
    └── examples/        # Example outputs (optional)
```

See `example-plugin/skills/example-skill/SKILL.template.md` for a full example.

### MCP Servers (`.mcp.json`)

External tools and data sources:

```json
{
  "mcpServers": {
    "my-server": {
      "command": "${CLAUDE_PLUGIN_ROOT}/server",
      "args": ["--port", "3000"],
      "env": {
        "API_KEY": "${MY_API_KEY}"
      }
    }
  }
}
```

See `example-plugin/.mcp.template.json` for a full example.

## Step 3: Add to Marketplace

Edit `../.claude-plugin/marketplace.json` and add your plugin to the `plugins` array:

```json
{
  "plugins": [
    {
      "name": "your-plugin",
      "source": "./your-plugin",
      "description": "What your plugin does"
    }
  ]
}
```

## Testing Your Plugin

1. Add this marketplace to Claude Code:
   ```
   /plugin marketplace add /path/to/this/repo
   ```

2. Install your plugin:
   ```
   /plugin install your-plugin
   ```

3. Test your commands/agents/skills

4. Make changes and reinstall to test updates

## Tips

- Start simple - add one command or agent first
- Use `allowed-tools` in skills to restrict what Claude can do
- Keep skill instructions under 500 lines for best performance
- Use `${CLAUDE_PLUGIN_ROOT}` for paths in MCP configs and hooks

## Next Steps

- See [PLUGIN_REFERENCE.md](./PLUGIN_REFERENCE.md) for complete field documentation
- See [MARKETPLACE_GUIDE.md](../.claude-plugin/MARKETPLACE_GUIDE.md) for marketplace management
