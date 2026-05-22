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
- **IaC cross-check** (`00-iac-cross-check.md`): each prioritised action was re-evaluated against the playbooks in this repo to filter real gaps from busywork. Headline: 11 real issues, 1 repo-policy "fight" (Action 9), 1 partial conflict (Action 7 log-level), 1 unclear (Action 6). Findings have been folded into the action notes below.

## Prioritised Findings

### 🔴 High Impact (do soon)

1. **Apply Critical security updates** — 🟢 real. Firefox 151, nss/nspr 3.123.1 bundle, yelp 49.1, evince 48.1 are all Critical-severity Fedora advisories. **Already solved by `playbooks/imports/play-AB-dnf-upgrade.yml`** (commit `cfea82d`); run that play rather than ad-hoc `dnf upgrade`.
2. **Switch `tuned` profile from `balanced` to `balanced-battery`** — 🟢 real. Confirmed no repo policy on `tuned` (TLP was archived 2025-08-29 with a note to use "Fedora defaults"). Setting a deliberate profile via new playbook is a clean gap-fill. Single coordinated change to EPP, governor bounds, ASPM policy + ACPI platform_profile.
3. **Resolve Plan 00043 partial-kernel-7.0.9 install** — ✅ done (Plan 00043 closed).
4. **Mask `thermald.service`** — 🟢 real. No repo policy on `thermald`. Enabled state came from package default. Masking doesn't fight any IaC.
5. **Apply remaining ~124 pending updates** — 🟢 real. **Same playbook as Action 1** (`play-AB-dnf-upgrade.yml` does `dnf "*" state: latest`). Collapses into a single run.

### 🟡 Medium Impact (polish, do when convenient)

06. **Lower `warp-svc` log verbosity** — ⚪ unclear. `play-cloudflare-warp.yml` configures warp-cli but does not touch warp-svc log level. **Verify first** that the log-config surface is a Cloudflare-supported stable interface (not auto-rewritten on package upgrade) before codifying. If unstable, defer.
07. **Tame `rclone-lts-photo` user-mount** — split into two subtasks per IaC cross-check:
    - 7a. 🟠 **Log level**: per `play-rclone.yml:271`, the unit template hard-codes `--log-level INFO` deliberately (header explains: "leaves a real exit code and log trail"). **Do NOT change the template default.** Instead set `log_level: NOTICE` for the photos mount specifically via the `rclone_mounts` per-mount override the template already supports.
    - 7b. 🟢 **Memory limits**: real gap. The unit template has no `MemoryHigh`/`MemoryMax`. Add them to the template — this is a generic improvement, no policy conflict. (17.1 GiB RSS observed on the photos mount.)
08. **Reduce `NetworkManager-wait-online.service` (5.79 s boot stall)** — 🟢 real. No repo policy on this unit. Set `NM_ONLINE_TIMEOUT=5`, or unhook units from `network-online.target` that don't actually need DHCP completion.
09. ~~**Decide on the `kernel` versionlock**~~ — 🟡 **REPO POLICY — busywork to revisit.** The lock on `kernel = 6.19.14-200` is set by `play-advanced-kernel-management.yml` as the previous-minor hardware-compat fallback. Plan 00043 explains the same intent. **Action**: confirm the lock is in place (it is); do not "decide" anything else.
10. **Install `lm_sensors`** — 🟢 real. No repo reference. One-line addition to a base packages play.

### ⚪ Low Priority (cosmetic or watch-only)

11. **Disable `copr:phracek/PyCharm` COPR** — 🟢 real. No playbook references `phracek` or PyCharm; the only repo-managed COPR is `pgdev/ghostty` (`play-terminal-emulators.yml:31`). The PyCharm COPR is orphaned state drift from a removed playbook or historical manual step. Clean removal.
12. **Blacklist `mtk_t7xx` 5G modem** — 🟢 real (if WWAN unused). No repo policy on modprobe blacklists (`files/etc/modprobe.d/` doesn't exist). Clean addition once the policy decision is made.
13. **NetworkManager SIGABRT from 2026-05-21 11:55** — 🟢 real, watch-only. Observational, no IaC change implied.
14. **Test NVMe `power/control=auto`** — 🟢 real. No repo policy; correctly framed as test-first.
15. **ELAN touchpad `i2c_hid_acpi` incomplete-report at boot** — 🟢 real, watch-only. Observational.

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

### Phase 2: Apply updates via existing playbook ⬜

- ⬜ **Run `playbooks/imports/play-AB-dnf-upgrade.yml`** — IaC-native answer to Actions 1 + 5 (Critical security updates + remaining ~124 pending). Existing play, no edit needed. Plan 00043 already complete so the kernel-trap coordination concern is resolved.
- ✅ ~~Verify versionlock~~ — confirmed: `kernel = 6.19.14-200` is the deliberate previous-minor fallback set by `play-advanced-kernel-management.yml`. No further action.

### Phase 3: Ansible playbook changes ⬜

Create / update playbooks for the durable system changes:

- ⬜ **`play-laptop-power-tuning.yml`** (new) — set `tuned` active profile to `balanced-battery` AND mask `thermald.service` (Actions 2 + 4 — both clean gaps, single playbook covers both). Verify with `tuned-adm active`. Tag: `power`.
- ⬜ **Update rclone playbook**:
  - 7a. In `rclone_mounts` config (likely `host_vars/localhost.yml`), set `log_level: NOTICE` for the photos mount — uses the existing per-mount override in `play-rclone.yml:271`. Do NOT touch the template default.
  - 7b. In the user-unit template in `play-rclone.yml` (around lines 255-277), add `MemoryHigh=` and `MemoryMax=` directives. Generic improvement.
- ⬜ **Reduce `NetworkManager-wait-online`** (Action 8) — drop-in or extend existing NM playbook; set `NM_ONLINE_TIMEOUT=5`.
- ⬜ **Add `lm_sensors`** (Action 10) to a base packages play.
- ⬜ **Disable `copr:phracek/PyCharm`** (Action 11) — extend the repos / cleanup play with a `community.general.dnf_copr` task (`state: disabled`).
- ⬜ *(Conditional)* **`play-warp-quiet.yml`** (Action 6) — only after confirming the warp-svc log-config surface is stable across upgrades. Otherwise defer.

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

### 2026-05-22

- IaC cross-check performed (sub-agent), output in `00-iac-cross-check.md`. Headline: most actions are real, but several need reframing:
  - Actions 1 + 5 collapse into "run `play-AB-dnf-upgrade.yml`" (already exists, IaC-native).
  - Action 7 split: log level half FIGHTS the repo's deliberate INFO default — must be done via per-mount override, not template change. Memory limits half is a real gap.
  - Action 9 (versionlock) is busywork — deliberate previous-minor fallback policy. Downgraded to "confirm" only.
  - Action 6 (warp-svc) marked unclear pending verification of the log-config surface stability.
- Plan 00043 closed (kernel recovery verified on 7.0.9-105). Phase 2 of this plan no longer needs coordination with Plan 00043.
