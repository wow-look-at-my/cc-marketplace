# Marketplace Guide

This guide explains how to manage the `marketplace.json` file in this directory.

## marketplace.json Structure

```json
{
  "name": "your-marketplace-name",
  "owner": {
    "name": "Your Name",
    "email": "your@email.com"
  },
  "metadata": {
    "description": "Description of your marketplace",
    "pluginRoot": "./plugins"
  },
  "plugins": [
    {
      "name": "plugin-name",
      "source": "./plugin-folder",
      "description": "What the plugin does"
    }
  ]
}
```

## Required Fields

| Field | Description |
|-------|-------------|
| `name` | Marketplace identifier (kebab-case, no spaces) |
| `owner.name` | Your name or team name |
| `plugins` | Array of plugin entries |

## Optional Fields

| Field | Description |
|-------|-------------|
| `owner.email` | Contact email |
| `metadata.description` | Brief marketplace description |
| `metadata.pluginRoot` | Base directory for relative plugin paths (e.g., `"./plugins"`) |

## Adding a Plugin

Add an entry to the `plugins` array:

```json
{
  "plugins": [
    {
      "name": "my-plugin",
      "source": "./my-plugin",
      "description": "What this plugin does",
      "version": "1.0.0",
      "author": {
        "name": "Author Name"
      },
      "category": "development",
      "keywords": ["keyword1", "keyword2"]
    }
  ]
}
```

### Plugin Entry Fields

**Required:**
| Field | Description |
|-------|-------------|
| `name` | Plugin identifier (kebab-case) |
| `source` | Where to find the plugin (see Source Types below) |

**Optional:**
| Field | Description |
|-------|-------------|
| `description` | Brief plugin description |
| `version` | Semantic version |
| `author` | Object with `name` and optional `email` |
| `homepage` | Documentation URL |
| `repository` | Source code URL |
| `license` | SPDX identifier (e.g., `"MIT"`) |
| `category` | Freeform category string |
| `keywords` | Array of tags for discovery |

## Source Types

### Local Path (Relative)

For plugins in this repository:

```json
{
  "name": "my-plugin",
  "source": "./my-plugin"
}
```

With `pluginRoot: "./plugins"` set in metadata, you can shorten this to:

```json
{
  "name": "my-plugin",
  "source": "my-plugin"
}
```

### GitHub Repository

For plugins hosted on GitHub:

```json
{
  "name": "external-plugin",
  "source": {
    "source": "github",
    "repo": "owner/repo-name",
    "ref": "v1.0.0"
  }
}
```

The `ref` field is optional and can be a branch, tag, or commit SHA.

### Git Repository

For plugins on other Git hosts (GitLab, Bitbucket, self-hosted):

```json
{
  "name": "gitlab-plugin",
  "source": {
    "source": "git",
    "url": "https://gitlab.com/org/plugin.git",
    "ref": "main"
  }
}
```

### NPM Package

For plugins published to npm:

```json
{
  "name": "npm-plugin",
  "source": {
    "source": "npm",
    "package": "@scope/plugin-name"
  }
}
```

## Removing a Plugin

Delete the plugin entry from the `plugins` array. Optionally delete the plugin directory if it's local.

## Updating a Plugin

For local plugins, just update the plugin files - no marketplace.json changes needed.

For external plugins with pinned versions, update the `ref` or `version` field:

```json
{
  "source": {
    "source": "github",
    "repo": "owner/repo",
    "ref": "v2.0.0"
  }
}
```

## Categories

The `category` field is freeform text. Common examples:
- `development` - Development workflow tools
- `productivity` - General productivity enhancements
- `integrations` - External service integrations
- `security` - Security-focused tools
- `code-quality` - Linting, formatting, review tools

## Reserved Marketplace Names

These names cannot be used:
- `claude-code-marketplace`
- `claude-code-plugins`
- `claude-plugins-official`
- `anthropic-marketplace`
- `anthropic-plugins`
- `agent-skills`
- `life-sciences`

## Validating Your Marketplace

Run validation before publishing:

```bash
claude plugin validate /path/to/marketplace
```

Or interactively in Claude Code:

```
/plugin validate /path/to/marketplace
```

## Further Reading

- [Official Plugin Documentation](https://docs.anthropic.com/en/docs/claude-code/plugins)
- [Marketplace Documentation](https://docs.anthropic.com/en/docs/claude-code/plugins#discover-plugins)
