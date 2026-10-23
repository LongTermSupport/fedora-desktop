# Plan 00223 design: one confirm to launch ccy again with the same settings

Line numbers are of the base this plan was written on (`c15620d06`). Paths under
`files/var/local/claude-yolo/` are written from that directory; paths under `src-go/internal/`
from `internal/`.

**Words.** "Relaunch" already means the restart path in this codebase
(`CCY_RESTART_RELAUNCH_SESSION`, the entrypoint's "relaunch this session"), so this design does
not use it for anything new. The screen is the **Quick Launch screen**, the name the launcher
and docs/ccy.md already give the saved-settings offer it replaces; "launching again" is a person
starting `ccy` again by hand in a checkout where it ran before. The plan folder's name predates
this and is left as it is. The "outer run" is the launcher a person starts; the "inner run" is
the same launcher re-executed inside tmux by `ccy_tmux_insulate` (1196).

## 1. What launching again asks today

Launching again here means wanting the same token, keys, network and seats. Restarts
(`ccy-teams restart`, the restart request) and restores are unattended and ask nothing; they are
out of scope.

### 1.1 Plain `ccy` (no flags)

| #   | Question                                                         | Where                                                                       | When                                                                                                                                                                                                    |
| --- | ---------------------------------------------------------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P0  | Detached-session picker, `a session is detached here` (fzf)      | `lib/tmux-session.bash:245-248` (`ccy_tmux_offer`), from `claude-yolo:1196` | before Q1, whenever this project has a detached tmux session; with a canonical `--teams` list only the sessions holding one of its seats (209-224); plain `ccy` offers every one, a teammate's included |
| Q1  | Quick Launch summary, `Use same configuration? [Y/n]`            | `claude-yolo:1278` (block 1237-1343; prompt text `lib/common-pure.bash:40`) | every launch with no `--token`, `--ssh-key`, `--ssh-agent`, `--no-ssh`, `--network`, `--no-network` (1237-1239)                                                                                         |
| Q2  | Zombie containers menu `[a/s/i/q]`                               | `lib/docker-health.bash:210`, called at `claude-yolo:1847`                  | a container of this image with no controlling terminal                                                                                                                                                  |
| Q3  | Existing containers menu `[c/s/m/q]`                             | `lib/docker-health.bash:585`, called at `claude-yolo:1859`                  | **any** other running container of this project: every launch of a second team seat in the same repo                                                                                                    |
| Q4  | Saved network gone, `Continue without network connection? [Y/n]` | `claude-yolo:2625`                                                          | the saved `LAST_NETWORK` no longer exists                                                                                                                                                               |
| Q5  | Compose services, start/stop                                     | `lib/network-management.bash:1038`, `claude-yolo:3958`                      | the project has compose services                                                                                                                                                                        |
| Q6  | Token expired, `Create a new token now? (Y/n)`                   | `claude-yolo:1553`                                                          | the saved token is expired or expires today                                                                                                                                                             |
| Q7  | SSH key passphrase                                               | `lib/ssh-handling.bash:871`                                                 | a saved key needs one and no agent holds it (an answer, not a confirmation)                                                                                                                             |
| F1  | Seat held by another session: launch **refused** (exit 75)       | `agent-bus seat take` at `claude-yolo:3876` (`lib/agent-bus-seats.bash:87`) | the saved seat is held; found only after Q1-Q7 and the image work, because `agent-bus seat check` (`lib/agent-bus-seats.bash:45`) changes nothing and does not look at holders                          |

Answering **n** to Q1 brings back the full questions: the token menu (`select_token`,
`claude-yolo:1606`), the SSH key menu (`lib/ssh-handling.bash:647`, with `Use it anyway? [y/N]`
at 698), the network menus (`claude-yolo:3077`, `3116`, `3149`), and **no team at all** unless
`--teams` was given.

### 1.2 `ccy --teams` (bare) or `ccy --teams TEAM`

`ccy_launch_teams` runs at `claude-yolo:892-899`, **before** Quick Launch and before the tmux
insulation at 1196, and calls `ccy-teams choose-teams` (`claude-yolo:878`). Its canonical list
then replaces the `--teams` words in `set --` and `CCY_ORIG_ARGS` (`ccy_teams_canonical_args`,
895-898), which the tmux re-exec (1196, where P0 reads it: `lib/tmux-session.bash:376-380`) and a
restart (3776) replay.

| #   | Question                                          | Where                         | When                                               |
| --- | ------------------------------------------------- | ----------------------------- | -------------------------------------------------- |
| T1  | Team list (fzf, multi)                            | `teams/chooser/list.go:37`    | bare `--teams`                                     |
| T2  | `<team> does not trust <repo>. Add ... ?`         | `teams/chooser/widen.go:51`   | a chosen team does not cover this checkout         |
| T3  | `Team <team>: which seat does this session take?` | `teams/choose/choose.go:241`  | every team named alone, and each team chosen in T1 |
| T4  | Role (keep, make coordinator, stop being one)     | `teams/choose/choose.go:281`  | an existing seat with a member                     |
| T5  | Role change confirmation                          | `teams/seats/role.go:40`      | T4 answered with a change                          |
| T6  | Element consent                                   | `teams/chooser/consent.go:80` | `cc --teams` only (host type), once per user       |

Then P0 (scoped to the chosen seats), then Q1, showing the seats T1-T4 just chose
(`claude-yolo:1274`), then Q2-Q7 and F1 as above. Keeping everything the same through
`ccy --teams` therefore costs T1, T3, T4 and Q1 at least, plus Q3 whenever a teammate runs in the
same repo: the "multiple confirmations" the owner reports.

### 1.3 What is remembered

`.claude/ccy/.last-launch.conf`, written at `claude-yolo:3449-3463` and read by
`load_launch_config` (`claude-yolo:529-593`): `LAST_TOKEN`, `LAST_SSH_KEYS`, `LAST_NETWORK`,
`LAST_TEAMS` (the canonical `SEAT@TEAM` list), `LAST_LAUNCH_DATE`. The role is not stored: it
is the seat's, in the team registry, and is read back for display by
`ccy-teams describe-seats` (`teams/choose/choose.go:113`), whose describer
(`describer.describe`, `choose.go:356`) returns a sentence, not fields. Plan 00221's label is the
same kind of fact (its L11, L16): a property of the seat, not of the launch.

## 2. The Quick Launch screen

### 2.1 Where it runs, and how its answer travels

The screen runs in the **outer run, just before the tmux insulation** (before 1195-1196), where
the team chooser's result already is today, so every question is answered before tmux. It
hands over **only the answer and the seats**: `--settled=VALUE` and, on a team, `--teams LIST`.
The token, SSH keys and network stay on **today's saved-settings path**: the inner run takes them
from `.last-launch.conf` in the Quick Launch block exactly as today's yes does (1293-1295,
`NETWORK_FROM_CONFIG=true` included), and no `--token`, `--ssh-key`, `--ssh-agent` or `--network`
is written. This is decision C6 (coordinator default; the owner may change it). The reason: an
explicit flag is not the same as a saved value. `--network NAME` refuses a network that has gone
where the saved one offers compose and Q4 (2581-2659); `--ssh-agent` is dropped from a session
record (lib/session-registry.bash:124) and an explicit `--token` turns Quick Launch off on a
restore (1237), so the key menu would wait with nobody there. Keeping the saved path keeps every
one of those as today, attended and on restore.

- **When it runs** (all must hold), each fact read where it is already set before 1195: the
  insulation guard of 1195 (not headless, not `--create-token`, not `--update-token`; every mode
  that exits without a session has returned above, per the comment at 1184-1186); attended by
  `ccy_launch_teams`'s own test (875-876: `ccy_nobody_to_ask` false, stderr a terminal,
  `CCY_SESSION_RESTORE` empty, `CCY_RESTART_RELAUNCH_SESSION` empty; `SESSION_RESTORE` and
  `CCY_UNATTENDED_LAUNCH` are derived later, 1208-1223, and are not used here); no launch-choice
  flag (the 1237-1239 test); no `--settled`; and `load_launch_config` accepts the saved file. That
  call moves here from 1243 for this path. When the file is missing or refused, its warning and
  deletion happen here once, and the inner run finds no file, as today's second look would.
- **The session name** (R11): line 1201 (`CCY_SESSION_NAME=$(ccy_tmux_current_session)`) moves
  above 1195. It reads only `TMUX`, `TMUX_PANE` and the two pids (`lib/tmux-session.bash:474-480`),
  nothing 1195-1198 sets, so it is the same value at either place: empty in an outer run that
  insulation will re-execute (the inner run, a fresh process, computes its own), the pane's
  session in a run already inside one. The screen gets `--session "$CCY_SESSION_NAME"` when set.
- **Its answer** is NUL-separated words on stdout (StderrHygiene: the payload; everything else
  on stderr). The first word is the answer:
  - `run`, then the **complete new argument list** for the launcher. The Go builds it from the
    launcher's arguments (passed after `--args`) and the `--teams` flag and value positions the
    bash recorded while parsing: the given `--teams` words removed; at the front,
    `--settled=launch|token|ask`, then `--teams SEAT@TEAM,...` when the session is on a team (never
    with `--no-bus` or `--opencode`, R16). Nothing else is added.
  - `attach`, then a session name (R4): the bash calls `ccy_tmux_attach` and exits with its
    status. A refused attach (the session went away between the screen and the call) is said on
    stderr by `ccy_tmux_attach` and ends the launch; it is not re-run as `ccy_tmux_insulate` does
    at `lib/tmux-session.bash:391-397`, because a re-run would only show the screen again, and
    running `ccy` again does that.
  - `quit`: exit 0, nothing started.
- **On `run`** the bash re-executes itself with the new list: `ccy_token_handoff_give` (the
  GitHub token's existing hand-off, `lib/token-handoff.bash`), then
  `CCY_GH_TOKEN_HANDOFF=<path> exec "$CCY_LAUNCHER_PATH" "${new[@]}"`. The fresh run parses the
  arguments, takes the seats at 892, reaches the insulation, and P0 there gets the canonical seats
  (B1 of round 3). The exec makes "the arguments this run was parsed from" and "the arguments
  tmux records" the same list by construction. When the screen ran inside a ccy session (given
  `--session NAME`: a `ccy` started by hand in a ccy pane), that session's record was written
  before this run; the screen then records the answer's seats there in process (the code of
  `ccy-teams record-seats`), as the join at 1304 does today.
- **`--settled=VALUE`** is an internal argument only the screen's answer writes (as
  `CCY_SESSION_RESTORE` is only the restore's), not listed in `--help`. Its value is checked at
  parse (`launch`, `token` or `ask`; anything else refused, exit 64). In the inner run:
  - `launch`: the Quick Launch block runs with the answer already given: the elif chain at
    1268-1273 runs as today (so `--no-bus` keeps the saved team in `quick_kept_team`, 1272), then
    `--settled` sets `quick_unasked` to `Chosen on the Quick Launch screen`, so the yes body
    (1288-1317) applies the saved token, keys and network as today. The banner is printed in the
    pane, as a restore's is; 1281's team clause (`but not its team`) is left out under
    `--settled`, whose seats are the `--teams` it carries.
  - `token`: the same, except that 1293 leaves `SPECIFIED_TOKEN` empty, so the token is resolved
    by `select_token` with the gate's marks; keys and network are the saved ones.
  - `ask`: the block is skipped (`--settled=ask` joins the 1237 test), so every question is
    asked as with no saved file.
  - `launch` and `token`: Q3 at 1859 is not asked (R11).
- **Records, restores and restarts never carry `--settled`.** It is classified `drop` in the
  session registry (lib/session-registry.bash: `--settled=*` beside `--rebuild=*` in
  `ccy_registry_flag_class`, and `--settled` in `CCY_REGISTRY_DROP_FLAGS`, so
  `scripts/test-ccy-session-registry.bash`'s derived-flag check, 160-190, finds it classified).
  `ccy_restart_relaunch_args` (lib/restart-request.bash:264) uses the same classes, so a restart
  drops it too. A restore therefore replays `--teams LIST` and the person's own flags and goes
  through today's unattended branch: saved token (the one `select_token` chose and 3449-3458
  saved, for a `token` answer), saved keys with the agent sentinel, saved network with
  `NETWORK_FROM_CONFIG`. That is the record today's yes with a saved team produces (1303-1304).

### 2.2 Behaviour (R1-R20)

- **R1 One screen.** On the path of 2.1, one screen replaces Q1, absorbs Q3, comes before the
  team chooser (R13) and P0 (R11): the saved token, SSH keys, network, each team's seat, role
  and label, and the other ccy sessions of this checkout. Enter (the default row) launches with
  all of it.
- **R2 Rows, not magic keys** (InteractiveScripts rule 14, `ui` headings rule 15): named rows
  with accelerators drawn beside them: `Launch with these settings (y)`,
  `Choose another token... (t)`, `Choose a seat... (s)`, `Answer every question again (n)`,
  `Quit (q)`. Heading crumbs `ccy > Quick Launch`, context line the checkout (abbreviated) and
  the saved date.
- **R3 Only the seat is rechecked** for being usable, by `agent-bus seat choose <team>` (the
  same read `PickSeat` makes, `teams/choose/choose.go:89`), whose CHOICE rows give each seat's
  `free`, `held` or `new` state and whose DEFAULT is the seat to start on. Token, keys, network,
  role and label are shown, not questioned.
- **R4 Held seat.** A saved seat in state `held` is shown with what agent-bus reports, and no
  more: `alpha: seat 2 - held (agent-bus: held by a live session)`; when R11's lookup maps that
  seat to a session of this checkout, its name and state (attached, detached) are added. The
  first row becomes `Launch with alpha seat <DEFAULT> instead (y)`; `Choose a seat... (s)`
  stays; when the holder is a **detached** tmux session of this checkout, a row
  `Attach to <session> (holds alpha seat 2) (a)` is added (the case P0 serves today). The screen
  never offers to launch into a held seat.
- **R5 Next free seat.** "Next free" is agent-bus's DEFAULT for that team, never computed
  here. A DEFAULT that is itself held, or none, leaves only `Choose a seat...` for that team.
- **R6 Role on a moved seat.** The new seat keeps its own role: a `new` seat joins as a
  worker (today's rule, `teams/choose/choose.go:262`). When the saved seat was the
  coordinator, the screen says `coordinator stays with seat 2 (held)`. No promotion is
  offered here; `Choose a seat...` runs the full seat and role questions (T3, T4).
- **R7 Choose a seat** runs `PickSeat` in process (T3, T4, and Plan 00221's label step once it
  lands) for the teams the person picks, then returns to the screen with the new seats.
- **R8 Token preview.** The token line shows the name, the expiry label
  (`token_expiry_label`'s words) and the usage read by the token-usage gate's reader (section
  3\) **from its cache only** (`fetch=false`): no network round trip delays the screen. The bash
  gate fetches later, at the token's resolution in the inner run. With no cached figures the
  line says `usage not read yet (checked before start)`.
- **R9 Token not usable.** Expired, missing, or used up (by the cached figures): the line says
  why (the gate's own `Explain` words for used up, e.g. `7-day limit used up; clears Sat 00:00`),
  and the first row becomes `Choose another token... (y)`. That answer is `--settled=token`, so
  the inner run reaches the existing token chooser (`select_token`, with the gate's used-up
  marks) and takes every other saved setting from the file (2.1).
- **R10 Usage unknown** (offline, no curl, `CCY_TOKEN_USAGE=0`, no cache): shown as in R8, and
  launching stays the default, as the gate itself lets such a token start.
- **R11 Other sessions.** The ccy sessions of this checkout, each with its seats and whether a
  terminal is attached, come from `internal/teams/discover` (the one lookup of which session
  names a seat: the session records and desired memberships, the same lookup the board uses)
  and the tmux listing (`internal/tmuxccy`), cross-checked against the project's running
  containers (the list Q3 reads, through `internal/engine` and the `ccy-session` container
  label). The outer run has no session of its own yet, so none is left out (a ccy started by
  hand inside a ccy pane passes `--session NAME`, `CCY_SESSION_NAME` computed before 1195 as
  2.1 says, and that one is left out).
  A container no live session names is shown as `container <name> - no session here (the zombie menu after this screen can stop it)`. Launching from the screen means "start beside
  them", Q3's `c`: Q3 is not asked after `launch` or `token`. P0 afterwards gets the canonical
  seats: for a team launch it then offers only a detached session holding one of them, which the
  screen has already offered as `Attach` (R4), so P0 shows nothing new. For a launch on no team
  P0 still offers every detached session (decision C13).
- **R12 Zombies (Q2) stay after the screen**, in the inner run at 1847, with the whole
  container preflight block (1788-1848) unmoved (decision C12). Reason: moving the block before
  the screen means running it in the outer run, and the inner run would run it again (Q2 twice);
  making it once-only needs a new function or a duplicated block in a file at its function limit
  (12 on `scripts/bash-limits.allowlist:55`). The case it would serve, a zombie holding a saved
  seat, is shown on the screen (R11's `no session here` line) with the seat moved to the next
  free one by default; a person who wants that seat back chooses Quit and answers Q2 on the next
  launch, or `Choose a seat...` later. The screen's own reads are safe before the never-root and
  rootless checks: it refuses to run as uid 0 itself (the same words as 1794), and its engine
  call is a read-only `ps`.
- **R13 `ccy --teams`.** See 2.3.
- **R14 Unattended is unchanged.** Restore, headless, no terminal and end of input take the
  saved token, keys and network without the screen and never the saved team (Plan 00161 M2.9,
  `claude-yolo:1263-1282`). A used-up token stops an unattended launch (the gate's rule). The
  banner (1244-1262) and the unasked message (1280-1283) stay for them, byte for byte; a
  record never carries `--settled` (2.1), so a restore and a restart take this branch as today.
  One case changes: stdin a terminal and stderr not (`ccy 2>file`). Today Q1 is read there; the
  outer run shows no screen (875-876's test), insulation still runs (it tests stdin and stdout
  only, `lib/tmux-session.bash:360-365`), and the inner run, in the pane with all three on
  terminals, shows the screen there, recording its seats as 2.1 says. Without insulation (stdout
  not a terminal either) `ccy_nobody_to_ask`'s sibling test `[ ! -t 2 ]` joins the 1271 branch:
  the saved settings without the team, as end of input does today (decision C14).
- **R15 Nothing new is stored.** `.last-launch.conf` and its `CONFIG_VERSION` are unchanged:
  role and label stay the registry's, shown fresh each time. Plan 00146 Task 3.3's rewrite of
  the file after a token switch is unaffected.
- **R16 One reader of the saved file.** The bash reads it (`load_launch_config`, with its format
  and key checks) and passes the values to the screen as arguments (`--token NAME --ssh-keys "K1 K2" --network NAME --teams LIST --saved DATE`, and `--teams-given LIST` with the flag and
  value positions when `--teams` was given, and `--no-bus` when `NO_BUS_MODE` is true, which
  `--opencode` sets too, lib/agent-choice.bash:61). The Go side never opens `.last-launch.conf`.
  The values are shown, not handed back (2.1). A saved `LAST_SSH_KEYS` word equal to
  `SSH_AGENT_SENTINEL` (`ssh-agent`, lib/ssh-handling.bash:295) is shown as the agent; the Go
  holds the same constant with a parity test that reads it from that file. A saved key path
  that no longer exists is shown as `missing`, and launching stays the default, because the
  saved-settings path handles it as today. With `--no-bus` the screen shows
  `Team: none this launch (--no-bus); the saved seats are kept`, probes no seat, and its answer
  never carries `--teams` (869 would refuse it); the inner run keeps the saved team through
  1272's `quick_kept_team`, written back at 3461 as today.
- **R17 Fail fast.** An `agent-bus seat choose` that fails stops the launch with its reason (no
  silent "assume free"); a session or container lookup that fails does too. A usage read that
  cannot be made is R10, which is the gate's own rule, not a skip.
- **R18 Answer every question again** is `--settled=ask`: the inner run skips the Quick Launch
  block and asks the token, SSH key and network questions as with no saved file. Teams are
  settled on the screen before that: when the saved settings had teams or `--teams` was given,
  the screen runs the team chooser in process (`internal/teams/chooser`, the code of
  `choose-teams`): the list (T1) for bare `--teams` or saved teams, with a `No team` row added
  (only the list has rows to add it to); for `--teams TEAM` the seat question of that team (the
  person named it, so no `No team`). `No team` means the answer's argument list has no `--teams`.
  Nothing new crosses the bash: no stdout token, no change at 878-880, none to
  `ccy_teams_canonical_args` (and so none to `lib/agent-bus-seats.bash`, at 57 of 60 code lines
  and 4 of 4 functions). Without saved teams and without `--teams`, no team question is asked
  (today's rule).
- **R19 The race between two launches.** Two people (or one person in two terminals) starting in
  one checkout at once can both see seat 2 free: the seat is held only once the container's
  keeper takes its lock, minutes later after the image work. v1 has no host-side hold to take
  earlier (`agent-bus seat take` creates seats and refuses held ones; it does not hold them).
  So: when `agent-bus seat take` refuses with 75 on an attended launch, the inner run calls
  `ccy-teams quick-launch --seats-only TEAM` (the next free seat first, or the chooser), and
  takes once more with the new list. The inner run's arguments are canonical (2.1: two words,
  `--teams LIST`), so the positions its own parse recorded point at them, and
  `ccy_teams_canonical_args` rewrites `CCY_ORIG_ARGS` there; `ccy-teams record-seats` then fixes
  the tmux record, which was written at insulation. A second refusal stops the launch as today.
  Unattended launches keep today's refusal. A true reservation is decision C9. The retry is
  inline at 3876, adding no function (the row's 12 do not grow). It runs after
  `ccy_lifecycle_finalize` (3842), so a relative `--run-for` deadline is already fixed and the
  time spent answering counts against it; accepted, because it is one question on a rare path,
  and said in the re-offer's own text (`the --run-for time has started`).
- **R20 The screen is attended only.** It refuses (exit 64, said on stderr) when stdin or
  stderr is not a terminal; the bash never calls it then (2.1). On the screen, Esc or Ctrl-C is
  `Quit`: nothing starts. This departs from today's end of input at Q1 (1278-1279: take the
  saved settings without the team), which on a terminal is a person leaving, not a pipe ending;
  a stdin that is not a terminal still takes today's unattended branch.

### 2.3 `ccy --teams` and the screen (R13)

- **At 892.** `ccy_launch_teams` and the canonical-args step are **deferred** when all hold:
  `--teams` is given bare or with at least one team named alone (the case that reaches the
  chooser at 877 today); the launch is attended (877's own test); not a token mode
  (`--create-token`, `--update-token`), headless already being unattended; no launch-choice
  flag; no `--settled`. Facts all parsed by 892; no saved file is read there. Otherwise
  (`--teams SEAT@TEAM,...` only, unattended, a token mode, a choice flag, or the inner run of a
  screen) today's order stands.
- **At the screen's place (2.1), deferred:**
  - the screen runs (2.1's conditions hold): it receives the saved `LAST_TEAMS` and the given
    list and **the Go does the filtering**: bare `--teams` offers every saved seat;
    `--teams TEAM,...` offers the saved seats of the named teams only (saved seats of teams not
    named are left out, because the command line names the teams), and a named team with no
    saved seat is shown as `beta: no saved seat - you choose it next`, its seat question asked
    in process after the screen's yes (`resolve`, `teams/chooser/chooser.go:179-201`). The
    answer's `--teams` is the full canonical list;
  - the screen does not run, for any reason (no saved file, the file refused): the deferred
    `ccy_launch_teams` and canonical-args step run here instead, the same two calls as 892-899,
    so the chooser is asked before tmux as today (later in the run, with nothing asked in
    between) and P0 receives canonical seats. The test is "deferred at 892 and no `run`
    answer", one condition whatever kept the screen away.
- **Saved teams with no `--teams`** (plain `ccy`): the answer's argument list carries
  `--teams LIST` when the screen's seats are not empty: the join of 1296-1305 becomes part of the
  answer, and the inner run takes the seats at 892 like any explicit list. Nothing is appended
  to `CCY_ORIG_ARGS` by hand and `record-seats` is not needed, because the tmux record is written
  from the canonical list.
- **`--teams SEAT@TEAM,...`** is never deferred: taken as given at 892 and shown on the screen
  (today's rule, `claude-yolo:1274`).

### 2.4 Where it is built, and what stays in bash

The bash launcher is on the bash-limits ratchet (`scripts/bash-limits.allowlist:55`, 2782 lines,
12 functions, 2713 statements): it may shrink, never grow, and new tooling is Go. So the screen
is Go: a **`ccy-teams quick-launch`** subcommand (decision C1). `ccy-teams` already has
`PickSeat`, the chooser, the describer, `discover`, `tmuxccy` and the agent-bus wiring, and is
already built and installed by `play-claude-yolo.yml`, so no new binary, play task or
`go-binary-suites.tsv` row is needed.

- Seats and roles come in process from `internal/teams/choose`, through a structured seat view
  this plan adds (`SeatView{Team, Seat, Handle, State, Role, Label}`, `describe` rebuilt on it, so
  `describe-seats` keeps its bytes), and `internal/agentbus` (`SeatChooseAs`, `List`) through
  `execx` to the `agent-bus` wrapper. `Label` is always empty until Plan 00221 sets it (section 4).
- Token expiry from `internal/tokens`; usage from the gate's reader, cache only (R8).
  `TokenUsability` is a method on `ccylaunch.Front`, and `ccy-teams` should not import the
  launcher's front. Default (C11, Task 0.2): once the gate is merged, its cache-only read is
  moved into `internal/tokens` (the pool package ccy and cc already share),
  `Front.TokenUsability` calls it, and `ccy-teams` calls the same function. The gate's tests move
  with it unchanged.

**Leaves the bash** (the attended read, which the screen and `--settled` replace): the
`while true` loop and the `read` (1277-1279), `use_config=unasked` and the `case` with its `Y`
pattern, `break` and `;;` (1282, 1286-1287, 1318-1319, 1331-1332: every path is now the yes
body), the `n` branch (1320-1326), the invalid-answer branch (1327-1330), and the saved-team
join (1300-1305, now the answer's `--teams`, 2.3).

**Stays in bash, byte for byte for unattended launches (R14):** the flag test (1237-1239); the
banner and token label (1244-1262); the elif chain (1268-1273); `describe-seats` (1274); the
unasked message (1280-1283); the yes body (1288-1299, 1306-1317); the headless refusals
(1336-1341); `load_launch_config` (529-593), now also called at the screen's place; the
container preflight block, unmoved (R12); P0 (`lib/tmux-session.bash`, unchanged).

**Changed in place, no line added:** 1237's test gains `--settled=ask`; 1271 gains `[ ! -t 2 ]`
(C14); 1281's team clause is left out under `--settled`; 1293 leaves the token empty under
`--settled=token`; line 1201 moves above 1195.

**New in bash:** the deferral test at 892 and the deferred two calls (2.3); the screen's call
before 1195, its `case` on `run`, `attach` and `quit`, and the hand-off and exec (2.1); parsing
`--settled` and its check; `--settled` setting `quick_unasked` after the chain; the Q3 skip at
1859; the R19 retry at 3876.

**Estimate** (counted lines, the gate's measure: no blank lines, no whole-line comments). Leaves:
about 25 (loop and read 3, the `case` frame and `unasked` 7, the `n` branch 5, the invalid
branch 4, the join 6). New: about 24 (deferral test and the deferred calls 6, the screen's call
and its `case` with hand-off, exec, attach and quit 10, `--settled` parse and check 3, the
`quick_unasked` line 1, the Q3 skip 1, the R19 retry 3). That is level, with no margin to
spare, so the estimate is not the criterion. The criterion is the ratchet's own: `claude-yolo`'s
counted lines, statements and functions **do not grow**, and the allowlist row is set to what
the gate measures after the change.

**If it would grow** (decision C15, default): first shrink the new bash inside these limits (the
screen's `case` can print its own refusals; the deferred calls can share the 892 lines by moving
them below the screen's place for every `--teams` launch, which is the same order for the
undeferred cases because nothing between 892 and 1195 reads `CCY_SEATS`: Task 3.2 verifies that
claim first). If it still grows, the build stops at that commit and the owner decides; the
ratchet is not raised by this plan. No fallback calls `ccy-teams` on an unattended launch (R20)
or touches the unattended branch (R14). `lib/tmux-session.bash` and `lib/agent-bus-seats.bash` are not
changed; `lib/session-registry.bash` gets the one classification of 2.1 (no line added).

## 3. The token-usage gate (sibling work)

A sibling worker is adding `ccy-token-usable` (Go, `internal/ccylaunch` F15, `usable.go`, with
`TokenUsability(ctx, file, fetch bool)` on the launcher front) and `lib/token-usable.bash`'s
`ccy_token_usable`, called right after the token is resolved (today's `claude-yolo:1562`): exit 3
for a token with no usage left sends an attended launch to `select_token`, whose rows carry a
used-up mark; an unattended launch stops. When this plan was reviewed that work was staged in its
worktree but **not committed**.

This plan builds on it and does not duplicate it:

- The screen reads the same `Usability`, with `fetch=false` (R8); it does not define "used up".
- The bash gate stays the one authority at 1562 (in the inner run) and fetches: a token the
  screen showed as usable that is used up by fresh figures is still caught there, and goes to
  the chooser as the gate says.
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
