# Task 4.2 handoff: what is left after part one

Part one is commit `2af1ca078` (report: `subagent-reports/261126-task-4-2-build.md`). The Go rule
`project_seat_number_text` is blocking and green. `seatchoice.Pick` and `Default` choose by
`Seat.LastRanMS`, which `chooser.poolOf` fills through `Teamchat.LastRan`
(`conversation.SeatLastRan`, wired in `teams/app.go`). `membership.Launch` and
`membership.Teams` turn a seat list into the launch by team. `conversation.SeatsOfTeams` turns a
team into its one recorded seat.

## Left, in the design's order

1. **Text check** (design §1 part 2): `src-go/qa/seattext` plus a `qa/cmd` main, run by
   `qa-changed` and `qa-all` (`scripts/qa-gates.tsv`, `scripts/qa-routing.tsv`, `CLAUDE/QA.md`).
   Reuse the analyzer's expressions: move the regexps out of `qa/rules/seatnumbertext.go` into a
   package both import. Scope is `docs/**/*.md` (not `docs/ccy-changelog.md`), `src-go/**/usage.txt`,
   `src-go/**/help.txt`, and the message lines of `files/var/local/claude-{yolo,code}/`. Prove it
   red, then fix the hits the design lists (§1 "Help and launcher text", "Docs") and §4. Any
   launcher edit bumps `CCY_VERSION`: re-read it at HEAD and commit through a temp index.
2. **Bare `ccy --teams`** (§2): when `system.PoolTeams` holds exactly one team, take it without
   the list. The entry point is the chooser's bare-list path (`chooser.go`, `withTeamchat`).
3. **Plain `cc`** (Q1, decided yes): in a checkout holding a host seat, join it; `cc --no-teams`
   opts out (`files/var/local/claude-code/cc-teams.bash:54-74`, which is bash and on the ratchet).
4. **Unattended pick**: `chooser.go:104` refuses every unattended team named alone. Use
   `seatchoice.Pick` first, and refuse, in words, only a genuine choice.
5. **R89 restore seat pass** (§3): `ccy-sessions restore` with `--dry-run`. `restore.go:549`
   still prints the replayed argv with the seat.
6. **Session names by role** (Q7): `ccy-<team>-<project>-<role>` where the role is unique.
7. Rows still lack the conversation's first prompt (Q5) and the seat's role. A seat's own model
   (`seats model`) is still asked by seat, though its usage no longer shows it.

## Notes

- `qa/rules/rules_test.go` `TestAllIsValid` counts the rules. Re-read it at HEAD before a
  commit: fix-431b is also adding a rule.
- Other agents share this worktree. Commit only your paths through a temp `GIT_INDEX_FILE`, and
  commit without a pathspec (a pathspec re-reads the working copy).
