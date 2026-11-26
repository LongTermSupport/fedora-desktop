# Plan 00266: seat restart after reboot

**Status**: In Progress (design for the owner; nothing is built until the owner has seen it)
**Created**: 2026-11-26
**Owner**: dev
**Priority**: High (Phase A is a security gap)

## Overview

After the nightly host reboot, the dev LXC containers and the Claude seats inside them do not
come back. The owner asked for three things. First, a host-level snapshot of which LXC
containers were running at reboot, and a start of exactly those (not autostart for all).
Second, a way to restart the Claude sessions inside them. Third, a judgement on whether
"each seat knows its own startup command" is safe.

The design reads the code to answer that last question. A stored free-form command is the
hazard, so the restart must be rebuilt from a structured record whose every field is
validated. `ccy-sessions restore` is already close to that, for ccy and for host `cc`
sessions. The research found something more urgent. A container can write files in its
checkout that the host launcher applies on the next launch without asking, and one of them
can widen the container's own mounts. That is closed first, as a class, by Defence Before
Fix.

The full findings, each with file and line, are in [DESIGN.md](DESIGN.md).

## Goals

- A container session can never cause code to run on the host through anything its checkout
  holds, at an attended launch or an unattended restore.
- Every field a restore replays is validated against a fixed rule. No field is executed as
  given.
- A host `cc` seat resumes its seats' conversation on restore, as a ccy seat does.
- After a reboot, the LXC containers that were running, and the member sessions that were
  active, are started again. Nothing else is started.

## Non-Goals

- Autostarting every LXC container.
- A free-form "startup command" stored per seat.
- Switching a session's sign-in token (Plan 00146).

## Tasks

### Phase A: checkout content the host applies (security; Defence Before Fix)

- [ ] ⬜ **Task A.1**: Name the class "host launcher applies container-writable checkout
  config". Write its detector, prove it red on `.claude/ccy/mounts` and the compose start,
  and sweep for every other instance.
- [ ] ⬜ **Task A.2**: Make `.claude/ccy/mounts` an allowlist. Require approval of a changed
  file at an attended launch, with the approved file's hash kept outside the checkout. An
  unattended restore refuses a changed file.
- [ ] ⬜ **Task A.3**: Under restore, `--compose start` refuses a compose file whose hash differs
  from the one recorded at the attended launch.
- [ ] ⬜ **Task A.4**: Derive the record's `launcher` from its prefix (a fixed table), never
  executing a recorded path.
- [ ] ⬜ **Task A.5**: Replay arguments through an allowlist of the launcher's own flags. No
  unknown flag, and no word after `--`, for cc above all.

### Phase B: host cc seats and field validation

- [ ] ⬜ **Task B.1**: Resume by seat record for cc seats (`bySeats` is ccy-only today).
- [ ] ⬜ **Task B.2**: In `judgeRestore`, validate the checkout (a real git work tree, not a
  link, not at or above `$HOME`), the token (a pool name, never a path) and the seats
  (declared for this machine, of the record's kind).

### Phase C: LXC containers and member sessions

Owner (26 Nov, verbatim in substance): starting claude in an LXC container means getting in
(ssh or lxc-attach), going to the right folder, then the right claude command, and the
commands inside the container may differ from the host's. For now, log what command starts
each LXC seat and replay it. Do not agonise over the threat model for LXC yet; harden later.

So, for LXC only, each seat's record is the way in (lxc-attach or ssh, the container, the
user), the directory, and the launcher or claude command as it is inside that container. The
launch that starts the seat writes it, into a host-owned file (root or the host user, never
inside the container or a checkout it mounts), and restore replays it. The validation of
Phase A and B is owed for LXC too, and is recorded as owed, not blocking.

**The supported route for a teamchat (v2) seat inside a container** is its session unit, which
`teamchat-admin lxc start --member <container>/<key>` starts from the host (checked,
`src-go/internal/teamchat/host/lxc/unit/unit.go:1-95`).

- It runs as the container user, in the seat's checkout:
  `tmux … new-session -d … teamchat run --claude --kind lxc --key <key> --resume-recorded -- claude`.
  So it already resumes the seat's recorded conversation.
- It has no `[Install]` section and `Restart=no`, so nothing starts it at boot.
- A seat that must come back after the container boots is restarted through that unit (Task
  C.2), never through a unit of its own inside the container.
- (DESIGN.md's `agent-bus-claude-<user>.service` is the v1 bus's unit, not this one.)

**Sign-in (checked, `unit.go:84-95`):** the unit sets only HOME, XDG_RUNTIME_DIR and PATH and
passes no token, so claude uses the container user's own stored Claude login. A seat inside a
container therefore needs that user logged in to Claude once, interactively, inside the
container. The host's per-session token pool does not reach it.

**Finding the claude command (checked in `unit.go:92`; the install fact is from a team seat's
own check in a project's containers):** the unit's PATH is fixed:
`~/.local/bin:/usr/local/bin:/usr/bin:/bin`. In some containers, claude is an npm package
under nvm (`~/.nvm/versions/node/<version>/bin/claude`), and `~/.bashrc` loads nvm only in an
interactive shell. That container's own launchers source `nvm.sh` themselves. So a seat unit
there finds neither `claude` nor the `node` it runs on. This already breaks
`teamchat-admin lxc start` in such a container, not only a restart after a reboot.
Decision for C.2 (and the fix for today's start):

- **At every start, the boot replay included,** the host asks the container user's login
  shell for `command -v claude` and `command -v node`. Both directories go first in the PATH of
  that start.

- **Nothing resolved is frozen.** Neither the unit file nor the recorded start command keeps
  the PATH. The reason (from the coordinator, 26 Nov): such a container's play installs the
  node version its inventory names and removes every other version, so the nvm directory
  changes after any node bump, and a frozen PATH would refuse at the next boot.

- It refuses loudly, naming what is missing, only when the lookup itself finds nothing.

- Nothing is guessed from a version number or a directory pattern.

- **How it is built** (settled 26 Nov by this plan's builder, from the R77 sub-agent's
  questions, report `subagent-reports/261126-r77-c2.md`):

  - **The lookup is a new fixed operation of the in-container `teamchat` binary**, run as the
    seat's user, so the host still runs only its own binary there (TB3 holds; no raw
    `lxc-attach` shell).
  - **It asks the account's own shell** (the login shell named for that user in the
    container's passwd database), as `<shell> -l -i -c`, because nvm is loaded only by an
    interactive rc file. That runs the user's own rc files, as that user, in that user's
    container: what an interactive login there does anyway. It is bounded in time, and the
    two answers are read between markers, so anything an rc file prints is ignored.
  - **Each answer must be one clean absolute path** to an executable file, with no newline or
    control character. Anything else is a refusal naming what was found.
  - **The answer reaches the start through a runtime file, not the unit.** `lxc apply` writes,
    once, a fixed `EnvironmentFile=` line in the unit naming a file under the container's
    `/run`. Each start rewrites that file with the PATH it just found, then starts the unit.
    `/run` is emptied at every boot, so nothing survives to be stale. systemd's manager-wide
    environment is not used: it would reach every unit in the container.
  - **Who writes it, and where** (the fact-check, `subagent-reports/261126-plan-fact-checker-r89.md`
    claim 10, found the unit's own `RuntimeDirectory` cannot hold it: it is made at unit start,
    after the lookup, and removed at stop): the in-container operation runs as root, as the
    host's attach does. Only the shell lookup drops to the seat's user. The operation then
    writes the file as root into a directory of its own under `/run` (root-owned, 0755, the
    file 0644: it holds only a PATH). systemd reads `EnvironmentFile=` as the manager, so the
    seat's user needs no write access. The line has no `-` prefix: a start without a lookup
    fails loudly instead of running with the fixed PATH.

- [x] ✅ **Task C.0**: How an LXC seat starts and signs in: answered above.

- [ ] ⬜ **Task C.1**: Owner ruling, given 26 Nov: seats in LXC come back after a reboot, which
  reverses Plan 00257 Task B.12, "no LXC member session starts with its container", for the seats active at
  shutdown. Recorded; nothing more to decide.

- [ ] 🔄 **Task C.2**: `teamchat-admin lxc reconcile`, plus a root unit from
  `play-lxc-install-config.yml` that calls it. The interface was agreed 26 Nov with a seat whose
  host's own IaC already starts the right containers at boot and after its maintenance runs.

  - **The record:** every seat started through `lxc start --member` and not since stopped on
    purpose with `lxc stop --member`, root-owned under `/var/lib`, outside every container and
    checkout. A seat whose container was stopped (by a shutdown or by maintenance) stays recorded.
  - **The verb, idempotent:** for each recorded seat, if its container runs and its session unit
    does not, it starts the seat (`lxc start --member`, by the declared container and key only).
    It leaves a live session alone. It skips a stopped container and reports it, never starting
    it. One line per seat (started, already live, container stopped, refused with its cause).
    The exit is non-zero only when a start was refused. `--container <name>` limits it to one
    container.
  - **The unit:** at shutdown it records the running containers. At boot it starts exactly those
    containers, then runs the verb, before the user's session restore. A host variable,
    `teamchat_lxc_seat_restore_starts_containers` (default true), turns the container start off on a
    host whose own IaC owns that. The verb still runs there, called by that IaC at the end of
    its "started" step. This repo never names another project's systemd target.
  - **No watcher:** a container restarted outside any boot or maintenance run is not noticed
    until the next call.
  - **Tests:** the four seat cases, a refused start's exit, the record kept across a container
    stop, `--container`, and the variable off.
  - **Tell the asking seat** the release that carries the verb, so it wires the call in then,
    with a check that the verb exists.
  - Logic in Go. Before it: the claude and node lookup above, test first, in the unit and in
    `lxc start` (register R77). The lookup is built (R77 is fixed on v2-smoke). node is
    optional: a native claude has none beside it, so only a missing claude refuses. `guest check` finds claude the same way, or apply would refuse an nvm seat before its unit exists.
  - **Built (26 Nov):** the record is `/var/lib/teamchat-admin/_lxc-started.json` (root 0600,
    `lxcstate.Started`); `lxc start` adds a seat after a start that succeeded. `lxc stop --member` did not exist and is added: it takes the seat out of the record first (so a stop
    of a seat whose container is offline still counts), then stops its unit, with no lookup of
    claude. `lxc reconcile` judges a session live by its seat lock, as the L3 probe does, and
    prints `STARTED|LIVE|STOPPED|REFUSED\t<container>/<key>\t<why>`.
  - [x] ✅ The boot unit, its running-containers record and `teamchat_lxc_seat_restore_starts_containers`.
    **Decided (26 Nov, this plan's builder; the question is in `subagent-reports/261126-c2.md`):**
    the unit belongs to the play that installs `teamchat-admin`, not to
    `play-lxc-install-config.yml`. An LXC seat exists only where teamchat is installed, so a host
    without teamchat needs no unit, and the unit can rely on the command it calls. It is ordered
    after LXC's own service. The container snapshot (at stop) and the container start (at boot)
    are two more `teamchat-admin lxc` operations, which keeps one command and one record
    directory. The play passes the start step only when `teamchat_lxc_seat_restore_starts_containers` is
    true. With it false, the unit runs only `reconcile`, and the host's own IaC still calls
    `reconcile` itself at the end of its "started" step.
  - [x] ✅ **Decided:** a seat that leaves its team (`lxc apply --phase absent`) is a stop on
    purpose. The record rule already says "not since stopped on purpose", and an IaC removal is
    one. So `apply --phase absent` takes the seat out of the started record, as `lxc stop --member` does. Reconcile then never reports a seat that can no longer exist. **Built** (`absent.go`
    `run.leave`, first step, whether the container runs or not).
  - **Built (26 Nov), the boot unit:** `teamchat-lxc-record.service` (`ExecStop=` `lxc record-running`, after `lxc.service`) records the running containers at shutdown in
    `/var/lib/teamchat-admin/_lxc-running.json`. `teamchat-lxc-boot.service` (after
    `multi-user.target`) runs `lxc start-recorded`, then `lxc reconcile`. Both carry the six v1
    `InaccessiblePaths=`, so each runs in a private mount namespace. That is why
    start-recorded never runs `lxc-start` itself: it asks the host systemd to start
    `lxc@<container>.service`, through `units.Manager.Containers`, which starts only an `lxc@`
    instance whose name fits the container grammar. It then waits up to 60 s on the clock for
    lxc-info's `RUNNING`; a container still not running is `REFUSED`, named. The drop-in
    `lxc@.service.d/teamchat.conf` (`Before=teamchat-lxc-record.service`) makes the record
    unit stop first at shutdown, while every instance still runs. The play makes the drop-in's
    directory and writes it with the units, starts the record unit and enables the boot unit
    while an LXC member is present, and with `teamchat_lxc_seat_restore_starts_containers` false prints
    the boot unit without start-recorded.
  - [ ] ⬜ Open, unchecked: whether the host's `lxc` package ships `lxc@.service`. Fedora's is
    believed to; without it start-recorded refuses every container.
  - [ ] ⬜ Open, for C.3: after a boot start, the guest's systemd may not be ready when reconcile
    reaches it, so a seat may be `REFUSED`; the live reboot will show it.
  - [x] ✅ **Decided (26 Nov, the lead):** the variable agreed as
    `lxc_seat_restore_starts_containers` is renamed `teamchat_lxc_seat_restore_starts_containers`,
    because the paths rule D 3.1 (`TestEveryNameIsDerivedFromTheProduct`) requires every v2 name
    to be formed from `teamchat`, and that rule is not narrowed. The asking seat had not wired
    it yet. It is told the final name together with the release that carries the verb.
  - **Departure from the first design:** the container start is not wired from `host.Systemd`.
    That adapter refuses every unit but the teamchat server, terminator and restore units (SD8,
    SD10), so it would have refused every container on a host. `units.Manager.Containers` is a
    separate, start-only adapter for `lxc@` instances, and `host.Systemd` still refuses `lxc@`.

- [ ] ⬜ **Task C.3**: verify-restore reports the LXC set.

- [ ] ⬜ **Task C.4**: a restarted LXC seat signs in from its team's own token pool, on the token
  it was on before. Asked for by another team's owner (26 Nov) for their containers, and agreed
  by the coordinator. Design only until the owner has seen it; built after C.2 ships.

  - **The last-launch record:** the seat's launch writes, beside the record of its conversation,
    the token's pool name (a file's basename, never the secret, a descriptor or an inode) and its
    flags, checked against an allowlist. It sits in a host-owned or root-owned place, never in
    the checkout.
  - **The restart** reads that record and opens the named token from the pool inside the
    container (checked with that team: one token per file, `<team>_<name>.token`, under the
    agent user's `~/.claude-tokens/tokens/`, directories 0700 and files 0600, all owned by that
    user). It hands the token to claude through the root-written `/run` env file the seat unit
    already reads (that file is then 0600). The pool directory is recreated on every run of that
    team's play, so the record names a file, never a handle.
  - **Never a fallback:** a missing or emptied token is a refusal that names it. The restart
    never picks another token.
  - **Rotation** is deliberate only: a verb rewrites the record (that team prefers a command to a
    file). A restart never rotates.
  - **Sharing:** that team's owner has set no rule on two live seats using one token, so the
    design works either way. A rule, if one is wanted, is his to make.
  - **Later:** the same record is what a standalone cc/ccy would read. That is a separate plan,
    after batch 24 (agreed with the coordinator 26 Nov).

## Success Criteria

- [ ] A test plants each checkout-content change of Task A.1's class, and the launch and the
  restore both refuse it.
- [ ] A restore refuses a record with a launcher, flag, checkout, token or seat outside its rule.
- [ ] After a live reboot, the containers and seats that were running are back, and no others.

## Delivery & Milestones

- Design: [DESIGN.md](DESIGN.md), read in the code on 26 Nov.
