# tmux Sessions — leave work running, come back to it with one key

`play-tmux-sessions.yml` installs tmux with a system-wide config
(`files/etc/tmux.conf`) that hides the multiplexer behind a single key. There are no
panes to learn and no prefix chords to remember.

## The three things you need

```bash
tmux new -s NAME     # start a named session and land in it
tmux attach          # get back into the most recent session
```

Inside a session, press **F12**. A menu appears:

| Item              | Key | What it does                                                  |
| ----------------- | --- | ------------------------------------------------------------- |
| New session       | n   | asks for a name, starts a fresh shell in it                   |
| Rename session    | r   | asks for a new name for the current session                   |
| Switch session    | s   | lists live sessions; arrow keys and Enter to jump full-screen |
| Detach            | d   | leaves the session running and returns you to your shell      |
| Kill this session | k   | ends the session and everything running in it (asks first)    |

Escape closes the menu. Everything else is your normal terminal: `cd`, start `ccy`,
attach to an LXC container, run a long job. Mouse scrolling scrolls the session.

## What survives what

| Event                          | Session       |
| ------------------------------ | ------------- |
| SSH connection drops or closes | keeps running |
| Laptop sleeps, network changes | keeps running |
| You detach (F12, Detach)       | keeps running |
| Host reboots                   | gone          |

Sessions are transient dev state by design. Nothing restarts a plain tmux session after a
reboot. `ccy` and `cc` sessions are the exception, on a machine that has opted in: they
are recorded while they run and started again at boot, detached, in the same project
directory — see [CCY: Sessions Survive a Reboot](ccy.md#sessions-survive-a-reboot). Off by
default, so a machine that has not opted in behaves exactly as this table says.

## CCY sessions have their own server

`ccy` puts every interactive session into tmux automatically, on a separate server so that
no session ever lands in a server a plain tab happened to start. Inside one, F12 and
everything above work the same. A plain `tmux ls` does not list them: `ccy-sessions` is
the menu for those — list, attach a detached one, end one — and `ccy` in a project directory
offers its own detached session. The full behaviour, including the one-terminal-per-session
rule, is in [CCY: Sessions Survive the Terminal](ccy.md#sessions-survive-the-terminal).

## How you know you are inside one

The terminal's window title. With the status bar off it is the one visible sign, so the
config sets it to `tmux: <session>  (F12 then Detach leaves it running)` for every
session — `ccy-<project>`, `cc-<project>`, or the name you gave `tmux new -s`.

## Why the tmux status bar is off

Claude Code draws its own status line at the bottom of the terminal. A tmux bar would sit
one row below it and cost a row for nothing, since the F12 menu already shows session
names. `tmux ls` prints them in a plain shell.

## Anything else

The standard tmux prefix (`Ctrl-b`) still works for anyone who wants panes or windows;
nothing in the config depends on it.
