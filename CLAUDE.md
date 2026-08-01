## Enhanced Auto-Allow Plugin

The enhanced-auto-allow plugin lives at `plugins/enhanced-auto-allow/`. It decides Bash and MCP tool
permissions via a PermissionRequest hook: auto-approving read-only work, and refusing programs this
environment does not allow to run.

- **Rules**: `plugins/enhanced-auto-allow/rules.xml` -- three sections (`<allow>`, `<ask>`, `<deny>`)
  of `<rule>` nodes, plus top-level `<test>` cases and the `<mcpServer>` blocks
- **Hook code**: `plugins/enhanced-auto-allow/cmd/hook.go` (evaluation), `cmd/rules_xml.go` (schema),
  `cmd/deny_process.go` (process resolution)
- **Plugin config**: `plugins/enhanced-auto-allow/.claude-plugin/plugin.json` -- one PermissionRequest
  hook on matcher `*`

### The rule node

One node type, in all three sections; the section supplies the verdict, so the same node denies under
`<deny>` and allows under `<allow>`. A rule matches one of two ways:

- **Command rule** (`name=`) -- matched against argv as written, with the flag and argument
  constraints (`allowedFlags`, `deniedFlags`, `requiredFlags`, `denyArgSubstrings`, nested `<rule>`
  subcommands, ...).
- **Process rule** (`process=`) -- matched against the RESOLVED process name: basename, `VAR=VAL`
  assignments and wrapper commands stripped (`env`, `sudo`, `uv run`, `npx`, `timeout`, ...), a
  trailing version ignored. One `process="python"` covers `python3`, `python3.11`,
  `/usr/bin/python3`, `env python3`, `sudo -E python` and `uv run python`. Enumerating spellings can
  never be finished, which is the whole reason this exists.
  - `inlineScript="true"` narrows it to invocations handed a SCRIPT rather than a file, via
    `evalFlags` (`-e`, `--eval`; single-dash flags also match inside a cluster, so perl's `-pe` and
    `-lane` are covered), `evalSubcommands` (`deno eval`), stdin markers (`-`, `/dev/stdin`),
    heredocs, and pipes into the process. That denies `node -e '...'` while leaving `node script.js`
    working -- which matters, because this environment's own hooks are node files.

Precedence is deny > ask > allow, and process rules are evaluated first. Deny walks the WHOLE parse
tree, including the command substitutions, subshells and redirects the allow path deliberately
refuses to read: giving up is the right answer when granting permission and the wrong one when
refusing it, or a denied program would be one `$(...)` away from running.

The trust unit for MCP is the **server**, never a tool-name pattern. Tool names
are author-chosen, so `get_`/`query_` guarantee nothing, and a server-name glob
would extend the allowlist to servers nobody has connected yet. A glob inside a
server is used only where every tool it matches on that server's real surface
was checked read-only (hence `actions_get`/`actions_list` spelled out on
`github`, since `actions_*` would swallow `actions_run_trigger`).

### Adding rules

- **Allow a Bash command**: add a `<rule name="...">` under `<allow>`.
- **Ban a program outright**: add `<rule process="..." message="..."/>` under `<deny>`.
- **Ban only its inline-script form**: add `inlineScript="true"` and the interpreter's `evalFlags`.
- **MCP tools**: add an `<mcpServer name="...">` block. No `plugin.json` change is needed -- the `*`
  matcher already routes every tool through the binary.
- **Other non-Bash tools**: add to the tool name allowlist in `cmd/hook.go`.

Every rule change wants a `<test cmd="..." expect="allow|deny|ask|"/>` beside it (an empty `expect`
means passthrough). The tests are embedded in `rules.xml` itself and run by `go-toolchain`, so a rule
and its proof cannot drift apart.
