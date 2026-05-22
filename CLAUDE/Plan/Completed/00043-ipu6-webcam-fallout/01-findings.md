# Synthesised Findings

## Timeline (UTC+1 / BST)

| Time             | Event                                                                                                                                                                    |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 2026-05-21 18:21 | dnf transaction 81: playbook installs IPU6 stack — pulls in **kernel-7.0.9 partial** + `kernel-devel-matched-7.0.9` + akmod-intel-ipu6. **Also removes kernel-6.19.13.** |
| 18:24            | dnf txn 82/83: install freshly built `kmod-intel-ipu6` and `kmod-v4l2loopback` RPMs from akmods cache.                                                                   |
| 19:05 (boot -4)  | Reboot into 7.0.4-100 (because 6.19.14 was current). Camera worked end-to-end via libcamera+pipewire. Playbook session was here.                                         |
| 21:16 (boot -3)  | Reboot — grub defaults to newest available = **7.0.9-104**. **No WiFi, no BT**. User noticed all radios dead. Shut down.                                                 |
| 21:23 (boot -2)  | Reboot — same 7.0.9, same outcome. Tried again.                                                                                                                          |
| 21:45 (boot -1)  | Reboot, manually selected **7.0.4** in grub. Logged in to GNOME. **Hard freeze** ~1m 47s later. Power-cycled.                                                            |
| 21:48 (boot 0)   | Manually selected **6.19.14** in grub. Working. Currently here.                                                                                                          |

## Diagnosis 1 — kernel 7.0.9 has no radios (CONFIRMED)

Direct check of installed packages and module trees:

```
$ rpm -qa | grep ^kernel | sort
kernel-6.19.14-200.fc43.x86_64               # full
kernel-7.0.4-100.fc43.x86_64                 # full
kernel-core-6.19.14-200.fc43.x86_64
kernel-core-7.0.4-100.fc43.x86_64
kernel-core-7.0.9-104.fc43.x86_64            # PARTIAL — no `kernel-7.0.9`
kernel-devel-7.0.9-104.fc43.x86_64
kernel-devel-matched-7.0.9-104.fc43.x86_64
kernel-modules-6.19.14-200.fc43.x86_64
kernel-modules-7.0.4-100.fc43.x86_64
kernel-modules-core-6.19.14-200.fc43.x86_64
kernel-modules-core-7.0.4-100.fc43.x86_64
kernel-modules-core-7.0.9-104.fc43.x86_64    # but no `kernel-modules-7.0.9`
kernel-modules-extra-6.19.14-200.fc43.x86_64
kernel-modules-extra-7.0.4-100.fc43.x86_64   # and no `kernel-modules-extra-7.0.9`
...
```

And the on-disk module dir for 7.0.9:

```
$ ls /lib/modules/7.0.9-104.fc43.x86_64/kernel/drivers/net/wireless/intel/iwlwifi/
dvm  mld  mvm  tests          # subdirs exist but EMPTY of .ko.xz files
$ ls /lib/modules/7.0.9-104.fc43.x86_64/kernel/drivers/bluetooth/
ls: cannot access '...': No such file or directory
```

Compare 7.0.4 which has `iwlwifi.ko.xz`, `btusb.ko.xz`, `btintel.ko.xz`, `btintel_pcie.ko.xz`, `hci_uart.ko.xz`, etc.

**Conclusion**: 7.0.9 is half-installed. NetworkManager and bluez start fine but find no devices to operate, because the drivers simply do not exist on disk.

### Why was the install partial?

dnf history transaction 81 (the playbook run) installed `kernel-devel-matched`, which `Requires: kernel-devel = %{version}`, which pulls a specific `kernel-devel`. dnf then dragged in `kernel-core` and `kernel-modules-core` (transitive deps of `kernel-devel` build environment) but **NOT** the full `kernel` metapackage that pulls `kernel-modules` + `kernel-modules-extra`.

This is a known gotcha: installing `kernel-devel` directly does NOT guarantee the matching full `kernel` is installed. The repo metadata makes `kernel-devel` standalone-installable; the upgrade path normally goes through `kernel` (the meta) which pulls `kernel-devel` as a Recommends.

## Diagnosis 2 — kernel 7.0.4 freeze (PROBABLY UNRELATED)

Boot -1 last 30 seconds before freeze:

- IPU6 / camera stack quiet since 21:46:41 (about 30s before the freeze at 21:47:11)
- `v4l2-relayd@icamerasrc.service` crashed 5× early (21:45:37–21:45:39) due to missing `SPLASHSRC` env var — this is the known Fedora stub-without-setup.cfg behaviour; systemd hit start-limit, gave up, no further attempts.
- Wireplumber/pipewire spammed 29× `Cannot open '/dev/videoN': Permission denied` (the IPU6 subdev nodes; pipewire was trying them all) — stopped at 21:46:41.
- Final ~30s: routine GNOME activity (xdg-portal, firefox D-Bus timeout, GJS warnings). No `BUG:`, no `Oops`, no `soft lockup`, no `RIP:`, no GPU hang, no panic anywhere in 1401 kernel-ring lines.
- Journal ends abruptly at 21:47:11 — no shutdown, no Journal-stopped message → hard hang.

**Conclusion**: most likely a spurious one-off hardware/firmware-level hang. The IPU6 stack had quiesced ~30s prior. The same kernel + same IPU6 stack worked perfectly on boot -4 the same day. Not enough evidence to blame the playbook. Worth keeping the v4l2-relayd@icamerasrc cosmetic cleanup in the followup, but no rollback warranted.

## Diagnosis 3 — akmod-intel-ipu6 build state (POOR, BUT NOT RADIO CAUSE)

Per `/var/cache/akmods/intel-ipu6/*.log`:

| Kernel    | akmod outcome                                                                                                                                                                                                                                                                                                                    |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 7.0.9-104 | **Built**, but only `intel-ipu6-psys.ko` + i2c sensor stubs. No `intel-ipu6-isys.ko` (intentional — isys is now in mainline). **210 depmod WARNINGs** about the /extra/intel-ipu6/ tree — symbol-version mismatches against mainline `intel-ipu6` it depends on. Module loads but is functionally useless on this hardware path. |
| 7.0.4-100 | **Build never attempted** — kernel-devel-7.0.4 not installed.                                                                                                                                                                                                                                                                    |
| 6.19.14   | **Build never attempted** — kernel-devel-6.19.14 not installed.                                                                                                                                                                                                                                                                  |

**Implication**: the akmod ships a partial overlay (`extra/intel-ipu6/.../psys/intel-ipu6-psys.ko.xz`) that declares `depends: intel-ipu6` against the in-tree core. modinfo shows it tries to bind via `auxiliary:intel_ipu6.psys` — designed to load *alongside* mainline. But with 210 depmod symbol warnings, even the load attempt is risky. **On Fedora 43+ with mainline IPU6 isys+psys+core in-tree, the akmod is redundant at best and harmful at worst.**

No modprobe blacklist file is shipped by the akmod (we verified `/etc/modprobe.d/` and `/usr/lib/modprobe.d/` — no ipu6 entries). So both in-tree and out-of-tree modules can try to load; the kernel will pick whatever depmod resolves first (in-tree wins on a default install because /extra/ ordering is implementation-dependent).

## What we should change

1. **Playbook**: drop `akmod-intel-ipu6` on Fedora 43+ — mainline kernel ships everything needed. Keep the userspace bits (`ipu6-camera-bins`, `ipu6-camera-hal`, `gstreamer1-plugins-icamerasrc`, `v4l2-relayd`, `libcamera-tools`, `v4l-utils`). See `06-playbook-fix-proposal.md`.
2. **Playbook**: do not install `kernel-devel` / `kernel-headers` unconditionally. They're only needed if we install an out-of-tree kmod, which we're now removing. If we keep them, ensure the matching full `kernel` package is also installed.
3. **System recovery**: fix kernel 7.0.9 by installing the missing `kernel-modules` + `kernel-modules-extra`. See `05-recovery-plan.md`.
