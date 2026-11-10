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
