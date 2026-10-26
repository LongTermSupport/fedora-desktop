# SPEC: winding a team down

Plan 00203, Task 3.14. The owner's request, condensed: every member of a team, the
coordinator included, is told to wind down. Winding down means we are about to shut down; it
does not mean the work is done. Clean up work in progress, clean up sub-agents, push what can
be pushed. A `/goal clear` goes in first, automatically, so that an active goal does not keep
the agent working against the wind-down.

## 1. Facts this spec rests on

- F1. `/goal clear` is a Claude Code slash command. Only text typed into the session's own
  input box runs a slash command; a pingbus message arrives as a message to read, never as a
  command, so it cannot clear a goal.
- F2. The one path from the host into a desktop session's input box is the session's pane on
  ccy's tmux server: `tmuxccy.Tmux.SendKeys`, then `SendEnter` after a submit pause, read back
  with `CapturePane` and judged with `screen.ClaudeState` and `restore.Judge`. `ccy-teams shutdown` types `/exit` this way (package `shutdown`), `ccy-sessions` types its lines this way
  (`sessions.typeInto`). Covers ccy sessions and cc sessions on ccy's tmux server.
- F3. An LXC member this repository installs with `service: true` runs in the container under
  the unit play-agent-bus.yml writes (`helpers/agent_bus_lxc/members.py`
  `render_member_unit`): `tmux -L agent-bus-claude new-session -d -s agent-bus-claude agent-bus-claude`, as the member's user. The host reaches that pane as
  `docs/agent-bus-lxc-members.md` documents for a person: `sudo lxc-attach -n <container> -- runuser -u <user> -- tmux -L agent-bus-claude ...`. The play's LXC member record
  (`lxcrecord`) names each member's container and user and whether the last apply joined it,
  and `lxcfreeze.Lister` already runs `lxc-attach` through `sudo`. So the F2 path is the same
  for an LXC member, with `tmuxccy.Tmux` given a runner that runs tmux in the container
  (`lxcfreeze.Lister.InContainer`, `-P <lxcpath> --clear-env`, `env -i PATH=...`). Out of
  reach: a member whose container is stopped, or which the last apply held back; a member the
  record does not name (a container on another machine, or one joined by hand); a member
  started by hand rather than by the unit (no `agent-bus-claude` session on that server); any
  member when sudo is not ready. A member on another machine and a held seat no session record
  names are out of reach too.
- F4. Typed into a busy session, a line can be lost or garbled: the ccy supervisor records that
  an Enter mid-turn did not submit a pasted `/goal`, and only injects at an idle moment with an
  empty input box (`.claude/ccy/claude-supervise.py`). Text already in the box would be joined
  to what is typed.
- F5. A pingbus message of kind `HUMAN` is one sent from a human's Matrix account listed in the
  team record. ccy-teams holds no human's credential: a password is shown once
  (`login`), and issuing a new one signs the person's Element sessions out. Posting from an
  agent's or the admin's account would make a `PEER` message or a forged one.

## 2. Decisions

- D1. **The wind-down instruction is typed into the session, not sent on the bus.** Typed into
  the input box, it is the user's own turn: Claude Code gives it the authority of the person at
  the keyboard, and the person at the keyboard is the owner, who ran the command as their own
  user on their own tmux server, exactly as if they had attached and typed it. Nothing is
  forged. A bus message would either be a peer note (sent from any account ccy-teams can use)
  or need a human credential ccy-teams does not hold (F5); and only the pane can carry the
  `/goal clear` (F1), so one path keeps the two in order. Like `shutdown`, nothing goes on the
  bus.
- D2. **Never interrupt a turn.** A busy session is waited for until it is idle with an empty
  box, bounded (W6). Escape is never pressed: an interrupted turn can leave the very work in
  progress the wind-down asks to be saved half done.
- D3. **Text in the box is never typed over.** A member whose box holds text before anything is
  typed fails, naming why (as `shutdown` does).
- D4. **A member out of reach is a failure, not a skip** (exit 1): an LXC member that no LXC
  member record of this host names, or any LXC member when sudo is not ready (F3); members on
  another machine; a held seat no record names; a team whose members could not be read. An LXC
  member in a running container whose service session is missing is a failed job. Each is named
  with how to wind it down by hand, and the instruction text is printed once so it can be
  pasted. sudo is asked for only when the team has an LXC member that can be reached. A stopped
  session, a free seat and a parked member have no agent to tell: each is named on its own line
  and is not a failure. **An offline LXC member is not out of reach**: one whose container is
  stopped, or that the last apply held back (record status `stopped` or `deferred`), is named
  under "Not running" as `offline (container <name> stopped)` and is not a failure (Plan 00211,
  rule `offline-member-blocks`; CLAUDE/AgentNotes.md, "An offline team member never blocks
  anything"). This departs from the first version of D4, which failed the run on them.

## 3. Expectations

- W1. `ccy-teams wind-down <team> [--yes]`; `-h`/`--help` prints the usage to stdout and exits
  0\. No team, a second team, or an unknown option is exit 64 with `See ccy-teams --help.`,
  before anything is read. An unknown team is exit 1, naming it, with nothing typed.
- W2. Which members: every agent member row of the team. A running session that a session
  record names for a seat of the team is one job, once per session, whatever its role; the
  coordinator is one of them. Rows that are not agents (the team row, people) are not members.
- W3. The preview lists each job (`<session>   seat <seat> (<handle>) in <dir>`, with `, the coordinator` and `, also on <team> (seat <s>)` where they apply), then `Not running, nothing to wind down:` (stopped sessions, free seats, parked members), then `Not reachable from here:`
  with each member's why, then what will be typed.
- W4. Asked first, default No (`Wind down N sessions of <team>?`); No is exit 0 with `Nothing was wound down.` and nothing typed. Without a terminal only `--yes` proceeds; otherwise exit
  64\. With no job at all nothing is asked; the report follows.
- W5. For each job, in order, through the pane (F2): wait for idle with an empty box (W6); type
  `/goal clear`, pause, press Enter; read the pane again until the box is empty and idle (if
  the text is still in the box after one more poll of grace, Enter once more); only then type the wind-down text (W8), pause,
  press Enter, and see the box empty (busy or idle) once more (again one more Enter at most).
  `/goal clear` is always typed, whether a goal is set or not: with none, Claude Code only says
  so.
- W6. Bounds: up to 300 s for a session to become idle before the first keystroke (D2), and up
  to 30 s for each typed line to leave the box. A pane is read once a second. The jobs are
  watched together, so one slow session does not delay the others.
- W7. A job fails, typing nothing more, when: its box held text before anything was typed
  (D3); it stayed busy past the bound (`nothing was typed`); a typed line stayed in the box after
  the extra Enter; its session ended before the wind-down text was entered; its pane cannot be
  read or its launcher has ended or waits at a launcher question; a keystroke failed. A failure
  after `/goal clear` went in says so, so the owner knows the goal is already gone.
- W8. The wind-down text is one line (a newline would submit early) and says: this is the
  owner's wind-down of `<team>`, run from ccy-teams on the host; we are about to shut down; this
  does not mean the work is done; commit work in progress (WIP commits are fine); stop every
  sub-agent and background task started and wait for them to end; push each branch whose QA gate
  passes, and say which were not pushed and why; write down where each unfinished piece of
  work stands and what comes next, where it belongs (plan, handoff), so it can be resumed; start
  and dispatch nothing new; any `/goal` was cleared for this, set no new one; then stop, and if
  a hook asks to carry on, stop anyway with `STOPPING BECAUSE: the owner ordered a team wind-down`. The coordinator's text adds that its workers are told the same, and to dispatch
  nothing and record what the team still owes.
- W9. The report: `✓ <session>: /goal clear and the wind-down typed` per job done, `✗ <session>: <why>` per job failed, `- <member>: not running: <why>` and `✗ <member>: not reached: <why>` for the rest; then `<team>: N wound down, N failed, N not running; N members not reachable from here.` (the last clause only when there are any). With any member out of
  reach, the text is printed once after `To wind them down by hand, type /goal clear in each one's session, then:`.
- W10. Exit 0 only when every job was wound down and no member was out of reach; else 1.
- W11. Local only: tmux (in a container through `sudo lxc-attach`), the session registry, the
  LXC member record, `lxc-config` and `lxc-ls`, and the reads the table makes. Nothing goes on the
  bus, no session is ended or killed, no record is changed.
- W12. The picker: a team row's menu has `Wind down the team's sessions (clear goals, save and push work, stop)`, no shortcut, after Restart and before Shut down; it runs the same flow with
  the picker's yes/no, then returns to the list once the report has been read. It is not first
  in the menu.
- W13. `ccy-teams --help` lists `wind-down <team> [--yes]` with W5 and W10 in brief.

## 4. Known limits

- The ccy supervisor's own goal injection (Plan 00269 in the hooks daemon) can type a new
  `/goal` into a session after the wind-down, when the daemon's goal ledger still owes one for
  a plan in progress. The text says to set none and to stop anyway; the supervisor is
  daemon-owned and is not changed here.
- **The supervisor types at the same moments.** The ccy supervisor injects (`/compact`,
  `continue`, `/goal`, `/goal clear`) only at an idle moment with an empty box, which is when
  the wind-down types too. Two typists on one pane can interleave within one poll (1 s): a
  supervisor line can land between the wind-down's text and its Enter, or the reverse. The
  read-back after each Enter catches a line left in the box (W5, W7), not one joined to the
  supervisor's and submitted. The supervisor is daemon-owned; a lock both honour would close it.
- An LXC member started by hand, not by the play's unit, is not on the `agent-bus-claude`
  server and fails (F3).
- `ccy-teams shutdown` still leaves LXC members out; the same path reaches them in Task 3.15
  (section 5, a draft).

## 5. `ccy-teams shutdown` reaching LXC members (Task 3.15, draft expectations)

Draft: numbered for the Fable review, built tests first after it. The rules every lifecycle
operation shares (offline members, the per-team lock) are CR1 to CR8 of
[DECISIONS-lifecycle.md](DECISIONS-lifecycle.md).

- LS1. `ccy-teams shutdown` types `/exit` into a running container's `agent-bus-claude`
  session through `lxcfreeze.Lister.InContainer` (F3), as wind-down does, instead of naming
  every LXC member as not reachable. A container stopped at the shutdown is offline: named, not
  a failure (CR1).

- LS2. Why a typed `/exit` is not enough for a `service: true` member: its unit
  `agent-bus-claude-<user>.service` has `Type=forking`, `ExecStop=... kill-server`,
  `Restart=always` and `RestartSec=10s` (`helpers/agent_bus_lxc/members.py`
  `render_member_unit`). claude's exit ends the tmux server, so the main process, and systemd
  starts a fresh conversation 10 s later, which a later `--continue` would take. A
  `systemctl stop` run after the `/exit` races that restart, since every read of the session
  is one `sudo lxc-attach`.

- LS3. **The hold.** Before the `/exit`, the shutdown writes the hold drop-in
  `agent-bus-claude-<user>.service.d/ccy-teams-hold.conf` in the unit's directory
  (`GUEST_UNIT_DIR`, `/etc/systemd/system`) and runs `systemctl daemon-reload`. It holds:

  - a comment line naming the team whose shutdown wrote it;
  - `[Unit]` `ConditionPathExists=!<its own path>`, so no start (an apply's, a container boot's
    or a restart's) starts the unit while the file exists;
  - `[Service]` `Restart=no`, so the `/exit` (or the hard stop) leaves the unit inactive with
    no restart to race.

  It is under `/etc`, not `/run`, so a container reboot keeps it (Decision 24 names the
  alternatives). The file name is one constant, `HOLD_DROPIN` in members.py and
  `lxcfreeze.HoldDropIn` in Go, each pinned to the one value by a test against a shared
  fixture under `src-go/internal/bus/declare/testdata/parity/lxc/`.

- LS4. The hold is written as root through the root runner (LS9) by
  `install -D -m 0644 /dev/stdin <path>`, the text on stdin, and removed by `rm -f -- <path>`
  followed by `systemctl daemon-reload`. No Python text is copied into Go; members.py keeps its
  own `UNIT_WRITE_PY` and `UNIT_REMOVE_PY` pattern.

- LS5. After the `/exit` it runs `systemctl stop` of the unit (nothing to do once inactive; the
  end of the hard stop otherwise). The member counts as ended only when the unit is inactive
  and its `NRestarts` is unchanged since before the `/exit`. Otherwise the report says
  "restarted before it was stopped: its newest conversation may be a fresh one", and the member
  is a failure (exit 1). A start of a held unit returns success and leaves it inactive, so no
  step reads "started" or "stopped" from `systemctl`'s exit alone: it reads `ActiveState`.

- LS6. **Off the success path the hold is removed.** On every path where the member is not
  ended (the `/exit` or the hard stop failed, its `lxc-attach` failed, a `--cancel` stopped the
  forced phase before it, or it was not reached after the write), the shutdown removes the hold
  and runs `daemon-reload` before it reports, so a running member keeps `Restart=always`. A
  removal that fails is a failure naming the member and the drop-in's path (exit 1).

- LS7. **An apply honours the hold.** `_ensure_service` (members.py) starts any inactive unit
  on every play-agent-bus apply today (its `elif state["ActiveState"] != "active"` branch). With
  the hold present it writes and reloads a changed unit and enables it as before, but does not
  start or restart it, and reports
  `<label> held stopped by a ccy-teams shutdown of <team>; ccy-teams resume <team> starts it`.
  The play does not fail. `_drop_service` removes the hold with the unit. The board and
  `ccy-teams apply` show the member as held. members.py and its tests change with 3.15.

- LS8. For each LXC member ended, the last-shutdown record's entry (RS1) also records the name
  and mtime of the newest transcript in the member's store (Decision 23: the ccy store of its
  checkout, or the user's own store for a service member), read-only through the user runner.

- LS9. **Two runners.** `lxcfreeze.Lister.InContainer` runs tmux only, on the socket and session
  `agent-bus-claude` (`winddown.go` `lxcTmux`), and stays the route for reading and typing. The
  hold, `systemctl`, `agent-bus seat list` (3.17), the transcript read and the resume command
  under the default connect (3.18) need a new `lxcfreeze` runner: a named program run inside
  the container, as root or as the member's user, through the same
  `sudo -n lxc-attach --clear-env`, each program on an allow list (SPEC-lxc-connect-and-resume
  K10).

- LS10. **The preview names what else ends.** A service member's one session is in every team
  its user is declared in (one session per container and user, members.py `_check_between`),
  so its `/exit` and `systemctl stop` take it off those teams too. The preview (3.15's and
  3.16's) shows "also on <team>" for it, as SD4 does for a desktop session.

## 6. A timed wind-down that ends in a forced shutdown (Task 3.16, draft expectations)

Draft, as section 5. Decisions 14 to 17 and 21 of
[DECISIONS-lifecycle.md](DECISIONS-lifecycle.md) are its open defaults.

- TW1. `ccy-teams wind-down <team> --then-shutdown[=<minutes>]` and a team menu line, Wind down
  and shut down the team: an outer window (default 15 minutes, 30 offered in the picker) and an
  inner coordinator deadline inside it (Decision 14).
- TW2. 3.14's typed path reaches the coordinator first, then every other member (Decision 15).
  Each text names the inner deadline as a clock time and says the team is shut down by force
  at it.
- TW3. Enforcement is on the host, never left to the coordinator. At the inner deadline (or
  earlier, Decision 16) Plan 00201's `ccy-teams shutdown` path (SPEC-shutdown.md SD10 to SD12:
  `/exit` typed, a busy turn interrupted, then `tmux kill-session`) runs on every session still
  open, its waits cut so that it ends inside the outer window.
- TW4. The wait runs outside the owner's terminal unless the team has an online LXC member
  (Decision 17), takes the per-team lock (CR4 to CR8), and can be cancelled.
- TW5. LXC members are wound down by 3.14's route. They are forced only once 3.15 lands (LS1
  to LS7). Until then an online LXC member is named as not forced, with the text to give it by
  hand, and is a failure (exit 1: it is online and the shutdown did not end it). Its entry is
  still written, and 3.17 judges it (RS7).
- TW6. Once 3.15 has landed, the wind-down writes each online service member's hold (LS3) at
  its start, not at the force. So a service member that ends during the window (the owner
  `/exit`s it, or claude exits) stays ended in its own conversation, instead of being started
  fresh by `Restart=always` with a newer transcript.
- TW7. An offline member is shown as offline, is not typed to or forced, and changes no exit
  code (CR1).
- TW8. **The record is a draft until the force.** 3.17's last-shutdown record is drafted at
  the wind-down's start, from the members found then, into a separate draft file beside the
  record. When the forced phase begins the draft is merged and promoted: every draft entry is
  kept, those still found are refreshed, and the result becomes the team's last-shutdown
  record. So a member that ended during the window is still in it. A draft that no lock holds
  (left by a holder that crashed) is overwritten by the next wind-down and never read by
  `resume`.
- TW9. A `--cancel` before the force, or a foreground countdown ended by a closed terminal,
  deletes the draft and leaves the previous last-shutdown record as it was, so a shutdown that
  never happened can neither replace a real one nor be resumed. It removes the holds TW6 wrote;
  a service member that ended during the window is started through its unit with the
  `continue-once` flag (SPEC-lxc-connect-and-resume K7), so the cancel leaves it in its own
  conversation.
- TW10. A hold the countdown could not remove on its way out (a closed terminal whose sudo is
  gone) is left in place. The apply and the board name it held (LS7), and the next resume of
  the team the hold names releases it (RS10).
- TW11. **Forced means forced.** At the inner deadline the cut shutdown departs from SD10 and
  SD11 for this mode only:
  - a session whose input box holds unsent text is ended by `tmux kill-session` after the
    record is written, and the report names it with its unsent text, as much of it as the pane
    shows (a long input is clipped). That text is printed to stderr only, never written to the
    record, the draft, the unit's journal or any log, since it may hold a secret; a unit run
    says only that the session had unsent text;
  - a session left at the launcher's compose-stop question is ended by `kill-session` too, the
    report naming its compose services as left as they were.
- TW12. `--cancel` before the deadline stops the unit (or the countdown) and says what was
  already typed. Once the forced phase has begun it stops before the next session not yet typed
  to or killed, and the report names which sessions ended and which were left running. The
  record already written stays; an LXC member not ended has its hold removed (LS6).
- TW13. Plan 00201's SPEC-shutdown.md carries a line pointing here, so SD10 to SD14 and this
  mode do not drift.

## 7. A compaction before the shutdown (Task 3.20, draft expectations)

The owner's request: compact each session just before it is shut down, so the next day's
resume does not reload a large conversation into a cold prompt cache. A compaction run while
the cache is warm costs a fraction of one run after a restore, which is what `ccy-sessions set-going` does today (Plan 00135 Task 8.2: `/compact` as the first input when the context is
at or above the floor). Draft, as section 5.

- WC1. Nothing new is needed upstream. The typing is 3.14's typed path into the member's pane
  at an idle moment, the same one `set-going` uses; a compaction's end is the transcript's
  `compact_boundary` entry, which `src-go/internal/transcript` already reads; and the
  hooks-daemon supervisor stands aside for a typed `/compact` (it recognises one and does not
  stack its own on top).
- WC2. After the wind-down instruction has been answered (or at the inner deadline, TW1), each
  open session whose context is at or above `set-going`'s floor is typed `/compact` followed by
  a fixed instruction: it is being wound down, should keep what it needs to resume the work
  another time, and should drop the rest. A session below the floor is not compacted.
- WC3. The shutdown waits for that session's `compact_boundary`, bounded by the outer window
  (TW1, TW3): a compaction not finished by then is reported, and the session is forced as TW11
  says. A session busy at the deadline is not compacted: TW3 interrupts it as before.
- WC4. Coordinator first, as TW2, then the others; compactions run one session at a time
  unless the window would not hold them, so the host is not loaded by several at once.
- WC5. A plain `ccy-teams shutdown` (no wind-down) does not compact unless asked
  (`--compact`), since it is the quick path.
- WC6. On resume, a compacted session is below the floor, so `set-going` types `continue`
  and no cold `/compact` is spent.
- WC7. A member with no pane the typed path can reach (none today: ccy and cc sessions are in
  tmux) is named as not compacted; reaching one would need a hooks-daemon supervisor request,
  which is out of scope until such a member exists.
