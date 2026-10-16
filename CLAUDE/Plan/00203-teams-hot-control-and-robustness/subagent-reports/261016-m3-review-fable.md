# M3 pre-merge review (Fable): hot add and remove

Branch `feat/hot-control-m3` at 0cfeb28e, diffed against `origin/F44` (bbbd0604). Read-only;
no qa-all. Read: SPEC-hot-control.md rev. 2 (H11-H16, sections 3, 5, 7), the Opus build notes
with their deviations, the open items of the M1+M2 review section 12.

**Verdict: MERGE.** No blocking finding. Six findings to carry into the M3 follow-up or M4,
one of them (B1) before the owner relies on hot add in daily use.

## 1. Authority

- The desired file has one writer, `internal/teams/membership` (`Write` checks the schema,
  `writeDesired` is temp-file-then-rename in the private directory). `hot` never reads anything
  from the bus into it: the seat list comes from `agent-bus seat list`, the session from the
  `ccy-session` label, the report (3.3) only to explain the lock.
- The `MEMBERSHIP` notice is built by `envelope.render_membership` from pingbus's own templates.
  Every fill is grammar-checked (`NAME_FIELDS`: team, seat, handle, description, a closed `why`),
  every line is refused on a control character, U+2028/9, `<<` or `>>`, and the entry is closed by
  pingbus's own marker. A Matrix body cannot imitate it: bodies in `render` are JSON-escaped
  strings inside one `body:` line, so a body saying `kind: MEMBERSHIP` is quoted text. `KINDS`
  gains `MEMBERSHIP` but nothing maps a bus item onto that kind: `AGENT_KINDS` is unchanged and
  the kind of a bus entry is still derived from the team record. The session-start frame line
  tells the model that `MEMBERSHIP` is pingbus's own.
- The container-private inputs (`applied.json`, `told.json`) can be forged by the model, but they
  only change what the session is told about itself; `active_teams` stays applied ∩ desired (H4).
- Small gap (N1): `membership_notice` filters a team description for `<<` only, while
  `render_membership` refuses `>>` too, and `protocol.is_description` allows both. A description
  containing `>>` makes `_tell` raise `ValueError` on every 2 s tick ("could not be built", one
  `watch.log` line each) until the next prompt's hook tells it (the hook path does not render).
  Filter `>>` in `membership_notice`, or drop the about line rather than the notice. The mutation
  that removes the `>>` check from `render_membership` survives the tests.

## 2. The watcher's runtime team start and stop

- Exactly-once across stop and restart holds as the spec accepts it: a removed team's thread gets
  its own `stop` event, the main loop drops the team from `seats` the same tick, so nothing of it
  is written to the socket or consumed; items already in the checkout's `inbox/` stay and are
  delivered on re-add (H23a's accepted window). `waiting.failures` for a removed team are skipped.
- Re-baseline on removal: `remove` waits for the seat lock to read free, then `pingbus seat retire` waits for the sync lock (`_sync_lock_waited`, 40 s) before `forget_sync_token`, so the
  token is cleared **after** the removed thread's last batch stored its token. The acceptance (e)
  posts after `remove` returned, so it proves the common case.
- **The 30 s lock release (deviation 1) is wider than the notes say** (N2). Between the desired
  write and the end of the long poll in flight, the removed thread still syncs and **stores** into
  the seat's `inbox/`; the main loop no longer delivers that team. Those items (posted up to 30 s
  after the owner's remove) are delivered on a later re-add as fresh `HUMAN`/`PEER` deliveries.
  H23a's "nothing posted while it was out is delivered" is therefore true from the end of the long
  poll, not from the remove. Acceptable for M3, but record it in the spec's H23a, or have the
  thread drop what it stored after its stop was set.
- **A fast re-add colliding with the lock: not through `remove`, which waits**, but there is a
  real hole (B1): `_Following.follow` puts a team whose `start_team` returns `None` into
  `self.failed`, "not started again until it has left and come back". `start_team` returns None
  on `inbox.Busy`, which `_take_waiter_lock` raises at once for a `wait` holder (a `recv` holder is
  waited). So if the model has a `pingbus wait` running that took the new team's lock in the
  ≤2 s between the keeper applying the seat and the watcher's tick, or a `seat retire` of an
  earlier failed remove still holds it, the added team is silently unwatched for as long as it
  stays desired: the watcher is alive and holds the watch lock, so the hooks' revive does not
  fire either. This contradicts H11 ("a team that fails for a transient reason is retried ...
  never dropped for good"). Treat `Busy` as retry-next-tick (do not add to `failed`), keep
  `failed` for `ConfigError` only. Not exercised by the acceptance; no test covers `Busy` on a
  hot add.
- Minor: `_tell` runs `hooks.tell_membership` every tick, which takes the told lock, reads both
  files and `load_bundle`s every active team, with no stamp check. Cheap, but it is per-2 s I/O
  in every seated session; a stamp of `applied.json`/desired would gate it.

## 3. `ccy-teams session add` and `remove`

- Idempotence: `alreadyIn` prints every step `-` and exits 0; `remove` of a team the session is
  not in and owes nothing prints `-` and exits 0; a farewell still owed is finished by the same
  command (`!in && owed`). Rollback: `restore` rewrites record then desired with the old seats
  on a failed wait or a container change (H28), both tested (the mutation removing `restore` is
  killed by two tests). Exit codes follow H17: 64 usage and the coordinator refusal, 75 a held
  or named seat (`ErrBusy` from `seat take` too), 78 not running/no label/no record/no membership
  file/`--no-bus`. One reading: "not running" is 78, where 1 would also fit; fine.
- The first-join relaunch: `relaunch.json` is written into the session's own membership
  directory; the lifecycle plugin reads it through the container's own `PINGBUS_MEMBERSHIP`
  mount, so it cannot target another session **as long as the mount is the session's own**; see
  section 7 for the nested-pane bug class. No loop: `membership-init` removes the file at every
  launch, and a launcher that refuses the relaunch leaves a dead session, not a loop.
- The wait for the relaunch requires a new container ID plus a report with `inbound: true` and
  the seat held, so a stale report from the old keeper cannot satisfy it.
- Minor (N3): after `✗ session: not relaunched within 180 s` (exit 1), a re-run takes the
  `alreadyIn` path and says "Nothing to do" while `relaunch.json` is still pending; and if the
  owner then removes the team before the idle moment, the relaunch still happens for nothing.
  Consider removing `relaunch.json` in `remove` when the session's desired file goes back to no
  seats, and having `alreadyIn` say a relaunch is pending.
- Minor: `ccy_handle_restart_exit` pipes the argv through `ccy-teams internal restart-args`
  without `pipefail`; a failed builder that prints nothing is still caught by the empty-array
  check, so no silent wrong relaunch.

## 4. The shared session discovery

- `discover.Index` is populated from records (`AddRecord`, launcher prefixes only, as before) and
  from live sessions' desired files (`AddDesired`, only when `Live` has the session). The board
  keys `SeatSession`/`SeatRestore` from it; `shutdown.seatsOf` now asks `Seats.SeatsOf(name)`.
- No regression in `ccy-teams shutdown`: the old loop over `SeatSession` and the new `SeatsOf`
  agree for well-formed `seat@team` items; malformed items (no `@`) are now skipped rather than
  keyed, which is correct. A hot-added seat now appears in a team's shutdown plan, as intended.
- Known limit, M4's (N4): a record of a session that is not running but has `Restore` wins over a
  live desired file for the same `(checkout, team, seat)` (`AddDesired` keeps the record's
  session), and `pickSeat`'s `taken` consults only live desired files and the lock, not dead
  restorable records. A seat can be hot-given to session B while a stopped session A's record
  will replay it at the next restore; A's keeper then refuses it as held. H21 scopes this to M4.

## 5. Mid-turn (H15, H16) and the status line (H14)

- H15: `config.removed_reason` gives "removed by the owner" from the farewell entry while it
  exists and from `told.left` after; `load_active` under the membership turns "no team active"
  into "not a member of `<team>`" (64) rather than a whole-session failure. `cmd_say` resolves the
  member before it checks `--to`, so the acceptance's `pingbus --team acc-m3b say` sees the 64
  with the reason.
- H16: the SessionEnd goodbye uses active teams (applied ∩ desired), which excludes a team from
  the moment the desired file drops it; the host's `seat farewell` posts once and the entry is
  then dropped. Only a failed "farewell done" write after a successful post can repeat it; that is
  H19's re-run and acceptable.
- H14: `statusline.membership_teams` reads desired ∩ applied (held) each render, no module import,
  pinned equal to `config.active_teams` by `test_the_copy_agrees_with_config_active_teams`.
  `[]` gives no segment; an unreadable file gives the red line.

## 6. Not done in Go: the H20 locks

Two concurrent `session add` runs on one session are unsafe: both read generation N, both write
N+1 with their own seat set; the first's `waitHeld` then sees N+1 without its seat, times out and
`restore`s to the **original** seats at N+2, wiping the second add, which may already have
reported Done. The registry record is read-modify-written without a lock as well. The end state
is silently short of a team that one command said it added. Not blocking: this is an owner
racing himself, and M4 schedules H20; but the usage text and ACCEPTANCE M3 should say "one
session command at a time per session" until then (N5).

## 7. Acceptance

`_acceptance-m3.inc.bash` proves (a)-(f) on two throwaway teams (`acc-m3a`, `acc-m3b`, host
vars in the run directory) and one throwaway headless session on `acc-m3a`, every judgement from
a tool's output (stderr step marks, the transcript file, `agent-bus seat list`, the room's
`/messages`), unattended, with cleanup registered first and teams purged however it ends.
Details:

- (a) checks exit 0, no `^✗`, no relaunch line; (f) checks no `^✓ record|membership`. Good.
- (d) requires `^✓ re-baseline`, the left notice, the goodbye in the room, the seat free, and 64
  with the reason. Good.
- (e) posts after remove returned; see N2 for why that is the common case, not the 30 s window.
- It does not depend on the caller's `TMUX`: the session is made with `tmux -L ccy new-session -d -s <name>` and the container found by name through M2's `m2_container_up`. **But** the ccy
  inside that pane resolves its identity through `ccy_tmux_current_session`, the function the
  nested-pane bug class lives in. If `meta-deploy` is run from a shell inside a ccy pane, M2's
  and M3's throwaway sessions could resolve to the caller's identity; the fix on
  `fix/m2-acceptance-host` (pane-pid check in `lib/tmux-session.bash`) closes that for bash.
  M3 should merge after it, or be run from a plain terminal until it does.
- Same class in Go (N6): `tmuxccy.Tmux.CurrentSession` keeps the old semantics (TMUX socket
  only). After the fix branch merges, a ccy launched from a shell in another session's pane
  takes no identity in bash, but `ccy-teams internal restart-args` (and `record-seats`) would
  still answer with the **outer** session's record. Align Go with the fixed bash in the merge.
- M3 (g), the interactive first join, is correctly left to the owner: a headless session has no
  supervisor. One doc nit: after the relaunch, SessionStart baselines `told.json` silently, so
  the resumed session is told its teams by the session-start context, not by a "you joined"
  notice; ACCEPTANCE (g) says "says it joined", which the owner should read as that.

## 8. Merges

- `git merge-tree origin/F44 feat/hot-control-m3`: clean (exit 0). M3 was renumbered onto the
  timer-state fix already.
- `git merge-tree origin/fix/m2-acceptance-host feat/hot-control-m3`: **five conflicts**, all
  in plan, doc and version files: the day's JOURNAL, PLAN.md, meta-deploy.list,
  docs/ccy-changelog.md, and `claude-yolo` (the `CCY_VERSION` line: 3.99.4.1 against 3.99.6).
  No code conflicts; `_acceptance-m2.inc.bash` and `acceptance.bash` auto-merge. Whoever merges
  second bumps CCY past both (3.99.7; the container stays 2.73, the fix touches host libraries
  only), rewrites the version line, and merges the journal entries in time order and the
  meta-deploy comment into one. Both branches edit the same meta-deploy block, so re-read the
  merged entry for the acceptance sequence it describes.

## 9. Tests, by mutation

Baseline in a detached worktree: the touched pingbus suites and `ccy_lifecycle` tests pass
(480), the Go packages `hot`, `find`, `discover`, `membership`, `board`, `shutdown`,
`registry`, `agentbus`, `sessprobe` pass. Seven mutations:

| Mutation                                                      | Result                                                                                           |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `render_membership`: stop refusing `>>`                       | **survived** (N1)                                                                                |
| `_Following.follow`: do not set the removed team's stop event | killed: `test_a_team_removed_is_let_go_and_its_items_are_not_delivered`                          |
| `membership_notice`: drop the teams left from the told record | killed: `test_a_removal_is_told_with_its_reason`                                                 |
| `Keeper._apply`: never `_wake` the watcher                    | killed: `test_a_first_seat_starts_the_watcher_with_the_recorded_socket`                          |
| `hot.apply`: no `restore` after a failed wait                 | killed: `TestAddThatTheKeeperRefusesIsRolledBack`, `TestAddWhenTheSessionEndsMidwayIsRolledBack` |
| `find.ReplaceTeams`: keep the old `--teams` value             | killed: `TestReplaceTeams`                                                                       |
| `hot.apply`: always take the inbound path (never relaunch)    | killed                                                                                           |

## Open items from the M1+M2 review, section 12

Folded in: 6. Still open, as the notes say: 1, 2 (`check_distinct` on the non-strict path is
now live-relevant: a hot add is exactly the path it misses), 3 (`seat retire` is still runnable
inside the container; `seat farewell` now is too), 4, 5, 7.

## Findings, numbered

- **B1** (fix before daily use, with M3's follow-up): `_Following` treats `Busy` as permanent;
  a hot-added team can stay silently unwatched. Retry on the next tick instead.
- **N1**: filter `>>` in `membership_notice`'s about line; pin it with a test.
- **N2**: items stored by the removed thread's last long poll are delivered on re-add; record
  in H23a or drop them at stop.
- **N3**: a pending `relaunch.json` is invisible to the re-run and survives a remove.
- **N4**: dead restorable records versus live desired files in seat allocation: M4 (H21).
- **N5**: say "one session command at a time per session" until H20 lands.
- **N6**: `tmuxccy.CurrentSession` keeps the nested-pane semantics the bash fix removes; align
  in the merge with `fix/m2-acceptance-host`.
