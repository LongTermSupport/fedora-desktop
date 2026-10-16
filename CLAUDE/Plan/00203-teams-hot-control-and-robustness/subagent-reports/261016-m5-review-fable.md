# Plan 00203 M5: Fable review of `feat/hot-control-m5` (aa3c476f on F44 171e0d41)

Verdict: **FIX-FIRST**, for one blocking finding in the deploy queue. The code is MERGE-grade.

`go test ./internal/teams/...` passes (19 packages), `go vet` is clean, `golangci-lint run ./internal/teams/...` reports 0 issues. Read-only review; nothing was run on the host.

## Blocking

### B1. `CLAUDE/Plan/meta-deploy.list` carries a queue F44 has emptied

`CLAUDE/Plan/meta-deploy.list` lines 22-121 on the branch. The branch was cut at 171e0d41, whose
queue held the M3/M4 deploy (play-claude-yolo, play-agent-bus, 00203, 00201/acceptance.bash,
00161/acceptance.bash). F44 342fae6f ran that deploy and removed all five entries. The branch
keeps them and appends two more, so after a merge the queue would be seven entries, five of them
re-running a deploy that is done (two of whose acceptances are known to fail at M2 (c) and
00161 M2.10 for reasons M5 does not touch). The developer's notes say the old entries are "left
for the coordinator to clear"; the standing rule in CLAUDE.md is that the list is exact in the
same commit.

Fix: merge F44 into the branch (or rebase), and leave exactly two entries after the 21-line
header, in this order, each with its comment:

1. `playbooks/imports/play-claude-yolo.yml`
2. `00203-teams-hot-control-and-robustness`

Also correct the second comment. It says the run "re-proves `ccy-teams session list` (M4 (e))".
Per the F44 journal (06:57), the 00203 acceptance stops at M2 (c), so M1, M3 and M4 never run
until the (c) fix lands; until then the entry re-proves nothing about M4 (e). Either say the
acceptance is queued and that (c) must be fixed first, or drop the M4 (e) claim.

## Not blocking

### N1. Add as coordinator with the roster unread exits 64 instead of asking again

`src-go/internal/teams/picker/sessions.go:258-281` (`addToTeam`). When `r.snap.Members[team]`
is unread (sudo refused, homeserver down between the board pass and the question), `held` is
false, so the picker omits `--demote-current`; if a coordinator does exist, `session add` refuses
with "add --demote-current" (exit 64) and the picker prints "(... failed, exit 64: see above.)"
with no re-prompt. The command line behaves identically, so the "same code path" claim holds;
but the picker could offer the retry InteractiveScripts.md asks for. Low priority: the team
question already refuses a team whose homeserver is down, so the window is narrow.

### N2. The command's confirmation does not name the demoted coordinator

`src-go/internal/teams/hot/add.go:55-62` and `role.go:126`. The picker's option line discloses
the demotion ("coordinator (alpha's is <handle>; it becomes a worker)"), so the choice is not
silent; but the one yes/no the command then asks says "..., coordinator?" or "Make <handle> ...
coordinator?" without the holder's name. The M4 command owns that question. Worth a follow-up
in M7 or a later fix: the question should say who becomes a worker.

### N3. Untested flows

`src-go/internal/teams/picker/sessions_test.go` drives the picker through the repo's scripted
fzf/gum console and a `fakeSessions` double, as the existing picker tests do, and asserts the
exact argv; `app_test.go` proves the double is wired to the real `*hot.Hot`. Not covered:

- `moveTeams` with the leaving seat the coordinator (`askCoordinator` on the move path);
- Esc inside a question ("leaving the question changes nothing": no `Run`, no refusal counted);
- `askSeat` with `SeatChooseIn` failing (the Refuse path at `sessions.go:313`);
- the Running sessions view when `len(s.Teams) == 0` and Ctrl-X/Ctrl-O ("is on no team").

None of these is a correctness doubt from reading the code; they are the picker's own branches
and cheap to add.

### N4. Spec 5.3 wording

The spec's remove screen offers "hand it to seat 5 (<handle>, held by <session>)"; the build
offers "Hand it to <handle>". The handle is what `--coordinator=hand-to=` accepts, so this is
fine, but the seat number is what the roster table shows the person, and it is one map lookup
away (`r.snap.Rows`).

## Answers to the brief

1. **Spec and H items.** 5.1 (the view, Tab, the H35 line once, state words), 5.2 (team, seat,
   role; the refused team reasons; the coordinator line), 5.3 (the coordinator question on
   remove), Repair only when unguarded (H10), attach through `seats.AttachSession` with a seat
   attach's checks. Met. cc sessions (5.1's `cc-<id>` row) are deferred to M6, consistent with
   the milestone table (M6 is cc parity). The picker's own 3-tries rule and the picker-wide
   refusal tally both apply.
2. **Same code path.** True. `Picker.Session` is the `*hot.Hot` that `ccy-teams session`
   dispatches to (`app.go:109`, proved by `TestThePickersRunningSessionsAreTheSessionCommand`).
   Every flow ends in `r.session(ctx, args)` -> `Hot.Run`, which parses the argv with the
   command's own `parse`, takes the change and allocation locks in `resolve`, asks `confirm`
   (the picker never passes `--yes`; `interactive()` is true on the picker's terminal), and runs
   the same step and rollback code. No picker path writes a record or a desired file.
   **Argument safety.** The argv is a Go `[]string`, no shell. Session names come from
   `runningSessions`, which keeps only names matching `membership.ValidSession`
   (`^[A-Za-z0-9_]...`, no leading `-`); team names come from the board's configured teams and
   must match `teamPattern` (`[a-z][a-z0-9-]{0,23}`: no spaces, no leading dash); seats come
   from `agent-bus seat choose` and are re-validated by `ParseSeats`; the hand-to handle must
   match `handToValue`; `parse` accepts only the verb's whitelisted option names and enforces the
   word count, so a word that did begin with `-` would be a usage error, not a flag. No option
   injection.
3. **InteractiveScripts.md and UI conventions.** A refused answer is said and the same question
   asked again, 3 times, then "Nothing was changed after 3 tries." (rule 1, 2, 9). Esc in a
   question is a clean no-change exit (rule 3). The view reuses `Kit.Choose`, `Refuse`, `Note`,
   `BackToList`, the `maxNoOps` tally, the `action`/`option` menu shape and the header style of
   the teams list; `Tab` is in both key lines and in the help and usage text.
   **Terminal state.** fzf runs with `--height=~70%` (no alternate screen), so a `Tell` before
   a re-ask stays visible above the next window. Ctrl-C: `Picker.Run` wraps the context in
   `signal.NotifyContext`; in fzf it is `ErrLeave` -> view back to teams -> `Interrupted` ->
   `Cancelled`; in the command's gum question it is `ExitCancelled` -> `InPicker` ->
   `Cancelled`; mid-step the command returns its failure after its own rollback, `BackToList`'s
   `readLine(ctx)` returns on the cancelled context, and `once` leaves on `Interrupted`. Same as
   a Ctrl-C on the command line.
4. **The five decisions.** Tab/Esc, menu order, no seat question on move, no M5 acceptance
   script, cc deferred to M6: all within the spec's own wording (5.4 shows `move` with no seat;
   the M6 row owns cc). None needs the owner. Choosing coordinator over a current one is not
   silent: the option line names the holder and says it becomes a worker, and the command still
   asks its yes/no; N2 notes the question could name the holder too.
5. **Tests.** Fakes, as the repo does: `teamtest.Interactive` scripts fzf and gum, `fakeSessions`
   records the argv, no pty. 14 new picker tests, 3 hot, 1 seats, 1 app. Gaps in N3.
6. **Versions.** Only `src-go/` and docs changed. `ccy-teams` is built by
   `tasks/build-go-command.yml` from `play-claude-yolo.yml:857`; the pre-commit gate
   (`scripts/git-hooks/pre-commit:125-126`) watches `files/var/local/claude-yolo/claude-yolo`
   and `lib/` only. No CCY or container bump is due. Correct.
7. **meta-deploy.list.** B1.
8. **Install-specific names.** None in the added lines. Test fixtures use `/home/u`,
   `proj.*`, `local`, `192.0.2.10` (reserved example range).
