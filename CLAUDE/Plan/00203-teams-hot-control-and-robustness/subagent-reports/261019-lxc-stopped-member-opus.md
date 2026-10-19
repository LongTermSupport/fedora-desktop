# Task 2.6: LXC members whose containers are stopped (report)

Branch `fix/lxc-stopped-member-2`, branched from 437da85e (the tip of `fix/lxc-stopped-member`,
which was still checked out in the previous agent's worktree). Two commits on it: 437da85e (the
previous agent's core fix) and 125fe94f (this agent's work), with this report committed after
them. Not pushed, not merged.

## Cause

`run_present` in `helpers/agent_bus_lxc/members.py` (F44 lines 1016-1031) called
`_guest_running` (F44 lines 784-791) for every member declared present, on every run, and raised
`MemberError` (exit 78) on any container that was not RUNNING. That included members joined long
ago. The absent phase did the same. So one stopped container failed the whole play, and with it
every other team change, a different member's `ccy-teams lxc-add` included.

## Fix (spec: SPEC-lxc-stopped-members.md, L1-L11)

- **The play (437da85e, L1-L8).**
  - A container that is not running is never entered and never fails the play.
  - A member already joined is skipped, and keeps its seat claimed.
  - Any other member is deferred, and is joined by the first run with its container up.
  - The seat derivation passes over active seats that no entry names (`unheld_ok`), so a new
    member is never refused because of a stopped one.
  - A missing container, and every failure inside a running one, still fail.
  - An absent member is parked from the host side. The cleanup inside its container waits for
    the first run with the container up.
  - The play prints `SKIPPED`, `DEFERRED` and `LXC-SUMMARY` lines, in a task after each phase.
  - The play writes the member record `/var/lib/agent-bus-applied/lxc/<team>.json` through
    `helpers/agent_bus_lxc/applied_record.py`.
  - `helpers/agent_bus/` and `health.py` are unchanged, so there is no CCY or image bump.
- **ccy-teams apply (437da85e, L9).** `internal/teams/lxcrecord` reads the record. `ops` names
  each member that was held back. An unreadable record fails the apply.
- **Board (125fe94f, L10).**
  - A joined member whose container was stopped gets ", container stopped at the last apply" on
    its WHERE.
  - A deferred member gets a row of its own: kind `lxc`, state `deferred`, and WHERE
    `deferred: <why>`.
  - A listed member that is leaving with its container stopped shows `deferred` and
    `deferred (leaving): <why>`.
  - An unreadable record is reported on the team's row.
  - `deferred` is shown in yellow.
- **Picker (125fe94f).** A deferred row has no handle, so it no longer offers "Park -".
- **lxc-add (125fe94f, L11).**
  - Before the summary, it says "Declared in <team>, container not running: user@c, ... The
    play skips a member already joined and defers one not yet joined; neither blocks this add.
    Nothing is started."
  - After the apply, a new member that the record shows as deferred is reported as deferred,
    with its why, not as joined.
  - An unreadable record fails.
  - `Adder.AppliedDir` is set from the host config.
- **meta-deploy.list.** Header lines 1-21 are unchanged. The `play-claude-yolo.yml` comment now
  covers the ccy-teams changes. `playbooks/imports/play-agent-bus.yml` is queued after it, with
  a comment. On F44 the queue has since been emptied (b2e49b1e), so the coordinator reconciles
  the list at merge.
- **Journal.**
  - A `correction` entry: `health.py` was not refactored.
  - An `action` entry with the red and green results.
- **PLAN.md.** The Task 2.6 line names both branches and says the deploy is queued. The task
  stays 🔄 until it is deployed.

## Tests, red then green

| Tests                                        | Red (before the code change)           | Green (after it) |
| -------------------------------------------- | -------------------------------------- | ---------------- |
| Board: `board/lxcrecord_test.go`             | 5 of 6 failed                          | Pass             |
| Picker: `TestADeferredLxcRowOffersNoParking` | Failed, offering "Park -"              | Pass             |
| lxc-add: `lxcadd/stopped_test.go`            | 3 of 5 failed (the other 2 are guards) | Pass             |
| Python (earlier commit)                      | 77 errors of 141                       | 236 pass now     |

## Targeted QA (all pass)

- `python3 -m unittest` over test_members, test_applied_record and test_derive: 236 OK.
- `go test -race ./internal/teams/...`: pass.
- `golangci-lint run ./internal/teams/...`: 0 issues. `gofmt` is clean.
- `scripts/test-ccy-teams-command.bash`: passed 383, failed 0.
- `scripts/qa-hidden-bindings.bash`: OK.
- `scripts/qa-bash-limits.bash`: OK.
- `ansible-playbook --syntax-check playbooks/imports/play-agent-bus.yml`: passes. This was a
  syntax check only; no play was run.
- `qa-all` was not run, as instructed.

## For the coordinator

- Reconcile `CLAUDE/Plan/meta-deploy.list` at merge. It needs `play-claude-yolo.yml`, which
  rebuilds ccy-teams, and `play-agent-bus.yml`.
- No CCY or container version bump is needed.
