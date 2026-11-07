# Plan 00248 research: what starting a team session asks today

Read from the code at F44 `2d4aa6842` (CCY 3.108.0 and later, with the launch summary of Plan
00201 Task 2.33 merged). Paths under `files/var/local/claude-yolo/` are written from that
directory (`claude-yolo`, `lib/`); Go paths from `src-go/internal/`. Docs read alongside:
`docs/ccy.md` (Quick Launch, sessions with teams: 1246-1248, 1277-1278) and
`docs/agent-bus.md` (Getting started step 3: 78-98; Starting sessions: 547-561).

A "step" below is one screen or prompt that waits for a person. Conditional steps are marked
**(if ...)**. Status lines and banners that wait for nothing are not counted.

## 0. The launcher's order (every route ends here)

The bash launcher runs in this order; every route below ends in it with some arguments.

| #   | Step                                                        | Where                                                                                                                                  | When                                                                                                                                                                        |
| --- | ----------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| L0  | Old session-directory migration `read -rp`                  | `claude-yolo:497`                                                                                                                      | (if old v1 session directories exist) rare                                                                                                                                  |
| L1  | The team step: `ccy_launch_teams`                           | `claude-yolo:865-894`, run at 886-887 when `--teams` is given                                                                          | before tmux; calls `ccy-teams choose-teams` (872) when the list is empty or names a team alone; then `agent-bus seat check` (`lib/agent-bus-seats.bash:45-63`), no prompt   |
| L2  | `--debug` layer questions                                   | `claude-yolo:1030`, `1068`                                                                                                             | (if `--debug`)                                                                                                                                                              |
| L3  | Detached-session picker (P0), fzf                           | `lib/tmux-session.bash:213-250` (`ccy_tmux_offer`, pick at 248), from `ccy_tmux_insulate` 382, `claude-yolo:1182`                      | (if this project has a detached ccy tmux session); with a canonical `--teams` list only sessions holding one of its seats                                                   |
| L4  | Quick Launch: the launch summary (gum)                      | `claude-yolo:1228-1313`; `lib/launch-summary.bash:62-80`; Go `teams/chooser/summary.go:83-196`                                         | (if `.claude/ccy/.last-launch.conf` exists and no `--token`, `--ssh-key`, `--network`, `--no-network`, `--no-ssh`: 1223-1225), at a terminal with gum and ccy-teams (38-40) |
| L4n | Quick Launch without gum: `Use same configuration? [Y/n]`   | `claude-yolo:1258` (text `lib/common-pure.bash:40`)                                                                                    | (if no gum or no `ccy-teams`)                                                                                                                                               |
| L5  | SSH key menu, and `Use it anyway? [y/N]`                    | `lib/ssh-handling.bash:647`, `698`, from `claude-yolo:1330`                                                                            | (if no saved file, or Quick Launch declined, or the summary's SSH line picked)                                                                                              |
| L6  | Token: expired `Create a new token now?`                    | `claude-yolo:1523`                                                                                                                     | (if the token is expired or expires today)                                                                                                                                  |
| L7  | Token: usage gate, then the token menu                      | gate `claude-yolo:1534` (`lib/token-usable.bash:15-32`); menu `select_token` `lib/token-management.bash:1060`, from `claude-yolo:1576` | (if no token yet, or the chosen one has no usage left: the gate empties it and the menu is shown)                                                                           |
| L8  | Zombie containers `[a/s/i/q]`                               | `lib/docker-health.bash:163`, from `claude-yolo:1811`                                                                                  | (if a container of this image has no terminal)                                                                                                                              |
| L9  | Existing containers of this project `[c/s/m/q]` (Q3)        | `lib/docker-health.bash:509`, from `claude-yolo:1823`                                                                                  | **any** other running container of this project, so every second seat of a team in one checkout                                                                             |
| L10 | Saved network gone, `Continue without network connection?`  | `claude-yolo:2570`                                                                                                                     | (if `LAST_NETWORK` no longer exists)                                                                                                                                        |
| L11 | Network engine conflict, prune                              | `claude-yolo:2744`, `2763`                                                                                                             | (if a network of the other engine has the project's name)                                                                                                                   |
| L12 | Network auto-detection `Connect? (Y/n)`, `(Y/n/list)`, pick | `claude-yolo:2996`, `3035`, `3068`                                                                                                     | (if a project network matches and no network was saved or given)                                                                                                            |
| L13 | GitHub port 443 offer                                       | `lib/ssh-handling.bash:1366`                                                                                                           | (if port 22 is blocked)                                                                                                                                                     |
| L14 | SSH key passphrase                                          | `lib/ssh-handling.bash:871`                                                                                                            | (if a key needs one and no agent holds it)                                                                                                                                  |
| L15 | `agent-bus seat take`, sudo password                        | `claude-yolo:3809` (`lib/agent-bus-seats.bash:78-90`)                                                                                  | a seat that does not exist yet is made with sudo; a held seat is **refused here (exit 75)**, after the image and container work                                             |
| L16 | Compose services, stop at exit                              | `claude-yolo:3891`                                                                                                                     | (if the project has compose services)                                                                                                                                       |

What the summary (L4) knows about teams (`teams/chooser/summary.go`): it is given the saved
`LAST_TEAMS` and `LAST_TEAM_ROLES` (`lib/launch-summary.bash:66-67`), shows one
`Change team T (seat S, role R)` row per team (251-261), and marks a team NEEDS AN ANSWER when
it is gone, down or excludes the checkout (205-211), does not cover the checkout (216), its seat
could not be checked (218), is held (220), or its role changed (222). Picking a team row asks the
scope (`settle`, 333) then the seat and role (`Seat`, which is `choose.PickSeat`, 345). Launch is
refused while any row is marked (286-292). **When `--teams` is given, or `--no-bus`, the summary
is called with `--skip-teams` (`lib/launch-summary.bash:64`) and shows no team at all**: the
teams were settled at L1, before tmux. Not offered from the summary (00201 Task 2.33's open
items): adding a team; the network line, which clears the saved network and is asked at L12.

There is **no model choice** at launch today (no `--model` in `claude-yolo`); Plan 00207 adds a
minimum model per role.

## 1. The team questions (L1, `ccy-teams choose-teams`)

| #   | Step                                                      | Where                                                          | When                                                                                                  |
| --- | --------------------------------------------------------- | -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| T1  | Team list (fzf, Tab marks several, `+ Create a team` row) | `teams/chooser/list.go:37`                                     | bare `--teams`; **skipped** when exactly one team covers the checkout and is up (30-33)               |
| T1c | The create wizard (several gum inputs and a review)       | `teams/create/wizard.go:507`, `740`, `963`                     | (if `+ Create a team` is marked)                                                                      |
| T2  | Scope: `T does not trust R. Add R (any branch) to it?`    | `teams/chooser/widen.go:51`                                    | (if a chosen team does not cover the checkout)                                                        |
| T2s | sudo password for `play-agent-bus.yml`                    | `teams/chooser/widen.go:90-129` (`widen`, `widenHeld`)         | (if T2 was answered Add)                                                                              |
| T3  | Seat: `Team T: which seat does this session take?` (gum)  | `teams/choose/choose.go:302` (`pickSeat`, from `PickSeat` 112) | every team chosen in T1, and every team named alone; the default row is agent-bus's DEFAULT (296-300) |
| T4  | Role: keep it, make it coordinator, take over, stop       | `teams/choose/choose.go:342` (`offerRole`)                     | an existing seat with a member (a new seat joins as a worker, 323-326)                                |
| T5  | Role change confirmation, then sudo                       | `teams/seats/role.go:40`, sudo 44                              | (if T4 answered with a change)                                                                        |
| T6  | Element consent                                           | `teams/chooser/consent.go:81`                                  | `cc --teams` only (host seats), once per user                                                         |

## 2. The routes

### R1. `ccy --teams` (bare), in a checkout where ccy ran before

L1 runs `choose-teams` with no list: T1 (unless one covering team), T2 and T2s (if not
covering), T3, T4 (T5); then L3 scoped to the seats chosen; then the summary L4 **without the
teams** (`--skip-teams`), so the seats the person just chose are not shown on the screen that
is meant to show every default; then L9 when a teammate runs in the checkout; then L15.

Typical count (several teams on the machine, an existing seat, a teammate running):
**T1, T3, T4, L4, L9 = 5**, plus T2, T2s, L3 and L15's sudo when they apply. The saved
`LAST_TEAMS` is ignored: `ccy --teams` always asks again (`claude-yolo:1281`, `TEAMS_GIVEN`).

### R2. `ccy --teams TEAM`

L1: `choose-teams TEAM` resolves the team named alone (`chooser.go:181-203`): T3, T4. Then L3,
L4 (`--skip-teams`), L9, L15. **T3, T4, L4, L9 = 4.**

### R3. `ccy --teams SEAT@TEAM`

L1 asks nothing (all seats named; `agent-bus seat check` only). Then L3, L4 (`--skip-teams`),
L9, L15; no role is offered on this route. **L4, L9 = 2**; a held seat is found only at L15.

### R4. Plain `ccy`, saved team

L3 (every detached session of the project, a teammate's included: no seats to scope by), L4
with the team rows, Enter; `ccy_launch_teams` for the saved seats (`claude-yolo:1281-1285`,
no question, `record-seats` 1285); L9; L15. **L4, L9 = 2.** This is the route Task 2.33 made
short; held seats are marked on the summary.

### R5. First launch in a checkout (no saved file), `ccy --teams`

T1 (or T1c), T2, T2s, T3, T4, L3, then no L4 (nothing saved), so L5 SSH menu, L7 token menu,
L12 network detection (if matched), L9, L15 sudo for the new seat.
**T1, T2, T2s, T3, L5, L7, L15 = 7**, more with T1c, L12, L9.

### R6. From the picker: `ccy-teams`, "Start a ccy session in DIR?"

`teams/picker/offers.go:396` (a free covering team) or `413` (no team covers: after
`Create a team for R now?` at 408, answered No), a gum yes/no defaulting to No
(`offerStart`, 421-430), runs `ccy --teams` bare in the checkout (`teams/seats/seats.go:61-74`,
`StartCcy`): then all of R1. **1 + 5 = 6.**

### R7. From the picker's team row: "Start a ccy session in TEAM, in this folder"

The row's menu (`teams/picker/menu.go:128`), then the seat picker
`New ccy session in TEAM` (`teams/seats/seats.go:187`, `NewSession` 155-203, which reads the
seats itself through `SeatList` and `seatChoices`), then `ccy --teams SEAT@TEAM` (R3). The role
is never offered on this route. **Menu, action, seat, L4, L9 = 5.**

### R8. After creating a team: "Start a ccy session in DIR as a member of TEAM?"

`teams/create/wizard.go:1146`, default No, then `ccy --teams TEAM` (1150): R2.
**1 + 4 = 5.**

### R9. Adding a team to a running session (`ccy-teams`, sessions view or a team's menu)

Sessions view, `Add to a team`: team (`teams/picker/sessions.go:227`), seat
(`askSeat` 237, rows built at 317-329 from `SeatChooseIn`, with its own "lowest free seat"
first row), role (250), then `ccy-teams session add`, which asks
`Add S to T as seat N ...?` (`teams/hot/add.go:81-99`, asked through `hot.go:664`); sudo when a
seat is made. From a team's menu, `Add a running session` (`teams/picker/addrunning.go:86`) asks
the session first, then the same seat and role. **Team or session, seat, role, confirm = 4.**
No container launch, so L1-L16 do not apply.

### R10. `cc --teams` (a host session)

The same `choose-teams --type=host` (T1-T5) plus T6 once; `cc` has no Quick Launch (Plan 00223
non-goal). Out of this plan's scope (see the plan's Non-Goals).

## 3. Where each default could come from (already in the code)

| Choice              | Source today                                                                                                                                                                 |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Token, SSH, network | `.claude/ccy/.last-launch.conf` (`LAST_TOKEN`, `LAST_SSH_KEYS`, `LAST_NETWORK`), read by `load_launch_config` (`claude-yolo:529-593`), Go reader `ccylaunch/launchconfig.go` |
| Teams and seats     | the same file's `LAST_TEAMS` (canonical `SEAT@TEAM`)                                                                                                                         |
| Roles               | the same file's `LAST_TEAM_ROLES` (Task 2.33); the live role from the roster (`choose/probe.go:497-517`)                                                                     |
| A free seat         | `agent-bus seat choose`'s `DEFAULT` (`agentbus/parse.go:133-136`, `Choices.Default`), already T3's default row                                                               |
| Covering teams      | `declare` `Choices`/`TeamsFor` with each team's standing to the repository (`chooser.go:291-329`)                                                                            |
| Token usage         | `ccylaunch/usable.go:120` (`Front.TokenUsability(ctx, file, fetch)`), the gate's cache; expiry from `internal/tokens`                                                        |
| Who holds a seat    | `SeatChooseAs` row state `held` (`choose/probe.go:502-507`); the session holding it from `teams/discover` and `internal/tmuxccy` (Plan 00223 Task 1.3's design)              |
