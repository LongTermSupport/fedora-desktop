# Plan 00223 review, round 1 (Opus)

Reviewed: `git diff F44...HEAD` on branch `agent-ae04210a0169536be-fb911524` (commit 49ccd1a73,
plan only). Sibling work read: Plans 00221 and 00222 on `agent-a73de92448810ff4c-98c4ef93`;
the token-usage gate in the `agent-a702a2956db3e651a-6946d77b` worktree (it is staged there but
**not committed**: the branch tip is still `c15620d06`).

**Verdict: CHANGES**

## Does it meet the request

Mostly. One screen with token, keys, network, seat, role and label is "teams merged with
token/ssh keys". The rule that only the seat is rechecked matches "the team seat being the only
one that needs checking". Absorbing Q3 removes the extra confirm the owner hits with
teammates. Labels are handled as "future" through 00221. The gaps are the `ccy --teams`
ordering (B1), the reconfigure path (S5) and the case the owner names, several sessions in
one repo at once (S6).

## Claims checked

Every file:line in DESIGN section 1 was checked against the worktree and is accurate:
claude-yolo 529-593, 877-893, 1237-1343, 1263-1282, 1274, 1278, 1300, 1553, 1562, 1606, 1847,
1859, 2625, 3077/3116/3149, 3449-3463, 3876, 3958; lib/agent-bus-seats.bash 45 and 87;
docker-health 210 and 585; common-pure 40; network-management 1038; ssh-handling 647, 698 and
871; list.go 37; widen.go 51; choose.go 89, 113, 241, 262 and 281; role.go 40; consent.go 80;
bash-limits.allowlist 55. `agentbus.SeatChooseAs`, `Choices.Default`, `Script.HeadingAt`,
`CCY_TOKEN_USAGE=0` and the gate's `Usability`/`Explain` all exist.

## Blocking

1. **R13 has no design.** `ccy_launch_teams` runs at claude-yolo:892-899, long before the
   Quick Launch block. It also rewrites `set --` and `CCY_ORIG_ARGS` from `CCY_SEATS` (the
   canonical-args step at 895-898). So to show the screen first for bare `--teams` or
   `--teams TEAM`, either the screen has to move before line 892, which needs the saved file
   and the token/keys flag test earlier, or the team step and the argument rewrite have to
   move after it. Task 3.2 says only "R13 ordering (877-893)". The plan must name which one,
   say what happens to the argument rewrite and to `TEAMS_GIVEN`, and say how it treats a
   saved list that has teams the command line did not name.
2. **Task 3.2 says "replace 1237-1343", but that block also holds the unattended and headless
   paths**: restore, headless, no terminal, end of input (1263-1283, 1306-1317), and the
   headless refusal at 1336-1341. Task 2.4 has the Go command refuse when there is no
   terminal, so all of that stays in bash. The task should list what stays and what moves,
   and the "shrinks" success criterion should be restated to match.
3. **The structured describer is assumed but does not exist.** `describer.describe`
   (choose.go:356) returns a sentence. The screen's seat, role and label columns need fields.
   Add a test-first task to expose a structured seat description from `teams/choose`. The
   "requirement for 00221" (a label field) is written only in this plan: record it in 00221
   (or in 00221's dependencies), or 00221 will not build it.

## Should-fix

04. Drop the Go strict reader and its oracle (Task 1.1). The bash has already loaded `LAST_*`
    through `load_launch_config`. Pass those values to the command instead. As planned there
    are two readers of one file, and they can disagree: a file bash sources the Go reader
    could refuse.
05. C1: "relaunch" already means the restart path in this codebase
    (`CCY_RESTART_RELAUNCH_SESSION`, the entrypoint's "relaunch this session"). Pick another
    name, for example `quick-launch`. Also weigh making it a `ccy-teams` subcommand: that
    binary already has PickSeat, the describer and the agent-bus wiring, and is already built
    and installed. That would mean no new binary, play task or tsv row.
06. The concurrent case the owner names (two sessions starting in one repo) is the race R4
    leaves open. Both screens see seat 2 free, and the image build takes minutes before
    `seat take`. At least state this as a known gap with a decision. Consider re-running the
    probe just before the take, or taking the seat earlier.
07. `TokenUsability(ctx, file, fetch bool)`: say whether the screen fetches (a network round
    trip before the screen appears) or reads the cache only. The default should be cache, with
    the bash gate fetching later.
08. Q2 (zombies) runs after the screen. A zombie of this checkout that holds the saved seat
    makes the screen move the seat, and then Q2 offers to kill the holder. Order Q2 before the
    screen, or tie the two together.
09. "Answer every question again (n)" should include the team and seat chooser. Today `n`
    joins no team, which goes against "teams merged with token/ssh".
10. Task 3.1: the difftest needs a fake of the new command, the way `ccy-teams` is faked
    (scenarios.json). Also name where "Running ... seat N" comes from: the container-to-seat
    mapping (the session registry or the ccy-session label), not `internal/engine` alone.
11. R4's wording "held by another session in this checkout" is not what `held` means: the
    holder can be in another checkout. Show what agent-bus reports.
12. Task 0.2: the gate is not committed yet. Make "merged into F44" the hard gate, since both
    plans edit claude-yolo, the allowlist row, `CCY_VERSION` and the quick-launch goldens.

## Fit

Go not bash, the ratchet, the CCY bump, IaC through `build-go-command.yml` plus meta-deploy,
and TDD are all present. No install-specific names were found. No clash with 00222. 00221
L16's label step sits behind "Choose a seat...", which fits.
