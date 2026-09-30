# Health document refresh (Task 4.6)

## Root cause

`host-status.json` was written only by `host-health.service` (login). Desktops had no
collector timer; only servers did. A fixed finding therefore never cleared the icon.

## Changes

- `helpers/play_ledger/health_refresh.py` (new) and `tests/helpers/play_ledger/test_health_refresh.py`
  (new, 10 tests): restarts `host-health-collect.service --no-block`; returns a
  `HEALTH-REFRESH-FAILED` line instead of raising; skips silently when the unit is not installed.
  A test pins the unit name to the playbook's `collect_service`.
- `callback_plugins/play_ledger.py`: calls it in `v2_playbook_on_stats` after a successful ledger write.
- `playbooks/imports/optional/common/play-host-health-login-report.yml`: collector service and
  timer deployed, reloaded, enabled and started on both profiles; desktop "Remove The Server
  Collector" task deleted; comments updated.
- `files/home/.config/systemd/user/host-health-collect.timer.j2`: desktop branch hourly
  (`OnStartupSec=15min`, `OnUnitActiveSec=1h`, 5 min jitter); server branch unchanged.
- `files/home/.config/systemd/user/host-health-collect.service.j2`: comments only.
- Plan 00109: `PLAN.md` (Task 4.6), `DESIGN-panel.md` section 4 (superseded, decisions recorded),
  `JOURNAL/00109-Journal-26-09-30.md` (new).

## Decisions

- Callback failure cannot fail a play (Ansible swallows callback exceptions; producer is
  reporting-only). Made loud on stderr, not silent, and no BROKEN sentinel.
- `restart` not `start`; path unit rejected (fires mid-burst of ledger appends).
- No section is login-only (all judge current state); DKMS-still-building risk handled by the
  15 min start delay. Handoff file is a fixed overwritten path.

## QA

`qa-helper-tests.bash`: 2260 tests OK. `qa-all.bash`: only the known failures
(deployed-drift for vmtest, ccy-relabel-preflight).

## Deploy and verify (coordinator / host)

Run `./playbooks/imports/optional/common/play-host-health-login-report.yml`. The
callback change takes effect on the next playbook run (it is read from the repo).
Verify:

- `systemctl --user list-timers host-health-collect.timer --no-pager | cat` shows a next run.
- `stat -c %y ~/.local/state/fedora-desktop/host-status.json`, re-run any play, and the mtime
  (and `generated_at`) moves within seconds; the icon clears within the panel's 300s poll.
- `systemctl --user status host-health-collect.service --no-pager | cat` shows the run; exit 3
  (findings) counts as success.
