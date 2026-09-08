## Link All Refs Plugin

The link-all-refs plugin lives at `plugins/link-all-refs/`. It is one **MessageDisplay** hook. It renders a reference as a markdown link while the message streams. It sends NOTHING back to the model.

**It was a Stop hook. That was wrong twice over.** A Stop hook runs after the message has streamed, so refusing cannot unsend anything. The user reads the bare reference, then reads a near-identical retype with the link in it. Worse, the retype names the reference again while explaining itself. The guard fires a second time and asks for a third message with the same property. The only reply that escapes that loop is one carrying nothing but links. That is what the user was left reading. It also never honoured `stop_hook_active`, so the loop had no bound at all.

**A missing link is not work only the model can do.** The token plus the checkout determine the URL, so the hook writes it: no round trip, and nothing to loop on. This is the standing rule for a guard here -- mitigate rather than refuse.

**`displayContent` is display-only**, verified against the shipped bundle rather than assumed. The transcript and the model's next request are both fed from an array populated BEFORE the hook runs, and never updated from its result. `displayContent` is the only field the event's output schema carries. So this changes what the reader sees and nothing else. That is fine here, because the reader is the whole point. A link exists to be clicked.

**A reference whose target cannot be shown to exist is left as plain text.** A link is a demand to stop reading and move your hand. The reader pays it before knowing whether it was worth paying. So a branch is linked only once `refs/remotes/origin/<branch>` exists, and a SHA only once the object is in the repository. A branch that was never pushed has no compare page. Silence beats a guessed URL.

**A BARE `#N` is never linked**, which is the sharpest form of that rule. Every other kind can be checked first. A branch goes against the remote and a commit against the object database. `owner/repo#N` carries its own repository. A URL is self-evidently real. A bare `#N` can be checked against nothing. The repository it resolves against is a guess, and a session with many checkouts has one working directory. It is also the shape an ordinary numbered list uses, so `#7` in a status message is usually not a reference at all. Guessing there does not produce a dead link, which the reader notices. It produces a link to a real, unrelated issue, which the reader does not notice. This was found live. A message listing open tasks as `#7`, `#8` and `#10` pointed at three unrelated issues.

**`/issues/N`, never `/pull/N`.** GitHub redirects an issue number to the pull request when it is one, and `/pull/N` on a plain issue is a 404. Nothing here knows which it is, so it uses the spelling that is right for both.

A bare `owner/repo` has the same shape as a directory path, and nothing separates `go/core` from a repository slug by looks alone. The `owner/repo#N` form IS matched, because the number carries it -- and it needs no checkout at all, since it names its own repository. That is the deliberate boundary of the check, not an oversight to fix by guessing.

A commit SHA needs both a digit and an a-f letter. A `&#N;` character reference is blanked before matching, so this org's `&#0;` is not read as an issue number.

**A `.` is the whole subtlety in the boundary check.** It joins a filename to its extension, so `6884dd2abc.log` must not be reported as a commit. It also ends a sentence. Counting every trailing `.` as part of the token made every reference that ENDS a sentence invisible to the guard. So a `.` counts only when an alphanumeric follows it. The branch matcher separately may not END on a `.` or `/`. Git forbids a ref that does, so a trailing one belongs to the sentence.

Text that is already a link is **blanked** before matching -- each link becomes an equal run of spaces. Blanking rather than stripping, because a replacement that changes the length moves every offset after it. A moved offset splices the next link into the middle of a word.

**A token inside a lone pair of backticks swallows them into its own range.** Splicing a link over just the bare token leaves the backticks straddling it (`` `[x](url)` ``), which markdown does not render as a link at all. `backtickWrapped` widens the match to include a single backtick on each side, and `Linkify` puts fresh backticks back INSIDE the brackets (`` [`x`](url) ``). A double backtick is the escape a code span uses for a literal backtick, not a wrap, so it is left alone.

A bare `owner/repo` has the same shape as a directory path, and nothing separates `go/core` from a repository slug by looks alone. The `owner/repo#N` form IS matched, because the number carries it. That is the deliberate boundary of the check, not an oversight to fix by guessing.

A commit SHA needs both a digit and an a-f letter. A `&#N;` character reference is stripped before matching, so this org's `&#0;` is not read as an issue number.

- **Entry point**: `plugins/link-all-refs/main.go` -- the MessageDisplay payload (`turn_id`, `message_id`, `index`, `final`, `delta`), the `displayContent` envelope, and the per-message state file that carries fence state across flushes. `delta` is whole lines except on the final flush, and `final` is the end-of-message signal even when its delta is empty
- **Rewrite**: `plugins/link-all-refs/display.go` -- the per-line walk, the right-to-left splice so an earlier offset stays valid, and the fenced/quoted exemption. Inline backticks are NOT exempt: a SHA in backticks is the exact thing this catches
- **Resolution**: `plugins/link-all-refs/linkify.go` -- token to URL, the `Resolver` seam, and `GitResolver` reading the checkout with a bounded, memoized `git`. Every answer is memoized because one message can name the same reference several times and this runs in the render path. `parseRemote` takes only the two path segments after the host, so a credential in a remote's userinfo cannot reach the user's screen
- **Detection**: `plugins/link-all-refs/refs.go` -- link blanking, the four matchers, the boundary check, and overlap dropping (a URL contains a slug the branch matcher also matches, and rewriting both nests a link inside a link)
- **Tests**: `detect_test.go` (each kind, the sentence-final case and the dotted-name case it must not break, offsets surviving an earlier link on the line), `display_test.go` (rendering, the dead-link refusals, already-linked text, fence state, and every fail-open path), `linkify_test.go` (remote spellings, credential stripping), `resolver_test.go` (a real git checkout, where a local-only branch has no page and an absent commit is not linked, plus memoization)
- **Plugin config**: `plugins/link-all-refs/.claude-plugin/plugin.json` -- one MessageDisplay hook registration

Every failure path prints nothing, which leaves the CLI showing the original text. This runs in the render path. A guard that can eat output is worse than no guard. `CC_LINK_ALL_REFS=0` disables it.

There is no wording half. A snippet stating this rule was deleted. Prose the model reads and ignores is context paid on every request for nothing. This plugin is the only enforcement. It has to earn that on its own.
