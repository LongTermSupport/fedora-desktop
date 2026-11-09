# Review round 2: ccy --teams with an unknown team, child exit codes, host seat KIND

Branch `agent-a8c69aa67110156ce-c48195fd`, `git diff F44` (working tree, part staged). Read-only
review: nothing was changed, staged or committed. The r1 review file was not read.

## What was run

- `go build ./...`, `go vet` and `go test` over `internal/teams/...`, `internal/term`,
  `internal/agentbus`, `internal/sessions`, `internal/ccylaunch/...` (the help difftest goldens
  included): all pass.
- `src-go/qa`: `go test ./rules/` passes. The project rule driver was built and run as
  `go vet -vettool=<driver> ./...` over `src-go`: no findings.
- `golangci-lint run` on chooser, term, board, seats, create, agentbus, ops and `qa/rules`:
  0 issues.
- `scripts/test-ccy-teams-command.bash` (383 passed, 0 failed) and `scripts/test-cc-teams.bash`
  (83 passed, 0 failed).
- `scripts/qa-bash-limits.bash` with base F44: OK. The launcher's help keeps its line count.
- Scan of the added lines for install-specific names: none found. The fixtures use `box`,
  `local`, `proj` and `example-org`.

## What holds

- `chooser.resolve` and `known`: a team whose homeserver is up, or that the host_vars declare,
  is taken as before. A name the team file refuses gets 64 with its reason (`declare check` is
  the `ErrUsage` kind). Otherwise the person is asked, at most `maxRounds` times. The settled
  refs and the remaining items are passed as the list as it stands. Esc and "Cancel" both end
  with 0 and an empty payload, and the launcher reads an empty payload as `exit 0`
  (claude-yolo, `ccy_launch_teams`). A run with no terminal never reaches `known`: the launcher
  calls choose-teams only when attended, `Chooser.Run` refuses a team named alone with 64, and
  `ccy_seats_check --resolved` stays fail-fast.
- The wizard runs the apply inside `RunForChooser`, so a team made there has its homeserver up
  before `Seat` is asked. `named` is reset on every run and only replaces the suggestion.
- `term.FromChild` and `Explain` work as documented. Explain runs after the echo hold is let
  go and before `Finish`. `InPicker` only gained cases the exhaustive linter needs, so its
  behaviour is the same.
- KIND: every old `Kind == KindCcy` site was a seat row, and seat rows were always `ccy`, so
  `IsSession` keeps their behaviour. The only rows whose behaviour changed are host member rows
  (`Dir "-"`, which were `host` before):
  - `SnapshotRunning` now asks for a folder.
  - `seatOwnActions` and the `Incomplete` case of `RemoveOrPark` treat a cc member row as they
    treat a podman one, as the catalogue says.
  - `Label` and `Crumb` check `Dir`, and `addActions` needs a `Record`.
- Seat handles come from `member.json`, which `parseMember` validates, so `KindOf` always has a
  type word to read.
- The rules match their catalogue text. `refusalkind` treats `FromChild` as handing the code on
  whole. The help in claude-yolo, in `help.txt` and in the three goldens is the same. The
  version is 3.100.23, above F44's 3.100.18, and the changelog lists 3.100.23 above 3.100.22.

## Findings

1. **medium: answering "No" at the wizard's "Create team X?" fails the whole launch with exit 1.
   It does not come back to the question.**

   - In the real wizard a decline at `confirmSummary` is `&stop{term.ExitFailure}`, and
     `create` returns nil only after `c.created` is set. So `RunForChooser` never returns
     `(\"\", ExitOK)`.
   - That makes `answerUnknown`'s `code == term.ExitOK && made == ""` branch ("Nothing was
     created; back to the choices.") dead code. The one decline a person really makes goes to
     `code != term.ExitOK`, then `leaveRun`, and ccy exits 1 after "Nothing was added.".
   - The person who said no to creating `nosuch` loses the other choices (start the rest, swap
     in another team), and the run ends as a failure. The changelog's "Leaving the wizard comes
     back to the question" holds only for Esc.
   - The tests do not show this because the fake `Create` can return `ExitOK` with "", which
     the real wizard never does.
   - Fix: in `forChooser` mode make the summary decline return `term.ExitCancelled`, or a
     dedicated "declined" result, and treat it like a cancel in `answerUnknown`. Delete the dead
     branch, or make it the decline's branch. Add a create-package test that drives
     `RunForChooser` to a "no" at the review and asserts the code. Also add a chooser test whose
     fake returns that code.
   - Decide whether the bare list's `created` (where a decline is also exit 1) should follow.

2. **low: docs/ccy.md now contradicts itself.** The paragraph still opens with "The launcher
   checks the list on the host (`agent-bus seat check`, … 78 for a team whose homeserver is not
   running …) before anything else runs". The new sentence then says an unknown team "is asked
   about first". When attended, `ccy_launch_teams` runs `choose-teams` before
   `ccy_seats_check`. Fix: say that on a terminal a team named alone goes to the chooser first,
   and the check (64/78) runs on the resolved list; headless, the check refuses at once. Also
   rewrap the long line.

3. **low: the changelog gives the host seat handle as `<team>.<n>+<host>.host`.** The grammar
   is `<repo>.<seat>+<host>.host` (docs/agent-bus.md:666; `KindOf`'s doc). Fix the placeholder.

4. **low: `teams/usage.txt:266` still lists `host` as a kind** ("a member of any kind (ccy, cc,
   LXC, host, docker, vm)"). The board no longer shows `host`: a host member is `cc`. The
   `picker.memberActions` doc comment has the same list. Fix: drop `host` from both lists, or
   say "cc (host)".

5. **low: the `changed-plays` description in `CLAUDE/Plan/meta-deploy.list` still says CCY
   3.100.18** and does not mention this branch's change. Task 7.9 says the host run is CCY
   3.100.23 with the unknown-team offer. The entry picks the play up anyway, but the comment is
   how the owner learns what the run does. Fix: update it to 3.100.23 and name the unknown-team
   offer, the documented exit codes and the board KIND.

6. **low: the DBF report gives two counts for the same tests.** It says "the chooser's unit
   tests (16)" in its owner decisions and "`chooser/unknown_test.go` (12 tests)" in "The
   original defect". The file has 16 `Test` functions. Fix: say 16 in both places.

7. **low: a gap in `project_child_exit` that the catalogue does not record.** A local declared
   with `var code = de.Code` (an `*ast.ValueSpec` inside an `*ast.DeclStmt`) is not tainted;
   only `AssignStmt` is followed. So `var c = ee.Code; return term.ExitCode(c)` passes. Fix:
   follow `ValueSpec` with values too, and add a `bad.go` case. Otherwise, add it to "Not
   caught" in the doc string and in GoProjectRules.md.

8. **low: the unknown-team question drops options that do not apply instead of showing them as
   unavailable.**

   - When this machine has teams but none is up, "Choose one of this machine's teams instead"
     is simply missing, so the person is not told why it is not offered.
   - F44's menus keep such a line and say why. That convention is written for the board's
     menus, so this is a consistency point, not a rule breach.
   - Fix (optional): show it as "…: unavailable, no other team's homeserver is up" and answer
     it with that note and a re-ask.

9. **low: the `--teams` help lists exits 64, 75 and 78 but not 1.** 1 is what the 3-tries limit
   and a failed or declined wizard give. Fix (optional): "1 failed or declined" beside them,
   within the same line count.

## Verdict

One medium finding (1) remains. The rest are low and can be done in the same pass.

VERDICT: CHANGES
