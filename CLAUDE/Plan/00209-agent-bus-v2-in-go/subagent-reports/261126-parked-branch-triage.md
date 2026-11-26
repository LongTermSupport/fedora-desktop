# Parked-branch triage: lightning-default-client, photography-idempotent, restore-drill

Read-only review against `origin/F44` at `eed98ea2a` (fetched 2026-11-26). Each branch's park
commit cites Plan 00209 Task 6.5 ("review after v1 retirement"); that blanket reason fits none of
the three. Commits listed by `git cherry origin/F44 origin/<branch>`; the "Park branch:" marker
commits are skipped. Cherry-pick conflicts were measured with `git merge-tree` only; no branch was
touched.

Sources read: `gh issue view` for #324 and #325 (body and comments); #253 and #327 are cited by
the commits but carry no decision these verdicts depend on.

## 1. origin/lightning-default-client

One commit past the park marker, merge base `1e93be889`.

### f29352f9f: Plan 00254 Task 7.1, Lightning is the default Matrix client (held)

Changes the default from `element` to `lightning` in `src-go/internal/bus/declare/clients.go`,
`src-go/internal/declare/declare.go`, the play's `default('lightning')`
(`play-agent-bus-element.yml`), the shared `testdata/client-cases.json`, the plan's Python
checker, `teamtest` fakes (which now pin `element` so the Element flows stay under test),
`localhost.yml.dist`, `docs/playbooks.md`, `docs/agent-bus.md`, the picker help and usage text,
and the queued `meta-deploy.list` lines. 29 files.

**Classification: (a) STILL WANTED, not on F44 in any form, but deliberately held.**

- Evidence it is not on F44: `origin/F44:src-go/internal/bus/declare/clients.go:29` says "The
  default stays element until Plan 00254 Task 7.1"; `play-agent-bus-element.yml:53` is
  `agent_bus_matrix_client | default('element')`; `localhost.yml.dist:221` says "element (the
  default)".
- The hold is the plan's own: `origin/F44:CLAUDE/Plan/00254-lightning-default-team-client/PLAN.md:237-242`.
  Task 7.1 lands only when both gates are met: (a) a Lightning release with the thread-root fix
  on Flathub, and (b) the plan's `acceptance.bash` passed on the host for a team set to
  `lightning`.
- Gate (a) is met: the journal entry `JOURNAL/2026-11-20/110651Z-finding-d35bf8.md` records
  v0.10.6 on Flathub since 2026-11-16, carrying #77 and #76. Gate (b) is not recorded anywhere
  on F44. Status line (PLAN.md:3) still says "Task 2.5's live check is next".
- Not V1-only, but it sits on v1 plumbing. The resolver is in `internal/bus` and
  `internal/declare`, which Plan 00270's INVENTORY.md (rows `internal/bus`, `internal/declare`)
  classes `stays-v1`. It reads the `agent_bus_matrix_client` key, and Task 6.5
  (`00209 PLAN.md:523-528`) ends with "the declaration check refusing `agent_bus_*` keys".
  So v1 retirement does not make this moot. The owner wants Lightning as the default (#253), and
  Plan 00265 REGISTER.md:679-681 says the branch is un-parked when v1 is retired. But after
  Task 6.5 the setting and its resolver have to be re-homed off the `agent_bus_*` declarations,
  because Q4 keeps the v1 packages out of the new repos.

**Porting cost (when the gate clears):** 5 conflicts against F44: `acceptance_lightning.py`,
`meta-deploy.list`, `docs/agent-bus.md`, `docs/ccy-changelog.md`, `claude-yolo`. The CCY bump
(3.123.0) is stale: F44 is at 3.124.85. The commit message also says "ccy's version is
unchanged", but the diff bumps it, so that has to be decided again on the port. The other 24
files auto-merge.

**Verdict: keep parked: blocked on Plan 00254 Task 7.1 gate (b)**, the host `acceptance.bash`
pass for a team set to `lightning`, after Task 2.5's live check. Re-homing the setting off
`agent_bus_*` becomes part of the port once Task 6.5 lands.

## 2. origin/photography-idempotent

One commit past the park marker, merge base `17d115389`.

### 74a4145f0: play-photography, probe RapidRAW by its RPM name, rapid-raw (WIP, #325)

`playbooks/imports/optional/common/play-photography.yml` only. It adds `rapidraw_package: rapid-raw` and replaces the `shell: rpm -q rapidraw ... || echo "not-installed"` probe with
`command: argv [rpm, -q, --queryformat, "%{VERSION}", rapid-raw]`, `check_mode: false`, and a
`failed_when` that accepts only rc 0, or rc 1 with "is not installed". It renames the register
`rapidraw_installed`.

**Classification: (a) STILL WANTED, not on F44 in any form.**

- Evidence: `origin/F44:playbooks/imports/optional/common/play-photography.yml:171-176` still
  runs `rpm -q rapidraw ... || echo "not-installed"`, so every run downloads and deletes the
  RPM, as #325 says. #325 is OPEN.
- It has nothing to do with the bus, so v1 retirement is irrelevant. The park reason is wrong
  for this branch.
- #325's comment says the ART and mock-cache changes were a one-time convergence, so this
  commit is the whole fix.

**Porting cost:** cherry-picks clean (`git merge-tree` exit 0, no conflict). Two things are
still owed before F44, both named in the commit and in #325's comment:

1. `ansible-playbook --syntax-check` in a worktree that has the vault password file. The
   original worktree had none.
2. The idempotence proof through `meta-deploy.bash`: two `play-photography` passes, the second
   with `changed=0`. This needs a `meta-deploy.list` entry in the porting commit.

**Verdict: port: 74a4145f0** (cherry-pick, then the syntax check and the two-pass `changed=0`
proof via meta-deploy). Batch 24 already names it (REGISTER.md:681-682).

## 3. origin/restore-drill

Eleven commits past the park marker (`f5e26c32e`), all dated 2026-11-16, merge base
`c0744c43c`, all for #324. #324 is OPEN.

### What F44 did with #324 meanwhile

F44 answered only step 3 of #324's design comment, the restore onto a fresh guest. It did so a
different way: the sealed carry (`b97599dc8 Merge sealed-carry`; `SPEC-carry.md`;
`src-go/internal/teamchat/carry/`). SPEC-carry.md:25-28 and O7 (:717-719) say the carry is "Not a
restore. DC8, n2g-D8 and RW2 are unchanged; `restore` stays refused in production" and "Not
`--onto-fresh`". So steps 1, 2 and 5 of #324 (the drill, the proof record, the backup docs) are
**not** on F44:

- `origin/F44:src-go/internal/teamchat/admin/wire/wire.go:66-68,170-171` still has the
  `restoreProven bool` that nothing sets, and returns `restore.ErrUnproven`.
- No `restore/proof.go`, `restore/drill/`, `restore/staged.go`, `units/version.go`,
  `hostnet/answers.go`, `admin/drill*.go`, `RestoreProofFile`, `DrillPrefix` or
  `docs/agent-bus-backups.md` exists on F44 (`git ls-tree`, `git grep`).
- 00209 PLAN.md:217-218 (Task 2.4) and :265-269 (Task 2.5) still require H-C5 and H-C15 before
  restore is enabled. This branch is the tool that runs those checks.

### Per commit

All eleven are **(a) STILL WANTED** and v2-only (teamchat), so none is (d). V1 retirement does
not touch them.

| Commit      | What                                                                                                                                                                                  | Files                                                                                                                                                           | Class                                                                                                                                                                                                                                               |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `330a5e31e` | the proof record (`restore-proof.json`): restore enabled only for the running digest, Tuwunel pin and systemd major, with every H-C5 and H-C15 clause passed; `restoreProven` removed | `restore/proof.go`, `restore/unproven.go`, `units/version.go`, `paths/restore.go`, `admin/wire/wire.go`, `admin/{restoreverb,verbs}.go`, SPEC-n2g RW2, CCY bump | (a)                                                                                                                                                                                                                                                 |
| `94a96a7c7` | scratch team `rd-<hex>`, its marker, `CheckStart`, `Clean`; `declare` refuses `rd-` ids                                                                                               | `restore/drill/scratch.go`, `paths/restore.go`, `declare/declare.go`, CCY bump                                                                                  | (a), **with a collision**: see below                                                                                                                                                                                                                |
| `b2b34eab4` | `restore.Deps.OnStaged` and `AfterSwap` hooks, nil in production                                                                                                                      | `restore/{deps,engine}.go`, `hooks_test.go`                                                                                                                     | (a)                                                                                                                                                                                                                                                 |
| `4d64684dd` | `teamchat_restore_drill`, `teamchat_restore_drill_port`, its SELinux label only where enabled; meta-deploy entry                                                                      | `play-teamchat.yml`, `meta-deploy.list`                                                                                                                         | (a)                                                                                                                                                                                                                                                 |
| `367354a1e` | "What a backup holds" in C 3.12.1, and `docs/agent-bus-backups.md`                                                                                                                    | `DESIGN-v2-C-host.md`, `docs/agent-bus-backups.md`, `docs/README.md`                                                                                            | (a), but the text predates the carry: it says putting a team on a machine that never held it "is not built". On F44 that is now the sealed carry (from a seal, not a nightly `N.tar`, SPEC-carry SI5), so the doc must name the carry as the route. |
| `99bc86d7b` | `drill.Run`, the flow and each clause's check over a fakeable rig                                                                                                                     | `restore/drill/drill.go`                                                                                                                                        | (a)                                                                                                                                                                                                                                                 |
| `84d756555` | the `restore-drill --port P [--clean]` verb, its exit codes, `HostDrill` (Run refused)                                                                                                | `admin/{drillverb,hostdrill,exit,run,verbs}.go`, `wire.go`                                                                                                      | (a)                                                                                                                                                                                                                                                 |
| `b3415c570` | `restore.Staged` (re-serve the staged copy for the probe) and `OS.Owners` (the lstat walk)                                                                                            | `restore/staged.go`                                                                                                                                             | (a)                                                                                                                                                                                                                                                 |
| `04a498413` | `admin.DrillRig`: apply and seed the scratch team, the Matrix probes; the `rd-` refusal moved to the host_vars teams list                                                             | `admin/{drillrig,drillmatrix}.go`, `declare/declare.go`                                                                                                         | (a)                                                                                                                                                                                                                                                 |
| `5311a2699` | the host probes (owners, joined epoch, member reach, staged); `hostnet.Answers`                                                                                                       | `admin/drillprobe.go`, `hostnet/answers.go`                                                                                                                     | (a)                                                                                                                                                                                                                                                 |
| `9b210f7f4` | the host drill wired to the rig, port-in-use refusal, `--where host\|vm-lab`                                                                                                          | `admin/{drillverb,hostdrill}.go`, `wire.go`                                                                                                                     | (a)                                                                                                                                                                                                                                                 |

### The blocking collision: two meanings of `AdminState(t)/drill.json`

The branch's `paths.DrillMarker(t)` (commit `94a96a7c7`) is `AdminState(t) + "/drill.json"`.
F44 already gives that exact path another meaning:
`origin/F44:src-go/internal/teamchat/paths/carry.go:13-14`, `DrillRecord(t)`, "a drill export
under way (SD2)".

F44's carry guard reads that file. `carry/guard.go:49-50` maps `RecordDrill` to
`paths.DrillRecord`. `wire.go:136` joins `carry.Guard` to every lock-taking verb, and
`RemoveGuard` (`carry/guard.go:165-169`) refuses `team remove` while it exists. Cherry-picked
as-is, the restore drill's scratch team would have its own apply, backup, restore and purge
refused as "a drill export of team rd-… is under way". `--clean` could never remove a leftover.
The marker needs renaming (for example `restore-drill.json`), with a `paths` test that the two
names differ.

### Porting cost

- `git merge-tree origin/F44 origin/restore-drill`: 11 conflicting files: `meta-deploy.list`,
  `ccy-changelog.md`, `Dockerfile`, `claude-yolo`, `play-teamchat.yml` (F44 added the carry
  archive cap, `6bb3232d8`), `admin/exit.go`, `admin/verbs.go`, `admin/wire/wire.go`,
  `wire_test.go`, `paths/paths_test.go`, `paths/separation_test.go`. Most come from the carry
  landing in the same `wire.go`, `verbs.go` and `exit.go` regions.
- The CCY bumps (3.122.16 and 3.122.17) are stale against F44's 3.124.85. `paths` is an image
  input, so the port needs one fresh bump.
- The drill marker rename above, and a check that the drill's `CheckStart` ("no other team on
  the machine") and `Clean` also respect the carry records (`sealed.json`, `carried.json`, the
  import record), since the carry's guards now share the lock.
- The backup doc (`367354a1e`) updated to name the sealed carry.
- Not on the branch and still owed by #324: step 4, the VM-lab scenario
  `teamchat-restore-drill`, and the host run, which #324 Q1 holds until the owner answers.
- None of this enables restore. Per the branch's own design, `H-C15.pool-seat` and
  `H-C15.held-session` are always recorded `skipped` (`99bc86d7b`), so restore stays refused
  until Gate B and Task 4.4. The port lands inert, as the carry did.

**Is a still-open task blocked on it?** Yes, the other way round. Plan 00209 Task 2.5's H-C5 and
H-C15 (`PLAN.md:265-269`), and through them the production `restore` of Task 2.4
(`PLAN.md:217-218`), have no tool to run them without this branch. Nothing blocks the branch
itself except the owner's #324 Q1 for a host run. The VM-lab run does not wait.

**Verdict: port: 330a5e31e, 94a96a7c7, b2b34eab4, 4d64684dd, 367354a1e, 99bc86d7b, 84d756555,
b3415c570, 04a498413, 5311a2699, 9b210f7f4**, in that order, with these reworks: rename the
`drill.json` marker so it no longer collides with the carry's `DrillRecord`, give the CCY bump
fresh numbers, update the backup doc for the carry, and resolve the 11 conflicts against the
carry's wiring. Batch 24 already names it (REGISTER.md:681-682).
