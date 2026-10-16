# M1+M2 pre-merge review (Fable): the seat keeper, the membership reader, the launcher

Branch `feat/hot-control-m1-pingbus` at `6b54c3e8`, diffed against `origin/F44` (`3cd8601e`).
Read-only; no `qa-all`. Spec: [SPEC-hot-control.md](../SPEC-hot-control.md) revision 2. Prior
reviews: `261016-m2-review-fable.md`, `261016-hot-control-spec-review-fable.md`. Developer
notes: `261016-m1-pingbus-opus.md`, `261016-m2-launcher-opus.md`.

What was run, on a `git archive` of the branch tip under `untracked/scratch/` (never the
working tree, which is at F44): the pingbus unit tests named below (465, green), seven
mutations against them (section 9), `scripts/test-ccy-agent-bus.bash` (78/0),
`scripts/test-cc-teams.bash` (69/0), `scripts/test-ccy-teams.bash` (185/0), and
`go test ./internal/teams/... ./internal/registry/...` (18 packages ok).

## Verdict: MERGE

No blocking finding. The four M2 fixes landed as asked (section 1). The keeper is the sole lock
holder and the forgery, lifetime and compatibility claims hold under the probes below. The
non-blocking findings are in section 10; none changes behaviour the owner's acceptance would
see, and each can follow in M3.

## 1. The four M2 fixes

1. **Zero-seat session: no inbound, no socket, first join relaunches once.**
   `seat.session_settings(inner, seated=...)` writes `crossSessionInbound: accept` only when
   `seated`; `cmd_seat_exec` passes `seated=bool(seats)` from the keeper's `applied.json`.
   Tests: `test_a_session_launched_on_no_team_accepts_no_inbound`,
   `test_a_seat_the_keeper_takes_later_never_turns_inbound_on` (a real keeper takes a seat
   added at generation 2; the settings file is byte-identical to the launch's). Both fail when
   the `if seated` is removed (mutation M-E). `m2-check.bash plain` asserts no
   `crossSessionInbound` and no `*.sock` under `cc-socks`. The relaunch itself is M3's; the
   spec (H22, H34), PLAN.md Decision 1, docs/ccy.md and the changelog all say so consistently.
2. **Versions.** F44 is CCY 3.98.13 / container 2.68 / plugin 1.2.3. The branch is CCY 3.99.3,
   `REQUIRED_CONTAINER_VERSION="2.70"`, Dockerfile label `2.70`, plugin 1.3.0. The changelog
   carries 3.99.2 (2.69, the pre-F44-merge state) and 3.99.3 (2.70); ACCEPTANCE.md names 2.70.
   Unique and above F44.
3. **Plan 00201 and 00161 acceptances ported.** `m7_seat_listing` and `u20_membership_listing`
   read each ccy container's `/run/ccy/membership` mount source and the host's
   `membership.json` through `jq`, replacing the `ccy-seats` label in both discovery sites
   (`m7_remove_containers`, `m7_container_up seated`, `u20_seat_label_listing`,
   `u20_container_up`). `m7_check.py expect-plain` admits exactly `PINGBUS_MEMBERSHIP=off`
   and refuses `/run/ccy/membership` (test added). Both are queued in `meta-deploy.list` as
   `<plan>/acceptance.bash`, after the 00203 run.
4. **The dispatcher.** `acceptance.bash` is one dispatcher (bootstrap, flags, run log,
   `REPORT`, `check_leg`) sourcing `_acceptance-m2.inc.bash`, `_acceptance-m1.inc.bash` and
   `_acceptance-robust.inc.bash` in that order, each with its `acceptance_<m>` runner. All
   three inc files exist on the tip; `acceptance.bash` and `m2-check.bash` are mode 100755.

## 2. Keeper as sole lock holder

- `inbox.acquire_lock_at` opens every lock `O_CLOEXEC`; the only caller of
  `Lock.set_inheritable` left in pingbus is the watch supervisor's own watch lock
  (`cli._supervise`), never a seat or keeper lock. The keeper (`seat.Keeper`) never execs or
  forks in M1, so nothing can inherit its descriptors.
- `seat exec` spawns the keeper with `close_fds=True, start_new_session=True`, waits for
  `READY`, then `execvpe`s into the command holding **no** seat lock (`RealExecTest`
  reads `/proc/self/fd` from inside the exec'd session: zero `seat.lock` entries).
- **Audit F5:** `test_a_detached_process_does_not_keep_the_seat` starts a `setsid` child with
  `close_fds=False` from the session and sees the lock free the moment the session ends.
  Correct, because the session had no lock fd to leak.
- **`LOCK_UN` on release:** `Lock.release` is truncate, `flock(LOCK_UN)`, close.
  `test_release_unlocks_even_when_the_descriptor_was_duplicated` fails without the `LOCK_UN`
  (mutation M-A).
- One way claude's own processes can still affect a seat lock: by killing the keeper
  (`pkill -f 'seat keep'` from a Bash tool). The locks are then free while `applied.json`
  still says held, so a sibling session can take the seat while this one's `pingbus send`
  still works from the readable bundle. This is inside D6 ("control, not a boundary within a
  checkout") and H10 makes it visible (hooks say `keeper_stopped`; ccy-teams `unguarded` is
  M5). Not blocking; recorded in section 10.

## 3. Keeper lifetime

- `cmd_seat_keep --parent <pid>` calls `prctl(PR_SET_PDEATHSIG, SIGTERM)` first, then checks
  `getppid() == parent` (closes the race where the parent died before the prctl), and
  `seat.keep` polls `getppid()` every 0.5 s as the backstop and `release_all`s in `finally`.
  `test_the_keeper_ends_when_its_parent_changes_and_releases_everything` covers the poll.
- `seat exec` execs in place, so the keeper's parent PID (and the forking main thread, which
  is what PDEATHSIG is tied to) survives into claude. In a ccy container that PID is the
  container's init chain; the keeper ends with the container. On the host (`cc --teams`) it
  ends when claude does. SIGTERM's default action frees the locks by process exit; the links,
  the `active` flag and `applied.json` are left as the spec says (H10 "visible and repairable").
- No restart in M1: a dead keeper stays dead until Repair (`seat keep` with no `--launch`,
  M5), which adopts the home, re-takes the desired seats and clears stray links
  (`test_a_repair_keeper_retakes_every_seat_in_the_existing_home`). `KeeperBusy` (75) refuses
  a second keeper on one home.
- **Gap, not blocking:** removing the `rt.die_with_parent()` call makes no test fail (mutation
  M-D): the getppid backstop carries `RealExecTest`. The fake Runtime in `ClaimTest` pins the
  keeper argv but not that `die_with_parent` was called. One assertion on the fake would close
  it.

## 4. Forgery

- `config.active_teams` with `PINGBUS_MEMBERSHIP` set: `membership.active(read_desired(mount), read_applied(home))`, i.e. held seats whose exact `(team, seat)` the **desired** file names,
  in desired order. The desired file is read from the marker's directory on every call, never
  cached, never from the home. `PINGBUS_TEAMS` is ignored when the marker is set
  (`test_membership_wins_over_pingbus_teams`).
- Mutation M-B (return every held seat, ignoring desired) fails six tests, including
  `ForgedAppliedCliTest.test_send_to_the_forged_team_is_refused_64` (a forged `applied.json`
  naming an extra team; `send --team` exits 64 "not a member", nothing posted, only the
  desired team loaded).
- A forged applied entry naming a desired `(team, seat)` the keeper has not locked would make
  the team "active" for that session: that is within the host's own intent for the session,
  so not an escalation. A writable membership path cannot make a seat active in ccy: the mount
  is `:ro` and `mount_dir` refuses `off` and relative paths; on the host (cc, M6) the spec
  accepts the weaker authority (H31). The agent can of course run `pingbus` with its own
  `PINGBUS_MEMBERSHIP`/`PINGBUS_HOME` pointing at forged files and reach any seat's bundle in
  the checkout; that is D6, stated, and the hooks run with Claude Code's environment, which
  the agent cannot change mid-session.
- The keeper's report (3.3) goes to `<seats root>/../sessions/<session>/applied.json` where
  `<session>` comes from the host-written desired file under `_SESSION_RE` (no `/`, no
  leading `.`), so the session cannot steer the path.

## 5. The merged reader (`helpers/pingbus/membership.py`)

- Strictness: `_object` refuses missing and unknown keys; `v` must be `int` and `== 1` (bools
  refused by `type() is int`); `generation >= 1`, `written_ms >= 0`; one seat per team; seats
  and farewells under `protocol.is_team_name`/`is_seat_name`; `farewell.reason` from the
  closed set `{removed}`; `kind` in `(ccy, cc)`; file read `O_NOFOLLOW`, regular file only,
  64 KiB cap, duplicate JSON keys refused. `parse_applied` admits `told` as the one optional
  key and `generation` 0 for a `PINGBUS_SEATS` launch.
- Session name: `_SESSION_RE = [A-Za-z0-9_][A-Za-z0-9_.@+-]{0,254}` is character-for-character
  the Go writer's `sessionName`. `.` and `..` cannot match (leading dot refused).
  `test_every_session_name_the_host_writes_is_read` pins the Go/Python agreement by reading
  `membership.go`. The M1 developer notes quote a narrower grammar
  (`[A-Za-z0-9][A-Za-z0-9._-]{0,127}`); the code and the Go side agree with each other, so the
  note is the stale one.
- Checkout: `_CHECKOUT_RE` is an absolute path with no control characters; `Desired.identity`
  returns `(checkout, team, seat)` (`test_the_checkout_is_part_of_a_seat_identity`). Nothing in
  the container consumes it yet, which is right: allocation authority keyed on the checkout is
  the host's (H21, M4).
- `active()` returns `()` when `applied` is None; `config` turns an empty result into a 78
  with "no team is active: ... or its seat keeper has not taken its seats yet".

## 6. Compatibility

- `seat exec` with an empty membership: `desired_source` picks `MembershipFile`; the keeper
  makes the home, holds nothing, writes `applied.json` with no seats and no flag; `seat exec`
  writes `settings.json` (no inbound) and `statusline-inner`; no `PINGBUS_TEAMS`, no forge
  check (`test_an_empty_membership_makes_the_home_holds_nothing_and_asks_no_gh` with a
  logged-out fake `gh`). The entrypoint replay in `test-ccy-agent-bus.bash` confirms the same
  through the real zipapp.
- `cc --teams` still runs `env PINGBUS_SEATS=... pingbus seat exec` (`cc-teams.bash:141`);
  `LaunchSeats` is generation 0, no report, `PINGBUS_TEAMS` set for the child, `PINGBUS_SEATS`
  beside `PINGBUS_MEMBERSHIP` refused 78. `test-cc-teams.bash` 69/0.
- Today's `ccy --teams` end to end: the launcher's `ccy_seats_take` now calls
  `ccy_membership_args` (label, membership-init, mount, marker) before `agent-bus seat take`;
  the entrypoint's AGENT-BUS block links pingbus and passes
  `--settings /tmp/pingbus-home/settings.json`; the real-zipapp replay with a two-seat desired
  file shows the flag up, `crossSessionInbound: accept`, `PINGBUS_TEAMS=team-a,team-b` and the
  project's status line kept as the inner one. Every other `container_cmd run` in the launcher
  and `token-management.bash` uses `--entrypoint`, so none meets the new "marker unset"
  refusal. The remaining host-side proof (seat `held` in `agent-bus seat list`, the keeper as
  the only `seat.lock` holder) is ACCEPTANCE.md M1 (b), the owner's step, plus the ported
  00201/00161 runs.

## 7. `pingbus seat retire` and `pingbus reach`

- `seat retire [--root] SEAT@TEAM`: one `SeatRef` only (a bare team or a list is 64),
  `check_seat_dir` (78 with the seat named), then it **takes the seat lock** itself (75 if
  held; `test_a_held_seat_is_refused_75_and_kept`), takes the `recv` sync lock, unlinks
  `sync.json` with a directory fsync, and publishes `listening` with `until = now`. Homeserver
  down: the position is still cleared and the exit is the Matrix one (tested). Mutation M-F
  (no seat lock taken) fails the held-seat test, so the lock is load-bearing in the tests.
  Authority: it is a host command in the spec, but the pingbus binary is on the container's
  PATH, so an agent can retire any **free** sibling seat in its checkout (clearing its sync
  position and expiring its status). That is a nuisance inside D6's boundary, not a privilege
  gain; a held seat is refused. Noted in section 10.
- `reach --team T [--root]`: `--team` required and grammar-checked (64), the team's first seat
  under the root (78 if none), `GET /_matrix/client/versions` with `auth=False` and a 3 s
  limit; the base URL is named on both outcomes. `test_no_token_is_sent` is real: the fake
  server's `http_log` exists and records headers.

## 8. The `told` race

`Keeper._told()` re-reads `applied.json` and carries `told` over on every write; nothing in
M1 writes `told`, so the two-writer race the M1 notes mention cannot occur yet. It becomes real
in M3 when the watcher or the hook records what it told. The notes' recommendation (give
`told` its own file, or route every write through the keeper) is the right one for M3; the
M3 spec text (H13 "either path records what it told in `applied.json`") should be amended
then, not now.

## 9. Tests, with mutation probes

Baseline on the branch tip: `test_membership`, `test_seat`, `test_seat_retire`, `test_inbox`,
`test_hooks`, `test_config`, `test_plugin_contract`: 465 tests, OK.

| Mutation                                            | Tests that fail                                                                                                      |
| --------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| M-A `Lock.release` without `LOCK_UN`                | `test_release_unlocks_even_when_the_descriptor_was_duplicated`                                                       |
| M-B `membership.active` ignores the desired file    | 6, including `test_send_to_the_forged_team_is_refused_64`, `test_a_seat_applied_but_not_desired_is_not_active`       |
| M-C keeper never drops a removed seat               | `test_a_removed_seat_is_unlinked_and_unlocked`, `test_a_seat_swapped_within_a_team_moves_the_link`                   |
| M-D keeper does not call `die_with_parent`          | **none** (the getppid backstop carries the real-exec test)                                                           |
| M-E a session on no team gets `crossSessionInbound` | `test_a_session_launched_on_no_team_accepts_no_inbound`, `test_a_seat_the_keeper_takes_later_never_turns_inbound_on` |
| M-F `seat retire` without the seat lock             | `test_a_held_seat_is_refused_75_and_kept`                                                                            |
| M-G hooks never say `keeper_stopped`                | `test_a_free_keeper_lock_is_said`                                                                                    |

Every new behaviour but PDEATHSIG has a test that fails without it.

## 10. Acceptance

`acceptance.bash` runs unattended after one sudo prompt (`plan_prime_sudo`) and touches only
throwaway state: M2 starts `m2-plain-<pid>` and `m2-nobus-<pid>` with
`tmux -L ccy new-session -d` and `ccy --headless --no-restore --no-supervise`, stdin a fifo it
holds open; M1 adds `m1-plain-<pid>` through the same launch and appends it to `M2_NAMES`, so
M2's registered cleanup (tmux, containers by `ccy-session` label, registry records, membership
directories, transcripts) covers it on any exit; the robust leg builds and removes
`acc-robust`. The owner's sessions are never selected. Preconditions are stated: Enforcing
host, ccy launched interactively once in the checkout. The seated half of M1 (b) is the
owner's step in ACCEPTANCE.md, as it needs a team.

One fragility, not blocking: `m1_holders` counts any descriptor whose name matches
`[a-z]*.lock` as a lock; a process in the container holding some unrelated lowercase `*.lock`
would be counted as "other holder" and fail (b). The table is written to the run directory
for diagnosis, so a false failure is explainable, not silent.

## 11. Hygiene and merge-tree

- `git merge-tree --write-tree origin/F44 origin/feat/hot-control-m1-pingbus`: clean, no
  conflicts (the tip already merges `3cd8601e`).
- Public-repo scan of every added line: only placeholder paths (`/home/u/proj`,
  `ccy-proj-1`), `example`-class names and spec placeholders. The journal entries name no
  host, container, project or person.
- Bash limits: the launcher and the entrypoint both shrank (`2825/2747` to `2823/2745`;
  entrypoint statements `448` to `444`); `agent-bus-membership.bash` is a new 2-function
  library under the limits.
- Docs: `docs/ccy.md`, `docs/agent-bus-protocol.md` §12-§13, `docs/ccy-changelog.md`,
  `--help` and the Dockerfile comment all describe the keeper and the membership consistently
  with the spec, including the "differ by the mount, the plugin, the status line wrapper and
  the keeper" correction from the M2 review.

## 12. Non-blocking findings, for M3 or a follow-up

1. Pin `die_with_parent` in `ClaimTest` through the fake Runtime (mutation M-D is green
   today).
2. `check_distinct` (two seats sharing a token or account) runs only on a strict launch; a seat
   the keeper takes on a later poll is not checked against the others. Add it to the
   non-strict path when M3 makes hot adds real.
3. `seat retire` is runnable inside the container against a free sibling seat. Either refuse it
   when `PINGBUS_MEMBERSHIP` is set (it is a host command), or state the D6 consequence in the
   protocol doc.
4. A killed keeper leaves `applied.json` saying `held` while the locks are free; the hooks say
   so, but `pingbus status`/`send` do not. M3's watcher tick (which re-reads `applied.json`)
   or the `_keeper_lines` check could be folded into `survey` so every command sees it.
5. The M1 notes' session-name grammar does not match the code; the code matches Go. Fix the
   note when it is next touched.
6. `m1_holders`: match the two lock names (`seat.lock`, `keeper.lock`) exactly rather than
   `[a-z]*.lock`.
7. `ccy_no_bus_check "$NO_BUS_MODE" "${TEAMS_GIVEN%false}"` relies on `TEAMS_GIVEN` being the
   literal `true`/`false`; a comment or a plain `[ "$TEAMS_GIVEN" = true ]` would make the
   intent legible.
