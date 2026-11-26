# Task 4.2 handoff: what is left after part two

Part one is commits `2af1ca078` and `d88d06df4`; part two's first commit holds items 1 and 2 below
(report: `subagent-reports/261126-task-4-2-build.md`, "Part two"). The Go rule
`project_seat_number_text` and the gate `seat-number-text` (`src-go/qa/seattext`, command
`qa/cmd/qa-seat-text`, `scripts/qa-seat-text.bash`) share one `seattext.Finding`; both are
blocking and green. `seatchoice.Pick` chooses a team's seat by its last run; the chooser's bare
list takes the one team this checkout has seats in (`chooser.seatedTeam`).

## Done in part two

1. Text check, red at 47 findings, swept to green; 10 allowlist rows (agent-facing, v1 and
   `ccy-teams internal` lines, each with its reason). Wired: `qa-gates.tsv`, `qa-routing.tsv`
   (and the perfile-gate list), `qa/changed/checks.go`, its tests, `addedAfterPort`, the captured
   wording, SPEC-qa-changed B70 and its 25 pinned scenarios, `CLAUDE/QA.md`, GoProjectRules.
2. Bare `ccy --teams` takes the one team with seats here (`TestABareListTakesTheOneTeam...`).

## Left, in order

3. **Plain `cc`** (Q1, decided yes): in a checkout holding a host seat, join it; `cc --no-teams`
   opts out (`files/var/local/claude-code/cc-teams.bash:54-74`, bash on the ratchet: the logic
   belongs in Go, `ccy-teams choose-teams --type=host`).
4. **Unattended pick**: `chooser.go:104` refuses every unattended team named alone. Use
   `seatchoice.Pick` first, and refuse, in words, only a genuine choice. Then update
   `docs/agent-bus.md` "A ccy session: seats" (its last sentence says the refusal stands) and
   the launcher message at `claude-yolo:887`.
5. **R89 restore seat pass** (§3): `ccy-sessions restore` with `--dry-run`. `restore.go:549`
   still prints the replayed argv with the seat.
6. **Session names by role** (Q7): `ccy-<team>-<project>-<role>` where the role is unique; then
   `docs/ccy.md` "A launch with `--teams` names its session" and the two allowlist rows for
   `ccy-teams internal session-name` in `src-go/qa/seattext/allowed.tsv`.
7. Rows still lack the conversation's first prompt (Q5) and the seat's role. A seat's own model
   (`seats model`) is still set by seat for scripts; a person has no way to set it in words.

## Notes

- `qa/rules/rules_test.go` `TestAllIsValid` counts the rules; re-read it at HEAD before a commit.
- A new `qa-changed` check needs a SPEC-qa-changed B number and `-update-deviations` for every
  golden scenario it joins (`untracked/scratch/devb70/` was the one-off used for B70).
- Other agents share this worktree. Commit only your paths through a temp `GIT_INDEX_FILE`.
