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
- D4. **A member out of reach is a failure, not a skip** (exit 1): an LXC member whose container
  is stopped or was held back, or that no LXC member record of this host names, or any LXC
  member when sudo is not ready (F3); members on another machine; a held seat no record names; a
  team whose members could not be read. An LXC member in a running container whose service
  session is missing is a failed job. Each is named with how to wind it down by hand, and the
  instruction text is printed once so it can be pasted. sudo is asked for only when the team
  has an LXC member that can be reached. A stopped session, a free seat and a parked member have no agent to tell: each is
  named on its own line and is not a failure.

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
- `ccy-teams shutdown` still leaves LXC members out; the same path would reach them (a plan
  task of its own).
