# Review: rclone-tail Go port, round 2 (Opus, adversarial)

Branch `go-port/rclone-tail` at `3f18eac71`, diff against F44. Round 1:
`261023-review-rclone-tail-r1-opus.md` (M1, L1, L2).

What I ran: `go test -race -count=1` over `internal/rclonetail`, `internal/rclonerc`,
`internal/rclonecache`, `cmd/rclone-tail` and `internal/ftpcamera` (all pass), and `go vet` on
`internal/rclonetail`. I built the binary into `untracked/scratch/rtrev2/` and ran it over the
round 1 fakes (`findmnt`, an `rclone` whose RC calls take 0.4 s or 4 s, and a fake `rclone mount`
process). Each run was in a process group of its own and got signals sent to the whole group, the
way a terminal sends them. The scripts are `race.bash`, `orphan.bash` and `orphan2.bash`. No
Ansible was run and no file under src-go, playbooks or scripts was changed.

## Round 1 fixes: checked

- **M1 is fixed.** `deploy-rclone-tail.bash:53-56` runs `--tags rclone-tail,rclone-cache-status`,
  and its header, PLAN_USAGE, SPEC section 7 and PORTS.md all say so. Both tags are on the
  `play-golang.yml` import and on their own build task (`play-rclone.yml:96-100, 490-512`), so
  the mounts stay up.
- **L1 is fixed.** `detached` (`app.go:333-340`) sets `NewSession` on every child, and the
  snapshot uses the same wrapped runner (`app.go:127, 214-218`), so that covers rclonecache's
  calls as well. `TestStopAfterLastCall` asserts `NewSession` on every recorded call, the closing
  snapshot's included.
  - SIGINT to the whole group during the opening snapshot: 0 of 6 runs had an `rc unreachable`
    line, and all 6 exited 0. The snapshot printed true figures. Round 1 saw that line 5 of 5
    times.
  - SIGINT during a tick: 0 of 6 runs had the line. The heading was printed with no mount line,
    then an empty line and the closing snapshot, as E15 says.
- **L2 is fixed.** `tick` now checks `ctx.Err()` after each `describe` (`app.go:277-279`). That
  covers the no-`--rc` line, the more-than-one `--rc-addr` line and the credential-missing line,
  because `ask` turns any error into errStopped once ctx has ended. The `--once` branch turns a
  nil tick after a stop into errStopped (`app.go:153-156`).
  - `TestStopAfterLastCall` covers the first half: a stop after the last call, with a no-rc
    mount after it.
  - `TestStopDuringLastLine` covers the second half: a stop while the last line is written.
  - Both assert the exact stdout.
- **rclonerc `Ask` contract:** it returns only `*RCError` or ctx's error. So the
  `!errors.As(err, &rcErr)` branch in `ask` (`app.go:359`) is reached only after a cancel, and no
  real failure is read as a stop.

## Findings

### L1 (Low, new, from the L1 fix): a second signal leaves the snapshot's `rclone rc` running

Where: `app.go:63-64, 127, 192-197` together with `execx/session.go:23-25`.

- The cause: E15 releases the signals before the closing snapshot, "so a second Ctrl-C during it
  ends the process at once". Each child now runs in a session of its own. The terminal's second
  signal therefore no longer reaches it, and execx kills a session group only when the run's ctx
  ends. The snapshot runs on the root `context.Background()` (`cmd/rclone-tail/main.go:18`),
  which nothing cancels. The process does die, but its child is left behind.
- Reproduced with `orphan2.bash`:
  1. `rclone rc` takes 4 s.
  2. SIGINT goes to the group during the opening snapshot.
  3. SIGTERM goes to the group 1.5 s into the closing snapshot.
  4. rclone-tail exits 143, but `rclone rc --url=http://h:1 vfs/stats` is still running, with
     the RC credential in its environment. It runs until its own call ends; against an address
     that drops packets, rclone's connect timeout is a minute.
- Before the fix the group signal ended the child too. This difference is not listed in the
  spec, and D5 ("the terminal's Ctrl-C reaches this run alone") now holds where E15 needs the
  opposite.
- (Harness note: `orphan.bash` used SIGINT for the second signal and it was ignored. That is an
  artefact of the harness, because a non-interactive bash starts background jobs with SIGINT
  ignored. `orphan2.bash` uses SIGTERM.)

Fix: instead of releasing the signals to their default action, run the closing snapshot on a
context that a second `OnStop` cancels. execx then SIGKILLs the snapshot's session groups, and
the run exits at once with a distinct code (for example `term.ExitCancelled`). E15 and the
`onSignal` comment would then say "a second signal ends the snapshot and its calls". Add a test
in which the fake's snapshot call sees the second stop and returns ctx's error, and assert that
the run returns without waiting for it.

### Nit: a stop just after `Mounts` still prints the heading, or ends as "no mount"

Where: `app.go:249-271`. If the stop arrives after the check at line 249, one of two things
happens:

- The heading is printed. A following mount line is still suppressed, so this matches E15's
  "before its next mount line".
- With no matching mount, the run exits 1 with no closing snapshot.

The window is microseconds wide and harmless. Optionally, check `ctx.Err()` again before the
heading and before the no-mount return.

### Nit: SPEC-rclone-tail.md E15 line not rewrapped

`SPEC-rclone-tail.md:129` is longer than the 100 columns the rest of the spec is wrapped to, and
the line length looks like an artefact of the edit.

## Checked and found sound

- **rclonecache refactor:** `Mounts`/`RCAddr`/`Ask` keep its behaviour. B12 (`subject`
  returning the target as a name under `--no-dir`) is correct, and an empty target still means
  every mount. usage.txt and SPEC-rclone-cache-status D3/E4/E9/4a/B12 match the code.
- **ftp-camera:** its call (`teardown.go:196-197`) and its tests are unaffected. A SIGTERM from
  its cancel reaches rclone-tail's `OnStop`, which cancels the tick and kills the child sessions.
- **Play and QA wiring:** the build task tags, the `go-binary-suites.tsv` row, and the removal of
  the allowlist, qa-routing and callers_test entries are right. The `rclone-rc-auth.bash`
  comment is updated, and only `rclone-cache-warm` still sources that library.
- **Docs:** src-go/CLAUDE.md, PLAN.md and PORTS.md match the branch's state (not merged, H1
  pending the merge).
- **Stdout/stderr hygiene and the exit codes:** these hold in every path I ran.

VERDICT: CHANGES
