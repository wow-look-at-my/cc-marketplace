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
  "description": "What this plugin does",
  "author": {
    "name": "Your Name",
    "email": "you@example.com"
  },
  "keywords": ["tag1", "tag2"],
  "mh": {
    "include_in_marketplace": true
  }
}
```

**Required fields:**
- `name` - Unique identifier (kebab-case, no spaces)
- `mh.include_in_marketplace` - Must be `true` for CI to build this plugin

**Optional fields:**
- `description` - Brief explanation
- `author` - Object with `name`, `email`, `url`
- `keywords` - Tags for discovery
- `homepage` - Documentation URL
- `repository` - Source code URL
- `license` - SPDX identifier (e.g., `"MIT"`)

**Note:** Version is NOT specified in plugin.json. CI automatically assigns and bumps versions based on release tags.

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

### Build Script (`justfile`)

If your plugin needs a build step (compiling TypeScript, bundling, etc.), create a `justfile`:

```just
[private]
help:
    @just --list

build:
    npm install
    npm run build

test:
    npm test
```

See `example-plugin/justfile.template` for a starting point. The `build` recipe runs automatically during CI.

## Step 3: Enable Marketplace Inclusion

Ensure your `plugin.json` has `mh.include_in_marketplace: true`:

```json
{
  "mh": {
    "include_in_marketplace": true
  }
}
```

When you push to any branch, CI will:
1. Detect plugins with `mh.include_in_marketplace: true`
2. Run `just build` if a `justfile` exists
3. Create an orphan tag with the built plugin: `{branch}/{plugin}/v{version}`
4. Update `marketplace.json` with the new version

**You don't need to manually edit marketplace.json** - CI handles it automatically.

## Build & Release Process

Plugins are built and released automatically on push:

- **Tag naming:** `{branch}/{plugin}/v{version}` (e.g., `master/my-plugin/v1.2.3`)
- **Version bumping:** Patch version auto-increments (1.0.0 → 1.0.1)
- **Branch isolation:** Each branch has independent version series
- **Cleanup:** When a branch is deleted, all its tags are removed

To test locally without pushing:
```bash
just release prepare-matrix     # See which plugins would build
just release build-plugin my-plugin  # Dry-run build
```

## Testing Your Plugin

### Local Development

For local development before pushing:

1. Add the local repo as a marketplace:
   ```
   claude plugin marketplace add /path/to/this/repo
   ```

2. Install your plugin directly from source:
   ```
   claude plugin install your-plugin
   ```

3. Test your commands/agents/skills

4. Make changes and reinstall to test updates

### After Pushing

Once pushed, CI builds and releases your plugin. Users can install via:

```bash
# Add the marketplace (uses 'latest' tag = master branch)
claude plugin marketplace add owner/repo#latest

# Or specify a branch
claude plugin marketplace add owner/repo#feature-x/latest

# Install plugins from the marketplace
claude plugin install your-plugin
```

## Tips

- Start simple - add one command or agent first
- Use `allowed-tools` in skills to restrict what Claude can do
- Keep skill instructions under 500 lines for best performance
- Use `${CLAUDE_PLUGIN_ROOT}` for paths in MCP configs and hooks

## Next Steps

- See [PLUGIN_REFERENCE.md](./PLUGIN_REFERENCE.md) for complete field documentation
- See [MARKETPLACE_GUIDE.md](../.claude-plugin/MARKETPLACE_GUIDE.md) for marketplace management
