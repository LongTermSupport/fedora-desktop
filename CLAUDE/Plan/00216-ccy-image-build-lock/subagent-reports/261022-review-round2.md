# Plan 00216 review, round 2 (adversarial, Opus, fresh reviewer)

Verdict: FIX-FIRST

Scope: `git diff F44...HEAD` on feat/ccy-image-build-lock (7877a9c8b and 9b633d49f). I ran
`./scripts/test-ccy-image-build-lock.bash` (78 passed, 0 failed, no stray `kill` errors) and
`go test ./internal/ccylaunch/...` (ok). I also checked by hand, with a throwaway script, that a
background subshell started inside a function called as `fn {fd}>&-` holds no copy of the lock:
the lock was free while the subshell still ran.

## Round 1, checked

- R1-01 (the stop could signal a process that is no longer the build): fixed. `stopBuild`
  re-probes the lock and record and re-vouches the holder after the answer (`stillHolds`,
  buildlock.go:212-223). Each tree pid is signalled only while its start tick is unchanged
  (`signalTree`/`sameProcess`). The SIGKILL round walks the holder's tree again only while the
  holder is still the same process (`snapshotTree`, :239). All three tests round 1 asked for are
  there (`TestBuildLockStopsNothingWhenTheBuildEndedWhileAsking`,
  `TestBuildLockKillsOnlyTheSameProcesses`).
- R1-02 (the daily update's re-check was not the launcher's): fixed. The daily update now
  re-checks with `ccy_claude_is <image> <latest_version>`, using the same first-x.y.z extraction
  as `auto_update_claude_code`. `--unless-changed` is used only for explicitly requested
  updates (D4).
- R1-03 (no errexit inside the build): fixed. `run_project_build` returns on a failed pipeline
  and chains the hash writes with `&&`; `ccy_base_image_build` checks every step;
  `build_container_with_hash` returns on the base stage; the library header says so.
- R1-04 (the suite's last `--build-lock` case): fixed. builds.log is truncated, the new holder
  is required to differ from the old victim and to be alive, and the status of `kill -9` is
  checked.
- R1-05 and R1-07 were accepted into D6 and D4. R1-06: "stopped" now also waits for every tree
  process to end. R1-09 (help alignment) and R1-10 (src-go/CLAUDE.md F14) are fixed. R1-11 is
  unchanged and predates this branch.

## Checked afresh and sound

- **Unlocked check, then the lock.** The check always runs again under the lock, so a launch
  that loses the race without waiting still re-checks.
- **The holder's release and the next holder's record write.** The record is emptied before
  the release. A write through a separate open neither drops nor takes the flock (flock
  belongs to the open file description).
- **The Go probe's momentary shared lock.** The cost is one false waiting line (D6).
- **A holder killed mid-build:**
  - SIGKILL, SIGTERM and Ctrl-C all free the lock, because the kernel releases the flock and
    no child has the descriptor.
  - The stale record left behind is named stale by both readers and overwritten by the next
    holder.
  - The launcher has no TERM trap, only EXIT, so a SIGTERM really ends the holder.
- **Pid reuse.** `unvouched` compares `btime + start/100` with `began`. With integer
  truncation this is never later than the true start, so a real holder is not refused while
  the clock is not stepped. A reused pid always starts after `began`.
- **Build sites.** Every `container_cmd build` and `commit` in the launcher and lib/, and the
  play's build, are under the lock. The only other image build in the repo is
  play-claude-devtools.yml (a different image). Nothing in src-go builds an image. The lock
  keys agree: the daily update and the restart path lock the image they update, the same key
  the project build uses.
- **A failed build fails the launch.** The base step runs at top level under `set -e`. The
  project build keeps its diagnostic fallback, and the daily update its FAIL-FAST-OK
  continuation.
- **The play.** It runs `become: false` (play level), as the user, so it uses the same
  `$XDG_RUNTIME_DIR` fallback as the launcher. The libraries are deployed before the build
  task, and the check is `false`, so no `container_cmd` is needed in its `bash -c`.
- **Public repo.** No host-specific names in the diff.

## Findings

1. **FIX-FIRST: the only real-process test of the stop path cannot tell a stop from a
   non-stop.** `scripts/test-ccy-image-build-lock.bash:456-470`. The Go unit tests use a fake
   /proc and a fake kill. This case is the only one that exercises the real parts:
   `/proc/<pid>/stat` parsing, `btime`, `unvouched` against a real holder, `stillHolds`
   against a real flock, and real signals. Its assertions pass when those are broken:

   - `has "it names the holder" ... "process $victim"` also matches the unvouched message
     ("the lock is held, but its record names process N, ...: that record is stale ...;
     nothing to stop"). A btime or start-tick misreading on a real kernel would therefore
     pass the "answered no" half.
   - `has "it says the build was stopped" ... "stopped"` also matches "ended while you
     answered; nothing was stopped." and "Nothing was stopped.". These are exit 0 too.
   - The checks "the holder is gone" and "the lock is free" come after `wait "$pid_a"`. The
     fake build sleeps 30 s, under the 60 s `timeout`, so the build finishes by itself and
     both checks pass even if nothing was signalled.

   In short: if `stillHolds` or `unvouched` returned "not vouched" on a real system, which is
   exactly the code round 1 made stricter, the suite would still be green.

   Fix:

   - assert the exact lines: `claude-yolo:latest is being built by process $victim` in the
     no-run, `[y/N]` on its stderr, and `The build of claude-yolo:latest by process $victim was stopped.` in the yes-run;
   - check `alive "$victim"` = no and `lock_free` = yes immediately after `--build-lock`
     returns, before `wait`;
   - check that `wait "$pid_a"` reports a non-zero status (the launch was killed);
   - optionally, check that builds.log has no image file written for that build.

2. **NOTE: "nothing on stdout but the result" checks only one of the two launches.**
   `scripts/test-ccy-image-build-lock.bash:311` runs `lacks` on `a.out` only. Which launch
   waits is random, so half the time the check runs on the holder, which never waits. Check
   both a.out and b.out.

3. **NOTE: a zombie counts as a running process.** `buildlock.go:340-365`: `procStat` ignores
   the state field. A tree member that has exited but has not been reaped yet stays "the same
   process", so `buildDone` is false. A stop can then go on to SIGKILL (a no-op on a zombie)
   and end with "could not stop ... one of them still runs". Parents normally reap promptly
   (a shell, tmux, ansible), so this is unlikely. Treating state `Z`/`X` (`fields[0]`) as
   ended costs one line.

4. **NOTE: piped answers for a second build are lost.** `buildlock.go:142`: each `offerStop`
   makes a new `ui.Lines`, which wraps `In` in its own `bufio.Reader`. With two builds in
   progress (base and a project image) and the answers piped (`printf 'y\ny\n' |`), the first
   reader buffers both lines. The second prompt then sees end of input and exits 130,
   "Nothing was stopped.". A terminal is unaffected. One `ui.Lines` for the whole run fixes
   it.

5. **NOTE: a stopped in-place update leaves its temporary container behind.**
   `claude-yolo:1984-2016`: `update_claude_inplace` runs `ccy-claude-update-$$` without
   `--rm` and removes it only on its own return paths. When `ccy --build-lock` stops that
   holder (or it is killed in any other way), the container is left, and nothing removes it
   later. This predates the branch for Ctrl-C, but `--build-lock` now offers it as a routine
   action. Consider removing stale `ccy-claude-update-*` containers whose pid has gone, at the
   start of `update_claude_inplace`, or note it in docs/ccy.md.

6. **NOTE: the daily update starts one extra container.** `image-build-lock.bash:41`: the
   first, unlocked check for the daily update is `ccy_claude_is`, a `podman run --rm`. The
   launcher started that same probe moments before. It is correct but costs a container start
   on every daily update. It could be avoided by letting a caller skip the unlocked check
   (it has just made it).

7. **NOTE: `--build-lock` works only as the first argument.** `front.go:230` and
   `claude-yolo:168` both match only `$1`. `ccy --engine docker --build-lock` is passed to the
   bash parser, which has no case for it, so it falls through towards claude as an unknown
   option. Either reject it by name anywhere in the arguments, or document that it must come
   first.

8. **NOTE: a forward clock step can refuse a real holder.** `buildlock.go:327`. If the wall
   clock steps forward between a holder's start and its record write (an NTP correction), a
   holder that started within the step before `began` is reported as "another process given
   the same number" and is not offered. This matters for the play's `bash -c`, which writes
   its record within milliseconds of starting. The error is on the safe side; mention it in
   D5 or allow a small margin.

9. **NOTE: the `kill -9 "$victim"` in the "holder killed mid-build" case is unchecked.**
   `scripts/test-ccy-image-build-lock.bash:410`. This is the same class as R1-04, now fixed at
   :477. Check its status here too.
