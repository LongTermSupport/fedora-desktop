# Checker fixes review, round 2 (Plans 00211, 00201, 00214, and the sweep's 00161, 00194, 00203, 00109)

Diff reviewed: `89e0a1a71..HEAD` (5 commits), with the focus on 814237ad4, the round-1 fixes
and the DBF sweep. Adversarial review. Read-only apart from this file.

## Verdict: MERGE

Nothing blocks. One should-fix (sudo can lapse during U20's build wait), which is not in the
meta-deploy queue, cannot produce a false pass, and fails fast. Everything else is a note.

## What was run

- `00211 acceptance.bash --stage 2 --fake`: exit 0.
- `test_m7_check.py`: 60 tests OK. `test_check_changed_plays.py`: 4 tests OK.
- `scripts/test-planlib.bash`: passed 168, failed 0. The one UNVERIFIED (a mode 0000 directory
  read as root) is expected in this container.
- `00194 acceptance.bash --fake`: exit 0.
- `shellcheck -x -S warning` over the four acceptance dispatchers that now source
  `_image-build.inc.bash`, the include itself and 00109's triage: clean.

## Round-1 findings, re-checked against the source

- **R1-1 (no cap):** kept uncapped, as the owner instructed. I found no case where the uncapped
  grace lets a check pass on a wrong product. The grace runs only after the polled check has
  returned 3, never in place of it. Every caller still checks that the launch is alive first
  (`m7_alive`, `u20_sessions_alive`, `gb_alive`, `tmux has-session` for M2), so a failed build
  still fails at once. The message names `ccy --build-lock`.
- **R1-2 (judging):** `plan_image_build` now matches `buildlock.go reportLock`/`unvouched`
  case by case. An unheld lock counts as no build, whatever its record says. A held lock with
  an unparseable or empty record is a build starting (`? <image>`; `${image/_/:}` is the same
  as `imageOfLock`). A held lock whose pid has gone, is a zombie, or started after `began` is
  stale. The probe (`flock -n -s -E 75`) is the same LOCK_SH|LOCK_NB that `probeLock` takes.
  The old `read ... || return 1` failure on a record with no trailing newline is gone, because
  the record is now read as `$(<file)` followed by a here-string.
- **R1-3:** both suites now drive the real `ccy_image_build_locked` from
  `files/var/local/claude-yolo/lib/image-build-lock.bash` (check `false`, build `sleep`). The
  production record writer and the location of its lock file are what get exercised.
- **R1-4:** `a3_join` reads `cli.EXIT_OK` and `cli.EXIT_NOTHING` with `python3 -I`, and fails
  when the read fails.
- **R1-6:** M7's report records "waited for an image build first".
- **R1-8:** the 00201 meta-deploy text says any ccy image build counts, and how to stop a hung
  one.

## Findings

### 1. SHOULD-FIX (00161 U20, fail-fast but defeats the fix): sudo can lapse during the build wait

For a seated launch, the launcher takes its seats after the image build: `ccy_seats_take` is
at `claude-yolo:3876`, while the project build is at about line 2189. With `--no-prompt` the
take goes through `sudo -n agent-bus` (`helpers/agent_bus/checkout.py:441`), and U20's first
launches create their seats that way.

M7 keeps sudo's timestamp fresh, because `m7_poll` runs `m7_sudo_alive` (`sudo -n true`) on
every poll. `u20_poll` makes no sudo call, and neither does `u20_container_up` (podman and jq
only). So a project build that runs longer than sudo's `timestamp_timeout` (5 min by default)
leaves the harness waiting correctly. The launch then fails to take its seat, and U20 fails
with "Not starting: agent-bus seat take ...".

This is a fail, not a false pass. But it is still "a legitimate build fails the check", the
class the sweep set out to fix. The fix is to call `u20_refresh_sudo` in `u20_poll` when
`U20_BUILD_GRACE=1`, as `m7_poll` does. 00194's goodbye run is not affected, because it takes
its seats from the run's terminal before the launch. 00203's M2 sessions run in tmux with no
sudo credential, so it is not affected either.

### 2. NOTE (tests): the three sweep call sites are untested

- U20's wiring: `u20_poll_launch` with `local U20_BUILD_GRACE=1`.
- The goodbye wiring: `gb_poll_launch`.
- M2's inline grace in `m2_container_up`.

I read all three and they are correct. But no test drives them, so a typo in any of them would
first show on the host. Only M7's `LaunchWaitTest` and `test-planlib.bash` exercise the grace
logic.

### 3. NOTE (00211 A3): the "joined" line is still unproven

Round-1 finding 5 is still open. When the sync lock is busy, recv exits 3 without syncing, and
A3 still prints "joined". It cannot pass a check: `a3_report` then fails with "not joined",
and A3's state directory is its own. The handling recorded in the DBF record is acceptable.

### 4. NOTE (hidden error, low risk): `ps` errors other than a missing pid are read as stale

In `plan_image_build`, `ps -o stat= -o etimes= -p "$pid" || continue` treats any `ps` failure
as a stale record, not only a process that has gone. The worst outcome is the old 600 s
failure. It cannot produce a pass.

### 5. NOTE (cosmetic): one message clock across all waits

`PLAN_IMAGE_BUILD_SAY_AT` is a single global clock. A new wait that starts within 60 s of the
last "waiting for the image build" line stays silent until the minute has passed.

### 6. 00214 `check_report`: sound

It reads `EXIT_FINDINGS` from the checkout with `/usr/bin/python3 -P` and fails if that read
fails. Any status other than 0 or EXIT_FINDINGS fails the leg. A status of 0 while a play is
to run also fails. With no play to run, the run is NOT JUDGED. It exercises the production
`login_report`.

### 7. 00109 triage labelling: sound

The findings code is read from the helper, not copied. A status of 0, findings or a crash each
gets its own label. If the read itself fails, the triage stops.

### 8. meta-deploy.list: accurate

The 00211, 00201 and 00214 entries match the code. The 00201 entry runs before the
play-claude-yolo.yml entry, but the 00213 entry above it has already run that play, so the
build-lock lib is on the host when M7 runs. 00161, 00194 and 00203 are not queued, so no entry
is owed for them.

### 9. Sourcing (f): no regression

Every caller that reaches `plan_image_build_grace` sources `_image-build.inc.bash` at the top
level of its dispatcher:

- 00161 acceptance (U20).
- 00194 acceptance (goodbye, and M7 through `P201`).
- 00201 acceptance.
- 00203 acceptance (M1, M2, M3 and M4 all reach `m2_container_up`).

`test_goodbye_check.py` sources the goodbye include on its own, but it only calls
`has-session`, never `gb_poll`. Under `set -u`, every new variable is either local or set when
the file is sourced.
