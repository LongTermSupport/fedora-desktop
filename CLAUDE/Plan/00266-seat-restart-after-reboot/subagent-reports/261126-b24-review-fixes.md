# Batch 24 review fixes (Plan 00266 C.2, R72/R77 history)

The review: `untracked/agent-reports/auto/261126-162548-qa-reviewer-af166d9d84c27824a.md`.

## Fixes

1. **One refused container no longer keeps every seat down.** New verb `teamchat-admin lxc boot`:
   `lxc.StartRecorded`, then `lxc.Reconcile` whatever the start found. Every line of both goes to
   stdout, and the verb exits 1 naming both refusal counts, so the unit still ends failed. The boot
   unit has the one `ExecStart=... lxc boot` (`lxc reconcile` alone with the container start off).
   Test: `admin.TestLXCBootReconcilesAfterARefusedContainer` (a refused box-1 and a reconciled
   box-2 seat in one run); the unit goldens and `TestTheBootUnit` pin the single ExecStart.
2. **Seats running before the deploy are seeded.** New verb `lxc record-started`
   (`lxc.RecordStarted`): each team-record seat, not left, whose container runs and whose session
   holds its seat lock is added to `_lxc-started.json` (`ADDED`, or `KEPT`). It starts and removes
   nothing. A read it cannot make fails the run with the record unchanged. The play runs it after
   the present phase while an LXC member is present (`changed_when` on `ADDED`). Tests:
   `TestRecordStartedRecordsTheLiveSeats`, `TestRecordStartedFaults`, `TestLXCRecordStarted`,
   `TestThePlayRecordsTheSeatsAlreadyRunning`.
3. **A user's own HISTFILE is kept.** `__history_shared_file` is defined only inside
   history-search.bash's redirect branch, so `__history_append` restores the shared file only
   where the include redirected it. Test: `scripts/test-bash-history-session.bash`, new case
   (HISTFILE=/dev/null set before the include): red 2, then green 11/11.
4. and 5. **PLAN.md matches the code**: the status line, the sign-in PATH line, who writes the env
   file (host root, into `/run/teamchat-session-<key>.env`; the guest op runs as the seat user),
   and C.2's owner (`play-teamchat.yml`). It now also covers `lxc boot`, the seeding and the
   template-unit check.
5. **`lxc@.service` is asserted.** The play stats `/usr/lib/systemd/system/lxc@.service` and
   asserts it exists before the units are written, while an LXC member is present and the
   container start is on. Test: `TestThePlayChecksLXCsTemplateUnitBeforeTheUnits`.

Nits: the `lxc restart` doc now says it leaves the started record as it is. I left the hardcoded
drop-in directory alone, because `TestThePlayMakesTheLXCDropInDirectoryFirst` already pins it to
`paths.LXCContainerDropInFile`.

## Defence Before Fix

None of the four defects is a pattern a detector can express. Each is semantic:

- a second step that must run whatever the first did;
- a record with no seed;
- a function whose being defined changes behaviour elsewhere;
- a unit from another package that is assumed to exist.

Each is fixed conventionally. A regression test now holds each one.

## Dogfood

The edit hook's `ansible-playbook --syntax-check` of `play-teamchat.yml` fails in the container
because the vault password file is absent. That failure says nothing about the edit. Reported to
the lead to log.

## Second review

The review: `untracked/agent-reports/261126-qa-review-b24b-review-fixes.md`.

1. **`lxc record-started` no longer records a seat no boot can start.** `sessionLive` now also
   reads the seat's session unit file through the pinned root (the same file `runSeatJob`
   refuses without). A live seat with no unit (`service: false`, run by hand) is returned as
   `SKIPPED\t<container>/<key>\t<why>` and is not added. The admin verb prints those lines on
   stderr, so the play's `changed_when` on `^ADDED\t` in stdout is unchanged.
   Test: `TestRecordStartedSkipsALiveSeatWithNoUnit`, red before the fix (`ADDED` and recorded).
2. **Reconcile keeps reporting, not dropping, a recorded seat with no unit.** A missing unit can
   also be an apply that has not run yet, so dropping it would lose a seat silently. It stays a
   `REFUSED` line, naming the cause, and fails the boot unit. `lxc stop --member` forgets it (the
   record is cleared before the refused unit stop). With fix 1, only a seat recorded before it,
   or one whose unit was later removed, can reach this.
3. **`sessionLive` takes the verb.** record-started's errors are `lxc record-started`'s.
   Test: `TestSessionLiveErrorsNameTheirVerb` (pin fault), red before the fix (`lxc reconcile`).
4. **`lxc start-recorded` is deleted** (verb, table entries, its admin tests; the remaining test
   now drives `lxc boot`). `StartRecorded` stays as `lxc boot`'s container start, and its
   errors now say `lxc boot` (the same wrong-verb class as 3). Comments that named the verb
   (`boot.go`, `lxc.go`, `units.go`, `wire/lxc.go`) and PLAN.md now say `lxc boot`.
   No doc or play called the verb. Left stale: the comment on `LXCRunningFile` in
   `paths/paths.go` still names `lxc start-recorded`. That file is a ccy image source, so
   editing even a comment needs a CCY_VERSION bump in the launcher, and other agents have
   uncommitted hunks in the launcher. Fix it with the next commit that bumps the version.
5. **Docs:** `docs/agent-bus-lxc-members.md` says a `service: false` seat is not added.

Not done: the `lxc@.service` path literal nit (play and `play_test.go`), not in this brief.
