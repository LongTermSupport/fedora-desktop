# Task 4.2 build, part one (R90): the detector, its sweep, and the seat a launch picks

## Scope against the design

The lead's scope for the detector, "A+rows", matches the design (§1 and the decided Q2): a seat
shown as the thing a person types or picks is flagged everywhere (messages, help, errors, rows);
a row labelled by its seat is flagged; a line naming a seat for diagnosis is not, nor keeper logs
or agent-facing text. One difference: the design names human *sinks* (`say`, `Refuse`,
`Gum.Choose` rows ...); the rule judges the *shape* of every string in the human-facing trees
instead, since one sink list would miss each new helper. Rows are found by the function that
builds them (name ends in row, rows, choices, options, label).

## Built

- **Rule** `project_seat_number_text` (`src-go/qa/rules/seatnumbertext.go`, registered in
  `rules.All`, so blocking). Fixtures in the `fix` module: `testdata/src/fix/internal/teams/ badseattext` (10 findings), `cleanseattext`, `outsideseattext`. Catalogue:
  `CLAUDE/GoProjectRules.md`.
- **Red, real tree** (`go vet -vettool` over `internal/{teams,sessions,conversation,ccylaunch, registry}`, `cmd/ccy-{teams,sessions}`): 41 on the first draft. Three shapes there were
  diagnosis and were taken out of the rule: a record's `--teams` step line, a `hand-to=` value
  echoed back, and `shutdown`'s `v2Label`, which has an allowance row. That left 35. Adding the
  filled-list repair shape found 1 more (`conversation/elsewhere.go`). **Green: 0.**
- **Sweep, every instance fixed:** `seatchoice` (Row, WaysOn, WrongKind), `chooser` (teamchat
  seat question, the unattended line, consent and launch-summary input errors, summary rows),
  `choose` (v1 rows, the no-gum line, describe-seats), `seats` (new-session rows and header),
  `seatadd`, `seatmodel` (usage, `--host` error), `membership` (parse error), `teamchatlaunch`
  (launch, seat-check, the sync line), `hot` (help, hand-over lines in move and role), `picker`
  (seat label), `seatconversation`, `seatresume`, `conversation` (`WaysOn`, `parseList`,
  `elsewhere`).
- **The fix (R90, step 2 for `ccy --teams <team>` / `cc --teams <team>` at a terminal):**
  `conversation.SeatLastRan` (the seat record's time while its transcript is here),
  `Teamchat.LastRan` wired in `teams/app.go`, `seatchoice.Pick` and a new `Default`. The one
  free seat with a conversation is taken and said in words. With none ever run and none held, a
  free seat is taken. Two seats with a conversation, or a held seat, are asked in words (`A seat of <team>: last ran 2 hours ago`, `Running now in session <s>: re-attach it`), the newest the
  default. `membership.Launch` and `membership.Teams` turn a seat list into the launch by team.
  `seats conversation` takes a team and finds its one recorded seat (`conversation.SeatsOfTeams`).
- **#430 follow-ups:** (a) `TestEveryTeamsBusIsAskedAtOnce`: each fake probe blocks until every
  team has been asked. (b) `system.Answers` with no pool seat says "no pool seat of it was found
  in this checkout, so its bus was not asked". The chooser prints Answers' words as given
  (`TestATeamWithNoSeatHereIsNotCalledDead`).

## Tests run

`go test` on `internal/teams/{seatchoice,chooser,choose,seats,seatadd,seatmodel,membership, teamchatlaunch,hot,picker,system}`, `internal/teams`, `internal/conversation`, and `qa/rules`
(`TestSeatNumberText`): all pass. `golangci-lint` on `./internal/teams/...`,
`./internal/conversation/` and `qa/rules`: 0 issues. No full QA was run.

## Left (Task 4.2 stays in progress)

1. Step 1's text check, `src-go/qa/seattext`, over `docs/**/*.md`, `usage.txt`, `help.txt` and
   the launchers' messages. The design's sweep list for it is still unfixed: `teams/usage.txt`,
   `ccylaunch/help.txt`, `claude-yolo:243,247,752,887`, `cc-teams.bash:66` and the docs.
2. Step 2's other half: a bare `ccy --teams` taking the checkout's one team; plain `cc` joining
   its seat (Q1, with `cc --no-teams`); the unattended pick (restore or no terminal deciding
   every case but a genuine choice). Rows still lack the conversation's first prompt (Q5) and
   the seat's role.
3. R89's restore seat pass and `restore --dry-run` in words. `restore.go:549` still prints the
   replayed argv; the rule cannot see it.
4. Session names by role (Q7). `seats model` still takes a seat for a seat's own model, though
   its usage no longer shows it. Asking in words for that is not built.
5. A design question: the `hand-to=` flag stays for scripts, and people are pointed to the
   picker's Change role instead.
