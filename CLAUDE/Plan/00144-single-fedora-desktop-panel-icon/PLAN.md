# Plan 00144: single fedora desktop panel icon

**Status**: Not Started
**Created**: 2026-09-30
**Owner**: joseph
**Priority**: Medium

## Overview

The top bar is crowded, and two of its icons are this repository's own:
**Container Watch** (`extensions/container-watch@fedora-desktop`, the container watchdog's
surface) and **Fedora Desktop** (`extensions/fedora-desktop@fedora-desktop`, the host
drift/health panel). Both use the same amber warning glyph for their alert, so two icons
do not even tell the user which subsystem is shouting. The owner asked for the two to be
consolidated into one Fedora Desktop icon to save space on the taskbar.

This plan makes container-watch a **section** of the single Fedora Desktop panel, gives
the one icon a defined way to surface a container alert alongside the drift state, and
retires `container-watch@fedora-desktop` declaratively: removed from the
`enabled-extensions` key and its deployed files deleted, by playbook. The container-watch
**backend** (helper, CLI, `systemd --user` timer, DBus signal) is untouched.

Facts, with file and line citations, are in
[RESEARCH-current-state.md](RESEARCH-current-state.md). Options and recommendations are in
[DECISIONS.md](DECISIONS.md); three of them are the owner's to settle before Phase 3.

## Goals

- Exactly one fedora-desktop icon in the top bar on a host with both features deployed.
- Everything the container-watch menu offers today is reachable from the Fedora Desktop
  menu: both finding shapes (process, crash loop), advisories, copy `exec_hint`, copy
  restart-policy advice, and the deduped new-finding notification.
- A container finding moves the single icon according to the rule chosen in D1.
- `container-watch@fedora-desktop` is removed from `org.gnome.shell enabled-extensions`
  and from `~/.local/share/gnome-shell/extensions/` by a playbook, never by hand.
- Every gate that covered the old extension covers the new section, and none goes
  silently blind (the no-kill gate especially).

## Non-Goals

- No change to the container-watch backend, its report schema, its timer or its
  containment behaviour.
- No new rendering of report fields the old extension ignored (`generated_at` staleness,
  the `containment` list of stopped containers). Candidates for a follow-up plan.
- Not folding `speech-to-text@fedora-desktop` or `dock-recovery-on-unlock@fedora-desktop`
  into the panel. The latter has no panel icon; the former is a separate feature and a
  separate request.
- Not changing the status document, its producer, or `check_panel_contract.py`'s scope.

## Context & Background

- The panel's registry and three-state icon come from Plan 00109 (`DESIGN-panel.md` §3,
  §5, §7). Its founding rule is that absence is ignorance and never renders as healthy.
- Container-watch comes from Plan 00055. Its extension deliberately treats "no report" as
  "nothing flagged", because its subject is live (`statusDocument.js:16-21` records why).
- The declared enabled-extensions route is Plan 00112's `apply_enabled_extensions`, which
  is additive only and has no way to remove a uuid today.
- Wayland: loading new extension JavaScript requires **logging out and back in**; no other
  method works (`extensions/CLAUDE.md`). Removing a uuid from `enabled-extensions` is
  expected to disable the loaded old extension live (GNOME 48.7 source; to re-confirm on
  GNOME 50 in Task 1.2).

## Tasks

### Phase 1: Decisions and confirmation

- [ ] ⬜ **Task 1.1**: Owner settles D1 (icon state combination), D2 (absent/unreadable
  report) and D3 (which play retires the old extension). Record each in DECISIONS.md and
  a `decision` journal entry.
- [ ] ⬜ **Task 1.2**: Re-extract the GNOME Shell JS for the installed version
  (`./extensions/scripts/gnome-shell-extract-js.bash`) and confirm in `extensionSystem.js`
  that removing a uuid from `enabled-extensions` disables it live. Record the finding.
- [ ] ⬜ **Task 1.3**: Write `triage.bash` (on `_planlib.inc.bash`) capturing the before
  state: both uuids in `enabled-extensions`, both directories deployed, report.json
  presence, and the container-watch timer state.

### Phase 2: Declarative retirement support (Python, test-first)

- [ ] ⬜ **Task 2.1**: `tests/helpers/gnome/test_enabled_extensions.py` then
  `helpers/gnome/enabled_extensions.py` — a pure `retire(current, retired)` returning the
  list without the named uuids and what was removed. Named uuids only; nothing else is
  ever removed; a uuid both declared and retired in one call is an error.
- [ ] ⬜ **Task 2.2**: `tests/helpers/gnome/test_apply_enabled_extensions.py` then
  `apply_enabled_extensions.py` — `--retire-uuid` (repeatable), `--uuid` no longer
  required when only retiring, read-back proves the retired uuid is gone, marker
  `GNOME-EXT-RETIRED removed=<uuid>`; a failed removal is `GNOME-EXT-FAIL`. Update the
  module docstrings, which currently say it only adds.
  - [ ] ⬜ Run `./scripts/qa-helper-tests.bash` and `./scripts/qa-all.bash`

### Phase 3: Container-watch as a panel section (JavaScript)

- [ ] ⬜ **Task 3.1**: Registry extension per D5 — optional per-section `source`
  (`start(onChange)`/`stop()`) and `state()`; `extension.js` starts/stops sources in
  `enable()`/`disable()` and folds section states into `overallState`. `health` and
  `plays` unchanged in behaviour.
- [ ] ⬜ **Task 3.2**: `extensions/fedora-desktop@fedora-desktop/containerReport.js` —
  the report path, async read with cancellation, parse, and the D2 cases as data (no
  widgets). The DBus constants move here from the old extension unchanged.
- [ ] ⬜ **Task 3.3**: `sections/containers.js` — port the menu: both finding shapes,
  advisories below their own separator, copy `exec_hint`/advice with notification, the
  deduped new-finding notification (dedupe state owned by the section and cleared on
  `stop()`), D2 wording. Use `labels.wrap` and stylesheet classes instead of inline
  styles. Register it in `SECTIONS` in the position D1 chooses. The source is the DBus
  `FindingsChanged` subscription plus the fallback poll interval the old extension used.
- [ ] ⬜ **Task 3.4**: Implement the D1 icon rule in `extension.js` (and a new constant
  only if D1 = B). Update the `extension.js` header, `statusDocument.js:16-21`/`:77-79`
  and `sections/health.js:89-91` comments that describe container-watch as a separate
  extension.
- [ ] ⬜ **Task 3.5**: Delete `extensions/container-watch@fedora-desktop/`.
- [ ] ⬜ **Task 3.6**: ESLint: `cd extensions && node_modules/.bin/eslint .` — clean, no
  blocking calls, no suppressions.

### Phase 4: Tests and QA gates

- [ ] ⬜ **Task 4.1**: `tests/extensions/gi-stubs.mjs` — a `Gio.DBus.session`
  `signal_subscribe`/`signal_unsubscribe` stub that records subscriptions and can fire
  the signal.
- [ ] ⬜ **Task 4.2**: `tests/extensions/test-panel-containers.mjs` (new suite; add it to
  `TEST_FILES` in `scripts/test-panel-sections.bash`): each finding shape renders,
  advisories never move the icon or notify, copy rows copy the hint/advice, notification
  dedupe and re-notify after disappearance, each D2 case, the DBus signal triggers a
  re-read, and nothing in the section spawns a process.
- [ ] ⬜ **Task 4.3**: `tests/extensions/test-panel-indicator.mjs` — the combined icon
  per D1 across the drift × container matrix, and `disable()` unsubscribes DBus and
  removes the container poll timer.
- [ ] ⬜ **Task 4.4**: `scripts/qa-nokill-containerwatch.bash` — scan the new section and
  report modules instead of the deleted directory, and fail when a declared target file is
  missing rather than `nullglob` it away; keep `--self-test` green and the summary wording
  that `scripts/lib/qa-helper-summary.bash` and `scripts/test-qa-helper-summary.bash` parse.
- [ ] ⬜ **Task 4.5**: Confirm `python3 -m helpers.gnome.check_extension_compat` and
  `python3 -m helpers.gnome.check_panel_contract .` still pass with the directory gone.
  - [ ] ⬜ Run `./scripts/test-panel-sections.bash` and `./scripts/qa-all.bash`

### Phase 5: Playbooks

- [ ] ⬜ **Task 5.1**: `play-container-watch.yml` — remove the `extension_*` vars and the
  three desktop-gated extension tasks (directory, copy, declare-enabled); update the header
  to name the panel play as the desktop surface. The backend tasks are untouched.
- [ ] ⬜ **Task 5.2**: The play chosen in D3 — retire the old extension, desktop-gated:
  `apply_enabled_extensions --retire-uuid=container-watch@fedora-desktop` with an assert on
  its markers, **then** `ansible.builtin.file: state: absent` on the deployed directory.
  Comment the pair as transitional. No `failed_when: false`, no `ignore_errors`.
- [ ] ⬜ **Task 5.3**: `play-fedora-desktop-panel.yml` — header and the `:17-19`/`:72-75`
  comments updated; the logout message still ends the play.
  - [ ] ⬜ Run `./scripts/qa-all.bash` (syntax-check and fail-fast grep)

### Phase 6: Documentation

- [ ] ⬜ **Task 6.1**: `docs/playbooks.md` — the `play-container-watch.yml` entry no longer
  installs an extension and points at the panel; the `play-fedora-desktop-panel.yml` entry
  describes the container section, the D1 icon rule and the retirement of the old uuid.
- [ ] ⬜ **Task 6.2**: `CLAUDE/QA.md:55` — the no-kill gate's scanned files.
- [ ] ⬜ **Task 6.3**: Pointer notes (not rewrites) in Plan 00109 `DESIGN-panel.md` §5/§7
  and Plan 00055 `testing-checklist.md` (its L3 visual pass now targets the panel section).

### Phase 7: HOST deploy and verification

- [ ] ⬜ **Task 7.1**: `deploy.bash` (on `_planlib.inc.bash`) — runs the container-watch
  play then the panel play, in that order, and ends by telling the operator to **log out
  and log back in** (Wayland: the only way the new panel code loads).
- [ ] ⬜ **Task 7.2**: `acceptance.bash` — checks `container-watch@fedora-desktop` is absent
  from `enabled-extensions` and from disk, `fedora-desktop@fedora-desktop` is enabled and
  deployed with `containerReport.js` and `sections/containers.js`, the timer is still
  active; prints `COVERAGE: n of m`. Names as NOT ESTABLISHABLE: the logout, and the
  visual check that one icon shows and turns amber for an injected finding.
- [ ] ⬜ **Task 7.3**: (On HOST) run `deploy.bash`, log out and back in, run
  `acceptance.bash`, then the human visual pass: one icon; inject a finding via the
  backend's `scan --inject` seam and see the icon and notification; copy a hint.

### Phase 8: Review

- [ ] ⬜ **Task 8.1**: Run the **`qa-reviewer` agent** over the full plan diff; resolve
  every BLOCK and FIX-BEFORE-MERGE finding.
- [ ] ⬜ **Task 8.2**: Update `CLAUDE/Plan/README.md`, set Complete, move to `Completed/`.

## Dependencies

- Depends on: owner decisions D1–D3 (Phase 1) before Phase 3 and Phase 5.
- Related: Plan 00109 (In Progress) owns the panel design this extends; Plan 00134
  (In Progress) edited the same extension — rebase on its latest panel changes first.
- Supersedes the surface in Plan 00055 (Dormant): its pending L3 visual pass of the old
  extension should be re-pointed at the panel section, not performed on the old one.

## Technical Decisions

Full options and reasoning in [DECISIONS.md](DECISIONS.md).

- **D1** icon state combination — **open**; recommended: fold into the three states,
  worst-of, container section first when it has findings, keep the notification.
- **D2** absent/unreadable report — **open**; recommended: not installed → hidden; no
  report yet → stated in words, `ok`; unreadable → `unavailable`.
- **D3** retiring play — **open**; recommended: the panel play.
- **D4** retirement mechanism — settled: new `--retire-uuid` in `apply_enabled_extensions`,
  then delete the files.
- **D5** section data sources — settled: optional per-section `source` and `state()`.

## Success Criteria

- [ ] On a host with both plays deployed and after a logout: one fedora-desktop icon.
- [ ] An injected container finding changes that icon per D1 and raises one notification.
- [ ] `container-watch@fedora-desktop` is in neither `enabled-extensions` nor the extensions
  directory, and re-running both plays reports no change.
- [ ] `./scripts/test-panel-sections.bash`, ESLint and `./scripts/qa-all.bash` pass; the
  no-kill gate reports a non-zero count of JavaScript files scanned.
- [ ] `qa-reviewer` finds no unresolved BLOCK or FIX-BEFORE-MERGE issue.

## Risks & Mitigations

| Risk                                                             | Impact | Probability | Mitigation                                                     |
| ---------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------- |
| No-kill gate silently scans no JS once the directory is deleted  | H      | H           | Task 4.4: re-point and fail on a missing target                |
| Retirement removes a user's own extension                        | H      | L           | Named uuids only; test that nothing else is removed (Task 2.1) |
| A live crash loop is hidden behind standing drift amber (D1 = A) | M      | M           | Notification kept; section first; Option B available           |
| Host keeps both icons, or loses the container surface            | M      | M           | D3: retire in the play that deploys the replacement            |
| Operator tests old code because they did not log out             | M      | M           | Both plays and `deploy.bash` end with the logout instruction   |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00144-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan written; research and decision options recorded.
