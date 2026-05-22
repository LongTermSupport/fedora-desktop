# Research 02: systemd / boot timing audit

**Host**: ThinkPad X1 Carbon Gen 11 — Fedora 43, kernel 6.19.14-200
**Captured**: 2026-05-21, current boot uptime ~24 min
**Mode**: Read-only audit. No `restart`/`enable`/`disable`/`mask` executed.

## Summary

The system is broadly healthy: one persistent failed unit (`v4l2-relayd@icamerasrc`), no OOM kills, no NetworkManager runtime crashes in the current boot, and only seven coredumps in the last 7 days. Boot takes **~39.9 s** end-to-end, but only **~11.9 s** of that is userspace; the dominant cost is firmware (10.1 s) + loader (5.0 s) + initrd (11.6 s). The two biggest fixable userspace contributors are `dracut-initqueue` (10.8 s, inside initrd) and the LUKS unlock for `/home` (10.6 s). The single largest day-to-day pain points are noise/memory from `rclone-lts-photo` (17 GiB RSS, 4873 log lines/boot) and `warp-svc` (884 log lines, persistent NTP/DNS errors at startup). 34 boots in the last ~50 days — normal laptop usage, not a crash-loop.

## Failed units

### System units (1)

| Unit                             | State                                       | Root cause                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| -------------------------------- | ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `v4l2-relayd@icamerasrc.service` | `failed (start-limit-hit)` after 5 restarts | The MIPI IPU6 GStreamer pipeline aborts on startup with `gst_element_set_state: assertion 'GST_IS_ELEMENT (element)' failed` immediately after the `(sh)` wrapper warns *"Referenced but unset environment variable evaluates to an empty string: SPLASHSRC"*. The camera relay (recently introduced by `play-ipu6-webcam` — commit `e5d0e33`) never reaches a healthy state at boot. The unit is `enabled-runtime` (templated), so masking the instance is the cleanest fix. |

### User units (0)

None failed. However, three user-session services exited 1 once during this boot before being auto-restarted to a healthy state (now `active (running)`):

- `gnome-software.service` (restart counter 1)
- `evolution-alarm-notify.service` (restart counter 1)
- `dbus-:1.2-org.gnome.Settings.GlobalShortcutsProvider@0.service` (one-shot failure)

These are GDM session-startup race conditions, not chronic — no current restart-limit hits.

## Boot timing

```
Startup finished in 10.129s (firmware) + 4.957s (loader)
                  + 1.318s (kernel) + 11.610s (initrd)
                  + 11.875s (userspace) = 39.890s
graphical.target reached after 11.874s in userspace.
```

### Top 10 slowest userspace contributors

| Time     | Unit                                 | Notes                                                                                                                                                       |
| -------- | ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 10.771 s | `dracut-initqueue.service`           | Inside initrd phase. Dominated by waiting for the LUKS device. Hard to optimise — out of scope.                                                             |
| 10.558 s | `systemd-cryptsetup@luks-2e1c827e…`  | `/home` LUKS unlock. Interactive passphrase — expected.                                                                                                     |
| 5.790 s  | `NetworkManager-wait-online.service` | Cleanly succeeded in 5 s (Wi-Fi DHCP). Blocks `network-online.target`. Not strictly needed by anything that should boot blocking — candidate for reduction. |
| 2.278 s  | `plymouth-quit-wait.service`         | Plymouth handover. Cosmetic.                                                                                                                                |
| 942 ms   | `docker.service`                     | Rootful Docker daemon startup. Expected.                                                                                                                    |
| 910 ms   | `fwupd.service`                      | Firmware update agent. Fine.                                                                                                                                |
| 865 ms   | `initrd-switch-root.service`         | Pivot from initrd to root.                                                                                                                                  |
| 693 ms   | `firewalld.service`                  | Normal.                                                                                                                                                     |
| 676 ms   | `NetworkManager.service`             | Normal.                                                                                                                                                     |
| 391 ms   | `upower.service`                     | Normal.                                                                                                                                                     |

Critical chain bottoms out at `home-joseph-mnt-lts-photo.mount @11.033s` — the rclone-FUSE mount blocks `local-fs.target`, which in turn delays the rest of the chain. That mount is the path Plymouth waits on.

### Device-level "blame" noise

The top 25 of `systemd-analyze blame` is dominated by `.device` entries at ~12.0–12.3 s (TPM, ttyS0–3, NVMe by-id symlinks). These are uevent settle times, not unit start times, and are not actionable.

## Restart storms

| Unit                                                                    | Restarts (this boot) | Cause                                                                                   | Recommended                                                                                                                 |
| ----------------------------------------------------------------------- | -------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `v4l2-relayd@icamerasrc.service`                                        | 5 (hit start-limit)  | GStreamer asserts; missing `SPLASHSRC` env var; camera userspace stack not initialising | Mask the template instance OR fix the IPU6 webcam playbook to gate the relay on hardware presence. See Recommended actions. |
| `gnome-software.service` (user)                                         | 1 (recovered)        | GDM session startup race                                                                | No action — self-heals.                                                                                                     |
| `evolution-alarm-notify.service` (user)                                 | 1 (recovered)        | Same race                                                                               | No action.                                                                                                                  |
| `dbus-:1.2-org.gnome.Settings.GlobalShortcutsProvider@0.service` (user) | 1 (one-shot failure) | Transient DBus activation                                                               | No action.                                                                                                                  |

No other unit in this boot is in a restart loop. No `start-limit-hit` other than `v4l2-relayd@icamerasrc`.

## Unit verification

`sudo -n systemd-analyze verify /etc/systemd/system/*.service` reported only one issue:

```
NetworkManager-dispatcher.service: Command 'man NetworkManager-dispatcher.service(8)' failed with code 16
```

This is a `man-db` lookup quirk during `verify` (the manpage exists, but `man` returned exit 16 in the audit context). Not an actual unit-definition problem — vendor unit, no deprecated directives flagged. Custom local units (`kernel-version-manager.service`, `ssh-suspend-guard.service`, `warp-svc.service`) pass verification.

## Noisy units / timers

### Journal noise — top 10 sources (current boot, ~24 min)

| Source           | Lines    | Worth silencing?                                                                                                                                                                                                                                                                    |
| ---------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `rclone`         | **4873** | **Yes.** Two log streams: (1) once-per-minute `vfs cache: cleaned` summary (24 lines), (2) per-file upload status for ~228 GiB photo library backfilling Google Drive — dominates the log. Drop log-level from `INFO` to `NOTICE` or `WARNING` once the initial backfill completes. |
| `systemd`        | 1221     | Mostly transient user-scope `app-*.scope` start/stop — normal.                                                                                                                                                                                                                      |
| `warp-svc`       | 884      | **Yes.** 7 ERROR / 37 WARN / 204 INFO / 504 DEBUG. Errors are clustered at startup (NTP timeout, DNS unreachable, captive-portal probe failures before NM is up) — a known Cloudflare WARP startup race on Linux. DEBUG should not be on by default; drop verbosity.                |
| `audit`          | 378      | SELinux/audit baseline. Leave.                                                                                                                                                                                                                                                      |
| `systemd-logind` | 238      | Session activity. Leave.                                                                                                                                                                                                                                                            |
| `packagekitd`    | 214      | Periodic refresh. Leave.                                                                                                                                                                                                                                                            |
| `NetworkManager` | 176      | Normal.                                                                                                                                                                                                                                                                             |
| `gnome-shell`    | 106      | Normal extension/desktop chatter.                                                                                                                                                                                                                                                   |
| `sudo`           | 80       | Audit trail. Leave.                                                                                                                                                                                                                                                                 |
| `containerd`     | 77       | Docker rootful internals. Leave.                                                                                                                                                                                                                                                    |

### Timers

7 timers active, all healthy. Nothing fires too often. `dnf-makecache.timer` fires every 1h (expected on Fedora). `fwupd-refresh.timer` is **disabled** — that's a deliberate Fedora-Desktop choice and saves ~weekly metered-network hits. `raid-check.timer` is enabled despite no MD RAID on this NVMe-only laptop (the `mdadm` package pulls it in) — harmless, runs Sundays.

### User-session "known offenders"

Enabled user units (`systemctl --user list-unit-files --state=enabled`) are lean:
`dbus-broker, gnome-remote-desktop, obex, rclone-lts-photo, systemd-tmpfiles-setup, warp-desktop-svc, wireplumber, xdg-user-dirs, pipewire-pulse, podman, grub-boot-success, systemd-tmpfiles-clean`.

No `tracker-miner-fs-3`, no `evolution-source-registry`, no `xdg-document-portal` *enabled* at the user-unit-file level. Good hygiene already.

## Crashes / OOM (last 7 days)

### Coredumps: 7 total

| Date                  | Process          | Signal  | Note                                                                                                                                                                                             |
| --------------------- | ---------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 2026-05-15 22:49      | `darktable`      | SIGSEGV | One-off photo-editor crash — corefile present.                                                                                                                                                   |
| 2026-05-16 22:17 (×2) | `vsftpd`         | SIGABRT | Inaccessible (UID 1001 = ftp camera user). Related to `ftp-camera` work.                                                                                                                         |
| 2026-05-21 11:55      | `NetworkManager` | SIGABRT | Inaccessible. Single event — not recurring this boot. Possibly TimeoutStopFailureMode=abort hitting NM during shutdown (Fedora's `10-timeout-abort.conf` drop-in is in effect on every service). |
| 2026-05-21 13:57 (×2) | `vsftpd`         | SIGABRT | Same as above.                                                                                                                                                                                   |
| 2026-05-21 19:25      | `wireplumber`    | SIGABRT | One-off, corefile present.                                                                                                                                                                       |

**No oom-killer events in the last 7 days.** The `oom` grep returns 0 actual OOM-killer lines (the 15 matches are unrelated text mentioning "out of memory" or "oom" in other contexts).

The `vsftpd` SIGABRTs are most likely the `--hotspot` / FTP-camera workflow being torn down via `TimeoutStopFailureMode=abort`. They are functionally session-end aborts, not server faults — but worth a look in the FTP-camera plan.

## Recommended actions (ranked)

1. **Fix `v4l2-relayd@icamerasrc.service`** — the only persistent failed unit on the host. Two paths:
   a. *Quick:* `sudo systemctl mask v4l2-relayd@icamerasrc.service` (acceptable if the IPU6 webcam stack isn't ready for daily use).
   b. *Proper:* update `play-ipu6-webcam` to set `SPLASHSRC` and gate the relay's `ExecCondition` on the IPU6 camera node actually existing. The current unit fails before reaching `ExecStart` cleanly.
2. **Tame `rclone-lts-photo` log + RSS pressure.** 17 GiB resident, 4873 log lines in 24 min. Lower `--log-level` from `INFO` to `NOTICE` once the photo-library backfill finishes; consider a `MemoryHigh=` / `MemoryMax=` on the user unit so a runaway upload can't squeeze the rest of the session.
3. **Cut `warp-svc` log verbosity.** 504 DEBUG lines per boot is not appropriate for steady-state. Drop to INFO and accept that the startup NTP/DNS warnings are a known WARP-on-Linux race — they don't indicate a real problem.
4. **Trim `NetworkManager-wait-online.service` (5.79 s).** Either drop `network-online.target` dependencies that don't truly need DHCP completion, or `systemctl edit NetworkManager-wait-online` to set `NM_ONLINE_TIMEOUT=5` and a stricter `--exit` flag. The current 5.8 s on Wi-Fi DHCP is the largest avoidable userspace stall.
5. **Investigate the NetworkManager SIGABRT on 2026-05-21 11:55.** Single event, no recurrence in this boot, but the `10-timeout-abort.conf` drop-in is now applied to every service — meaning *any* slow stop turns into a coredump. Worth confirming whether NM was killed during a suspend/resume cycle (X1 Carbon Gen 11 has known suspend quirks).

## Out-of-scope

- **Firmware (10.1 s) and loader (5.0 s)** dominate boot. These are BIOS/UEFI + GRUB — handled via firmware updates and bootloader config, not systemd. Track in a separate plan if real-world boot speed matters.
- **LUKS unlock for `/home` (10.6 s)** is interactive and a security choice. Not a defect.
- **`raid-check.timer`** is harmlessly enabled by `mdadm` despite no MD arrays. Cosmetic.
- **`vsftpd` SIGABRT cluster** belongs to the FTP-camera plan (recent commits 0a4ae9b / 5a1d7bc / 669fe51), not this audit.
- **GDM-session race (gnome-software, evolution-alarm-notify, GlobalShortcutsProvider one-shot failures)** — self-healing, vendor-side, not actionable here.
- **34 boots / ~50 days** is normal laptop usage. No reboot-storm pattern.
