# Plan 00216 review, round 3 (adversarial, Opus, fresh reviewer)

Verdict: FIX-FIRST

Scope: `git diff F44...HEAD` on feat/ccy-image-build-lock (7877a9c8b, 9b633d49f, 2a8081958).
I ran `./scripts/test-ccy-image-build-lock.bash` (81 passed, 0 failed) and
`go test ./internal/ccylaunch/...` (ok, difftest included). Read-only otherwise.

## Round 2, checked

- R2-01 (the real-process stop test could not tell a stop from a non-stop): fixed.
  `scripts/test-ccy-image-build-lock.bash:462-473` now asserts the exact lines ("is being built
  by process $victim", "The build of claude-yolo:latest by process $victim was stopped."). It
  checks that the holder is gone and the lock is free right after `--build-lock` returns,
  before `wait`, and that the stopped launch's status is non-zero. Only `[y/N]` on stderr is
  not asserted, which is minor.
- R2-02 (stdout checked on one launch only): fixed (:311-312).
- R2-03 (zombie): fixed (`procStat` treats Z/X as gone, buildlock.go:366;
  `TestBuildLockCountsAZombieAsGone`).
- R2-04 (piped answers lost): fixed (one `ui.Lines`, buildlock.go:96-97;
  `TestBuildLockAsksAboutEachBuild`).
- R2-09 (unchecked `kill -9`): fixed (:411-412).
- R2-05 and R2-08 were accepted into D6. R2-06 and R2-07 are unchanged NOTEs. Neither is wrong:
  for R2-07, a `--build-lock` that is not the first argument reaches claude's fail-fast flag
  check and is refused there.

## Checked afresh and sound

- **Lock function.** `{fd}>&-` is on the check and on the build. No external command runs
  with the descriptor between `exec {fd}>>` and the close, except `flock`, which needs it. The
  record is written through a separate truncating open, so the flock is kept. The record is
  emptied before the release. A flock error other than 75 fails with a message. The
  unlocked-then-locked check means a launch that takes the lock without waiting still
  re-checks.

- **Re-checks after waiting.**

  - base: `ccy_base_image_current`, the launcher's own check;
  - project image: hash files written inside `run_project_build`;
  - daily update: `ccy_claude_is` against `latest_version`;
  - restart path and `--rebuild=claude|project`: `--unless-changed`, with the ID taken before
    the wait.

  The keys agree: after line 2255 `IMAGE_NAME` is the project image, so the daily update and
  the restart path lock the same key the project build does.

- **Build sites.** Every `container_cmd build`/`commit` in the launcher and lib/ is under the
  lock, and so is the play's build. `build_container_with_hash` has no other caller, and src-go
  builds no image. That leaves one gap, a site whose check is wrong (finding 1).

- **A holder killed mid-build.** The kernel frees the flock and the record is left behind.
  Both readers name it stale, and the next holder overwrites it. The suite proves the waiter
  then builds.

- **Go stop path.**

  - `stillHolds` re-probes the lock, the record and the vouching after the answer.
  - Each pid is signalled only while its start tick is unchanged.
  - The SIGKILL round adds new children only while the holder is still the same process.
  - "stopped" requires the record to have left the lock and every tree process to have ended.
  - A waiter that takes the lock during the stop is never in the tree.
  - The holder is the launcher's `$$`, which the front `exec`s into. The lock is held only
    before a session's `podman run` (lines 2050-2267) or after it has exited (the restart path,
    3797), so the holder's tree never contains a running session, apart from the window in
    NOTE 4.

- **Pid reuse.** Both terms of `btime + start/100` are truncated, so the sum never exceeds the
  true start, and the `$EPOCHSECONDS` record is never earlier than it. A real holder is never
  refused while the clock does not step (D6 accepts the step).

- **Fail fast and stderr.**

  - At top level (`set -e`) the function's every step is checked. In `if` context the build
    functions check their own steps (R1-03).
  - Waiting and "built while this launch waited" go to stderr, and the check's stdout is
    discarded under the lock.
  - The Go report goes to stdout, the prompt to stderr, and usage errors are exit 64 on stderr.

- **Public repo.** No host-specific names in the added lines. The plan's `Owner` field is the
  template's own.

## Findings

1. **FIX-FIRST: `ccy --rebuild=claude` with no base image still builds twice when two
   launches start together.** `files/var/local/claude-yolo/claude-yolo:2026-2027`:

   ```bash
   echo "No base image found - running full build first..."
   ccy_image_build_locked "$IMAGE_NAME" false -- ccy_base_image_build --no-cache
   ```

   The check is `false`, so a launch that waited for another's first-time build throws that
   build away and runs a second full `--no-cache` build. This is the reported defect itself:
   two launches, a missing image, two builds of many minutes. D4 allows `false` only for an
   explicit `--rebuild=full` ("it asked for a fresh build"). This launch asked for an in-place
   Claude Code update, and builds only because the image is missing. The comment at
   :2018-2021 agrees with D4 ("An explicit --rebuild=full builds even after waiting"). The suite
   has no case for this branch.

   Fix: give this site the launcher's check:

   ```bash
   ccy_image_build_locked "$IMAGE_NAME" ccy_base_image_current "$REQUIRED_CONTAINER_VERSION" -- ccy_base_image_build --no-cache
   ```

   The unlocked check fails at once (no image), so a lone launch still builds with `--no-cache`.
   A waiter finds the holder's image current and uses it. Then add a suite case: two
   `base.bash`-style launches running this exact line on an empty image store give one build
   (2 builds.log lines), and add the line to `launcher_calls` (:186-193).

2. **NOTE: the buildah-cache recovery purges a shared cache outside the lock.**
   `claude-yolo:2186-2196`.

   - A's project build fails with the cache-permission error and releases the lock.
   - B, which was waiting, takes the lock and starts the same build.
   - A then runs `purge_stale_buildah_cache` (`rm -rf /var/tmp/buildah-cache-<uid>`, shared
     by every build) while B is building, and retries under the lock.

   B will likely also hit the stale cache first. This predates the branch, since concurrent
   builds raced it before, but the lock now makes the ordering systematic. Consider running
   purge-and-retry as one locked step: a build function that purges and rebuilds when the log
   shows the permission failure.

3. **NOTE: a holder killed during the restart path's update runs its EXIT trap with the lock
   descriptor open.** `claude-yolo:3797` runs after `trap cleanup EXIT` (:2411). SIGTERM, from
   `ccy --build-lock` or a reboot, makes bash run `cleanup`. That runs `ccy_bus_disconnected`,
   which calls `podman ps`/`podman stop -t 10` and `pingbus` under a timeout. Every one of them
   inherits the lock fd: the `{fd}>&-` covers only the build function, not the trap. The lock
   is released late (D6's 10 s wait then SIGKILLs the bash). If one of those commands left a
   long-lived child, as D5 fears of podman's pause process, that child would keep the lock.
   The pause process normally already exists, so this is unlikely. A cheap guard is to close
   the descriptor first thing in `cleanup` when it is set, or to document it in D5.

4. **NOTE: an inherent window between the last vouch and the signal.**
   `buildlock.go:186-202`. If the build ends between `stillHolds` and `signalTree` (one /proc
   walk), the launcher that has just finished its build is SIGTERMed while it carries on
   towards its session. Its later children, such as the session's `podman run`, are not in the
   snapshot. They could outlive a launcher that is killed after starting them, though that
   needs the whole launch tail to fit inside the walk, so in practice it means only killing a
   launch that was about to proceed. It cannot be closed from outside the lock. One more
   `stillHolds` immediately before the SIGTERM round would shrink it to the signal loop itself.
   Worth one line in D6.

5. **NOTE: the "Project image built successfully" message after a wait.**
   `claude-yolo:2204-2208`. A launch that waited and used the holder's project image still
   prints "✓ Project image built successfully … Rebuild reason: first time" after the lock's
   "was built while this launch waited; using it". The same holds after the cache-recovery
   retry ("Build succeeded after cache recovery" when it in fact used another launch's build).
   This is cosmetic.

6. **NOTE: the waiting line can show a negative age.** `image-build-lock.bash:79-80`. If the
   record's `began` is ahead of `$EPOCHSECONDS` (a clock step), it prints e.g. `-1m-30s`. This
   is cosmetic.
