# Plan 00223 design: one confirm to launch ccy again with the same settings

Line numbers are of the base this plan was written on (`c15620d06`). Paths under
`files/var/local/claude-yolo/` are written from that directory; paths under `src-go/internal/`
from `internal/`.

**Words.** "Relaunch" already means the restart path in this codebase
(`CCY_RESTART_RELAUNCH_SESSION`, the entrypoint's "relaunch this session"), so this design does
not use it for anything new. The screen is the **Quick Launch screen**, the name the launcher
and docs/ccy.md already give the saved-settings offer it replaces; "launching again" is a person
starting `ccy` again by hand in a checkout where it ran before. The plan folder's name predates
this and is left as it is.

## 1. What launching again asks today

Launching again here means wanting the same token, keys, network and seats. Restarts
(`ccy-teams restart`, the restart request) and restores are unattended and ask nothing; they are
out of scope.

### 1.1 Plain `ccy` (no flags)

| #   | Question                                                         | Where                                                                       | When                                                                                                                                                                           |
| --- | ---------------------------------------------------------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Q1  | Quick Launch summary, `Use same configuration? [Y/n]`            | `claude-yolo:1278` (block 1237-1343; prompt text `lib/common-pure.bash:40`) | every launch with no `--token`, `--ssh-key`, `--no-ssh`, `--network`, `--no-network` (1237-1239)                                                                               |
| Q2  | Zombie containers menu `[a/s/i/q]`                               | `lib/docker-health.bash:210`, called at `claude-yolo:1847`                  | a container of this image with no controlling terminal                                                                                                                         |
| Q3  | Existing containers menu `[c/s/m/q]`                             | `lib/docker-health.bash:585`, called at `claude-yolo:1859`                  | **any** other running container of this project: every launch of a second team seat in the same repo                                                                           |
| Q4  | Saved network gone, `Continue without network connection? [Y/n]` | `claude-yolo:2625`                                                          | the saved `LAST_NETWORK` no longer exists                                                                                                                                      |
| Q5  | Compose services, start/stop                                     | `lib/network-management.bash:1038`, `claude-yolo:3958`                      | the project has compose services                                                                                                                                               |
| Q6  | Token expired, `Create a new token now? (Y/n)`                   | `claude-yolo:1553`                                                          | the saved token is expired or expires today                                                                                                                                    |
| Q7  | SSH key passphrase                                               | `lib/ssh-handling.bash:871`                                                 | a saved key needs one and no agent holds it (an answer, not a confirmation)                                                                                                    |
| F1  | Seat held by another session: launch **refused** (exit 75)       | `agent-bus seat take` at `claude-yolo:3876` (`lib/agent-bus-seats.bash:87`) | the saved seat is held; found only after Q1-Q7 and the image work, because `agent-bus seat check` (`lib/agent-bus-seats.bash:45`) changes nothing and does not look at holders |

Answering **n** to Q1 brings back the full questions: the token menu (`select_token`,
`claude-yolo:1606`), the SSH key menu (`lib/ssh-handling.bash:647`, with `Use it anyway? [y/N]`
at 698), the network menus (`claude-yolo:3077`, `3116`, `3149`), and **no team at all** unless
`--teams` was given.

### 1.2 `ccy --teams` (bare) or `ccy --teams TEAM`

`ccy_launch_teams` runs at `claude-yolo:892-899`, **before** Quick Launch and before the tmux
insulation at 1196, and calls `ccy-teams choose-teams` (`claude-yolo:878`). Its canonical list
then replaces the `--teams` words in `set --` and `CCY_ORIG_ARGS` (`ccy_teams_canonical_args`,
895-898), which the tmux re-exec (1196) and a restart (3776) replay.

| #   | Question                                          | Where                         | When                                               |
| --- | ------------------------------------------------- | ----------------------------- | -------------------------------------------------- |
| T1  | Team list (fzf, multi)                            | `teams/chooser/list.go:37`    | bare `--teams`                                     |
| T2  | `<team> does not trust <repo>. Add ... ?`         | `teams/chooser/widen.go:51`   | a chosen team does not cover this checkout         |
| T3  | `Team <team>: which seat does this session take?` | `teams/choose/choose.go:241`  | every team named alone, and each team chosen in T1 |
| T4  | Role (keep, make coordinator, stop being one)     | `teams/choose/choose.go:281`  | an existing seat with a member                     |
| T5  | Role change confirmation                          | `teams/seats/role.go:40`      | T4 answered with a change                          |
| T6  | Element consent                                   | `teams/chooser/consent.go:80` | `cc --teams` only (host type), once per user       |

Then Q1 is asked anyway, showing the seats T1-T4 just chose (`claude-yolo:1274`), then Q2-Q7
and F1 as above. Keeping everything the same through `ccy --teams` therefore costs T1, T3, T4
and Q1 at least, plus Q3 whenever a teammate runs in the same repo: the "multiple
confirmations" the owner reports.

### 1.3 What is remembered

`.claude/ccy/.last-launch.conf`, written at `claude-yolo:3449-3463` and read by
`load_launch_config` (`claude-yolo:529-593`): `LAST_TOKEN`, `LAST_SSH_KEYS`, `LAST_NETWORK`,
`LAST_TEAMS` (the canonical `SEAT@TEAM` list), `LAST_LAUNCH_DATE`. The role is not stored: it
is the seat's, in the team registry, and is read back for display by
`ccy-teams describe-seats` (`teams/choose/choose.go:113`), whose describer
(`describer.describe`, `choose.go:356`) returns a sentence, not fields. Plan 00221's label is the
same kind of fact (its L11, L16): a property of the seat, not of the launch.

## 2. The Quick Launch screen

### 2.1 Behaviour (R1-R20)

- **R1 One screen.** When Q1 would be asked today on a terminal, one screen replaces Q1, absorbs
  Q3, and (R13) comes before T1-T4: the saved token, SSH keys, network, each team's seat, role and
  label, and the other ccy sessions running in this checkout. Enter (the default row) launches
  with all of it.
- **R2 Rows, not magic keys** (InteractiveScripts rule 14, `ui` headings rule 15): the answers
  are named rows, each with its accelerator drawn beside it:
  `Launch with these settings (y)`, `Choose another token... (t)`, `Choose a seat... (s)`,
  `Answer every question again (n)`, `Quit (q)`. Heading crumbs `ccy > Quick Launch`, context
  line the checkout (abbreviated) and the saved date.
- **R3 Only the seat is rechecked** for being usable, by `agent-bus seat choose <team>` (the
  same read `PickSeat` makes, `teams/choose/choose.go:89`), whose CHOICE rows give each seat's
  `free`, `held` or `new` state and whose DEFAULT is the seat to start on. Token, keys, network,
  role and label are shown, not questioned.
- **R4 Held seat.** A saved seat in state `held` is shown with what agent-bus reports, and no
  more: `alpha: seat 2 - held (agent-bus: held by a live session)`; when the session lookup of
  R11 maps that seat to a running session, its session name is added. The screen does not say
  where the holder runs. The first row becomes `Launch with alpha seat <DEFAULT> instead (y)`
  and `Choose a seat... (s)` stays. The screen never offers to launch into a held seat.
- **R5 Next free seat.** "Next free" is agent-bus's DEFAULT for that team, never computed
  here. A DEFAULT that is itself held, or none, leaves only `Choose a seat...` for that team.
- **R6 Role on a moved seat.** The new seat keeps its own role: a `new` seat joins as a
  worker (today's rule, `teams/choose/choose.go:262`). When the saved seat was the
  coordinator, the screen says `coordinator stays with seat 2 (held)`. No promotion is
  offered here; `Choose a seat...` runs the full seat and role questions (T3, T4).
- **R7 Choose a seat** runs `PickSeat` (T3, T4, and Plan 00221's label step once it lands)
  for the teams the person picks, then returns to the screen with the new seats.
- **R8 Token preview.** The token line shows the name, the expiry label
  (`token_expiry_label`'s words) and the usage read by the token-usage gate's reader (section
  3\) **from its cache only** (`fetch=false`): no network round trip delays the screen. The bash
  gate fetches later, at the token's resolution. With no cached figures the line says
  `usage not read yet (checked before start)`.
- **R9 Token not usable.** Expired, missing, or used up (by the cached figures): the line says
  why (the gate's own `Explain` words for used up, e.g. `7-day limit used up; clears Sat 00:00`),
  and the first row becomes `Choose another token... (y)`. Choosing it runs the existing token
  chooser (`select_token`, with the gate's used-up marks) and keeps every other saved setting.
- **R10 Usage unknown** (offline, no curl, `CCY_TOKEN_USAGE=0`, no cache): shown as in R8, and
  launching stays the default, as the gate itself lets such a token start.
- **R11 Other sessions.** The running ccy sessions of this checkout, each with its seats, come
  from `internal/teams/discover` (the one lookup of which session names a seat: the session
  records and desired memberships, the same lookup the board uses), cross-checked against the
  project's running containers (the list Q3 reads, through `internal/engine` and the
  `ccy-session` container label). This launch's own tmux session (`CCY_SESSION_NAME`, passed as
  `--session NAME`) is left out. A container no record names is shown as `1 container with no session record`. Launching from the screen means "start beside them", Q3's `c`: Q3 is not
  asked after an accepted screen. `Answer every question again` keeps Q3.
- **R12 Container checks first.** The container preflight block, `claude-yolo:1788-1848` in its
  current order (the never-root check at 1794, `engine_assert_rootless` at 1816, the stale
  container cleanup at 1826, `CCY_NO_CONTAINER_PROMPTS` at 1831-1834, then Q2 at 1847), moves
  **as one unit** to just before the Quick Launch block (1229), **for every launch**, not only
  the screen's path, so there is one order to reason about. Everything it reads is set by then:
  `CONTAINER_ENGINE` (`lib/common.bash:33`, `--engine` at 696), `HEADLESS_MODE` and
  `CCY_UNATTENDED_LAUNCH` (1208-1223). A zombie of this checkout that holds a saved seat is
  then stopped or kept by the person's Q2 answer before the seat probe runs, so the screen never
  moves a seat that Q2 frees a moment later. Each check fails the launch as it does today. Every
  difftest golden whose output order changes is re-recorded with the reason (Task 3.1).
- **R13 `ccy --teams` with saved seats.** See 2.3.
- **R14 Unattended is unchanged.** Restore, headless, no terminal and end of input take the
  saved token, keys and network without the screen and never the saved team (Plan 00161 M2.9,
  `claude-yolo:1263-1282`). A used-up token stops an unattended launch (the gate's rule).
- **R15 Nothing new is stored.** `.last-launch.conf` and its `CONFIG_VERSION` are unchanged:
  role and label stay the registry's, shown fresh each time. Plan 00146 Task 3.3's rewrite of
  the file after a token switch is unaffected.
- **R16 One reader of the saved file.** The bash has already read it (`load_launch_config`, with
  its format and key checks); it passes the values to the screen as arguments
  (`--token NAME --ssh-keys "K1 K2" --network NAME --teams LIST --saved DATE`, plus
  `--teams-given LIST` when `--teams` was deferred (2.3) and `--session NAME`). The Go side never
  opens `.last-launch.conf`, so the two cannot disagree about it.
- **R17 Fail fast.** An `agent-bus seat choose` that fails stops the launch with its reason (no
  silent "assume free"); a session lookup that fails does too. A usage read that cannot be made
  is R10, which is the gate's own rule, not a skip.
- **R18 Answer every question again** runs the full token, SSH key and network questions **and**,
  when the saved settings had teams or `--teams` was given, the team chooser
  (`ccy-teams choose-teams`, T1-T4), which gains a `No team` row for this call
  (`--offer-none`), so a person can still start on no team. Without saved teams and without
  `--teams`, no team question is asked (today's rule). The answer travels on **stdout**, which is
  all `$(ccy-teams choose-teams ...)` at 878 captures: `No team` prints the single line `-`
  (never a team list: a team name starts with a letter, `TEAM_NAME_PATTERN`,
  `helpers/pingbus/protocol.py:87`), exit 0. Line 880 changes from "empty means left the
  chooser, exit 0" to: `-` means no team (`TEAMS_LIST` and `CCY_SEATS` empty, no seat check or
  take, and the canonical-args step **drops** the `--teams` words, so `ccy_teams_canonical_args`
  accepts an empty canonical list as "remove"); empty still means the person left, exit 0.
- **R19 The race between two launches.** Two people (or one person in two terminals) starting in
  one checkout at once can both see seat 2 free: the seat is held only once the container's
  keeper takes its lock, minutes later after the image work. v1 has no host-side hold to take
  earlier (`agent-bus seat take` creates seats and refuses held ones; it does not hold them).
  So: when `agent-bus seat take` refuses with 75 on an attended launch, ccy shows only the
  screen's seat part again for the refused team (the next free seat first, or the chooser),
  rewrites `CCY_SEATS`, the canonical arguments and the session record (`ccy-teams record-seats`), and takes once more. A second refusal stops the launch as today. Unattended
  launches keep today's refusal. A true reservation is decision C9. The retry is written inline
  at 3876 (`ccy-teams quick-launch --seats-only TEAM` and the rewrite), adding no function: the
  file's function count (12 on the allowlist row) does not grow. It runs after
  `ccy_lifecycle_finalize` (3842), so a relative `--run-for` deadline is already fixed and the
  time spent answering the re-offer counts against it; accepted, because it is one question on a
  rare path, and said in the re-offer's own text (`the --run-for time has started`).
- **R20 The screen is attended only.** It refuses (exit 64, said on stderr) when stdin or
  stderr is not a terminal: every unattended path stays in bash (2.4). The bash never calls it
  then (it tests `ccy_nobody_to_ask` and `-t 2` first). On the screen, Esc or Ctrl-C is `Quit`:
  nothing starts. This departs from today's end of input at Q1 (1278-1279: take the saved
  settings without the team), which on a terminal is a person leaving, not a pipe ending; a
  stdin that is not a terminal still takes today's unattended branch.

### 2.2 The screen (example, no colour)

```text
ccy > Quick Launch
in ~/Projects/<repo> - settings saved <date>

  Token     personal    expires in 41 days - 5-hour 32%, 7-day 61% (cached)
  SSH keys  ~/.ssh/<key>
  Network   none
  Teams     alpha  seat 2  worker  [infra]     (label once Plan 00221 lands)
            beta   seat 1  coordinator
  Running   2 other sessions here: <session-A> (alpha seat 1), <session-B> (alpha seat 3)
            this one starts beside them

> Launch with these settings        (y)
  Choose another token...           (t)
  Choose a seat...                  (s)
  Answer every question again       (n)
  Quit                              (q)
```

### 2.3 `ccy --teams` and the screen (R13)

The team step moves **after** the Quick Launch block whenever it would ask a question; the
screen does not move earlier, and nothing at 892 reads the saved file.

- **The deferral test at 892 needs no saved file.** `ccy_launch_teams` and the canonical-args
  step are deferred when all hold: `--teams` is given bare or with at least one team named alone
  (exactly the case that reaches the chooser at 877 today); the launch is attended (877's own
  test); and no `--token`, `--ssh-key`, `--no-ssh`, `--network` or `--no-network` is given.
  These are all facts already parsed by 892. `load_launch_config` is not called there, so it
  runs once, inside tmux, as today, and its warnings and deletions happen once. Otherwise
  (`--teams SEAT@TEAM,...` only, unattended, a choice flag) today's order stands.
- **Deferred.** `TEAMS_GIVEN` stays `true`; `TEAMS_LIST` keeps what was given (empty, or a
  list with team names alone). The tmux re-exec at 1196 replays the arguments as given, so
  inside tmux the same test defers again.
- **Then the Quick Launch block.** Three outcomes:
  - `load_launch_config` refuses the file, or there is none: no screen. The deferred step runs
    right after the block, before the SSH questions (1355): `ccy_launch_teams` with the given
    list, which is today's chooser, now inside tmux and after the (absent) Quick Launch instead
    of before tmux. The person sees the same questions in the same order relative to SSH and
    token.
  - The screen runs. The bash passes the saved `LAST_TEAMS` **and** the given list
    (`--teams-given LIST`); **the Go screen** does the filtering: bare `--teams` offers every
    saved seat; `--teams TEAM,...` offers the saved seats of the named teams only (saved seats
    of teams not named are left out, because the command line names the teams), and a named
    team with no saved seat is shown as `beta: no saved seat - chosen after this screen`. On
    `launch` or after `Choose a seat...`, the payload's seat list holds a `SEAT@TEAM` for each
    covered team and the bare name for each uncovered one.
  - `Answer every question again`: the deferred step runs the chooser for the given list (R18).
- **The deferred step**, in the order of today's Quick Launch yes (`claude-yolo:1300-1305`):
  `TEAMS_LIST` set to the payload's list; `ccy_launch_teams`, which runs `choose-teams` only for
  the items still named alone (its `resolve` keeps each `SEAT@TEAM` and asks only for a team
  alone, `teams/chooser/chooser.go:179-201`); then the canonical-args step, which rewrites
  `set --` and `CCY_ORIG_ARGS` with `ccy_teams_canonical_args` at the flag and value positions
  recorded while parsing (replacing the `--teams` words, not appending as 1303 does today for a
  launch that had none); then `ccy-teams record-seats` for the tmux session record. A restart
  (3776) then replays the canonical seats, as it does today.
- **`--teams SEAT@TEAM,...`** is never deferred: taken as given at 892 and shown on the screen
  (today's rule, `claude-yolo:1274`).

### 2.4 Where it is built, and what stays in bash

The bash launcher is on the bash-limits ratchet (`scripts/bash-limits.allowlist:55`): it may
shrink, never grow, and new tooling is Go. So the screen is Go: a **`ccy-teams quick-launch`**
subcommand (decision C1). `ccy-teams` already has `PickSeat`, the describer, `discover` and the
agent-bus wiring, and is already built and installed by `play-claude-yolo.yml`, so no new binary,
play task or `go-binary-suites.tsv` row is needed.

- Seats and roles come in process from `internal/teams/choose`, through a structured seat view
  this plan adds (`SeatView{Team, Seat, Handle, State, Role, Label}`, `describe` rebuilt on it, so
  `describe-seats` keeps its bytes), and `internal/agentbus` (`SeatChooseAs`, `List`) through
  `execx` to the `agent-bus` wrapper. `Label` is always empty until Plan 00221 sets it (section 4).
- Token expiry from `internal/tokens`; usage from the gate's reader, cache only (R8).
  `TokenUsability` is a method on `ccylaunch.Front`, and `ccy-teams` should not import the
  launcher's front. Default (Task 0.2): once the gate is merged, its cache-only read is moved
  into `internal/tokens` (the pool package ccy and cc already share), `Front.TokenUsability`
  calls it, and `ccy-teams` calls the same function. The gate's tests move with it unchanged.
- It prints its answer on stdout (StderrHygiene: the payload), everything else on stderr: one
  line `launch`, `token`, `reconfigure` or `quit`, then the seat list to use (canonical
  `SEAT@TEAM,...`, possibly moved by R4, R7), possibly empty.

**What moves out of the Quick Launch block (`claude-yolo:1237-1343`)** to the Go screen: the
attended display (the banner and lines 1244-1262 and 1274-1275), the `[Y/n]` read and its retry
loop (1277-1332, the attended branch), and Q3's question on the accepted path (1859 is skipped
after `launch`).

**What stays in bash:** the flag test (1237-1239); `load_launch_config` and its file checks
(529-593); the unattended and headless choice (1263-1273: restore, headless, no terminal,
`--no-bus`); the unattended message of 1280-1283 for a stdin that is not a terminal (R20);
applying the saved token, keys and network (1293-1295); the
headless replay and its refusals (1306-1317, 1336-1341); the deferred team step (2.3); the
container preflight block (moved whole before the Quick Launch block, R12); and the R19 retry
around 3876.

The attended branch replaced is roughly 60 counted lines; its replacement (the call, a `case`
on the answer, the deferred team step and the R19 retry) is expected to be smaller. The
criterion is the ratchet's own: `claude-yolo`'s counted lines and statements **do not grow**,
and the allowlist row is set to what the gate measures after the change. If the replacement
would grow the file, more of the block moves to Go (the unattended branch's messages) until it
does not.

## 3. The token-usage gate (sibling work)

A sibling worker is adding `ccy-token-usable` (Go, `internal/ccylaunch` F15, `usable.go`, with
`TokenUsability(ctx, file, fetch bool)` on the launcher front) and `lib/token-usable.bash`'s
`ccy_token_usable`, called right after the token is resolved (today's `claude-yolo:1562`): exit 3
for a token with no usage left sends an attended launch to `select_token`, whose rows carry a
used-up mark; an unattended launch stops. When this plan was reviewed that work was staged in its
worktree but **not committed**.

This plan builds on it and does not duplicate it:

- The screen reads the same `Usability`, with `fetch=false` (R8); it does not define "used up".
- The bash gate stays the one authority at 1562 and fetches: a token the screen showed as usable
  that is used up by fresh figures is still caught there, and goes to the chooser as the gate
  says.
- R9's `Choose another token...` hands to the same `select_token` the gate hands to, so a
  person sees one token chooser, with the gate's marks.
- **Hard gate:** this plan's build starts only once the gate is merged into F44 (Task 0.2). Both
  edit `claude-yolo`, its allowlist row, `CCY_VERSION` and the Quick Launch difftest goldens, so
  building on an unmerged gate would mean re-recording and re-measuring twice.

## 4. Plan 00221 (labels and a tree board)

The brief called it 00222; on its branch it is **Plan 00221, team member labels and a tree
board** (00222 there is worker tool restrictions). Nothing here re-designs labels:

- 00221 L16 adds a label step to the seat question; R7's `Choose a seat...` is that question,
  so the step arrives with it. On the screen's yes the label question is not asked: it is the
  "keep the label" answer 00221 already gives a launch that asks nothing.
- This plan adds the structured `SeatView` with an empty `Label` field (Task 1.2); 00221's
  Task 3.2 (the launch block, L22) fills it from where it reads labels, and the screen shows it
  as `[label]` (00221 CA3's form) with no change here. Whichever plan builds first adds
  `SeatView`; the other uses it.
- No label is written to `.last-launch.conf` (R15, 00221 L11).
- 00221 L19's status line and its tree board are untouched by this plan.

The `SeatView.Label` requirement has to reach 00221's own Dependencies section. This plan cannot
edit 00221's branch; Task 0.3 asks its owner seat (through the coordinator) to record it there.

## 5. Plan 00209 (v2)

v2 replaces v1's seat store. The screen asks "is this seat free, and which is next" through
one small interface (`seatProbe`) with the v1 implementation over `agent-bus seat choose`; v2
supplies its own when a team migrates, and may give the true reservation C9 asks for. No v2 file
is edited here.
