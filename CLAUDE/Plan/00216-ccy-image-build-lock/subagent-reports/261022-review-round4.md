# Plan 00216 review, round 4 (adversarial, Opus, fresh reviewer)

Verdict: MERGE

Scope: `git diff F44...HEAD` on feat/ccy-image-build-lock (7877a9c8b, 9b633d49f, 2a8081958,
5a062ed35). I ran `./scripts/test-ccy-image-build-lock.bash` (85 passed, 0 failed) and
`cd src-go && go test ./internal/ccylaunch/...` (ok, difftest included). Read-only otherwise.

## Round 3, checked

- R3-01 (`--rebuild=claude` with no base image built twice): fixed. `claude-yolo:2027` now
  passes the launcher's own check, `ccy_base_image_current "$REQUIRED_CONTAINER_VERSION"`, to a
  `--no-cache` build. The suite has the race ("--rebuild=claude with no image: two launches at
  once build it once", 2 build lines). The line is in `launcher_calls`, so the suite fails if the
  launcher drifts from it.
- R3-02 to R3-05 are recorded as accepted in D6. R3-06 (a negative age) is unchanged and
  cosmetic.

## Checked afresh and sound

- **The lock function** (`lib/image-build-lock.bash:28-69`):
  - The check runs unlocked first, and again under the lock with the descriptor closed and its
    stdout discarded.
  - Only `flock` itself and builtins run with the descriptor open.
  - The record is written through a separate truncating open, so the flock on the other open
    file description is kept.
  - The record is emptied before release.
  - A flock exit other than 75 fails with a message.
  - `-E 75` covers both the `-n` conflict and the `-w` timeout.
  - The function behaves correctly at top level under `set -e` and in `if` context: every step
    is `||`-guarded or a condition.
  - The function never nests, so one process never waits on itself.
- **Every check is the launcher's own.**
  - Base: existence plus `validate_container_version`, exactly the old pair.
  - Daily update: `ccy_claude_is` matches the old `grep -oP | head -1` extraction against
    `latest_version`.
  - Project image: the old `should_build` conditions. The hash files are now written inside
    `run_project_build`, under the lock.
  - Explicit in-place updates use `--unless-changed`. `--rebuild=full` and the play use `false`,
    as D4 says.
  - The keys agree: after line 2255 `IMAGE_NAME` is the project image for the daily update and
    the restart path alike.
- **Build sites.** Every `container_cmd build`/`commit` in the launcher and lib/ is under the
  lock, and so is the play's `podman build`. The play runs with `become: false`, so it resolves
  the same `/run/user/<uid>` lock directory as the user's launches, and its library is installed
  earlier in the same play. `build_container_with_hash` has no other caller, and src-go builds
  no image. Only `play-claude-devtools.yml:64` builds without the lock, and that image is a
  different tool's (NOTE 4).
- **A holder killed mid-build.** The kernel frees the lock and the record is left behind. The
  bash waiter and the Go front both name it stale, and the suite proves the waiter then builds.
- **The Go stop path** (`buildlock.go:185-309`):
  - `stillHolds` runs after the answer.
  - A process is signalled only while its start tick is unchanged.
  - New children are added in the SIGKILL round only while the holder is unchanged.
  - "Stopped" needs both the record gone and the whole tree gone.
  - A waiter that takes the lock is never in the tree.
  - At every lock site the holder (`$$`, which the Go front `exec`s into) has no running
    session under it. The restart path's update runs after the session's container has exited,
    and the helpers still running then belong to that launch.
- **Stderr hygiene.** The waiting and "used the holder's build" lines go to stderr, as does the
  `[y/N]` prompt. The report and its answers go to stdout. A usage error is exit 64.
- **Project rules.**
  - Version 3.99.15.3 is bumped, with its changelog entry.
  - Both new libraries are in `CCY_LIBS` and in the play's list.
  - The allowlist only shrank.
  - The gate row and the `addedgates` entry are present.
  - The meta-deploy entry has a host check.
  - No host-specific names appear in the added lines. `Owner: joseph` is the scaffolder's
    git-identity default, as on 111 other plans.

## Findings

1. **NOTE: `--unless-changed` counts any change to the image's ID as "updated".**
   `claude-yolo:3797` (restart path), and `:2027-2045`/`:2140` (`--rebuild=claude|project`).
   For example, launch B is on the restart path and waits on the project lock while launch A
   rebuilds that project image (its Dockerfile changed) from a `claude-yolo:latest` that still
   has the old Claude Code. B's check sees a new ID and skips the update. B then stamps
   `VERSION_CHECK_CACHE` at `:3804`, so the daily check is also skipped for today, and the
   session restarts on the old Claude Code. A base rebuild that hit the npm layer cache has the
   same effect on `:latest`. D4 accepts the ID check, and the case is narrow, which is why this
   is a NOTE. If it is to be closed: on the restart path, stamp the cache only when this launch
   ran the update itself, or judge with `ccy_claude_is` against the registry's latest version
   as the daily update does.

2. **NOTE: the daily update starts one more container than it needs.** `claude-yolo:1957`.
   `auto_update_claude_code` has just found the image's version (`:1912`) and seen that it
   differs from `latest_version`. `ccy_image_build_locked` then runs `ccy_claude_is` unlocked
   again, which is a second `podman run --rm` and costs a container start before every update.
   This is harmless, because the locked re-check is the one that matters. A cheap fix is a
   leading `--locked-only`-style mode, or accepting the cost.

3. **NOTE: the "DEVELOPER ERROR" banner prints twice on stderr for a lone launch.**
   `lib/common.bash:978-995` writes its "Dockerfile modified without version bump" banner to
   stderr. The locked re-check (`image-build-lock.bash:61`) discards only stdout, so a launch
   that waited for nobody shows the banner twice. The ordinary version-bump banner goes to
   stdout and correctly appears once (golden `launch-image-older.txt`). This is cosmetic. The
   fix is `2>/dev/null` on the locked re-check as well: the unlocked run has already said why.

4. **NOTE: the devtools image builds without the lock.**
   `playbooks/imports/optional/common/play-claude-devtools.yml:64` builds
   `{{ ccdt_image }}:latest` with no lock. It is not a ccy image and no ccy launch builds it, so
   the requirement as stated does not cover it. It is recorded here only so that D1's "every
   build site" is not read as covering it.

5. **NOTE: a missing runtime directory gives a misleading error.** `image-build-lock.bash:46-47`.
   With `XDG_RUNTIME_DIR` unset and no `/run/user/<uid>` (for example a `su` shell without
   pam_systemd), `mkdir -p` fails, and the message says the directory "must be a directory of
   your own, not a link". The launch still fails fast. Only the wording misdirects.
