# Plan 00201 spec: shut down every session of a team from ccy-teams

Task 2.14. The owner asked to "pass /exit to all members of a team, so i can easily shut down
a whole team from ccy-teams with a single command". Placeholders only: teams `alpha`, `beta`;
checkout `~/proj`; sessions `ccy-proj`, `cc-proj`; handles `proj.1+local.podman`. Exit codes
are ccy-teams' own: 0, 1 refusal or failure, 64 usage, 130 cancelled.

## Facts this is built on (read from the code)

- **Which sessions are a team's.** A ccy session runs in a tmux session on CCY's own server
  (`tmux -L ccy`, `lib/tmux-session.bash`), named `ccy-<project>[-N]`; `cc` runs on the same
  server as `cc-<project>[-N]`. Each has a record in the session registry
  (`~/.local/state/ccy/sessions/<name>`, `lib/session-registry.bash`) holding its launch
  arguments. ccy writes the canonical `--teams <seat>@<team>,...` list into those arguments
  before tmux starts (`claude-yolo`, `ccy_teams_canonical_args`), so a ccy record names its
  seats. cc chooses its seats after tmux starts (`cc-teams.bash`), so a cc record names its
  seats only when it was launched with a full `<seat>@<team>` list. ccy-teams' table already
  joins the records' seats to `agent-bus seat list` per checkout (`board.Snapshot.SeatSession`).
- **What a seat's "held" is.** `agent-bus seat list` says `held` while a live process holds
  the seat's lock (`helpers/pingbus/seat.py`, `seat_state`: an flock on `seat.lock`). The lock
  is the session's own process (the watcher in a ccy container; `pingbus seat exec`, which
  becomes claude, for cc), so the seat is released by the kernel when that process ends. No
  command releases a seat; a normal exit releases it by ending.
- **LXC members** run `ccy --teams` inside their container, on that container's own tmux, out
  of reach of the host's `tmux -L ccy`. Members of type `host`, `docker` or `vm` whose
  checkout this host does not know are on another machine or outside ccy's tmux. ccy-teams
  sees them only as roster lines (`agent-bus list`).
- **How a session can be told `/exit`.** Two routes exist, and only one types a command:
  - The **inbox socket** (`CLAUDE_CODE_MESSAGING_SOCKET`, written by `pingbus watch`) is a
    message to the model, not keyboard input. Claude Code records it as a user entry, or a
    `queued_command` attachment of origin `peer` during a turn, framed "Another Claude session
    sent a message:" (Plan 00194 DESIGN.md §1.2, SPEC-inject.md E16). It cannot run a slash
    command, and it is the bus, which this feature must not use.
  - **Typing into the pane** through tmux is how ccy already drives a session:
    `ccy-sessions set-going` types `/compact ...` or `continue` with `tmux send-keys -l`, waits
    the submit delay, then sends Enter (`internal/sessions/setgoing.go`, `typeInto`), after
    reading the pane with `capture-pane` and `screen.ClaudeState`. The container's supervisor
    ends a session for a restart by typing `/exit` into claude's terminal and waiting 30 s
    (`.claude/ccy/claude-supervise.py`, `_RESTART_EXIT_COMMAND`). Typing `/exit` through tmux
    is therefore the human's own `/exit`, keystroke for keystroke.
- **What ends after `/exit`.** claude exits; in a ccy session the container ends with it
  (`--rm`), the launcher returns, and the pane's trampoline removes the registry record and
  closes the tmux session (`ccy_registry_trampoline`). Two things can keep the tmux session
  open after claude has ended: the launcher's "Stop compose services? [Y/n]" question (only
  when ccy started compose services that session), and, on a non-zero exit, the trampoline's
  "... Press Enter to close this session." (`screen.EndedText`).
- **The hard stop** is ccy-sessions' End: `tmux kill-session`, then the registry record
  removed (a killed pane never reaches its trampoline). Killing the session hangs up the
  podman client, which tears the container down (`lib/tmux-session.bash`, WHY).

## Expectations

### Entry points

- **SD1** `ccy-teams shutdown <team> [--yes]`. No team, a second team, or an unknown option is
  a usage error (64). A team that is not on this machine's table (`board`) is refused (1),
  naming `ccy-teams --list`. `--yes` may come before or after the team.
- **SD2** In the picker, the menu of a **team row** has `Shut down every session of <team>`
  (no shortcut). It runs SD3 to SD14 for that team with the picker's yes/no, then waits for
  Enter and shows the list again.
- **SD3** Shutdown is local only. It acts through `tmux -L ccy`, the session registry and
  `agent-bus seat list` (as the user). It sends nothing on the bus or to Matrix, and nothing
  received from the bus can start it.

### What is shut down, shown first

- **SD4** The team's **sessions** are the live tmux sessions that a registry record names for
  a seat of the team (records of prefix `ccy` and `cc`), one entry per session however many of
  its seats are the team's. A session that also holds seats on other teams is shown with them
  ("also on beta (seat 2)"): ending it takes it off those teams too.
- **SD5** **Not reachable from here** (listed, never acted on, never counted as done):
  - a seat that `agent-bus seat list` says is held but that no live session's record names:
    "held by a session ccy-teams cannot name (a cc session started with --teams <team>
    alone, or one outside ccy's tmux); attach it (ccy-sessions) and type /exit";
  - a roster member of type `lxc`: "its session runs inside the LXC container, out of reach
    of this host's tmux; in the container, type /exit in its session";
  - any other roster member that is not a seat of a checkout known here (`host`, `docker`,
    `vm`, or `ccy` with no checkout): "not a session on this host's ccy tmux (another
    machine, or a checkout ccy-teams does not know); end it where it runs";
  - the team's members when the roster could not be read (sudo, homeserver down): "the team's
    members could not be read (<why>), so members outside this host's checkouts are not
    listed".
    Parked members are not listed: a parked member has no session.
- **SD6** The preview, on stderr, lists each session (name, seat and handle, checkout, "open
  on a terminal" when a client is attached, other teams), then the unreachable members with
  their reason, then one line: each session is sent `/exit` as if typed; one that has not
  ended in time is stopped; the homeserver keeps running.
- **SD7** With no session and nothing unreachable: "Team alpha has no running session.",
  exit 0, no question. With no session but something unreachable: the preview, no
  question, exit 1.

### The confirmation

- **SD8** The question defaults to No: "Shut down N session(s) of alpha?" with the one row
  "Yes, shut down N session(s)". No, Esc or q: "Nothing was shut down.", exit 0.
- **SD9** `--yes` skips the question. Without `--yes`, with no terminal on stdin and stderr,
  it is a usage error (64) that names `--yes`, and nothing is touched (InteractiveScripts).
  The preview is still printed.

### Clean first

- **SD10** Each session's pane is read (`capture-pane`, `screen.ClaudeState`), all sessions in
  one pass, then again every poll (1 s) until each is done:
  - **ready** (claude's prompt, input box empty): `/exit` is typed (`send-keys -l /exit`), then
    Enter after the submit delay (200 ms). Its exit wait (60 s) starts.
  - **busy** (a turn running): Escape is pressed once, as a person interrupts a turn, and the
    pane is read again until it is ready (prompt wait, 30 s).
  - **text in the input box**, before `/exit` was typed: nothing is typed over it; the
    session **failed**: "its input box holds unsent text; attach it (ccy-sessions), send or
    clear it, and run shutdown again". It is not stopped.
  - **text in the input box** after `/exit` was typed (the completion menu took the Enter):
    Enter is pressed once more.
  - **not drawn** (starting, a dialog): read again until ready (prompt wait).
  - **at a launcher question** (`screen.KnownPrompts`) other than compose-stop: claude is not
    running; the session is **stopped** at once (SD12), "it was at the launcher's <name>
    question, so claude was not running".
- **SD11** A session is **exited cleanly** when, after `/exit` was typed, its tmux session is
  gone; or its pane shows the launcher's compose-stop question (claude has ended; the line
  adds "its launcher asks whether to stop compose services: attach it (ccy-sessions) to
  answer", and the session is left at that question); or its pane shows `screen.EndedText`
  (claude has ended, the launcher reported an error: the window is closed with
  `kill-session`, as its Enter would). A session that ends by itself before `/exit` was typed
  is exited too ("ended by itself").
- **SD12** The **hard stop**, only when the session has not ended within its wait: `tmux kill-session`, then the session's registry record removed, as ccy-sessions' End. The line
  says why: "did not end within 60 s of /exit" or "its prompt was not ready within 30 s". A
  session already gone at the kill is exited, not stopped.
- **SD13** Each step that fails (a tmux or registry error) makes that session **failed**, with
  the tool's words; the other sessions carry on.

### Seats

- **SD14** After the sessions, every seat each exited or stopped session held (on every team)
  is read again with `agent-bus seat list` in its checkout until none is held (seat wait,
  30 s, every poll). A seat still held then makes that session failed: "seat 1@alpha is still
  held N s after: something of the session still runs (podman ps)". A seat list that cannot
  be read makes its sessions failed, with the reason. Nothing releases a seat by hand: the
  seat is released by the session's process ending, exactly as on a normal exit. The team's
  homeserver is not touched.

### The result

- **SD15** One line per session on stderr: `<session>: exited cleanly`, `<session>: stopped: <why>`, or `<session>: failed: <why>`, each followed by the seats it held, "released"; then
  one line per unreachable member, `<member>: not shut down: <why>`. Then a count line.
- **SD16** Exit 0 only when every session exited cleanly or was stopped, every seat is
  released and nothing was unreachable; otherwise 1. No member is skipped silently.

## Limits, stated plainly

- `/exit` is typed, so it is delivered only to a session whose terminal this host owns: a ccy
  or cc session on `tmux -L ccy`. An LXC member, a session on another machine, and a cc
  session outside ccy's tmux are listed as not shut down (SD5), never stopped from here: the
  host has no clean route into them, and no hard stop that would not reach into the
  container or machine behind the owner's back.
- A cc session started with `cc --teams <team>` (a team alone, the chooser's form) has a
  record without its seat, so it is listed as held by a session ccy-teams cannot name.
- A session with unsent text is left alone (SD10) rather than having that text destroyed.
