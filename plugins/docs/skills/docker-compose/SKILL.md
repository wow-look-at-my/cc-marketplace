---
description: Read before writing or editing a compose.yaml, compose.yml, docker-compose.yml, or any `docker compose` invocation. Corrects the specific things the model consistently gets wrong about Compose from stale training data - the obsolete top-level `version` key, depends_on health conditions, file naming, merge/override rules, watch, lifecycle hooks, and v2/v5 CLI differences.
---

# Docker Compose: what is actually true

Notes to self. Checked against the current Compose Specification pages on
docs.docker.com (`content/reference/compose-file/**`) and the Compose manuals. Where my
instincts and the docs disagree, **the docs win**.

## The three reflexes to unlearn immediately

1. **Do not write `version:`.** The top-level `version` property is *obsolete*. Compose
   always validates against the newest schema regardless of it, and emits a warning
   telling you it's obsolete. `version: "3.8"` at the top of a file is a tell that the
   output came from 2019 training data. Delete it from files being edited too.
2. **The file is `compose.yaml`.** That's the preferred name, then `compose.yml`.
   `docker-compose.yaml` / `docker-compose.yml` are supported for backward
   compatibility only, and `compose.yaml` wins if both exist. When creating a new file,
   create `compose.yaml`. When editing an existing `docker-compose.yml`, leave the name
   alone - renaming is not the task.
3. **The command is `docker compose`, two words.** `docker-compose` is Compose v1
   (Python, 2014). Compose v2 (Go, 2020) ignores `version:` entirely; **Compose v5
   (2025)** is functionally identical to v2 and adds an official Go SDK - the jump from
   2 to 5 exists to avoid colliding with the old "file format v2/v3" names. Compose file
   format 1 (no `services:` key) does not run at all on v2/v5.

## `depends_on` does not do what it looks like it does

Short syntax waits for the dependency to be **started**, not healthy, not ready:

```yaml
depends_on: [db]     # db has been created/started. That is all.
```

To actually wait, use long syntax with a condition:

```yaml
services:
  web:
    depends_on:
      db:
        condition: service_healthy
        restart: true          # restart web when db is updated (Compose 2.17+)
      migrations:
        condition: service_completed_successfully
      cache:
        condition: service_started
        required: false        # warn instead of fail if absent (Compose 2.20+)
```

`service_healthy` requires the dependency to actually declare a `healthcheck` (or
inherit one from its image). Conditions: `service_started`, `service_healthy`,
`service_completed_successfully`.

For one-shot setup work there is now a better tool than a fake dependency service -
see `pre_start` below.

## Networking: stop adding `links`

Services on a shared network reach each other **by service name**, on any port. `links`
is not required for that and doesn't override network configuration. `expose` is also
usually unnecessary - ports a container listens on are reachable from the same network
whether or not `expose` is declared (and any `EXPOSE` in the image already covers it).

- No `networks:` anywhere means every service joins an implicit `default` network. That
  is a real network, not "no network." To genuinely detach, `network_mode: none`.
- `network_mode` and `networks` are mutually exclusive - Compose rejects a file with both.
- `ports` must not be combined with `network_mode: host` (runtime error).
- Customize the implicit network by declaring `networks: {default: {name: a_network}}`.
- Per-network service options: `aliases`, `ipv4_address`/`ipv6_address` (needs matching
  `ipam` subnets), `interface_name`, `mac_address`, `link_local_ips`, `driver_opts`,
  `priority`, `gw_priority`. **`priority` picks the network for a service-level
  `mac_address`; `gw_priority` picks the default gateway.** They are different keys and
  neither controls `ethN` naming.

## `ports`

Short form is `[HOST:]CONTAINER[/PROTOCOL]`, and **must be quoted** - `8080:80`
unquoted can be read as a YAML base-60 float.

Omitting the host IP binds `0.0.0.0`, which **bypasses host firewall rules** and can
expose the container to the internet on a public-IP host. Bind explicitly when the port
is only for local use: `"127.0.0.1:8080:80"`. IPv6 goes in brackets: `"[::1]:6001:6001"`.

Long form fields: `name`, `target`, `published` (string, may be a `start-end` range),
`host_ip`, `protocol`, `app_protocol`, `mode` (`ingress`|`host`, Swarm).

## Development loop: `develop.watch`, not bind mounts

```yaml
services:
  web:
    build: .
    develop:
      watch:
        - path: ./src
          action: sync
          target: /app/src
          ignore: [node_modules/]
        - path: ./package.json
          action: rebuild
        - path: ./etc/config
          action: sync+exec
          target: /etc/config/
          exec: {command: app reload}
```

Actions: `sync`, `rebuild`, `restart` (2.32+), `sync+restart` (2.23+), `sync+exec`
(2.32+). Extras: `include` (allowlist patterns - quote them, a leading `*` is a YAML
alias node), `ignore` (`.dockerignore` syntax, and the build context's `.dockerignore`
is loaded implicitly), `initial_sync`.

Prerequisites that bite: the **image must contain `stat`, `mkdir`, and `rmdir`**, and the
container `USER` must be able to write the target path (so `COPY --chown` the initial
content in the Dockerfile). Run it with `docker compose up --watch` or
`docker compose watch`.

## Lifecycle hooks and init containers

Not a bind-mount hack, not a wrapper entrypoint - these exist as first-class keys:

- **`pre_start`**: a sequence of ephemeral init containers run before the service
  container starts, in order, each must exit 0. Fields: `command`, `image` (defaults to
  the service's image), `user`, `privileged`, `working_dir`, `environment`,
  `per_replica: false`. They run *after* `depends_on` conditions are satisfied, join the
  service's networks, and share its volume mounts. A successful step is not re-run on a
  later `up` unless its definition changes or the service is recreated. This is the
  right place for migrations and chowning a volume.
- **`post_start`**: commands run inside the running container after it starts (exact
  timing not guaranteed).
- **`pre_stop`**: same shape, before a deliberate stop. Does not run if the container
  dies on its own.

## Merging, overriding, and modularizing

Three different mechanisms, routinely confused:

- **Multiple `-f` files / `compose.override.yaml`** - merged. Mappings merge, sequences
  **append**. Exceptions: `command`, `entrypoint`, and `healthcheck.test` are
  *replaced*, not appended. `ports`/`volumes`/`secrets`/`configs` merge by unique key
  (`target` for the last three; `{ip, target, published, protocol}` for ports).
- **`!reset` / `!override` YAML tags** - `ports: !reset []` clears an inherited value;
  `ports: !override [...]` replaces instead of appending. Without `!override`, both the
  base and the override ports end up published.
- **`extends`** - pulls one service definition into another (`{file:, service:}`).
  It does **not** import the referenced service's `volumes`/`networks`/`depends_on`
  targets; those must be declared locally. No circular references. Unsupported with
  `docker stack deploy`.
- **`include:`** - top-level, pulls in whole Compose applications, each loaded with its
  own project directory so relative paths resolve against *their* file. Evaluated after
  the main files are merged; name conflicts warn, they don't merge. Recursive. Long
  form takes `path` (string or list), `project_directory`, `env_file`.

## Variables

- `${VAR}`, `${VAR:-default}`, `${VAR-default}`, `${VAR:?err}`, `${VAR?err}`,
  `${VAR:+alt}`, `${VAR+alt}`, and nesting (`${A:-${B:-x}}`). Nothing else -
  `${VAR/foo/bar}` is not supported.
- `$$` is a literal `$`. Needed whenever a value contains a shell variable the container
  should expand: `command: /bin/sh -c 'echo "hello $$HOSTNAME"'`.
- Interpolation applies to **values, not keys**. For `labels`/`environment`, that means
  the `- "KEY=value"` list form interpolates a variable in the key position and the
  `KEY: value` map form does not.
- Unresolved and undefaulted -> warning + empty string, not an error. Use `:?` when it
  must be set.
- Anchors/aliases (`&x` / `*x`) resolve **before** interpolation, so variables can't
  name anchors. YAML merge (`<<:`) works on mappings only, never sequences - which is
  why `environment` must use the `KEY: value` map form when merging fragments.
- `x-` prefixed keys are extension fields and are legal anywhere user keys aren't
  expected; the usual pattern is `x-common: &common` at top level.

## Fields that solve problems I try to solve manually

- **`command` does not run in a shell.** Unlike Dockerfile `CMD`, it isn't wrapped by
  the image's `SHELL`. Anything relying on expansion needs an explicit
  `/bin/sh -c '...'`. List form = exec form.
- **`healthcheck.test`**: as a list, the first element must be `NONE`, `CMD`, or
  `CMD-SHELL`. As a bare string, it's implicitly `CMD-SHELL`. `disable: true` (or
  `test: ["NONE"]`) kills an inherited healthcheck. `interval`/`timeout`/`start_period`/
  `start_interval` take duration strings (`1m30s`).
- **`secrets`** top-level source is `file:` or `environment:` (`environment` is
  Compose-only, not `docker stack deploy`). Mounted read-only at
  `/run/secrets/<name>`. `uid`/`gid`/`mode` are **silently ignored** for `file:` secrets
  (they're bind-mounted underneath).
- **`configs`** top-level source is `file:`, `environment:`, `content:` (inline, and
  interpolated - handy for generating a config from env), or `external: true`.
- **`env_file`** entries may be mappings: `{path: ./x.env, required: false}` and
  `{format: raw}` (no interpolation, `$` and quotes passed through). `environment:`
  always wins over `env_file`, even for empty values. In `.env` files, inline comments
  on unquoted values need a preceding space.
- **`pull_policy`**: `always`, `never`, `missing` (default), `build`, `daily`, `weekly`,
  `every_<duration>` (e.g. `every_12h`). `latest` is always pulled even under `missing`.
- **`restart`**: `no` (default - **quote it**, bare `no` is YAML false), `always`,
  `on-failure[:max-retries]`, `unless-stopped`.
- **`profiles`**: services without a profile always run. A service named explicitly on
  the CLI activates its own profile. `links`/`extends`/`service:x` references do *not*
  auto-enable a profiled service - they error.
- **`gpus: all`** (or a list of `{driver, count}`) instead of the old
  `deploy.resources.reservations.devices` incantation.
- **`models:`** top level + `models:` per service - Docker Model Runner integration.
  Compose injects `<MODEL_KEY>_URL` (uppercased, `-`->`_`) or the names given by
  `endpoint_var`/`model_var`.
- **`provider:`** - hands a service's lifecycle to an external binary
  (`{type:, options:}`); dependents get `<SERVICE>_<VAR>` env vars back.
- **`use_api_socket: true`** mounts the engine socket + credentials for containers that
  need to push/pull.
- **`attach: false`** stops Compose collecting that service's logs.
- **`label_file`** loads labels from a file, like `env_file`; `labels:` wins on conflict.
- **`init: true`** for signal forwarding / zombie reaping when the image's PID 1 can't.
- `container_name` **prevents scaling** past one container. Don't set it on anything
  that might be scaled.

## Volumes

- Short form: `VOLUME:CONTAINER_PATH[:ACCESS_MODE]` where access mode is a comma list of
  `rw`/`ro`/`z`/`Z`. Relative host paths must start with `.` or `..` to avoid being read
  as a named volume, and only work for local runtimes.
- Short-form bind mounts **create the host directory if missing** (legacy compatibility).
  Use long form with `bind: {create_host_path: false}` to stop that.
- Long form `type:` is `volume`, `bind`, `tmpfs`, `image`, `npipe`, or `cluster` -
  `image` mounts content straight out of an image (`image: {subpath: ...}`, 2.35+), and
  `volume: {subpath: ...}` mounts a subdirectory of a named volume.
- A named bind mount (stable name, fixed host path) is the `local` driver:

```yaml
volumes:
  app-data:
    driver: local
    driver_opts: {type: none, o: bind, device: /srv/app-data}   # absolute, must exist
```

- `external: true` means "already exists, don't create it, error if absent" - and then
  every attribute except `name` is rejected.

## Build

`build:` is an optional part of the spec but fully supported by Compose.

- String form = context path (or a Git URL with a `#ref:subdir` fragment).
- `dockerfile:` is resolved **relative to the context**, not the Compose file - so
  `{context: backend, dockerfile: ../backend.Dockerfile}` refers to a file next to the
  Compose file. `dockerfile` and `dockerfile_inline` are mutually exclusive.
- `target:` picks a multi-stage stage. `tags:` adds tags beyond `image:`.
- `additional_contexts` accepts paths, Git URLs, `docker-image://ref`, and
  `service:<name>` to build on another service's image.
- `secrets` here maps a top-level secret to a Dockerfile
  `RUN --mount=type=secret,id=<target>`; `ssh: [default]` forwards the agent.
- `platforms`, `cache_from`/`cache_to`, `provenance`, `sbom`, `entitlements`,
  `no_cache`, `pull`, `network` (incl. `none`).
- With both `build` and `image` and no `pull_policy`, Compose tries to pull first and
  builds only if the pull fails. Push skips services with no `image:`.

## Misc facts I get wrong

- Booleans in `environment:` must be quoted (`SHOW: "true"`) or the YAML parser turns
  them into `True`/`False`.
- A single-key `environment` entry with no value (`USER_INPUT:`) passes the host value
  through, and unsets the variable if the host doesn't have it.
- `deploy:` is an optional spec; it's ignored rather than invalid when unsupported.
  Non-Swarm resource limits also exist as flat service keys (`cpus`, `mem_limit`,
  `mem_reservation`, `pids_limit`, `shm_size`, `ulimits`) and must stay consistent with
  their `deploy.resources` counterparts if both are set.
- The `com.docker.compose` label prefix is reserved; using it is a runtime error.
  Compose always sets `com.docker.compose.project` and `com.docker.compose.service`.
- `stop_grace_period` defaults to 10s before SIGKILL; `stop_signal` changes the signal.
- Project name comes from top-level `name:`, is exposed as `COMPOSE_PROJECT_NAME`, and
  can be overridden per-invocation.
