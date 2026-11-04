# Review: fix/lxc-stopped-member-2 at 3fe83b95 (Task 2.6, stopped LXC members)

Reviewed against SPEC-lxc-stopped-members.md L1-L11 and the owner's requirement (the play must
not need LXC containers or sessions running). Nothing in the branch was changed. Base: 226a292d.

## Verdict: FIX-FIRST

The core is right: a stopped container is never entered, never fails, is skipped when joined and
deferred when not, a deferred member joins on the first run with it up (traced through the
tests and the code), a skipped member's seat stays claimed, a missing container and every
failure inside a running one still fail, and the record is written atomically and only on
change. Two things must be fixed before merge: the user docs still say a stopped container is
a failure, and a team's record is never removed once its last LXC entry goes, so the board and
`apply` keep telling the owner to start a container nobody declares any more.

## Checks run (branch checked out detached in this worktree)

| Check                                                                                          | Result                                     |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------ |
| `python3 -m unittest tests.helpers.agent_bus_lxc.test_members test_applied_record test_derive` | 236 OK                                     |
| `go test ./internal/teams/...`                                                                 | pass                                       |
| `golangci-lint run ./internal/teams/...`                                                       | 0 issues                                   |
| `scripts/test-ccy-teams-command.bash`                                                          | passed 383, failed 0                       |
| `ansible-playbook --syntax-check playbooks/imports/play-agent-bus.yml`                         | pass (syntax only; no play was run)        |
| `git merge-tree --write-tree de85f5f6 3fe83b95`                                                | 2 conflicts (finding 4)                    |
| grep of the diff for usernames, home paths, hostnames                                          | none; fixtures use `devbox`, `box2`, `box` |
| `failed_when` / `ignore_errors` added                                                          | none                                       |

## What was traced

- **Every container entry point.** `run_present` probes `_existing_state` per (container, user)
  group before `host.guest()`; a non-RUNNING state goes to `_hold_back` and `continue`, so
  `_home`, `_check_gh`, `_check_route`, `_check_service`, the kit sync, `_join`, `_observe`,
  `_ensure_service` never see it. The absent phase returns from `_absent_held_back` before
  `host.guest()`, and the unit cleanup loop now tests `== RUNNING` (a missing container was
  already handled by `_absent_one`). Seat derivation: a held-back member with a known handle is
  passed to `resolve_members` with host, seat and repo set, so it is not `needy`: `identify`
  (which reads the checkout through `/proc/<init pid>/root`, RUNNING only) and `bundle`
  (`guests[...]`, which has no entry for it) are never called for it. One without a handle is
  left out of `found`. No other task in play-agent-bus.yml and nothing in `helpers/agent_bus`
  (health included) enters a container.
- **State probe.** `_container_state`: `lxc-ls` failure raises (error), name absent returns
  None (missing, exit 78 via `_existing_state`), `lxc-info -sH` failure raises, otherwise the
  state word. STOPPED, FROZEN and any other non-RUNNING word defer. Correct.
- **Seats (L4).** Known handle: `with_handle` sets host, seat, repo, so `resolve_members` puts
  the seat in `claimed` and `_check_between` sees it. Unknown handle: `unheld_teams` turns on
  `unheld_ok` for that team only; `derive_seat`'s final loop still never returns a seat whose
  handle is active (`roster.get(...) is not False`), so an active seat is never handed out
  twice. Tests `test_a_skipped_members_seat_is_claimed` and
  `test_a_deferred_member_with_no_record_does_not_block_the_next_seat` pin both.
- **Record (L8).** `applied_record.write`: `.<name>.new` opened `O_EXCL|O_NOFOLLOW` 0640,
  fchown, `os.replace`; unchanged text, gid and mode return False, so a no-op run emits no
  `CHANGED`. Symlinked directory refused; missing applied directory refused with the installer
  named. Written only after the phase (`_write_records` is the last step of `run_phase`; a
  `MemberError` earlier leaves the record untouched, pinned by
  `test_a_failure_of_a_running_member_still_fails_while_another_is_stopped`). Read by Go
  `lxcrecord.Read` (ENOENT is "none", other errors and bad JSON and a wrong version are errors
  that fail `apply` and `lxc-add`, and are said on the board's team row) and by `parse_record`
  in Python (bad JSON or shape is exit 78). `"handle": null` unmarshals to "" in Go, as the
  lxcadd test proves.
- **Deferred members join later.** `test_a_member_never_joined_whose_container_is_stopped_is_deferred_and_the_others_join`
  runs the phase three times (stopped, up, up): joined on the second, no change on the third.
  `test_its_handle_is_parked_from_the_host_and_the_rest_waits_for_the_container` does the same
  for the absent phase, with nothing parked twice.
- **Play tasks.** The two `debug` tasks are gated on `is not skipped` and a non-empty
  selection; `changed_when` still matches only `CHANGED\t`. The helper prints the held-back
  lines after the `CHANGED` lines and one `LXC-SUMMARY`.

## Findings

1. **FIX — user docs still document the old failure.** `docs/agent-bus-lxc-members.md:44`
   (the refusals table) lists `LXC container <container> is STOPPED: start it, then run the play again` as a refusal, and `docs/agent-bus.md:401` says "the container must be running".
   Neither doc mentions SKIPPED/DEFERRED, the member record, `ccy-teams apply`'s held-back
   lines, the board's `deferred` row or lxc-add's notice. The branch changed behaviour the
   docs state as fact; `qa-reviewer`'s docs-drift class. Update both (and the "Leaving"
   paragraph at `agent-bus-lxc-members.md:220` for L6: the handle is parked from the host and
   the bundle waits for the container).

2. **FIX — a team's record is never removed when its last LXC entry goes.** `_write_records`
   iterates `dict.fromkeys(member.team for member in members)`, and `run_phase` reads records
   for those teams only. Remove the only `agent_bus_lxc_members` entry of a team (or empty the
   list, which skips both LXC tasks through `when: agent_bus_lxc_member_list | length > 0`),
   and `<applied>/lxc/<team>.json` keeps its old items for ever: a `deferred` item then shows
   as a board row and in `ccy-teams apply`'s "held back (above)" with a why telling the owner
   to "start it, then run the play again" for a member no entry declares. This contradicts L8
   ("an item no entry declares any more is dropped") in the case where the dropped item was the
   last one, and it is exactly the case of an owner who gives up on a stopped member. A fix:
   the helper sweeps `RECORD_DIR` and removes (or empties) the record of every team in
   `--teams-present` that has no entry in `--members`, with care for `agent_bus_only_teams`
   (the member list is scoped but `--teams-present` is not, so only teams in scope may be
   swept; passing the scope, or sweeping only when the run is unscoped, would do), and the play
   runs the helper even when the member list is empty so the sweep happens. Add a test:
   record present, entry removed, run, record gone.

3. **Note (no change needed, but say it in the deploy comment).** On the first run after this
   deploys, an already-joined member with a derived entry (no host/seat/repo) in a stopped
   container has no record yet, so it is DEFERRED with "this host has no record that it joined",
   not SKIPPED; its team's seat derivation runs with `unheld_ok`; and the board shows both its
   roster row (active, lxc) and a second kind-`lxc` `deferred` row with no handle for the same
   member, until the first run with the container up writes `joined`. This is what L2/L4 say,
   and it resolves itself, but the owner will see it on the very first deploy.

4. **Merge conflicts against F44 de85f5f6 (two files, both expected).**

   - `CLAUDE/Plan/00203-.../JOURNAL/00203-Journal-26-10-19.md`: both sides appended entries
     after 10:xx; keep both sides' entries in time order.
   - `CLAUDE/Plan/meta-deploy.list`: F44 now queues `play-agent-bus.yml` (Plan 00194 Task 2.0b
     kit) then `play-claude-yolo.yml` (ccy 3.99.12.14); the branch queues
     `play-claude-yolo.yml` then `play-agent-bus.yml` with its own comments. Resolve to each
     play once, with both plans' comments merged under it; keep F44's order (agent-bus before
     claude-yolo, as its comment requires) since this branch has no ordering need between them.

5. **Minor — one failure path is a traceback, not exit 78.** `_write_record` catches
   `StampGroupError` and `RecordError` but `grp.getgrnam` raises `KeyError` when the recorded
   group does not exist; the play still fails (exit 1, traceback) so Fail Fast holds, but it is
   the only error in the helper that does not come out as a `MemberError` message.

6. **Minor — report arithmetic.** The opus report's table says "236 pass now" for Python and
   the journal's 11:03 entry says 420 including test_health and test_declare; both are
   consistent with what I measured (236 over the three agent_bus_lxc modules). No action.

Everything else asked for checks out: no install-specific names in tracked files, no
`FAIL-FAST-OK` needed (nothing suppresses a failure), no CCY or image bump needed
(`helpers/agent_bus` and `health.py` untouched; the new writer is under `helpers/agent_bus_lxc`),
the picker no longer offers "Park -" on a handle-less row, lxc-add names the team's declared
members in stopped containers before the yes and reports a deferred new member as deferred.
