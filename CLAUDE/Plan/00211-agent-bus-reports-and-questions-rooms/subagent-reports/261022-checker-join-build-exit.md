# Three checker defects from the b0eb800fa host run: A3's join, M7's build wait, 00214's exit 3

One branch, one commit per fix. The product passed in every case; each was the checker. This
report is the Defence Before Fix record for all three (Plan 00211 Task 2.11, Plan 00201 Task
2.23, Plan 00214 Task 4.3c); the 00201 and 00214 reports point here.

## 1. Plan 00211 A3: an invited worker that never joined

**Class: invite-without-join fixture.** A checker adds a member (`agent-bus add-member`, which
only invites, as for every member) and then acts as it in a way that needs the room joined,
without the member ever syncing (the production join, `Syncer._take_invites` on the first
sync) or joining.

**Fix.** `a3_take` ends with `a3_join`: one `pingbus recv --team <team>` as the worker, from
the worker's own session home. That is the production path: `recv` and the keeper's `watch`
both run `Syncer.sync_once`, whose first sync takes the main room's invite, joins, and
verifies the room (`_after_join` → `verify_room`), or fails (exit 10). Exit 0 or 3 (nothing
pending, the usual first sync) passes; anything else fails with pingbus's error. A sync was
practical, so the raw `/join` fallback was not used: a `/join` would skip the verification a
real seat does.

**Hint.** `a3_pingbus` keeps pingbus's stderr; `a3_failed` names the Element step only when
that stderr holds `config._refuse_beside_element`'s refusal, otherwise it says the failure is
pingbus's own error shown above.

**Proof, red first** (`acceptance.bash --stage 2 --fake`, `fake_a3_seat`): a fake pingbus loads
the bundle through this checkout's `config.load_bundle` and, like the homeserver, refuses a
report from a worker that never synced, in `verify_room`'s words. Against the old `a3_take`:
`accepted ok` FAILED with exactly the host's error; `accepted join-refused` FAILED ("blamed the
wrong cause"); `accepted report-refused` FAILED ("without naming its cause"). `unaccepted ok`
passed on both (the old hint was right for that one case). All four pass after the fix.

**Detector: not written.** Whether an added member must be joined depends on what later runs
as it: Plan 00201's M7 adds `m7peer`/`m7solo` only to be named as `say --to` targets (it even
deletes their tokens), which needs no join; and the join can come from a sync in another
process (a keeper, an LXC guest's recv, Plan 00161's `place_member`). A text rule cannot tell
these apart without following data across processes, so it would be either unsound or noisy.

## 2. Plan 00201 M7: a fixed container wait that counted an image build

**Class: fixed wait that ignores an image build.** A checker launches ccy and waits a fixed
time for the container, while the launch may first build the project image under the build
lock, which takes as long as it takes.

**Fix.** `m7_poll_launch` (both start waits of `m7_start`): `m7_poll` with `M7_BUILD_GRACE=1`,
which restarts the `M7_START_S` deadline whenever `plan_image_build_grace` (the new shared
`CLAUDE/Plan/_image-build.inc.bash`) sees a build. `plan_image_build` judges the locks as
`ccy --build-lock` does (`buildlock.go` `reportLock`): a build runs while its lock is held
(probed with a shared `flock -n`, as `probeLock` does), unless the record names a process that
has gone or one started after the build began; a held lock with no record yet is a build
starting. No cap on the build, as the owner asked: a failed build ends the launch, and
`m7_alive` fails on that at once with the launch's stderr; a hung build is named once a minute
with `ccy --build-lock` as the way to see and stop it. The run report records that a wait sat
through a build. Lock-based, so the in-place Claude Code update (also under the lock) is
covered and the launch's wording is not copied. Any ccy image build extends the wait (a launch
also waits for another's build of its image), which can delay a failure, never pass a check.

**Proof, red first.** `LaunchWaitTest` (`test_m7_check.py`, sourcing the include): against the
old include, `a live build restarts the wait` FAILED ("not within 2 s") and `both start waits allow for a build` FAILED. The library's tests in `scripts/test-planlib.bash` FAILED before the
library existed. Both now drive the launcher's own `ccy_image_build_locked` from
`files/var/local/claude-yolo/lib/image-build-lock.bash` (round 1 finding 3), so a change to the
product's lock or record breaks them; all pass.

**Detector:** see the sweep below.

## 3. Plan 00214: a documented non-zero result treated as failure

**Class: a checker treating a documented non-zero result code as failure.** `login_report`
exits `EXIT_FINDINGS` (3) whenever it has findings, and a stale play is one; the leg's
precondition is a stale play, so `|| fail` made it unpassable.

**Fix.** The status is captured (`|| status=$?`, safe under `set -e`); 0 and
`login_report.EXIT_FINDINGS` (read from this checkout's helper, never a copy) pass; any other
status fails; 0 with a play to run fails (a stale play must be a finding).

**Proof, red first** (`CLAUDE/Plan/00214-update-all-changed-plays/test_check_changed_plays.py`,
the real checker over a stand-in checkout): against the old checker three of four tests FAILED
(3 with a play to run: "the collection exited 3", the host's error; 3 with none; 0 with a play
to run accepted). All pass after the fix.

**Detector:** see the sweep below.

## Sweep

Independent search (no rule text given to it):
`261022-checker-classes-independent-search.md`. Active plan folders, bash and Python checkers.

**Invite without join.** No other instance. Plan 00201 M7 adds members only to name them as
`say --to` targets (their tokens are deleted); Plan 00161 `place_member` and `u23_join` join
by a first `recv` before acting.

**Fixed wait that ignores an image build.** Three more, all fixed on the shared library:
Plan 00161 `_acceptance-u20.inc.bash` (four start waits, now `u20_poll_launch`), Plan 00194
`_acceptance-goodbye.inc.bash` (`gb_poll_launch`), Plan 00203 `_acceptance-m2.inc.bash`
`m2_container_up` (a 120-iteration loop, now a deadline restarted by `plan_image_build_grace`;
M1, M2, M3 and M4 wait through it). Each acceptance.bash sources the library. Plan 00169 has a
1800 s cap sized for a rebuild; ruled out. The 00194 and 00211 `--fake` runs still pass.

**Documented non-zero result read wrongly.** The reverse form in Plan 00109 `triage.bash`:
every non-zero `login_report` exit was labelled "findings", so a crash read as findings. It now
reads `EXIT_FINDINGS` from the helper and says when the report itself failed. Scratch proof
with a stand-in `login_report` exiting 1: the old section printed "the findings above", the new
one "the report itself FAILED". Not fixed, recorded: Plan 00201 `m7_status_ready` and Plan
00161 `u20_status_field` read any `pingbus status` failure as "not yet", so a real error is
reported as a timeout; the run still fails, only the diagnosis is late.

**Detectors: none written.** Invite without join: see section 1. Image build waits: a rule
that every plan launching ccy calls `plan_image_build_grace` would flag plans that launch ccy
with no start wait or with a wait sized for a build (00063, 00073, 00080, 00092, 00098, 00169
and 00196 name the launcher), and cannot tell a container wait from any other loop; it would be
noise, and the shared library is the defence for new checkers. Documented non-zero codes:
which codes are results is per tool and lives in its source, so a rule that knew them would
copy every contract, and one that flagged every `|| fail` would flag every correct use.

## Review

Round 1 (`261022-checker-fixes-review-round1.md`): FIX-FIRST. Done: 1 (kept uncapped as the
owner asked, the message names `ccy --build-lock`; scoping to the launch's own build was
rejected because a launch legitimately waits for another build of its image), 2 (the locks
judged as `ccy --build-lock` judges them, held first), 3 (the tests drive the real
`ccy_image_build_locked`), 4 (`a3_join` reads `EXIT_OK` and `EXIT_NOTHING` from
`helpers/pingbus/cli.py`), 6 (the report records a wait that sat through a build), 8 (the
00201 description says any build counts, and how to stop a hung one). Left: 5 (a recv whose
sync is skipped for a busy lock still says joined), as the reviewer notes it cannot pass a
check (the report then fails), and the state directory is A3's own.

Round 2 (`261022-checker-fixes-review-round2.md`): MERGE. Its one should-fix is done: U20's
launch waits keep sudo fresh on each poll (`u20_refresh_sudo`), as M7's do, since the launch
takes its seat with `sudo -n` after a build of any length. Its notes (the three sweep callers
have no own test, read correct; `ps` failing reads as a stale record, at worst the old timeout;
one global spacing for the waiting line) are accepted as they stand.
