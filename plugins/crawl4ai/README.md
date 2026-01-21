# crawl4ai

MCP server for crawling web pages using [crawl4ai](https://github.com/unclecode/crawl4ai) and returning content as markdown.

## Installation

```bash
# Add the marketplace
claude plugin marketplace add wow-look-at-my-code/mcp-crawl4ai

# Install the plugin
claude plugin install crawl4ai
```

## Building

Requires Go 1.23+:

```bash
cd plugins/crawl4ai
just build
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CRAWL4AI_URL` | URL of the crawl4ai API server | `http://192.168.1.84:11235` |

## MCP Tools

### `crawl`

Crawl a URL and return its content as markdown plus all links found on the page.

**Parameters:**
- `url` (required): The URL to crawl

**Returns:**
- Markdown content of the page
- All links found on the page

## Requirements

- Running crawl4ai server (docker container or standalone)
- Go 1.23+ (for building from source)
