# ccy: the Go caches out of the container's writable layer

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Not plan work: no plan owns the CCY container caches (Plan 00191 mentions only the host-side
`ccy-go-build` scratch directory). Problem and measurements: `261023-ccy-shutdown-hang.md`.

## What changed

- `src-go/internal/teams/gocache` (new): `ccy-teams internal go-cache <project> [z]`. It
  makes `~/.cache/ccy/<project>/go/{go-build,golangci-lint,mod,go-qa-tools}` (0700, each a
  real directory, else exit 1) and prints the `podman run` arguments one per line:
  `-v <root>:/var/cache/ccy-go[:z]` and `-e GOCACHE=…/go-build`,
  `-e GOLANGCI_LINT_CACHE=…/golangci-lint`, `-e GOMODCACHE=…/mod`,
  `-e GOQA_TOOLS_DIR=…/go-qa-tools`. `GOQA_TOOLS_DIR` is go-qa-ci's own override
  (`internal/toolstore.DirEnv`); without it go-qa writes to `os.UserCacheDir()/go-qa/tools`.
- The bound runs at launch. If regular files under the root hold more than 8 GiB on disk
  (`st_blocks`), stage 1 clears `go-build` and `golangci-lint`. If the total is still over,
  stage 2 clears `mod` and `go-qa-tools`. Each cache is renamed into a fresh `.prune-*`
  directory and then removed: read-only module dirs are made writable through an `os.Root`,
  and the cache is made again afterwards. A `.prune-*` left by an interrupted launch is
  removed before the size is measured. The bound adds to go's own trimming: go drops build-cache entries unused for 5 days.
- `ccy-teams`: dispatch, usage text and package doc.
- `files/var/local/claude-yolo/claude-yolo`: two lines call the command and append its
  output to `DOCKER_MOUNTS` (fail-fast `|| exit 1`). To stay under the ratchet, the
  `CCY_EXTRA_MOUNTS` and project-mount blocks lost their `if` guards and debug lines (an
  empty array expands to nothing). The allowlist record dropped from 2782 to 2777 code lines.
  Statements are unchanged at 2713.
- CCY 3.100.20 (the container version is unchanged: the image does not change). Updated
  `docs/ccy.md` (Extra Mounts) and `docs/ccy-changelog.md`.
- difftest: a `ccy-teams internal go-cache difftest-project` fake in the common world, and
  the goldens re-recorded.

## Review round 1 (Opus, CHANGES) and what changed

The full review is in `261023-ccy-go-cache-review-r1.md`.

- **B1 (a file vanishing mid-walk failed a seat's launch): fixed.** The size walk and the
  chmod walk now treat `ErrNotExist` as an empty entry (`blocks`, `skipGone`, unit tested).
  The prune and leftover sweep run under an `flock` (in round 1 at `<root>/.lock`; moved out
  of the bind in round 3), so two launches never
  prune at once, and a launch never removes another launch's live `.prune-*`. Tests cover
  the order (lock, then prune, then unlock) and that the lock really excludes others.
- **B2 (root-owned files under docker): fixed.** With `CONTAINER_ENGINE=docker` the command
  prints the named volume `ccy-go-cache-<project>` with no relabel, makes no host directory,
  and applies no bound. An unknown engine is exit 1.
- **B3 (meta-deploy.list): not done, on purpose.** The lead told me not to edit it. The host
  run is named below for the lead to queue.
- **Non-blocking items fixed:** the docs no longer claim that no seat ever sees a cache half
  deleted (module-download caveat added), and no longer claim golangci-lint trims itself.
  The docs now say the `-e` values override a Dockerfile `ENV`. The stale `# Mounts:`
  comment is updated.
- **Non-blocking items accepted:**
  - the launch-time walk (about 60k files, comparable to podman's own `:z` relabel of
    `/workspace`);
  - there is no bound across all projects;
  - `XDG_CACHE_HOME` is not honoured (the launcher's other caches use `$HOME/.cache` too).

## Review round 2 (Opus, CHANGES) and what changed

The full review is in `261023-ccy-go-cache-review-r2.md`.

- **B1 fixed: a planted `.prune-*` symlink made the host chmod directories outside the
  cache.** All work inside the root now goes through one `os.Root`: the leftover sweep,
  measuring, renaming, making the trash directory, chmod, removal and making the caches. An
  entry that is not a directory is only unlinked. Three red/green tests cover it: a planted
  leftover link, a link inside a pruned cache, and a cache that is itself a link. Each checks
  that a read-only directory outside the cache keeps its mode and its file.
- Docs now note that `GOMODCACHE` moves a module cache that a Dockerfile's `GOPATH` placed.
- Coverage was below the 90% floor, so I added a test-only seam (`Cache.wrap`) over the
  `os.Root` operations (`fsRoot`). Each file-system failure path now has a table test,
  which brings coverage to 96.6%. The size measure is now the files' total size (no
  `Stat_t` branch). The trash name comes from `crypto/rand.Text`.

## Review round 3 (Opus, CHANGES) and what changed

The full review is in `261023-ccy-go-cache-review-r3.md`.

- **B1 (the lock was inside the bind the container can write): fixed.** The lock is now
  `~/.cache/ccy/<project>/go.lock`, beside the bind, so the container cannot hold it,
  remove it or replace it. Only another host launch of the same project can hold it, and
  only for the prune.
- **B2 (the bound was not enforced): fixed.**
  - The size is measured in allocated blocks again, so a sparse file counts only the space
    it really uses (tested).
  - If the total is still over the bound after both stages, the launch exits 1, names the
    root and says that what remains is not a cache ccy keeps (tested).
- **Non-blocking items fixed:**
  - the docs now note that a running seat has to reinstall go-qa's tools after the tools
    cache is pruned;
  - in the launcher, `mapfile …; DOCKER_MOUNTS+=` replaces `&&`, so under `set -e` a mapfile
    failure stops the launch instead of skipping the append.
- **Non-blocking items accepted:**
  - the bound is per project name, and nothing removes the caches of a deleted project;
  - the cost of the `:z` relabel on Enforcing hosts is unmeasured: worth timing once on the
    host;
  - a container `chown` making later launches fail loudly;
  - there is no difftest scenario for `go-cache` failing.

## Bind mount, not a named volume: why

- **The host can bound it without the engine.** `ccy-teams` measures and prunes a plain
  directory as the login user. A named volume would have to be found through
  `podman volume inspect`. Under docker (rootful) it lives in `/var/lib/docker`, which the
  user cannot reach at all.
- **Rootless podman:** the container's root is the host user, so every cache file belongs
  to the user and can be deleted. SELinux: the launcher's existing `CCY_MOUNT_RELABEL` (`z`,
  shared) is passed through, so two seats of one project can both use the cache. The private
  `Z` is refused, because it would lock the cache to one container.
- **Easy to see and remove** (`du -sh ~/.cache/ccy/*/go`), and `podman volume prune` does
  not wipe it as a side effect.
- **Cost:** the `:z` relabel walks the tree at each start. `/workspace` already pays this on
  a far larger tree.
- **Docker** (compatibility-only for ccy) gets a named volume instead, because rootful
  docker would write root-owned files into `~/.cache`.

## Shared by seats: why it is safe

- **go-build and mod:** cmd/go's caches are designed for concurrent processes. Entries are
  written atomically and modules are extracted, then renamed.
- **golangci-lint:** its cache is derived from cmd/go's cache and has the same atomic writes.
- **go-qa tools:** `toolstore.install` builds in a `MkdirTemp` scratch dir and renames the
  result into place. If another process already installed it, the rename "loses" and that
  counts as success.
- **The prune** is the one risky moment. It renames each cache aside, so nothing ever sees a
  half-deleted cache. Removing the build caches mid-build is what `go clean -cache` does; go
  ignores cache write misses. Stage 2 can make an in-flight module download in another seat
  fail once. It only runs when mod and tools alone exceed 8 GiB, which in practice means never.

## Host run needed

`playbooks/imports/play-claude-yolo.yml`. It rebuilds and installs `ccy-teams` and the
launcher. The launcher needs the new `ccy-teams` subcommand, and both come from the same play.
