# docs

Corrective reference notes for topics where Claude's training data is reliably stale or
wrong. Each skill is a set of notes the model writes for itself, checked against the
upstream documentation, and loads before touching the relevant file type.

## Skills

| Skill | Loads before |
|-------|--------------|
| `/docs:dockerfile` | Writing or editing a Dockerfile, or a `docker build` command |
| `/docs:docker-compose` | Writing or editing a `compose.yaml`, or a `docker compose` command |
| `/docs:node-typescript` | Deciding whether a file must be JavaScript, adding a build step or a `.d.ts` to ship TypeScript, or debugging a `.ts` import that will not resolve |

The skills are model-invoked: their descriptions are written so Claude pulls them in on
its own when it is about to edit one of these files. Invoking them by name also works.

## Adding a skill

Add `skills/<topic>/SKILL.md` with a `description` in the front matter. Write the
description as a trigger ("Read before ..."), not a summary — it is the only thing the
model sees when deciding whether to load the skill.

Do not set a `name`. The directory name already determines the command
(`skills/dockerfile/` → `/docs:dockerfile`); adding `name` additionally registers the
bare `/dockerfile` as an alias, which pollutes the root slash namespace.

Write the body as notes to yourself, not documentation for a human. State what is
actually true, name the specific wrong instinct it replaces, and cite the behavior
rather than the vibe. Verify every claim against upstream docs before writing it down.

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install docs
```
