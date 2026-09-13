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
  in a second, private infrastructure checkout that consumes its plays, and the
  3119-line `claude-yolo` launcher has **zero**
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
- [x] ✅ **Task 2.2**: Confirm the on-disk transcripts for all four affected
  projects are intact and that `--continue` recovers each conversation. All
  four transcripts parse cleanly with no torn records; two sessions were already
  resumed by the user before the check ran, which is the `--continue` proof.

### Phase 3: Choose the mechanism

- [x] ✅ **Task 3.1**: tmux, not `systemd-run --pty`. A transient unit keeps the
  process alive but the pty is forwarded through the `systemd-run` client,
  which dies with the tab — nothing to re-attach to. tmux is the only option
  where re-attaching is the design, and Plan 00105 already ships it.
- [x] ✅ **Task 3.2**: Inside the launcher, automatic. Only the launcher knows
  which of its modes start a session; `--top`, `--help`, token management and
  `--headless` must not be wrapped. The re-exec sits after the last no-session
  mode returns and before the first prompt. Sessions use a dedicated socket
  (`tmux -L ccy`) and the server is started under `systemd-run --user --scope`,
  so its cgroup is never a terminal's — verified by experiment before building.
  This reverses 00105's "no wrapper", on the one fact 00105 never weighed: a
  terminal death now costs whole sessions.
- [x] ✅ **Task 3.3**: `ccy-<project>`, `ccy-<project>-2`, … from the same
  `get_project_name` as the container name. A launch re-attaches a *detached*
  session for the project if one exists, else takes the next free name.
- [x] ✅ **Task 3.4**: No reaper. A session ends when the launcher exits; one
  that outlives its terminal is the point. `tmux -L ccy ls` shows what is detached.

### Phase 4: Implement and deploy

- [x] ✅ **Task 4.1**: Implement via Ansible. Delivered as
  `files/var/local/claude-yolo/lib/tmux-session.bash`, sourced by the launcher
  (CCY 3.52.0) and deployed by `play-claude-yolo.yml`'s existing lib loop.
  `play-tmux-sessions.yml` and `tmux.conf` from Plan 00105 needed no change:
  the mechanism is CCY's, the tmux install is theirs. Deployed with
  `deploy.bash`. Two requirements added mid-plan by the user and delivered in
  the same change: `ccy` *offers* a detached session (Enter attaches, `n` is new,
  `q` quits) rather than attaching silently; and one terminal per session is
  enforced twice — an open session is never offered, and a server-side
  `client-attached` hook detaches any second client, so a race cannot mirror
  one `claude` into two terminals.
- [x] ✅ **Task 4.4**: `ccy-sessions` — a human command, not raw tmux
  incantations in the docs. An fzf picker of every session with state and
  directory: arrows choose, Enter attaches a detached one, Ctrl-X ends one,
  Ctrl-N runs a normal `ccy` in the current directory (git project folders
  only), Esc leaves; an open-elsewhere row refuses Enter. The first cut was a numbered
  menu with `k<number>`, which the user found unclear; replaced the same day.
  Deployed to `~/.local/bin` by `play-claude-yolo.yml`, sources the same library.
- [x] ✅ **Task 4.5**: The host `cc` wrapper gets the same insulation
  (CCY 3.53.0), on the same server, as `cc-<project>` sessions: offer on
  re-launch, single attach, listed by `ccy-sessions`. `get_project_name` moved
  to `common-pure.bash` so `cc` names sessions the way `ccy` names containers.
  Acceptance launches the deployed `cc` and checks it enters tmux before its
  token chooser.
- [x] ✅ **Task 4.2**: `podman run --rm` stays. With the tmux server owning the
  pty, the `podman run` client no longer dies with the tab, so `--rm` only runs
  when `claude` itself exits. No launcher change, no conflict with Plan 00079.
- [x] ✅ **Task 4.3**: `acceptance.bash` + `insulation-steps.bash`. It does not
  kill Ptyxis — that would destroy the user's real terminals — it SIGKILLs a
  throwaway pty owner (Python `pty`), which delivers the identical hang-up, and
  asserts survival, detachment, re-attachment, the server's cgroup, and the
  no-op cases, all against the deployed library.

### Phase 5: Verify

- [x] ✅ **Task 5.1**: Run `./scripts/qa-all.bash`. Green.
- [ ] ⬜ **Task 5.2**: Run `acceptance.bash` against a real Ptyxis kill — the
  production failure path, not a simulation of it.
- [x] ✅ **Task 5.3**: Run the `qa-reviewer` agent. Round one found three real
  blockers — the re-exec sat *after* the Quick Launch and SSH prompts, so every
  launch prompted twice; the acceptance never ran the launcher, so it could not
  see that; and "inside tmux means protected" was false for a tab-spawned
  server. All fixed, with the launcher itself now under acceptance (`--help`
  exits with no session; an interactive launch enters tmux before any prompt
  and shows its first prompt once), plus the should-fix items: `tmux` installed
  by the ccy play, `~/.local/bin` created first, listing failures propagated,
  sessions matched by directory not name, `--debug` says it is uninsulated.
  Journal 15:05 has the detail.

## Success Criteria

- [ ] Killing the Ptyxis process with a live CCY session running loses no
  conversation state, and the session is re-attachable with its in-flight
  turn present. (Proven for a pty hang-up by `acceptance.bash`; the real
  Ptyxis kill is Task 5.2, by hand.)
- [x] After such a kill, every surviving session is listable by project name
  and re-attachable without knowing a pid or uuid — `ccy-sessions`, or `ccy`
  in the project directory.
- [x] No CCY container is killed or removed as a consequence of its terminal
  dying — the `podman run` client now lives under the tmux server, so the
  `--rm` teardown cannot be triggered by a terminal.
- [x] The pty-owning process is a child of `systemd --user`, not of any
  `ptyxis-spawn-*.scope` — `acceptance.bash` asserts the server's cgroup is a
  `ccy-tmux-*.scope` under `user@`.
- [x] A session can be attached from one terminal only; `acceptance.bash`
  proves a second raw attach is bounced by the server.
- [x] `triage.bash` correctly distinguishes an OOM kill from a Wayland client
  error when run against this incident's journal window.
- [x] `qa-all.bash` passes and `qa-reviewer`'s findings are all addressed.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00111-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 substantially complete: cause established as a Wayland client error,
  OOM ruled out, container-detachment ruled out as a defence.
