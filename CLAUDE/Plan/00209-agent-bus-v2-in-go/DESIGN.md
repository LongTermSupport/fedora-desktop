# Plan 00209 design: agent bus v2, rebuilt in Go

**Status**: proposed, for Fable's review and the owner's decisions (section 14). Nothing here is
built.

**The owner's direction.** "I thought we'd migrated ccy-teams to Go. Why is it using Python?",
then: "I think we need to be porting agent bus to Go... don't just do a naive port of Python to
Go... properly optimise... a version 2, a rebuild rather than a straight-up port."

So this is a design from the requirements, not a translation. v1 (the Python, bash and Go that
run today) is read only to find what the bus must do and what it learnt the hard way. Where v1 is
shaped by its own constraints (Python start-up, bash's limits, no daemon), v2 is not bound by it.

**Evidence.** Four research digests, each with `file:line` citations into v1, are committed
beside this file and are this design's requirement sources:

| Digest                                                   | Covers                                                                                                                          |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| [RESEARCH-pingbus.md](RESEARCH-pingbus.md)               | the member client: every command, state file, lock, Matrix call, delivery, goodbye and disconnected; 29 hard-won behaviours     |
| [RESEARCH-host.md](RESEARCH-host.md)                     | the host side: admin tool, root wrapper, sudoers, installer, units, play, on-disk files, Tuwunel quirks; 39 hard-won behaviours |
| [RESEARCH-lxc-robustness.md](RESEARCH-lxc-robustness.md) | LXC members, many teams per session, hot control, stopped containers, play lock, incidents                                      |
| [RESEARCH-go-callers.md](RESEARCH-go-callers.md)         | every call ccy-teams and the launcher make into the bus, how often, and where an open's time goes                               |

Citations below name the digest (`[P]`, `[H]`, `[L]`, `[G]` and a section) rather than repeat v1's
line numbers.

Related plans: 00161 (the bus, its DESIGN.md decisions D1-D65), 00194 (live delivery), 00196
(coordinator role), 00197 (seat broker daemon, not started), 00201 (many teams per session),
00203 (hot control), 00206 (deployed tools never run from the checkout), 00202 (the pingbus kit
built twice), 00192 Task 3.2 (the Go port of `agent-bus-install`, built on branch
`go-port/agent-bus-install`, awaiting the owner's host test).

---

## 1. Why v2

**Opening `ccy-teams` takes 9-23 s.** Every bus read is a separate Python process, run one after
another [G 0, 1d]. An open starts Python `7 + C + T + H` times (C checkouts with seats, T teams
that are up, H people in them): seven `declare.py` runs (`overview` three times), one
`agent-bus seat list` per checkout, and through `sudo -n` -> bash -> `runuser` -> Python one
`agent-bus list` per team and one `human devices` per person. Each start costs about 0.45 s, and
the cost is the imports, not the interpreter (bare `python3` 33 ms, `import helpers.agent_bus.checkout` 420 ms [G 0]); from a zipapp nothing is cached as bytecode. With six
checkouts, three teams and six people that is 22 starts, about 10 s, before sudo, PAM and HTTP.
Every redraw pays about 10 more [G 1d].

**Every running session pays it too.** The status line runs `pingbus status-line` every second
(`refreshInterval: 1`), and every prompt, every turn end and every SessionEnd starts a Python
`pingbus hook` [P 7]. Claude Code gives all SessionEnd hooks 1.5 s together; Python's start-up
alone was about 1 s in a container, which is why no goodbye was ever sent until the session's
settings raised the limit to 8 s [P 5.01].

**One subsystem is five languages of glue.** A bash installer, a bash root wrapper that
`runuser`s to a Python zipapp, Python helpers loaded straight from the checkout (`ccy-teams` runs
`python3 -P -m helpers.agent_bus.declare` with the checkout on `PYTHONPATH`, which Plan 00206
records as a runtime-from-checkout offender), Python scripts sent into LXC containers as
`python3 -I -c` strings, and Go `ccy-teams` parsing all of their tab-separated marker lines and
exit codes [H 8.01, L 7.01, G 2]. The same rule (a handle, a unit name, a state path) is written
two or three times [H 8.06]; the pingbus kit is built from two sources (Plan 00202).

**Reads need root for the wrong reason.** `list` and `human devices` need root only because the
admin token that answers them sits in the `agent-bus` user's 0700 directory. The sudoers rule
that lets them run without a password exists to work around the cost of asking [H 5].

v2's aim, in one line: **one Go codebase for every part of the bus, the host's live team state
held by one daemon and read in-process, and no Python on any hot path**, with every requirement
and every hard-won behaviour of v1 kept, and v1 and v2 able to run side by side until v1 is
retired.

---

## 2. Requirements

What the bus must do, grouped. Each group cites where v1 states or proves it. A requirement is
kept unless section 12 says otherwise and why.

### 2.1 Teams and the homeserver (host)

- R1. One private Tuwunel homeserver per team, federation off, registration closed, room creation
  only by the team's admin, encryption off, one invite-only room, room version 12 [H 3].
- R2. The team is declared in a team file (from the untracked host_vars on a desktop, from
  another project's IaC elsewhere) and applied by the installer, idempotently, printing `CHANGED`
  only on change and restarting the homeserver only when its configuration, drop-in, binary or base
  unit changed [H 6, H 7.12].
- R3. Loopback plus the team's `listen` addresses only; `allow_from` enforced by both firewalld
  and the unit's IP filter; no TLS (documented); the LXC bridge's live subnet admitted
  automatically [H 6, H 7.36].
- R4. `server_name` never changes; a team's name is its unit and data folder [H 7.08].
- R5. Hardened units: per-team `TemporaryFileSystem` + `BindPaths`, `PrivatePIDs`, no resolver,
  `Type=notify` with a watchdog, `Restart=always` without a start limit [H 5, H 7.22, H 7.23].
- R6. Daily backups (database + a root-only state tar with the same number), backup before a run
  that restarts homeservers, restore validated before anything stops; the backup holds the root of
  trust [H 6, H 7.14-7.16].
- R7. Health: a minutely probe that restarts a homeserver missing three probes, notes systemd's
  restarts, checks backups, and writes a group-readable record that expires [H 1.5, H 7.23-7.26].
- R8. An applied stamp per team (config and software digests) so `ccy-teams` can say up to date,
  out of date, not built or decide [H 6, D65].

### 2.2 Accounts, members, seats and roles (host)

- R9. Handles `<repo>.<seat>+<host>.<type>`, never the machine's hostname; seats numbered per
  `<repo>+<host>.<type>`, counters never go down; a parked handle returns with a new token and its
  role; a removed handle never returns [H 4].
- R10. At most one coordinator per team, refused naming the holder; demote before promote across
  every change [H 4, L 1.3].
- R11. A member's token leaves the host only inside its bundle; a human's password only as
  `human password`'s output, never stored; park = revoke (password reset with logout) then mark
  [H 3, H 7.10].
- R12. Root never writes under a user's path: a bundle is placed by the user, by file descriptor,
  `O_NOFOLLOW|O_EXCL`, `member.json` last, after `git check-ignore` proves the seat directory cannot
  be committed [H 5, H 7.31-7.33].
- R13. Creating or parking a member needs the owner's consent at the time (today: the sudo
  password), and every launch or `ccy-teams` run that used sudo drops the cached credential before
  it ends [H 1.4, H 7.28].
- R14. Seat allocation is serialised per checkout by a lock no container can hold; a seat named by
  a running session's desired membership counts as held; a launch into a held seat is refused;
  seat check and take wait up to 180 s for a homeserver that is still starting [H 4, H 7.27-7.30].

### 2.3 Messaging (wire protocol, both sides)

- R15. Protocol version 1 as `docs/agent-bus-protocol.md` states it: custom types under
  `agent_bus`, the team record and status state events, closed verbs, the reference grammar,
  human text, agent text, size and rate limits, the drop reasons [P 3].
- R16. Every reference checked at GitHub with the member's own `gh` login; fork commits never
  prove provenance; external content marked `external` and read-only verbs only [P 3, P 5.23].
- R17. The room is verified (create event, admin-sent record, exact power levels, own role)
  before anything is trusted, again on a limited timeline or a trust-type event [P 3].
- R18. The inbox is an untrusted cache: only a homeserver re-fetch is ever printed or delivered
  [P 5.05].
- R19. First sync and post-join sync skip history; a limited timeline is gap-filled; sends retry
  with the same transaction ID; 429 honoured within bounds [P 3, P 5.17, P 5.18].
- R20. Several teams per session: send and say infer the team only when one fits, otherwise
  refuse with a list [P 5.26, L 2].

### 2.4 Delivery into a session (member side)

- R21. Every message reaches the session as its own envelope (`agent-bus delivery <nonce>: ...`)
  that the sender's text cannot close or imitate, with the kind (`HUMAN`, `PEER`, `PING`,
  `TIMEOUT`, `MEMBERSHIP`) set from the admin-written record, never from text; bodies rendered by
  an allow-list [P 4, P 5.24].
- R22. Delivered exactly once: re-fetch outside the delivery lock, re-check under it; a full
  socket write counts as sent even if the close fails; a delivery not found in the transcript is
  reported [P 5.06, P 5.07].
- R23. Read receipts for a HUMAN item only after its nonce is found in the transcript [P 3].
- R24. The receiver survives homeserver restarts, including at its first sync, says so once when
  an outage starts and once when it ends, and is restarted if it dies [P 5.10, P 5.11].
- R25. Hooks never block a turn on a missing memory (Stop guard once per pending set), never
  announce counts while delivery is live, always answer [P 5.12, P 5.13].
- R26. The status line shows `👥 team (role) (down)` rows, refreshed every second, always exits 0,
  wraps the session's own status line [P 5.19].

### 2.5 Presence and lifecycle notices

- R27. Presence is the seat lock alone, held by one process (close-on-exec), dying with its
  session; locks are `flock`, never PIDs [P 4, P 5.29].
- R28. Goodbye: one fixed-template message per real session end (exit, logout, Ctrl-C), never on
  `/clear` or `/resume`, to humans and coordinators only, recorded before the attempt, within the
  SessionEnd budget [P 5.01-5.04].
- R29. Disconnected: posted from the host for each seat a session held when it ended without a
  goodbye, never both, never twice for one claim, tolerant of a dead terminal [P 5.03, L 4].
- R30. Membership notices (joined, left, role changed, teams now) once per change, with the next
  prompt as the fallback [L 4].

### 2.6 Hot control, multi-team sessions, LXC

- R31. The host writes a session's desired membership into a directory the container mounts
  read-only; the session reconciles within 2 s; active teams = held seats ∩ desired; record before
  desired file; farewell and retire on remove; nothing posted while out is delivered on re-add
  [L 3].
- R32. A session started on no team is relaunched once on its first join (Claude Code reads
  `--settings` once) [L 6.11].
- R33. LXC members: handle derived from the container's checkout read from the host as root,
  never by running container-controlled code; every running container checked before any change;
  a stopped container is ordinary (SKIPPED / DEFERRED), only a missing one fails; one coordinator at
  every instant; the member record per team [L 1].
- R34. Every change that runs a play takes the host's play lock first and writes the host_vars only
  once it holds it [L 3.4].

### 2.7 The owner's views (ccy-teams)

- R35. The board: each team (homeserver state, port, APPLIED, health, description), its seats
  (holder, restore), its LXC and other members (active, parked, deferred), its people (in the room,
  how many Element sessions) [G 1, docs/agent-bus.md].
- R36. `ccy-teams --list`, the picker, choose-teams, describe-seats, the login flow, settings,
  create, apply, lxc-add, session add/remove/move/role/repair, shutdown, restart, backup, restore,
  remove: every flow keeps its behaviour and wording unless a numbered departure changes it.

### 2.8 Security invariants (from Plan 00161 section 9, unchanged)

- R37. No agent runs as a user that holds a human's Matrix session (dedicated agent user, ccy
  container, or the accepted `cc` exception).
- R38. Root on the homeserver host, and whoever holds a backup, can instruct every agent; v2
  must not widen who that is.
- R39. Container-written files are never authority: the inbox, a seat's report, a checkout's
  `ccy.env.local` are data to validate, never instructions.

---

## 3. Shape of v2

```text
                     ┌──────────────────────── host ─────────────────────────────────────┐
  owner ── ccy-teams ─┤ in-process: bus/view (host_vars, stamps, health, seats, units)    │
         (Go, user)   │            └─ one request ──▶ /run/agent-bus/view.sock            │
                      │                                    │                              │
  sudo agent-bus ... ─┤──────── one request ──▶ /run/agent-bus/admin.sock                 │
  (Go CLI, root peer) │                                    │                              │
                      │   agent-busd (Go, user agent-bus, one per host)                   │
                      │     per team: admin session, room sync, registry (single writer), │
                      │               device poll, live view                              │
                      │                                    │ loopback HTTP                │
                      │   agent-bus-hs@<team> (Tuwunel) ◀──┘                              │
                      │   agent-bus-install (Go, root): software, team, remove, backup,   │
                      │                      restore, check, health-check, lxc-members    │
                      └───────────────────────────────────────────────────────────────────┘
  in a ccy / cc / LXC / docker / VM member:
     pingbus (Go, static): keeper (locks, supervision) ── worker (sync, deliver) ── hooks, status line, CLI
```

### 3.1 One codebase, four binaries

All in `src-go`, built by `tasks/build-go-command.yml`, versioned by source digest (Plan 00206):

| Binary              | Runs as                            | Where                                                                                                                              | Replaces                                                                                                                                |
| ------------------- | ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `agent-busd`        | `agent-bus` (system user)          | host, one systemd unit for every team                                                                                              | the admin half of `agent-bus.pyz`, the `sudo list` / `human devices` reads                                                              |
| `agent-bus`         | the caller (user, or root by sudo) | host                                                                                                                               | the bash root wrapper, its `runuser` and tar dance, the seat commands, `health status`                                                  |
| `agent-bus-install` | root                               | host                                                                                                                               | the bash installer (Plan 00192 Task 3.2's port), `render`, `bridge.py`, `config_restart`, `timer_state`, the health check, `members.py` |
| `pingbus`           | the member's user                  | every member: ccy container, cc on the host, LXC, docker, VM; also the host for `seat disconnected`, `farewell`, `retire`, `reach` | `pingbus.pyz` and the kit's Python                                                                                                      |

`ccy-teams` (and the ccy launcher's Go helpers) link the same packages and call them as functions;
no `ccy-teams` read starts a bus process.

### 3.2 Packages

Under `src-go/internal/bus/`, each behind an interface with a fake, test first:

| Package             | Owns                                                                                                                                                                                                                                     |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `bus/proto`         | the wire protocol v1: identifiers and handle grammar, event content types, ping, verbs, references, human and agent text, team record, power levels, limits, validation, drop reasons. The one home of every grammar, used by both sides |
| `bus/state`         | every on-disk schema (section 5): strict decoders with version ranges, atomic writers, the path layout (one home for `/var/lib/agent-bus/<team>/...`, unit names, seat paths)                                                            |
| `bus/matrix`        | a small Matrix client: one transport (no proxy, no redirects, token only in an unredirected header, bounded bodies, errors without server text), sync, send with txn reuse, state, receipts, messages, admin endpoints                   |
| `bus/matrix/fakehs` | an in-memory homeserver for tests: the endpoints both sides use, with Tuwunel's recorded quirks as switchable behaviours (section 11)                                                                                                    |
| `bus/forge`         | the GitHub checks (R16) and their cache                                                                                                                                                                                                  |
| `bus/admin`         | the team admin operations (bootstrap, members, roles, humans) over `bus/matrix` and `bus/state`; used by `agent-busd` only                                                                                                               |
| `bus/view`          | the read model: the types ccy-teams reads, assembled from in-process sources and one daemon request (section 4)                                                                                                                          |
| `bus/seat`          | a checkout's seats: list, choose, take, remove, the allocation lock, bundle placement by fd                                                                                                                                              |
| `bus/declare`       | the host_vars declarations: read, check and edit `agent_bus_teams`, `agent_bus_lxc_members`, `agent_bus_element_teams`, `agent_bus_address` (replaces `declare.py`)                                                                      |
| `bus/member`        | the member client: config and bundle checks, inbox and outbox, sync engine, delivery (envelope, socket, transcript), hooks, status line, keeper, membership                                                                              |
| `bus/lxc`           | LXC member derivation and apply (R33), guest operations through the kit's `pingbus guest`                                                                                                                                                |
| `bus/install`       | what is today `internal/businstall` on the 00192 branch, plus the daemon's unit and the health check                                                                                                                                     |

The exec, marker-line and exit-code layers of today's `internal/agentbus` and `internal/declare`
disappear for every in-process call [G 6]. Marker lines and exit codes stay where a process
boundary stays: the CLIs a human, a play or the bash launcher reads.

### 3.3 Why a daemon, and what it is not

`agent-busd` is one long-running process, as the `agent-bus` user, that holds each team's admin
session. It exists for three reasons:

1. **A live read model.** It follows each team room with the admin account's own `/sync` (the
   admin is a member of the room), so room membership changes reach it as events, and polls each
   human's devices on a short interval. A `ccy-teams` open then asks one question and gets every
   team's members and people from memory, without root, in milliseconds.
2. **One writer.** Every registry and homeserver admin change goes through one process, so the
   registry lock, the "claim first, then account, undo on failure" discipline and the
   one-coordinator rule are enforced in one place instead of across concurrent CLI processes.
3. **The seat broker's home.** Plan 00197's broker (owner-approved seat requests from containers)
   is a change-serving daemon beside the homeservers. v2 builds the daemon it needs, without the
   broker's network listener (section 13).

It is **not** on the message path: members talk to their homeserver directly, as in v1, so a
daemon that is down stops admin changes and the owner's live view, never a message. It holds no
human's password and adds no secret: the admin tokens it uses are the files the admin tool reads
today, owned by the same user.

---

## 4. The read model ccy-teams uses

### 4.1 Sources, privilege and cost

| Fact on the board                                        | Source                                                                                                   | Read by                                       | Privilege              | v1 cost per open                                           | v2 cost         |
| -------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | --------------------------------------------- | ---------------------- | ---------------------------------------------------------- | --------------- |
| declared teams, descriptions, LXC entries, Element teams | host_vars `localhost.yml`                                                                                | `bus/declare`, in-process                     | the user               | 7 Python starts (~3.7 s)                                   | one YAML parse  |
| APPLIED (current / out of date / not built / decide)     | stamps `/var/lib/agent-bus-applied/<t>.json` (0640, stamp group) + the deployed binaries' source digests | `bus/view`, in-process                        | stamp group (the user) | inside `overview`, with a zipapp rebuild per call (183 ms) | file reads      |
| health, backups                                          | `/var/lib/agent-bus-applied/health/<t>.json`                                                             | in-process (already Go)                       | stamp group            | none                                                       | none            |
| held-back LXC members                                    | `/var/lib/agent-bus-applied/lxc/<t>.json`                                                                | in-process (already Go)                       | stamp group            | none                                                       | none            |
| homeserver unit state                                    | systemd                                                                                                  | one `systemctl show` for every unit, or D-Bus | the user               | 2-3 `systemctl` runs                                       | one call        |
| a checkout's seats, held or free                         | `<checkout>/.claude/ccy/pingbus/seats/<t>/<s>/member.json` + a `flock` probe of `seat.lock`              | `bus/seat`, in-process                        | the user               | 1 Python start per checkout (~2.8 s for 6)                 | file reads      |
| members, roles, parked, room membership                  | registry + homeserver room state                                                                         | `agent-busd`, live                            | `agent-bus`            | sudo->bash->runuser->Python + HTTP per team (~2 s for 3)   | one request     |
| a person's Element sessions                              | homeserver admin API                                                                                     | `agent-busd`, polled                          | `agent-bus`            | the same per person (~3.5 s for 6)                         | in that request |

So one open is: the YAML parse, a handful of file reads, one systemd call, the git and tmux calls
the board already makes, and one request to the daemon.

### 4.2 The view request

`agent-busd` serves `/run/agent-bus/view.sock` (a `SOCK_STREAM` Unix socket). One request, one
JSON answer: for every team the daemon serves, its members (handle, role, membership, parked), its
humans (membership, device count, newest last-seen time), the daemon's freshness for each source
(`observed_at` of the last room sync and device poll, and the last error), and the daemon's own
version. Device IDs are not in it: `ccy-teams` uses only the count [G 2], so the view gives
less than today's `human devices` does.

**Who may ask.** The daemon reads the caller's uid with `SO_PEERCRED` and answers only root and the
owner's uid, recorded by the play in `/etc/agent-bus/owner` beside today's
`/etc/agent-bus/stamp-group`. Anyone else is refused and the refusal is logged. The socket file
itself may be world-connectable; the peer check is the gate, and it cannot be bypassed by a
symlink or a mode change. This replaces the NOPASSWD sudoers rule, which already gave every process
of the owner's uid exactly these facts (plus device IDs); the view gives the same uid less, gives
no other uid anything, and needs no sudo. A ccy container never sees the socket (it is not
mounted), so a session in a container gains nothing; a `cc` session on the host runs as the owner
and can read it, as it can run the sudoers-permitted commands today.

**Freshness.** Membership is event-driven (sub-second). Devices are polled every 20 s and on demand:
the view request takes `refresh=devices`, which the login flow sends right after a sign-in or a
new password. The answer carries `observed_at`, so the board can say a fact is old instead of
showing it as current.

**Failure.** A daemon that does not answer is reported on each team row ("agent-busd is not running:
members unknown") and on stderr, and `ccy-teams` exits non-zero for `--list`; it never falls back
to sudo or to Python. An answer older than its bound (the daemon could not reach a homeserver)
marks that team's members stale, with the daemon's last error.

### 4.3 Seat flows and choose

`seat choose` and `seat take` in v1 run a nested `sudo -n agent-bus list` from inside Python
[G 6]. In v2 they are `bus/seat` functions that take the team's roster from the same view
request, in-process in `ccy-teams`, and as the `agent-bus seat ...` CLI for the bash launcher until
the launcher calls Go.

### 4.4 The latency target

**`ccy-teams` (and `--list`) open in about 1 s or less** at six checkouts, three teams and six
people, and a redraw in under 200 ms, measured on the host (H1, H10). The budget, from v1's
measured parts \[G 5\]:

| Part                                            | v2 estimate |
| ----------------------------------------------- | ----------- |
| process start (Go)                              | ~5 ms       |
| host_vars parse + stamps + health + LXC records | ~5 ms       |
| systemd unit state, one call                    | ~20 ms      |
| git and tmux (as the board already runs)        | ~100-150 ms |
| seats of six checkouts (files and lock probes)  | ~5 ms       |
| one view request                                | ~2 ms       |
| drawing                                         | as today    |

The estimate is well under the target; the target is set at 1 s so that the git, tmux and podman
probes, which this plan does not rework, have room. Each stage of section 10 measures the open
before and after.

---

## 5. On-disk state, schemas and versions

### 5.1 The rule

Every file the bus writes has a version field (`v`), or is a fixed single-value format. Each
reader accepts a range of versions and refuses any other with exit 78 naming the file; unknown
keys and duplicate keys are refused (v1's strict decoding, [H 7.09], [P 2]). **During coexistence
a writer writes the version the oldest live reader accepts**, so a v1 tool and a v2 tool can take
turns on the same file; a version is bumped only after the last v1 reader of that file is retired
(section 10, stage 8), and the bump lands with its decoder and fixture.

### 5.2 Files kept as they are

All of these keep their path, owner, mode, schema and version through the migration (schemas in
[H 2], [P 2], [L 5]):

- host: `team.json`, `registry.json` (v2) and `registry.lock`, `room_id`, `secrets/*`,
  `tuwunel.toml`, root's records and state tars under `/var/lib/agent-bus-install/`, applied stamps
  (v1), health records (v1), LXC member records (v1), `/etc/agent-bus/stamp-group`;
- checkout seats: the bundle (`member.json` protocol 1, `token`, `README`), `seat.lock` (`seat <claim_ms>`), `said-goodbye.json` (v1), `state/` (`sync.json`, `inbox/`, `consumed/`,
  `outbox.json`, `team.json`, `about.json`, `room.json`, `untrusted.json`, `forge-cache.json`,
  `dropped.log`, `lock`);
- session: the desired and applied membership (v1), `conversation.json` (v1), the session home's
  `applied.json`, `active`, `socket.json`, `keeper.lock`;
- locks: the allocation and change locks under `$XDG_RUNTIME_DIR/ccy/`, the play lock.

Keeping the seat directory's `state/` exactly is what lets a seat be used by a v1 session today
and a v2 session tomorrow, and back (rollback, section 10.3).

### 5.3 New files

| Path                               | Owner / mode                  | Schema                                                                                       | Why                                                      |
| ---------------------------------- | ----------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| `/run/agent-bus/view.sock`         | agent-bus, 0666 in a 0755 dir | request/answer JSON, `{"v":1,...}`                                                           | the read model (4.2)                                     |
| `/run/agent-bus/admin.sock`        | agent-bus, 0666 in a 0755 dir | request/answer JSON, `{"v":1,...}`; a bundle answer carries the three files as base64 fields | admin changes (6.2)                                      |
| `/etc/agent-bus/owner`             | root:root 0644                | one uid + newline                                                                            | who the view answers, written by the play                |
| `/var/lib/agent-bus/<t>/admin.log` | agent-bus 0600                | append-only JSON lines `{"v":1,"at","peer_uid","op","handle","result"}`                      | an audit of every admin change (the broker's log, 00197) |

`/run/agent-bus` is the unit's `RuntimeDirectory=`, so a stale socket never outlives the daemon.

### 5.4 The fixture corpus both implementations test against

`tests/fixtures/agent-bus-state/` holds one directory per file kind, each with `valid/` and
`invalid/` examples and a `cases.json` naming what each must decode to or which error it must
raise (by class: `version`, `unknown-key`, `duplicate-key`, `type`, `grammar`, `size`). The
Python helper tests and the Go `bus/state` tests both walk it, so v1 and v2 cannot disagree on a
file without one of them failing. The same holds for the protocol:
`tests/fixtures/agent-bus-protocol/` holds event JSON with the expected verdict (accepted with
the parsed fields, ignored with its reason, dropped with its reason) and, for the envelope, the
input bodies with the expected rendered bytes. Both test suites consume it; neither is the other's
oracle, and a case is added from the protocol spec or a found bug, never by recording one
implementation's output.

---

## 6. Privilege: what needs root, and the least way

### 6.1 Who does what

| Operation                                                            | v1                                           | v2                                                                                    | Why that privilege                                                              |
| -------------------------------------------------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| install software, team units, firewall, NetworkManager, restore      | root (installer)                             | root (`agent-bus-install`)                                                            | writes `/etc`, `/usr/local`, firewalld, other users' directories                |
| health check (probe, restart a homeserver, read root's backup tars)  | root timer, sandboxed with two capabilities  | the same, run as `agent-bus-install health-check`                                     | restarts a system unit; reads root's 0700 records                               |
| read members, roles, room membership, devices                        | root by NOPASSWD sudo, then `agent-bus`      | the owner's uid, by peer check on the daemon's view socket                            | the facts live behind the admin token; the daemon holds it, the owner only asks |
| add, park, remove a member; set a role; human password, lock, logout | root by sudo with password, then `agent-bus` | root by sudo with password (`sudo agent-bus ...`), as a root peer of the admin socket | the owner's consent at the time (R13); the admin token stays with `agent-bus`   |
| place a bundle in a checkout                                         | the user, from a tar root re-packed          | the user, from the answer `sudo agent-bus add-member --out=-` prints                  | root never writes under a user's path (R12)                                     |
| a checkout's seats                                                   | the user                                     | the user, in-process                                                                  | the files are the user's                                                        |
| LXC member apply                                                     | root (play, `members.py`)                    | root (`agent-bus-install lxc-members`)                                                | reads container rootfs, runs `lxc-attach`                                       |
| homeserver                                                           | `agent-bus`                                  | unchanged                                                                             |                                                                                 |

### 6.2 The admin socket and sudo

`sudo agent-bus <admin command>` runs the Go CLI as root; the CLI connects to
`/run/agent-bus/admin.sock`, and the daemon accepts an admin request only from a peer whose uid is
0\. So the owner's password (or a cached credential) is still what authorises a change, exactly as
today, but root's part shrinks to one socket connection: no `runuser`, no bash, no tar unpacked by
root. For `add-member --out=-` the daemon answers with the three bundle files as typed fields, and
the CLI prints them on stdout for the user-side seat code, which places them by fd. For
`--out=DIR` the CLI writes as the user who ran sudo, in a child started with that user's uid and gid
and no supplementary groups (v1's `setpriv` invariant, D61).

**Why not let the owner's uid write without sudo.** A `cc --teams` session runs as the owner; a
socket that authorised changes by uid would let any agent in such a session add members or change
roles with no one asked. The password is the only presence check the host has (D3).

### 6.3 The sudoers change

The NOPASSWD rule `/etc/sudoers.d/agent-bus-read-only` is **removed** once ccy-teams reads the view
(stage 4). After that no sudoers rule mentions the bus: reads need no privilege beyond the peer
check, and every change needs the password. Least-privilege argument:

- the rule granted root (dropping to `agent-bus`) for two argv shapes; its removal takes away the
  only passwordless route to root that the bus had;
- the replacement grants the owner's uid one read of facts it could already read, minus device IDs,
  and grants it nothing it could change;
- a bug in the view request's parser runs as `agent-bus`, never as root, whereas a bug in the
  wrapper reached by the sudoers rule ran as root first.

The rule stays installed through stage 4's acceptance so that a rollback to the previous ccy-teams
works; it is removed in the commit after the owner accepts stage 4.

### 6.4 Could a group-readable file avoid the daemon?

Yes, for reads, and it is weighed in D1: the daemon (or a timer) writes
`/var/lib/agent-bus-applied/live/<t>.json`, 0640 to the stamp group, as the health record is
written today, and ccy-teams reads it like the stamps. It needs no socket and no peer check.
Against it: freshness is then the writer's interval, not an event; the login flow cannot ask for a
refresh; and the file's group is the desktop user's primary group, so every file the user's
primary group can read is the access boundary, which is broader than one uid. The socket is
recommended; the file is a sound fallback if the owner prefers no listening socket.

---

## 7. Concurrency and locking

- **Registry and homeserver admin**: one writer, `agent-busd`, one goroutine per team serialising
  that team's changes. During coexistence (stages 4-5) the Python admin tool still runs for the
  changes not yet moved, so the daemon takes the same `registry.lock` flock for each change and
  re-reads the registry on every change of the file (inotify), never caching it across a write it
  did not make.
- **Installer**: v1's installer takes no lock (B16). v2's takes one host lock,
  `/run/agent-bus/install.lock`, for every mutating command (software, team, remove, backup-now,
  restore, lxc-members, health-check's restart), with a bounded wait that names the holder, so the
  backup timer and a play can no longer interleave (00192 spec D4).
- **The daemon and the installer**: a team's restart or restore makes its homeserver vanish; the
  daemon treats that as an outage (marks the team, retries with backoff), never as a reason to
  exit. The installer tells the daemon to reload a team's files after `team`, `restore` and
  `remove` by one admin request (`reload <team>`), so no inotify on root-owned files is needed.
- **Seats**: the allocation lock, change lock and seat locks keep v1's places and semantics (all
  `flock`, all out of a container's reach except the seat lock, which a session must hold) [L 3.3].
- **Member side**: v1 has seven locks [P 4]. v2 keeps the seat lock and the per-team sync lock
  (both cross-process and shared with v1 sessions of the same seat) and replaces the spool,
  watch, keeper, told and outbox locks with ownership inside one process tree (8.2): the keeper
  owns the session's state and the worker is its only child that writes it.
- **Play lock**: unchanged; held by ccy-teams across the host_vars write and the play (R34).

---

## 8. The member client, pingbus v2

### 8.1 What stays

The CLI surface and its contract: every command of [P 1], its output lines (fields only ever
appended) and its stable exit codes (protocol §13-§15). Agents and the skill call these; another
harness relies on them (00194 decision A). The bundle, the env file, `PINGBUS_*` variables and the
plugin's hooks stay, so a session cannot tell v1 and v2 apart except by speed.

### 8.2 One resident process tree per session

v1 runs, per session: a seat keeper, a watch supervisor, a watch child, a short-lived Python per
hook, a Python per status-line refresh, and a hook-driven revive path when the watcher dies
[P 4]. The owner rejected the fallback chain once already and asked for the root cause [L 6.12].
v2 has two processes:

- **the keeper** (`pingbus seat keep`, started by `seat exec` before Claude Code, or by
  `agent-bus-claude` for a member without seats): holds the seat locks and the per-team sync locks,
  follows the desired membership, dies with its session (`PR_SET_PDEATHSIG`), and supervises the
  worker (restart with backoff, a status mark when it keeps failing). It makes no network calls,
  so it has nothing that fails at run time but its own file reads;
- **the worker**: one goroutine per active team for the sync loop, one delivery loop, the
  membership notices, the `listening` status. Its state is in memory, written through to the v1
  files in `state/` (5.2) so recv, status and a v1 session see the same thing.

Hooks and the status line become Go calls that read the keeper's state file (or ask the keeper over
a socket in the session home) and answer in milliseconds: no Python, no import cost, and the
SessionEnd goodbye fits Claude Code's default 1.5 s with room to spare (the 8 s setting stays, as a
margin). `recv`, `wait` and `history` stay for sessions without a socket and for agents that ask.

### 8.3 Delivery

One path: the envelope written to Claude Code's inbox socket (00194's amended decision B), with
every rule of SPEC-inject E1-E26 kept as expectations: re-fetch, delivered once, nonce, the
allow-list renderer, flood budget, held-back line, stall and missing marks, transcript proof,
receipts. Relay mode (the spool for a supervisor plugin that has not shipped) is not built in v2
(D4); when the hooks-daemon plugin API it waits for ships, the supervisor path is specified against
v2 as its own stage.

The envelope's allow-list renderer classifies characters by Unicode block and category. Go's
`unicode` tables and Python's `unicodedata` follow different Unicode versions, so the two
renderers can differ on new characters; the corpus (5.4) carries the rendered bytes for every
listed block and for characters newer than either table, and v2 refuses (escapes) any character
its own table does not know, which is the safe direction.

### 8.4 No Python in members

The kit ships one static `pingbus` (`CGO_ENABLED=0`). Containers, LXC guests, docker and VM
members no longer need Python 3.11 for the bus; the LXC kit sync stops sending Python into the
container (8.5). One build per architecture the host runs; amd64 only until a member on another
architecture exists (D8).

### 8.5 LXC guest operations

v1 runs `python3 -I -c <script>` inside each container as root and as the agent user, for kit
sync, bundle placement, env merge and the service unit [L 1.3]. v2 copies the kit's static
`pingbus` into the container once (streamed on stdin to `lxc-attach -- install -m 0755 /dev/stdin <path>.new` then renamed, which needs only coreutils), then runs `pingbus guest <op>` for each step,
with v1's guards as expectations: its own umask first and every leaf mode named [L 6.02], no-follow
opens, `O_EXCL` placement, the managed env lines only. Reading the checkout stays on the host side,
as root, from the container's rootfs, never by running container code (R33).

---

## 9. The homeserver interaction

- **One transport** (`bus/matrix`) for the admin side and the member side: no proxy, no
  redirects, the token only in an unredirected `Authorization` header, a request timeout, a body
  cap, errors carrying method, path, status and errcode only [P 3, H 3]. The admin side refuses any
  base URL but `http://127.0.0.1:<port>`.
- **Admin session**: the daemon loads `admin.token`, checks it with `whoami` once per team when it
  starts and after any 401, not before every command as v1's per-process tool must.
- **Room sync for the view**: a filtered `/sync` on the admin token, state types
  `m.room.member`, `agent_bus.team`, `m.room.topic`, timeline empty; first sync records the
  current state, later syncs apply membership events. On a limited answer or a reconnection it
  re-reads `/rooms/<id>/state`.
- **Devices**: `GET /_synapse/admin/v2/users/<id>/devices` per human every 20 s and on request.
- **Every Tuwunel quirk v1 found is an expectation** of `bus/admin` and a switch in `fakehs`: no
  `admin` key on account creation (500), password reset with logout as the only revoke, one server
  admin, config keys bare, `max_request_size` floor, PID-1 signal drop before ready, SIGUSR2 backup,
  login buckets in memory [H 3].
- **The member sync loop** keeps v1's filter, first-sync and post-join rules, gap fill, trust
  re-verification, backoff, and the `listening` status cadence [P 3]. In a long-running worker the
  room's verified state is kept in memory, so `send` no longer re-verifies with three GETs per
  message when the worker is running (it asks the worker); a send with no worker verifies as v1 does.

---

## 10. Migration: v1 and v2 side by side

### 10.1 Principles

- **The wire protocol stays version 1** until every member is v2 and a change has a reason
  (D5): members on other machines may run v1 for a long time.
- **State files stay readable by both** (5.1); a version bump waits for stage 8.
- **One component at a time**, each a deploy of its play and its own acceptance, so a regression
  points at one change.
- **No fallbacks between v1 and v2 at run time.** A stage switches a caller from v1 to v2; it does
  not try v2 and fall back to v1 (fail fast). Rollback is a redeploy of the previous commit.

### 10.2 Cut-over order

| Stage | What moves to v2                                                                                                                                        | v1 left running                                                          | What the owner sees                                             |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | --------------------------------------------------------------- |
| 1     | `bus/proto`, `bus/state` and the two fixture corpora; Python tests read the corpora too                                                                 | everything                                                               | nothing                                                         |
| 2     | the installer (00192 Task 3.2's Go port, merged after its host test), with the install lock; health check in Go                                         | admin tool, pingbus, LXC helper, declare                                 | nothing, or the port's numbered departures                      |
| 3     | `ccy-teams` in-process reads: `bus/declare` (read side), stamps with the Go software digest, seats; one systemd call                                    | `sudo list` / `human devices`, declare's write side, seat take/remove    | open drops by the 7 + C starts (about 6 s at the owner's scale) |
| 4     | `agent-busd` serving the view; `ccy-teams` reads members and devices from it; then the sudoers rule removed                                             | admin changes (Python, through the wrapper)                              | open about 1 s; no sudo at open                                 |
| 5     | admin changes in the daemon; the Go `agent-bus` CLI replaces the bash wrapper; `bus/declare` write side; seat take/remove in Go                         | pingbus, LXC helper                                                      | nothing but speed                                               |
| 6     | pingbus hooks and status line in Go (reading v1 files), then keeper and worker, then the remaining CLI commands                                         | v1 pingbus in sessions launched before the deploy, and in remote members | a new session uses v2; a running one keeps v1 until it restarts |
| 7     | LXC member apply in Go, guest ops through `pingbus guest`                                                                                               | v1 pingbus inside containers until the kit sync replaces it              | containers need no Python for the bus                           |
| 8     | retire v1: delete `helpers/agent_bus`, `helpers/pingbus`, `helpers/agent_bus_lxc`, the zipapps and their build, the bash wrapper; version bumps allowed | v1 pingbus in remote members (protocol 1 still served)                   | one language                                                    |

Stage 3 needs a new definition of the software digest in the applied stamp: v1 hashes the
`agent-bus.pyz` it rebuilds on every overview and the checkout's installer file [H 6, H 8.03].
v2's digest is the source digest each deployed Go binary is built with (Plan 00206's version),
read from the deployed binaries, and the stamp's `software` field is recomputed from it; the
installer port's own D1 already chose this. The first stage-3 deploy therefore marks every team
"out of date" once, and an apply restamps it (H6).

### 10.3 Rollback

- Stages 1-3 and 6-7 change no state format: rolling back is redeploying the previous commit.
- Stage 4: the sudoers rule stays until the owner accepts the stage; rolling back redeploys the
  previous `ccy-teams` and stops the daemon's unit.
- Stage 5: the daemon and the Python tool take the same registry lock and write the same registry
  version, so redeploying the previous commit restores the Python path with the daemon's changes
  intact; the audit log is simply not written by v1.
- Stage 6: a seat's `state/` is v1's; a session restarted on v1 picks it up. A drill (H9) proves
  it before stage 6 is accepted.
- Stage 8 is the one-way step and waits for every earlier stage to be accepted and to have run
  without a rollback for the period the owner sets.

---

## 11. Test strategy

- **Spec first, per stage.** Each stage has a `SPEC-<component>.md` in this plan with numbered
  expectations (E1...), the v1 requirement each one comes from, and departures (B1...) where v1
  was wrong, as `src-go/CLAUDE.md` "How a port is built" says. The v1 code is read for
  requirements and never recorded as an oracle.
- **Fakes**: `fakehs` (the homeserver, with Tuwunel's quirks as switches and fault injection:
  refused connection, 429 with `Retry-After`, a limited timeline, a restart mid-sync); a fake clock;
  `execx.Fake` for systemctl, lxc and git; real temporary directories with real `flock` for every
  lock rule (a lock is only proven by a second process, so lock tests start one); a fake Claude
  Code inbox socket and transcript for delivery.
- **Shared corpora** (5.4) run by both the Python and the Go tests until stage 8.
- **The real homeserver**: Go tests behind a build tag run against the pinned Tuwunel binary when
  it is present (the host, the VM lab), covering bootstrap, members, the view's room sync and the
  member sync loop; the VM lab gets an end-to-end scenario (a team, two members, a goodbye, a
  disconnect, a restore).
- **Latency**: a test pins the number of processes an open starts (zero bus processes from stage 4)
  and a host check measures wall time (H1, H10).
- **The bash suites** that drive ccy-teams (`scripts/test-ccy-teams-command.bash`) keep running;
  a behaviour they pin that v2 changes is changed in the suite in the same commit, with the reason.

---

## 12. What v1 does that v2 does not carry

| v1                                                                                                                  | Why not in v2                                                                                              |
| ------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| A process per read, serial, and the caches built to hide it (`Collector.devices`, `listCache`, the describer) [G 6] | reads are in-process or one request                                                                        |
| `declare overview` three times per open, and a zipapp rebuild in each to hash it [G 1d]                             | one parse per open; the digest is the binary's source digest                                               |
| The NOPASSWD sudoers rule and the bash root wrapper with `runuser`, tar unpack and `setpriv` [H 5]                  | the view needs no root; a change is one root socket connection; the user places the bundle                 |
| `declare.py`'s hand-written YAML-subset line editor [H 8.02]                                                        | a Go YAML node editor over the real document, with comment and vault-tag preservation as expectations (D6) |
| ccy-teams loading `helpers/agent_bus/declare.py` from the checkout (Plan 00206 R1)                                  | linked into the deployed binary                                                                            |
| Python strings run in containers, and the Python 3.11 prerequisite in every member [L 7.01]                         | one static binary in the kit                                                                               |
| `render`, `stamp-group`, `bridge.py`, `config_restart`, `timer_state` as separate processes [H 8.01]                | functions of the installer                                                                                 |
| The watch supervisor, watch child, hook revive and waiter chain [P 4, L 6.12]                                       | one keeper supervising one worker                                                                          |
| Relay mode: spool, `relayed/`, `sent.jsonl`, `sent-offset`, the `relay=` field [P 6]                                | built for a plugin API that has not shipped (D4)                                                           |
| Two body caps, two goodbye records, duplicated constants and regexes [P 6]                                          | one of each, in `bus/proto` and `bus/state`                                                                |
| The status line re-implementing membership logic to save import time [P 6]                                          | Go start-up removes the reason                                                                             |
| `verify_room` (three GETs) on every send [P 6]                                                                      | the worker's verified state                                                                                |
| An installer with no lock [H 8.08]                                                                                  | the install lock (7)                                                                                       |
| The test-prefix hook in the production installer [H 8.05]                                                           | fakes and build tags                                                                                       |
| Root running the checkout's installer in place [H 8.04]                                                             | the play installs a root-owned binary and runs that                                                        |
| Legacy forms: role `orchestrator`, registry v1, container-name LXC seats [H 8.13, L 7.03]                           | read until stage 8, never written; then dropped with a one-off migration (D9)                              |
| A separate LXC record writer kept to avoid a ccy image bump [L 7.05]                                                | one writer in `bus/state`                                                                                  |
| Constant `TOOL_VERSION = "0.1.0"` [P 6]                                                                             | every binary reports its source digest                                                                     |

What v2 keeps on purpose, though it looks like overhead: the strict decoders, check-everything
before changing anything, demote before promote, handle stability, locks out of a container's
reach, record before desired file, the inbox as an untrusted cache, and the SessionEnd timeout
setting [L 7.10].

---

## 13. Observability

- Every binary answers `--version` with its source digest; `agent-busd`'s view answer carries it,
  so the board can say a daemon is older than the ccy-teams asking.
- `agent-busd` logs to the journal, one line per event (team connected, outage start and end, a
  change with its peer uid and result), never a token, password or message text.
- `agent-bus status` (as the owner) prints the view: each team, the daemon's last room sync and
  device poll, and the last error, with exit 1 when a team is stale or unreachable.
- The admin audit log (5.3) records every change.
- The member side keeps `watch.log`, the status-line marks (`(down)`, `(role?)`, keeper stopped)
  and `pingbus status`.

---

## 14. Owner decisions

| #   | Question                                                                                                              | Options                                                                                                                                                                                 | Recommendation and why                                                                                                                                                         |
| --- | --------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| D1  | How does ccy-teams read members and devices without Python?                                                           | (a) `agent-busd` view socket, peer-uid check; (b) a group-readable live file written by the daemon or a timer; (c) no daemon: one Go helper under sudo that reads every team in one run | (a): event-fresh, no sudo, no group widening, and the daemon is needed for D2's single writer anyway. (c) reaches about 1 s too but keeps a NOPASSWD rule and root on the path |
| D2  | One daemon for the host or one per team?                                                                              | one / per team                                                                                                                                                                          | one: the admin side is small, a per-team daemon multiplies units and sockets, and the message path does not go through it                                                      |
| D3  | Do admin changes still need the sudo password?                                                                        | yes, root peer only / the owner's uid without a password                                                                                                                                | yes: a `cc --teams` session runs as the owner's uid                                                                                                                            |
| D4  | Relay mode (Plan 00194 Task 2.1's spool for a supervisor plugin)?                                                     | drop in v2 and re-specify when the upstream API ships / port it                                                                                                                         | drop: it waits on an unreleased API and is not used; it doubles the delivery code                                                                                              |
| D5  | The wire protocol?                                                                                                    | keep version 1 through and after the migration / plan a protocol 2                                                                                                                      | keep 1: members on other machines run v1; a protocol 2 needs a requirement first                                                                                               |
| D6  | Where do team declarations live?                                                                                      | host_vars, edited by a Go YAML node editor / a v2 store of its own that the play reads                                                                                                  | host_vars: the play stays the one route (IaC rule); needs `go.yaml.in/yaml/v3` in the main module (an allowlist row)                                                           |
| D7  | LXC members?                                                                                                          | port the play-driven apply to Go now / build a reconciler on container start now / wait for the 00197 broker                                                                            | port now (stage 7) with the same play-driven model; a reconciler and the broker build on `agent-busd` later                                                                    |
| D8  | Member binaries: static amd64 only?                                                                                   | amd64 only / amd64 + arm64                                                                                                                                                              | amd64 until a member on another architecture exists                                                                                                                            |
| D9  | Legacy forms (`orchestrator`, registry v1, container-name LXC seats) at retirement?                                   | drop with a one-off migration / keep reading for ever                                                                                                                                   | drop at stage 8, after a migration run that rewrites them                                                                                                                      |
| D10 | 00192 Task 3.2's Go installer?                                                                                        | v2 adopts it as its installer (merge after the owner's root test) / v2 rewrites it                                                                                                      | adopt: it is built, reviewed and spec'd from requirements; v2 adds the lock, the daemon unit, the health check and LXC apply                                                   |
| D11 | Plan 00197's seat broker?                                                                                             | its own plan on `agent-busd` after stage 5 / inside this plan                                                                                                                           | its own plan: its approval flow and network listener are a separate trust question                                                                                             |
| D12 | The admin commands no program calls (`human logout-all`, `lock`, `unlock`, `rotate-token`, `remove-member`, `check`)? | keep / drop                                                                                                                                                                             | keep: they are the documented lost-phone and retire procedures                                                                                                                 |
| D13 | Is the 1 s open the acceptance bar, at what scale?                                                                    | 1 s at six checkouts, three teams, six people / another bar                                                                                                                             | 1 s at that scale, measured with `--list` and the interactive open                                                                                                             |

---

## 15. Risks

| Risk                                                                             | Mitigation                                                                                                                                        |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| v1 and v2 members disagree on a protocol rule and one drops what the other sends | the protocol corpus, run by both test suites; protocol 1 unchanged (D5)                                                                           |
| The envelope renders differently (Unicode table versions)                        | rendered bytes in the corpus; unknown characters escaped (8.3)                                                                                    |
| A long-running daemon is a new target as `agent-bus`                             | systemd sandbox as the homeserver's (no network but loopback, no new privileges, read-only system); peer checks; no listener but two Unix sockets |
| The daemon is down and the owner cannot see members                              | said on the board and by `agent-bus status`; `Restart=always`; members unaffected                                                                 |
| The Go YAML editor loses comments, ordering or vault tags in host_vars           | expectations over real-shaped host_vars fixtures (comments, `!vault`, anchors), a backup before every write as v1 does                            |
| Coexistence lasts long and doubles maintenance                                   | stages are small and each retires its v1 part; stage 8 is in the plan, not deferred                                                               |
| A lock rule lost in the rewrite (the class v1 found most bugs in)                | each lock rule is an expectation proven with a second real process                                                                                |
| The software digest change marks every team out of date once                     | said in the stage-3 release note; one apply restamps (H6)                                                                                         |
| Scope: this is the largest single change to the bus                              | the owner's decisions first; each stage independently useful (stage 3 alone removes most of the open's cost)                                      |

---

## 16. Host checks

Each is run on the host by the owner through `meta-deploy.bash` or by hand where noted, with the
result recorded in the journal.

- H1. Baseline: wall time of `ccy-teams --list` and of an interactive open, with the counts of
  checkouts, teams and people, before stage 3.
- H2. The per-start cost on the host: `time /usr/bin/python3 -I /usr/local/lib/agent-bus/agent-bus.pyz version`.
- H3. `SO_PEERCRED` through sudo: a root CLI started by `sudo` is seen by the daemon as uid 0;
  the owner's plain process as the owner's uid.
- H4. The admin account's `/sync` on Tuwunel 1.9.4 delivers `m.room.member` changes for the team
  room, and how soon.
- H5. Device poll cost: the admin devices call for each human of every team, timed.
- H6. After stage 3's deploy every team reads "out of date" once, and `ccy-teams apply --all`
  makes them current.
- H7. A host `flock` probe of a `seat.lock` held inside a running ccy container reports it held.
- H8. The static `pingbus` runs in the ccy image, in each LXC container, and in a docker member.
- H9. Rollback drill: a seat used by a v2 session, then by a v1 session, then by v2 again, with
  its inbox and history intact.
- H10. After each stage: the H1 timings again.

---

## 17. Departures from v1 already chosen by this design

Numbered for the stage specs to carry; each needs no owner decision beyond section 14.

- B1. Reads of members and devices need no sudo (stage 4).
- B2. `human devices` is no longer what ccy-teams reads; the view carries a count and the newest
  last-seen time. The command stays for a human.
- B3. The installer refuses to run while another installer command holds the install lock, naming
  it, after a bounded wait.
- B4. The applied stamp's software digest is the deployed binaries' source digest.
- B5. `send` with a running worker does not re-verify the room by itself.
- B6. A failed daemon read is an error on the board, never a fallback.
