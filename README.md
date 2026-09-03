# repo-index

Points you at the repository that already does the job, before you build a
second one.

A `UserPromptSubmit` hook matches your prompt against an index of your
repositories. Each one that matches is added to the prompt as a link and a line
of description. Each repository appears at most once per session.

**The index is built from GitHub, never written by hand.** The description is
the repository's own, or the first paragraph of its README when GitHub carries
no description. The words it matches on are its name and the topics its owner
set, plus the words its description uses and almost no other description does.
Nothing in it is typed by a person, so nothing in it can quietly stop being
true: rename a repository, retag it, rewrite its description, and the next
refresh follows.

## Example

> how do I validate an xsd schema?

```
Repositories that look relevant to this request. Read one before you build
something it already provides. Each repo appears here at most once per session.

- **wow-look-at-my/xml-validator** -- https://github.com/wow-look-at-my/xml-validator
  Strict XML 1.1 validator with optional XSD schema validation. Anything the
  validator does not understand is a hard error ...
```

Ask again in the same session and nothing is added. Ask in a new session and it
comes back.

## Which repositories

By default, the owner of the checkout you are working in — its `origin` remote
— and, failing that, your own GitHub account. Name owners explicitly in either
file to override that:

- `~/.claude/repo-index.json` — every session
- `<project>/.claude/repo-index.json` — that project only

```json
{ "owners": ["wow-look-at-my", "PazerOP"] }
```

That is the whole configuration surface. It names owners, never repositories:
what each repository is comes from the repository.

Archived repositories, forks, and repositories that say nothing about
themselves are left out.

## Refreshing

The index rebuilds itself once a day, in a separate process, so a prompt never
waits on the network. A stale index still suggests; only a missing one stays
quiet, and it says so on stderr along with where its report lands.

To rebuild it now, run the hook binary directly:

```
"$(claude plugin root repo-index)"/build/repo-index --refresh
```

It prints what it indexed and everything it dropped.

Requests go through the `gh` CLI when it is installed, so an existing
`gh auth login` is enough. Otherwise it calls the API directly with `GH_TOKEN`
or `GITHUB_TOKEN`, and without either it still sees public repositories.

## Limits

At most three repositories are added per prompt; any beyond that are named on
stderr, so the cap is never silent. A repository needs more than one
description word in common with your prompt before it earns an injection —
naming it is enough on its own.
