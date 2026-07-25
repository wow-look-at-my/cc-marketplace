---
name: dockerfile
description: Read before writing or editing a Dockerfile, Containerfile, or any `docker build` invocation. Corrects the specific things the model consistently gets wrong about Dockerfiles from stale training data - ADD tar extraction, BuildKit RUN mounts, syntax directives, exec form, ARG/ENV scoping, and cache behavior.
---

# Dockerfile: what is actually true

Notes to self. Every line here was checked against the current Dockerfile reference
(moby/buildkit `frontend/dockerfile/docs/reference.md`) and docs.docker.com's
building best practices. Where my instincts and the docs disagree, **the docs win** -
they always have, every single time this has come up.

## Read this first

**Start every Dockerfile with `# syntax=docker/dockerfile:1`.** Line 1, before
anything, including comments. It makes BuildKit pull the latest stable frontend
instead of the one bundled with the local engine. Without it, half of what follows
silently doesn't exist. Parser directives must be at the very top - once any comment,
blank line, or instruction is processed, BuildKit stops looking, and a later
`# syntax=` line is just a comment.

Second directive worth knowing: `# check=error=true` turns build-check warnings into
failures (pin the syntax version if you use it, or a future check will break the
build). `# check=skip=JSONArgsRecommended,StageNameCasing` skips named checks;
check names are Pascal case and case-sensitive. `docker build --check .` runs the
checks without building.

## ADD - the one I get wrong every time

The reflex is `RUN curl -O ...tar.gz && tar xzf ... && rm ...tar.gz`. Then, when told
to use `ADD`, the follow-up mistake is insisting "ADD doesn't unpack tarballs." Both
are wrong. What the reference actually says:

- **A local tar archive is extracted by default.** Recognized formats: gzip, bzip2,
  xz, or uncompressed. It behaves like `tar -x` and unions with whatever is already
  at the destination.
- Format is detected **from the file contents, not the filename**. An empty file
  named `foo.tar.gz` is just copied, with no decompression error.
- **A remote tar (`<src>` is a URL) is NOT extracted by default.** This is the real
  asymmetry, and the origin of the confused half-memory. To extract it, pass
  `--unpack=true` (Dockerfile v1.17+).

```dockerfile
# syntax=docker/dockerfile:1
FROM alpine
ADD --unpack=true https://example.com/archive.tar.gz /download   # download + extract
ADD --unpack=false my-archive.tar.gz .                           # local, keep packed
ADD local.tar.gz /opt/thing/                                     # local, extracted (default)
```

The docs explicitly prefer `ADD` over hand-rolled `wget`/`curl` + `tar`: it produces a
more precise build cache, and it supports checksum validation, which curl-and-untar
does not.

Other `ADD` facts I under-use:

- `--checksum=sha256:<hash>` verifies an HTTP source; for a Git source the checksum is
  the commit SHA (full or a prefix). Use it.
- Git repositories are first-class sources:
  `ADD git@github.com:moby/buildkit.git#v0.14.1:docs /buildkit-docs` - the fragment is
  `#<ref>:<subdir>`. `.git` is stripped unless `--keep-git-dir=true`. SSH sources need
  `docker build --ssh default`.
- Remote URL files land with mode `0600`. `ADD` itself has no auth flag - use the
  `HTTP_AUTH_HEADER_<host>` / `HTTP_AUTH_TOKEN_<host>` build secrets, or fall back to
  `RUN curl` inside the container.
- Trailing slash on the destination is significant: `ADD x.txt /abs` writes a *file*
  named `/abs`; `ADD x.txt /abs/` writes `/abs/x.txt`.
- Also supports `--chmod`, `--chown`, `--link`, `--exclude` (same semantics as `COPY`).

## COPY

- `COPY --from=` takes a **stage name, a named build context, or an image reference**.
  `COPY --from=nginx:latest /etc/nginx/nginx.conf /nginx.conf` is legal and useful.
  Source paths in `--from` always resolve from the filesystem root of that stage/image.
- **`--link` is recommended by default.** It puts the copied files on their own layer
  that doesn't get invalidated when earlier layers change, which is exactly what makes
  `COPY --from` in multi-stage builds cache well. Only skip it when the destination
  path contains a symlink that must be followed (with `--link`, the destination path is
  always plain directories).
- `--chmod` accepts **symbolic notation** since v1.14, not just octal:
  `COPY --chmod=u=rwX,go=rX . /app/` (capital `X` = executable only if a directory or
  already executable). `--chmod`/`--chown` are unsupported for Windows containers.
- `--parents` (v1.20) preserves source directory structure; `./x/./y/*.txt` pivots on
  the `./` marker like rsync's `--relative`. `**` matches any number of path components.
- `--exclude=<pattern>` (v1.19), repeatable.
- Wildcards use Go `filepath.Match` rules. Escape brackets Go-style: `arr[[]0].txt`.
- Heredoc source creates files inline - no `RUN echo > file` needed:

```dockerfile
COPY <<EOF /etc/greeting.txt
hello world
EOF
```

Quote the delimiter (`<<-"EOT"`) to stop build-time variable expansion; use `<<-` to
strip leading tabs.

## ADD vs COPY, decided

- Copying between stages or from the context into the final image: `COPY`.
- Downloading a remote artifact, or cloning a Git repo: `ADD` (with `--checksum`).
- Needing a context file only for the duration of one `RUN`: **neither** - bind-mount it:

```dockerfile
RUN --mount=type=bind,source=requirements.txt,target=/tmp/requirements.txt \
    pip install --requirement /tmp/requirements.txt
```

The docs say bind mounts are more efficient than `COPY` for this, and the file leaves
no trace in the image.

## RUN and its flags

Shell form and exec form; shell form is normal for `RUN`. Options:

| Flag | Since | Notes |
|---|---|---|
| `--mount` | 1.2 | `bind` (default), `cache`, `tmpfs`, `secret`, `ssh` |
| `--network` | 1.3 | `default`, `none`, `host` (host needs an entitlement) |
| `--security` | 1.20 | `sandbox` (default), `insecure` (needs an entitlement) |
| `--device` | 1.14-labs | CDI devices, needs `docker/dockerfile:1-labs` + BuildKit 0.20+ |

Cache mounts - the thing that replaces "clean up the package cache in the same layer":

```dockerfile
# syntax=docker/dockerfile:1
FROM ubuntu
RUN rm -f /etc/apt/apt.conf.d/docker-clean; echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get --no-install-recommends install -y gcc
```

`sharing=locked` because apt needs exclusive access (`shared` is the default; `private`
forks a new mount per concurrent writer). Do **not** also `rm -rf /var/lib/apt/lists/*`
in this shape - that fights the cache mount. The `rm -rf` cleanup belongs to the
*non*-cache-mount style. Cache contents are never guaranteed; the build must work with
an empty cache.

Secrets - never `ARG` a token:

```dockerfile
RUN --mount=type=secret,id=API_KEY,env=API_KEY some-command --token-from-env $API_KEY
RUN --mount=type=secret,id=aws,target=/root/.aws/credentials aws s3 cp s3://... ...
```

`env=` (mounts as an env var instead of a file) is v1.10+. `required=true` errors when
the secret is missing. Default file mode `0400`, default path `/run/secrets/<id>`.
Secret *contents* are not part of the cache key - changing a secret does not bust the
cache; add a throwaway `ARG CACHEBUST` if a rebuild is needed.

`RUN` cache is never invalidated by the outside world. `RUN apk add curl` a week later
still returns the cached layer. Bust it with `--no-cache`, `--no-cache-filter <stage>`,
`docker builder prune`, or by changing an earlier layer.

Heredocs work in `RUN` and beat `&&`-chaining for anything long:

```dockerfile
RUN <<EOT bash
  set -ex
  apt-get update
  apt-get install -y vim
EOT
```

If the heredoc has a shebang, that interpreter is used.

Pipes: `/bin/sh -c` only checks the exit code of the last stage, so `RUN a | b` succeeds
when `a` fails. Prefix `set -o pipefail &&`, and on Debian's `dash` use exec form with
bash explicitly.

## ENTRYPOINT and CMD

Use **exec form (JSON array, double quotes only)** for both. Shell form wraps the
command in `/bin/sh -c`, so the process isn't PID 1, gets no `SIGTERM` from
`docker stop`, and takes ~10s plus a SIGKILL to die. The `JSONArgsRecommended` build
check flags this.

Shell-form `ENTRYPOINT` **ignores `CMD` and any `docker run` arguments entirely.** The
interaction table:

|  | No ENTRYPOINT | `ENTRYPOINT exec_entry p1` (shell) | `ENTRYPOINT ["exec_entry","p1"]` |
|---|---|---|---|
| No CMD | error | `/bin/sh -c exec_entry p1` | `exec_entry p1` |
| `CMD ["exec_cmd","p1"]` | `exec_cmd p1` | `/bin/sh -c exec_entry p1` | `exec_entry p1 exec_cmd p1` |
| `CMD exec_cmd p1` (shell) | `/bin/sh -c exec_cmd p1` | `/bin/sh -c exec_entry p1` | `exec_entry p1 /bin/sh -c exec_cmd p1` |

Also: setting `ENTRYPOINT` **resets any `CMD` inherited from the base image** to empty.
If the image needs default args, re-declare `CMD` in this Dockerfile. Only the last
`ENTRYPOINT`/`CMD` in a stage takes effect.

Need shell features (globs, pipes, `&&`) at runtime? Write an entrypoint script and
`ENTRYPOINT ["/entrypoint.sh"]` - and `exec "$@"` at the end of it so the real process
becomes PID 1. Alternatively declaring `SHELL ["/bin/bash","-c"]` marks shell form as a
conscious choice and suppresses the check.

## ARG and ENV

- **`ENV key=value`, `ARG key=value`.** The space-separated legacy form (`ENV key value`)
  is deprecated and flagged by `LegacyKeyValueFormat`. Multi-line values go in quotes:
  `ENV DEPS="\` + continuation lines + `"`.
- An `ARG` declared **before the first `FROM` is global scope only** - usable in `FROM`
  lines, invisible inside stages. To use it in a stage, re-declare bare `ARG VERSION`
  inside that stage.
- An `ARG` declared in a stage is inherited by stages built `FROM` it, never by
  unrelated stages.
- `ENV` beats `ARG` of the same name for the rest of the stage.
- `ARG` values are **not secret**: visible in `docker history` and in max-mode
  provenance attestations (attached by default with the Buildx GitHub Action on public
  repos). Use `RUN --mount=type=secret`.
- `ENV` persists into the final image *and* into the layer it was set on - a later
  `RUN unset X` does not scrub it. Set-use-unset within one `RUN` instead, or use `ARG`.
- Automatic platform args exist in global scope and need re-declaring per stage:
  `TARGETPLATFORM`, `TARGETOS`, `TARGETARCH`, `TARGETVARIANT`, `BUILDPLATFORM`,
  `BUILDOS`, `BUILDARCH`, `BUILDVARIANT`.
- Proxy args (`HTTP_PROXY`, `NO_PROXY`, `ALL_PROXY`, lower-case variants, ...) are
  predefined, excluded from `docker history`, and cache-exempt unless explicitly
  re-declared with `ARG`.
- Bash-style modifiers work in the builder: `${VAR:-default}`, `${VAR-default}`,
  `${VAR:+alt}`, `${VAR+alt}`. Pattern operators (`${var#pat}`, `${var/a/b}`) are
  **pre-release only** (`docker/dockerfile-upstream:master`) - don't reach for them.
- Substitution happens only in `ADD`, `COPY`, `ENV`, `EXPOSE`, `FROM`, `LABEL`,
  `STOPSIGNAL`, `USER`, `VOLUME`, `WORKDIR`, and `ONBUILD` wrapping one of those. In
  `RUN`/`CMD`/`ENTRYPOINT` the *shell* does it - which means exec form does no
  substitution at all. `RUN ["echo", "$HOME"]` prints the literal string.

## Everything else worth not getting wrong

- **`MAINTAINER` is deprecated.** Use `LABEL org.opencontainers.image.authors="..."`.
- `EXPOSE` publishes nothing. It is documentation plus a target for `docker run -P`.
  `EXPOSE 80/udp` needs its own line alongside `EXPOSE 80/tcp`. IP addresses and
  host-port mappings in `EXPOSE` are invalid and will become an error.
- `WORKDIR` should always be absolute; relative paths stack onto the previous
  `WORKDIR`, and a base image may set one you didn't expect (`WorkdirRelativePath`
  check). It creates the directory if missing.
- `VOLUME`: the old lore "changes to a volume path after `VOLUME` are discarded" is
  **legacy-builder behavior**. Under BuildKit the changes are kept. Still can't specify
  a host path - that's runtime-only.
- `USER`: give an explicit UID/GID if it matters (allocation order isn't stable across
  rebuilds). `useradd --no-log-init` avoids the sparse-file disk-exhaustion bug for
  large UIDs. Avoid `sudo`; use `gosu` if root-then-drop is needed.
- `HEALTHCHECK` options: `--interval` (30s), `--timeout` (30s), `--start-period` (0s),
  `--start-interval` (5s, needs Engine 25.0+), `--retries` (3). One per Dockerfile;
  `HEALTHCHECK NONE` disables an inherited one. Exit 0 healthy, 1 unhealthy, 2 reserved.
- `STOPSIGNAL` affects `docker stop` only, not Ctrl+C (which sends SIGINT directly).
- `LABEL`: combining labels into one instruction stopped mattering after Docker 1.10.
  Use double quotes - single quotes suppress interpolation. Labels from a stage that is
  only referenced via `COPY --from`/`RUN --mount=from=` are **not** inherited; only the
  final `FROM` chain contributes.
- `ONBUILD` can't chain (`ONBUILD ONBUILD`) and can't trigger `FROM`/`MAINTAINER`. Since
  v1.11 it may carry `COPY --from`/`RUN --mount=from=`.
- `SHELL` must be JSON form. Linux default `["/bin/sh","-c"]`, Windows `["cmd","/S","/C"]`.
- Windows: `# escape=\`` ` as the first line, because `\` is the path separator.
- Multi-stage: BuildKit only builds stages the `--target` actually depends on (the
  legacy builder built everything up to the target). `FROM <earlier-stage>` to branch.
- Pin base images with a tag **and** a digest (`FROM alpine:3.21@sha256:...`) when supply
  chain matters. `docker build --pull` refreshes the base image;
  `docker build --no-cache` re-executes layers. They are different flags for different
  jobs and compose fine together.
- `ADD`/`COPY` cache checksums come from file metadata but **ignore `mtime`**. Every
  other instruction is cached on the instruction string alone.
- `SOURCE_DATE_EPOCH` participates in `WORKDIR` cache validity - a per-commit value
  breaks the cache from that point on.
- `.dockerignore` keeps the context small; the `CopyIgnoredFile` check catches copying
  something the ignore file excluded.
- Sort multi-line package lists alphanumerically; combine `apt-get update &&
  apt-get install` in one `RUN` (separate `RUN`s produce a stale-index cache bug).
  `--no-install-recommends` always. Official Debian/Ubuntu images already run
  `apt-get clean`, so don't add it.

## Before saying it's done

Run `docker build --check .` if a daemon is available. It catches stage-name casing,
`FROM ... as` casing, JSON args, legacy key/value, undefined vars, undeclared `ARG` in
`FROM`, secrets in `ARG`/`ENV`, duplicate/reserved stage names, redundant
`--platform=$TARGETPLATFORM`, empty continuation lines, and more - cheaply, and without
guessing.
