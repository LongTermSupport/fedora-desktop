# Plan 00223 design: one confirm to relaunch ccy

Line numbers are of the base this plan was written on (`c15620d06`). Paths under
`files/var/local/claude-yolo/` are written from that directory; paths under `src-go/internal/`
from `internal/`.

## 1. What a relaunch asks today

A "relaunch" here is a person starting `ccy` again in a checkout where it ran before, wanting
the same token, keys, network and seats. Restarts (`ccy-teams restart`, the restart request)
and restores are unattended and ask nothing; they are out of scope.

### 1.1 Plain `ccy` (no flags)

| #   | Question                                                         | Where                                                                       | When                                                                                                                                                                           |
| --- | ---------------------------------------------------------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Q1  | Quick Launch summary, `Use same configuration? [Y/n]`            | `claude-yolo:1278` (block 1237-1343; prompt text `lib/common-pure.bash:40`) | every launch with no `--token`, `--ssh-key`, `--no-ssh`, `--network`, `--no-network` (1237-1239)                                                                               |
| Q2  | Zombie containers menu `[a/s/i/q]`                               | `lib/docker-health.bash:210`, called at `claude-yolo:1847`                  | a container of this image with no controlling terminal                                                                                                                         |
| Q3  | Existing containers menu `[c/s/m/q]`                             | `lib/docker-health.bash:585`, called at `claude-yolo:1859`                  | **any** other running container of this project: every relaunch of a second team seat in the same repo                                                                         |
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

`ccy_launch_teams` runs at `claude-yolo:893`, **before** Quick Launch, and calls
`ccy-teams choose-teams` (`claude-yolo:878`):

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
`ccy-teams describe-seats` (`teams/choose/choose.go:113`). Plan 00221's label is the same kind
of fact (its L11, L16): a property of the seat, not of the launch.

## 2. The relaunch screen

### 2.1 Behaviour (R1-R16)

- **R1 One screen.** When Q1 would be asked today, one screen replaces Q1 and absorbs Q3: the
  saved token, SSH keys, network, each team's seat, role and label, and the other sessions
  running in this checkout. Enter (the default row) relaunches with all of it.
- **R2 Rows, not magic keys** (InteractiveScripts rule 14, `ui` headings rule 15): the answers
  are named rows, each with its accelerator drawn beside it:
  `Relaunch with these settings (y)`, `Choose another token... (t)`, `Choose a seat... (s)`,
  `Answer every question again (n)`, `Quit (q)`. Heading crumbs `ccy > Relaunch`, context
  line the checkout (abbreviated) and the saved date.
- **R3 Only the seat is rechecked** for being usable, by `agent-bus seat choose <team>` (the
  same read `PickSeat` makes, `teams/choose/choose.go:89`), whose CHOICE rows give each seat's
  `free`, `held` or `new` state and whose DEFAULT is the seat to start on. Nothing else is
  re-asked: token, keys, network, role and label are shown, not questioned.
- **R4 Held seat.** A saved seat in state `held` is shown as
  `alpha: seat 2 - held by another session in this checkout`, the first row becomes
  `Relaunch with alpha seat <DEFAULT> instead (y)` and `Choose a seat... (s)` stays. The
  screen never offers to relaunch into a held seat, so F1 cannot happen after the screen
  (a race in the moment between screen and take still ends in F1's refusal, unchanged).
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
  3): `personal - expires in 41 days - 5-hour 32%, 7-day 61%`.
- **R9 Token not usable.** Expired, missing, or used up: the line says why (the gate's own
  `Explain` words for used up, e.g. `7-day limit used up; clears Sat 00:00`), and the first row
  becomes `Choose another token... (y)`. Choosing it runs the existing token chooser
  (`select_token`, with the gate's used-up marks) and keeps every other saved setting.
- **R10 Usage unknown** (offline, no curl, `CCY_TOKEN_USAGE=0`): shown as
  `usage unknown (<reason>)`, and relaunch stays the default, as the gate itself lets such a
  token start.
- **R11 Other sessions.** The running containers of this project (the list Q3 reads) are shown
  with their seats where they have one. Relaunching from the screen means "start beside them",
  Q3's `c`: Q3 is not asked after an accepted screen. Q2 (zombies) is still asked: it reports an
  anomaly, not a preference. `Answer every question again` keeps Q3.
- **R12 Teams on relaunch.** The saved seats are taken on the screen's yes, as Q1's yes takes
  them today (`claude-yolo:1300`); `--teams` given on the command line wins over them.
- **R13 `ccy --teams` with saved seats.** Bare `--teams`, or `--teams TEAM` where every named
  team has a saved seat, shows the screen first; T1-T4 run only behind `Choose a seat...`.
  `--teams SEAT@TEAM` is taken as given and shown on the screen (today's rule).
- **R14 Unattended is unchanged.** Restore, headless, no terminal and end of input take the
  saved token, keys and network without the screen and never the saved team (Plan 00161 M2.9,
  `claude-yolo:1263-1282`). A used-up token stops an unattended launch (the gate's rule).
- **R15 Nothing new is stored.** `.last-launch.conf` and its `CONFIG_VERSION` are unchanged:
  role and label stay the registry's, shown fresh each time. Plan 00146 Task 3.3's rewrite of
  the file after a token switch is unaffected.
- **R16 Fail fast.** A saved file that cannot be read is today's reconfigure path
  (`load_launch_config`); an `agent-bus seat choose` that fails stops the launch with its
  reason (no silent "assume free"); a usage read that fails is R10, which is the gate's own
  rule, not a skip.

### 2.2 The screen (example, no colour)

```text
ccy > Relaunch
in ~/Projects/<repo> - settings saved <date>

  Token     personal    expires in 41 days - 5-hour 32%, 7-day 61%
  SSH keys  ~/.ssh/<key>
  Network   none
  Teams     alpha  seat 2  worker  [infra]     (label once Plan 00221 lands)
            beta   seat 1  coordinator
  Running   2 other sessions here: alpha seat 1, alpha seat 3 - this one starts beside them

> Relaunch with these settings      (y)
  Choose another token...           (t)
  Choose a seat...                  (s)
  Answer every question again       (n)
  Quit                              (q)
```

### 2.3 Where it is built

The bash launcher is on the bash-limits ratchet (`scripts/bash-limits.allowlist:55`): it may
shrink, never grow, and new tooling is Go. So the screen is a Go command,
`ccy-relaunch` (name: decision C1), in `internal/ccylaunch` beside the token-usage gate's
`ccy-token-usable` (F15 there; this is F16), installed the same way by `play-claude-yolo.yml`
through `tasks/build-go-command.yml`.

- It reads `.last-launch.conf` as data (strict `KEY=VALUE` / `KEY="VALUE"` lines, the keys
  `load_launch_config` requires; anything else refused), never by sourcing it. An oracle test
  writes the file with the bash writer's own lines and reads it back (an on-disk format, which
  src-go/CLAUDE.md allows an oracle for).
- Seats and roles come in process from `internal/teams/choose` (its describer) and
  `internal/agentbus` (`SeatChooseAs`, `List`), through `execx` to the `agent-bus` wrapper:
  no second parser of `describe-seats` text. Plan 00221 adds the label to that describer, so
  the screen shows it with no change here.
- Token expiry from `internal/tokens`; usage from the gate's `Usability` (in process).
- The running containers through `internal/engine` (the engine `container_cmd` would use).
- It prints its answer on stdout (StderrHygiene: the payload), everything else on stderr: one
  line `relaunch`, `token`, `reconfigure` or `quit`, then the seat list to use (canonical
  `SEAT@TEAM,...`, possibly moved by R4/R7). Token, keys and network the bash already has from
  the saved file it read.
- The bash block at `claude-yolo:1237-1343` becomes a call and a `case` on the answer, a net
  shrink; the allowlist row is lowered to the new count in the same commit. Q3's call at
  `claude-yolo:1859` is skipped after `relaunch` (one variable, the existing
  `CCY_NO_CONTAINER_PROMPTS` mechanism's sibling).

## 3. The token-usage gate (sibling work)

A sibling worker is adding `ccy-token-usable` (Go, `internal/ccylaunch` F15, `usable.go`) and
`lib/token-usable.bash`'s `ccy_token_usable`, called right after the token is resolved
(today's `claude-yolo:1562`): exit 3 for a token with no usage left sends an attended launch to
`select_token`, whose rows carry a used-up mark; an unattended launch stops.

This plan builds on it and does not duplicate it:

- The screen reads the same `Usability` in process for R8-R10; it does not define "used up".
- The bash gate stays the one authority at 1562: a token the screen showed as usable that is
  used up a moment later is still caught there (and goes to the chooser as the gate says).
- R9's `Choose another token...` hands to the same `select_token` the gate hands to, so a
  person sees one token chooser, with the gate's marks.
- Build order: this plan's Go work starts after the gate is merged (Task 0.2).

## 4. Plan 00221 (labels and a tree board)

The brief called it 00222; on its branch it is **Plan 00221, team member labels and a tree
board** (00222 there is worker tool restrictions). Nothing here re-designs labels:

- 00221 L16 adds a label step to the seat question; R7's `Choose a seat...` is that question,
  so the step arrives with it. On the screen's yes the label question is not asked: it is the
  "keep the label" answer 00221 already gives a launch that asks nothing.
- 00221 L22 adds the label to the launch block and the describer; the screen's team line
  reads it from there (2.3), shown as `[label]` (00221 CA3's form).
- No label is written to `.last-launch.conf` (R15, 00221 L11).
- 00221 L19's status line and its tree board are untouched by this plan.

Requirement for 00221, to take when it builds Task 3.2: the describer exposes the label as a
field, not only inside a sentence, so this screen does not parse words.

## 5. Plan 00209 (v2)

v2 replaces v1's seat store. The screen asks "is this seat free, and which is next" through
one small interface (`seatProbe`) with the v1 implementation over `agent-bus seat choose`; v2
supplies its own when a team migrates. No v2 file is edited here.
