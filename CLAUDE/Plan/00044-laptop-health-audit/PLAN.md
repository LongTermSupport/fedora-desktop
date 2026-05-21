# Plan 00044: Laptop Health Audit & Tuning

**Status**: In Progress — Phase 1 complete
**Created**: 2026-05-21
**Owner**: joseph (with Claude assistance)
**Priority**: Medium

## Overview

Comprehensive read-only audit of the daily-driver laptop to surface anything that could make it run more reliably, more quietly, or longer on battery. Four parallel sub-agents covered:

1. Kernel ring / dmesg / firmware / driver health
2. systemd unit health + boot timing
3. Package state, repos, and security advisories
4. Power, thermal, suspend, and battery health

The headline result: **the system is healthy.** Zero kernel taint, zero out-of-tree modules currently in use, no oops/NMI/soft-lockup in 7 days, no firmware loader failures, no OOM kills in 7 days, suspend works reliably, battery at 100% of design with charge thresholds already configured. Most findings are small wins, not fires.

The audit also corrected hardware identification used in Plan 00043: this is a **ThinkPad X1 Carbon Gen 10** with **i7-1260P** (Alder Lake-P, 16 threads), not Gen 11 / i7-1365U as previously assumed. DMI `product_version` + `/proc/cpuinfo` confirmed by two independent agents. Lenovo machine code `21CB007CUK` ⇒ Gen 10.

## Goals

- Apply the high-impact actionable findings (security updates, power profile, redundant service masks).
- Decide on the medium-impact polish items (log verbosity, boot-time stalls, kernel versionlock).
- Codify the chosen fixes as Ansible playbooks where they touch system state, per the IaC rule.
- Leave the laptop tangibly better than it started: faster boot, longer battery, quieter journal.

## Non-Goals

- Anything in Plan 00043 (IPU6 / kernel 7.0.9 recovery is tracked there).
- BIOS/firmware updates — no current updates outstanding per `fwupdmgr`.
- Re-architecting any working subsystem (audio, networking, GNOME session) — current state is healthy.
- Enabling Secure Boot — separate decision; flagged for awareness only.

## Context & Background

- Audit ran 2026-05-21 on kernel 6.19.14-200.fc43.x86_64.
- Four research docs in this directory:
  - `research-01-kernel-dmesg.md` — kernel/firmware/driver health
  - `research-02-systemd-boot.md` — systemd units + boot timing
  - `research-03-packages.md` — packages + repos + security
  - `research-04-power-thermal.md` — power, thermal, suspend
- All research used read-only commands; no system state was modified.
- Power agent confirmed: Fedora's `tuned` + `tuned-ppd` is the modern blessed path (replacing TLP and the older `power-profiles-daemon` package). The current active profile is `balanced` — the desktop default, not laptop-optimised.

## Prioritised Findings

### 🔴 High Impact (do soon)

1. **Apply Critical security updates** — Firefox 151, nss/nspr 3.123.1 bundle, yelp 49.1, evince 48.1 are all Critical-severity Fedora advisories. Doable independently of Plan 00043 recovery via `dnf upgrade --exclude='kernel*'`. *(45 advisory rows total: 3 Critical, 5 Important, 1 Moderate, 1 Low, 1 None.)*
2. **Switch `tuned` profile from `balanced` to `balanced-battery`** — single coordinated change to EPP, governor bounds, ASPM policy + ACPI platform_profile. Current `balanced` is the desktop default. Ansible playbook target.
3. **Resolve Plan 00043 partial-kernel-7.0.9 install** — cross-referenced; tracked separately.
4. **Mask `thermald.service`** — enabled but exits at every boot after detecting `dytc_lapmode`. Lenovo DYTC handles thermal at firmware level. Misleading "enabled" state. Ansible target.
5. **Apply remaining ~124 pending updates** *(after the security batch and kernel decision is made)* — clears `kernel-headers` drift (stuck at 6.19.6 while running 6.19.14), bulk of which is KF6 6.26.0 (36 pkgs) + LibreOffice 25.8.7.3 (19 pkgs).

### 🟡 Medium Impact (polish, do when convenient)

06. **Lower `warp-svc` log verbosity** — 504 DEBUG + 7 ERROR + 37 WARN lines per boot; startup chatter is a known WARP-on-Linux NM race. Drop to `INFO` or lower.
07. **Tame `rclone-lts-photo` user-mount** — 4 873 log lines per 24 min and 17.1 GiB RSS. Drop `--log-level INFO` → `NOTICE` once photo backfill is done; add `MemoryHigh=`/`MemoryMax=` to the user unit.
08. **Reduce `NetworkManager-wait-online.service` (5.79 s boot stall)** — set `NM_ONLINE_TIMEOUT=5`, or unhook units from `network-online.target` that don't actually need DHCP completion.
09. **Decide on the `kernel` versionlock** — the system is reportedly version-locked at `kernel-6.19.14-200`, silently deferring all kernel security updates and blocking the natural path to fix Plan 00043. Worth a deliberate decision: keep, raise, or remove. *(Needs verification — check `dnf versionlock list`.)*
10. **Install `lm_sensors`** so `sensors` is available for future audits. One-line package add.

### ⚪ Low Priority (cosmetic or watch-only)

11. **Disable `copr:phracek/PyCharm` COPR** — no `pycharm-*` RPM installed (Toolbox-managed). Zero-risk removal.
12. **Blacklist `mtk_t7xx` 5G modem** if WWAN is genuinely unused — silences "Port AT is not opened, drop packets" boot chatter. The radio is already rfkill-blocked, so this is purely cosmetic.
13. **NetworkManager SIGABRT from 2026-05-21 11:55** — single event. Fedora's `10-timeout-abort.conf` drop-in turns slow-stopping services into coredumps. Likely a suspend/resume teardown race. Correlate with suspend logs before tuning; no action if it doesn't recur.
14. **Test NVMe `power/control=auto`** — only suspendable PCIe device still on `on`. Test manually, watch for Samsung PM9A1 I/O regressions, only then add to Ansible.
15. **ELAN touchpad `i2c_hid_acpi` incomplete-report at boot** — harmless. Watch for recurrence after suspend/resume; if so, the known ELAN i2c-hid post-wake bug fix (small systemd unit) applies.

### ⚪ Confirmed non-issues (recorded so we don't re-investigate)

- Stay on `i915`, not `xe` — `xe` is loaded but inactive; ADL-P is not its target.
- i915 PSR "Selective fetch area calculation failed in pipe A" — known benign ADL-P edge-case; upstream quietened in 7.0.9.
- NVMe APST enabled, no errors, PCIe L1.1/L1.2 substates active.
- Audio SOF + ALC287 — clean.
- Bluetooth + iwlwifi — clean (1.48 s BT load is normal for AX211).
- IPU6 camera — probes clean now that mainline driver is in use (post Plan 00043 playbook fix).
- Battery: 100% of design (57.57/57 Wh, 71 cycles), charge thresholds 75/80 — already correct.
- s2idle suspend mode — correct for Alder Lake-P; suspend cycles clean.
- All USB devices at `power/control=auto`.
- `dnf check` — clean, no broken deps.

## Tasks

### Phase 1: Audit ✅

- ✅ Dispatched 4 parallel read-only sub-agents.
- ✅ Captured research docs `research-01-…` through `research-04-…`.
- ✅ Corrected hardware identification (Gen 10 / i7-1260P, not Gen 11).
- ✅ Prioritised findings into ranked list above.

### Phase 2: Quick wins (no Ansible required) ⬜

- ⬜ `dnf upgrade --refresh --exclude='kernel*'` — pulls Critical security updates without touching the partial-7.0.9 trap. Coordinate with Plan 00043 — do **not** run until Plan 00043 Phase 3 recovery has been decided. Otherwise, dnf may try to "fix" the partial 7.0.9 install in an unpredictable way.
- ⬜ Verify `dnf versionlock list` to confirm whether kernel is locked and decide policy.

### Phase 3: Ansible playbook changes ⬜

Create / update playbooks for the durable system changes:

- ⬜ **`play-laptop-power-tuning.yml`** (or extend an existing power play) — set `tuned` active profile to `balanced-battery` on AC and battery via `tuned-adm profile balanced-battery` made idempotent. Verify with `tuned-adm active`. Tag: `power`.
- ⬜ **Mask `thermald.service`** — extend an existing laptop playbook or add to power tuning play. Use `ansible.builtin.systemd` with `masked: true`.
- ⬜ **`play-warp-quiet.yml`** *(optional)* — drop warp-svc log level. Single config-file edit.
- ⬜ **Update rclone user-unit template** — log level `NOTICE`, `MemoryHigh`/`MemoryMax`. Locate the existing rclone playbook before adding.
- ⬜ **Add `lm_sensors` to a base packages play** so `sensors` is universally available.
- ⬜ **Disable stale COPR**: extend the repos play (or relevant copr-add play) to remove `phracek/PyCharm`.

### Phase 4: Watch-only items ⬜

Per the "Low Priority" section, leave watch flags rather than fixing:

- ⬜ Re-check ELAN touchpad after a suspend/resume cycle. If "incomplete report" recurs post-resume, plan the systemd workaround.
- ⬜ Re-check NetworkManager coredump frequency in 30 days. If only the 2026-05-21 one-off, close as transient.
- ⬜ Decide on `mtk_t7xx` blacklist — is the 5G modem ever going to be used? If no: Ansible `/etc/modprobe.d/blacklist-mtk-t7xx.conf` via blockinfile. If yes: leave the boot chatter.

### Phase 5: Verification ⬜

After Phase 3:

- ⬜ Reboot, capture new `systemd-analyze blame` and confirm `NetworkManager-wait-online` is below 2 s.
- ⬜ `tuned-adm active` shows `balanced-battery`.
- ⬜ `systemctl is-enabled thermald` returns `masked`.
- ⬜ `journalctl -b 0 -p warning --no-pager > /tmp/warn.txt` and confirm line count drops noticeably.
- ⬜ Battery time-to-empty under typical idle-and-Firefox usage compared to a pre-tuning baseline.

## Dependencies

- **Plan 00043 (IPU6 webcam fallout)**: Phase 2 of this plan (`dnf upgrade`) must coordinate with Plan 00043 Phase 3 (kernel 7.0.9 recovery). Run the kernel decision first, then upgrade non-kernel packages.

## Technical Decisions

### Decision 1: `tuned` profile selection

**Context**: Active is `balanced` (desktop default). Available laptop-oriented options: `balanced-battery`, `powersave`, `laptop-battery-powersave`.

**Options**:

1. **`balanced-battery`** — laptop-aware variant of balanced; coordinates EPP, ASPM, platform_profile.
2. **`powersave`** — more aggressive; may noticeably slow heavy workloads.
3. **`laptop-battery-powersave`** — aggressive battery-life focus.

**Decision**: Start with **`balanced-battery`** — coordinated, conservative, low risk. Can move further down the scale later if battery life still falls short.

**Date**: 2026-05-21

### Decision 2: Keep `i915` (not `xe`)

**Context**: Both `i915` and `xe` modules are loaded. `xe` is the newer driver but targets newer Intel GPUs (Lunar Lake, Battlemage). On ADL-P its refcount is 0.

**Decision**: **No change.** `i915` is the correct driver for this iGPU. Recorded so we don't re-investigate.

**Date**: 2026-05-21

### Decision 3: Don't enable Secure Boot in this plan

**Context**: Power agent flagged Secure Boot is disabled. Enabling it is a separate, larger decision (signing kernel modules, MOK enrollment, akmod implications — see Plan 00043).

**Decision**: **Out of scope.** Document, don't touch.

**Date**: 2026-05-21

## Success Criteria

- ⬜ All 🔴 high-impact items either done or have a documented decision to defer.
- ⬜ `tuned-adm active` shows `balanced-battery` (or a deliberately-chosen alternative).
- ⬜ `systemctl --failed` shows zero failed units (post Plan 00043 playbook re-run, which masks `v4l2-relayd@icamerasrc.service`).
- ⬜ No Critical security advisories outstanding.
- ⬜ Boot time userspace phase < 10 s (currently 11.9 s).
- ⬜ All decisions and watch-only items recorded.

## Risks & Mitigations

| Risk                                                                        | Impact | Probability | Mitigation                                                                                                                  |
| --------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------------------------- |
| `dnf upgrade` resolves the partial 7.0.9 install in an unexpected way       | High   | Med         | Coordinate strictly with Plan 00043. Run kernel-specific decisions first; use `--exclude='kernel*'` for non-kernel updates. |
| `balanced-battery` profile causes a noticeable performance hit              | Med    | Low         | Easy revert: `tuned-adm profile balanced`. Playbook should be parameterised so the profile is a single variable.            |
| Masking `thermald` masks a future fallback we'll need                       | Low    | Low         | Document the unmask command in the playbook header. DYTC is firmware-level; if firmware ever stops doing thermal, unmask.   |
| Lowering `NetworkManager-wait-online` timeout breaks a unit that needs DHCP | Low    | Low         | Test by booting after change; revert via `systemctl edit --runtime` if anything fails.                                      |

## Notes & Updates

### 2026-05-21

- Audit complete (Phase 1).
- Cross-cutting finding: this laptop is genuinely well-set-up already. No emergencies, mostly polish.
- Awaiting user backup confirmation before any Phase 2 / Phase 3 work — per user instruction "no further system changes until I confirm I have backed up important stuff".
- Hardware-ID correction (Gen 10 / i7-1260P) will be propagated to Plan 00043 as a small follow-up.
