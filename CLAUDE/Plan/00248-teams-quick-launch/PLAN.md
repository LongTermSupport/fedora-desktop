# Plan 00248: teams quick launch

**Status**: Not Started (plan only; owner decisions D1-D11 open, recommendations stated)
**Created**: 2026-11-06
**Owner**: joseph
**Priority**: Medium

## Overview

The owner: "the new ccy 'just change one thing from the quick launch' is great - we really need
this for the teams bit. Starting up a teams session is very clunky with lots of steps."

Quick Launch is now one summary screen of every default, where Enter accepts them all and a
line picked changes only that one (`ccy-teams launch-summary`, Plan 00201 Task 2.33). It shows
teams only for a plain `ccy` with saved teams. Every other team start (`ccy --teams`,
`ccy --teams TEAM`, the picker's and the create wizard's "Start a ccy session", adding a team to
a running session) still asks the team, the seat and the role one question at a time before the
summary, which is then shown without the teams. The map of every route, prompt by prompt with
file and line, is [RESEARCH-team-start-today.md](RESEARCH-team-start-today.md).

This plan makes the summary the one place a team launch is settled: every team choice gets a
default (the saved seat, the free seat agent-bus offers, the seat's current role, the one team
that covers the checkout), Enter accepts all of them, and a line opens the existing question
for that one choice. The typical `ccy --teams` start goes from five answers to one. The design,
its defaults, the failure cases and the step counts are [DESIGN.md](DESIGN.md).

## Goals

- `ccy --teams`, `ccy --teams TEAM` and `ccy --teams SEAT@TEAM` in a checkout where ccy ran
  before are one Enter when nothing has changed (DESIGN section 7).
- Every team start shows its teams, seats and roles on the summary, with where each default
  came from; adding or leaving out a team is a line of the summary.
- A default that no longer holds is shown, never acted on silently: a held seat proposes the
  next free one and says so, a used-up or expired token is marked and opens the token chooser
  (DESIGN section 4).
- One screen and one team model in Go: no second summary, and the bash launcher does not grow.

## Non-Goals

- A second screen (Plan 00223's `ccy-teams quick-launch`): superseded, decision D1.
- `cc --teams` (host sessions): `cc` has no Quick Launch.
- Porting the SSH key or network discovery to Go (Plan 00191); the network line stays as today.
- A model choice at launch (Plan 00207 owns roles' minimum models).
- A host-side seat reservation against two launches racing for one seat.
- Restores, restarts and headless launches: they ask nothing today and keep doing so.

## Dependencies

- Builds on Plan 00201 Task 2.33 (merged, CCY 3.108.0) and takes its two open items (adding a
  team from the summary; the network line, kept as is by D8).
- Supersedes Plan 00223's screen, if the owner agrees (D1); its still-valid requirements are in
  this plan's tasks (held seat to the next free one, attach a detached holder, the token line,
  the Q3 skip, the `--teams` filtering).
- Plan 00207 adds a mark to a team line when it lands (D9); Plan 00221's label is one more field
  of the team line.

## Tasks

Every build task is test first: the failing test committed or shown before the code
(src-go/CLAUDE.md), red and green noted in the journal.

### Phase 0: Decisions

- [ ] ⬜ **Task 0.1**: The owner answers D1-D11, or the recommendations stand; each answer in
  the journal and the tasks adjusted. On D1, Plan 00223's status and its README row are updated
  in the same commit.

### Phase 1: One team line (Go)

- [ ] ⬜ **Task 1.1**: `choose.Probe` returns, besides `taken` and the role, agent-bus's
  `DEFAULT` seat and, for a held seat, the session holding it and whether it is detached.
  Accepted when table tests cover free, held by an attached and a detached session, no free
  seat, a new seat, and an agent-bus failure (an error, never a guess).
- [ ] ⬜ **Task 1.2**: The team line (`launchTeam`, `assess`, `row`, `change` of
  `teams/chooser/summary.go`) moved into a reusable type, with the held-seat proposal of DESIGN
  section 4. Accepted when the existing `summary_test.go` passes unchanged in meaning and new
  tests cover each row of DESIGN section 4 that concerns a team.

### Phase 2: The summary settles every team launch (Go)

- [ ] ⬜ **Task 2.1**: `launch-summary --given=WORDS` replaces `--skip-teams`: the bare, named
  and seat forms of DESIGN section 3, the saved seats filtered by the named teams, and a team
  named with no saved seat given `DEFAULT`. Accepted when each row of section 3 is a test and
  the payload's `TEAMS` is the canonical `SEAT@TEAM` list.
- [ ] ⬜ **Task 2.2**: `Add a team...` (the `take` list and `settle`, in process) and
  `Leave team T out`; `Add a team...` marked when no team is shown on a `--teams` launch.
  Accepted when tests cover adding one and several teams, a team that needs the scope, a team
  created from the list, and leaving the last team out.
- [ ] ⬜ **Task 2.3**: The `Attach` row and an `ATTACH` payload line (DESIGN section 4: the
  client is switched to the holder's session, the new one ends with nothing started). Accepted
  when the row appears only for a detached holder of this checkout, and a test shows the
  switch and the launcher's exit with no container started.
- [ ] ⬜ **Task 2.4**: The token line with expiry and cached usage: the cache-only read moved
  from `ccylaunch/usable.go` into `internal/tokens` with its tests, called by the front and by
  `ccy-teams`. Accepted when usable, used up, expired, missing, no cache and usage disabled are
  tests, and a used-up or expired token marks the line.
- [ ] ⬜ **Task 2.5**: No saved file (D6): the summary runs for a team launch with the token
  default of DESIGN section 3, SSH and network shown as `asked next`. Accepted by tests.
- [ ] ⬜ **Task 2.6**: The heading names the other running sessions of this checkout (D7).

### Phase 3: The launcher (bash: moves and removals only)

- [ ] ⬜ **Task 3.1**: An attended difftest mode, or a `script -q` driven suite where a pty
  cannot be made reliable in the harness (Plan 00223 Task 3.0's design), so the attended paths
  below are pinned. Accepted when a scenario fails on a broken attended path.
- [ ] ⬜ **Task 3.2**: Scenarios first, then the bash of DESIGN section 5: `ccy_launch_teams`
  defers to the summary when attended with gum and `ccy-teams`; the summary runs for team
  launches with no saved file; `--skip-teams` removed from `lib/launch-summary.bash`; the
  canonical seats reach `CCY_ORIG_ARGS` and the session record through
  `ccy_teams_canonical_args` and `record-seats`; Q3 skipped after an accepted team summary, the
  zombie menu kept; the `attach` answer handled. Accepted when every route of DESIGN section 7
  has a scenario, unattended launches keep today's goldens, and `qa-bash-limits` measures
  `claude-yolo` and `lib/launch-summary.bash` at or below today's counts. `CCY_VERSION` bump and
  `docs/ccy-changelog.md`.
- [ ] ⬜ **Task 3.3**: The `scripts/test-ccy-*` suites that pin Quick Launch's text or the
  order of its questions updated in the same commit, each with the reason.

### Phase 4: The picker's routes (Go)

- [ ] ⬜ **Task 4.1**: R7 (D10): `seats.NewSession` keeps its refusals and runs
  `ccy --teams TEAM` instead of its own seat chooser. Accepted when its tests show no seat
  question and the launcher's arguments.
- [ ] ⬜ **Task 4.2**: R9 (D11): adding a team to a running session is the team (or session)
  choice and then one screen on the team line, Enter running `session add ... --yes`. Accepted
  when tests cover the default seat and role, changing each, a held seat, and Esc (nothing
  changed).

### Phase 5: Docs, deploy, review

- [ ] ⬜ **Task 5.1**: `docs/ccy.md` (Quick Launch, sessions with teams), `docs/agent-bus.md`
  (Getting started, Starting sessions), `ccy-teams --help` (`teams/usage.txt`).
- [ ] ⬜ **Task 5.2**: `play-claude-yolo.yml` (which builds `ccy-teams` and installs the
  launcher) queued in `CLAUDE/Plan/meta-deploy.list` in the commit that needs it.
- [ ] ⬜ **Task 5.3**: Adversarial review of the diff until MERGE, then the `qa-reviewer` agent.
- [ ] ⬜ **Task 5.4**: On the host, each route of DESIGN section 7 counted by hand, and the
  failure cases: a saved seat held (attached, then detached), a used-up token, a team gone.

## Decisions

Each is open; the recommendation stands unless the owner changes it.

| ID  | Decision                                      | Recommendation                                                                                                                                                     |
| --- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| D1  | Plan 00223's separate screen                  | Superseded by this plan: it would be a second summary. 00223 is Cancelled with a pointer here; its design stays as the reference for the requirements carried over |
| D2  | Where the screen runs                         | Inside tmux, where the summary runs today; no hand-off across the re-exec (DESIGN section 5)                                                                       |
| D3  | The detached-session picker (L3) on `--teams` | Unchanged; for bare `--teams` and `--teams TEAM` it is no longer scoped by seat, as for plain `ccy` today. The summary's `Attach` row covers a held saved seat     |
| D4  | A saved seat that is held                     | The line proposes the next free seat and says which seat is held by whom; Enter accepts. A seat named on the command line is marked instead, never moved           |
| D5  | Bare `ccy --teams` with saved teams           | Defaults to the saved seats (today it always asks the team list); `Add a team...` and `Leave team T out` change them                                               |
| D6  | First launch in a checkout                    | The summary still runs for a team launch; token default the unexpired token with most usage left in the cache; SSH and network asked at their usual steps          |
| D7  | The existing containers menu (Q3)             | Not asked after an accepted team summary (the other containers are teammates); the summary's heading names them. The zombie menu is kept                           |
| D8  | The network line                              | As today: cleared and asked at its own step                                                                                                                        |
| D9  | Model                                         | No row now; Plan 00207's minimum model becomes a mark on a team line when it lands                                                                                 |
| D10 | The picker's team-row seat chooser            | Removed; the row runs `ccy --teams TEAM` and the summary proposes the seat and offers the role                                                                     |
| D11 | Adding a team to a running session            | The team choice, then one screen of seat and role defaults; Enter adds                                                                                             |

## Success Criteria

- [ ] Every route of DESIGN section 7 asks no more than its design count, on the host.
- [ ] No default is changed without the line saying so; a held seat is never launched into.
- [ ] Restore, restart, headless and no-terminal launches are unchanged (goldens).
- [ ] `claude-yolo` and `lib/launch-summary.bash` do not grow (`qa-bash-limits`).
- [ ] `qa-changed.bash` on each commit, `qa-all.bash` before push, `qa-reviewer` passes.

## Delivery & Milestones

- Plan and design: branch `plan/teams-quick-launch`.
