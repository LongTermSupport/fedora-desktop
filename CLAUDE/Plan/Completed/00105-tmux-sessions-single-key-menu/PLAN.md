# Plan 00105: tmux sessions single key menu

**Status**: Complete
**Created**: 2026-09-08
**Owner**: joseph
**Priority**: Medium

## Overview

A headless development box runs long-horizon AI jobs (ccy in podman, work inside LXC
containers) that must keep running when the SSH connection drops or the operator walks
away, and must be easy to get back to. The operator does not want to learn a multiplexer:
no panes, no prefix chords, no send-keys, no layouts.

tmux already provides everything required (detachable sessions that survive a dropped
connection, naming, renaming, a built-in session chooser and a popup menu). This plan ships
tmux with a system-wide configuration that hides all of it behind ONE key: F12 opens a menu
with new / rename / switch / detach / kill. The status bar is off so it never competes with
Claude Code's own status line. Nothing else about the terminal changes.

## Goals

- `tmux` installed on every provisioned host (general scope, desktop and server alike).
- One key (F12) opens a menu offering: new session, rename session, switch session, detach,
  kill session. No other key binding is required to use it.
- Mouse scrolling works, scrollback is large, the tmux status bar is off.
- A user doc explains the three commands a person needs (`tmux new -s NAME`, `tmux attach`,
  F12) and what survives what (SSH drop: yes; reboot: no).

## Non-Goals

- No auto-attach on SSH login, no custom picker script, no per-project session templates.
- No persistence across reboot (systemd units, tmux-resurrect). Dev sessions are transient.
- No changes to how ccy, podman or LXC are started inside a session.

## Tasks

### Phase 1: ship it

- [x] ✅ **Task 1.1**: `files/etc/tmux.conf` — mouse on, 50k scrollback, status off, F12
  `display-menu` with new / rename / switch / detach / kill.
- [x] ✅ **Task 1.2**: `playbooks/imports/play-tmux-sessions.yml` (scope `general`): install
  `tmux`, deploy the config; imported by `playbook-main.yml`.
- [x] ✅ **Task 1.3**: `docs/tmux-sessions.md` and an index row in `docs/README.md`.
- [x] ✅ **Task 1.4**: deployed on a headless box by a downstream consumer and verified:
  the loaded config carries exactly one F12 binding (the five-item `display-menu`), status off,
  mouse on, `rename-session` works; a session started with a running job over one SSH
  connection was still there, job intact, from a second connection (journal 19:05, 19:25).

## Success Criteria

- [x] After a deploy, the deployed config loads with the five-item F12 menu bound; rename
  works against it (switch and detach are tmux built-ins the menu calls).
- [x] A session with a running process survives the SSH connection that started it ending;
  it is listed, with its process, from a fresh connection.
- [x] `./scripts/qa-all.bash` green.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00105-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Config, play, docs: see JOURNAL for the delivery commit.
