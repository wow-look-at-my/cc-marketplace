## No Repeat Reply Plugin

The no-repeat-reply plugin lives at `plugins/no-repeat-reply/`. It is one Stop hook against one failure. A gate re-fires. The assistant answers with the message it just sent. The pair then spin until the session dies having produced nothing.

**The incident.** A goal checker judged its condition unmet and blocked the stop. The session reported the block. The checker re-fired. The session then answered with the same four words on every turn that followed. Those words were "Blocked. Waiting on you.", for eighteen checker iterations. No turn after the first produced work. The claim under the repeat was never re-tested. That claim was false. The repository the session called unreachable was listed and pushable the whole time. That is the second half of the refusal text. It is also why the refusal names `list_repos`.

**The bound is a marker, NOT `stop_hook_active`.** Every other Stop guard here returns at once when that flag is true. This one cannot. The loop it exists to catch happens ENTIRELY inside a stop-hook continuation, which is when the flag is set. Honouring the flag makes the plugin blind to its only case. The bound is instead one marker per session at `<tempdir>/no-repeat-reply/<sha256(session_id)[:16]>`. The refusal therefore fires at most once. It cannot wedge a session even in principle. A marker that cannot be written allows the stop instead.

**It judges no wording.** `focus-please` reads the transcript to ask whether a reply exists. This asks only whether the last closing message matches the one before it, after whitespace and case collapse. That is a mechanical fact about a session making no progress. It is not an opinion about prose. A wording refusal causes a retype loop, and this avoids that by judging no wording at all.

**A closing message is `stop_reason: "end_turn"` plus a text block.** A record that stopped with `tool_use` is mid-turn. A thinking block is not something the user read. A repeat inside one turn is out of reach by design. So is a reworded repeat. So is the third repeat. The snippet `a-repeated-reply-is-a-dead-session.md` in PazerOP/claude-code-web-config carries those.

Short repeats are allowed, under 24 characters. "Done." twice is an acknowledgement rather than a stuck session. Every failure path allows the stop. Those paths are an unparseable payload, another event, no session id, and a missing or garbage transcript.

- **Hook binary**: `plugins/no-repeat-reply/hook.go` -- payload, the one-shot marker, normalization, and the refusal text
- **Transcript**: `plugins/no-repeat-reply/transcript.go` -- bounded tail read, closing-message extraction
- **Tests**: `plugins/no-repeat-reply/hook_test.go` -- the refusal, whitespace and case insensitivity, and one-shot per session. Also per-session isolation, differing messages, the short-repeat floor, mid-turn and thinking records, and every fail-open path
- **Plugin config**: `plugins/no-repeat-reply/.claude-plugin/plugin.json` -- the Stop registration
