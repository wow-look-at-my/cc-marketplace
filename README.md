# misc-skills

Standalone skills with no theme in common. A skill lands here when it is worth having
but does not justify a plugin of its own.

## Skills

| Skill | Use for |
|-------|---------|
| `/misc-skills:new_model_pass` | Pointing a newly released model at an existing codebase to find and fix what earlier models missed |

### `/misc-skills:new_model_pass`

Runs a full improvement pass: read the code end to end, find everything worth fixing,
fix it, verify it, and report. It steers the pass toward defects that need a lot of
the codebase held at once — contract mismatches, broken invariants, silent failure,
tests that assert the easy half — because the cheap findings are already gone by the
time a new model gets there. It is explicitly not a security audit, and it forbids
churn: no reformatting, no renaming for taste, no abstraction with one caller.

The final report separates what was fixed, what was found and deliberately left, what
was read and found healthy, and what was not covered — so two models' passes over the
same repo can actually be compared.

Manual only (`disable-model-invocation: true`). It kicks off a large sweep, so it runs
when you ask for it and never on its own.

```
/misc-skills:new_model_pass                 # whole repo
/misc-skills:new_model_pass src/parser      # one subsystem
```

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install misc-skills
```
