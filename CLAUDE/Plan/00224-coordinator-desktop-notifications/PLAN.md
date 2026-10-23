# Plan 00224: coordinator desktop notifications

**Status**: Not Started (plan only; decisions D1-D11 in DESIGN.md carry coordinator defaults)
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

A `cc` session on the desktop can send the owner a notice with `notify-send`; a ccy session
cannot, because a container has no session bus. The owner asked for the same ability for ccy
agents and LXC agents, **only when the agent is its team's coordinator**.

The container never gets the session bus: that bus can start host commands as the owner,
read the keyring and drive the whole session (DESIGN.md section 2). Instead, one Go command,
`notify-owner`, is the same everywhere. On the host it calls `notify-send` itself. In a
container it writes a small request into a spool directory that host tooling made for that
ccy session or LXC member, and a relay in the owner's graphical session handles it. The relay
works out who is asking from which directory the request is in, and from nothing the
container wrote. It checks that this member is a coordinator **now**, from the host's own
records (the session's membership file and `sudo agent-bus list`). It then validates and
sanitises the text, applies rate limits and runs `notify-send`. A spool of files is used, not
a socket, because SELinux stops a container connecting to a host socket (measured for the ssh
agent). Files cross that boundary with the relabel the membership mount already uses.

Remote members (Plan 00220) share no filesystem, so their route is the bus. The design leaves
a role-source seam for v2 (Plan 00209) and hands the note over; it builds nothing for them.
The full design, the threat reasoning and the decisions are in [DESIGN.md](DESIGN.md).

## Goals

- A coordinator in a podman ccy session or an LXC container runs `notify-owner "<title>" "<body>"` and the owner sees it, prefixed with the team.
- A worker, or any process whose identity is not a coordinator, is refused (exit 77) by the
  host, whatever the request says.
- A looping agent cannot flood the desktop: per-member and global limits, a duplicate filter,
  and a limit that stays across relay restarts.
- No container gets the session D-Bus, a host socket, or a relaxed SELinux label for this.
- The same command name and arguments in `cc`, ccy and LXC.

## Non-Goals

- Remote members (DESIGN.md section 5; Plan 00209 builds the bus side if wanted).
- Notification actions, icons, sounds, or replies from the owner.
- Gating host `cc` sessions by role (decision D2).
- Replacing Element's own notifications or the bus's human messages.

## Unverified premises (Task 1.1 settles them)

- A file a rootless ccy container writes into a rw, per-session-relabelled bind of a directory
  under `$XDG_RUNTIME_DIR` is owned by the owner's uid on the host and can be read and deleted
  by the owner's unconfined process; the host's inotify sees it.
- `sudo -n agent-bus list <team>` works from a systemd user unit (no terminal).
- A user unit bound to `graphical-session.target` has `DBUS_SESSION_BUS_ADDRESS`, and
  `notify-send` from it shows a notice.
- GNOME Shell renders markup in a notification body (whether escaping is needed or merely
  harmless).
- In a privileged LXC container an `lxc.mount.entry` bind of a `/run` directory works, a
  `tmpfiles.d` `a+` ACL applies on `/run`, and the agent user's uid inside is its uid on the
  host.
- A session's `membership.json` seats resolve to handles without a bus round trip (as
  `ccy-teams describe-seats` does).

## Tasks

### Phase 1: Facts

- [ ] ⬜ **Task 1.1**: `triage.bash` (host, read-only apart from a throwaway directory and
  container it removes in an `EXIT` trap): each premise above, with its measured answer, the
  podman user-namespace mode ccy uses, and the SELinux denials (`ausearch`) if any.
  Findings in `triage-analysis.md`; a refuted premise changes DESIGN.md before Phase 2.

### Phase 2: The Go core (TDD: each test red before its code)

- [ ] ⬜ **Task 2.1**: `SPEC-notify-owner.md`, numbered requirements from DESIGN.md sections
  3.1-3.6 (routes, exit codes, request schema, checks, gate, limits, file safety).
- [ ] ⬜ **Task 2.2**: `src-go/internal/notifyowner` request: tests first for every refusal in
  DESIGN.md 3.3 (sizes, lines, control and bidi characters, urgency, unknown keys) and for
  markup escaping and the team prefix; then the code.
- [ ] ⬜ **Task 2.3**: the limiter: tests first with a fake clock (burst, refill, global cap,
  duplicates, pre-gate counter, the one "paused" notice, state persisted and reloaded, a
  corrupt state file refused loudly rather than reset); then the code.
- [ ] ⬜ **Task 2.4**: the gate: tests first with fake roster and membership readers (ccy seat
  that is coordinator, worker, parked coordinator, several teams with and without `team`, a
  role changed between two requests, an LXC item whose file owner uid does not match, `sudo`
  failing → refused with the reason, never allowed); the role source as an interface (v1
  roster now, v2 later); then the code.
- [ ] ⬜ **Task 2.5**: the spool: tests first in a temp directory (symlink, FIFO, hard link,
  oversize, a planted `<id>.result` symlink, pruning beyond 16 entries and after 60 s, a
  request left from before start); then the code.
- [ ] ⬜ **Task 2.6**: `src-go/cmd/notify-owner`: the client (route choice, atomic write, wait,
  exit codes) and `relay` (inotify on both trees, new directories, `notify-send` by argument
  list through `execx`), tests through fakes; `go-qa` green.

### Phase 3: Host IaC

- [ ] ⬜ **Task 3.1**: `play-agent-bus.yml`: build and install `notify-owner` to `/usr/local/bin`
  and the member kit with `tasks/build-go-command.yml`; deploy `notify-owner-relay.service`
  (user unit, `graphical-session.target`) and enable it; `tmpfiles.d` for `/run/agent-notify`;
  the play header names the relay.
- [ ] ⬜ **Task 3.2**: `CLAUDE/AgentNotes.md`'s `notify-send` rule: name `notify-owner` as the
  one command (host and containers), the coordinator-only rule for containers, and the
  no-spam rule unchanged.

### Phase 4: podman ccy

- [ ] ⬜ **Task 4.1**: the launcher: create `$XDG_RUNTIME_DIR/ccy/notify/<session>/` (0700) for
  every session carrying the bus kit, mount it rw at `/run/ccy/notify` with the per-session
  relabel, pass `NOTIFY_OWNER_SPOOL=/run/ccy/notify`, and remove it in cleanup. The logic goes
  in Go (`ccy-teams internal membership-init` or the `notify-owner` binary prints the mount),
  so the bash on the ratchet does not grow; a test in `scripts/test-ccy-agent-bus.bash` first.
  **CCY version bump** (`CCY_VERSION`).
- [ ] ⬜ **Task 4.2**: `play-claude-yolo.yml` builds `notify-owner` into
  `/opt/claude-yolo/optional/agent-bus/`; the entrypoint puts it on `PATH` with the kit and
  refuses a `NOTIFY_OWNER_SPOOL` that is not a directory. **Container version bump** (the
  Dockerfile label and `REQUIRED_CONTAINER_VERSION`) **and CCY version bump**.
- [ ] ⬜ **Task 4.3**: the pingbus skill (`plugin/pingbus/skills/pingbus/SKILL.md`): a
  coordinator may use `notify-owner` when the owner asked to be told, or work is blocked on
  them; never for progress. In the image, so it rides Task 4.2's container version bump.

### Phase 5: LXC members

- [ ] ⬜ **Task 5.1**: `play-agent-bus.yml`'s LXC member phase: per member, a `tmpfiles.d`
  line for `/run/agent-notify/lxc/<container>/<user>` (`0700 <uid>`, an `a+` ACL for the
  owner's user); one `lxc.mount.entry` per container; `NOTIFY_OWNER_SPOOL` in the member's
  env file; `notify-owner` in the pushed kit. The play reports a container whose mount waits
  for its next start (decision D7); a test of the rendered lines first.
- [ ] ⬜ **Task 5.2**: `ccy-teams lxc-add`'s closing text names the restart when the mount is
  new; `docs/agent-bus-lxc-members.md` gains the spool (what the play does, the restart).

### Phase 6: Docs and handoff

- [ ] ⬜ **Task 6.1**: `docs/agent-bus.md`: a short section on coordinator notifications (who
  may, the limits, the exit codes), pointing at the spec.
- [ ] ⬜ **Task 6.2**: DESIGN.md section 5 handed to Plan 00209's coordinator as a design note
  (a task line in that plan); this plan sends no bus message itself.

### Phase 7: Deploy and accept

- [ ] ⬜ **Task 7.1**: `deploy.bash` (`play-agent-bus.yml`, `play-claude-yolo.yml`) and
  `acceptance.bash` with a coverage line: on a throwaway team, a ccy coordinator's notice is
  shown (the relay's journal line and `notify-send`'s exit); a worker in the same team is
  refused 77; the fourth notice within the burst is refused 75; a symlinked request is refused;
  the limit survives a relay restart; with the relay stopped the client exits 69 within its
  timeout; an LXC coordinator's notice is shown, or the container's pending restart is named
  as NOT ESTABLISHABLE. Entry in `meta-deploy.list`. Separate owner asks: look at the screen
  once for the shown notice, and restart an LXC container if one is pending.
- [ ] ⬜ **Task 7.2**: the `qa-reviewer` agent over the finished work.

## Success Criteria

- [ ] A coordinator's `notify-owner` from a ccy session and from an LXC container is shown,
  prefixed with its team.
- [ ] A worker's is refused by the host with exit 77, and no request content can change that.
- [ ] Limits hold across a relay restart; a loop of requests shows at most the burst and one
  "paused" notice.
- [ ] No container is given the session bus, a host socket or `label=disable` for this.
- [ ] The same `notify-owner` command works on the host, in ccy and in LXC.

## Delivery & Milestones

- Plan written; reviews in `subagent-reports/`, recorded in the journal.
