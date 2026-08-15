# repo-index

A `UserPromptSubmit` hook. It matches the prompt against an index of
repositories and injects the link and one description for each match. Each
repository is injected at most once per session.

- The once-per-session promise is the feature. A record under
  `$TMPDIR/repo-index/<hash-of-session-id>.json` holds the names already sent.
  A lost record repeats a suggestion, so `run` reports the failure on stderr
  and still injects.
- `repos.json` is embedded at build time and is the default index. A user file
  at `~/.claude/repo-index.json` or `<project>/.claude/repo-index.json` merges
  over it by `name`. A malformed or unusable user file exits 1 with the reason.
  It never falls back to the default in silence.
- A match phrase must be specific enough to earn a whole injection. `xsd` is
  good. `build` would fire on almost every prompt.
- The cap is three repositories per prompt. Anything dropped is named on
  stderr.
- The hook never exits 2. Exit 2 blocks the user's prompt, and a suggestion is
  not worth that.
