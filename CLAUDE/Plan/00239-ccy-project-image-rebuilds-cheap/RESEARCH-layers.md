# Plan 00239 research: why a ccy release rebuilds every project image, and what would stop it

The owner's question: projects whose `.claude/ccy/Dockerfile` compiles a lot are expensive to
rebuild, and nearly every ccy release makes every project rebuild. Is there a podman or
Docker practice, for example around layers, that would make a new ccy image much cheaper?

The owner's constraint, added during this research: cheaper rebuilds must never lock ccy to an
out-of-date or insecure base image. Every option below that caches or pins says what refreshes
the cached part, how a stale base is detected and reported, and which events force a full
rebuild whatever the cache holds (section 4).

Everything here is read from the code at F44 `dcc5721c8` (CCY 3.102.1, container 2.79.15).
Line numbers refer to that commit. The podman and Docker behaviour is cited from their
documentation. Anything the documentation does not settle is listed as a measurement in
section 7 and is not taken as given.

## 1. What triggers a project image rebuild today

### 1.1 The project image check

`files/var/local/claude-yolo/claude-yolo:2084-2150`. A project with `.claude/ccy/Dockerfile`
gets `claude-yolo:<project>`, rebuilt when any of these holds:

| Trigger            | What is compared                                                                                                                 | Where            |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| first time         | `podman image inspect claude-yolo:<project>` fails                                                                               | `:2112`          |
| Dockerfile changed | md5 (16 hex) of `.claude/ccy/Dockerfile` against `~/.cache/claude-yolo-<project>-dockerfile-hash`                                | `:2085`, `:2116` |
| base image updated | the **image ID** of `claude-yolo:latest` against `~/.cache/claude-yolo-<project>-base-image`                                     | `:2096`, `:2120` |
| `--rebuild`        | always, except `--rebuild=claude`, which updates Claude Code in the project image in place and rewrites both stamp files instead | `:2137-2147`     |

The base comparison uses the image ID, not the version label, on purpose (comment at
`:2091-2095`): the build context (the pingbus zipapp, the plugin) can change without a
version bump, and a project image left on the old base would run old code.

Two things follow from comparing the image ID of `claude-yolo:latest`:

- **A project that starts `FROM claude-yolo:base` is compared against `:latest` anyway.**
  `:2096` inspects `claude-yolo:latest` whatever the project's `FROM` line names, so such a
  project rebuilds when `:latest` moves even if `:base` did not.
- **Anything that commits a new `:latest` triggers a full rebuild of every project image**,
  not only a ccy release. Section 1.3 shows that Claude Code's daily in-place update is such a
  commit.

The build itself (`:2170-2179`) is
`DOCKER_BUILDKIT=1 podman build --build-arg BUILDKIT_INLINE_CACHE=1 --build-arg DOCKERFILE_HASH=… -t claude-yolo:<project> -f .claude/ccy/Dockerfile .`.
Under podman, `DOCKER_BUILDKIT` and `BUILDKIT_INLINE_CACHE` do nothing: buildah is not
BuildKit and has no inline cache. The message printed beside it ("Building with Docker
BuildKit for optimal caching", `:2155`) is wrong on podman. The build uses podman's default
layer cache (`--layers`, default true), so after a base change every project layer misses:
each layer's cache entry hangs off its parent image, and the parent is the new `:latest`.

### 1.2 What a ccy release changes in `:base` and `:latest`

`claude-yolo:latest` is rebuilt when its `claude-yolo-version` label is not the launcher's
`REQUIRED_CONTAINER_VERSION`, or its `claude-yolo-dockerfile-hash` label is not the md5 of the
deployed Dockerfile (`lib/common.bash:934-1011`, called from `lib/image-build.bash:13-20` and
`claude-yolo:2056`). The play also runs `podman build` every time (`tasks/claude-yolo-image.yml:436-457`;
"the play always builds"), with the layer cache on, so a play run with an unchanged build
context should be a full cache hit with the same image ID. Section 7 measures that.

The project rule (`CLAUDE/ContainerRules.md`, "CCY Version Bump Requirement") requires a
container version bump for any change to the Dockerfile, `entrypoint.sh`, or **any file the
Dockerfile `COPY`s from the build context**: the docs, the skills, the optional kit (pingbus
zipapp, `pingbus-status-line`, the plugin), the supervisor plugins, the agent-browser stub and
guard. So most ccy releases that touch anything inside the container move the label.

**Where the label sits is what makes that expensive.** `files/var/local/claude-yolo/Dockerfile:36`
puts `LABEL claude-yolo-version="2.79.15"` as the **second instruction of the `base` stage**,
directly after `FROM node:lts-slim AS base`. Every instruction after it (the three apt layers,
gh, yq, uv, `npm install -g @anthropic-ai/claude-code`, the Chromium libraries, agent-browser
plus the 190 MB Chrome download, Lightpanda, opencode, then the `full` stage's LSP npm
install) has the label step as an ancestor. Docker's documentation states the rule: "Once the
cache is invalidated, all subsequent Dockerfile commands generate new images and the cache
isn't used" ([Docker: build cache invalidation](https://docs.docker.com/build/cache/invalidation/)).
Whether buildah treats a changed
`LABEL` step as a cache miss for its descendants is not stated in its manual; its cache
lookup compares each candidate's history with the instruction, and a changed label is a
changed history line. **Section 7 measures it (M2)**, because it decides how much of the
base build a release costs.

The Dockerfile hash label was already moved to the end of the `full` stage for exactly this
reason (PERF-02, `Dockerfile:38-40` and `:513-517`). The version label was not.

Each ccy release that moves the label therefore:

1. rebuilds all of `:base` (if M2 confirms), Chrome download and apt included, then `:latest`;
2. changes the image ID of `:latest` (it would change even on a full cache hit, since the
   label is image content);
3. makes every project image rebuild on its next launch, from its first instruction.

How often: on F44's first-parent history over the three months to the date of this research,
78 commits changed the `claude-yolo-version` label, on 24 different days; `CCY_VERSION`
changed in 196 commits. How often a host actually took one of these depends on how often the
owner deploys, which the triage in section 7 reads from the image history instead.

### 1.3 The daily Claude Code update commits a new `:latest`

`auto_update_claude_code` (`claude-yolo:1877-1969`) runs once a day per image, on the image the
launch runs (`:2263-2267`). When Claude Code is behind npm `latest`, `update_claude_inplace`
(`:1984-2015`) runs `npm install -g @anthropic-ai/claude-code@latest` in a temporary container
and `podman commit`s it back onto the same tag. The same in-place update runs for
`--rebuild=claude`, for `--rebuild=project` (on `:latest` and `:base` before the project
build, `:2038-2047`), and before a supervisor-requested restart (`:3794`).

For a project **without** a custom Dockerfile the image it runs is `claude-yolo:latest`, so
its first launch of a day on which Claude Code has had a release commits a new `:latest`. The
next launch of every project **with** a custom Dockerfile then sees a different `:latest` ID
and rebuilds in full ("Base image updated", `:2120-2136`), even though the only change was a
newer Claude Code, which that project image would have received anyway from its own daily
in-place update. Claude Code publishes releases most days, so on a host that runs both kinds
of project this is very likely the most frequent cause of full project rebuilds, more
frequent than ccy releases. Section 7 (M1) confirms it from the commit layers in
`podman image history claude-yolo:latest` and the age of each project's base stamp.

### 1.4 What is not refreshed today

The current shape keeps the base reasonably fresh by accident, not by design:

- `podman build` defaults to `--pull=missing`: "Only pull the image when it does not exist in
  the local containers storage" ([podman-build(1)](https://docs.podman.io/en/latest/markdown/podman-build.1.html)).
  No build anywhere passes `--pull` (`rg` of the launcher, `lib/` and the image task), so
  `node:lts-slim` and `rust:slim` are whatever was first pulled on this host. Nothing
  refreshes them. A Node LTS security release or a Debian point release in the upstream
  image never reaches ccy until someone removes the local copy.
- The base layers get current Debian packages only because the label at the top makes every
  release run `apt-get update && apt-get install` again. No layer runs `apt-get upgrade`, so
  the packages that came with `node:lts-slim` stay as old as that first pull.
- Chrome (`last-known-good-versions`), yq (`releases/latest`), uv, agent-browser and the LSPs
  are fetched unpinned and refreshed only when their layer reruns.

So the label placement that makes releases expensive is also the only thing that refreshes
the base. **Moving the label without adding an explicit refresh would freeze the base**,
which is the owner's constraint exactly. Section 4 makes the refresh explicit.

### 1.5 Small defects found on the way

- `Dockerfile.project-template:40-58`, `Dockerfile.example-golang`, the generated prompts in
  `lib/dockerfile-custom.bash` and `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt:223-239`
  recommend apt cache mounts but finish with `rm -rf /var/lib/apt/lists/*` inside the mounted
  `/var/lib/apt`, which empties the cache on every run. Debian's official images also ship
  `/etc/apt/apt.conf.d/docker-clean`, which deletes downloaded `.deb`s after install; Docker's
  own apt cache example removes that file first ([Dockerfile reference, RUN --mount=type=cache](https://docs.docker.com/reference/dockerfile/)).
  If `node:lts-slim` carries it (M6 checks), the apt cache mounts in every template cache
  almost nothing. `CUSTOM-DOCKERFILES.txt:288-290` and the generated prompt also set
  `PIP_NO_CACHE_DIR=1` beside a pip cache mount, which turns that cache off.
- The same documents promise "~30 seconds" rebuilds "with cache". After a base change no layer
  cache applies, and the cache mounts are the only reuse left.
- `DOCKER_BUILDKIT=1`, `BUILDKIT_INLINE_CACHE=1` and the BuildKit message (1.1) do nothing on
  podman.

## 2. The podman and buildah facts the options rest on

From [podman-build(1)](https://docs.podman.io/en/latest/markdown/podman-build.1.html) and
[buildah-build(1)](https://github.com/containers/buildah/blob/main/docs/buildah-build.1.md):

- `--layers`: "Cache intermediate images during the build process" (default true in podman).
  The cache is the local store; nothing is shared with another host.
- `--pull`: `always`, `missing` (default), `never`, `newer` ("Pull base … images from the
  registries … if newer"). `newer` is the cheap way to keep a floating tag current: it asks
  the registry and pulls only when the remote is newer.
- `--cache-ttl`: "Limit the use of cached images to only consider images with created
  timestamps less than *duration* ago"; `--cache-ttl=0` equals `--no-cache`. This is a built-in
  maximum age for every cached layer: with `--cache-ttl=168h` no layer older than a week is
  reused.
- `--no-cache`: rebuild every step. It does not refresh the `FROM` image; `--pull` does.
  Docker gives the same advice: rebuild regularly, combine `--pull` and `--no-cache`
  ([Docker: build best practices](https://docs.docker.com/build/building/best-practices/)).
- `--cache-from` / `--cache-to`: a **registry** repository as cache source or destination,
  ignored unless `--layers`. Useful when builders are ephemeral or several machines share a
  cache; one host's local store already is the cache.
- `--squash` merges the new layers into one, `--squash-all` everything. Both remove the layer
  boundaries the cache works on, so neither makes a rebuild cheaper. Only the layer count goes
  down.
- `--timestamp`, `--source-date-epoch`, `--rewrite-timestamp`, `--identity-label=false`,
  `--omit-history`: reproducible output. The same inputs give the same image ID, so a rebuilt
  but identical image does not look like a new base.
- `--build-context name=…`: a named extra context a `FROM` or `COPY --from` resolves first.
  ccy could use it to point a project's `FROM` at an image of its choosing without editing the
  project's Dockerfile.
- `RUN --mount=type=cache` is supported by buildah ([Containerfile(5)](https://github.com/containers/common/blob/main/docs/Containerfile.5.md):
  `id`, `mode`, `ro`, `uid`, `gid`, `from`; buildah also takes `sharing`). The cache sits on
  the host outside the image (for this rootless user `/var/tmp/buildah-cache-<uid>`, the
  directory `purge_stale_buildah_cache` in `lib/common.bash:361` already knows). Since buildah
  1.29 each user gets their own cache parent ([buildah 1.29 release notes](https://buildah.io/releases/2023/01/27/Buildah-version-v1.29.0.html)).
  `buildah prune` clears it **and the intermediate images**
  ([buildah-prune(1)](https://www.mankier.com/1/buildah-prune)), so it is not a tool for
  bounding the cache mounts alone. Red Hat tracks a buildah bug where a layer using a `bind`
  or `cache` mount is not invalidated when the mounted contents change
  ([RUN-2454](https://issues.redhat.com/browse/RUN-2454)), so a cache mount must hold only
  caches, never inputs.
- `skopeo inspect --format '{{.Digest}}' docker://…` reads a registry digest without pulling
  ([skopeo-inspect(1)](https://github.com/containers/skopeo/blob/main/docs/skopeo-inspect.1.md)).
  No play installs skopeo (`rg` of `play-podman.yml`, `play-claude-yolo.yml`), so a digest
  check either adds it to `play-podman.yml` or uses `podman pull` and compares the image ID
  before and after, which transfers only the layers that changed.

Plan 00235's prune (`src-go/internal/containerimages/prune.go:7-46`) matters here:

- **P1** removes a dangling image (no tag, nothing built on it) once it is over a day old and
  no container uses it, and **P7** repeats until nothing is left, so a whole untagged chain
  goes in one run. A multi-stage build's builder stage ends in exactly such an image: its last
  step has no tag and no child. **The day after a build, the daily prune would remove the
  builder stage's cache**, and the next build would compile again. Option (a) only works if
  that cache is either tagged or exempted.
- **P2** removes a `claude-yolo:<project>` tag after 30 days with no launch.
- **P3a** removes nothing while any ccy build lock is held, which protects a build in progress.
- `container-images replaced` (`lib/image-build-prune.bash`) removes each image a build
  replaced once no tag names it; an old `:latest` that project images are built on is refused
  and left to the daily prune.
- Nothing in it bounds `/var/tmp/buildah-cache-<uid>`.
- Plan 00235's Q6 currently holds the prune off on this host, so none of this is acting yet.

## 3. The options

Costs are relative and are confirmed by the measurements in section 7; nothing is assumed in
seconds or gigabytes.

### (0) Stop the rebuilds that change nothing the project image needs

Not in the brief's list, and it has the best value for the effort.

- **What**: key the project rebuild on what the base image *was built from*, not its image ID.
  The base build writes a label (proposed `claude-yolo-inputs`) holding a hash of the
  Dockerfile, the whole staged build context and the digest of each upstream image it was
  built `FROM`. `podman commit` of a Claude Code update keeps the labels it inherited, so a
  daily update no longer looks like a new base. A project is compared against the image its
  own `FROM` line names (`:base` or `:latest`), not always `:latest`.
- **Savings**: removes every full project rebuild caused by Claude Code's in-place update
  (section 1.3), and every one caused by a `:latest` move a `FROM claude-yolo:base` project does
  not inherit. The project image gets the newer Claude Code from its own daily in-place
  update, as now.
- **Cost**: launcher and `lib/` only (a version bump), plus one label in the Dockerfile. No
  project changes its Dockerfile.
- **Risk**: an input missing from the hash means a project image keeps an old base. The hash
  has to be derived from the build context directory, never from a list (the lesson
  `ContainerRules.md` records for `CCY_HASH`). The context-hash comment at `:2091-2095` stays
  true, since the context is hashed.
- **Freshness**: this stops a rebuild only when the base's inputs are unchanged, so the
  refresh in section 4 still applies. A refreshed base has new upstream digests, so its inputs
  hash changes and the project image is rebuilt.

### (a) Multi-stage project builds: compile in a builder stage, `COPY --from` onto ccy

- **What**: the expensive compile runs in a stage `FROM` a pinned upstream toolchain image
  (`rust:<version>-slim-<debian>`, `golang:<version>-<debian>`, a Debian with build-essential)
  and only the built artefacts land on the ccy image with `COPY --from=<stage>`. The builder
  stage's cache key hangs off the toolchain image, not off ccy, so a new ccy base leaves it a
  cache hit and the final stage only copies the artefacts again. ccy's own Dockerfile already
  does this for PHPantom (`Dockerfile:7-17`, `:508`).
- **Savings**: the compile no longer reruns on a ccy release or a base refresh, only when the
  builder's inputs change or its cache expires. What still reruns is the final stage: its apt
  installs and the copies.
- **Cost**: each heavy project's Dockerfile is restructured. ccy changes are small: the
  template, the examples, the generated prompts and `CUSTOM-DOCKERFILES.txt` show the
  pattern, and the builder cache is protected from the daily prune (below).
- **Risks**:
  - The prune (P1) removes the builder chain a day after it is built (section 2). Either ccy
    tags each named builder stage after a build (`podman build --target <stage> -t claude-yolo-cache:<project>-<stage>`,
    a cache hit at that point), or P1 keeps a dangling image carrying a
    `claude-yolo-build-cache` label until it is older than the maximum age (section 4). The label
    is the simpler contract and turns the prune into the expiry.
  - ABI: an artefact built on a newer glibc does not run on an older one. `node:lts-slim`
    floats across Debian releases (the comment at `Dockerfile:58-61` says so), so a project
    pinning its builder to one Debian release can end up newer or older than ccy's runtime.
    ccy should publish its runtime Debian release as a label and in its docs, and static
    builds (musl, `CGO_ENABLED=0`) avoid the question, as PHPantom does.
  - Shared libraries the artefact needs must still be installed in the final stage.
- **Migration**: opt-in per project. A project that is not heavy stays as it is. ccy's
  `--custom-docker` improvement prompt can propose the split when it sees a compile step.
- **Freshness**: the pinned builder tag is checked with `--pull=newer` whenever the project
  image is built, and `--cache-ttl` (section 4) bounds how long its compile is reused. A
  security fix in the toolchain image therefore reaches the artefact within the maximum age.
  Version bumps of the pinned tag are the project's own. The health report can name a builder
  cache older than the maximum age (section 4).

### (b) Split ccy's image into a slow base and a thin fast top

Two forms.

**(b1) Reorder the one Dockerfile.** Slow, rarely changing layers first (OS packages,
toolchains, Chromium, LSPs), then the thin, fast-changing ones last (the ccy files it `COPY`s,
the Claude Code npm install, the labels). The version label moves to the end of `base`, after
every `RUN`.

- **Savings**: a ccy release rebuilds only the thin tail of the base. This saves time on the
  base build itself. **It does not save any project rebuild**: `:latest` still gets a new ID and
  every project still rebuilds from its first line, because its layers sit above the whole
  image.
- **Cost**: a Dockerfile reorder and a container version bump.
- **Risk**: this removes the accidental refresh (section 1.4), so it must not ship without the
  refresh in section 4.

**(b2) Put the project's layers between the slow base and ccy's thin top.** ccy builds
`claude-yolo:tools` (the slow part, no ccy files, no Claude Code). A project's Dockerfile
builds on that. ccy then applies its thin top to the result (a generated final stage, or a
second `podman build` `FROM` the project's tools image), and tags that as `claude-yolo:<project>`.

- **Savings**: a ccy release that changes only ccy's own files rebuilds no project layer at
  all, only the thin top (seconds). Project layers rebuild only when the slow base changes,
  which section 4 makes deliberate and bounded.
- **Cost**: the largest change of these options. Image composition in the launcher, the
  `:base`/`:latest` contract (a project `FROM claude-yolo:latest` today expects the LSPs and
  Claude Code beneath it), the self-prune's view of which tags replace which, and the docs.
  An `ENTRYPOINT`, `CMD`, `WORKDIR` or `ENV` set by the project would be overridden by the top
  unless the top only adds files.
- **Risk**: two images per project; a project that relies on something in ccy's top at build
  time (`claude` on PATH during its build, say) breaks.

**(b3) Mount ccy's fast part at run time instead of baking it in.** The entrypoint, docs,
skills, optional kit and supervisor plugins come from the host's `/opt/claude-yolo` through a
read-only bind mount; the image holds only tools.

- **Savings**: most container version bumps disappear, since the files that cause them are no
  longer image content. Neither the base nor any project rebuilds for them.
- **Cost**: launcher, entrypoint and Dockerfile changes; the version-bump gate's file set
  changes (`scripts/test-ccy-container-version-hook.bash` checks it matches what the play puts
  in the image).
- **Risks**: the supervisor refuses a plugin file not owned by root or its own uid
  (`Dockerfile:443-452`). A root-owned host file appears as the overflow uid in a rootless
  container, so a bind-mounted plugin would be refused; it would need copying at start, as
  the entrypoint already does for skills. A container and its mounted files can now come from
  different releases while a session runs across a deploy.
- **Claude Code itself** stays baked: it is updated in place every day, and with option (0)
  that update no longer causes project rebuilds, so mounting it gains little.

### (c) Do not move `:latest`/`:base` when their content did not change

- **What**: (c1) the version label moves to the end of the build (b1) or out of the image
  into option (0)'s inputs label and the launcher; (c2) the image is rebuilt only when its
  inputs change; (c3) reproducible layers (`--timestamp` or `--source-date-epoch` with
  `--rewrite-timestamp`, `--identity-label=false`) so a rebuild of the same inputs gives the
  same ID.
- **Savings**: (c2) is what the play's cached build already achieves when the context is
  unchanged (measured by M4). (c3) adds that an uncached rebuild of identical inputs gives an
  identical image. With unpinned downloads (Chrome, yq, npm `latest`) a rebuild is never
  byte-identical, so (c3) helps only after pinning those, which costs a version process. Option
  (0) gets the benefit without pinning, by comparing inputs, not bytes.
- **Risk**: an image that "did not change" because its inputs did not is exactly a frozen
  image. (c) is only safe with section 4.

### (d) `RUN --mount=type=cache` for package-manager and compiler caches

- **What**: apt, pip, npm, cargo registry and target, go module and build caches kept on the
  host across builds.
- **Savings**: reruns of a layer, after any cache miss, download and recompile less. This is
  the only option that also helps a build whose layer cache has gone (base refresh, expiry,
  prune). For cargo and go, a cached target or build directory turns a full compile into an
  incremental one.
- **Cost**: small, but the current templates do it wrongly (section 1.5): fix `docker-clean`
  (in ccy's base, so every project benefits), drop the `rm -rf` of mounted directories and
  `PIP_NO_CACHE_DIR`.
- **Risks**: the cache directory grows without bound and nothing prunes it (section 2); an
  input placed in a cache mount is not tracked (RUN-2454). The cache must be bounded by the
  daily prune (age of each `id`'s directory) while no build lock is held.
- **Freshness**: a cache mount holds downloads, not decisions: `apt-get update` still reads
  current lists, and package managers verify hashes, so it does not pin anything.

### (e) Anything else podman-specific

- `--squash`/`--squash-all`: no rebuild saving, removes reuse; not recommended.
- `--layers`: already on by default in podman; nothing to gain.
- `--cache-from`/`--cache-to` with a **local registry**: adds a registry service to the host
  for no gain on one machine, since the local store is already the cache. Worth it only if
  several hosts should share builds, which nothing here asks for.
- `--cache-ttl`: the useful one. It turns "how old may a cached layer be" into a build flag,
  which section 4 uses as the maximum age.
- `--pull=newer`: the other useful one, for the upstream `FROM` images.
- `--build-context`: a way for ccy to resolve a project's `FROM claude-yolo:…` to an image of
  its choosing without editing the Dockerfile, if (b2) goes ahead.

## 4. Freshness: what keeps a cached base current (the owner's constraint)

Requirement: **no cached or pinned layer outlives a stated maximum age, every upstream image is
checked for a newer digest on a stated cadence, and a base older than the maximum age is
reported where the owner reads it.** This is the plan's own requirement R-FRESH.

| Part                                                                         | What refreshes it                                                                                                                                                                                                                                              | How staleness is detected and reported                                                                                                       | Forces a full rebuild whatever the cache holds                                         |
| ---------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Upstream `FROM` images (`node:lts-slim`, `rust:slim`, project builder bases) | `--pull=newer` on every base and project build; a daily check of the registry digest (`podman pull` and compare, or skopeo installed by `play-podman.yml`)                                                                                                     | the inputs label records the digest built from; the health report names a base whose upstream digest moved more than the grace period ago    | the upstream digest changed (rebuild of the slow base with `--pull=newer --no-cache`)  |
| ccy's slow base layers (OS packages, Chrome, LSPs)                           | a scheduled refresh: `--pull=newer --no-cache` once the base is older than the maximum age, run by a user timer (from `play-claude-yolo.yml`) so no launch waits; `apt-get upgrade` in the first apt layer so packages from the upstream image are current too | the base's created time against the maximum age, read by ccy at launch (one line on stderr) and by the health report                         | maximum age reached; `ccy --rebuild=full`; a change to the base's own Dockerfile lines |
| ccy's thin top (b1/b2)                                                       | every ccy release, as now                                                                                                                                                                                                                                      | the version label against the launcher, as now                                                                                               | (not needed: it is rebuilt every time)                                                 |
| Project layers                                                               | rebuilt when the base's inputs change (option 0), which a base refresh does                                                                                                                                                                                    | ccy at launch: the project image's base inputs against the current base's; the health report lists project images older than the maximum age | the base was refreshed; the project Dockerfile changed; `ccy --rebuild`                |
| Builder-stage caches (a)                                                     | `--pull=newer` for the builder image and `--cache-ttl=<maximum age>` on the project build, so a compile is reused for at most the maximum age                                                                                                                  | the prune log and dry run name each builder cache and its age; the health report names one past the maximum age                              | the builder image digest changed; the maximum age passed; `ccy --rebuild`              |
| Cache mounts (d)                                                             | not a freshness concern (downloads, hash-checked)                                                                                                                                                                                                              | the prune dry run lists their size                                                                                                           | none; removed by age by the daily prune                                                |

`--cache-ttl` deserves the attention: one flag on every ccy build (base and project) means no
layer older than the maximum age can be reused, whatever else goes wrong. It is the backstop
that makes "cheaper" unable to become "frozen". The timer is what makes the refresh happen at
a quiet time instead of at a launch.

A refreshed base makes every project rebuild once, which is the point. With (a) in place that
rebuild skips the compile unless the builder's own base also moved, and with (d) the rest
downloads from the cache.

## 5. Recommendation, by value for effort

1. **Option (0)** plus the **trigger fixes**: compare the base's inputs, not its image ID;
   compare against the image the project's `FROM` names; drop the BuildKit no-ops and the
   misleading message. This alone ends the daily full project rebuilds that Claude Code's
   in-place update causes, if M1 confirms those happen. Launcher, `lib/` and Dockerfile changes
   only; no project changes.
2. **The freshness machinery of section 4**, landed with or before anything that removes a
   rebuild: `--pull=newer` and `--cache-ttl` on every build, `apt-get upgrade` in the first apt
   layer, the inputs label carrying upstream digests, the refresh timer, the launch notice, the
   health report check. Without it, item 3 freezes the base.
3. **(b1)**: move the version label to the end of `base` and order the base slow-to-fast. A
   ccy release then rebuilds seconds of base instead of all of it (if M2 confirms buildah
   treats the label as a cache break).
4. **(d) done properly**: `docker-clean` handled in ccy's base, the templates and docs
   corrected, the cache mounts bounded by the daily prune.
5. **(a)** as the documented pattern for heavy projects: template, example, prompts,
   `CUSTOM-DOCKERFILES.txt`; the `claude-yolo-build-cache` label kept by the prune until the
   maximum age; ccy's runtime Debian release published as a label.
6. **(b3)**, then possibly **(b2)**: only if the measurement after items 1-5 still shows ccy
   releases costing heavy projects too much. (b3) removes most container version bumps; (b2)
   removes the project rebuild on a ccy release entirely, at the highest cost.

Not recommended: `--squash`/`--squash-all`, a local registry with `--cache-from`/`--cache-to`,
and byte-reproducible builds (c3) before the downloads are pinned.

### Project-Dockerfile contract changes implied

- Unchanged for a project that does nothing: `FROM claude-yolo:latest` or `:base` keeps working.
- New, opt-in: a heavy compile goes in a builder stage `FROM` a pinned upstream image and ends
  with `LABEL claude-yolo-build-cache="<project>"`; only `COPY --from` reaches the final stage;
  artefacts are static or built for ccy's published Debian release.
- Cache mounts: the templates' form changes (no `rm -rf` inside mounted directories, no
  `PIP_NO_CACHE_DIR`), with `id=` per tool so projects share apt and pip caches deliberately.
- A project must not put inputs into a cache mount.
- With (b2) only: a project may not set `ENTRYPOINT`/`CMD`, and must not need ccy's top at
  build time.

### How ccy's self-prune (Plan 00235) interacts

- P1 would remove builder-stage caches after a day: the prune needs the
  `claude-yolo-build-cache` rule (keep while younger than the maximum age), or option (a)
  saves nothing after the first day. This is a change to `container-images`, so it waits for
  Plan 00235 to finish and is designed with it (its Q6 currently holds the prune off).
- P2 (project tag after 30 days unlaunched) is unaffected, and builder caches of a removed
  project expire on their own by age.
- `replaced` removes an old `:latest` once nothing is built on it. With option (0), project
  images stay built on an older `:latest` for longer (until a real base change), so `replaced`
  will refuse more often and the old base stays until the project rebuilds; the measurement
  reports the store size this costs.
- The prune gains a bound for `/var/tmp/buildah-cache-<uid>` (age per cache `id`), held off while
  any build lock is held, like P3a.
- P3a's lock check covers a refresh-timer build, since that build takes the same image lock.

## 6. Questions for the owner, with recommended defaults

See PLAN.md, "Owner questions", which holds the list and its defaults.

## 7. Measurements (host, through a plan script)

Everything is built in a separate repository name (`claude-yolo-measure:*`) and removed by the
script at the end, so the real images and their stamps are untouched. The heavy project is a
synthetic fixture in the plan folder, "project-A": a pinned, deterministic compile that takes
minutes (a pinned Rust crate built from source, say), in the current single-stage shape and in
the multi-stage shape. Measured: wall time of each build, and bytes rebuilt, meaning the sum of
the sizes of the layers in the new image that the previous image did not have (from
`podman image inspect` `RootFS.Layers` and `podman image history`, never `podman system df`,
P5).

- **M1** (triage, read only): `podman image history claude-yolo:latest` (count and dates of
  in-place commit layers), each project base stamp's age against the current `:latest` ID, and
  the image creation dates, to see how often each trigger fired on this host. Project names
  are written only to the untracked run log.
- **M2**: does a changed `LABEL` near the top bust buildah's cache for all later steps? Build
  the ccy Dockerfile into `claude-yolo-measure:base`, change only the label value, rebuild,
  and time it.
- **M3**: the cost of today's chain: base rebuild after a label bump, then project-A rebuilt on
  it (single-stage); and project-A rebuilt after an in-place Claude Code commit on the base.
- **M4**: does a cached rebuild of an unchanged context keep the image ID?
- **M5** (after): the same events with option (0) (no rebuild expected after an in-place
  commit), (b1) (label at the end), and project-A multi-stage with its builder cache kept.
- **M6**: does `node:lts-slim` carry `/etc/apt/apt.conf.d/docker-clean`, and do the template's
  apt cache mounts keep any `.deb`?
- **M7**: the store growth of each shape (layers kept), and the cache-mount directory size.
