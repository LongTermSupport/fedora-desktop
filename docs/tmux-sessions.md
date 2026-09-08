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

Sessions are transient dev state by design. Nothing restarts them after a reboot.

## Why the tmux status bar is off

Claude Code draws its own status line at the bottom of the terminal. A tmux bar would sit
one row below it and cost a row for nothing, since the F12 menu already shows session
names. `tmux ls` prints them in a plain shell.

## Anything else

The standard tmux prefix (`Ctrl-b`) still works for anyone who wants panes or windows;
nothing in the config depends on it.
