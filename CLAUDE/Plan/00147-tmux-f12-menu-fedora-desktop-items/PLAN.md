# Plan 00147: tmux F12 menu: Fedora Desktop items

**Status**: Not Started (decision gate: the Phase 1 owner decisions; the first item also
waits on Plan 00146)
**Created**: 2026-10-02
**Owner**: joseph
**Priority**: Medium

## Overview

F12 in every tmux session opens a fixed `display-menu` defined in `files/etc/tmux.conf`
(new, rename, switch, detach, kill). Adding a feature to it today means editing that one
binding, and running tmux servers only see the change after a reload. ccy sessions are
long-lived and are restored at boot, so every new item would mean reloading every server.

This plan adds ONE fixed entry, "Fedora Desktop ▸", to the F12 menu. Choosing it runs a host
script that builds the submenu at press time from a drop-in directory holding one
executable file per item. Each item file describes itself (label, key, where it applies,
popup or background) in leading comment lines, and the play that owns a feature ships that
feature's item, so adding an item needs no `tmux.conf` edit and no server reload. Items that
prompt run in a `display-popup -EE`, which stays open when the command fails. A `check` mode
validates every drop-in and is run by the playbook, so a bad item fails the play instead of
the next F12 press.

The first item is the ccy token switch from Plan 00146. Research, tmux constraints and the
file-by-file design: [RESEARCH-f12-menu.md](RESEARCH-f12-menu.md).

## Goals

- F12 shows a "Fedora Desktop ▸" entry whose submenu lists the installed drop-in items valid
  for the current server and session.
- A play adds an item by deploying one file; no `tmux.conf` change and no reload.
- A malformed drop-in, a duplicate key, a menu too big for the terminal, or an item opened
  in the wrong context fails with a message that stays on screen, never silently.
- The playbook fails on a bad drop-in.

## Non-Goals

- Changing the existing top-level F12 items.
- A per-user tmux config. `/etc/tmux.conf` stays the only config.
- The token switch mechanism itself (Plan 00146); this plan only gives it a menu entry.

## Tasks

### Phase 1: Owner decisions

- [ ] 🚫 **Task 1.1**: Owner decision: the host script's name and the submenu label.
  Options: `tmux-desktop-menu`, `fedora-desktop-menu`, `desktop-menu`; label
  "Fedora Desktop ▸" or ASCII "Fedora Desktop >" (the `▸` shows as `_` on a non-UTF-8
  client). Recommendation: `fedora-desktop-menu` (plain words, says whose menu it is) with
  the `▸` label. Blocked on the owner.
- [ ] 🚫 **Task 1.2**: Owner decision: whether `play-tmux-sessions.yml` re-sources
  `/etc/tmux.conf` into running tmux servers for the one-time F12 change. Options: (a) a
  probe-then-act task over the user's `ccy` and default sockets, acting only where a server
  answers and failing on any other error; (b) no task, and the docs say sessions pick it up
  after a restart. Recommendation: (a), since under strict IaC a "run this once by hand"
  line is not acceptable. Blocked on the owner.
- [ ] 🚫 **Task 1.3**: Owner decision: whether items marked for ccy also appear in `cc-`
  sessions (same `ccy` socket, Claude on the host). Options: (a) a separate `when` value per
  launcher (`ccy`, `cc`, `any`); (b) ccy items shown in both. Recommendation: (a), and the
  token switch is `ccy` only, because `cc` has no container and no ccy token pool. Blocked
  on the owner.

### Phase 2: The menu script

- [ ] ⬜ **Task 2.1**: Tests first: `scripts/test-<script>.bash` over the parse and validate
  function with fixtures (duplicate key, bad id, `#` or a leading `-` in a label, missing
  label, reserved keys `q`/Escape, `when` filtering), wired into `scripts/qa-all.bash` like
  the other `test-*.bash` suites.
- [ ] ⬜ **Task 2.2**: The script under `files/usr/local/bin/` with `menu <socket> <client> <pane>`, `run <id> <socket> <session> <pane>`, `check` and `--help`. It builds the
  `display-menu` argv as a bash array, validates every value it interpolates, checks the
  menu fits the client, exports the `TMUX_MENU_*` context to the item, and reports errors on
  stderr and with `display-message -d 0`. Non-interactive and fail-fast.
- [ ] ⬜ **Task 2.3**: `files/etc/tmux.conf`: add the one "Fedora Desktop" entry (key `f`)
  and update the header comment to name the drop-in directory.
- [ ] ⬜ **Task 2.4**: `playbooks/imports/play-tmux-sessions.yml`: create the drop-in
  directory (`root:root 0755`), install the script (`0755`), run `<script> check` after the
  existing config check (`changed_when: false`), and the Task 1.2 outcome.

### Phase 3: First item, the ccy token switch

- [ ] 🚫 **Task 3.1**: The token switch drop-in (`when: ccy`, popup), deployed by
  `play-claude-yolo.yml`, which fails with a named error if the drop-in directory is
  missing. It resolves the session's container and runs Plan 00146's switch command.
  Blocked on Plan 00146 Phase 3.

### Phase 4: Docs and verification

- [ ] ⬜ **Task 4.1**: `docs/tmux-sessions.md` (the new entry, an "Adding a menu item"
  section with the drop-in format), `docs/playbooks.md`, and `docs/ccy.md` for the token
  item.
- [ ] ⬜ **Task 4.2**: `deploy.bash` and `acceptance.bash` in this folder;
  `./scripts/qa-all.bash` green; `qa-reviewer` agent over the full diff.
- [ ] 🚫 **Task 4.3**: **HOST**: run `deploy.bash` then `acceptance.bash`, and press F12 in
  a ccy, a cc and a plain tmux session. Blocked on the owner: Ansible never runs in the ccy
  container, and the menu needs a person.

## Dependencies

- The first item depends on Plan 00146 (the switch command). Phases 1, 2 and 4 do not.

## Success Criteria

- [ ] F12 → "Fedora Desktop" lists exactly the items valid for the session it was opened
  from.
- [ ] A new item reaches running sessions by deploying one file, with no reload.
- [ ] A bad drop-in fails the playbook, and at press time shows an error that stays until
  a key is pressed.
- [ ] The token switch is reachable from F12 in a ccy session and absent elsewhere.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00147-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- (none yet)
