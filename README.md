# Claude Code Marketplace

A marketplace of Claude Code plugins.

## For Users: Installing Plugins

### Add This Marketplace

```bash
claude plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json
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

1. Create `plugins/your-plugin/.claude-plugin/plugin.json` with your metadata
2. Add your commands, agents, or skills
3. Add the plugin to `.claude-plugin/marketplace.json`

A prose rule does not belong here. It goes in [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix), and `plugins/slopfix/` reaches it with a launcher.

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

## Resources

- [Official Claude Code Docs](https://docs.anthropic.com/en/docs/claude-code)
- [Official Plugin Examples](https://github.com/anthropics/claude-plugins-official)
- [MCP Specification](https://modelcontextprotocol.io/)
