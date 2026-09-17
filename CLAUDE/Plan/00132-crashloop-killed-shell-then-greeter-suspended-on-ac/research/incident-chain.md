# Incident chain

Every link below was read from this host's journal and live state. Times are local
(BST, UTC+1) unless marked. Container and project names are replaced with placeholders
— this is a public repository.

## The chain

| #   | Time          | Event                                                                                                                                          | Evidence                                                                                                             |
| --- | ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| 1   | ~19:00 onward | `container-A` (rootless, `restart: unless-stopped`) crash-loops on a missing native module. It can never start successfully                    | Journal shows the same startup traceback repeating continuously                                                      |
| 2   | 20:00–05:52   | Restart rate holds at **~8,800 starts/hour** for over ten hours                                                                                | `journalctl --grep "Started libpod-"`, bucketed per hour: 8608, 8890, 8849, 8828, 8777, 8800, 8531, 8517, 8530, 7514 |
| 3   | 05:52:42.437  | `dbus-broker`: `UID <uid> exceeded its 'bytes' quota`. Peers are disconnected en masse — at least `:1.17`, `:1.48`, `:1.63`, `:1.64`, `:1.156` | Journal, microsecond precision                                                                                       |
| 4   | 05:52:42.439  | `flatpak-session-helper` exits 1                                                                                                               | Journal                                                                                                              |
| 5   | 05:52:42.441  | `gnome-shell`: `Shutting down GNOME Shell` — **4 ms after the quota breach**                                                                   | Journal                                                                                                              |
| 6   | 05:52:42.481  | `JS ERROR: Error: incorrect pop` from `unlockDialog.js:907` via `popModal`                                                                     | Journal                                                                                                              |
| 7   | 05:52:43      | SEGV at `cogl_onscreen_egl_dispose`, called from `meta_context_destroy` ← `main`                                                               | `coredumpctl` backtrace                                                                                              |
| 8   | 05:52:43      | Every Wayland client dies: terminals, four browser windows, portals, calendar                                                                  | `Lost connection to Wayland compositor` / `Broken pipe`                                                              |
| 9   | 05:52:52      | GDM greeter session opens                                                                                                                      | `gdm-launch-environment: session opened for user gdm-greeter`                                                        |
| 10  | 06:07:53      | `systemd-sleep: Performing sleep operation 'suspend'`                                                                                          | Journal                                                                                                              |
| 11  | 08:38:13      | `System returned from sleep operation 'suspend'`                                                                                               | Journal                                                                                                              |
| 12  | 08:39:01      | A **new** session is created on login; the old session is removed at 08:39:22                                                                  | `systemd-logind: New session '857'` / `Removed session 856`                                                          |

## What the evidence rules out

### It did not reboot

`uptime` reports **5 days, 21 hours**; `who -b` gives a boot time six days before the
incident. `journalctl --list-boots` shows the current boot as the only recent one. Every
non-GUI process — including a container-hosted agent whose own logs bracket the suspend
window exactly — survived. The desktop vanishing is not evidence of a reboot, and on
Wayland it never is.

### The segfault is not the cause

This is the misreading the journal invites, because the SEGV is the loudest line in it.
The backtrace settles it:

```
#0  cogl_onscreen_egl_dispose
#1  meta_onscreen_native_dispose
#5  meta_renderer_native_finalize
#7  meta_backend_dispose
#12 meta_context_destroy
#13 main
```

`meta_context_destroy` called from `main` is the **clean shutdown path**. The shell had
already logged `Shutting down GNOME Shell` one second earlier and was tearing itself
down when it crashed. The crash is an exit-time defect in the teardown of the renderer,
downstream of a decision to exit that had already been taken. Likewise the
`unlockDialog.js` `incorrect pop` at #6 — that is the lock screen being destroyed during
teardown.

The causal boundary is the 4 ms between the D-Bus quota breach and
`Shutting down GNOME Shell`. Everything after it is consequence.

### The battery was not why it suspended

The initial reading — that the machine was on battery and therefore subject to
`sleep-inactive-battery-type=suspend` — is **wrong**, and is corrected in
[greeter-power-policy.md](greeter-power-policy.md). The greeter suspends on AC too. The
arithmetic is exact:

```
05:52:52   greeter session opens
         + 900 s  (sleep-inactive-ac-timeout)
= 06:07:52
  06:07:53   suspend logged
```

One second. Whether the machine was plugged in made no difference to this outcome.

## Why the blast radius was the whole desktop

Two properties combined:

1. **Rootless podman uses the session bus.** The same bus the desktop depends on. A
   container restart storm and the compositor are not isolated from one another; they
   compete for one per-UID quota.
2. **On Wayland the compositor is the display server.** There is no separate X server to
   survive it. When `gnome-shell` exits, every client's connection is severed — there is
   no reconnect path and no application-level recovery.

So a defect in an unrelated project's container reached all the way to killing the user's
running work, with no intervening boundary.

## Still live during triage

The loop was **still running** while these facts were being gathered: 66 container starts
in a 60-second sample, with the container observed at `Up 1 second`. This is noted because
it is the only real test case available for validating a detection defence, and stopping
the loop destroys it. See [detection-gap.md](detection-gap.md).
