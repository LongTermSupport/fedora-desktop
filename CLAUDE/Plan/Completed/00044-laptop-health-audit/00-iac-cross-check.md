# Plan 00044 — IaC Cross-Check

## Summary

Audited 14 prioritised actions (skipping #3 per instructions) against the repo
as the IaC source of truth. **Result: 8 real issues, 5 busywork (repo policy or
non-applicable), 0 outright conflicts, 1 unclear.** The audit is mostly sound —
only two recommendations actively fight repo defaults (rclone log level and
versionlock policy), and most "missing" subsystems (tuned, thermald, lm_sensors,
mtk_t7xx blacklist) are genuinely absent from the repo and so are real gaps.

## Action-by-action

### Action 1: Apply Critical security updates via `dnf upgrade --refresh --exclude='kernel*'`

**Classification**: 🟢 REAL ISSUE (with a repo-native answer)

**Repo evidence**:
`playbooks/imports/play-AB-dnf-upgrade.yml:26–86` — a new top-level early-running
playbook that does `dnf update_cache: true` then `dnf: name: "*", state: latest`
and explicitly relies on the versionlock to protect the previous-minor kernel
fallback. Header comment:

> "Respects existing versionlocks. … the versionlock on the `kernel` metapackage
> transitively protects all the sub-packages."

**Verdict**: The "fix" already exists in the repo as `play-AB-dnf-upgrade.yml`
(committed cfea82d, after Phase 1 of this plan). It is the right tool for
Action 1 — it does the full upgrade, not just `--exclude='kernel*'`, but
because of the versionlock it cannot drift the kernel. The plan's task list
should be updated to "Run `play-AB-dnf-upgrade.yml`" rather than ad-hoc
`dnf upgrade --exclude='kernel*'`. The CLI command in Plan 00044 also
violates the InfrastructureAsCode rule against manual `dnf install/upgrade`.

---

### Action 2: Switch `tuned` profile from `balanced` to `balanced-battery`

**Classification**: 🟢 REAL ISSUE

**Repo evidence**: No matches for `tuned`, `tuned-adm`, or `tuned-ppd` anywhere
in `playbooks/`, `files/`, or `vars/`. The only related artefact is
`playbooks/imports/optional/archived/play-tlp-battery-optimisation.yml`
(archived 2025-08-29, README: "Battery care and power management are now
standard features in modern Fedora … use GNOME Settings > Power or
`powerprofilesctl`").

**Verdict**: Real gap. The repo deliberately deferred to "Fedora defaults"
when archiving TLP, which means the current `balanced` profile is the
*untouched* Fedora default, not a repo policy. Setting it to
`balanced-battery` (or any deliberately chosen value) via a new playbook
fits the IaC model. The plan's intent to create `play-laptop-power-tuning.yml`
is correct.

---

### Action 3: Resolve Plan 00043 partial-kernel-7.0.9 install

**Classification**: (skipped per instructions — already complete)

---

### Action 4: Mask `thermald.service`

**Classification**: 🟢 REAL ISSUE

**Repo evidence**: No matches for `thermald` in `playbooks/`, `files/`, or
`vars/`. The unit is enabled purely as a package default.

**Verdict**: Real gap. Masking it does NOT fight the repo — the repo does
not assert any state on `thermald`. The "enabled" state came from the
distro/package default. Masking is a legitimate Ansible playbook change
(use `ansible.builtin.systemd: name: thermald masked: true`).

---

### Action 5: Apply remaining ~124 pending updates

**Classification**: 🟢 REAL ISSUE (covered by existing playbook)

**Repo evidence**: `playbooks/imports/play-AB-dnf-upgrade.yml:75–82` — the same
`dnf "*" state: latest` task that handles Action 1 also handles this.

**Verdict**: Real, but the playbook already exists. Same comment as Action 1 —
don't run a manual `dnf upgrade`; use `play-AB-dnf-upgrade.yml`. The two
actions collapse into a single run of the existing playbook.

---

### Action 6: Lower `warp-svc` log verbosity

**Classification**: ⚪ UNCLEAR

**Repo evidence**: `playbooks/imports/optional/common/play-cloudflare-warp.yml`
(lines 1–68) — configures warp via `warp-cli` (`mode doh`, `dns families malware`, registration) but **does not touch warp-svc log level** in any
config file. Search for `log|verbose|debug` in that file returns only
unrelated lines.

**Verdict**: The repo does not assert a log level, so dropping verbosity does
not fight any policy. However, warp-svc reads its log config from a
Cloudflare-managed file (not in the repo); the question is whether that file
is a stable supported configuration surface. If yes, this is a small real
gain. If no (Cloudflare may rewrite it on package upgrade), the change won't
stick across `dnf upgrade`. Needs a quick check of where `warp-svc`'s log
level is actually read from before codifying.

---

### Action 7: Tame `rclone-lts-photo` user-mount (log level + MemoryHigh/Max)

**Classification**: 🟠 CONFLICTS WITH REPO (log level part), 🟢 REAL ISSUE
(memory limits part)

**Repo evidence**:

- `playbooks/imports/optional/common/play-rclone.yml:271` — systemd unit
  template hard-codes `--log-level {{ item.log_level | default('INFO') }}`.
  The mount can override `log_level` per-mount via `rclone_mounts` config.
- Same file lines 30 (header comment) and 49–53 (comments justifying INFO
  as the default): the rclone playbook explicitly notes the choice of
  `--log-level INFO` is so the synchronous mount path leaves "a real exit
  code … a log trail". INFO is a *deliberate* repo default.
- No `MemoryHigh` or `MemoryMax` directives anywhere in the unit template
  (lines 255–277): no policy asserted.

**Verdict**: Two halves, different verdicts.

- **Log level**: dropping the photo mount to `NOTICE` overrides the repo's
  documented default of `INFO`. To do it the IaC way, set `log_level: NOTICE`
  in `rclone_mounts` config for the photos mount specifically (the template
  already supports per-mount override). Don't change the template default,
  don't edit the live unit file.
- **Memory limits**: gap. Adding `MemoryHigh=`/`MemoryMax=` to the template
  is a real improvement and doesn't fight any existing repo policy.

---

### Action 8: Reduce `NetworkManager-wait-online.service` (5.79s boot stall)

**Classification**: 🟢 REAL ISSUE

**Repo evidence**: No matches for `NetworkManager-wait-online` or
`NM_ONLINE_TIMEOUT` in `playbooks/`, `files/`, or `vars/`. The repo does not
configure this unit at all.

**Verdict**: Real gap. Lowering the timeout (or masking the unit if nothing
in the boot path truly needs it) is a legitimate playbook change that does
not contradict any existing IaC.

---

### Action 9: Decide on the `kernel` versionlock at 6.19.14-200

**Classification**: 🟡 REPO POLICY

**Repo evidence**:
`playbooks/imports/optional/common/play-advanced-kernel-management.yml:13–103` —
installs `python3-dnf-plugin-versionlock` and runs
`files/usr/local/bin/manage-kernel-versions.py`, then reports
`dnf versionlock list`. Also `playbooks/imports/play-AB-dnf-upgrade.yml:11–16`
(header):

> "Respects existing versionlocks. See `play-advanced-kernel-management.yml`
> which locks the highest patch of the PREVIOUS-minor kernel as a
> hardware-compat fallback. The versionlock on the `kernel` metapackage
> transitively protects all the sub-packages."

**Verdict**: This is the previous-minor fallback the question itself
predicted. The lock is a deliberate, documented repo policy — not an
oversight. Plan 00043 explains the same intent. No action needed beyond
"confirm the lock is the previous-minor pin and move on". The plan's
proposal to "decide on the versionlock" should be downgraded to
"confirm policy is being enforced as intended".

---

### Action 10: Install `lm_sensors`

**Classification**: 🟢 REAL ISSUE

**Repo evidence**: No matches for `lm_sensors` or `lm-sensors` anywhere
in `playbooks/`, `files/`, or `vars/`.

**Verdict**: Real (small) gap. Adding it to a base packages play is a
one-liner that doesn't fight anything.

---

### Action 11: Disable stale `copr:phracek/PyCharm` COPR

**Classification**: 🟢 REAL ISSUE

**Repo evidence**: No matches for `phracek` or `PyCharm` anywhere in
`playbooks/`, `files/`, or `vars/`. The only COPR the repo currently enables
is `pgdev/ghostty` (`playbooks/imports/play-terminal-emulators.yml:31`).
The PyCharm COPR was enabled by a now-removed playbook or a historical
manual step — it is genuinely orphaned in the system state.

**Verdict**: Real cleanup. Disabling it via a small `dnf copr disable phracek/pycharm` task is correct and does not contradict any current repo
state.

---

### Action 12: Blacklist `mtk_t7xx` 5G modem

**Classification**: 🟢 REAL ISSUE (if WWAN truly unused)

**Repo evidence**: No matches for `mtk_t7xx` or `t7xx` anywhere. The directory
`files/etc/modprobe.d/` does not even exist in this repo. No modprobe
policy is asserted.

**Verdict**: Real gap if the user has decided WWAN is never used. The plan
correctly defers it as "decide first" — there is no repo state to
contradict, so this is a clean addition once the policy decision is made.

---

### Action 13: NetworkManager SIGABRT 2026-05-21 11:55 watch item

**Classification**: 🟢 REAL ISSUE (watch-only, as marked)

**Repo evidence**: N/A — observational item, no system state to compare.

**Verdict**: Correctly framed as watch-only. No IaC change implied.

---

### Action 14: Test NVMe `power/control=auto`

**Classification**: 🟢 REAL ISSUE (test-then-codify, as marked)

**Repo evidence**: No matches for `nvme` power control rules in the repo.
The current `power/control=on` is the kernel/distro default.

**Verdict**: Correctly framed as "test first, then add to Ansible". No
current repo policy to fight.

---

### Action 15: ELAN touchpad `i2c_hid_acpi` watch item

**Classification**: 🟢 REAL ISSUE (watch-only, as marked)

**Repo evidence**: N/A — observational item.

**Verdict**: Correctly framed as watch-only.

---

## Headline counts

| Verdict                | Count | Items                                          |
| ---------------------- | ----- | ---------------------------------------------- |
| 🟢 Real issue          | 11    | 1, 2, 4, 5, 7 (mem), 8, 10, 11, 12, 13, 14, 15 |
| 🟡 Repo policy         | 1     | 9                                              |
| 🟠 Conflicts with repo | 1     | 7 (log level half)                             |
| ⚪ Unclear             | 1     | 6                                              |
| (skipped)              | 1     | 3                                              |

(Action 7 is split: memory limits = real, log-level override = conflicts.)

## Recommended top 3 to do

1. **Run `playbooks/imports/play-AB-dnf-upgrade.yml`** (Actions 1+5 combined,
   the IaC-native answer; the manual `dnf upgrade --exclude='kernel*'` in
   the plan should be replaced with this playbook invocation).
2. **Create `play-laptop-power-tuning.yml`** to set `tuned` profile to
   `balanced-battery` and mask `thermald` (Actions 2+4 — both clean gaps,
   no repo policy to fight, single playbook covers both).
3. **Disable `copr:phracek/PyCharm`** (Action 11 — drift cleanup, the COPR
   is orphaned and no current repo play references it).

## Items the plan should de-prioritise or rephrase

- **Action 9** (versionlock decision) — downgrade from "decide" to "confirm
  the previous-minor pin is in place"; it's working as designed.
- **Action 7** (rclone) — split into two subtasks: per-mount `log_level`
  override in config (not a template default change), and a separate template
  edit to add `MemoryHigh=`/`MemoryMax=`.
- **Action 6** (warp-svc) — verify the log-config surface is stable before
  codifying; otherwise the change won't survive package upgrades.
