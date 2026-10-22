# Adversarial review, round 2: fix/offline-members-never-block

Diff reviewed: `git diff F44...HEAD` at tip 8cb85c27c (58 files). Read-only review. This round checks
the fixes for round 1's findings 1-12 (commits 521e97108, 0be44b47f, 49256e214, 1959e982d,
617b48bc4), then looks for new defects across the whole diff.

All runs were green:

- `python3 -m unittest` over test_offline_member_rule, test_admin_side_rooms, test_cli,
  test_members and test_derive: 315 tests, OK
- `cd src-go && go test ./internal/teams/...`: every package ok
- `scripts/test-planlib.bash`: rc 0
- 00211 `acceptance.bash --stage 1 --fake`: FAKE ACCEPTED, rc 0. It includes the new legs for
  ABORTING, an unknown container, and an old pingbus running beside a stopped container.
- 00211 `acceptance.bash --stage 2 --fake`: FAKE ACCEPTED, rc 0

## Round 1 findings, checked one by one

| #   | Verdict                   | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| --- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | resolved                  | `check_rooms` reads each coordinator's `agent_bus.status` in the main room (`format=event`, through `protocol.read_status`, `until > now`). Invited or not joined while live is a PROBLEM that names `pingbus status` and the human_text case. Not live is OFFLINE. Tests cover listening+invited, listening+no reports, and a lapsed status. A status is published only by `watch`/`wait` (`cli.py:1674`, `:1782`), and those are the same processes that sync and join. So "not live" means "not on the bus", which is the right signal. |
| 2   | resolved                  | `_judge_coordinator` is now exhaustive. `join` passes. `leave` is a problem. `None` in questions is a problem. invite/None is split by liveness. Every other membership (`ban`, `knock`) is a problem. There is a `ban` test for both values of `live`.                                                                                                                                                                                                                                                                                    |
| 3   | resolved                  | Wind-down's `byRecord` and shutdown's `resolveLxc` now send `StatusStopped` through the live `Containers` list. Only `StatusDeferred` is offline from the record alone. Tests: a stopped record with a running container is wound down (wind-down) or exits 1 (shutdown). With the container still stopped, both say "offline (container box stopped)".                                                                                                                                                                                    |
| 4   | resolved, with a nit (N6) | The wording is now "not driven from here (offline, or a host member)", and the test question is NOT JUDGED. The joins of an undriven coordinator are left to A1/A2, which now judge by live status.                                                                                                                                                                                                                                                                                                                                        |
| 5   | resolved                  | SPEC §4.5, §5.3, §5.4 and §11.2 (with an explicit scope change to Task 2.6), PLAN Tasks 2.5, 2.6 and 2.8, the new Task 2.9 (the safe subset of roles), and ACCEPTANCE-owner.md all describe the new behaviour.                                                                                                                                                                                                                                                                                                                             |
| 6   | resolved                  | 00213 prints `ACCEPTED, C3 NOT JUDGED (...)` and 00203 prints `ACCEPTED, R9 NOT JUDGED (...)` when no declared container is online. See N3 for the partly-offline case.                                                                                                                                                                                                                                                                                                                                                                    |
| 7   | resolved                  | A coordinator that is live but not in reports gets the `pingbus status` repair. The "older than 0.2.0" text now appears only on `leave`.                                                                                                                                                                                                                                                                                                                                                                                                   |
| 8   | resolved                  | `roles.isMember` says "offline (container X stopped; ...)" only when `Containers` lists the container as stopped. Otherwise it says "its checkout could not be read (...)". Tests cover a running container and a failed list.                                                                                                                                                                                                                                                                                                             |
| 9   | resolved, with a nit (N1) | Restart has its cleanup hint back ("ccy-sessions ends the leftover tmux session"). The stage-2 line now names relaunching as the repair.                                                                                                                                                                                                                                                                                                                                                                                                   |
| 10  | resolved                  | R9 reads `agent_bus_lxc_members` through ad-hoc Ansible, as 00213's `read_members` does. It takes the present `service: true` entries and checks each online one's `agent-bus-claude-<user>.service` (matches `members.service_unit`). It no longer uses `lxc-ls`.                                                                                                                                                                                                                                                                         |
| 11  | resolved                  | `plan_lxc_online` writes offline lines behind `PLAN_LXC_OFFLINE_PREFIX`. `plan_lxc_online_named` reports only those lines and passes any other stderr through unchanged.                                                                                                                                                                                                                                                                                                                                                                   |
| 12  | resolved                  | ABORTING fails and names the repair. Every other state is printed raw as `container <state>`.                                                                                                                                                                                                                                                                                                                                                                                                                                              |

The decided item (a role change with no member record still stops, Task 2.9 follow-up) was not
reopened. The new Task 2.9 text matches round 1's suggestion.

Version bumps were checked and are consistent:

- After the F44 merge: CCY 3.99.15.5 above F44's 3.99.15.4.
- Container 2.78.17 in both the Dockerfile LABEL and REQUIRED_CONTAINER_VERSION.
- One changelog entry, 3.99.15.5, with the 3.99.14.13 and .14 entries folded in. No stale
  3.99.14.13 or .14 references are left in CLAUDE/ or docs/.
- meta-deploy.list carries the new wording and versions.

## New findings

None of these is a block, a false offline, or a running member let through. Every one is
wording or verdict text.

### N1. minor: the joins wait gives the wrong repair to an online coordinator for `leave` and for human_text false

File: `_acceptance-stage2.inc.bash:177`. When a drivable coordinator has not joined after 180 s,
the message says, for every membership, "a session started before deploy 1 runs the old pingbus,
and joins once it is relaunched". Two cases get the wrong repair:

- **`leave`.** A relaunch alone does not bring it back. SPEC §5.3 says "the re-run's play invites a
  `leave` coordinator again", so the repair is relaunch, then meta-deploy again.
- **`invite` from a running session whose bundle has `human_text: false`** (SPEC §4.5). This
  coordinator is not old, and relaunching does not help.

This leg fails before A1/A2 runs, so the operator never sees check-rooms' correct text.

**Fix:** split the message by membership. For `leave`: relaunch, then run meta-deploy again. For
`invite` or `-`: `pingbus status` in that session names why (an old pingbus, or human_text
false).

### N2. minor: A5's "no test question" refusal points at a deploy 2 that can now pass without asking one

File: `_acceptance-stage2.inc.bash:582`. The message reads "run deploy 2 until its acceptance is
ACCEPTED". Since the scope change, deploy 2 is ACCEPTED with the coordinator offline and no
question asked, so following this advice can loop.

**Fix:** "run deploy-stage-2.bash with the coordinator online: an earlier run had it offline and
asked no test question".

### N3. nit: verdict lines that overstate what was judged

- **Stage 2's final line** (`acceptance.bash:594`) does not use the form SPEC §11.2 now promises,
  `ACCEPTED, <check> NOT JUDGED: ...`. It keeps "kept a worker out of questions and every online
  inbox" even when the A3 inbox half was NOT JUDGED. The NOT JUDGED lines are in the report, so
  nothing is hidden, but the verdict text disagrees with the SPEC. Stage 1 and `--after-owner` do
  use the form.
- **00213 with some (not all) containers offline** still prints "installed everywhere"
  (`_acceptance.inc.bash` `accepted_line`).
- **00203 in the same case** still prints "R1-R9 passed".

Say "everywhere online" for 00213, or name the offline count.

### N4. nit: stale docstrings and fixtures that still say "invited, so offline"

- The `admin.RoomCheck` docstring: "offline (invited, its session not running)".
- The `fake_a12` OFFLINE fixture line: "alpha is offline (invited to questions)".
- The `test_cli.test_check_rooms_prints_an_offline_coordinator_and_exits_0` docstring: "invited
  but not joined is offline".

The rule is now "no live status". Being invited is not the reason.

### N5. nit: "its session is not running" is slightly stronger than the evidence

Found in `docs/agent-bus.md`, the changelog and the `check_rooms` docstring. What check-rooms
knows is "no live `agent_bus.status`". That covers a stopped session, and also a session whose
watcher is not running. The OFFLINE line itself words it correctly ("no live status in the team
room"); only the prose around it overstates.

### N6. nit: an undriven coordinator that is listening is not waited for before A1/A2

This is the last bullet of round 1 finding 4. A host-member coordinator that is listening and
has not synced yet would be a PROBLEM at A1/A2, with no 180 s grace. The risk is low: the ccy
play, with its image rebuild, runs between the agent-bus play's invite and the acceptance. And
the failure is loud, not silent. Record it, or have `rooms_joined` also wait on check-rooms'
live-but-not-joined problems.

## Checked and fine

- **Fail fast.**
  - `plan_lxc_online_named` returns the helper's rc after reporting.
  - `lxc_sessions`, `read_online`, `s2_check`, `c3_lxc` and `check_lxc_members` all propagate
    failures.
  - A container that lxc-info cannot read, or one that is ABORTING, still fails.
  - A running container with the old binary still fails, even next to a stopped one (fake leg).
  - The roles `listErr` only changes the wording and never decides the outcome. Both branches
    pass the entry over, as round 1 accepted.
- **No over-correction left.**
  - Wind-down and shutdown use the live list for every member that has a handle.
  - Check-rooms uses live status.
  - Roles uses the stopped list for its wording.
  - Stage 2 says "offline, or a host member", not just "offline".
- **Streams.**
  - `find_session` writes its message to stderr and the report, never to the captured stdout.
  - `a5_find` sets globals, not output.
  - Check-rooms OFFLINE lines are on stdout, which is its payload, as `cli.py`'s stream doc now
    says.
- **Public repo.** No install-specific names in the diff.
- **Bash limits.** The new bash is in plan folders (exempt) and `CLAUDE/Plan/_members.inc.bash`
  (2 functions, small). The test suite passed.
- **DBF.** Conformance round 2's open items (the second Narrowing, the single-instance allowance,
  entry-point green) are process items for the coordinator. Item 3 needs the coordinator's qa-all
  run before push. They are not code defects in this diff.

VERDICT: MERGE
