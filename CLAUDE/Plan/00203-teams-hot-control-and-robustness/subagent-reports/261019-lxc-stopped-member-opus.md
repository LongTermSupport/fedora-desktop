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

## FIX-FIRST round (fix/lxc-stopped-member-3, from 3fe83b95)

Fable's review is copied beside this report as `261019-lxc-stopped-member-review-fable.md`.
`fix/lxc-stopped-member-2` was checked out in another worktree, so this round is on
`fix/lxc-stopped-member-3`, branched from 3fe83b95.

| Finding                                   | What was done                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 Docs drift                              | `docs/agent-bus-lxc-members.md`: the STOPPED refusal row is gone. The checks are now described as for running containers. A new section, "Containers that are not running", covers SKIPPED / DEFERRED, the first-run deferral of a member that has no record yet, the member record (including the sweep), and what apply, the board and lxc-add say. "Leaving" now describes L6. `docs/agent-bus.md` ~:401 now says the same in short and links to that section. |
| 2 Stale record                            | `applied_record.names()` lists the records. `Host.record_teams` was added. `members.sweep_records()` removes the record of every team that no entry names. `main` runs the present phase on every call, with entries or none, and sweeps after it. The new `--only-teams` limits the sweep to a scoped run's teams. The play passes `--only-teams` only when `agent_bus_only_teams` is defined, and the present task's `when` is now just `not ansible_check_mode`. The absent phase sweeps nothing. SPEC L8 now states the sweep. |
| 5 KeyError                                | `_write_record` turns `grp.getgrnam`'s KeyError into a MemberError (exit 78) that names the group and the stamp-group file.                                                                                                                                                                                                                                                                                                                                    |
| 3 First-run note                          | Added to the agent-bus comment in meta-deploy.list, and to the docs.                                                                                                                                                                                                                                                                                                                                                                                  |
| 4 Merge F44 5ce37f5e (--no-ff)            | Journal: both sides' entries in time order (journal_union.py). Every line of both sides is still present. meta-deploy.list: lines 1-21 are exactly F44's. Each play is listed once, in F44's order (agent-bus, then claude-yolo). The branch's Task 2.6 comments are merged in. I dropped the branch's "Plan 00203 and Plan 00201 Task 2.18" ccy-teams text because 226a292d has already been deployed (b2e49b1e emptied the queue), so it is no longer pending. |

Tests were written first. Before the code change: 86 errors, from the missing `record_teams`, the missing `names` and the KeyError. After it: green. New tests:

- `RecordSweepTest`, 6 tests: the last entry removed; an empty list; a scoped sweep; a scoped run that keeps a team with entries; the absent phase sweeps nothing; `--only-teams` must be a list.
- `WriteRecordTest`.
- `test_names_*`, 2 tests.

### Targeted QA on the merged tree (all pass)

- `python3 -m unittest` over test_members, test_applied_record, test_derive and test_declare: 398 OK.
- `go test ./internal/teams/...`: pass. `golangci-lint run ./internal/teams/...`: 0 issues.
- `scripts/test-ccy-teams-command.bash`: passed 383, failed 0.
- `ansible-playbook --syntax-check playbooks/imports/play-agent-bus.yml`: passes. This was a syntax check only; no play was run.
- `qa-bash-limits`, `qa-hidden-bindings` and `qa-docs` (83 files): OK. `ruff check`: clean. qa-all was not run.

### For the coordinator: porting onto fix/teams-member-roles-2

- **Picker.** The guard is the three lines in `picker/menu.go` `kindActions`: a row whose
  `Handle` is `""` or `"-"` gets only open, start and help. Move it into member-roles'
  `memberActions`, and have it drop the role action as well as Park for such a row. The test
  is `TestADeferredLxcRowOffersNoParking` in `picker/menu_test.go`; extend it to check that no
  role action is offered.
- **lxc-add.** My change to `New()` is one field, `AppliedDir: h.AppliedDir`. Keep it when
  resolving the textual conflict. `lxcadd/stopped_test.go` does not fake `SetRole`.
- Nothing in this round touched Go.
