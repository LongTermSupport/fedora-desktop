# M3 fixes review (Fable): B1, N1-N3, N5, N6 and the merge with F44

Branch `feat/hot-control-m3` at b32c64f0 (4c1e3e0d, the fixes; then the merge of F44 7fd72106).
Read-only; no qa-all. Read against the M3 review (`261016-m3-review-fable.md`), the code and its
tests; ran the changed packages' tests.

**Verdict: MERGE.** No blocking finding. Every fix does what the commit says; the merge keeps
F44's launcher identity fix intact; the queue is right. Three notes to carry, none before merge.

## 1. The fixes, one by one

- **B1 (busy lock is transient).** `_Following._retry_later` keeps one `_Backoff` per busy team
  and a due time; `follow` skips the team until it is due, retries `start_team`, pops the entry
  on success, and drops entries for teams no longer active. `failed` is now `ConfigError` only.
  Test `test_a_team_added_while_its_lock_is_busy_is_retried_until_watched` holds a `wait` lock on
  the added team through ten ticks, releases it, and sees the watch lock taken. Correct. The
  only path where a busy team is still not retried is a watcher that is left with **no** seat at
  all (every active team busy or removed): it exits 0 and `_supervise` exits on 0 ("on no team").
  That is covered: at start-up, no seat is `EXIT_BUSY` (75), which the supervisor retries; and
  in the loop it needs a remove and an add to land within one 1 s tick, which sequential
  commands (N5) cannot do, and a later add's keeper starts the watcher again anyway.
- **N2 (a removed team's late fetch).** `_TeamStop.remove()` marks the stop as the owner's;
  `_discarded` consumes `batch.accepted` when the batch returned after a remove, in both
  `_watch_poll` and `_long_poll`; a stop from the process ending keeps the batch. Tests drive both
  thread bodies with a fake syncer that sets the stop during the sync, and check consumed, lock
  released, main thread not woken. Correct. A batch that returned **before** the remove but was
  not yet delivered when the tick dropped the team stays in `inbox/` (posted before the remove):
  that is H23a's accepted window, now microseconds wide instead of 30 s. The spec text was
  updated to match.
- **N1 (`>>` in a description).** `hooks._fits_a_notice` refuses both markers; the about line
  is left out, the notice delivered. `render_membership`'s `>>` refusal is now pinned
  (`test_a_line_that_could_break_the_frame_is_refused`), and the hook path has
  `test_a_description_with_a_marker_end_leaves_out_its_line_not_the_notice`. Correct.
- **N3 (pending relaunch).** `alreadyIn` checks `membership.RelaunchPending(t.dir)` first and
  exits 1 with a line that names the remedy; `remove` computes `last` before `takeOut` and
  withdraws the request once the session's last team is out, before the farewell. Both tested
  (`TestAddWhoseRelaunchDoesNotComeSaysWhatToDo` re-run; `TestRemoveOfTheLastTeamWithdrawsAPendingRelaunch`;
  `TestRelaunchPendingAndWithdrawn`). Correct. One gap (note 2 below).
- **N5.** `usage.txt`, `hot.go`'s help, the protocol doc's notice paragraph and ACCEPTANCE say one
  session command at a time per session. Done.
- **N6.** `tmuxccy.CurrentSession` is gone (`current.go`, `current_test.go` deleted; no reference
  remains in `src-go`). `ccy-teams internal restart-args <session>` takes the session as its one
  argument; the launcher passes `"${CCY_SESSION_NAME:-}"`, which it set once from
  `ccy_tmux_current_session` (the pane-pid check). `""` and no record pass the argv unchanged
  (`TestRestartArgsPassesThroughForAnUnnamedLaunchOrNoRecord`); the record is the named session's
  even with an outer session's `TMUX` in the environment
  (`TestRestartArgsTakesTheTeamsFromTheNamedSessionsRecord`, the rig sets `TMUX`). The bash
  suite `test-ccy-restart-request.bash` checks the argv goes through `restart-args ccy-named`.
  Correct.

## 2. The merge with F44

- `git diff 7fd72106 b32c64f0 -- files/var/local/claude-yolo/lib/` is empty: `tmux-session.bash`
  (the pane-pid `ccy_tmux_current_session`) and `agent-bus-membership.bash` are F44's byte for
  byte.
- The launcher's diff against F44 is the version line, the `printf` fold, and the `restart-args`
  pipe: `record-seats "$CCY_SESSION_NAME"` and `ccy_registry_record_ssh_key "$CCY_SESSION_NAME"`
  are as F44 left them.
- In Go, the only `TMUX` reads are in tests, each proving it is ignored; `find/record.go` and
  `find/restart.go` take the session as an argument. `RecordSeats` writes the named session's
  record and leaves the outer one alone (`TestRecordSeatsPutsTheTeamsInTheNamedSessionsOwnRecord`).

## 3. Versions

- CCY `3.99.7` (F44 is at 3.99.4.2, the parent M3 at 3.99.6); `REQUIRED_CONTAINER_VERSION` and
  the Dockerfile label agree at `2.73`; F44 is at 2.71, and the journal shows 2.71 is the last
  version any host built. So every host rebuilds the image and gets the pingbus fixes; the
  changelog's 3.99.7 entry describes M3 and names the two F44 fixes it carries.
- Process note (1): commit 4c1e3e0d changed `helpers/pingbus/cli.py` and `hooks.py` (67 code
  lines) with the label unchanged at 2.73 (its parent's). The pre-commit gate rejects exactly that
  (`scripts/git-hooks/pre-commit`, "CCY container version bump required"), so the commit was made
  past the hook. The end state is harmless only because 2.73 was never built anywhere; the rule in
  `ContainerRules.md` is per commit. Not a reason to hold the merge; worth knowing it happened.

## 4. meta-deploy.list

Entries, in order: `play-claude-yolo.yml`, `play-agent-bus.yml`, the 00203 plan folder
(`acceptance.bash` with its M1, M2, M3 and robustness parts), 00201's `acceptance.bash`, 00161's
`acceptance.bash`. F44's queue had the same minus `play-agent-bus.yml`, which M3 needs (the host
pingbus's `seat retire` waiting for the sync lock and `seat farewell`). Each entry has its
description; the claude-yolo one covers both M3 and the identity fix and says to relaunch team
sessions. Exact.

## 5. Tests run

`go build ./...`, `go vet` and `go test` over `internal/teams/...`, `tmuxccy`, `registry`,
`agentbus`, `sessprobe`: 23 packages ok. `pytest tests/helpers/pingbus tests/helpers/ccy_lifecycle`:
1566 passed.

## 6. New defects looked for, and the notes

- **Note 2 (N3's owed-farewell path).** `remove.go:36` computes `last := in && len(seats) == 1`.
  If `takeOut` writes the desired file but the seat is still held after `Waits.Apply` (exit 1,
  farewell owed), the re-run comes in with `!in && owed`, so `last` is false and a pending
  `relaunch.json` is not withdrawn; the session later relaunches on no team for nothing. Fix:
  also withdraw when `!in && owed && len(t.desired.Seats) == 0`. Minor: it needs the keeper to
  miss the release window first.
- **Note 3 (the restart pipe's status).** `claude-yolo:3787-3789` reads `wait "$!"` as the
  builder's status, but `$!` is now a pipeline's and the launcher has no `pipefail`, so
  `ccy_restart_relaunch_args` failing would be lost behind `restart-args`'s 0, and with a record
  that names teams the output is non-empty, so the empty-array check no longer catches it. Today
  the builder cannot fail there: `ccy_restart_request_read` has already validated the session id
  against the same pattern and the registry library is always loaded. Fix when next touched:
  `< <(set -o pipefail; ccy_restart_relaunch_args ... | ccy-teams internal restart-args ...)`,
  which restores the comment's promise.
- Looked at and fine: `_discarded` consumes under the thread that owns the team (the main loop
  has dropped the team the same tick, so no concurrent use of its state); `withdrawRelaunch` runs
  before `farewell`, so a failed goodbye does not leave the request; the second add while a
  relaunch is pending overwrites `relaunch.json` with the new generation and waits again, which
  is right; `start_team` says "busy" on every retry (1 s to 30 s apart) in `watch.log`, noise
  only; the `_tell` per-tick I/O from the M3 review stands, unchanged.

## Findings, numbered

- **Note 1**: 4c1e3e0d bypassed the container version gate; harmless here because 2.73 was never
  built on a host.
- **Note 2**: `remove` on the owed-farewell re-run does not withdraw a pending relaunch when the
  session is left on no team (`remove.go:36`).
- **Note 3**: the restart-args pipe hides the first stage's status; unreachable today, add
  `pipefail` inside the process substitution when next touched (`claude-yolo:3787`).
- N4 stays deferred to M4, as agreed.
