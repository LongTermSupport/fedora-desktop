# Plan 00215 rework review, round 3 (`feat/opencode`, `git diff F44...HEAD`)

This was a review only, and nothing in the branch was changed. I ran these checks in the
worktree, and all passed: `scripts/test-ccy-agent-choice.bash` (62 passed, 0 failed),
`test-ccy-teams.bash` (221/0), `test-ccy-session-registry.bash` (204/0),
`test-ccy-token-handoff`, `-git-signing` (76/0), `-restart-request` (180/0),
`go test -count=1 ./internal/ccylaunch/... ./internal/registry/... ./internal/sessions/... ./internal/teams/...`
(all ok), `scripts/qa-bash-limits.bash` (exit status 0), and `shellcheck -x` on
`lib/agent-choice.bash` and its test (clean).

I also ran the launcher's own Quick Launch block, cut out as `test-ccy-teams.bash` cuts it,
with a saved `LAST_TEAMS="dev1@team-a"`, the answer `y` and a terminal faked. With
`NO_BUS_MODE=true`, no team was launched, `record-seats` was not called, `CCY_SEATS` was empty,
and the saved value came out as `dev1@team-a`. With `NO_BUS_MODE=false`, the team was launched
and recorded as before. So the round 2 fix does what it says.

## The earlier answers hold

- **Round 1, finding 1 (restore).** The record is marked no-restore, `registry.Opencode` is
  checked before `conversation.For` and `ResumeArgs`, and `LaunchedWithBus` is false for the
  record. Tests cover both spellings and a word after `--`.
- **Round 1, finding 2 (Quick Launch team).** `quick_team` is the only way into
  `ccy_launch_teams`, and the no-bus branch clears it.
- **Round 1, findings 3, 4 and 5.** The keep flags are in bash and Go, the gate reads the
  `case`, a bare first word is refused, and the key read fails on a missing or blank key. All
  verified.
- **Round 2, finding 1 (saved team erased).** Fixed, and the behaviour was confirmed by the run
  described above.
- **Round 2 NITs that were not taken.**
  - NIT 2: the box says "Team: none" while a team is saved. Accepted as a NIT.
  - NIT 4: the `printf -v` loop. The SC2034 reason is valid for a library that shellcheck
    checks on its own.
  - NIT 8: the `env` check. The reason is right: `env` is a child, so it would see an export
    left behind.
  - NIT 6: moved to Task 3.3, where PLAN.md records it.
- **Checked, no finding:**
  - The Dockerfile verifies the sha256 before it extracts, asserts the version, validates both
    JSON files with `jq` (installed in `base`), and the LABEL matches
    `REQUIRED_CONTAINER_VERSION` (2.79.1).
  - CCY 3.100.3 is the same in the launcher, the changelog, PLAN.md and meta-deploy.
  - The meta-deploy entry is folded into the existing `play-claude-yolo.yml` entry.
  - The play's lib list and `CCY_LIBS` are both updated.
  - The key goes only by `-e NAME`, and `checkKeyOnlyTo` and its mutation prove it.
  - A word after `--` never sets opencode, in bash or in Go.
  - Every existing difftest golden is unchanged except the three `--help` ones.
  - The launcher shrank, and its allowlist row was tightened.

## Findings

1. **SHOULD-FIX: the no-bus Quick Launch behaviour is held only by text greps, and the reason
   given for that is wrong.** The relevant lines:

   - `scripts/test-ccy-agent-choice.bash:205-207` and `scripts/test-ccy-teams.bash:792-793`
     grep the launcher for the exact lines.
   - Commit 99dfeb6e8 says "the path needs a terminal; the harness has none".
   - But `test-ccy-teams.bash:623-667` already runs this exact Quick Launch block. Its `quick()`
     fakes `read` (`QUICK_ANSWER`), `ccy_nobody_to_ask` (`QUICK_NOBODY`), `ccy_launch_teams`
     (`TEAMS_LOG`) and `ccy-teams record-seats` (`TEAMS_REC`).

   This block has now been fixed twice, after a BLOCKING finding and then a SHOULD-FIX one. A
   grep passes if the line stays but the logic around it changes, for example if the `elif`
   order is changed or an earlier branch starts taking the no-bus case.

   **Fix:**

   - In `quick()`'s preset line (`test-ccy-teams.bash:656`), add
     `NO_BUS_MODE="${QUICK_NO_BUS:-false}"`.
   - Have the harness print `${quick_kept_team:-}` (for example into `TEAMS_END`, or as a
     `kept=` field).
   - Add checks: with `save personal /keys/id_a proj_net dev1@team-a` and
     `QUICK_NO_BUS=true QUICK_ANSWER=y`, `teams_after false` gives `ran=`, `recorded_after`
     gives `none`, and kept is `dev1@team-a`. Also check that `QUICK_ANSWER=n` keeps it.
   - Replace the greps at `test-ccy-agent-choice.bash:205-206` with these checks. The grep for
     the save line (`LAST_TEAMS="${CCY_SEATS:-${quick_kept_team:-}}"`) can stay as the one text
     check, because the heredoc lies outside the block.

2. **NIT: `quick_kept_team` is never given a starting value.** It is set only in the
   asked-Quick-Launch branch (`claude-yolo:1261`) and read at `claude-yolo:3475`. So a variable
   of that name in the caller's environment would be written into `.last-launch.conf` as the
   saved team by any launch that takes no seats. **Fix:** start it empty beside the others at
   `claude-yolo:1257` (`quick_unasked="" quick_team="$LAST_TEAMS" quick_kept_team=""`). That
   only covers launches that reach the Quick Launch block, so also start it empty where the
   launcher sets its other defaults, near `claude-yolo:630-650`.

3. **NIT: Task 1.3 in PLAN.md says less than the docs.** `PLAN.md:61-71` says "a Quick Launch's
   saved team is not taken", but not that it is kept for the next launch. The changelog,
   `docs/ccy.md` and the meta-deploy entry all say it is kept. **Fix:** add "and kept for the
   next launch".

VERDICT: FIX-FIRST
