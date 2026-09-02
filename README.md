# jq

MCP server wrapping `jq` for JSON filtering, querying, and transforming without Bash permission prompts.

## Installation

```bash
# Add the marketplace (if not already added)
claude plugin marketplace add wow-look-at-my-code/cc-marketplace#latest

# Install
claude plugin install jq
```

## Requirements

- `jq` must be installed on your system
  - **Linux**: `apt install jq` or `yum install jq`
  - **macOS**: `brew install jq`

## Tools

### `jq`

Run a jq expression against a JSON file or inline JSON string.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `filter` | string | Yes | jq filter expression (e.g. `.name`, `.[] \| select(.age > 30)`) |
| `file` | string | One of file/input | Path to a JSON file |
| `input` | string | One of file/input | Inline JSON string |
| `raw_output` | bool | No | Output raw strings without quotes (`-r`) |
| `slurp` | bool | No | Read entire input into an array (`-s`) |

### `jq_read`

Read and pretty-print a JSON file.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file` | string | Yes | Path to the JSON file |

## Examples

```
# Extract a field
jq(filter: ".name", file: "package.json")

# Filter an array
jq(filter: ".dependencies | keys", file: "package.json")

# Process inline JSON
jq(filter: ".[] | select(.active)", input: "[{\"name\":\"a\",\"active\":true},{\"name\":\"b\",\"active\":false}]")

# Pretty-print a file
jq_read(file: "config.json")
```

## Safety

- Output is returned as tool results only -- this server cannot write to files
- Output is capped at 1MB to prevent context blowout
- Execution timeout is 30 seconds
- No shell involved -- jq is invoked directly with no injection risk

## License

MIT
