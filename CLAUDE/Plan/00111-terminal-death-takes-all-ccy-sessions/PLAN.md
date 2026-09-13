# Plan 00111: terminal death takes all ccy sessions

**Status**: In Progress
**Created**: 2026-09-13
**Owner**: joseph
**Priority**: High

## Overview

On 2026-09-13 at 13:23:45 BST every terminal on the desktop died at once, taking
the in-flight work of four concurrent CCY sessions with it. The suspected cause
was the OOM killer. **It was not.** Triage ruled out memory pressure on four
independent grounds and established the real cause: mutter severed the Wayland
connection to Ptyxis (`WL: error in client communication`), Ptyxis exited `1`,
and because Ptyxis is a single process owning every window and tab, all six tab
scopes were torn down with it.

The Wayland fault itself is upstream, first-seen in five recorded boots, and not
worth chasing. The defect worth fixing is structural and ours: **a CCY session's
pty is allocated by `podman run` inside a terminal tab, so the session cannot
outlive the terminal emulator.** Any death of that one process — this protocol
error, a GTK bug, a GPU reset, an accidental window close, a package upgrade
restarting it — destroys every session simultaneously. Container survival does
not help: three of the four containers did survive, and their `claude` processes
are still running at `TTY = ?` with no pty to attach to, which makes them
unreachable.

This plan makes a CCY session survive the death of the terminal showing it, by
moving pty ownership out of the tab and into a daemon that `systemd --user`
owns. Full evidence and the reasoning that ruled each alternative in or out:
[research-triage-2026-09-13.md](research-triage-2026-09-13.md).

## Goals

- A CCY session survives the death of its terminal emulator and can be resumed
  with its in-flight turn intact, demonstrated by killing Ptyxis and recovering
  the session.
- Sessions are discoverable after a crash — a user who has just lost every
  window can list what is still alive and re-attach to a named session.
- No CCY container is destroyed as a side effect of its terminal dying.
- The mechanism is deployed by Ansible and needs no per-session discipline from
  the user to be effective.

## Non-Goals

- Diagnosing or fixing the upstream Ptyxis/GTK4/mutter Wayland protocol error.
  One occurrence in five boots in third-party code; out of scope.
- Replacing Ptyxis with another terminal emulator. Any single-process terminal
  has the same blast radius, so this would not address the defect.
- Preserving an in-flight turn against `claude` itself crashing, or against a
  host reboot. The scope is terminal-emulator death only.
- Reworking `claude-supervise.py`'s restart or recovery behaviour.
- Re-doing tmux provisioning. Plan 00105 (Complete) already installs `tmux` and
  deploys `files/etc/tmux.conf` via `playbooks/imports/play-tmux-sessions.yml`.

## Related plans

- **Plan 00105 — tmux sessions single key menu (Complete)** ships the tmux
  foundation this plan builds on, and named "no changes to how ccy, podman or
  LXC are started inside a session" as an explicit non-goal. Plan 00111 fills
  exactly that gap.

  What 00105 actually shipped is **config only, no script**: a 25-line
  `files/etc/tmux.conf` (mouse on, 50k scrollback, status off, F12 session menu)
  deployed by `playbooks/imports/play-tmux-sessions.yml`, plus
  `docs/tmux-sessions.md`. The config has **no `new-session -A`, no
  `has-session`, no auto-attach**.

  It also **explicitly rejected a wrapper**: 00105's non-goals bar "no
  auto-attach on SSH login, no custom picker script, no per-project session
  templates", and its journal rejects a systemd layer as "overkill" and a login
  picker with per-project templates as "over engineered". Task 3.2 therefore
  **revisits a decision that was deliberately made, not an oversight** — the new
  evidence being that a terminal-emulator death now costs whole sessions, which
  was not on the table in 00105. That reversal should be argued, not assumed.

  Searched and confirmed absent: no tmux wrapper script anywhere in this repo or
  in the lts-infra checkout, and the 3119-line `claude-yolo` launcher has **zero**
  occurrences of `tmux`, `TMUX` or `screen` — no `$TMUX` detection, no re-exec, no
  warning. `ccy` itself is one line:
  `alias ccy='/var/local/claude-yolo/claude-yolo'`
  (`files/home/bashrc-includes/claude-yolo.bash`), which is a convenient seam for
  wrapping without touching the launcher.

- **Plan 00079 — Podman container control (Blocked)** already records that
  `podman run --rm` prevents checkpoint/restore. Same subsystem as Task 4.2 and
  the same flag, different aim (freeze/thaw rather than surviving tab death);
  check it before changing `--rm` so the two do not conflict.

## Tasks

### Phase 1: Establish the facts

- [x] ✅ **Task 1.1**: Rule out OOM as the cause — kernel ring, `systemd-oomd`,
  coredumps, memory headroom.
- [x] ✅ **Task 1.2**: Identify the actual trigger and its blast radius —
  Wayland protocol error, Ptyxis single-process tab ownership.
- [x] ✅ **Task 1.3**: Determine why three containers survived and one was
  destroyed, and whether container survival preserves a session. It does
  not — the pty is what matters.
- [x] ✅ **Task 1.4**: Persist the triage evidence as a supporting document in
  this plan folder, and add `triage.bash` so the OOM-vs-Wayland
  determination is re-runnable against a future incident. Delivered as
  `triage.bash` + `probe-mass-terminal-death.bash`; a run reproduces the whole
  determination — four OOM negatives, the Wayland positive, the Ptyxis-restart
  fingerprint, and whether any session is currently insulated.

### Phase 2: Recover the current loss

- [x] ✅ **Task 2.1**: Reap the orphaned pty-less `claude` processes. No action
  needed in the end — all three exited on their own before anything was done to
  them. Recorded rather than dropped, because "it resolved itself" is the sort of
  thing that otherwise gets rediscovered as a mystery.
- [ ] ⬜ **Task 2.2**: Confirm the on-disk transcripts for all four affected
  projects are intact and that `--continue` recovers each conversation.

### Phase 3: Choose the mechanism

- [ ] ⬜ **Task 3.1**: Decide between a tmux-server-owned pty and a
  `systemd-run --user` transient unit. Triage recommends tmux; record the
  decision and its rationale before building. Note Plan 00105 has already
  shipped the tmux foundation, which weighs heavily for tmux.
- [ ] ⬜ **Task 3.2**: Decide where the wrapping belongs — inside the host
  `ccy` script, or a separate launcher the user invokes. This determines
  whether protection is automatic or opt-in. Plan 00105 deliberately left
  "how ccy, podman or LXC are started inside a session" as a non-goal, so
  this is the gap it left open, not a re-litigation of its decision.
- [ ] ⬜ **Task 3.3**: Design the session naming scheme so a session is
  findable by project after every window has gone.
- [ ] ⬜ **Task 3.4**: Decide whether a stale-session reaper is needed, and on
  what trigger, so abandoned sessions do not accumulate.

### Phase 4: Implement and deploy

- [ ] ⬜ **Task 4.1**: Implement the chosen mechanism via Ansible. If tmux, this
  extends the existing `playbooks/imports/play-tmux-sessions.yml` and
  `files/etc/tmux.conf` from Plan 00105 rather than adding a new playbook —
  tmux installation and configuration are already handled there.
- [ ] ⬜ **Task 4.2**: Address the `podman run --rm` teardown path, so a tab
  death cannot destroy a container outright as it did `family-qnap_yolo`.
- [ ] ⬜ **Task 4.3**: Add `acceptance.bash` that kills Ptyxis with a live CCY
  session running and asserts the session is recoverable.

### Phase 5: Verify

- [ ] ⬜ **Task 5.1**: Run `./scripts/qa-all.bash`.
- [ ] ⬜ **Task 5.2**: Run `acceptance.bash` against a real Ptyxis kill — the
  production failure path, not a simulation of it.
- [ ] ⬜ **Task 5.3**: Run the `qa-reviewer` agent.

## Success Criteria

- [ ] Killing the Ptyxis process with a live CCY session running loses no
  conversation state, and the session is re-attachable with its in-flight
  turn present.
- [ ] After such a kill, every surviving session is listable by project name
  and re-attachable without knowing a pid or uuid.
- [ ] No CCY container is killed or removed as a consequence of its terminal
  dying.
- [ ] The pty-owning process is a child of `systemd --user`, not of any
  `ptyxis-spawn-*.scope` — verifiable from the cgroup path.
- [ ] `triage.bash` correctly distinguishes an OOM kill from a Wayland client
  error when run against this incident's journal window.
- [ ] `qa-all.bash` passes and `qa-reviewer` reports no findings.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00111-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 substantially complete: cause established as a Wayland client error,
  OOM ruled out, container-detachment ruled out as a defence.
