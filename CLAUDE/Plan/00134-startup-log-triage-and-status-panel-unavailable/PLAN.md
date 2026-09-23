# Plan 00134: startup log triage and status panel unavailable

**Status**: In Progress
**Created**: 2026-09-22
**Owner**: joseph
**Priority**: High

## Overview

After a routine kernel/firmware update and reboot, the desktop delivered a burst of
"crash" notifications, a long uncopyable error notification, and the fedora-desktop
status panel showed the blue question-mark ("unavailable") icon over a wall of text.
The boot logs were reviewed end to end. **Nothing crashed during the boot** — no
coredump, no failed unit, no extension error. Every symptom is a report about earlier
state, re-delivered at login. The full evidence, anonymised, is in
[RESEARCH-startup-log-findings.md](RESEARCH-startup-log-findings.md) (F1–F8); the
probes that produced it are in [`triage.bash`](triage.bash).

The panel symptom has a deterministic root cause in this repo: the `play_ledger`
callback marks the ledger `BROKEN` whenever a plain ad-hoc `ansible … -m …` command is
run from the checkout, because an ad-hoc play has no source file (F2). The remaining
findings are real but silent defects the logs happened to expose: WirePlumber
configuration that has not applied since WirePlumber 0.5 (F4), a duplicated dnf repo
id (F5), a systemd cap that does nothing (F6), an unmanaged ABRT backlog that is the
source of the "crash" notifications (F3), and a large SELinux denial flood from
containers that needs its own diagnosis (F7).

## Goals

- An ad-hoc `ansible` command from the checkout never marks the play ledger broken.
- A panel/notification reason is readable and copyable, and the remedy is not truncated.
- The HD-audio and Bluetooth settings the repo claims to apply are actually loaded by
  WirePlumber 0.5.
- dnf5daemon builds every configured repo without an id conflict.
- Every `Type=oneshot` unit in `files/` uses a cap systemd honours.
- The host's ABRT policy (retention, applet behaviour) is an IaC decision, not a default.
- The container SELinux denial flood is explained and stopped.

## Non-Goals

- Fixing third-party extension warnings (dash-to-dock, blur-my-shell, tilingshell).
- Anything in the "reviewed and judged not actionable" table of the research doc.
- Investigating the user-project CLI crashes ABRT recorded; they are not this repo's.

## Tasks

### Phase 1: the reported symptom (panel + long notification)

- [x] ✅ **Task 1.1**: `callback_plugins/play_ledger.py` / `helpers/play_ledger` — a play
  with no source position that is an ad-hoc play is **skipped**, not recorded as a hole.
  Recognised by the playbook file name `__adhoc_playbook__` that `ansible.cli.adhoc`
  sets, not by the play name `Ansible Ad-Hoc`, which any playbook can use. A playbook
  play with no position still marks the ledger broken. Regression test in
  `tests/helpers/` using `plugin_support` (Ansible is not importable there).
- [x] ✅ **Task 1.2**: `helpers/host_health/login_report.py` — the untrustworthy-ledger
  finding keeps the remedy: carry the multi-line diagnostic as detail lines instead of
  `"; ".join(...)`, so the clear command is neither flattened nor cut off.
- [x] ✅ **Task 1.3**: `extensions/fedora-desktop@fedora-desktop` — add a "Copy" menu
  action for the findings text (the `St.Clipboard` pattern `container-watch` uses),
  and keep the `notify-send` body to a headline plus the findings file path. Every
  findings label also wraps (the owner reported the unwrapped line breaking the panel).
- [x] ✅ **Task 1.4**: Document in `docs/` that the BROKEN sentinel is cleared with the
  command the check prints, and that this is the operator's route (already coded in
  `check_freshness --clear-broken`). In `docs/playbooks.md`, under the host-health play.
- [x] ✅ **Task 1.5**: `helpers/play_ledger` — a removed play is reported `GONE` for ever.
  A tracked retired-plays map names each removed play's successor; the `GONE` finding
  names that successor and stops once the successor has run after the removal. An entry
  whose key still exists, or whose successor does not, is an error. Seeded with
  `play-claude-code.yml → play-claude-yolo.yml` (merged for the Plan 00135 `cc` break).
  Only a successful successor run retires it.
- [x] ✅ **Task 1.6**: the ad-hoc skip against real Ansible — extend
  `tests/helpers/play_ledger/test_source_position_against_real_ansible.py` so it drives
  the callback's playbook-start → play-start path with the CLI's `__adhoc_playbook__`
  marker, so an Ansible change to that marker fails a test. Then decide what
  `ansible-console` should do: it runs plays without a playbook-start event, so the
  callback still marks the ledger broken for it (ansible-core 2.19 `cli/console.py`).

### Phase 2: defects the logs exposed

- [ ] 🔄 **Task 2.1**: `play-hd-audio.yml` — port the two `.lua` files to
  `~/.config/wireplumber/wireplumber.conf.d/*.conf` (SPA-JSON `monitor.alsa.rules` /
  `monitor.bluez.properties` + `monitor.bluez.rules`), remove the `*.lua.d` files and
  directories, restart WirePlumber. Verify with `wpctl status` / `wpctl inspect` that
  the properties are present on the nodes. Code done (node properties now match nodes,
  not devices; other Lua left behind stops the play). HOST verify pending.
- [ ] 🔄 **Task 2.2**: `play-browsers.yml` — resolve the duplicate `[vivaldi]` repo id:
  keep exactly one of the two repo files (the RPM's own post-install writes
  `vivaldi.repo`; the play writes `vivaldi-fedora.repo`) and make the play remove the
  other on every run. Verify `dnf5 repolist` shows one `vivaldi` and dnf5daemon logs
  no `Id is present more than once`. Code done: `vivaldi-fedora.repo` is kept, and
  `/etc/default/vivaldi` `repo_add_once="false"` stops the scriptlet recreating the
  other (the RPM's scriptlets are quoted in the journal, 18:35). HOST verify pending.
- [ ] 🔄 **Task 2.3**: `files/home/.config/systemd/user/vmtest-bridge@.service` — replace
  `RuntimeMaxSec=120` with `TimeoutStartSec=120` and fix the comment. Audit every
  other `Type=oneshot` unit in `files/` for the same mistake. Code done: the other 12
  oneshot units carry no `RuntimeMaxSec` (neither do inline units in plays), and
  `systemd-analyze verify` shows the "no effect" warning for the old unit but not the new
  one. HOST verify pending (deploy leg 7).
- [ ] ⬜ **Task 2.4**: `play-toolbox-install.yml` — ensure
  `~/.config/autostart/jetbrains-toolbox.desktop` is mode 0644 whenever it exists (the
  application rewrites it, so this must run every pass, not be `creates:`-guarded).
- [ ] 🔄 **Task 2.5**: ABRT policy as IaC — `play-basic-configs.yml` sets
  `abrt_auto_reporting` (project default on; per-host override) via
  `abrt-auto-reporting`, and installs `abrt-prune-stale.{service,timer}` running
  `files/usr/local/bin/abrt-prune-stale.bash` daily with `abrt_retention_days`
  (default 30). Owner chose: reporting off on their host, 7-day retention — both set
  in `host_vars`, not in the repo — the play persists whatever values are in effect
  (`-e` or defaults) into a managed block there. Deployed (79 → 10 records).
  Desktop profile only, and the block installs `abrt` + `abrt-tui` itself (PR #50; journal
  16:40). Remaining: confirm no applet backlog notification at the next login.
- [ ] ⬜ **Task 2.6**: Thunar — confirm no play installs it, then either own it in a play
  or remove it; the duplicate `org.freedesktop.FileManager1` service file goes with it.

### Phase 3: things to diagnose before changing

- [ ] ⬜ **Task 3.1**: SELinux denial flood (F7) — extend `triage.bash` with probes that
  list, per running container, the workspace mount options (`:z` present or not),
  and the label the host sees on each frequently-denied path; then decide whether
  the CCY relabel is skipped in some launch mode or host-created paths revert.
- [ ] ⬜ **Task 3.2**: Docker 29 nftables backend vs `lxc-docker-user-iptables-reconcile`
  — verify whether the `DOCKER-USER` iptables chain the reconcile script edits is
  consulted at all with the nftables backend; if not, that script's egress rules are
  dead and Plan 00127's assumptions need revisiting. Also decide whether the per-boot
  firewalld `COMMAND_FAILED`/`NAME_CONFLICT` noise is worth silencing.

### Phase 4: close

- [ ] ⬜ **Task 4.1**: Run `./scripts/qa-all.bash` and ESLint for the extension change.
- [ ] ⬜ **Task 4.2**: Run the `qa-reviewer` agent over the plan's full diff; resolve every
  BLOCK / FIX-BEFORE-MERGE finding.

## Success Criteria

- [ ] `XDG_STATE_HOME=$(mktemp -d) ansible localhost -m ping` from the checkout leaves no
  `BROKEN` sentinel (the exact reproduction in the research doc).
- [ ] After clearing the sentinel and one graphical login, the panel icon is not
  `unavailable`, and any finding shown can be copied from the panel menu.
- [ ] `journalctl --user -b` has no `Lua configuration files are NOT supported` line and
  `wpctl` shows the configured ALSA/Bluetooth properties.
- [ ] `journalctl -b` has no `Id is present more than once` and no
  `RuntimeMaxSec= has no effect` lines.
- [ ] `ausearch -m avc -ts boot` count for `container_t` is explained and bounded.

## Delivery & Milestones

- Triage complete; findings and reproduction recorded (this plan's first commit).
- Phase 1 code merged; [`deploy.bash`](deploy.bash) puts it, the `cc` fix, Plan 00135
  Task 3.7 and Plan 00109 Task 4.3 on the HOST in one run.
