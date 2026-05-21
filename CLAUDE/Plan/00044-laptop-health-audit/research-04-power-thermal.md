# Research 04: power / thermal / suspend audit

Read-only audit of power, thermal, and suspend behaviour. Data captured `2026-05-21` on Fedora 43, kernel `6.19.14-200.fc43.x86_64`. All raw artefacts under `/tmp/audit04/`.

## Summary

The machine in question is actually a **ThinkPad X1 Carbon Gen 10** (DMI `21CB007CUK`, product_version `ThinkPad X1 Carbon Gen 10`) running a 12th-gen **i7-1260P** (Alder Lake-P, 16 threads — `family:model:stepping 0x6:9a:3`). The orchestrator brief said Gen 11 / i7-1365U; the hardware is one generation older. None of the audit conclusions change as a result — Alder Lake-P power/thermal characteristics are the same — but plan 00044 may want to update the hardware identification.

Overall the system is healthy and broadly well-tuned: s2idle is the active suspend mode (correct for this generation, S3 not supported), the battery is at 100% of design with only 71 cycles, charge thresholds are already configured (75/80, the ideal long-life setting), NVMe APST is enabled and aggressive (100 ms → PS4), and PCIe L1/L1.1/L1.2 substates are active on the NVMe and the 5G modem. There are **no firmware updates outstanding**, **no thermal throttling events**, **no spurious wakes**, and **no fwupd refresh failures** in the last 7 days. Secure Boot is **disabled** — flagged for awareness, not as a power finding.

The genuine power-tuning opportunities are second-order: the system uses Fedora 43's modern stack (`tuned` + `tuned-ppd`) with the **`balanced` profile active**, which is the desktop default rather than the laptop-optimised `balanced-battery` (or `powersave`) variant. EPP is `balance_performance` across all cores, fans are spinning at ~3.3-3.9 kRPM at ~50 °C package, and `hwp_dynamic_boost=0` is correct for thermal but means the system has no "snap to peak" behaviour. `thermald` is enabled but **exits at boot** because it detects `dytc_lapmode` (a ThinkPad-specific signal) and refuses to run — this is by design but means the unit is wasting a `systemctl enable` and could be masked.

## Power policy stack

- **Daemon**: neither `power-profiles-daemon` nor `tlp` is installed. Fedora 43 uses **`tuned` (2.27.0)** as the underlying engine, with **`tuned-ppd` (2.27.0)** as a PPD-compatible shim exposing the standard PPD D-Bus API on top of tuned.
- **Services**: `tuned.service`, `tuned-ppd.service`, `upower.service` all `active (running)` and `enabled`.
- **Active profile**: `tuned-adm active` → **`balanced`** (the general non-specialised profile). `tuned-adm recommend` → `balanced`.
- **ACPI platform profile**: `/sys/firmware/acpi/platform_profile` = `balanced`. Choices available: `low-power`, `balanced`, `performance`. (The `thinkpad_acpi` driver does not expose a per-platform-profile file directly — Lenovo's DYTC mechanism is surfaced via the standard ACPI interface on Gen 10.)
- **TLP**: not installed. No conflict to flag.
- **thermald**: package installed, service enabled, but **exits successfully at boot** after detecting `dytc_lapmode` and logging `Thermald can't run on this platform`. Lenovo DYTC handles thermal management at the firmware level — this is expected behaviour. The enabled unit is a noop; masking it would tidy the boot.

## CPU frequency / pstate

- `intel_pstate/status` = **`active`** (HWP, Intel's hardware-managed P-states — best on Alder Lake-P).
- `scaling_driver` = `intel_pstate`, `scaling_governor` = **`powersave`** uniformly across all 16 logical cores (this is the only HWP-active governor; `performance` is the alternative).
- **EPP** (`energy_performance_preference`): `balance_performance` uniform across all cores. Available options: `default / performance / balance_performance / balance_power / power`.
- **Turbo**: `no_turbo=0` (turbo enabled).
- **HWP dynamic boost**: `hwp_dynamic_boost=0` (disabled). On Alder Lake-P the kernel default is off; enabling it gives snappier short-bursts at the cost of slightly more power.
- `min_perf_pct=10`, `max_perf_pct=100`, `base_frequency=2.1 GHz`, `cpuinfo_max_freq=4.7 GHz`, `cpuinfo_min_freq=400 MHz`. `scaling_cur_freq` sampled at 1.34 GHz with `cpuinfo_avg_freq` 1.99 GHz — sensible idle behaviour.

## Thermal & throttling

- `lm_sensors` is **not installed** (`sensors` command not found). Read directly from sysfs instead.
- **CPU package temp** (`x86_pkg_temp` thermal_zone8): **50 °C** at sample time.
- **Core temps** (coretemp hwmon8): 45-52 °C across all cores at idle.
- **NVMe temp**: 39.85 °C across all three composite sensors — well within spec.
- **WiFi temp** (`iwlwifi_1`): 40 °C.
- **Thinkpad acpi temps** (hwmon7): 48 °C primary; secondary sensors return ENXIO ("no such device") which is normal — thinkpad_acpi exposes a fixed sensor array, not all populated.
- **Fans**: fan1 = **3901 RPM**, fan2 = **3265 RPM**, `pwm1_enable=2` (firmware/automatic mode). This is high for ~50 °C — the EC fan curve is aggressive at idle. Not a Linux-side tunable on Gen 10; suggests the user could investigate firmware-level "Quiet" thermal mode in BIOS / via `fw-fanctrl` is not applicable here as that's Framework-specific. Could be normal for X1C Gen 10's known thermal characteristics. Worth noting but not actionable from Ansible.
- **Throttling events**: `journalctl -k -b 0 -g "throttl|thermal|MSR|temperature above"` returned **only initialisation messages** (zone registration + `proc_thermal_pci 0000:00:04.0: enabling device`). **No runtime throttle events** in this boot. No `MSR_PERF_LIMIT_REASONS`, no `temperature above threshold`. Clean.

## Battery

- **Model**: Sunwoda `5B10W13975`, 57 Wh design (15.44 V × ~3.7 Ah).
- **Wear**: `energy_full = 57.57 Wh`, `energy_full_design = 57.00 Wh` → **0% wear** (slightly above design, common for fresh cells after early conditioning).
- **Cycle count**: **71** — very low.
- **Charge thresholds**: **already configured** — `charge_control_start_threshold = 75`, `charge_control_end_threshold = 80`. This is the optimal "long life" setting; nothing to change. Upower confirms `charge-start-threshold: 75%`, `charge-end-threshold: 80%`, `charge-threshold-supported: yes`.
- **Charge behaviour**: `[auto] inhibit-charge force-discharge` — the platform supports all three modes.
- **Current state at audit**: 78%, `Not charging` (between thresholds, holding).
- **Voltage**: 16.66 V — nominal.

## Suspend / resume

- **`/sys/power/mem_sleep`** = `[s2idle]` (no `deep` advertised — correct for this gen; ACPI supports `S0 S4 S5` only per kernel log, no S3).

- **`/sys/power/state`** = `freeze mem disk`.

- **This boot (~28 min uptime)**: no suspend cycles. SSH-suspend-guard custom service is running.

- **Last 7 days**: a **single** suspend cycle recorded — `May 14 22:26:47 → May 15 21:47:29` (~23 hours, exit clean). 17 PM-related log lines total. **No failed suspends.**

- **Spurious wakes** (`journalctl -k -g "spurious|wakeup from"`): **no entries** in journal.

- **Wakeup sources** (active_count from `/sys/class/wakeup/*`):

  - `serio0` (PS/2 — internal keyboard): 3280 events
  - `i2c-ELAN067C:00` (touchpad): 27843 events
  - `ucsi-source-psy-USBC000:002` (USB-C port 2): 4 events
  - `ucsi-source-psy-USBC000:001` (USB-C port 1): 1 event
  - `AC` (charger plug/unplug): 1 event
  - everything else: 0

  Keyboard + touchpad as top wakers is expected. AC + USB-C events match plug/unplug. No problem signals.

- **ssh-suspend-guard.service** (custom unit in `/etc/systemd/system/`) is actively running — `/usr/local/bin/ssh-suspend-guard`, polling every 10 s, ~6.78 s CPU in 28 min uptime (about 0.4% CPU integrated, mostly the `sleep 10` loop overhead). Functional but worth confirming the polling interval is what was intended.

## Firmware / EFI

- `mokutil --sb-state`: **SecureBoot disabled**. Flagged for awareness; the orchestrator brief asked to confirm state. Not a power finding.
- `/sys/firmware/efi/efivars/`: populated (179 entries).
- **fwupdmgr get-updates** → **"No updates available"**. Latest firmware on: System Firmware (`0.1.54`), Embedded Controller (`0.1.26`), Battery firmware (`1.9.2`), Samsung NVMe (`EL2QGXA7`), Synaptics fingerprint (`10.01.3478575`), TPM (`1.512.0.0`), Intel ME (`1.40.2765`), UEFI dbx (`20250902`).
- **fwupd journal (last 7 days)**: 135 lines, **zero** containing `fail|error|refresh` failure markers.

## PCIe ASPM & device power management

- **ASPM policy** (`/sys/module/pcie_aspm/parameters/policy`): `[default] performance powersave powersupersave`. Default = let firmware decide.
- **NVMe (`04:00.0`, Samsung PM9A1)**:
  - `LnkCtl: ASPM L1 Enabled`
  - L1 substates **enabled** at both end and root: `L1SubCtl1: PCI-PM_L1.2+ PCI-PM_L1.1+ ASPM_L1.2+ ASPM_L1.1+`
  - `LTR+`, `T_PwrOn=500us`
  - **APST**: `nvme get-feature 0x0c` → **enabled**, ITPT = 100 ms into PS4 for entries 0-3. Aggressive idle-power policy applied.
  - `/sys/class/nvme/nvme0/device/power/control` = `on`, `runtime_status` = `active` — runtime PM is **not** set to `auto`. This is the **one PCIe runtime-PM gap**: changing it to `auto` would allow the controller itself to enter D3 when idle, on top of APST. PM9A1 has had historical kernel quirk drama (`pcie_aspm=off` workarounds), so the conservative `on` may be intentional via a kernel quirk for this controller — confirm before changing.
- **5G WWAN modem (`08:00.0`, MEDIATEK T700)**:
  - `LnkCtl: ASPM L0s L1 Enabled`
  - L1 substates **enabled**: `PCI-PM_L1.2+ PCI-PM_L1.1+ ASPM_L1.2+ ASPM_L1.1+`
  - Runtime PM = `auto`. Good.
- **WiFi**: CNVi (integrated, not a PCIe endpoint — uses `00:14.3` MAC/PHY shim). No ASPM applies. Power management via `iwlwifi` module parameters instead.
- **Other PCIe devices**: of 25 PCI devices, all are `auto` runtime-PM **except**:
  - `0000:00:00.0` (host bridge) — not suspendable, expected.
  - `0000:00:0a.0` (Platform Monitoring Technology) — by design.
  - `0000:00:12.0` (Integrated Sensor Hub) — by design (handles lid/hinge/lapmode).
  - `0000:04:00.0` (NVMe) — discussed above.
- **USB**: all 7 enumerated USB devices have `power/control = auto`. The audio HDA controller (`00:1f.3`) is `auto`. No fix needed for the "audio device wakes the system" ThinkPad annoyance — it's already configured correctly.
- **GPU (`00:02.0`, Iris Xe / Alder Lake-P GT2)**:
  - `i915_runtime_pm_status` debugfs node doesn't exist on this kernel version (path changed in modern i915/xe).
  - dmesg shows GuC 70.49.4 + HuC 7.9.3 authenticated, **SLPC enabled**, **RC enabled**, **GuC submission enabled** — all the modern power-savers are on.

## Recommended actions

Ranked by impact-to-effort. None are critical; this is fine-tuning of an already-healthy system.

1. **(High value, low effort, Ansible)** Switch the tuned profile to a battery-aware one. `balanced` is the desktop default; on a laptop the right baseline is `balanced-battery` (or for max savings, `powersave`). Add an Ansible task that runs `tuned-adm profile balanced-battery` (or sets `active_profile=balanced-battery` in `/etc/tuned/active_profile` declaratively). This single change adjusts EPP, governor min/max, and PCIe ASPM policy in a coordinated way. **Path**: new `playbooks/imports/play-power-tuning.yml` or extend an existing power-related play.

2. **(Medium value, low effort, Ansible)** Mask `thermald.service`. It boots, detects `dytc_lapmode`, refuses to run, and exits — a wasted ~7 ms of boot CPU and a misleading "enabled" state. Either `dnf remove thermald` or `systemctl mask thermald.service` via Ansible. The package is harmless installed but the enabled unit is noise.

3. **(Medium value, low effort, Ansible)** Install `lm_sensors` so future audits have `sensors` available without sysfs spelunking. One-line package add.

4. **(Low-medium value, requires testing, manual)** Set `/sys/bus/pci/devices/0000:04:00.0/power/control` to `auto` and observe stability over a few days. PM9A1 can sometimes regress under runtime PM with certain firmware. Don't ship this in Ansible until verified — start with a one-shot `echo auto | sudo tee …` after a backup, and watch for I/O hangs or extended resume latency.

5. **(Optional, BIOS-side, manual)** Investigate the BIOS "Intelligent Cooling" / DYTC platform profile setting. Fans at 3.3-3.9 kRPM at 50 °C package suggests the firmware fan curve is in "Balanced" or "Performance" mode. Switching the ACPI platform profile to `low-power` via `echo low-power | sudo tee /sys/firmware/acpi/platform_profile` (one-shot, write succeeds) would push DYTC into the quieter mode. Worth testing — and **if** the quieter behaviour is desired, this is what `balanced-battery` tuned profile sets automatically (recommendation #1).

6. **(Low value, awareness)** Audit the `ssh-suspend-guard` custom service polling interval — 10 s polling, ~0.4% CPU integrated. Acceptable but if there's a way to make it event-driven (e.g. via a PAM session counter or `loginctl`), that would be cleaner. Not a power-bleed concern.

7. **(Awareness, no action)** Secure Boot is disabled. The brief asked to confirm — confirmed. Plan 00044 can decide whether to track enabling it as a separate hardening item; it has no power/thermal implication.

8. **(Awareness, no action)** Hardware identification mismatch: brief says "Gen 11 / i7-1365U", DMI + cpuinfo say "**Gen 10 / i7-1260P**". Plan 00044 should correct the hardware description.

## Out-of-scope

- Any state-changing operation (writing tuned profiles, masking services, switching ACPI platform profile). This was a read-only audit; all "recommendations" above are advisory until executed via Ansible by the user on the host.
- Hibernation tuning (`/sys/power/state` advertises `disk` but the swap layout and `resume=` cmdline were not inspected — separate research item).
- Wayland / GNOME session-level power policy (idle dim, screen blank, automatic suspend timers) — handled by gsettings, not in this audit's scope.
- Per-device firmware update planning. `fwupdmgr get-updates` returned no updates, so this is moot for now.
- TLP/PPD migration discussion — neither is installed and Fedora 43's `tuned`+`tuned-ppd` stack is the modern blessed path.
