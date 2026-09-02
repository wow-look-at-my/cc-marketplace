---
description: Read before designing how software gets INTO a container - choosing a base image, deciding whether something should be a layer or a mounted volume, splitting one image into several, or wiring the CI that builds them. Corrects the reflex of treating an image as a file-delivery format and hand-rolling what layer sharing already does. For Dockerfile syntax itself, read /docs:dockerfile.
---

# Docker images: what layers actually are, and what that means for design

Notes to self. `/docs:dockerfile` covers writing the file; this covers deciding
what the images should BE. Written after getting it wrong in a real codebase and
being corrected by the person who owned it.

## The failure this exists to prevent

A host ran agent sessions in containers. There was one big runtime image
(terminal, tmux, git, toolchains) and six CLI agents. The design I defended:

- build one "payload" image per agent, each `FROM ubuntu`, containing only that
  agent's binaries under `/agent`;
- `docker run` each payload image once, purely to `cp -a /agent/.` into a named
  volume;
- start the session from the RUNTIME image with that volume mounted read-only at
  `/agent`.

So the payload image's own filesystem was never a container root. It existed to
carry a directory. Asked why the agents were not simply `FROM` the runtime
image, I argued layering would save nothing, because the only layer it would
share (`ubuntu:24.04`) was already shared. The arithmetic was right and the
conclusion was wrong: **I was comparing byte counts and missing that I had
reimplemented distribution.** The reply was "I don't think you understand how
docker is supposed to work," and it was correct.

What the copy-into-a-volume design cost, none of which is a byte count:

- a marker file (`/agent/.agent-host-image`) and refresh logic, to answer
  "is the volume's content still the right version?" -- a question a tag answers;
- a `docker run` per agent per host, before any session can start;
- a volume per agent that nothing garbage-collects;
- weight nobody could see. One agent image shipped 831 MiB against ~95 MiB for
  its siblings -- a whole build toolchain and a second Node install -- because a
  standalone carrier image has no base to be a diff against, so nothing looked
  wrong. Layering makes that visible as "this child adds 700 MiB".

The rule I should have started from: **layer the program files, mount only the
mutable state.** An image is the unit of distribution AND the unit of execution.
Splitting those two apart is the smell.

## Layers, and why the sharing argument is not about your bytes

An image is an ordered stack of layers. Each layer has a SHA256 content
identifier, and identical layers are one object on disk:

> "Shared image layers are only stored once in `/var/lib/docker/`"
> "Docker already has all the layers from the first image, so it doesn't need to
> pull them again."
> -- docs.docker.com, Storage drivers

Consequences worth holding onto:

- **A child image's cost is its own layers.** `FROM big-base` then one `RUN`
  transfers and stores that one layer for anyone who has the base.
- **Sharing is by digest, not by name.** Two images share a layer when the layer
  is byte-identical, whatever the tags say. Rebuilding a base non-reproducibly
  gives a new digest, and every child that referenced it stops sharing.
- **"Layering saves no bytes here" can be TRUE and still not be the argument.**
  It was true in the case above. It was also irrelevant: the win is that the
  daemon does the distribution, versioning and identity, and you delete the code
  that was doing it by hand.
- **Layer count is not the metric.** Squashing to save a layer trades away
  sharing and cache hits.

## Image or volume: the actual test

Ask what the bytes ARE, not how they are shaped.

| The bytes are | Where they belong |
|---|---|
| Program files, libraries, a CLI, static assets | A layer. Immutable, versioned by tag/digest, shared. |
| State the container writes and must survive it | A volume. |
| Config for one deployment | Env vars, secrets, or a mounted file. |
| A build tool needed to produce the program files | A builder stage that never ships. |

Reaching for a volume to deliver read-only program files is the wrong direction
every time. It is the pattern above, and it always re-implements some of: pull,
versioning, garbage collection, integrity, and "is this the version I expect?".

The reverse mistake exists too: baking a database's data directory, a cache, or
per-deployment credentials into an image. Anything a running container mutates
belongs in a volume, because a layer is copy-on-write per container and vanishes
with it.

## What a child image inherits, and what will bite you

`FROM` inherits the base's filesystem AND its configuration. That is the point,
and it is also where a copy-based design's habits break when you convert it:

- **`ENTRYPOINT` and `CMD` are inherited.** A child of an image with an
  `ENTRYPOINT` needs `docker run --entrypoint ...` to run something else -- a
  bare `docker run child mytool --version` passes `mytool --version` as
  ARGUMENTS to the inherited entrypoint. Nothing warns you.
- **`ENV` is inherited, and becomes the container's environment.** A build-time
  `ENV HOME=/agent` was harmless while the image was only a carrier; the moment
  it became the image a container RUNS, it redirected every home-directory
  lookup. Set build-only variables per-`RUN` (`RUN export HOME=/agent && ...`),
  never as `ENV`.
- **`LABEL`, `WORKDIR`, `EXPOSE`, `USER`, `VOLUME` are inherited too.** Inherited
  labels are useful: a child carrying the base's version label is checkable
  evidence it really was built on that base.
- **`ARG` is not inherited the way you expect** -- and a global `ARG` (before the
  first `FROM`) is the ONLY kind usable in a `FROM` line. Declared after a
  `FROM` it belongs to that stage, resolves empty in the next `FROM`, and the
  build fails on an invalid reference. `/docs:dockerfile` has the full scoping
  table; I have gotten this wrong with the correct answer already written down,
  so read it rather than reasoning it out.

## Parameterizing the base

A child that must build against different bases (a local dev build vs an
exact-SHA published one) takes the base as a global ARG:

```dockerfile
# syntax=docker/dockerfile:1
ARG BASE_IMAGE=myapp-runtime          # before the first FROM, or it is unusable below
FROM ${BASE_IMAGE}
RUN install-one-thing
```

Built with `--build-arg BASE_IMAGE=registry.example.com/myapp-runtime:<sha>`.
Keep the child's tag equal to the base's: a child tagged with a different
revision than the base it was built on is a lie a reader cannot detect.

## CI: build order follows FROM

This is where the design change actually shows up in a pipeline, and it is easy
to get wrong because a matrix looks so tidy:

- **A base and its children cannot build in parallel.** The children need the
  base to exist first. That means separate jobs with `needs:`, not one matrix.
- **The child's builder pulls the base from the REGISTRY**, so the base has to be
  pushed, not merely built, before the child starts -- and the child's job needs
  registry credentials to pull a private base. This is easy to miss when the
  push path authenticates some other way (a CLI with its own token), because
  then nothing else in the job ever needed a `docker login`.
- **Put that login in the build action, not in every caller.** A caller adding a
  login step to work around an action that cannot pull its own registry's images
  is a workaround; fix the action.
- **A stale base is a real failure mode.** If children are rebuilt without the
  base, they silently keep the old runtime. Tie them together with an exact tag,
  and assert it: an inherited `LABEL` from the base is a cheap check that the
  child really was layered on the version you think.

## Multi-stage: the toolchain never ships

If producing the program files needs a compiler, the compiler goes in a builder
stage and the shipped stage copies the result:

```dockerfile
ARG BASE_IMAGE=myapp-runtime
FROM ubuntu:24.04 AS builder
RUN apt-get update && apt-get install -y --no-install-recommends g++ make python3
RUN build-the-thing --out /opt/thing

FROM ${BASE_IMAGE}
COPY --from=builder /opt/thing /opt/thing
```

The builder stage can be `FROM` anything -- nothing it contains is distributed.
Single-stage builds are how 700 MiB of `g++`, `python3` and an npm cache end up
in an image whose consumers only ever needed one directory.

## Before writing the design down, check these

- Am I moving read-only program files with anything other than a layer? Stop.
- Does the "carrier" image I am about to write ever run as a container? If not,
  it should be a stage or a base, not an image.
- Did I compare byte counts and conclude layering is pointless? Byte counts are
  not the argument; owning distribution code is.
- Does the child re-install what the base already has? Delete it -- `apt-get
  install ca-certificates curl` in a child of an image that has them is pure
  duplication.
- Will an inherited `ENV`/`ENTRYPOINT` change how this image runs?
- Is anything mutable baked into a layer, or any program file in a volume?

## Verify, don't recall

Registry manifests give real numbers without pulling anything:

```sh
curl -sH "Authorization: Bearer $TOKEN" \
  -H 'Accept: application/vnd.oci.image.manifest.v1+json' \
  "https://registry.example.com/v2/<project>/manifests/<tag>" |
  jq '{layers: (.layers|length), mib: (([.config.size]+[.layers[].size]|add)/1048576|round)}'
```

That is how the 831 MiB outlier above was found -- after it had already shipped.
`docker history <image>` attributes size per layer once the image is local.

## Keep this file growing

This skill exists because I was wrong in a way that a page of docs would not have
caught -- the mistake was architectural, not syntactic. **When something about
established Docker design surprises you, add it here in the same shape: what the
instinct was, what is actually true, and the evidence.** A surprise is the signal
that a wrong model just got corrected, and the correction is worth more written
down than remembered. Verify against docs.docker.com, the OCI image-spec, or the
observed behavior of a real daemon before writing it; a confidently wrong note in
this plugin is worse than no note, because it loads automatically.
