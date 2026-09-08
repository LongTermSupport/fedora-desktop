# Plan 00104 — Triage Evidence (sanitised)

Captured on the HOST during the incident session and transcribed here **sanitised** so the
plan can be executed from the CCY container, which has no access to the host journal.

**Sanitisation applied** — per [CLAUDE/ExampleValues.md](../../ExampleValues.md). The
hostname, username, MAC addresses, BSSID, LAN/bridge IP addresses, client project names and
client container names present in the raw journal have been replaced with placeholders
(`<host>`, `<user>`, `<mac>`, `192.0.2.x`, `<client-project>`, `<client-db-container>`).
Nothing below is a real address, host or client identifier. USB bus paths (`3-6`, `4-1.2`)
are topology, not identity, and are kept because the analysis depends on them.

Raw, unsanitised output is reproducible on the host at any time via
[`triage.bash`](triage.bash), which writes to a gitignored `triage-runs/` directory.

---

## F1 — The suspend was entered, then aborted within 3 seconds

Source: `journalctl -b -1 -k`, kernel-only, incident window.

```
11:40:41  systemd-logind: The system will suspend now!
11:40:49  kernel: PM: suspend entry (s2idle)
11:40:49  kernel: Filesystems sync: 0.033 seconds
11:40:49  kernel: evdi: (card2) VT switch detected / Notifying display power state: off
11:40:49  kernel: evdi: (card3) VT switch detected / Notifying display power state: off
11:40:49  kernel: rfkill: input handler enabled
11:40:52  kernel: usb 3-6: USB disconnect, device number 26
11:40:52  kernel: usb 3-6.3 / 3-6.3.2 / 3-6.3.2.1 / 3-6.3.2.2 / 3-6.5: USB disconnect
11:40:52  kernel: usb 4-1 / 4-1.1 / 4-1.2 / 4-1.3: USB disconnect
11:40:52  kernel: r8152-cfgselector 4-1.3.1: USB disconnect, device number 17
11:40:52  kernel: r8152 4-1.3.1:1.0 <ethdev>: Stop submitting intr, status -108
11:40:52  kernel: evdi: (card3) Disconnected / Removing i2c adapter bus number 17 / Closed
11:40:52  kernel: evdi: (card2) Disconnected / Removing i2c adapter bus number 18 / Closed
11:40:53  kernel: NVRM: rm_power_source_change_event: Failed to handle Power Source
                  change event, status=0x11
```

**There is no `PM: suspend exit` line anywhere in the boot.** The kernel logged entry into
s2idle and never logged a clean exit, yet userspace was demonstrably running again three
seconds later (F2).

## F2 — The machine ran continuously for the following 25 minutes

Source — exact command, no priority filter, reproducible as one invocation:

```bash
journalctl --no-pager -b -1 --since "2026-09-08 11:40" --until "2026-09-08 12:06" \
    -o short-iso -q | grep -oE '^2026-09-08T[0-9]{2}:[0-9]{2}' | uniq -c
```

```
2516 11:40      1726 11:47      1598 11:54      1651 12:01
1792 11:41      1585 11:48      1631 11:55      1684 12:02
1786 11:42      1606 11:49      1643 11:56      2160 12:03   <- lid opened
1746 11:43      1836 11:50      1670 11:57      1518 12:04
1618 11:44      1587 11:51      1597 11:58       692 12:05   <- last entry, boot -1
1712 11:45      1667 11:52      1570 11:59
1654 11:46      1643 11:53      1814 12:00
```

No idle gap at any point: sustained **~1500–2500 journal lines per minute for 25 minutes**
while sealed in a rucksack. Boot `-1` ends at 12:05:28; boot `0` begins 12:06:53 —
consistent with a forced power-off.

> **Correction.** An earlier revision of this document published a much lower histogram
> (~110–260/min). Those figures were not reproducible by any single command: they spliced a
> `-p notice` capture of 11:40–11:51 together with a `-p info` capture of 11:52–12:05, and
> the document then cited them as plain `journalctl -b -1`. The conclusion is unchanged and
> in fact stronger — the machine was an order of magnitude busier than first reported — but
> the provenance was wrong, so the numbers are replaced above with a single unfiltered run.

Contributors to the sustained load observed in the same window: a `<client-db-container>`
Podman healthcheck firing every ~10s, `pasta` retrying DNS against `<public-dns>` with no
route, an LXC bridge with an active DHCP lease, and the DisplayLink user-space daemon.

## F3 — No thermal trip point was logged

Source: `journalctl -b -1 -k --grep 'thermal|throttl|temperature|critical|overheat'`

```
-- No entries --
```

The machine was hot to the touch but the kernel logged no throttle or critical-temperature
event before the forced power-off. Absence of a logged trip is **not** evidence that
temperatures were safe — only that no threshold that logs was crossed, or that it was not
flushed to disk before power was cut.

## F4 — Only `s2idle` is available; there is no deep S3

Source: `cat /sys/power/mem_sleep`

```
[s2idle]
```

There is no `deep` option. With s2idle, "suspend did not complete" does not mean "powered
down and idle" — it means the CPU is running a software idle loop with peripherals partly
live, which produces real heat in an enclosed bag.

Related, from the current boot (`/sys/power/suspend_stats`): `total_hw_sleep 0` against
`max_hw_sleep 523986009990` — the platform had not achieved any hardware sleep residency.

## F5 — The disconnected USB tree had wakeup armed

Source: `grep -H . /sys/bus/*/devices/*/power/wakeup | grep -w enabled`

Wakeup was **enabled** on, among others:

```
/sys/bus/usb/devices/3-6/power/wakeup            <- the tree that disconnected at 11:40:52
/sys/bus/thunderbolt/devices/0-0/power/wakeup
/sys/bus/thunderbolt/devices/1-0/power/wakeup
/sys/bus/thunderbolt/devices/domain0/power/wakeup
/sys/bus/thunderbolt/devices/domain1/power/wakeup
/sys/bus/pci/devices/0000:00:14.0/power/wakeup   <- xHCI controller
/sys/bus/platform/devices/PNP0C0D:00/power/wakeup <- lid
/sys/bus/i2c/devices/i2c-ELAN0676:00/power/wakeup <- touchpad
```

`3-6` currently enumerates as a USB keyboard; during the incident it headed the hub tree that
carried the dock's downstream devices. Either way the bus path that generated the disconnect
storm at 11:40:52 was wakeup-armed.

## F6 — Exactly one suspend attempt in a 2.5-day boot

Source: `journalctl -b -1 --grep 'PM: suspend entry|PM: suspend exit|will suspend now|Lid closed|Lid opened|System resumed'`

Boot `-1` spans 2026-09-06 11:57 → 2026-09-08 12:05. Every match **for the pattern above**
(note it is narrower than `show_sleep_timeline` in `probe-suspend.bash`, which also matches
`Performing sleep operation` and so returns a fifth line):

```
09-07 15:02:09  systemd-logind: Lid closed.
09-08 11:40:41  systemd-logind: The system will suspend now!
09-08 11:40:49  systemd-sleep: Performing sleep operation 'suspend'...   <- probe-only match
09-08 11:40:49  kernel: PM: suspend entry (s2idle)
09-08 12:03:35  systemd-logind: Lid opened.
```

The lid was closed at 09-07 15:02 and the machine did **not** suspend — it stayed awake for
~21 hours. The only suspend attempt in the entire boot is the one that failed.

## F7 — logind lid configuration, as deployed

Source: `/etc/systemd/logind.conf.d/laptop-lid.conf` (deployed by
`playbooks/imports/optional/hardware-specific/play-laptop-lid-power-management.yml`)

Verbatim, including its comment lines:

```ini
# BEGIN ANSIBLE MANAGED: Laptop Lid Behavior
[Login]
# Suspend when lid closed on battery
HandleLidSwitch=suspend

# Don't suspend when lid closed on AC power
HandleLidSwitchExternalPower=ignore
# END ANSIBLE MANAGED: Laptop Lid Behavior
```

`HandleLidSwitchDocked=` is **not set** in the file. Its effective value is no longer an
inference from the documented default — logind reports all three directly:

```console
$ busctl get-property org.freedesktop.login1 /org/freedesktop/login1 \
      org.freedesktop.login1.Manager \
      HandleLidSwitch HandleLidSwitchDocked HandleLidSwitchExternalPower
s "suspend"
s "ignore"
s "ignore"
```

So **`HandleLidSwitchDocked` is measured as `ignore`**, confirming what F8's man-page default
predicted.

Note the trap: these are logind **manager** properties, not unit properties, so

```console
$ systemctl show systemd-logind --property=HandleLidSwitch ...
```

prints **nothing and exits 0** — a blind check indistinguishable from a clean one. An earlier
revision of `probe-suspend.bash` used exactly that and would have written a silently empty
section; `PLAN.md` had also built Task 2.2 on it, so the task would have compared nothing to
nothing. Both now use `busctl`.

## F8 — `man logind.conf` on precedence and event semantics

Source: `man logind.conf`, verbatim extracts.

> Only input devices with the "power-switch" udev tag will be **watched for key/lid switch
> events**.

> If the system is inserted in a docking station, **or if more than one display is
> connected**, the action specified by `HandleLidSwitchDocked=` occurs; if the system is on
> external power the action (if any) specified by `HandleLidSwitchExternalPower=` occurs;
> otherwise the `HandleLidSwitch=` action occurs.

> `HandleLidSwitchDocked=` defaults to "ignore".

> A different application may disable logind's handling of ... the lid switch by taking a
> low-level inhibitor lock ("handle-lid-switch"). This is most commonly used by graphical
> desktop environments...

Two consequences. First, handling is **event-driven**: logind acts on a lid *transition*, not
on a standing lid *state*. Second, the precedence order is docked → external-power → plain,
and with two dock-driven displays attached the branch that applied at 09-07 15:02 was
`HandleLidSwitchDocked` (unset, so `ignore`) — **not** the `HandleLidSwitchExternalPower`
line in F7.

## F9 — GNOME does **not** hold `handle-lid-switch`; logind does own the lid

Source: `systemd-inhibit --list`

```
WHO             UID   USER    COMM            WHAT                                                      MODE
ModemManager    0     root    ModemManager    sleep                                                     delay
NetworkManager  0     root    NetworkManager  sleep                                                     delay
UPower          0     root    upowerd         sleep                                                     delay
GNOME Shell     1000  <user>  gnome-shell     sleep                                                     delay
GNOME Shell     1000  <user>  gnome-shell     sleep                                                     delay
<user>          1000  <user>  gsd-media-keys  handle-power-key:handle-suspend-key:handle-hibernate-key  block
<user>          1000  <user>  gsd-media-keys  sleep                                                     delay
<user>          1000  <user>  gsd-power       sleep                                                     delay
slack           1000  <user>  xdg-dbus-proxy  sleep                                                     delay
```

`gsd-media-keys` blocks the power/suspend/hibernate **keys**, but nothing holds
`handle-lid-switch`. Per F8 this means the F7 settings are live and logind — not GNOME —
decides lid behaviour. This rules out the hypothesis that `laptop-lid.conf` is inert.

Note also that every `sleep` inhibitor above is `delay`, not `block`, so none of them
prevented the 11:40 suspend.

## F10 — GNOME idle-suspend is disabled on **both** power sources

Source: `gsettings get org.gnome.settings-daemon.plugins.power …`

```
sleep-inactive-battery-type      'nothing'
sleep-inactive-battery-timeout   900
sleep-inactive-ac-type           'nothing'
sleep-inactive-ac-timeout        900
org.gnome.desktop.session idle-delay  uint32 0
```

Both types are `nothing`, so the 900s timeouts are inert. `idle-delay 0` disables the idle
transition entirely.

## F11 — The AC setting is IaC-managed; the battery setting is unmanaged drift

Source: repo-wide `grep -rn "sleep-inactive"` excluding `.ansible/` and `.git/`.

```
playbooks/imports/play-prevent-ssh-suspend.yml:59:  - sleep-inactive-ac-type
```

`sleep-inactive-ac-type=nothing` is **deliberate** — set at
`play-prevent-ssh-suspend.yml:51-67` so inbound SSH sessions survive.

`sleep-inactive-battery-type` appears **nowhere in the repository** — not in a playbook,
template, var file or dotfile. GNOME's own default for it is `suspend`. Its `'nothing'` value
was therefore set outside IaC and nothing in the repo would ever restore it.

## F12 — `ssh-suspend-guard` takes a **block**-mode sleep inhibitor

Source: `files/usr/local/bin/ssh-suspend-guard`

```bash
systemd-inhibit --what=sleep --why="Active SSH session(s)" \
    --who="ssh-suspend-guard" sleep infinity &
```

`systemd-inhibit` defaults to `--mode=block`. While an inbound SSH session is established on
port 22, **all** sleep is blocked — including any idle-suspend a fix might introduce. The
guard polls every 10s and releases when the last session closes.

It was not holding a lock during this incident (F9 shows no such inhibitor), so it did not
contribute to the failure. It does, however, constrain the fix.

## F13 — Interfaces available for a fix (all verified present on the host)

```
/proc/acpi/button/lid/LID/state                    -> "state:      open"
busctl get-property org.freedesktop.login1 \
  /org/freedesktop/login1 \
  org.freedesktop.login1.Manager LidClosed         -> b false
/sys/class/power_supply/AC/online                  -> 1
/usr/lib/systemd/system-sleep/                     -> exists; already holds
                                                      displaylink.sh and nvidia
```

## F14 — logind's lid branch is directly readable; `systemctl show` is not

Source: `busctl` and `/sys/class/drm/*/status`, captured with the dock **detached**.

```
== logind Docked property (true => HandleLidSwitchDocked branch applies)
b false
== connected DRM outputs (more than one also selects the Docked branch)
connected: /sys/class/drm/card1/card1-eDP-1/status
connected output count: 1
```

Both inputs to the F8 precedence rule are therefore observable at runtime, which makes P1 a
one-observation question rather than an inference: read the same two values with the dock
attached and the branch is settled.

Note the negative result alongside it — this returns **nothing at all**:

```
systemctl show systemd-logind --property=HandleLidSwitch \
    --property=HandleLidSwitchDocked --property=HandleLidSwitchExternalPower
```

logind does not expose the `Handle*` settings as unit properties, so an unset value cannot be
read back from the running service; it can only be taken from the config files (F7) plus the
documented defaults (F8). An earlier revision of `probe-suspend.bash` probed this and would
have written a silently empty section, which reads as evidence of absence — the failure mode
`CLAUDE/PlanTriage.md` names as "never write a misleading empty result". The probe now states
the limitation in the report instead.

## F15 — Power is delivered directly, never through the dock (operator-reported)

Reported by the operator, 2026-09-08. Not derived from the journal, and recorded here as a
configuration fact the analysis depends on:

- **Home**: a USB dock drives external monitors and peripherals; mains power goes **direct**
  to the laptop, so the dock can be powered down while the laptop stays powered.
- **Away / office**: same arrangement — direct power.
- **At the time of writing**: laptop only, no dock, direct power.

Two consequences.

First, **the incident involved two independent disconnections**, not one. Unplugging the dock
alone would not have changed AC state, yet the journal shows both the USB disconnect storm
*and* an NVRM power-source change event one second later (F1) — consistent with packing up:
dock out, then mains out.

Second, and more usefully, **the two variables are separable by hand**. Unplugging mains with
no dock attached exercises the AC branch on its own; unplugging the dock with mains still
connected exercises the display-count branch on its own. That is what makes `--watch-power`
a legitimate test rather than a partial one.

It also sharpens the risk. Per F8 the precedence is docked → external-power → plain, and
`HandleLidSwitchDocked` defaults to `ignore` (F7). So at home, with the lid closed and
external displays connected, the `Docked` branch applies **regardless of power state** — a
lid-closed machine there is ignored by logind whether it is on mains or on battery.

---

## Facts → hypotheses

### Confirmed chain

1. Suspend was requested and s2idle was entered (F1).
2. The dock was unplugged ~3s later, generating a disconnect storm on a wakeup-armed USB
   tree (F1, F5), which aborted the suspend.
3. Because the abort left the lid **already closed with no new transition**, logind's
   event-driven handling never fired again (F6, F8).
4. No idle-suspend fallback existed on either power source (F10), and the battery half of
   that is unmanaged drift rather than a deliberate choice (F11).
5. With only s2idle available (F4), "awake" meant a hot CPU in a sealed bag for 25 minutes
   (F2).

### H1 — The same failure occurs with no suspend attempt at all

If the machine is docked with the lid closed (`HandleLidSwitchDocked=ignore`, F7/F8) and is
then simply undocked and carried away, there is again no lid transition and no idle fallback
— so it stays awake exactly as it did here. **This makes the aborted suspend a trigger, not
the root cause.**

*Confirms:* undock a lid-closed docked machine without touching suspend; observe it stay
awake. *Refutes:* observing logind suspend it within seconds of the power-source change.

### H2 — Disarming USB/Thunderbolt wakeup would prevent the abort

Plausible from F5, but it treats one trigger rather than the gap, and `3-6` is not a stable
identifier (it enumerated as a hub during the incident and as a keyboard afterwards).

*Confirms:* re-running the incident with wakeup disarmed on the dock tree and observing the
suspend hold. *Refutes:* the suspend aborting anyway from a different wakeup source.

### Unverified premises

- **P1** — That two DisplayLink-driven displays cause logind to take the `Docked` branch.
  F8 quotes the man page's "more than one display is connected", but whether logind counts
  *evdi* virtual outputs the same way as native ones is **not** established. If it does not,
  the 09-07 branch was `HandleLidSwitchExternalPower` instead. Either way the branch taken
  was `ignore`, so the F6 outcome is unchanged — but the correct **fix target** differs.

  **Partially settled — the measurement now exists (F14).** logind exposes a readable
  `Docked` property, so this no longer has to be inferred. What remains is one observation
  with the dock physically attached.

- **P2** — That the USB disconnect is what aborted the suspend, rather than merely being the
  first thing logged after an abort caused by something else. The 3-second ordering is
  strongly suggestive but the kernel logged no wakeup-source attribution.

- **P3** — That no `PM: suspend exit` was emitted, as opposed to emitted-but-lost. The
  journal was still writing normally either side of the gap, which argues against loss.
