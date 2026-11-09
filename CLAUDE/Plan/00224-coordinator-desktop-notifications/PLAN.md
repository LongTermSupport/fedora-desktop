# Plan 00224: coordinator desktop notifications

**Status**: Not Started (plan only; decisions D1-D14 in DESIGN.md carry coordinator defaults)
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
records: the session's host-written membership file, a host-only seat map written when a
seat is minted (never the checkout's seat files), and `sudo agent-bus list`. It then validates and
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
- GNOME Shell renders markup in a notification body and not in the summary.
- In a privileged systemd LXC guest, an `lxc.mount.entry` bind (`bind,optional,create=dir`)
  of a host `/run` directory at the guest's `var/lib/agent-notify` is visible after the guest
  boots. The guest's own `/run` tmpfs would hide a target under `/run`, which is why the
  target is not there. A `tmpfiles.d` `a+` ACL applies on the host's `/run`, and the agent
  user's uid inside is its uid on the host. The guest client can read a 0644 result file the
  relay (the owner's uid) wrote in the member's 0700 directory.
- `agent-bus seat take` sees the handle `add-member` mints for each new seat, so it can write
  the seat map entry itself.

## Tasks

### Phase 1: Facts

- [ ] ⬜ **Task 1.1**: `triage.bash` (host, read-only apart from a throwaway directory and
  container it removes in an `EXIT` trap): each premise above, with its measured answer, the
  podman user-namespace mode ccy uses, and the SELinux denials (`ausearch`) if any.
  Findings in `triage-analysis.md`; a refuted premise changes DESIGN.md before Phase 2.

### Phase 2: The Go core (TDD: each test red before its code)

- [ ] ⬜ **Task 2.1**: `SPEC-notify-owner.md`, numbered requirements from DESIGN.md sections
  3.1-3.6 (routes, exit codes, request schema, checks, gate, limits, file safety), with the
  fault split of 3.3: which faults refuse one entry and which stop the relay.

- [ ] ⬜ **Task 2.2**: `src-go/internal/notifyowner` request: tests first for every refusal in
  DESIGN.md 3.3: an `id` that is not 16 lowercase hex or does not match the file's stem
  (`../` included); sizes and lines; control, bidi and invisible characters (with ZWJ emoji
  accepted); urgency; unknown keys. Also body-only markup escaping and the team prefix. Then
  the code.

- [ ] ⬜ **Task 2.3**: the limiter: tests first with a fake clock (burst, refill, global cap,
  duplicates, pre-gate counter, the one "paused" notice, state persisted and reloaded, a
  corrupt state file refused loudly rather than reset); then the code.

- [ ] ⬜ **Task 2.4**: the gate, tests first with fake roster, membership, seat-map,
  container-liveness and `lxcrecord` readers. Cases:

  - a coordinator seat; a worker; a parked coordinator;
  - **a worker whose checkout `member.json` names the coordinator's handle, refused 77**;
  - a seat with no seat-map entry (made before this plan, not adopted), refused 77 naming
    adoption;
  - a session with no running container, refused; a plain-terminal session (no label) found by
    container name;
  - several teams, with and without `team`, and more than 8 teams;
  - a role changed between two requests;
  - an LXC item `joined` or `stopped` accepted, any other status refused; an LXC file owner uid
    that does not match;
  - `sudo` failing: refused with the reason, never allowed;
  - the roster read once per team within a request.

  The role source is an interface (v1 roster now, v2 later). Then the code.

- [ ] ⬜ **Task 2.5**: the seat map's schema (one spec section, read by Go and written by the
  v1 Python `seat take`) and its Go reader, tests first: entries keyed (checkout, team, seat);
  a corrupt map refuses every ccy request with the reason, and the relay keeps running; an
  adoption entry written only by `ccy-teams` after the owner confirms (D12). Then the code.

- [ ] ⬜ **Task 2.6**: the spool, tests first in a temp directory:

  - a symlink, a FIFO, a socket, a directory and a hard link where a request should be;
    oversize; a name that is not `<16 hex>.req`;
  - a planted `<stem>.result` symlink; a result written 0644 whatever the umask;
  - pruning beyond 16 entries and after 60 s, by non-recursive `unlinkat`;
  - an unreadable or undeletable entry, refused while the next directory is still served;
  - a subdirectory inside a spool, not taken for an identity;
  - a request left from before start.

  Then the code.

- [ ] ⬜ **Task 2.7**: `src-go/cmd/notify-owner`, tests through fakes:

  - the client: route choice, including an LXC guest without the env file exiting 69; atomic
    write; wait; exit codes;
  - `relay`: watch roots created or awaited; inotify at the identity depths; `notify-send` by
    argument list through `execx`; exit only on its own state.

  Register it: a `scripts/go-binary-suites.tsv` row, the `src-go/CLAUDE.md` layout line, and
  the `go-deps.allowlist` reason if `x/sys/unix` gains a use. `go-qa` green.

### Phase 3: Host IaC

- [ ] ⬜ **Task 3.1**: `play-agent-bus.yml`:
  - build and install `notify-owner` to `/usr/local/bin` and the member kit, with
    `tasks/build-go-command.yml` and `go_command_static: true`;
  - deploy `notify-owner-relay.service` (a user unit on `graphical-session.target`, no
    `NoNewPrivileges=`, `OnFailure=` a one-shot notice unit, as `stt-watch-failed.service`
    does) and enable it;
  - `tmpfiles.d` for `/run/agent-notify`;
  - the play header names the relay.
- [ ] ⬜ **Task 3.2**: the seat map's writers (D14), tests first:
  - `agent-bus seat take` (`helpers/agent_bus`, with its `tests/helpers/` test) writes the
    entry for each seat it mints, from `add-member`'s result, atomically and 0600. This covers
    the launch, because the launcher runs it before the container starts, and the hot add
    through `SeatTakeAllocated`. A launch that mints a seat ends with that seat in the map.
  - `src-go/internal/teams/hot/add.go` and `hot/role.go` take a seat's handle from the map,
    not from the checkout's seat list, wherever it feeds a trust decision;
  - every writer takes one `flock` and keys the checkout by its resolved real path;
    adoption refuses a handle another entry holds.
  - `ccy-teams` lists seats made before this plan, with the handle each claims and the
    registry check, and writes an adoption entry only when the owner confirms (D12).
- [ ] ⬜ **Task 3.3**: `CLAUDE/AgentNotes.md`'s `notify-send` rule: name `notify-owner` as the
  one command (host and containers), the coordinator-only rule for containers, and the
  no-spam rule unchanged.

### Phase 4: podman ccy

- [ ] ⬜ **Task 4.1**: `ccy-teams internal membership-init` (Go, tests first) also creates
  `$XDG_RUNTIME_DIR/ccy/notify/<key>/` (0700) for every `ccy`-kind session carrying the bus kit (not `cc`), beside
  the membership directory. It wipes one a reused name left, and still prints only the
  membership directory. In `lib/agent-bus-membership.bash`, the one `CCY_SEAT_RUN_ARGS+=(…)`
  statement that mounts membership gains the spool's rw `-v` (same relabel), with the path
  derived as membership's is, and `-e NOTIFY_OWNER_SPOOL=/run/ccy/notify`. That changes a line
  and adds none and no function. The file is held to the hard bash limits with no headroom,
  and `qa-bash-limits` proves it. Removal at session end needs no bash: the relay prunes by
  container liveness. First, `scripts/test-ccy-agent-bus.bash` pins the new `-v` and `-e`
  arguments, red. **CCY version bump** (`CCY_VERSION`): the launcher's `lib/` changes.
- [ ] ⬜ **Task 4.2**: `play-claude-yolo.yml` builds `notify-owner` statically into
  `/opt/claude-yolo/optional/agent-bus/`. The Dockerfile links it into `/usr/local/bin` beside
  the other kit binaries, and `entrypoint.sh` gains no line (it is on the ratchet). **Container
  version bump** (the Dockerfile label and `REQUIRED_CONTAINER_VERSION`) **and CCY version
  bump**.
- [ ] ⬜ **Task 4.3**: the pingbus skill (`plugin/pingbus/skills/pingbus/SKILL.md`): a
  coordinator may use `notify-owner` when the owner asked to be told, or work is blocked on
  them; never for progress. In the image, so it rides Task 4.2's container version bump.

### Phase 5: LXC members

- [ ] ⬜ **Task 5.1**: `play-agent-bus.yml`'s LXC member phase, tests of the rendered lines
  first. Per member:

  - a `tmpfiles.d` line for `/run/agent-notify/lxc/<container>/<user>` (`0700 <uid>`, an
    `a+` ACL for the owner's user);
  - `NOTIFY_OWNER_SPOOL=/var/lib/agent-notify/<user>` in the member's env file;
  - `notify-owner` in the pushed kit;
  - a refusal for a container whose passwd gives the owner's uid to another account (D13).

  Per container, one marked `blockinfile` in its LXC config:
  `lxc.mount.entry = /run/agent-notify/lxc/<container> var/lib/agent-notify none bind,optional,create=dir 0 0`.
  It is removed when the container's last member leaves. Both are computed from the unfiltered
  `agent_bus_lxc_members`, never the team-narrowed list, and a test runs the play limited to
  one team and shows another team's mount kept. This is the first play to write an
  LXC config, so the owner's own lines are left untouched and the task tests that. The play
  reports a container whose mount waits for its next start (decision D7).

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
  refused 77, including after its checkout `member.json` is rewritten to the coordinator's
  handle; the fourth notice within the burst is refused 75; a symlinked request is refused;
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
