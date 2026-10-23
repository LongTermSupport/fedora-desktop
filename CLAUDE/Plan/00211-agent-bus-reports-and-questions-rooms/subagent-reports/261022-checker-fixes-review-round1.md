# Checker fixes review, round 1 (Plans 00211, 00201, 00214)

Diff reviewed: `89e0a1a71..HEAD` (4 commits). Adversarial review, read-only.

## Verdict: FIX-FIRST

One blocking finding (M7's build grace can wait for ever, on any build on the host); the rest
are should-fix or notes.

## What was run

- `acceptance.bash --stage 2 --fake` (00211): exit 0; the four new A3 fake legs pass.
- `test_m7_check.py` (00201): 60 tests, OK.
- `test_check_changed_plays.py` (00214): 4 tests, OK.
- Red-first, checked: the new test files run against the plan folders at `89e0a1a71`
  (copied to `untracked/scratch/rv/old/`). `LaunchWaitTest`: 2 of 5 fail (the live-build
  restart and the launch-uses-`m7_poll_launch` tests; the other three pin behaviour the old
  code already had, which is correct for regression tests). `ReportCheckTest`: 3 of 4 fail
  (exit 3 with a play, exit 0 with a play, exit 3 with none). Both suites test the real
  checker files, not copies.

## Findings

### 1. BLOCKING (00201, fail-fast): the build grace is unbounded and not tied to this launch

`m7_poll` with `M7_BUILD_GRACE=1` resets the deadline on every poll while `m7_image_build`
finds any live record in `$XDG_RUNTIME_DIR/ccy/image-builds/*.lock`. Two consequences:

- A hung build (the failure mode `ccy --build-lock` exists to stop: "stops a hung one after
  a [y/N]", lib/image-build-lock.bash "if this one has hung") keeps the acceptance waiting for
  ever. The session is alive (it is blocked in `flock -w` or in the build), so `m7_alive`
  never fires, and sudo is kept alive. Nothing ends the run; the owner sees a "waiting"
  line once a minute that does not even name `ccy --build-lock` as the way out.
- The record looked for is any image's, not the one this launch builds. Another project's
  build, or a play-claude-yolo.yml run in another terminal, extends M7's wait just as well.

Fix: cap the grace (a generous outer bound, e.g. 3600 s, failing with the build's pid and
image and the `ccy --build-lock` hint), and preferably count only a build whose holder is
this launch (the holder pid is the launcher's `$$`; it is `SESSION_PID` or a descendant of
it), so an unrelated build never extends the wait.

### 2. SHOULD-FIX (00201): `m7_image_build` judges records differently from `ccy --build-lock`

The comment says "judged as `ccy --build-lock` judges them"; it does not:

- `buildlock.go reportLock` first probes whether the lock is held (`probeLock`, a
  `LOCK_SH|LOCK_NB` flock). `m7_image_build` never probes, so it relies on the record being
  emptied; a holder killed with SIGKILL leaves a record whose pid is gone (handled), but
  see the next point.
- A non-empty record that does not parse is printed as `? <file>` and treated as a live
  build, with no holder check. `buildlock.go` treats an unparseable record as "starting" only
  when the lock is held, and as nothing when it is not. With finding 1, an unheld garbage
  record makes the wait endless.
- The comment says "A record not written yet is a build starting", but the code `continue`s
  over an empty file, which is exactly the not-written-yet state (the holder takes the flock
  on an empty file, then writes the record). The comment and the code disagree.
- `read -r ... || return 1`: a record without its trailing newline (read mid-write, or
  written by anything but `printf '%s %s %s\n'`) makes `read` return 1 and the whole M7 run
  FAIL with "the image build locks could not be read". Unlikely (one write(2)), but it is a
  spurious hard failure rather than "starting".

Fix: probe the lock as `buildlock.go` does (`flock -n -s` on a read descriptor), treat an
unparseable record as starting only while the lock is held, and make the comment match.

### 3. SHOULD-FIX (00201, tests exercise a stand-in record): the fixture hand-writes the record

`WAIT_DRIVER` writes `"<pid> <epoch> claude-yolo:latest\n"` with its own `printf`; the
production writer, `ccy_image_build_locked` in `files/var/local/claude-yolo/lib/image-build-lock.bash`,
is never run. If the lib's record format or location changes, these tests stay green and
the host check silently stops seeing builds (the old 600 s failure returns). The lib needs no
container engine for a `false` check: source it and run
`ccy_image_build_locked claude-yolo:latest false -- sleep 5` as the live case, so the record
is the product's.

### 4. SHOULD-FIX (00211, copy of product constants): `a3_join` hardcodes `0 | 3`

These are `helpers/pingbus/cli.py` `EXIT_OK` and `EXIT_NOTHING`. The 00214 fix in the same
batch reads `EXIT_FINDINGS` from the helper precisely to avoid such a copy; A3 should read
`cli.EXIT_OK`/`cli.EXIT_NOTHING` the same way (one `python3 -P -c` against the checkout, or
the installed zipapp's module). Not a false-pass risk today.

### 5. NOTE (00211): "joined" is printed on a path that did not sync

`_recv_team` exits 3 without syncing when the sync lock is busy
(`inbox.Busy`: "not syncing: a ... process holds the sync lock"). A3 then writes
"A3 worker: joined the team room by its first sync" to the report though no join happened.
It is not a false pass (the report then fails "not joined"), and a throwaway worker has no
keeper, so this is unlikely; but the report line is a claim the check did not establish. It
could check the stderr for the busy line, or word the line as "synced (exit N)".

Verified otherwise: exit 3 from a sync that ran implies the join and `verify_room` succeeded,
since `_first_sync` calls `verify_room()` when no invite was taken and that raises
`EXIT_UNTRUSTED` (10) with the exact "not joined, or the room or its team record is missing"
text (syncer.py:325). So the real production sync path (`sync_once` -> `_first_sync` ->
`_take_invites` -> `_after_join`) is what the host run exercises. The Element-refusal text
grepped by `a3_failed` matches `config._refuse_beside_element` (config.py:447). The `--fake`
pingbus is a hand-written stand-in (it imitates "invited, not joined" in verify_room's words);
acceptable for `--fake`, and it loads the bundle through the real `config.load_bundle`.

### 6. NOTE (00201, can pass on a wrong product): a launch that rebuilds every time now passes

Before, a launch that wrongly rebuilt the project image (a cache-busting regression) would
time out at 600 s. Now it passes, and nothing in the evidence records that a build happened
or how long. Recommend writing the build's image, pid and duration into the run report so a
rebuild on every launch is visible.

### 7. 00214: sound

`check_report` captures the helper's status with `|| status=$?`, reads `EXIT_FINDINGS` from
the checkout's `login_report` (the real constant, value 3 at login_report.py:71), fails on any
other status before anything else, and fails on 0 when `plays_to_run`. With no play to run,
0 and 3 both lead to NOT JUDGED, which is correct (a stale play is not the only finding).
Tests go red on the old checker. No finding.

### 8. meta-deploy.list descriptions

- 00211 entry: accurate to `a3_take`/`a3_join`/`a3_failed`.
- 00201 entry: accurate, and honestly says "with no cap on the build itself". It does not say
  the wait also extends for any other image's build on the host, nor how to stop a hung one
  (`ccy --build-lock`); it should, if finding 1 is not fixed, and must be updated if it is.
- 00214 entry: accurate to the checker. The claim that the four named plays are stale ("none
  has run since it changed") is a statement about the host ledger that cannot be verified
  from the container; it is hedged by the NOT JUDGED sentence, so acceptable.
