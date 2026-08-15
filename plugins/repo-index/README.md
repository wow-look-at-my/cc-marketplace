# repo-index

Points you at the repository that already does the job, before you build a
second one.

A `UserPromptSubmit` hook matches your prompt against an index of repositories.
Each repository that matches is added to the prompt as a link and one line of
description. Each repository appears at most once per session.

## Example

> how do I publish a binary to buildhost?

```
Repositories that look relevant to this request. Read one before you build
something it already provides. Each repo appears here at most once per session.

- **wow-look-at-my/buildhost** -- https://github.com/wow-look-at-my/buildhost
  Universal package registry. Upload a release artifact once, then download it
  as a raw binary, tar.gz/xz/zst, zip, deb, Homebrew formula, npm package, or
  OCI image. ...
```

Ask again in the same session and nothing is added. Ask in a new session and it
comes back.

## Your own repositories

The built-in index covers the org's shared tools. Add your own in either file:

- `~/.claude/repo-index.json` -- every session
- `<project>/.claude/repo-index.json` -- that project only, and it wins

```json
{
  "repos": [
    {
      "name": "me/thing",
      "url": "https://github.com/me/thing",
      "description": "One or two sentences. This text goes into the prompt.",
      "match": ["thing", "frobnicator", "widget pipeline"]
    }
  ]
}
```

An entry replaces a built-in one with the same `name`, so you can point a repo
at your own mirror or rewrite its description.

A `match` phrase matches on whole words and ignores case. `buildhost` matches
`buildhost.` and `BuildHost`; it does not match `go-buildhost` or
`buildhosting`. A phrase may contain spaces.

A file that is present but malformed is an error, and the hook says so instead
of ignoring the file.

## Limits

At most three repositories are added per prompt. Any that a prompt matched
beyond that are named on stderr, so the cap is never silent.
