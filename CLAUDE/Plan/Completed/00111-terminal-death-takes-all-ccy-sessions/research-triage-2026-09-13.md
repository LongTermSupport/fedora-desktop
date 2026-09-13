# Triage: every terminal died at 2026-09-13 13:23:45 BST

Evidence and reasoning behind Plan 00111. Raw captures live outside git in
`untracked/triage/oom-terminals/`; everything load-bearing is reproduced here.

## The reported cause was wrong

The event presented as an OOM kill — every terminal vanishing at once, under a
heavy multi-session workload, is exactly what memory exhaustion looks like from
the outside. It was not OOM, on four independent grounds:

| Check             | Source                                                                      | Result                                                           |
| ----------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| Kernel OOM killer | `journalctl -b -k`, grepped for `out of memory`/`oom-kill`/`Killed process` | **zero matches** across 3352 kernel lines                        |
| `systemd-oomd`    | `journalctl -b -u systemd-oomd`                                             | **2 lines total**, both its own startup — no kill, no pressure   |
| Coredumps         | `coredumpctl --since=-24h list`                                             | **none** — nothing died on a signal                              |
| Memory headroom   | `free -h`, `zramctl`                                                        | 93 GiB total, 20 GiB used, **73 GiB available**; zram swap 0.8 % |

Each of these is sufficient alone. A machine with 73 GiB free and a swap device
at 0.8 % occupancy is not under memory pressure, and a userspace OOM killer that
logged nothing did not act.

This matters beyond correcting the record: an OOM diagnosis would have led to
memory limits, `MemoryMax=` on the ccy slice, or reducing concurrency — all
useless against the actual fault, and all of which would have degraded the
workstation for nothing.

## What actually happened

```
13:23:45  gnome-shell[6310]: WL: error in client communication (pid 13312)
13:23:45  ptyxis[13312]:     Error reading events from display: Invalid argument
13:23:45  systemd[2266]:     ptyxis-spawn-6025dbd6-….scope: Consumed …      ← tabs tear down
13:23:45  systemd[2266]:     ptyxis-spawn-2cef3093-….scope: Consumed …
13:23:45  systemd[2266]:     ptyxis-spawn-0274a76e-….scope: Consumed …
13:23:45  systemd[2266]:     ptyxis-spawn-f4b4924e-….scope: Consumed …
13:23:47  systemd[2266]:     dbus-:1.2-org.gnome.Ptyxis@0.service: Main process exited, code=exited, status=1/FAILURE
13:23:47  systemd[2266]:     dbus-:1.2-org.gnome.Ptyxis@0.service: Failed with result 'exit-code'
13:23:47  systemd[2266]:     ptyxis-spawn-88a89bd5-….scope: Consumed …
13:29:31  systemd[2266]:     Started dbus-:1.2-org.gnome.Ptyxis@1.service   ← relaunched by hand
```

The order is the finding. **mutter severed the connection first** — the
compositor rejected the client. Ptyxis then failed its next socket read
(`Invalid argument`, i.e. `EINVAL` on the Wayland fd) and exited `1`. Ptyxis did
not crash of its own accord; it was cut loose and shut down cleanly, which
`status=1/FAILURE` plus the absent coredump together confirm.

The compositor was never affected. `gnome-shell` pid 6310 has run continuously
since `Fri Sep 11 11:16:45` — it did **not** restart. The Wayland session
survived intact; exactly one client was dropped.

### Why one dropped client cost every terminal

Ptyxis is single-instance: one process (`ptyxis --gapplication-service`) owns
every window and every tab, with each tab's shell in its own transient
`ptyxis-spawn-<uuid>.scope`. Those scopes hang off the Ptyxis process, so its
exit tears all of them down together. Six went in two seconds.

The dead tabs had been alive 1d 19h to 1d 23h with peak RSS of 18.7 M to 1.2 G,
Ptyxis itself peaking at 852.9 M. Aggregate peak across all of them is ~4.9 GiB
against 93 GiB of RAM — a further angle on the same conclusion: memory was never
the constraint.

### Frequency

`journalctl --grep='WL: error in client communication'` over **all five recorded
boots** returns exactly one hit — this one, after Ptyxis had been up 2d 2h. This
is a first-time fault in upstream code, not a recurring one, which is why
Plan 00111 does not attempt to fix it.

### Versions at the time

| Component        | Version          |
| ---------------- | ---------------- |
| ptyxis           | `50.1-2.fc44`    |
| vte291           | `0.84.1-1.fc44`  |
| gtk4             | `4.22.4-2.fc44`  |
| mutter           | `50.4-1.fc44`    |
| gnome-shell      | `50.4-1.fc44`    |
| kernel (running) | `7.2.4-200.fc44` |

On relaunch Ptyxis logged `The new GL renderer has been renamed to gl. Try GSK_RENDERER=help`. Whether a stale `GSK_RENDERER` value is set anywhere in the
deployed environment is **unverified**, and is the one upstream-adjacent thread
worth a glance since a bad renderer name is a plausible contributor to a
GTK4/Wayland protocol fault.

## Container survival is not session survival

Three of four CCY containers survived; one was destroyed:

| Container                          | Claude PID | Outcome                |
| ---------------------------------- | ---------- | ---------------------- |
| `container-C`                | 501461     | survived               |
| `container-B` | 1204504    | survived               |
| `container-D`              | 1731869    | survived               |
| `container-A`                 | —          | **killed and removed** |

All four hit the same console failure the instant their pty vanished, so the
console failure is not the discriminator:

```
13:23:45 conmon[1170662]: conmon a60224f2dc8f… <nwarn>: Failed to write to remote console socket   ← survived
13:23:45 conmon[1729365]: conmon bb073b3a99e3… <nwarn>: Failed to write to remote console socket   ← survived
13:23:45 conmon[501011]:  conmon 8453a8dbcd64… <nwarn>: Failed to write to remote console socket   ← survived
13:23:45 podman[3892681]: container kill   f6b77e1f66f5…  (name=container-A)                  ← destroyed
13:23:45 podman[3892681]: container died   f6b77e1f66f5…
13:23:47 podman[3892681]: container remove f6b77e1f66f5…
```

The discriminator is the **foreground `podman run` client**:

- For the survivors no `podman` client remains — only `conmon` (pids 501011,
  1170662, 1729365), and `conmon`'s **ppid is 2266, `systemd --user`**. It lives
  in its own `libpod-*.scope`, outside any `ptyxis-spawn-*.scope`, already
  reparented clear of the tab. The tab's death could not reach it.
- `container-A`'s `podman` client (pid 3892681) was **still alive in a dying
  tab**. Losing its terminal, it ran its ordinary `--rm` teardown: kill, then
  remove. Podman destroyed that container deliberately, as instructed — it was
  not collateral damage.

So survival was **incidental**, turning on whether a `podman run --rm` client
happened to still be attached. It is not a property anything designed or can
rely on.

**And it bought nothing.** All three survivors sit at `TTY = ?`, state `Ssl+`,
parked in `ep_poll`: alive, with no controlling terminal and nothing listening on
their stdio. There is no pty to attach to, so `podman attach` reaches a process
that can no longer be driven.

This is the decisive result. **Detaching the container is a red herring for
preserving work — the thing that must outlive the terminal is the pty itself.**

The process chain shows where the pty sits:

```
ptyxis-spawn-<uuid>.scope → bash → podman run → [container] → claude-supervise.py (pts/0) → claude
```

Both `claude` and the `claude-supervise.py --arm` supervisor above it hold
`pts/0`, a pty allocated by `podman run -t` and wired to the tab. Kill the tab
and that pty is gone regardless of what happens to the container around it.

## Options considered

1. **`ccy` inside a persistent `tmux` session — recommended.** The tmux *server*
   is a daemon owned by `systemd --user`, not by the tab, and it owns the pty.
   The tab becomes a viewport; when Ptyxis dies the pty survives in the server
   and `tmux attach` restores the live session, mid-turn work included. The only
   candidate that preserves the pty, which the evidence above shows is what
   matters. `tmux` is already installed at `/usr/bin/tmux`, and **no tmux server
   was running** at the time — nothing was insulated.
2. **A transient `systemd --user` unit** (`systemd-run --user --pty`). Equivalent
   decoupling without a tmux dependency, but re-attaching to a `systemd-run` pty
   after its client dies is awkward, and it forgoes tmux's scrollback and
   multi-window handling.
3. **Detaching the container console from the tab — ruled out.** Shown above to
   preserve the container while losing the session, which is not a defence.

`screen`, `zellij`, `abduco` and `dtach` are all absent from the host.

Open design questions carried into Phase 3 of the plan: where the wrapping
belongs (host `ccy` wrapper versus user habit), how sessions are named so they
are findable after every window has gone, and whether a stale-session reaper is
needed.

### Secondary hardening

`podman run --rm` in a tab means **any** tab death destroys that container
outright, as `container-A` shows. Worth addressing even once a pty survives
the tab, because `--rm` turns a recoverable interruption into an unrecoverable
one. Independent of the pty question and should not be conflated with it.

## Recovery position after the incident

Claude Code's on-disk transcripts are intact for all four projects, so
`--continue` recovers each conversation to its last completed turn. What is lost
is the in-flight turn in each. The three orphaned `claude` processes should be
reaped before restarting: they cannot be driven and hold ~1.8 GiB RSS between
them.

## Unrelated observations, recorded so they are not re-investigated

- Numerous SELinux AVC denials from CCY containers writing to `user_home_t`
  (`thread-registry`, `verdicts.jsonl`, `.git` commit-graph files), all under
  `permissive=1`, so nothing was blocked. Real policy gaps, unconnected to this
  fault.
- `gnome-shell` logged `Cursor update failed: drmModeAtomicCommit: Invalid argument` at 12:16:21, ~67 minutes before the event, alongside a steady stream
  of `Can't update stage views actor … needs an allocation` warnings from
  dash-to-dock. Treated as **unrelated** absent evidence linking them, but noted
  because a display-stack `EINVAL` preceding a Wayland `EINVAL` is not a
  comfortable coincidence.
