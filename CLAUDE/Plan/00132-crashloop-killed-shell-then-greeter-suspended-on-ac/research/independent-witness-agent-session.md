# An independent witness: the CCY agent session's own transcript

`incident-chain.md` is built entirely from this host's journal and live state. This note
adds a **second, independent clock** that was running throughout and is not the journal: a
Claude Code session inside the CCY container, whose transcript records every turn with
millisecond timestamps.

It matters for two reasons. It brackets both key events to the second from outside the
Wayland session — a container process cannot die with the compositor, so it kept writing
while everything on the desktop was gone. And it puts a concrete cost on defect 2.

Times below are converted to **BST (UTC+1)** to match `incident-chain.md`. The container
runs UTC; `date` inside it and `/etc/localtime` both report UTC, so the conversion is a
straight +1 and is stated rather than assumed.

## The bracket

| Journal (BST)                             | Agent session (BST)                             | Delta     |
| ----------------------------------------- | ----------------------------------------------- | --------- |
| 05:52:42.441 `gnome-shell`: Shutting down | first turn after a 9 h idle gap at **05:52:50** | **+8 s**  |
| 05:52:52 greeter session opens            | session working normally                        | —         |
| 06:07:53 `systemd-sleep`: suspend         | last turn before a 2.6 h gap at **06:07:51**    | **−2 s**  |
| 08:38:13 return from suspend              | —                                               | —         |
| 08:39:01 new login session                | first turn after the gap at **08:41:44**        | +2 m 43 s |

Two boundaries, both within ten seconds of a journal event, from a clock that is not the
journal. The 06:07:51 turn is the session being cut off mid-write by the suspend; nothing
in the transcript anticipates it.

## What this adds to the chain

**The greeter window is directly observed.** Between the shell's death and the suspend the
agent session ran continuously for just under fifteen minutes — 05:52:50 to 06:07:51. That
is the greeter's idle countdown elapsing in real time, from a process that had no idea the
desktop was gone. It corroborates the 900-second reading in `greeter-power-policy.md`
against an unrelated clock: 06:07:53 − 05:52:52 = 901 s.

**The wake at 05:52:50 is NOT explained here.** A message arrived in the session eight
seconds after the shell exited, ending a nine-hour idle period. That could be the
supervisor reacting to the session's terminal dying, or a coincidence of scheduling. This
note does not claim to know which, and the eight-second coincidence is suggestive enough
that somebody will be tempted to assert a cause — so: **unestablished**, and it would need
the supervisor's own logs to settle.

## Why defect 2 costs more than an idle machine

The plan's overview describes the greeter suspending "while it was plugged in". What the
transcript shows is that the suspend **terminated an active, unattended agent run
mid-task** — not an idle desktop.

The session was working when it was cut off: it had been committing plan work continuously
through the preceding window, and the suspend landed between a tool call and its result.
The work resumed only when a human logged back in at 08:39. So the practical effect of
`sleep-inactive-ac-type` inverting for the `gdm` account is **2 h 30 m of unattended
machine time lost**, on a machine that was plugged in precisely so it could keep working.

That is worth stating in the plan's impact, because "the laptop suspended overnight" and
"the laptop suspended and killed a running job" argue for different urgency.

## Detection

`incident-chain.md` records that ten hours of runaway restarts produced no signal. The
observation here is narrower and more useful: **a surviving observer already existed**. The
CCY container is outside the session bus's blast radius, it keeps a timestamped log, and it
was running throughout. Anything the host-health surface wants to notice about a desktop
that has died — as opposed to a machine that has rebooted — has a place to notice it from
that does not itself die with the compositor.

Phase 3 settled where the check goes, and it is **not** here: it extends plan 00055's
container watchdog, which runs on the host every two minutes — see
[detection-gap.md](detection-gap.md#where-it-belongs-extend-plan-00055-do-not-build-anything-new).
A container-hosted observer survives the outage but has no route to tell anyone about it,
which makes it a good witness and a poor alarm.

The fact worth carrying forward is that the blast radius of the session bus stops at the
container boundary, and this incident demonstrates it rather than assuming it.

## How to reproduce this reading

The session transcript is JSONL, one object per turn, each with an ISO-8601 `timestamp`.
Sort by timestamp and diff consecutive entries to find the gaps; the two that matter here
are 9.15 h and 2.56 h. No host state is involved, so this is re-derivable from the
transcript alone at any time.
