# Re-review: fix/lxc-stopped-member-3 at 6dfa7c9b (Task 2.6, FIX-FIRST round)

Re-checked against `261019-lxc-stopped-member-review-fable.md` (findings 1-5 on 3fe83b95). Fixes
are 9b3d046c; 6dfa7c9b merges F44 5ce37f5e `--no-ff`. Branch checked out detached in this
worktree; nothing in it was changed.

## Verdict: MERGE

Every finding is fixed, and the riskiest new behaviour, the present phase running on every play
run to sweep records, is safe on a host that never used LXC: with no entries it never probes a
container, never looks for an `lxc-*` binary, never reads the stamp group and never needs the
applied directory. I ran the real helper in this container (no `lxc-ls`/`lxc-info`, no
`/var/lib/agent-bus-applied`, no `/etc/agent-bus/stamp-group`) with an empty member list,
unscoped, scoped, and in the absent phase: exit 0, no output each time. The scoping is right, the
sweep is idempotent, and `CHANGED` is printed only when a file is removed.

## Checks run

| Check                                                                                                               | Result                                      |
| ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------- |
| `python3 -m unittest tests.helpers.agent_bus_lxc.test_members test_applied_record test_derive`                      | 245 OK (236 before; 9 new)                  |
| `go test ./internal/teams/...`                                                                                      | 23 packages ok, no failure                  |
| `ansible-playbook --syntax-check playbooks/imports/play-agent-bus.yml`                                              | pass (syntax only; no play run)             |
| `scripts/qa-docs.bash` (whole core-doc scope; it takes no file arguments)                                           | 83 files OK, anchors included               |
| `ruff check` on the two helpers and two test modules                                                                | all checks passed                           |
| Real `main`, empty members, host with no LXC / no applied dir / no stamp group: unscoped, scoped, absent            | exit 0, no output, each                     |
| Real host functions against a scratch record dir (only `RECORD_DIR`, `STAMP_GROUP_FILE` redirected): see below      | as expected                                 |
| Journal merge: every non-blank line of F44's and the branch's day-file present; entries 09:50 … 11:14 in time order | union, ordered                              |
| `meta-deploy.list` lines 1-21 vs F44                                                                                | byte-identical (`cmp`)                      |
| `meta-deploy.list` entries                                                                                          | each play once; agent-bus, then claude-yolo |

The scratch run, in order: records `team-a`, `team-b` present. Scoped run `--only-teams ["team-a"]`, no entries: `CHANGED\tthe LXC member record of team team-a`, `team-b.json` kept.
Absent phase, no entries: nothing, `team-b.json` kept. Unscoped, no entries: `CHANGED … team-b`,
directory empty. Unscoped again: exit 0, no output.

## Findings 1-5, verified

1. **Docs drift — fixed.** `docs/agent-bus-lxc-members.md`: the STOPPED refusal row is gone; the
   prerequisite table is stated for running containers; a new section "Containers that are not
   running" covers SKIPPED, DEFERRED, the first-run deferral of a member with no record yet, the
   member record (what it holds, 0640 and the stamps' group, rewritten only on change, removed for
   a team not present or with no entry left, scoped runs remove only their teams'), and what
   `apply`, the board and `lxc-add` say; "Leaving" now describes L6 (parked from the host, bundle
   and unit wait for the container, `DEFERRED` line). `docs/agent-bus.md` ~:401 says the same in
   short and links the section; the anchor resolves (qa-docs). Every statement matches the code I
   traced in the first review and the sweep below.

2. **Stale record of a team with no entry left — fixed.** `applied_record.names()` lists
   `<dir>/*.json` (hidden `.<name>.new` temp files excluded; missing dir is `[]`; symlink or
   non-dir is `RecordError`). `Host.record_teams` is wired to it in `real_host`, with `OSError`
   and `RecordError` turned into `MemberError` (exit 78). `sweep_records` removes the record of
   each team not named by any entry (present or absent state), limited to `only_teams` when given.
   `main` runs the present phase on every call and sweeps after `run_phase`, so a team still
   named has its record written first and is never swept. The play's present task drops the
   `agent_bus_lxc_member_list | length > 0` guard (absent task keeps it, correctly: the absent
   phase sweeps nothing). SPEC L8 states the sweep. Tests: `RecordSweepTest` (6) pin the last
   entry removed, an empty list, scoped sweep, scoped keep, absent sweeps nothing, `--only-teams`
   must be a list.

3. **First-run note — done.** The agent-bus comment in `meta-deploy.list` ends with the
   already-joined-member-shows-deferred sentence, and the docs section carries it.

4. **Merge — resolved as prescribed.** Journal: union of both sides in time order (verified line
   by line). `meta-deploy.list`: header exact, each play once, F44's order (agent-bus before
   claude-yolo, as the Plan 00194 comment requires), both plans' comments merged under each. The
   branch's "Plan 00203 and Plan 00201 Task 2.18" ccy-teams text was dropped: F44 b2e49b1e removed
   exactly that block as deployed, so dropping it is correct. The claude-yolo comment's Task 2.6
   text ("rebuilds ccy-teams, no image change": apply/lxc-add held-back lines, lxc-add's
   stopped-members notice, the board's deferred row and "container stopped at the last apply")
   matches the Go changes in the branch.

5. **KeyError on a vanished stamp group — fixed.** `_write_record` catches `grp.getgrnam`'s
   `KeyError` and raises a `MemberError` naming the group and `/etc/agent-bus/stamp-group`
   (exit 78). `WriteRecordTest` pins it. (`teamfile.read_stamp_group` already refuses a
   non-existent group itself, so this path is reached only in the race its message names; the
   defence is still right.)

## The new behaviour, scrutinised

- **A host that never used LXC.** `main` → `real_host()` → `checkout.real_system()` only
  builds a `System` of callables and `Path.cwd()`/`getuid()`: no I/O. `run_phase("present", [], …)` returns before reading any record or calling `_check_teams`. `sweep_records` calls
  `names(RECORD_DIR)`: `lexists` false → `[]`, so `write_record`, and with it the stamp-group
  read, is never reached. No `lxc-*` binary is ever looked up (`_run` is only reached through a
  container probe, which needs an entry). Confirmed empirically above. A host where
  `/var/lib/agent-bus-applied/lxc` is a regular file or symlink gets exit 78 with a message:
  correct fail-fast, and not a state any supported path produces.
- **Records present, no entries.** `write(dir, name, None, gid)` only unlinks; it does not
  require the applied parent and does not mkdir. The stamp group is read first (`_write_record`);
  a record can only exist on a host whose earlier run had the group, and the same play rewrites
  `/etc/agent-bus/stamp-group` unconditionally before the present task, so the only way to hit
  the refusal is the group being deleted from `/etc/group` by hand, and 78 with a clear message
  is the right answer.
- **Check mode.** `when: not ansible_check_mode` skips the task; the debug task is gated on
  `is not skipped`. Nothing runs, nothing is reported. Same as before for the absent task.
- **Scoping.** Unscoped: `--only-teams` absent, `only is None`, every team with a record and no
  entry is swept, including a team removed from `agent_bus_teams` altogether (right: no entry
  declares it). Scoped: `agent_bus_lxc_member_list` holds only the scoped teams' entries, so an
  out-of-scope team looks unnamed, and `team in only_teams` is what protects it; the play passes
  `agent_bus_only_teams | to_json` exactly when the variable is defined, and the play's existing
  assertion (line 94) already requires it to be a list of declared team names. A team in scope
  with entries keeps its record (`test_a_scoped_run_keeps_…`). A team whose entries are all
  absent-state is named, so it is not swept; its record is the absent phase's (`left` items),
  and it is swept on the first run after the owner deletes the entry. That is L8 as written.
- **Idempotent, changed-only-on-removal.** `write(None)` returns False when the file is absent,
  so the second run emits nothing; `changed_when` matches only `CHANGED\t`. Confirmed above.
- **Order of output.** `CHANGED` lines are emitted during the run; the `SKIPPED`/`DEFERRED`/
  `LXC-SUMMARY` report lines are printed afterwards in `main`, as before. The sweep adds only
  `CHANGED` lines, so the debug task's selection is unaffected.

## Notes (no change needed before merge)

- 9b3d046c added no JOURNAL entry for the FIX-FIRST round; PLAN.md's Task 2.6 line does name
  `fix/lxc-stopped-member-3`. Worth one `mkplan.bash --journal` entry at merge or deploy time so
  the day-file records the round, but it is not drift in the plan's state.
- `real_host()` is now built on every present run even with no work; it is free of I/O, so this
  costs nothing. If `real_system()` ever grows a probe, the empty-members path would inherit it;
  the empty-list test (`test_an_empty_member_list_removes_every_record`) uses a fake host, so
  that regression would not be caught by the unit tests. Not actionable now.
- No install-specific names in any tracked file of the diff over F44 (fixtures still `devbox`,
  `box2`, `team-a`, `team-b`); no `failed_when`/`ignore_errors` added; no CCY or image bump
  needed (`helpers/agent_bus` and `health.py` untouched; `applied_record.py` is under
  `helpers/agent_bus_lxc`).
