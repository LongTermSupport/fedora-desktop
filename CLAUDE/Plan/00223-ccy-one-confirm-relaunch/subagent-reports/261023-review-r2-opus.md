# Plan 00223 review, round 2 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` (PLAN.md, DESIGN.md, JOURNAL, README row)
at afb949583, against the round-1 report and the code at the base. Line numbers are the base's.

**Verdict: CHANGES**

## Round-1 items

| R1  | State                 | Note                                                                                                                                                                                                                                                                                        |
| --- | --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | Partly fixed          | DESIGN 2.3 now names the order (team step after the screen; the screen does not move) and the rewrite. Its "when it applies" test is not buildable as written: S1 below                                                                                                                     |
| B2  | Fixed                 | DESIGN 2.4 lists what moves and what stays; criterion restated as the ratchet's own                                                                                                                                                                                                         |
| B3  | Fixed (decision)      | Task 1.2 `SeatView`; Task 0.3 routes the 00221 record through the coordinator. 00221's branch still has no mention (expected: this branch cannot edit it)                                                                                                                                   |
| S4  | Fixed                 | R16: one reader, the bash passes values                                                                                                                                                                                                                                                     |
| S5  | Fixed                 | `ccy-teams quick-launch` (C1); "Words" paragraph                                                                                                                                                                                                                                            |
| S6  | Fixed (decision C9)   | R19 retry on 75. The premise checks out: `named_elsewhere` (helpers/agent_bus/checkout.py:793-824) counts a desired membership only for a running session, and membership-init runs inside `ccy_seats_take` (lib/agent-bus-membership.bash:62) at 3876, so there is no earlier hold to take |
| S7  | Fixed                 | R8, C7: cache only                                                                                                                                                                                                                                                                          |
| S8  | Fixed, but new defect | R12 puts Q2 before the screen; see S2                                                                                                                                                                                                                                                       |
| S9  | Fixed, but new defect | R18, C8, Task 2.5; the "no team" signal is unreadable by the bash; see S3                                                                                                                                                                                                                   |
| S10 | Fixed                 | `ccy-teams` is in `observedTools` (difftest/fake_test.go:45); scenarios.json already has ccy-teams rules (lines 43-44); discover is the session lookup                                                                                                                                      |
| S11 | Fixed                 | R4 shows only what agent-bus reports                                                                                                                                                                                                                                                        |
| S12 | Fixed                 | Task 0.2 makes "merged into F44" the hard gate. The gate is still uncommitted on its branch, so the gate stays as written                                                                                                                                                                   |

These claims were checked and hold: claude-yolo 877-899, 1196, 1237-1343, 1847, 1859, 3776, 3876;
`agent-bus seat choose` CHOICE `free|held|new` plus DEFAULT (checkout.py:920-943); `seat take`
refusing held and named seats (checkout.py:836-871); choose.go 89/113/241/262/281/356;
`Script.HeadingAt` (teamtest/console.go:111); `internal/engine`, `internal/tokens` and
`teams/discover` exist; allowlist row 55 (`2782 12 2713`); the sibling's
`Front.TokenUsability(ctx, file, fetch bool)` and `Usability.Explain` (usable.go:113, 59).

## Blocking

None.

## Should-fix

1. **R13's "when it applies" test needs the saved file at line 892, and the design does not say
   how it gets there.** DESIGN 2.3 makes the deferral depend on "every named team having a seat
   in the saved `LAST_TEAMS`" and "a saved file `load_launch_config` accepts". But
   `load_launch_config` is called only at claude-yolo:1243, inside tmux, and it has side
   effects: it deletes a corrupt or other-format file and prints banners (529-593, e.g. 547,
   554-563, 580-590). Running it at 892 means it runs once outside tmux and again inside after
   the re-exec at 1196, so the developer-error banner prints twice and the outer run can delete
   the file the inner run then misses. The design should name one of these: (a) a side-effect-free
   probe at 892 (only "the file exists and names these teams", with the full load staying at 1243
   and a fallback if it then refuses), or (b) move the load to 892 for both paths and drop the
   one at 1243. It should also say what happens when the test passed at 892 but the load at
   1243 then refuses the file (today's chooser has already been skipped). This is the rest of
   round-1 B1.

2. **R12 moves Q2 before the root and rootless-engine safety checks.** On the screen's path Q2
   (`check_zombie_containers_startup`, claude-yolo:1847) would run at the Quick Launch block
   (around 1237). That is before the never-root check (1794), `engine_assert_rootless` (1816),
   the stale-container cleanup (1826) and the definition of `CCY_NO_CONTAINER_PROMPTS` (1831)
   that it takes as its second argument. Q2's TUI stops containers. The design should say that
   these checks (and the `CCY_NO_CONTAINER_PROMPTS` setup) run before Q2 on the screen's path,
   or move with it. It should also check that this ordering does not grow the file (R12, DESIGN
   2.4 "what stays", Task 3.2).

3. **Task 2.5's "distinct stderr line, which the bash reads" cannot be read.** The bash takes
   `choose-teams` output through `$(...)` (claude-yolo:878), which captures stdout only, and the
   chooser draws on the terminal. Capturing stderr would break StderrHygiene and the UI. Also,
   `ccy_launch_teams` exits 0 on an empty list (880), so "No team" would end the launch unless
   that line changes. The signal belongs in the payload or the exit code: for example, a stdout
   token `none`, or a dedicated exit code for "left the chooser". Change R18, Task 2.5, and Task
   3.2 (the 880 change) to match.

## Nits (not blocking)

- R19's re-offer sits at 3876, after `ccy_lifecycle_finalize` (3842), whose comment says it
  resolves a relative deadline "after every prompt". A `--run-for` window would lose the time
  the person spends at the re-offer. Either put that in R19, or re-finalize after the retry.
- The success criterion and Task 3.2 name lines and statements only. The allowlist row also
  holds the function count (12), so the R19 retry must not add a function. Say so.
- R11 "other sessions here": exclude the launch's own tmux session, which already has a record
  before the screen (insulate at 1196), and under R13 has a bare `--teams` there.
- DESIGN 2.3 `--teams TEAM,...`: say who filters `LAST_TEAMS` to the named teams, the bash
  (which costs counted lines) or `quick-launch` (pass both lists).
- Task 0.2: `TokenUsability` is a method on `ccylaunch.Front`, so the in-process default makes
  `internal/teams` import `ccylaunch` and build a Front. Note this as the cost of that choice.
