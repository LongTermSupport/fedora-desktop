# Team shutdown from ccy-teams (Task 2.14)

Branch `feat/teams-shutdown` from F44 0e53ac69. Spec: [SPEC-shutdown.md](../SPEC-shutdown.md).

## Facts established from the code

- **A team's live sessions.** ccy and cc both run in tmux on CCY's own server (`tmux -L ccy`),
  as `ccy-<project>[-N]` and `cc-<project>[-N]`, each with a session-registry record holding
  its launch arguments. ccy rewrites `--teams` to the canonical seat list before tmux starts,
  so a ccy record names its seats; cc chooses seats after tmux starts, so a cc record names
  them only when cc was given a full `<seat>@<team>` list. ccy-teams' table joins records to
  `agent-bus seat list` per checkout. LXC members run in their container's own tmux; other
  members (host, docker, vm on another machine) are only roster lines.
- **`/exit` delivery.** The inbox socket (`pingbus watch` writes to
  `CLAUDE_CODE_MESSAGING_SOCKET`) arrives as a message to the model (a user entry, or a peer
  `queued_command` during a turn, framed "Another Claude session sent a message:"; Plan 00194
  DESIGN.md §1.2, SPEC-inject.md E16). It cannot run a slash command, and it is the bus. The
  local route already in use is the pane: `ccy-sessions set-going` types with
  `tmux send-keys -l`, waits 200 ms, sends Enter (`internal/sessions/setgoing.go`). The
  container's supervisor ends a session for a restart by typing `/exit` and waiting 30 s
  (`.claude/ccy/claude-supervise.py`). So `/exit` typed through tmux is the human's own.
- **Clean stop and seats.** After `/exit` claude exits, the ccy container ends (`--rm`), the
  launcher returns, and the pane's trampoline removes the registry record and closes tmux.
  The seat is "held" while a live process holds its flock (`helpers/pingbus/seat.py`), so it
  is released by that process ending; no command releases it. The hard stop is ccy-sessions'
  End: `kill-session`, then remove the record. Two things keep tmux open after claude ends:
  the launcher's "Stop compose services?" question and the trampoline's error hold
  ("Press Enter to close this session.").

## Design

- `ccy-teams shutdown <team> [--yes]` and a team row's menu line (no shortcut). Preview first
  (sessions, other teams they are on, attached, unreachable members), question default No,
  `--yes` required with no terminal (64).
- Per session: read the pane; ready: type `/exit`, Enter after 200 ms; busy: Escape once, then
  `/exit` when ready; unsent text: failed, untouched; `/exit` left in the box: one more Enter;
  a launcher question: stopped at once (claude not running); compose question or the error
  hold after `/exit`: exited (the error hold's window is closed). Bounds: 30 s for the prompt,
  60 s after `/exit`, then the hard stop.
- Seats: every seat of each ended or stopped session is read again until free (30 s); still
  held fails the session.
- Out of reach, listed, never acted on: LXC members, other non-local members, a held seat no
  live record names, and the members when the roster could not be read. Exit 1 if any
  session failed or anything was out of reach.
- Board: cc records and `cc-` tmux sessions now count for seats and checkouts; `Row.Held`
  added. `tmuxccy.SendEscape` added.

## Tests

Go: `internal/teams/shutdown` (SD1-SD16 through the teamtest world with a stateful fake tmux,
seat list and clock), board, tmuxccy, app and picker. Bash: `test-ccy-teams-command.bash`
gained a shutdown section (no-terminal refusal, clean exit with seat release and the LXC line,
unsent text, the picker menu with No).

## Limits

- No route into an LXC container's session or another machine's: listed as not shut down.
- A `cc --teams <team>` session (team alone) has a record without its seat: listed as held by
  a session ccy-teams cannot name. Recording cc's chosen seats (as ccy's `record-seats` does)
  would close this; not done here.
- The fake proves the keystrokes, not Claude Code's reaction: the owner's check 12 is the
  live proof that `/exit` typed this way ends a session (the supervisor's own `/exit` is the
  precedent).
