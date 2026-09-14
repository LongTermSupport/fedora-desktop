# Plan 00112: GNOME extensions — the enabled list as declared state

**Status**: In Progress
**Created**: 2026-09-13
**Owner**: Repo owner + Claude
**Priority**: High

## Overview

The desktop acceptance scenario from Plan 00110 (`vmtest run desktop-fresh-install`, run `20260913T204106Z-desktop-fresh-install`) found
that a fresh install ends with every extension the repo deploys installed,
compiled and loaded by GNOME Shell — `State: INITIALIZED` for all eight — and
**none of them enabled**. The session's `org.gnome.shell enabled-extensions`
holds only Fedora's stock background-logo. A real user has to open the
Extensions app and flip eight switches by hand, which is the manual step the
repo exists to remove. Existing hosts never showed it because their lists were
flipped long ago and gsettings persists.

The cause is in `play-gnome-shell-extensions.yml`. The seven extensions from
extensions.gnome.org are fetched by `gnome-shell-extension-installer`, whose
own `gnome-extensions enable` runs the instant the zip is extracted — before
the running shell has scanned the new directory — and whose failure is an `&&`
the installer does not propagate; the play inspects only the download marker.
The custom extension's enable task is the same call a moment after the copy,
carrying `failed_when: false`, and its verify helper deliberately exits 0 for
"pending Wayland reload". So a first run enables nothing and reports every
task ok. `gnome-extensions enable` asks the *shell* to enable; the shell can
only enable what it has already loaded.

The fix is to declare the state rather than request an action: each deployed
UUID must be present in `enabled-extensions`, written through gsettings (which
the shell reads at session start and watches live), so it holds whether or not
the running shell has loaded the extension yet. The desktop scenario is this
plan's acceptance test: it certifies `desktop-44` forward only when the check
`deployed-extensions-active` is green in the post-reboot session.

## Goals

- A fresh install (the desktop scenario) ends with every UUID the repo deploys
  `ACTIVE` in the session a user gets, with no manual step.
- The enable step is idempotent declared state, not a racy request: present
  UUIDs are kept, missing ones added, nothing removed that the user added.
- No `failed_when: false` on the enable path; a UUID the shell rejects
  (`OUT_OF_DATE`, `ERROR`) still fails the play through the existing verify
  helper, which now runs for every deployed UUID, not one.

## Non-Goals

- Changing which extensions the repo installs, or their versions.
- Replacing `gnome-shell-extension-installer`.
- Making the shell reload on Wayland; a fresh install still reboots as
  `run.bash` already says.

## Tasks

### Phase 1: The declared enabled list

- [x] ✅ **Task 1.1**: `helpers/gnome/enabled_extensions.py` — pure parse/merge/format
  of the GVariant list plus `metadata.json` UUID discovery (36 tests). The
  side-effecting half is `apply_enabled_extensions.py`: it resolves a session bus
  (live socket, else `dbus-run-session`), merges, writes, and **re-reads what it
  wrote** so a refused write fails rather than passing (15 tests)
- [x] ✅ **Task 1.2**: The play calls the applier once, after the schemas are
  compiled and the custom extension is copied; it discovers the UUIDs from
  `metadata.json` under the user's extensions directory, and `--require` pins the
  custom one so a failed copy cannot read as "seven deployed, all enabled".
  `changed_when` keys on `GNOME-EXT-ENABLED-CHANGED`
- [x] ✅ **Task 1.3**: The `failed_when: false` enable task is gone;
  `helpers.gnome.verify_extension` now loops over every deployed UUID, taken from
  the applier's `GNOME-EXT-DEPLOYED` marker rather than re-derived from the play's
  own literals. Its pending-Wayland-reload rule is unchanged
- [x] ✅ **Task 1.4**: Confirmed — `run.bash`'s closing "System Reboot" step
  recommends a reboot on every path (interactive prompt, and the headless message
  whether or not `RUN_BASH_REBOOT=1`), so it already covers the case where this
  play changed the list. No change needed

### Phase 2: Acceptance

- [ ] ⬜ **Task 2.1**: HOST: deploy the play on the host (`deploy.bash`), confirm
  idempotent, and confirm the host's own list is unchanged (nothing removed).
- [ ] ⬜ **Task 2.2**: HOST: `vmtest run desktop-fresh-install` against the pushed
  commit; `deployed-extensions-active` green in the post-reboot session; run id
  recorded here; `desktop-44` certified forward by the passing run.
- [ ] ⬜ **Task 2.3**: QA, then `qa-reviewer` over the diff.

## Success Criteria

- [ ] `vmtest run desktop-fresh-install` verdict `pass`, 16/16, in the session
  after the reboot.
- [ ] Re-running the play on a host with extra user-enabled extensions removes
  none of them and reports no change.
- [ ] No `FAIL-FAST-OK` annotation remains on the enable path.
- [ ] `./scripts/qa-all.bash` passes.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00112-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
