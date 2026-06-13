# Plan 00053: Fedora 44 Fresh-Install Audit

**Status**: In Progress
**Created**: 2026-06-13
**Owner**: Repo owner + Claude (Opus 4.7)
**Priority**: Medium

## Overview

The first fresh F44 host converged by this repo (a ThinkPad P14s Gen 5
with hybrid Intel Arc + NVIDIA RTX 500 Ada graphics) was audited using
the new `playbooks/dev/play-collect-diagnostics.yml` collector. The
snapshot lives at `untracked/diagnostics/<timestamp>/` with a companion
`ANALYSIS.md` that triages every finding. This plan turns the
*repo-fixable* subset of those findings into concrete playbook work,
splits them by reusability ("any F44 desktop" vs "this hardware class"),
and adds the follow-on dev-tooling fixes uncovered while running the
collector itself.

The work is also a chance to validate two F44-specific design choices
(TuneD as the Power Mode backend, and the existing thermal/DYTC
masking play) — confirm they behave as intended and document the
decisions so future audits don't re-flag them as gaps.

## Goals

- Eliminate the only post-converge failed unit
  (`NetworkManager-wait-online.service`).
- Eliminate the recurring journal errors at every boot (firewalld⇄docker
  NAME_CONFLICT, gkr-pam, intel-lpmd noise) so a fresh F44 converge ends
  with a clean journal-error feed.
- Make sure the existing thermal/Optimus plays actually apply to this
  hardware class (DYTC mask, NVIDIA Optimus) — not silently skipped.
- Fix the diagnostic collector's own bugs uncovered on first real run.
- Decide the firmware-update story (auto-update via `fwupdmgr` vs
  documented manual flow) and ship whichever the host needs.

## Non-Goals

- Manual host fixes — every change goes through Ansible per
  `CLAUDE/InfrastructureAsCode.md`.
- Performance tuning beyond removing real defects (this is an audit, not
  a tuning sprint).
- Migration to `power-profiles-daemon` — TuneD's `tuned-ppd` is the F44
  default and the GNOME Power Mode panel works fine through it. Decision
  recorded in §"Technical Decisions" so future audits don't re-flag.
- Touching the SPI-NOR JEDEC-ID kernel warning — that's an upstream
  kernel coverage gap, not something Ansible can paper over.

## Context & Background

- **Evidence snapshot:** `untracked/diagnostics/<timestamp>/` (the
  directory created by `playbooks/dev/play-collect-diagnostics.yml`).
  `ANALYSIS.md` in that directory is the source-of-truth triage; this
  plan only references it (the snapshot contains PII that must not leave
  the host).
- **Diagnostic collector:** new in this session — see commit
  `feat(dev): add playbooks/dev/ + collect-diagnostics`.
- **Hardware under test:** ThinkPad P14s Gen 5 (Lenovo `21G3S16D…`),
  Intel Core Ultra 7 165H (Meteor Lake), NVIDIA RTX 500 Ada hybrid GPU,
  TPM via STM ST33HTPHA, encrypted btrfs root.
- **Software under test:** Fedora 44 Workstation, kernel 7.0.12, GNOME
  on Wayland, NetworkManager + systemd-resolved, firewalld + Docker
  (rootful per the repo's container-engine policy), TuneD + tuned-ppd.

## Tasks

### Phase 1: Generic Fedora 44 fixes (apply to any host)

These appear (or are very likely to appear) on any F44 desktop the repo
converges. Fixing them in the imports/ playbooks helps every user.

- [ ] ⬜ **Task 1.1**: Resolve `NetworkManager-wait-online.service` boot
  failure

  - [ ] ⬜ Investigate why `play-network-wait-tuning.yml`'s current
    timeout extension isn't preventing the failure on a Wi-Fi-only
    boot.
  - [ ] ⬜ Decide between "mask the service" vs "raise
    `OnlineTimeoutSec=` further" (see Decision 1).
  - [ ] ⬜ Update `playbooks/imports/play-network-wait-tuning.yml`
    accordingly.
  - [ ] ⬜ Run QA: `./scripts/qa-all.bash`.
  - [ ] ⬜ Deploy on HOST, reboot, confirm `systemctl --failed` reports
    zero failed units.

- [ ] ⬜ **Task 1.2**: Resolve firewalld⇄docker NAME_CONFLICT at boot

  - [ ] ⬜ Reproduce: confirm
    `firewalld[…]: ERROR: NAME_CONFLICT: new_policy_object():     'docker-forwarding'` fires on every boot.
  - [ ] ⬜ Choose remediation between (a) adding a systemd drop-in to
    order `docker.service` strictly after `firewalld.service`, (b)
    having Ansible pre-create the `docker-forwarding` policy so
    firewalld sees it as managed, (c) running
    `firewall-cmd --reload` after docker has set up its chains. (See
    Decision 2.)
  - [ ] ⬜ Update `playbooks/imports/play-docker.yml`.
  - [ ] ⬜ Run QA + reboot + verify the warning is gone from the journal.

- [ ] ⬜ **Task 1.3**: Resolve `gkr-pam: unable to locate daemon control     file` at every GDM login

  - [ ] ⬜ Confirm whether `gnome-keyring` PAM auto-start is firing
    before the keyring daemon socket exists, or whether GDM is
    starting under a session env that doesn't expose
    `XDG_RUNTIME_DIR/keyring/control`.
  - [ ] ⬜ Audit `/etc/pam.d/gdm-password` order and the
    `pam_gnome_keyring.so` placement.
  - [ ] ⬜ If a fix is found, drop it into `playbooks/imports/play-gnome-shell.yml`
    (or a new `play-gnome-keyring.yml` if the changeset is large).

- [ ] ⬜ **Task 1.4**: Eliminate `intel_lpmd` boot noise on F44

  - [ ] ⬜ Confirm `intel_lpmd[…]: Open /proc/sys/kernel/sched_itmt_enabled     failed` is harmless (the daemon falls back, ITMT just isn't
    scheduled).
  - [ ] ⬜ Decide whether to mask `intel_lpmd.service` repo-wide (Decision
    3\) — it adds nothing on a TuneD-managed host that already does
    power-profile switching.

- [ ] ⬜ **Task 1.5**: Audit and document the SSH brute-force-style noise

  - [ ] ⬜ Identify the on-LAN source attempting auth and stop it (likely
    a stale `~/.ssh/known_hosts` or agent entry on another personal
    host).
  - [ ] ⬜ Decide whether `sshd_config.d/` should ship a
    `MaxAuthTries 3` / `LoginGraceTime` tightening via Ansible.
  - [ ] ⬜ This is a flag-and-decide task, not a guaranteed playbook
    change.

- [ ] ⬜ **Task 1.6**: Decide on `dnf-makecache.service` boot cost

  - [ ] ⬜ Confirm 33.9s wall-time blame is on first daily boot only.
  - [ ] ⬜ Decide whether to weaken its `After=` so it doesn't push
    graphical.target latency, or accept as-is (Decision 4).

### Phase 2: Hardware-specific fixes (ThinkPad / NVIDIA hybrid)

These apply only to a class of laptops (Lenovo DYTC ThinkPads, hybrid
Intel + NVIDIA Optimus on Meteor Lake). They belong under
`playbooks/imports/optional/hardware-specific/`.

- [ ] ⬜ **Task 2.1**: Confirm `play-laptop-thermal-diagnostics.yml`
  applied and stuck on this hardware

  - [ ] ⬜ Verify `thermald.service` is masked on this host.
  - [ ] ⬜ If the play didn't run, document why (probably "not in
    playbook-main.yml import chain — opt-in") and recommend the user
    run it on this hardware class.
  - [ ] ⬜ Confirm `Thermald can't run on this platform` warning
    disappears after the mask sticks.

- [ ] ⬜ **Task 2.2**: Resolve `bluetoothd: Failed to set default system     config for hci0`

  - [ ] ⬜ Classify: is this generic to any host with Bluetooth, or
    Intel-AX-2xx-specific? If generic, move to Phase 1.
  - [ ] ⬜ If a real config issue, drop `/etc/bluetooth/main.conf` via a
    new play (likely `play-bluetooth.yml` under `imports/`) or
    amend `play-basic-configs.yml`.

- [ ] ⬜ **Task 2.3**: Investigate `irqbalance: thermal: failed to     receive messages`

  - [ ] ⬜ Confirm whether this stops once thermald is properly masked
    (Task 2.1) or whether it's a separate issue.
  - [ ] ⬜ If separate, decide whether to mask `irqbalance` or fix the
    thermal-event subscription.

- [ ] ⬜ **Task 2.4**: NVIDIA RTX 500 Ada Optimus on Meteor Lake

  - [ ] ⬜ Read the recurring `NVRM: GPU0 nvAssertOkFailedNoLog ...     failed to get target temp from SBIOS` and
    `nvidia-modeset: WARNING: GPU:0: Correcting number of heads for     current head configuration` warnings — figure out whether they
    are functional (just noisy) or causing missed GPU power states.
  - [ ] ⬜ Audit
    `playbooks/imports/optional/hardware-specific/play-nvidia.yml`
    for Meteor Lake / RTX 500 Ada coverage: `nvidia-drm.modeset=1`,
    proper `nvidia-prime` setup, `nvidia-powerd.service` enablement.
  - [ ] ⬜ Decide whether to leave the dGPU primary, default to iGPU
    with on-demand offload, or document both paths (Decision 5).
  - [ ] ⬜ Update `play-nvidia.yml` so the recommended path applies
    idempotently.

- [ ] ⬜ **Task 2.5**: Document hardware quirks the repo can't fix

  - [ ] ⬜ `spi-nor spi0.0: unrecognized JEDEC id bytes` — upstream
    kernel coverage gap for an SPI flash chip on this board.
  - [ ] ⬜ Slow TPM (`STM0925`) + serial (`ttyS0/2/3`) device discovery
    adding ~13s to the blame chart — likely firmware/driver, not
    actionable from Ansible.
  - [ ] ⬜ Add a "ThinkPad P14s Gen 5 known quirks" note to either
    `docs/` or the hardware-specific play's header.

### Phase 3: Diagnostic dev-tool fixes (`scripts/collect-diagnostics.bash`)

Surfaced by running the collector on real hardware. None of these break
the snapshot but they all reduce noise and improve agent-driven triage.

- [ ] ⬜ **Task 3.1**: Fix `lsblk -fO` mutually-exclusive flag

  - [ ] ⬜ Replace the single `lsblk -fO` probe with `lsblk -f` (and
    optionally a separate `lsblk -O` probe).
  - [ ] ⬜ Run QA + re-run the play to confirm the manifest goes from
    `rc=1` to `rc=0` on that probe.

- [ ] ⬜ **Task 3.2**: Fix output-dir timestamp using a stale cached
  fact

  - [ ] ⬜ The play stamps the output dir from
    `ansible_date_time.iso8601_basic_short` which is served from the
    on-disk fact cache (`untracked/facts/s1_localhost`), so the
    directory name is "yesterday" while the captures inside are
    current.
  - [ ] ⬜ Switch the timestamp source: either gather facts fresh in the
    play (or clear the cached `ansible_date_time` first), or compute
    the timestamp via `lookup('pipe', 'date +%Y%m%dT%H%M%S')`.

- [ ] ⬜ **Task 3.3**: Replace `powerprofilesctl` probe with a TuneD-aware
  probe

  - [ ] ⬜ On F44 the PPD interface is owned by `tuned-ppd`, not
    `power-profiles-daemon` — `powerprofilesctl` isn't part of the
    TuneD package set, so the probe always reports `rc=127` on a
    correctly-configured host.
  - [ ] ⬜ Replace with `busctl --no-pager status net.hadess.PowerProfiles`
    (or `dbus-send` equivalent) + a `tuned-adm active` probe so the
    snapshot answers "is the Power Mode panel actually wired up?"
    regardless of which daemon backs it.

- [ ] ⬜ **Task 3.4**: Quieten "tool absent" rc=127 noise

  - [ ] ⬜ Wrap optional probes (`lshw`, `inxi`, `sensors`, `vdpauinfo`,
    `xdpyinfo`, `snap`, `tlp-stat`, etc.) with a `command -v` guard
    so the captured file says `tool not installed on this host`
    with `rc=0`, leaving `rc != 0` in the manifest reserved for
    "real" defects.

- [ ] ⬜ **Task 3.5**: Decide on the `ausearch <no matches> = rc=1`
  pattern

  - [ ] ⬜ `selinux-denials.txt`, `audit-anomaly.txt`,
    `04-systemd/masked-unit-files.txt` all return `rc=1` when the
    result is "nothing to report" — which is the desired, clean
    outcome.
  - [ ] ⬜ Either post-process empty output to `rc=0`, or annotate the
    README so the agent doesn't chase them as defects (Decision 6).

### Phase 4: Firmware update workflow

Surfaced by `fwupdmgr get-updates`: Intel ME firmware is multiple
CVE-patched releases behind on this hardware. The repo has no
firmware-update story today.

- [ ] ⬜ **Task 4.1**: Decide ship-the-update-via-Ansible vs
  document-manual

  - [ ] ⬜ Weigh the risk (ME firmware updates *can* brick if
    interrupted; the LVFS update requires AC power and reboot)
    against the value (CVE patches landing without user effort).
  - [ ] ⬜ See Decision 7.

- [ ] ⬜ **Task 4.2**: Implement the chosen path

  - [ ] ⬜ If automated: new `playbooks/imports/optional/common/play-fwupd-updates.yml`
    that runs `fwupdmgr refresh --force` then `fwupdmgr update --no-reboot-check`
    only for `urgency >= high` releases, with an explicit opt-in and
    a banner in the readme.
  - [ ] ⬜ If documented: a short section in `docs/` explaining how to
    run `fwupdmgr update` after a converge, when it's safe, and what
    the reboot-after-install flow looks like.

## Dependencies

- Depends on: commit `feat(dev): add playbooks/dev/ + collect-diagnostics for repo-side audits` (already landed) — this plan is the first user
  of that collector.
- Blocks: nothing yet. Phase 3 dev-tool fixes will improve future audits
  but no other plan is waiting on them.

## Technical Decisions

### Decision 1: NetworkManager-wait-online — mask or extend?

**Context**: `play-network-wait-tuning.yml` already extends the timeout
but the unit still fails on a Wi-Fi-only boot.
**Options considered**:

- *Mask the unit.* Pro: guaranteed clean boot. Con: anything that
  genuinely needs the network at boot is now subtly broken — but
  this is a desktop, not a server.
- *Raise timeout further (e.g. 30s).* Pro: keeps the unit semantically
  meaningful. Con: adds boot latency on poor Wi-Fi, may still fail.
  **Decision**: TBD during Task 1.1 — favouring *mask* on the basis that
  nothing on a desktop should be network-online-blocking at boot.
  **Date**: pending.

### Decision 2: Resolving the firewalld⇄docker NAME_CONFLICT

**Context**: Race between firewalld and Docker's chain installation at
boot, on every boot.
**Options considered**:

- *systemd drop-in for `docker.service`* enforcing
  `After=firewalld.service` strictly.
- *Pre-create the `docker-forwarding` policy via Ansible* so firewalld
  treats it as managed and doesn't claim a name conflict.
- *Run `firewall-cmd --reload` post-docker-start* to resettle the
  policy graph.
  **Decision**: TBD during Task 1.2 — investigation needed.
  **Date**: pending.

### Decision 3: Mask `intel_lpmd` repo-wide on F44?

**Context**: TuneD already handles power-profile switching; intel-lpmd
adds noise without obvious benefit on this kernel.
**Options considered**:

- *Mask `intel_lpmd.service` for the desktop role.*
- *Leave installed for forward-compatibility with a future kernel that
  exposes `sched_itmt_enabled`.*
  **Decision**: TBD — leaning toward mask now, unmask only if a measurable
  benefit ever surfaces.

### Decision 4: `dnf-makecache.service` 33.9s blame cost

**Context**: Largest single blame entry; runs at boot but is a
once-per-day cost.
**Options considered**:

- *Accept as-is* — runs offline, doesn't block graphical.target on the
  critical chain.
- *Defer via drop-in* — make it `Wants=graphical.target` not part of
  basic.target's blame.
  **Decision**: TBD — likely "accept" unless critical-chain analysis says
  otherwise.

### Decision 5: NVIDIA Optimus default on hybrid laptops

**Context**: ThinkPads in this class have iGPU + dGPU; the right
default is contentious.
**Options considered**:

- *iGPU primary, dGPU on-demand offload (`__NV_PRIME_RENDER_OFFLOAD=1`)*
  — best battery life, default for most hybrid users.
- *dGPU always-on* — best perf, worst battery.
- *Hybrid PRIME, both active, render-offload from compositor* — newest
  path, less stable on Wayland.
  **Decision**: TBD during Task 2.4 — likely *iGPU primary, dGPU on-demand*.

### Decision 6: Treat "empty output" probes as `rc=0` or document-as-rc=1?

**Context**: `ausearch`, `systemctl list-unit-files --state=masked` and
similar return non-zero when there's nothing to report — which is the
*clean* outcome.
**Options considered**:

- *Post-process empty output → rc=0.* Pro: manifest only shows real
  defects. Con: hides the original tool exit code.
- *Annotate in README only.* Pro: preserves tool semantics. Con: noisy
  manifest.
  **Decision**: TBD during Task 3.5 — leaning toward post-process with an
  explicit "rewritten from rc=1 because no results" comment in the file.

### Decision 7: ship firmware-update auto-flow or document manual?

**Context**: Intel ME firmware on this host is multiple CVE-patched
releases behind. Repo currently has no firmware story.
**Options considered**:

- *Automated Ansible task* gated by `urgency >= high`, with explicit
  `--no-reboot-check` and an opt-in flag.
- *Document the manual flow* in `docs/` and trust the user.
  **Decision**: TBD during Task 4.1. The bias should be toward *document
  manual* given the bricking risk of ME updates — these are the kind of
  change a human should look at.

### Decision (recorded, no action): TuneD vs power-profiles-daemon

**Context**: The collector flagged `powerprofilesctl` absent and the
initial triage misread that as a missing playbook coverage. Fedora 44
ships `tuned` + `tuned-ppd` as the default Power Mode backend;
`power-profiles-daemon` is intentionally not installed.
**Decision**: Keep the F44 default. Do *not* install
`power-profiles-daemon`. The diagnostic-tool fix is to probe the
`net.hadess.PowerProfiles` D-Bus name instead of the
`powerprofilesctl` CLI (Task 3.3).
**Date**: 2026-06-13.

## Success Criteria

- [ ] `systemctl --failed` reports zero failed units on a fresh boot.
- [ ] `journalctl -b -p err` after a fresh boot contains no recurring
  errors (firewalld NAME_CONFLICT, gkr-pam, bluetoothd hci0,
  intel-lpmd) for issues addressed by this plan.
- [ ] A second run of `playbooks/dev/play-collect-diagnostics.yml`
  shows the new manifest with `rc != 0` only for genuine defects, no
  "tool absent" or "no matches" noise.
- [ ] The TuneD-vs-PPD decision is recorded in the repo (this plan
  counts as the record) and the diagnostic probe matches reality.
- [ ] Firmware-update decision (Decision 7) is captured in `docs/` and,
  if automated, in a play.
- [ ] `./scripts/qa-all.bash` passes after every change.

## Risks & Mitigations

| Risk                                                                                            | Impact | Probability | Mitigation                                                                                                                       |
| ----------------------------------------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Masking `NetworkManager-wait-online` breaks some service that genuinely needs network-at-boot   | M      | L           | Audit `systemctl list-dependencies network-online.target` after the mask; document any opt-in unmask path for users who need it. |
| NVIDIA driver / Optimus change destabilises the GNOME session                                   | H      | M           | Stage changes in `play-nvidia.yml` behind a separate variable; let users opt into the new default; keep the rollback documented. |
| ME firmware auto-update bricks the laptop                                                       | H      | L           | Bias toward *document manual* (Decision 7); never run firmware updates as part of `playbook-main.yml`.                           |
| Diagnostic-tool "rc rewrite" hides a real defect                                                | M      | L           | When rewriting empty-output rc=1 to rc=0, also annotate the file with the original exit code and the reason — surface, not hide. |
| PII leak — a Plan task copies hostname/serial/SSID from the snapshot into the plan accidentally | H      | L           | Plan tasks reference the snapshot *path* only. Pre-commit secret scanner is the safety net; never `--no-verify`.                 |

## Notes & Updates

### 2026-06-13

- Plan created from `untracked/diagnostics/<timestamp>/ANALYSIS.md`
  findings.
- Power-Mode finding corrected post-triage (F44 ships TuneD + tuned-ppd,
  not power-profiles-daemon). Decision recorded above.
- Phases split into: generic (1), hardware-specific (2), dev-tool (3),
  firmware (4).
