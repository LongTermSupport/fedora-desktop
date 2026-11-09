# Triage: all terminals died at 2026-09-13 13:23:45 BST

Untracked working notes. Verdict first, evidence after. Cherry-pick into Plan 00111.

## Verdict: NOT an OOM kill

The event was a **Wayland protocol error that killed the Ptyxis process**, and
Ptyxis is a single process hosting every terminal window and tab. One protocol
error therefore destroyed all six terminals simultaneously.

### Why OOM is ruled out — four independent negatives

| Check                                 | Command                                                                       | Result                                                                                  |
| ------------------------------------- | ----------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Kernel OOM killer                     | `journalctl -b -k` grep `out of memory\|oom-kill\|oom_reaper\|Killed process` | **zero matches** in 3352 kernel lines                                                   |
| `systemd-oomd` (userspace OOM killer) | `journalctl -b -u systemd-oomd`                                               | **2 lines total** — its own startup. No kill, no pressure event                         |
| Coredumps                             | `coredumpctl --since=-24h list`                                               | **No coredumps found** — nothing died on a signal                                       |
| Memory headroom                       | `free -h` / `zramctl`                                                         | 93 GiB total, 20 GiB used, 73 GiB available. zram swap 64 MiB of 8 GiB used (**0.8 %**) |

A machine with 73 GiB available and a swap device at 0.8 % occupancy is not
under memory pressure. Nothing was killed for memory.

## Actual root cause

```
13:23:45  gnome-shell[6310]: WL: error in client communication (pid 13312)
13:23:45  ptyxis[13312]:     Error reading events from display: Invalid argument
13:23:45  systemd[2266]:     ptyxis-spawn-6025dbd6-….scope: Consumed …   ← tab teardown
13:23:45  systemd[2266]:     ptyxis-spawn-2cef3093-….scope: Consumed …
13:23:45  systemd[2266]:     ptyxis-spawn-0274a76e-….scope: Consumed …
13:23:45  systemd[2266]:     ptyxis-spawn-f4b4924e-….scope: Consumed …
13:23:47  systemd[2266]:     dbus-:user@example.com: Main process exited, code=exited, status=1/FAILURE
13:23:47  systemd[2266]:     dbus-:user@example.com: Failed with result 'exit-code'
13:23:47  systemd[2266]:     ptyxis-spawn-88a89bd5-….scope: Consumed …
13:29:31  systemd[2266]:     Started dbus-:user@example.com   ← user relaunched
```

Read in order: **mutter severed the Wayland connection first**
(`WL: error in client communication`), Ptyxis then failed its socket read
(`Invalid argument` = EINVAL on the Wayland fd) and exited `1`. The direction
matters — the compositor rejected the client, the client did not crash on its
own. `status=1/FAILURE` and the absence of a coredump confirm a clean
`exit(1)`, not a segfault.

The compositor itself was never affected: `gnome-shell` pid 6310 has been alive
since `Fri Sep 11 11:16:45`, i.e. it did **not** restart. The session survived;
only this one client was cut loose.

### Blast radius — why one error took every terminal

Ptyxis runs single-instance: one process (`ptyxis --gapplication-service`)
owns all windows and tabs, each tab's shell living in its own transient
`ptyxis-spawn-<uuid>.scope`. The scopes are children of the Ptyxis process, so
when it exits they are all torn down together. Six scopes died in those two
seconds.

Peak memory of the dead tabs, from the scope teardown lines — useful as a
record that these were long-lived working sessions, not that they were large:

| Scope         | Wall clock | CPU         | Peak RSS |
| ------------- | ---------- | ----------- | -------- |
| `6025dbd6`    | 1d 19h     | 9.9 s       | 600.9 M  |
| `2cef3093`    | 1d 21h 51m | 1.6 s       | 18.7 M   |
| `0274a76e`    | 1d 21h 38m | 1 m 8.6 s   | 1.0 G    |
| `f4b4924e`    | 1d 20h 46m | 1 m 54.8 s  | 1.2 G    |
| `88a89bd5`    | 1d 23h 43m | 3 m 7.3 s   | 1.1 G    |
| Ptyxis itself | 2d 2h 6m   | 19 m 45.3 s | 852.9 M  |

Aggregate peak is ~4.9 GiB against 93 GiB of RAM — another angle on the same
point: memory was never the constraint.

### Frequency: first occurrence on record

`journalctl --grep='WL: error in client communication'` across **all five
recorded boots** returns exactly one hit — this one. This is not a recurring
fault; it is a first-time event after Ptyxis had been running continuously for
2 days 2 hours.

## Environment

| Component        | Version          |
| ---------------- | ---------------- |
| ptyxis           | `50.1-2.fc44`    |
| vte291           | `0.84.1-1.fc44`  |
| gtk4             | `4.22.4-2.fc44`  |
| mutter           | `50.4-1.fc44`    |
| gnome-shell      | `50.4-1.fc44`    |
| kernel (running) | `7.2.4-200.fc44` |

`ptyxis[1643440]` logged `The new GL renderer has been renamed to gl. Try GSK_RENDERER=help` on relaunch — worth checking whether a `GSK_RENDERER` value
is set anywhere in the deployed environment, since a stale renderer name is a
plausible contributor to a GTK4/Wayland protocol fault. **Not yet verified.**

## CCY session survival — partial, better than feared

Three of the four CCY containers **survived** the terminal death and still have
live `claude` processes:

| Container                          | Claude PID | Started         | Still up               |
| ---------------------------------- | ---------- | --------------- | ---------------------- |
| `lts-property_yolo`                | 501461     | Sep 11 12:02:45 | yes                    |
| `ec-claude-code-hooks-daemon_yolo` | 1204504    | Sep 11 12:59:19 | yes                    |
| `fedora-desktop_yolo`              | 1731869    | Sep 11 18:20:19 | yes                    |
| `family-qnap_yolo`                 | —          | —               | **killed and removed** |

`family-qnap_yolo` took a `container kill` at 13:23:45 in the same second and
is gone from `podman ps -a` entirely.

The three survivors are in a **half-dead state**: `ps` shows `TTY = ?` with
state `Ssl+` parked in `ep_poll`. They are alive but have no controlling
terminal, so there is **nothing to re-attach to** — a podman container that
survives is not a session that can be resumed. Their value is that Claude Code's
on-disk transcripts are intact, so `--continue` can recover the conversation;
the in-flight turn itself is lost.

### Why three survived and one did not — resolved

All four containers hit the same console failure the instant their pty vanished:

```
13:23:45 conmon[1170662]: conmon a60224f2dc8f… <nwarn>: Failed to write to remote console socket   ← survived
13:23:45 conmon[1729365]: conmon bb073b3a99e3… <nwarn>: Failed to write to remote console socket   ← survived
13:23:45 conmon[501011]:  conmon 8453a8dbcd64… <nwarn>: Failed to write to remote console socket   ← survived
13:23:45 podman[3892681]: container kill   f6b77e1f66f5…  (name=family-qnap_yolo)                  ← destroyed
13:23:45 podman[3892681]: container died   f6b77e1f66f5…
13:23:47 podman[3892681]: container remove f6b77e1f66f5…
```

The console failure is **not** what destroyed `family-qnap_yolo`. The difference
is the **foreground `podman run` client**:

- For the three survivors, no `podman` client process remains. Only `conmon`
  (pids 501011, 1170662, 1729365), and critically **`conmon`'s ppid is 2266 —
  `systemd --user`**, living in its own `libpod-*.scope`, not in any
  `ptyxis-spawn-*.scope`. It had already been reparented out of the tab, so the
  tab's death could not reach it.
- For `family-qnap_yolo`, the `podman` client (pid 3892681) was **still alive
  inside a dying tab**. On losing its terminal it executed its normal
  `--rm` teardown: kill, then remove. The container was not a casualty of the
  crash — podman deliberately destroyed it, exactly as instructed.

So container survival here is **incidental, not designed**: it depended purely
on whether a `podman run --rm` client happened to still be attached.

**And survival bought nothing.** All three survivors sit at `TTY = ?`, `Ssl+`,
parked in `ep_poll` — alive with no controlling terminal and no listener on
their stdio. There is no pty to re-attach to, so `podman attach` reaches a
process that can no longer be driven. This is the decisive finding: **detaching
the container is a red herring for preserving work.** The thing that must
outlive the terminal is *the pty itself*, not the container. Option 3 below is
therefore ruled out as a work-preservation measure.

The per-session process chain confirms where the pty sits:

```
ptyxis-spawn-<uuid>.scope → bash → podman run → [container] → claude-supervise.py (pts/0) → claude
```

`claude` and the `claude-supervise.py --arm` supervisor above it both hold
`pts/0` — a pty allocated by `podman run -t` and wired to the tab. Kill the tab
and that pty is gone, whatever happens to the container around it.

## The real defect to defend against

The Wayland error is the trigger, but the trigger is not the problem worth
fixing — it happened once in five boots and it is upstream code.

**The structural problem: CCY sessions are children of terminal-emulator
tabs, and every terminal shares one process.** Any death of that process —
this protocol error, a GTK bug, a GPU reset, an accidental window close, a
package upgrade restarting it — loses every in-flight session at once. The
single point of failure is architectural and entirely within our control,
unlike the Wayland bug.

`tmux` is installed at `/usr/bin/tmux` and **no tmux server was running** at
the time of the event, so nothing was insulated. `screen`, `zellij`, `abduco`
and `dtach` are all absent.

Candidate defences, for the plan to evaluate:

1. **Run `ccy` inside a persistent `tmux` session.** The tmux *server* is a
   daemon owned by `systemd --user`, not by the tab, and it owns the pty. The
   tab becomes a viewport: when Ptyxis dies the pty survives inside the server
   and `tmux attach` restores the live session, mid-turn work included. This is
   the only candidate that preserves the pty, which the evidence above shows is
   the thing that actually matters.
2. **Run each CCY session as a transient `systemd --user` unit**
   (`systemd-run --user --pty`). Equivalent decoupling with no tmux dependency,
   but re-attaching to a `systemd-run` pty after the client dies is awkward,
   and it gives up tmux's scrollback and multi-window handling.
3. ~~Detach the container console from the tab~~ — **ruled out**. Proven above
   to preserve the container while losing the session, which is not a defence.

Option 1 is the recommendation. tmux is already installed, needs no new
packages, and is the only option shown to protect the failure path actually
observed. The open design questions for the plan are where the wrapping belongs
(a host-side `ccy` wrapper versus the user's own habit), how sessions are named
so they can be found again after a crash, and whether a stale-session reaper is
needed to stop abandoned tmux sessions accumulating.

### Secondary hardening, worth considering separately

`podman run --rm` in a tab means **any** tab death destroys that container
outright, as `family-qnap_yolo` demonstrates. Even with tmux in place this is
worth revisiting, because `--rm` converts a recoverable interruption into an
unrecoverable one. This is independent of the pty question and should not be
conflated with it.

### Immediate recovery position

Claude Code's on-disk transcripts are intact for all four projects, so
`ccy --continue` (or `claude --continue`) recovers each conversation up to the
last completed turn. What is lost is the in-flight turn in each session. The
three orphaned `claude` processes should be reaped before restarting, since they
can no longer be driven and still hold ~1.8 GiB of RSS between them.

## Captured artefacts in this directory

| File                    | Contents                                                |
| ----------------------- | ------------------------------------------------------- |
| `kernel-thisboot.log`   | full kernel ring for boot 0 — the OOM negative          |
| `oomd-unit.log`         | `systemd-oomd` unit log — the second OOM negative       |
| `coredumps.log`         | `coredumpctl` output — the signal-death negative        |
| `wl-errors-history.log` | `WL: error in client communication` across all boots    |
| `death-window.log`      | full journal 13:23:30–13:23:55, 755 lines               |
| `window-1320-1332.log`  | full journal 13:20–13:32, 17 579 lines                  |
| `ptyxis-unit-today.log` | Ptyxis unit journal for today                           |
| `last4h-warn.log`       | warning-and-above for the 4 h before triage             |
| `cgls-app-slice.txt`    | `systemd-cgls` of `app.slice`, showing the scope layout |

A 2.0 GB full-boot journal capture was taken and deleted as unusable.

## Incidental findings, not part of this fault

- Many SELinux AVC denials from CCY containers writing to `user_home_t`
  (`thread-registry`, `verdicts.jsonl`, `.git` commit-graph files), all in
  `permissive=1` so nothing was blocked. Unrelated to the terminal death but
  they are real policy gaps.
- `gnome-shell` logs `Cursor update failed: drmModeAtomicCommit: Invalid argument` at 12:16:21 and a steady stream of `Can't update stage views actor … needs an allocation` warnings from dash-to-dock. The `drmModeAtomicCommit`
  EINVAL is ~67 minutes before the event and shares its errno; treated as
  **unrelated** absent evidence linking them, but noted because a display-stack
  EINVAL preceding a Wayland EINVAL is not a comfortable coincidence.
