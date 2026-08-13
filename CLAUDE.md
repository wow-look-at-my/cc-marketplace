## Misc Skills Plugin

The misc-skills plugin lives at `plugins/misc-skills/`. No code, no hooks -- a home for standalone skills that are worth having but do not
justify a plugin each. A skill graduates out of here into its own plugin (or into `docs`) once it has siblings that share a theme.

- **`skills/new_model_pass/SKILL.md`** (`/misc-skills:new_model_pass`) -- a full improvement pass over an existing codebase: map it, read
  it whole, fix what is wrong, verify, report. Written for the case it is named after -- a new model released, pointed at a repo older
  models have already been over -- so it assumes the cheap findings are gone and ranks effort toward defects that need several files held
  at once (contract mismatches, invariants broken elsewhere, silent failure, tests that assert the easy half, stale API knowledge). It is
  deliberately NOT a security audit: hardening means failing loudly and handling real inputs, not bolting auth onto a local script. It
  also bans churn (reformatting, renames for taste, one-caller abstractions), because a taste diff burying three real fixes is this
  skill's actual failure mode. The report's four sections -- fixed / found and left / checked and fine / not covered -- exist so two
  models' passes over the same repo can be compared; "checked and fine" is what stops silence about a subsystem reading as coverage.
  `disable-model-invocation: true`, unlike the `docs` plugin's skills: this one starts a large sweep and must never auto-load.

Underscores in a skill directory name are fine (verified on 2.1.223: `ptest:under_score_test` registered alongside a hyphenated sibling),
so the directory keeps the name it was asked for. As in `docs`, skills here omit the front-matter `name` field -- the directory already
determines the command, and setting `name` only additionally registers the bare form as a root-namespace alias.
