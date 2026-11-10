# Stale seat takeover, and a seat's member type (SPEC A15, Defence Before Fix)

## The suspected `--type=podman` defect: confirmed, fixed as a class

`picker.askSeat` (Add to a team, and the team menu's add) asked `agent-bus seat choose <team> --type=podman` for every session. agent-bus offers only seats of the asked type
(`helpers/agent_bus/checkout.py` `seat_choices`: "Only seats of the identity's type are
offered"), and `take_actions` refuses an existing seat of the other type. So a cc session was
offered ccy's podman seats, which `session add` (hot control asks with the session's own type,
`host`) then refused, and never its own host seats.

- Class: an agent-bus seat call for a session given a member type fixed in the code.
- Detector: `project_seat_member_type` (`src-go/qa/rules/seatmembertype.go`): a call to the
  agentbus client whose `memberType` argument is a compile-time constant, outside agentbus.
  Blocking: it is in `rules.All`, which the Go gate runs.
- Red: the fixture `testdata/src/fix/internal/badmembertype`; the real tree, two findings:
  `picker/sessions.go` `askSeat` (the defect) and `chooser/consent.go` `handles` (`typeHost`,
  correct only because its one caller is a host launch).
- Sweep: the module and the `ccyteamstest` and `busfailpoints` builds; those two were all.
- Fixed: `handles` and `consent` take the launch's own member type; `askSeat` asks
  `hot.MemberType(s.Kind)`, the one kind-to-type mapping, which hot control's own seat calls
  now use too. `TestAddOfACcSessionOffersItsHostSeats` was red first.
- Catalogue: `CLAUDE/GoProjectRules.md` `project_seat_member_type`.
- Commit: `8e7ce9cee`. Its message is wrong: a stale #182 message left in a scratch file was
  committed in place of this one (the content is right). Reported to the coordinator; amending
  was not this seat's to do.
- Also open, not this class: `ccy-teams session add <s> <team>/<seat>` given an EXISTING seat of
  the other type is not refused by `pickSeat` (H22 step 3 does not compare the seat's handle
  type; `seat take`, which would refuse it, runs only for a new seat). The picker's seat add
  cannot reach it (A12 refuses another kind). What happens past that point is not traced.

## The takeover (owner decision 2026-11-10, SPEC A16)

- A seat that a stopped, restore-after-reboot record names, and that no live process holds,
  now has an available Add a running session to this seat. The add has three steps: 1/3
  Session, 2/3 Role, 3/3 Take over. The last is a confirm that starts on No: "`<old>` is set
  to restart into this seat after a reboot; attaching `<new>` stops that."
- Yes turns the old record's restore off through `seats.SetRestore`. That is the code the seat
  menu's Stop starting its session again after a reboot (`ToggleRestore`) now calls too, so
  nothing is forked. Then the add runs.
- No or Esc changes nothing. A restore that cannot be turned off adds nothing. An add that does
  not finish turns the restore on again; if that also fails, the error names the menu line that
  turns it on.
- A seat a live process holds is still refused (A15).
- H22 needs no change, because it counts only restored records. `hot`'s
  `TestAddTakesASeatOnceTheRecordThatRestoresItIsTurnedOff` shows the exact sequence: the add is
  refused, the restore is turned off, the add then takes the seat.
- One behaviour change in `ToggleRestore`: a `SetRestore` failure now ends on "Nothing was
  changed." (the registry-not-found case already did). It used to go straight back to the list.
  `SetRestore` writes nothing when it fails.
- Tests, red then green: `addrunning_test.go` (the menu line, the takeover, cancel by No and
  Esc, a failed add, a restore that cannot be turned off, a restore that cannot be put back).
  Picker coverage is 90.1%, against a 90% floor.
- Docs: SPEC A15 and A16, `docs/agent-bus.md`, the picker help, a JOURNAL entry.

## For the owner

- The old record still lists the seat in its `--teams`. If that session is started by hand
  later, its launch finds the seat held and refuses it, as it would for any other held seat.
- Deploy: `ccy-teams` is built by `play-claude-yolo.yml`, which `meta-deploy.list`'s
  `changed-plays` already covers. Nothing under `files/var/local/claude-yolo/` changed, so
  there is no CCY version bump.
