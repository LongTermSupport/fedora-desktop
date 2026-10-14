# Plan 00196 adversarial review: team coordinator role

Branch `feat/team-coordinator` against F44 (62 files, +2428/-364), reviewed on
`review/team-coordinator`. Read: PLAN.md, the build report, the journal,
`docs/agent-bus-protocol.md`, `docs/agent-bus.md`, `helpers/pingbus/{protocol,cli,hooks}.py`,
`helpers/agent_bus/{registry,admin}.py`, `helpers/agent_bus_lxc/members.py`,
`src-go/internal/{agentbus,teams}` (role.go, choose.go, menu.go, board, parse.go), the launcher
and seats-library diff, the allowlist, the Dockerfile, the 00161 acceptance diff, and the test
diffs.

## Verdict: merge

No blocker. The trust boundary holds: nothing a peer puts in a message changes how the receiver
classes the sender or marks a copy. Four should-fix items below are narrow and none needs to
block the merge; the first is worth a follow-up commit before the host run reaches a multi-team
LXC container.

## Runs

- `pytest tests/helpers/pingbus tests/helpers/agent_bus tests/helpers/agent_bus_lxc`: pass
- `go test ./...` in `src-go`: pass
- `scripts/test-ccy-teams.bash`, `scripts/test-ccy-teams-command.bash`,
  `scripts/test-ccy-agent-bus.bash`: pass

No Ansible, no qa-all (sub-agent).

## Findings, ranked

### Should-fix

1. `helpers/agent_bus_lxc/members.py:717` — `coordinators_last` is defeated by `_groups`. The
   sort puts coordinator entries last, but `run_present` then groups by `(container, user)` and
   processes each group in order of its first entry. A container whose list has a worker entry
   in another team before its coordinator entry is grouped where the worker entry falls, so its
   `set-role --role=coordinator` can run before the old holder's demotion in a later group. The
   handover is then refused (loudly, holder named) on every run until the owner reorders the
   entries; a rerun does not recover. The test
   (`tests/helpers/agent_bus_lxc/test_members.py`, "workers join before a coordinator") only has
   one entry per container, so it does not reach this. Fix: order the groups by the sort, or run
   the demotions as a pass before any promotion.
2. `CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-u20.inc.bash:510` — acca is now the
   coordinator, so every `say`/`done` from accb or accc to the human also names acca. The U20
   checks that judge "exactly" what each seat received were written without that addressee.
   The build report records this as decision 5 (unexercised, host only); it is queued after
   00196 in `meta-deploy.list`, so the owner will see it fail or pass on the host. Flagging so
   the first red there is read as expected and not as a defect of the copy.
3. `helpers/pingbus/hooks.py:210` — a worker is told only `coordinators[0]` when a legacy record
   lists several, while `--to-coordinator` (`cli.py:689`) sends to every one. Small
   inconsistency in the "several coordinators" case the spec says receivers tolerate; say "the
   coordinators are ..." or name the one the writers would keep.
4. Red-first evidence: PLAN.md Task 1.1 says "(red tests first)" but the journal has no red
   record and each commit lands tests with code. The tests themselves are good (below), so this
   is a process gap, not a quality one.

### Nits

5. `CLAUDE/Plan/00196-team-coordinator-role/deploy.bash:11` says "CCY 3.97.0"; the launcher
   is 3.97.1 (`files/var/local/claude-yolo/claude-yolo:17`).
6. `docs/agent-bus-protocol.md:333` promises the copy that pushes `to` past the limit is refused
   `target`. True by `_check_text_targets` (`MAX_TEXT_TO = 16`, `protocol.py`), but no test
   pins a 16-human `say` with a coordinator in the team.
7. `files/var/local/claude-yolo/claude-yolo:1232` — `ccy-teams describe-seats` replaces an
   `echo` inside a `set -e` block. `Describe` is written never to fail on content, so this is
   only a concern on a host where the launcher is updated and the Go binary is not; the play
   deploys both.
8. `helpers/pingbus/cli.py:193` — `_copy` raising `ValueError` for a non-human in `copy_of` is
   unreachable (`_copy_of` filters on `ctx.humans`, which `parse_team_record` validated); fine
   as a guard, but a `ValueError` out of `recv` would be a crash, not a `DROPPED`.

## What held up

**Privilege and trust.** Sender class comes only from `ctx.roles` / `ctx.humans`, both out of the
admin-sent team record (`protocol.sender_class`, `check_text_sender`); nothing in content
reaches it. The copy mark is derived on the receiver (`protocol._copy_of`) from `to ∩ humans`
and only when the receiver's own role in the record is coordinator; the sender cannot add,
remove or forge it by text, and the `PEER`/`PING` lines are tab-separated with the text as an
`ensure_ascii` JSON string, so no text can grow a `copy:` field. A worker who names a human and
the coordinator by hand produces exactly the auto-copy shape; that is the spec's definition, and
it grants nothing. An unknown role value (`boss`) is still `Untrusted("roles")` in
`parse_team_record` after `normalise_role`, and `RegistryError` in `registry._check_role`; the
LXC parser refuses it too. Humans still cannot send agent text; the human path is untouched.
With several coordinators in a legacy record, each is treated as one and a coordinator's own
message to a human is never copied to another (the copy fires on `sender == worker` only).

**Compatibility.** A pre-rename pingbus reading a record that says `coordinator` hits
`role not in ROLES` → `Untrusted("roles")` → exit 10 on every command, `UNTRUSTED` in `status`,
and the watcher drops the team: loud, same mechanism as `org`/`exclude`. `orchestrator` reads
as `coordinator` in the record, the registry file, `agent-bus list` (Go `ParseRoster`) and the
LXC entries. Wire shape unchanged, so an old receiver accepts a copy as an ordinary message.
Protocol stays 1 by the same rule those two keys set. Container 2.55 forces the image rebuild
that carries the new pingbus.

**Auto-copy.** `with_coordinator_copy` adds only when sender is a worker, a listed human is in
`to`, and the coordinator is not already there; `_addressees` dedups `--to` against
`--to-coordinator`; coordinator → human is never copied to itself; no coordinator → nothing
added. The send gate admits one message, so a copy costs one token and one duplicate-window
entry, never two. Tests cover every branch through the real CLI and a fake room (`test_cli.py`,
`test_cli_history.py`), the receiver marks (`recv` line fields), and the hook context.

**Fail-fast.** Second coordinator refused in `Registry.add_member` and `set_role` with the
holder named (parked holder still counts); the LXC parser refuses two `present` coordinators in
one team before anything runs. Go `ChangeRole` reads the roster, refuses a non-member, asks
first, needs `SudoReady` (refusal → "sudo is not ready, so no role was changed"), demotes the
holder then promotes, and on a failed promotion restores the holder or says exactly which step
is left undone. The play resetting a ccy-teams-chosen role on an LXC member is a documented
`"X is now worker"` change line (`docs/agent-bus.md:402`), not silent; the inverse (an LXC entry
says `coordinator` while a ccy seat holds it) fails the play with the holder named.

**Bash ratchet and version.** `claude-yolo` row unchanged at 2839/12/2748; `agent-bus-seats.bash`
shrank 145/7/164 → 81/5/98 and lost its `ui` class; the chooser moved to Go (`teams/choose`)
with tests. No statements folded, `set -e` intact. CCY 3.96.2 → 3.97.1, container 2.54 → 2.55 in
both files, changelog entry present.

**Tests.** Not tautological: the CLI tests assert the sent content against `build_ping`/
`build_text` with the coordinator added, the receiver tests assert the eleventh `PING` field
and eighth `PEER` field from `recv` output, the registry tests assert the holder's handle in the
error, the Go tests drive `ChangeRole` through the fakes including the rollback path.
