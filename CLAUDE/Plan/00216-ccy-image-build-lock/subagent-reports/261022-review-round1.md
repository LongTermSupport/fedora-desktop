# Plan 00216 review, round 1 (adversarial, Opus)

Verdict: FIX-FIRST

Scope: `git diff F44...HEAD` on feat/ccy-image-build-lock (commit 7877a9c8b). Ran
`./scripts/test-ccy-image-build-lock.bash` (66 passed, but see finding 4: one `kill` printed
"No such process"), `go test ./internal/ccylaunch/...` (ok), `qa-bash-limits.bash` (OK), and
shellcheck on the two new libraries and the suite (clean). I checked empirically that
`fn {fd}>&-` keeps the lock descriptor out of an external child and out of a background subshell
started inside the function (the subshell holds no copy, and the lock is free while it is still
running), so D5's descriptor closing holds.

What holds up: the flock is per open file description, and the record rewrite (`>"$file"`, the
same inode) neither drops it nor races it. The record is emptied before the release, and a reader
treats an empty or half-written record as "not written yet". The base re-check under the lock is
the launcher's own check (`ccy_base_image_current`). The project hash files are now written
inside the lock. The failed-build path returns the build's status, frees the lock and fails the
launch. Every `container_cmd build` and `commit` site is under the lock; the only other image
build in the repo is play-claude-devtools.yml, a different image. The Go front's runtime dir
matches the bash's (`playlock.RuntimeDir`).

## Findings

01. **FIX-FIRST: `ccy --build-lock` can signal a process that is no longer the build, including
    a live ccy session.** `src-go/internal/ccylaunch/buildlock.go:93-96, 155, 168-195`.
    `reportLock` checks the holder once, before the `[y/N]` prompt. `stopBuild` then signals
    `rec.pid` and its tree with no further check, however long the prompt stayed open. The
    holder is the whole launcher (`$$`). If the build finishes while the prompt is open, that
    launcher goes on to start the session (podman run, the supervisor, the agent filter), and a
    late `y` sends SIGTERM to it and every process under it: a running Claude session is killed.
    If the holder exits and its pid is reused, the reused process and its children are signalled.
    The SIGKILL round (`:177-188`) has the same gap. It re-walks `processTree(rec.pid)` after the
    holder may have died, and it SIGKILLs the pids of the first walk without checking that they
    are still the same processes.
    Fix: in `stopBuild`, immediately before each round:

    - re-probe the lock: it must still be held, and its record must be byte-identical to `rec`
      (pid and began);
    - re-run `unvouched(rec)`;
    - record each tree pid's start tick from the first walk, and signal a pid only while its
      start tick is unchanged and it still descends from `rec.pid`.

    If any of these fail, print "that build has ended; nothing was stopped" and exit 0. Add
    tests for each: the record changed while the prompt was open, the lock was let go while the
    prompt was open, and a pid reused between the rounds.

02. **FIX-FIRST: the in-place update's re-check is not the check the launcher makes (Goal 2,
    D4), so updates are still repeated.**
    `files/var/local/claude-yolo/lib/image-build-lock.bash:37-39` and
    `files/var/local/claude-yolo/claude-yolo:1913-1957`. `auto_update_claude_code` decides to
    update from a version probe (`podman run ... --version`, a container start) and a curl with
    up to 5 s of timeout. `seen` is taken only afterwards, inside `ccy_image_build_locked`.

    - **Repeated update.** If another launch commits its update during that window, this
      launch's `seen` is already the new ID and it updates again. This is the realistic case:
      sessions started a few seconds apart, as `ccy-sessions restore` starts them after a reboot
      (the first launch of the day is when the daily update fires).
    - **False "updated" message.** The reverse also misleads. Any ID change counts as "updated",
      for example a project-image rebuild under the same lock. The launch then stamps the daily
      cache and prints "✓ Claude Code auto-updated to X" without the version having been checked.

    Fix: take the image ID at the start of `auto_update_claude_code`, before the version probe,
    and pass it in (for example `--unless-changed-from <image> <id>`). Better: make the
    under-lock check re-probe the Claude Code version against `latest_version`, which is the
    launcher's actual check. Add a suite case where the second launch's ID is read before the
    first launch's commit.

03. **FIX-FIRST: build functions now run without errexit, and two writes fail silently
    (fail-fast).** `image-build-lock.bash:63` runs the build as `"$@" {fd}>&- || rc=$?`, so
    `set -e` does not apply anywhere inside it.

    - `claude-yolo:2178-2179` (`run_project_build`): before this branch the hash files were
      written at top level under `set -e`. Now a failure of the first `echo >` is ignored, and the
      function's status is the second write's.
    - `files/var/local/claude-yolo/lib/image-build.bash:31` (`ccy_base_image_build`): a failing
      `rm -rf "$VERSION_CHECK_CACHE"` is ignored and the build reports success.

    Fix: add `|| return` (or `|| return 1`) after each of those statements. Add one line to the
    library header: the build it runs gets no errexit, so every build function checks each of
    its own steps.

04. **FIX-FIRST: the last `ccy --build-lock` case in the suite does not test what it claims.**
    `scripts/test-ccy-image-build-lock.bash:434-443`. There is no `reset_world` and no truncation
    of `builds.log` between 428 and 434, so `wait_for ... "build claude-yolo:base"` returns at
    once on the previous run's line. `holder_pid` then reads the stale record of the holder that
    `--build-lock` has just stopped, and `kill -9 "$victim"` fails. The run shows this:
    `line 438: kill: (...) - No such process`, and the exit status is not checked. The new launch
    is never killed: `wait "$pid_a"` sits through its 60 s `timeout`. The "named stale" assertion
    passes off the old record.
    Fix:

    - `: >"$TEST_STATE/builds.log"` (or `reset_world`) before 434;
    - wait until the lock's record names a live pid different from the previous victim;
    - check the status of `kill -9`;
    - assert that the stale record names that pid.

05. **NOTE: the Go probe's shared lock produces a false waiting message.** `buildlock.go:346`.
    While `--build-lock` holds `LOCK_SH`, a launch's `flock -n` fails, and the launch prints
    "waiting for another build ... (it has not written its record yet)" when no build is running.
    This is harmless, since the next `flock -w` gets the lock at once, but it is misleading.
    Possible fix: when the record is empty, retry `flock -w 1` once before printing.

06. **NOTE: "stopped" is reported when only the lock is free.** `buildlock.go:189-191`. The
    holder bash dies first, which frees the lock, so the run returns after SIGTERM without
    confirming that the podman/crun children have exited. A child slow to handle SIGTERM keeps
    building beside the waiter that now takes the lock: D5's SIGKILL caveat, reached through
    SIGTERM. Consider waiting for the tree (start-tick-checked, as in finding 1) to exit within
    `termWait`, then SIGKILL the survivors, before saying "stopped".

07. **NOTE: a failed holder build is repeated by each waiter in turn.** A waiter whose holder's
    build failed finds the image not current and runs the same failing build itself, so N
    launches started together fail N times in sequence. This is acceptable under the owner's
    wording, but worth one line in D4.

08. **NOTE: the suite re-types the launcher's call sites.** `scripts/test-ccy-image-build-lock.bash`
    (`base.bash`, `project.bash`, `update-launch.bash`) writes its own copies of the launcher's
    `ccy_image_build_locked` calls. Only `update_claude_inplace` and `run_project_build` are
    lifted from the launcher. A change to the launcher's own call (its check, the image key, or
    the `--` placement) is not caught, and `auto_update_claude_code`'s ordering (finding 2) is
    never exercised. Consider lifting the call lines from the launcher, or asserting with grep
    that they are present verbatim.

09. **NOTE: help alignment regression.** `claude-yolo:265`, `src-go/internal/ccylaunch/help.txt:90`
    and the re-recorded goldens: `--prevent` lost one space of padding, so its description no
    longer lines up with the other options.

10. **NOTE: doc drift.** `src-go/CLAUDE.md` still describes `internal/ccylaunch` as "F1-F13 in
    its package doc"; the package doc now has F14.

11. **NOTE: the play does not rebuild `claude-yolo:base` (existed before this branch).** The
    play builds only `:latest` under the lock (check `false`). A launch that waited for the
    play's build accepts `:latest` and leaves `:base` and the update-check stamps as they were.
    This branch did not cause it; noted because D2 says `:latest`'s lock covers `:base`.
