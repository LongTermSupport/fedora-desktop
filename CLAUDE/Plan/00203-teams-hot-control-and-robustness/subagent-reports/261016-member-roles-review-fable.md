# Fable review: `fix/teams-member-roles` at 0be311bd (Task 3.8, member roles for every kind)

Branched from F44 7f8b1c23; judged against origin/F44 0ce52a7e. Developer's report:
[261016-member-roles-opus.md](261016-member-roles-opus.md). Attack order as briefed.

## Verdict: FIX-FIRST

Two things before merge (list at the end). The work itself is right: one role path, every kind
offered the same role action in the same words, destructive actions last in every menu, the
registry's single-coordinator rule held for every kind, and the live notice reaching agents of
every launcher through the MEMBERSHIP kind only. What is wrong is the claim about the play lock,
which the merge with `fix/teams-wait-play-lock` would turn into a false "nothing was changed".

## 1. Equivalence

**Every role writer goes through `teams/roles`.** Outside the package, the only role writers
left are the adapters themselves: `src-go/internal/agentbus/agentbus.go:194` (`SetRole`) and
`src-go/internal/declare/lxc.go:117` (`LxcSetRole`). Their callers are `roles.step`
(`roles/roles.go:155`, `:165`) and nothing else: `seats.ChangeRole` (`seats/role.go:52`),
hot's `setRoleTo` (`hot/role.go:55`, which `session role`, `add --role` and a remove's hand-to
all reach), and lxcadd's `demote` (`lxcadd/adder.go:591`). No bypass. The Plan 00207 two-path
question is still asked by each caller (`coordinator.Holding` / `coordinatorPath`) and only
the change is shared, which is the right split.

**An LXC member can become coordinator end to end.** Entry (`declare lxc-role`), live
registry (`agent-bus set-role`), apply. The registry change is published to the room by
`_sync_team` (`helpers/agent_bus/admin.py:815-827`), so every member's pingbus reads the new
roles from the trusted team record; coordinator routing, the HUMAN rules and the status line
all read `record.roles` (`helpers/pingbus/hooks.py:519-531`). Matrix power levels depend only
on the humans (`admin.py:573`, `:588`), so no power-level change is owed by a role change.
Proven in `seats/role_test.go:283` and `:301`, `roles/roles_test.go:112`.

**Single coordinator, every kind.** Live: `registry.set_role` refuses a second holder
(`helpers/agent_bus/registry.py:208-222`, `_free_to_hold`), whatever the kind, so the
demote-then-promote order in `roles.Set` is required and is what every caller builds
(`seats/role.go:48-51`, `hot/role.go:49-54`). Declared: `lxc-role` refuses a second declared
coordinator and `summary` blanks the role for the dry run until the holder is demoted
(`lxcadd/adder.go:531-534`). Two instances racing: the second's promote fails at the registry
and `Set` puts its demotion back (`roles.go:141-147`, tested `roles_test.go:203`). Fine.

**The admitted differences**, judged against "fully equivalent in terms of roles":

1. Attach/restore on ccy rows only: not a role; fine.
2. "Needs its container running": understated. `entries.of` reads the checkout of EVERY
   candidate entry of the team to rule out ambiguity (`roles.go:244-255`), so a stopped
   container of a different, unseated entry blocks a role change of an unrelated LXC member,
   and the error names the other container. An entry with its seat set is skipped by `couldBe`
   (`roles.go:226-229`), so the cure is the one the ambiguity error already gives: seat the
   entries. Note, not a blocker; the error text should say which entry blocks and why it was
   read (see F3).
3. No wait for a handle: there is nothing to wait on; `roles.Reach` says how it learns. Fine.
4. Takeover says "not known here" for a non-ccy holder: honest. Fine.
5. Seatless same-repo entries refused, naming both: correct fail-fast (`roles.go:262-267`).

The one real inequality left: a declared LXC member has its role written in two places and
applied; a ccy seat has one (the registry). That is the data model (ccy seats have no declared
role), not this branch.

## 2. Atomicity: the play-lock claim is wrong (F1)

`roles.Change` (`roles.go:108-120`) is: `Set` (writes the entry with `declare lxc-role`, then
`set-role` live) THEN `s.Apply`, which is `ops.ApplyTeams`. On this branch `ApplyTeams`
(`ops/ops.go:128`) takes no lock at all; the play takes the host lock and exits 75 when busy.

On `fix/teams-wait-play-lock` (8da095cb), `ApplyTeams` wraps only itself in `o.Locked`
(`ops/ops.go` there: `return o.Locked(ctx, "apply "+…, applyHeld)`), whose contract is "what fn
writes to the host_vars and the plays it runs happen in one hold… a person who stops the wait…
leaves nothing changed" (`ops/playlock.go:45-50` on that branch). The lxc-add path there obeys
it: `write` puts `declareAndApply` inside `a.Locked` (adder.go on 8da095cb). The roles path does
not: after the merge, a role change of a declared LXC member writes host_vars and the live role,
THEN waits up to 30 minutes for the lock; if the person stops the wait, `waitEnded` prints
"Stopped waiting for the play lock; nothing was changed." and `roles.Change` then prints that the
member "is a coordinator now, live and in the host_vars, but the apply … failed". Two
contradictory messages, the first false. So: a second locking scheme (apply-only), not a
deadlock (`Locked` is re-entrant through `o.held`, and lxc-add's nested `Roles.Set` has no
apply). The developer's sentence "its play lock, so the fix on fix/teams-wait-play-lock covers
this too" (report, SPEC-hot-control.md:531) is not true.

Apply failure state, on this branch alone: entry and live agree, the error says how to finish
(`roles.go:115`), `defer o.changed()` refreshes, and `declare.Status` compares the stamp with
the declaration (`declare/declare.go:247-250`), so the board shows the team out of date. Good.
`Change` returns `Done{}` on apply failure so a caller cannot undo an agreeing state: deliberate
and right.

## 3. Live notification

- **Not spoofable.** The role lines come from `survey()`, which reads the cached team record
  through `inbox.load_cached_record` under the `protocol.Untrusted` guard
  (`hooks.py:507-515`): the admin's state event, never a peer's message. The notice is built
  locally and written to the session's own socket with a nonce (`cli.py:2149-2150`,
  `envelope.render_membership`), so it is `kind: MEMBERSHIP` (asserted in
  `tests/helpers/pingbus/test_notify.py:304`). Nobody "sends" it.
- Watcher side: the `elif injector is not None` branch (`cli.py:2021-2026`) runs only for a
  session not following the membership, so a ccy session keeps `tell_membership` and an
  agent-bus-claude session gets `tell_roles`; `tell_roles` itself returns None for a membership
  session (`hooks.py:412-413`), a second guard. Both under `TOLD_LOCK` (`membership.telling`).
- Baseline at session start (`hooks.py:713`, `_role_lines(…, baseline=True)`) records the
  roles without saying them again: right, SessionStart already says them.
- The old-watcher caveat is in the queue description (`CLAUDE/Plan/meta-deploy.list`, the
  play-agent-bus.yml entry) and the developer's report.
- F4: `tell_roles` turns an unreadable `told-roles.json` into `previous = None` silently
  (`hooks.py:418-421`); the next change is then recorded, not told (tested as intended,
  `test_hooks.py:851`). A corrupt file losing a notice without a word on stderr is a silent
  failure; `_role_lines` writes a line for the other errors (`hooks.py:741`). One `err.write`.
- F5: `_tell`'s failure line says "the membership notice could not be built" for both tellers
  (`cli.py:2165`); name the teller.

## 4. `declare lxc-identity`

Probe is JSON the host's own root reader printed for a checkout path taken from host_vars
(`roles.go:300`), judged by `derive.parse_probe` (keys, sizes, printable text, owner;
`helpers/agent_bus_lxc/derive.py:462-480`); `identity_of` is the same rule the play uses
(`derive.py:524`). Not JSON, wrong keys, unreadable: `UsageError` 64 (`declare.py:1357-1361`);
the Go side refuses anything but exactly one `IDENTITY` row (`declare/lxc.go:91-100`).
Ambiguity: refused, naming both entries (`roles.go:262-267`, `roles_test.go:178`). No
install-specific name in the new tests (fixture hosts `local`, `desk`, `worker`, `box`,
`lab`; `workstation` is the pre-existing pingbus fixture).

## 5. Menu ordering

- `lastDestructive` (`picker/menu.go:91-100`) moves `remove`, `shutdown` and the new `demote`
  id to the end of every row's menu; `TestNoMenuOfAnyRowStartsOnADestructiveActionAndEachComesLast`
  (`picker/order_test.go:90`) judges the LINES by their words, independently of the ids, over
  every row with every holder: a test that cannot be fooled by a mis-classed id. Team rows:
  "Shut down", "Take down" last (`menu.go:116-117`). LXC row order is role, Element, session,
  lxc-add, help, Park, as the owner asked.
- Sessions menu: Remove last (`picker/sessions.go:140-144`); a coordinator's role question
  starts on "(now)" (`sessions.go:326`); settings lists No coordinator last
  (`settings/settings.go:503-505`); take-down starts on Stay (`ops/takedown.go:181`).
- Confirmations: `seats.ChangeRole` default No (`seats/role.go:39-40`); the hot takeover
  default No (`hot/role.go:86`). **F6:** the remove flow's "Make it a worker as it leaves (the
  role goes to no one)?" defaults to YES (`hot/role.go:300-301`, `x.ask(…, true)`). Pre-existing,
  but the branch's own rule is now "each asks first, default No", and demoting is on its own
  destructive list. One `false` and its test.
- "New password" not classed destructive: agreed, it is recoverable.
- `scripts/qa-hidden-bindings.bash`: HIDDEN-BINDINGS-OK.

## 6. Versions

- Container bump required: yes. The image's `pingbus` zipapp is bundled from `helpers/pingbus`
  AND `helpers/agent_bus` (`helpers/pingbus/bundle.py:39`), and `hooks.py`, `cli.py`,
  `membership.py`, `declare.py` all changed. 2.77.12 placeholder; Dockerfile label and
  `REQUIRED_CONTAINER_VERSION` agree.
- CCY bump: follows from the container bump (the required version lives in `claude-yolo`).
  Placeholder 3.99.12.15.
- Changelog present (`docs/ccy-changelog.md`, "3.99.12.15 — container 2.77.12"). Its "Follows
  3.99.12.10 (container 2.77.7)" is F44's current pin (0ce52a7e has 3.99.12.10 / 2.77.7);
  `fix/lxc-checkout-typed` takes 3.99.12.14 / 2.77.8, so whichever merges second renumbers and
  rewrites the "Follows" line.

## 7. Merge safety (`git merge-tree --write-tree`)

| Against                           | Result                                                                                                                                                                                                                                                                                                                                |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| origin/F44 0ce52a7e               | clean; merged queue is exactly Plan 00179 play-speech-to-text.yml, play-claude-yolo.yml, play-agent-bus.yml                                                                                                                                                                                                                           |
| fix/teams-wait-play-lock 8da095cb | CONFLICT meta-deploy.list, `teams/app.go`, `teams/lxcadd/adder.go`; plus the semantic one, F1                                                                                                                                                                                                                                         |
| fix/context-headings 7e6f2ca6     | CONFLICT meta-deploy.list, `teams/hot/hot.go`, `teams/ops/takedown.go`, `teams/settings/settings.go`; `Picker.Pick`/`Confirm` take a `ui.Heading` there: `seats.ChangeRole`'s `Kit.Confirm` and settings' `Kit.Choose` compile only once the kit is adapted on that branch (its own change), nothing new here calls `Picker` directly |
| fix/teams-full-handles 117a1026   | CONFLICT meta-deploy.list, `teams/picker/menu.go`, `teams/picker/menu_test.go`                                                                                                                                                                                                                                                        |
| fix/lxc-checkout-typed 90c02eed   | CONFLICT meta-deploy.list, JOURNAL day-file, PLAN.md, `docs/ccy-changelog.md`, Dockerfile, `claude-yolo`, `teams/lxcadd/adder.go`                                                                                                                                                                                                     |

None of the four is in origin/F44 yet. Every pair conflicts in `meta-deploy.list` (append-only
file); the resolution is the three-entry queue above with each branch's own entries kept.

## 8. Fail-fast, stderr, names, docs

- Go: no discarded error; every failure names the member and the step; the live-change lines
  go to the caller's `said` (hot marks them, the picker drops them: the Note says the result).
- Python: F4/F5 above. `_role_lines` catching `OSError`/`MembershipError` to keep the prompt
  hook alive matches `_membership_lines`; acceptable.
- `hot.Run` routes `role` to `memberRole` when the first word parses as a handle
  (`hot/hot.go:190`): a session name has no `+`, so no ambiguity.
- Docs: usage text, help text, SPEC, agent-bus-protocol §(told-roles) and the SKILL.md all
  updated and agree with the code. PLAN.md Task 3.8 added. Journal entry stamped by the tool.
- No install-specific names in the diff.

## Targeted QA run

- `go test -race ./internal/teams/... ./internal/declare/...`: pass.
- `scripts/qa-hidden-bindings.bash`: HIDDEN-BINDINGS-OK.
- `python3 -m unittest` over pingbus `test_hooks`, `test_notify`, `test_membership` and
  agent_bus `test_declare`: 406 tests, OK.
- `scripts/test-ccy-teams.bash` (PINGBUS\_\* unset): pass.
- `scripts/test-ccy-teams-command.bash` (PINGBUS\_\* unset): first run 370/371, one create-wizard
  case ("a bad name, then a good one") lost its `rc=` marker under load; untouched code, and a
  rerun passed 371/371. Flake, not this branch. F44 edb42b65 fixed a different gum-PATH flake in
  the same suite after the branch point; take it on merge.
- qa-go.bash's semgrep step not run (targeted only); no timeout observed.

## FIX-FIRST list

1. **F1 — one hold for the whole role change.** Give `roles.Setter` a `Locked` seam
   (`Locked func(ctx, purpose string, fn func(context.Context) error) error`; nil runs fn, so
   the fakes stay as they are) and make `Change` run `Set` and `Apply` inside it; wire
   `ops.Locked` in `seats.New`, `lxcadd.New` and `app.Wire` when `fix/teams-wait-play-lock`
   merges (whichever lands second does the wiring). Until then the seam is inert. Test: a fake
   `Locked` that records that the entry write and the apply fall within one call.
2. **F2 — the claim.** Reword the developer report and SPEC-hot-control.md:531 ("its play lock,
   so the fix on fix/teams-wait-play-lock covers this too") to what F1 makes true.
3. **F6 — `hot/role.go:300`:** the remove flow's demote question defaults to No, with its test.

Should-fix, not blocking: F3 (name the blocking entry and say to seat the entries), F4 (one
stderr line when `told-roles.json` is unreadable), F5 (`_tell`'s message names the teller).
