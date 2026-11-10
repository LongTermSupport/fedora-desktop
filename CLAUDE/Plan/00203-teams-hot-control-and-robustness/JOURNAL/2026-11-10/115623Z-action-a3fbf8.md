## 11:56 · action · —   — DBF: a seat's member type fixed in the code

Defence Before Fix on the suspected `--type=podman` in the picker's `askSeat`: confirmed a defect.
`agent-bus seat choose` offers only seats of the asked member type (`seat_choices`), so Add to a
team offered a cc session ccy's podman seats, which `session add` then refused, and never its
host seats. Class: an agent-bus seat call for a session given a constant member type. Detector:
`project_seat_member_type` (`src-go/qa/rules/seatmembertype.go`), red on its fixture and on the
real tree (`picker.askSeat`, and `chooser.handles`' constant `typeHost`, correct only by its
caller), blocking through `rules.All`. Fixed: `handles` takes the launch's own type; `askSeat`
asks `hot.MemberType(s.Kind)` (`TestAddOfACcSessionOffersItsHostSeats`, red first). Catalogue:
CLAUDE/GoProjectRules.md. Report:
[subagent-reports/261110-stale-seat-takeover.md](../../subagent-reports/261110-stale-seat-takeover.md).
