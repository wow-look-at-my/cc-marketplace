# Why the binary is registered on two events

`plugin.json` registers the same binary on both `PermissionRequest` and
`PreToolUse`. That is not redundancy — each event reaches the tool call at a
different point, and only one of them is guaranteed to run.

## PermissionRequest only fires on the "ask" path

In `cli.js` (verified against 2.1.220), `createCanUseTool` evaluates the
permission rules first and returns early:

```js
let a = s ?? (await cM(t, r, n, o, i));
if (a.behavior === "allow") return a;
if (a.behavior === "deny") { ...; return a; }
...
let y = QdE(t, i, l, n, c)   // -> executePermissionRequestHooks
```

`QdE` is the only caller of `executePermissionRequestHooks`, and everything it
produces is tagged `decideLocation: "ask-path"`. So a `PermissionRequest` hook
gets a vote **only when the permission engine has already landed on "ask"**.

Anything that resolves the call earlier silences it. `defaultMode: "auto"` is
exactly that: the auto-mode classifier answers "allow" before the ask path is
reached. For as long as `PermissionRequest` was this plugin's only
registration, every deny rule in `rules.xml` was dead under that mode —
`python3 -c "print(1)"` ran with no prompt, while feeding the binary a
synthetic `PermissionRequest` payload for the same command returned a correct
deny. The rules were right; nothing ever asked them.

## PreToolUse fires unconditionally

The `PreToolUse` generator is iterated in the tool-execution path *before* the
permission pipeline is invoked at all — the permission call (`gan`, which is
what runs `canUseTool` and therefore auto-mode) happens afterwards. The only
guards on the dispatch are "is this the conversation-ending tool", "is this a
bare fork", and "is any `PreToolUse` hook registered". Nothing about the
permission outcome gates it.

When it denies, `gan` short-circuits before `canUseTool`, the tool never
executes, and `permissionDecisionReason` reaches the model verbatim as an
`is_error` tool_result.

## Why PreToolUse denies and never allows

`denyOnly` in `hook.go` suppresses every non-deny verdict on the `PreToolUse`
path. An allow there would settle the call *before* the permission engine runs,
so the user's own `permissions.deny` rules and auto-mode's `hard_deny` entries
would never get a vote — this plugin would be overriding the environment
instead of adding to it. Auto-approval is a convenience and belongs downstream
of those rules, which is where `PermissionRequest` sits.

The split is therefore: **deny rides `PreToolUse`** (must always fire),
**allow rides `PermissionRequest`** (must never outrank the user).

## Output shapes differ per event

The CLI rejects a payload whose `hookEventName` does not match the event it
dispatched, and the two events want different objects:

```jsonc
// PreToolUse
{"hookSpecificOutput":{"hookEventName":"PreToolUse",
  "permissionDecision":"deny","permissionDecisionReason":"..."}}

// PermissionRequest
{"hookSpecificOutput":{"hookEventName":"PermissionRequest",
  "decision":{"behavior":"deny","message":"..."}}}
```

`outputDecision` branches on the event for this reason. The stdin payloads are
otherwise near-identical (`tool_name`, `tool_input`, `session_id`, `cwd`,
`permission_mode`, …); `PreToolUse` adds `tool_use_id`, `PermissionRequest`
adds `permission_suggestions`. Neither is read here.

## Both can run for one call

On a call `PreToolUse` does not deny, `PermissionRequest` may still run
afterwards if the engine lands on "ask". The binary is stateless and derives
its verdict from `tool_input` alone, so evaluating twice is harmless.
