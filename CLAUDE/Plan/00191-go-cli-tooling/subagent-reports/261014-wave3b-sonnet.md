# ccy-teams Go port, wave 3B: the interactive picker

## Built

- `internal/teams`: `picker.go` (start-up, header, loop, `once`, sudo pass, `choose`/`confirm`/`inPicker`),
  `keys.go` (`act`: key and kind to flow), `offers.go` (create, path limits, update, join here, `backToList`),
  `session.go` (attach, Ctrl-N, Ctrl-R, Ctrl-X remove or park), `password.go`, `element.go`, `help.go` + `help.txt`,
  `ops.go` (`teamOperator` default), each with its `_test.go`.
- `app.go`: `teamOperator` interface, App fields `Gum`, `Picker`, `Ops`, `LookPath`, `OpenTTY`, `Exec`, and `wireAsking()`.
- `internal/ui`: `PickSpec.Tags` / `Picked.Tag`: a hidden per-row tag (`--delimiter=<TAB> --with-nth=1`, appended after
  the legacy flags) so identical rows are distinct (bug 20). The fake fzf returns the full line, so the suite is unaffected.
- `table.go`: `kindTeam`, `kindCcy`, `kindHuman` constants (used by `collect.go` too).

## Decisions on the section 8 bugs

- 2: the header and Ctrl-N name the checkout top, not `$PWD`.
- 3: a failed Element launch is an explicit `leave(1)`.
- 4: kept, narrowed. Three refused attempts in a row end the picker (`Nothing attached or started after 3 tries.`).
  Only refusals count (no session to attach, taken elsewhere, not a project, homeserver down, seats unreadable, no Element
  profile set up). A person calling a pick off does not count, and any other key that does something resets the tally.
- 5: Enter on a ccy member with no known checkout says so, in terms of the member.
- 12: `(ccy-teams apply --all failed, exit N: see above.)`, never `cmd_apply`.
- 13: `backToList` after every message that fzf would redraw over (Ctrl-R, Ctrl-X notes and results, Ctrl-P on a non-person,
  Enter on a row with no action, "That row has changed", attach and Ctrl-N refusals, park, seat remove).
- 17: `agentbus` has no `seat choose`, so the count is kept but restricted to the repository: only members whose handle starts
  with this checkout's directory name count, plus this checkout's own numbered seats.
- 20: tags, above. 21: `q:abort` kept (ui picker unchanged); documented in the shared picker.

## Not decided here

- Human rows carry the sign-in count in `Applied` (wave 3A) where the bash script has it under SESSION; the picker reads
  `Snapshot.SignedIn`, so it does not depend on the column.
- `Declare.Element` output (the CHANGED line) is not echoed.
- App fields `Gum`, `Picker`, `LookPath` and `wireAsking` may collide with the wizard builder's; merge by keeping one.
