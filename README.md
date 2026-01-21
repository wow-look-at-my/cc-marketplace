# Claude Code Marketplace

A marketplace of Claude Code plugins.

## For Users: Installing Plugins

### Add This Marketplace

```bash
# From GitHub
claude plugin marketplace add owner/repo-name

# From local path
claude plugin marketplace add /path/to/this/repo
```

### Install Plugins

```bash
# List available marketplaces
claude plugin marketplace list

# Install a plugin
claude plugin install plugin-name

# Enable/disable plugins
claude plugin enable plugin-name
claude plugin disable plugin-name
```

### Managing Plugins

```bash
# Remove a plugin
claude plugin remove plugin-name

# Update plugins
claude plugin update plugin-name

# Update marketplaces
claude plugin marketplace update
```

## For Developers: Creating Plugins

### Quick Start

1. Copy `plugins/example-plugin/` to `plugins/your-plugin/`
2. Rename `.template.` files (remove `.template.` from names)
3. Edit `.claude-plugin/plugin.json` with your metadata
4. Add your commands, agents, or skills
5. Add the plugin to `.claude-plugin/marketplace.json`

### Documentation

| Guide | Description |
|-------|-------------|
| [Creating Plugins](plugins/CREATING_PLUGINS.md) | Step-by-step plugin creation |
| [Plugin Reference](plugins/PLUGIN_REFERENCE.md) | Complete component reference |
| [Marketplace Guide](.claude-plugin/MARKETPLACE_GUIDE.md) | Managing the marketplace |

### Plugin Structure

```
plugins/your-plugin/
├── .claude-plugin/
│   └── plugin.json          # Plugin metadata
├── commands/                # Slash commands
├── agents/                  # Subagent definitions
├── skills/                  # Skills
├── .mcp.json               # MCP server configs
└── README.md               # Plugin docs
```

## Template Files

The `plugins/example-plugin/` directory contains `.template.` files showing the correct structure and format for each component:

| File | Creates |
|------|---------|
| [plugin.template.json](plugins/example-plugin/.claude-plugin/plugin.template.json) | Plugin metadata |
| [command.template.md](plugins/example-plugin/commands/command.template.md) | Slash command |
| [agent.template.md](plugins/example-plugin/agents/agent.template.md) | Subagent definition |
| [SKILL.template.md](plugins/example-plugin/skills/example-skill/SKILL.template.md) | Skill |
| [.mcp.template.json](plugins/example-plugin/.mcp.template.json) | MCP server config |
| [README.template.md](plugins/example-plugin/README.template.md) | Plugin documentation |

Copy the folder, rename files (remove `.template.`), and edit.

## Resources

- [Official Claude Code Docs](https://docs.anthropic.com/en/docs/claude-code)
- [Official Plugin Examples](https://github.com/anthropics/claude-plugins-official)
- [MCP Specification](https://modelcontextprotocol.io/)
