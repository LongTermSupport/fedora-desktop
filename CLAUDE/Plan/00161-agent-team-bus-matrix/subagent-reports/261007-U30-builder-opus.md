# U30 builder report: seat claim, status and hooks

Branch `seats/U30`, created from `origin/seats-integration` (U29 on top of the seats
design). The commit is the branch commit whose message starts "Plan 00161: U30".

## What was built

| File                                                                              | Change                                                                                                                                                                                                                                                                                                                                                                                  |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `helpers/pingbus/seat.py` (new)                                                   | `parse_seat_list` (section 5.5's rules, `SeatListError` is a `UsageError`, 64) and `canonical`; `SeatRef`; the layout (`CHECKOUT_SEATS`, `CCY_SEATS_ROOT`, `checkout_seats_root`, `LOCK_FILE`); `list_seats` (the directories are the record; anything else in the tree is refused); `seat_lines` (`SEAT` lines, a failure per unreadable seat); `claim`; `session_seats` for the hook. |
| `helpers/pingbus/inbox.py`                                                        | `acquire_lock_at` / `probe_lock_at`: the one lock primitive on a path; `acquire_lock` / `probe_lock` are now thin wrappers for the sync lock. `SEAT_KIND`, `PATH_LOCK_KINDS`; the kind is the file's first word, so `seat <ms>` reads as `seat`. `Lock.fileno`, `Lock.set_inheritable`.                                                                                                 |
| `helpers/pingbus/config.py`                                                       | A symlinked `PINGBUS_HOME/<team>` is accepted only onto a real directory (not a link) owned by the user, mode exactly 0700; a plain directory is unchanged. `read_member` (member.json without the token).                                                                                                                                                                              |
| `helpers/pingbus/cli.py`                                                          | `pingbus seat exec [--] CMD…`; `SEAT` lines after the teams in `status`; `Runtime.seats_root`, `.session_home`, `.execvpe`; exit 75's meaning names a held seat.                                                                                                                                                                                                                        |
| `helpers/pingbus/hooks.py`                                                        | `NAME_FIELDS` and `fill` (grammar-checked names in templates); `seat_self`, `seat_sibling`, `seat_history` templates at SessionStart; `seat_held` reworded ("another process of this seat"); an unreadable sibling is the `state` failure.                                                                                                                                              |
| `files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md` | A `## Seats` section: own handles, siblings, taking over a seat, who cannot be pinged.                                                                                                                                                                                                                                                                                                  |
| `docs/agent-bus-protocol.md`                                                      | §12 the link rule, "Seats" (layout, `seat.lock`, `PINGBUS_SEATS`, the session home), `PINGBUS_HOME` for a ccy session; §13 `seat exec` row and `SEAT` in `status`; §14 75 names a held seat; §15 report lines and `SEAT`, hook outputs may carry checked names.                                                                                                                         |
| `DESIGN.md`                                                                       | D52-D54.                                                                                                                                                                                                                                                                                                                                                                                |

`seat.py` is picked up by both zipapps unchanged (`bundle.collect` walks the trees);
checked by building both archives and finding `helpers/pingbus/seat.py` in each.

## Tests (seen failing first, then passing)

- `test_seat.py` (new, 40): the list rules table (whitespace dropped; empty list, empty
  item at each position, trailing-comma hint, exactly one `@`, bad seats and teams, a team
  named twice with two seats or the same one, an unprintable item not echoed; the canonical
  form keeps order); `list_seats` (the directories, refusals for non-seat entries and
  symlinks); `SEAT` lines (held by another real process, free once it exits, `self` via the
  session home's link, a broken seat reported while the others print); the claim through
  `cli.main` (every seat claimed, the command exec'd with `PINGBUS_HOME`, `PINGBUS_TEAMS`,
  `PINGBUS_SEATS`; `seat <ms>` in each lock; inheritable descriptors; a 0700 home with one
  link per seat; an existing home 78; missing or empty `PINGBUS_SEATS` 78; malformed lists
  78 not 64; a seat with no directory 78 naming `ccy --teams`; one of two seats held by
  another real process 75 with the first released and no home left; a 0750 seat directory
  78; a missing bundle naming `ccy --teams qa2@team-b`; a bad forge token file 78; no
  command 64); two real-exec tests (the locks held through `exec` by the session and a
  child, free once the last exits; a detached grandchild keeps the seat until it exits);
  the protocol doc's §12-§15 phrases.
- `test_config.py`: the link rule (accepted onto a private directory, relative link,
  refused onto a link, a foreign owner, 0750/0705/0755, a file or nothing; a plain 0755
  bundle directory still accepted); `read_member`.
- `test_inbox.py`: `LockAtPathTest` (kind and claim time, probe without creating, busy with
  the holder's kind, unknown kinds and negative times refused, symlink refused,
  `set_inheritable`); the sync lock refuses the `seat` kind.
- `test_cli_status.py`: `SEAT` lines with `self`, held and free, after the teams; a broken
  seat is 78 with the rest printed; no seats directory, no lines. (The status code was
  written with `seat exec` before these tests ran; they were then seen failing with the
  lines disabled and passing with them restored.)
- `test_hooks.py`: the template regex now knows each field's grammar; names are filled
  only after their grammar; `seat_held` no longer suggests another session; SessionStart
  names the own seat, a held and a free sibling, the history pointer for consumed or sent
  traffic only, nothing for other teams or a plain bundle, an unreadable sibling as the
  `state` failure, offline.
- `test_plugin_contract.py`: the skill's seats section and its phrases match the hook
  templates.
- `test_cli.py`: `BusCase.runtime` points `seats_root` into its temporary directory, so no
  status test reads the machine's own `/workspace/.claude/ccy/pingbus/seats`.

Runs: every `tests.helpers.pingbus.*` and `tests.helpers.agent_bus.*` module OK; the plan
folder's `test_acceptance_check`, `test_u20_check`, `test_deploy_check`,
`test_triage_probe`, `test_u01_probe` OK. ruff 0.16.8 clean on every touched Python file.
No bash was touched.

## Decisions (D52-D54)

- **D52** `SEAT` sits with the report lines: §13 defines it with `status`, §15 describes it
  in prose. §15's table is held equal to `cli.LINES` (the versioned item lines), so a
  `SEAT` row there would break that contract.
- **D53** The claim checks the forge credential source first (as `config check` does),
  releases its locks and removes the home it made on any failure, names `ccy --teams` for
  a seat with no directory or no `member.json`, and writes `seat <ms>`. A linked team entry
  needs mode exactly 0700.
- **D54** Hook templates carry grammar-checked names; the seat lines come from the session
  home's links (no new path or variable); "earlier traffic" is a non-empty `consumed/` or
  an existing `outbox.json`. The skill does not name `pingbus history` as a command, because
  U33 adds it and the skill's contract test parses every `pingbus` invocation; once U33 is
  merged, the skill's "Taking over a seat" bullet can name `pingbus history` directly.

## For the coordinator

- No change under `files/var/local/claude-yolo/`, so no CCY or container bump here. The
  new pingbus reaches the image when `play-claude-yolo.yml` rebuilds the zipapp; U31's
  entrypoint change (and its bumps) is what puts `seat exec` in front of the final `exec`.
- U32 should use `seat.parse_seat_list`, `seat.canonical`, `seat.checkout_seats_root`,
  `seat.list_seats`, `seat.seat_state` and `seat.seat_lines(root, None)` for `seat list`;
  `SeatHeld` is the 75 refusal.
- `cli._Seat` (the per-team run state of the network commands) predates seats and keeps
  its name; `cli.py` imports the new module as `seating` to keep the two apart.
- No host run belongs to U30 alone: U20's run exercises it.
