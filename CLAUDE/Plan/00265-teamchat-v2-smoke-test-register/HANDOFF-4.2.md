# Task 4.2 handoff: what is left after part three

Parts one and two: `2af1ca078`, `d88d06df4`, `ce6b1e9b1`. Part three (report:
`subagent-reports/261126-task-4-2-build.md`, "Part three") holds items 3 and 4, the dry-run half
of item 5, and the fix of the `test-ccy-teams-command.bash` picker case part one broke. The Go
rule `project_seat_number_text` and the gate `seat-number-text` share `seattext.Finding`; both
are blocking and green. `seatchoice.Pick` chooses a team's seat by its last run, at a terminal
and without one (`chooser.unattended`, `--attended=false` from the launcher); plain `cc` asks
`choose-teams --type=host --seated` (`chooser/seated.go`).

## Left, in order

5. **R89 restore seat pass** (design §3): after the session records, `ccy-sessions restore`
   walks `seat-checkouts`, and starts `ccy --teams <team>` (or `cc`) detached in each checkout
   whose pool seat has a conversation no running session holds and no restored record covers;
   names each one not started with its reason. `--dry-run` already speaks in words
   (`sessions.wouldStart`); the seat pass's own lines follow design §3's two examples.
6. **Session names by role** (Q7): `ccy-<team>-<project>-<role>` where the role is unique in
   the checkout (`src-go/internal/teams/sessionname`, N1); then `docs/ccy.md` "A launch with
   `--teams` names its session" and the two allowlist rows for `ccy-teams internal session-name` in `src-go/qa/seattext/allowed.tsv`. Needs a role lookup wired into
   `sessionname.Command`.
7. Rows still lack the conversation's first prompt (Q5) and the seat's role
   (`seatchoice.Row`). A seat's own model (`seats model`) is still set by seat for scripts; a
   person has no way to set it in words.

## Notes

- `qa/rules/rules_test.go` `TestAllIsValid` counts the rules; re-read it at HEAD before a commit.
- `cc-teams.bash` and `cc` are at their bash-limits ratchet records: anything added there needs
  as much taken out (part three moved `cc_teams_ccy_dir` to Go for room).
- Other agents share this worktree. Commit only your paths through a temp `GIT_INDEX_FILE`.
