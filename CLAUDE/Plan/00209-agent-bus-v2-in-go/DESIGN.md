# Plan 00209 design: agent bus v2, rebuilt in Go

**Status**: proposed, revised once after Fable's review
([subagent-reports/261019-v2-design-review-fable.md](subagent-reports/261019-v2-design-review-fable.md),
verdict REWORK); for the owner's decisions (section 14). Nothing here is built.

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

**Opening `ccy-teams` takes 9-23 s.** Measured on the host by the coordinator (H1's first
reading): five runs of `ccy-teams --list` took 14.5, 23.0, 14.1, 10.6 and 9.3 s, at a load
average of 30-39 on 22 cores, with six seat lists and eight sudo calls (`list` per team, `human devices` per person),
each Python start paying about 0.45 s of imports. The load inflates the spread; H1 is repeated
at an ordinary load, with the interactive open too, before the first stage that claims a saving
(stage 1 of section 10). Every bus read is a separate Python process, run one after
another [G 0, 1d]. An open starts Python `7 + C + T + H` times (C checkouts with seats, T teams
that are up, H people in them): seven `declare.py` runs (`overview` three times), one
`agent-bus seat list` per checkout, and through `sudo -n` -> bash -> `runuser` -> Python one
`agent-bus list` per team and one `human devices` per person. Each start costs about 0.45 s, and
the cost is the imports, not the interpreter (bare `python3` 33 ms, `import helpers.agent_bus.checkout` 420 ms [G 0]); from a zipapp nothing is cached as bytecode. With six
checkouts, three teams and six people that is 22 starts, about 10 s, before sudo, PAM and HTTP.
Every redraw pays about 10 more [G 1d].

**Every running session pays it too.** When this design was written the status line ran
`pingbus status-line` every second (`refreshInterval: 1`; 25 s since Plan 00213 Task 1.3, D16),
and every prompt, every turn end and every SessionEnd starts a Python
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

v2's aim, in one line: **one Go codebase for every part of the bus, every read ccy-teams makes
done in-process or by one short-lived reader for all teams, no root on any read, and no Python on
any hot path**, with every requirement and every hard-won behaviour of v1 kept, and v1 and v2
able to run side by side until v1 is retired.

**Where the optimisation stops, and why.** v2 is a new *process model* and a new *privilege
model* on v1's *data model*. New: reads in-process, one reader for every team instead of a
process per team and per person, no root on the read path, one keeper and one worker per
session instead of six process kinds, a typed bundle hand-over instead of a tar unpacked by
root in bash, one lock for the installer, one static binary in members. Unchanged, on purpose:
every file format and its version, every lock file and its meaning, the CLI contract, the wire
protocol, the file-per-event inbox with full re-validation, the three receive drivers
(`recv`, `wait`, and the worker that replaces `watch`), the host_vars as the declaration store and
the play-driven LXC model. They stay because v1 and v2 must share one checkout's seats, one
`PINGBUS_HOME` and one registry for the length of the migration (section 10); a resident worker
could hold the inbox in memory, for instance, but a v1 `recv` beside it reads the files. The data
model is open to change only after stage 8, each change with its own requirement.

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
  The check's unit keeps its sandbox: `CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_CHOWN` and
  `ReadWritePaths` the applied directory only, whatever binary its `ExecStart` names.
- R8. An applied stamp per team (config and software digests) so `ccy-teams` can say up to date,
  out of date, not built or decide [H 6, D65].
- R40. Admin bootstrap \[H 3\]: exactly one server admin, asserted on every apply; the admin
  password stored before the admin account is registered and deleted only after its token is
  stored (the recovery path for a run that dies between them); a name in `removed_humans` can
  never return; a human is removed before the record is rewritten; a stray `agent_bus.status`
  state event (under a key that is no user ID) is emptied
  (`helpers/agent_bus/admin.py:457-506,603-614`).
- R41. A team's `description` reaches members only as the room's `m.room.topic`, never as a team
  record key: an unknown record key makes an older member distrust the room (MT S-A8). This is a
  protocol invariant, carried in `bus/proto` and the protocol corpus
  (`helpers/agent_bus/admin.py:552,597-599`).

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
- R42. Outbox, acks and TIMEOUT \[P 5.14-5.16\]: a ping that expects an answer is recorded in
  `outbox.json`; each target that stays silent past the deadline gets exactly one TIMEOUT, keyed
  `<event_id>~<localpart>`, marked `reported` once delivered (`helpers/pingbus/cli.py:1222-1227`);
  an ack or nack counts by its `origin_server_ts` against the deadline, even when it is read late
  or dropped as stale; the send gate is charged before the network call, inside the outbox lock
  and with no network work under it (`helpers/pingbus/inbox.py:560-571`), and the 60 s duplicate
  window is persisted in the outbox's `gate`.
- R43. Member-side token handling \[P 2a, 2c\]: the member token is re-read and re-checked (owned
  by the user, mode 0600 or tighter, one printable line) on every use and never held on the
  member object; two bundles sharing a user ID or a token are refused (exit 78); `say` refuses
  text containing the member's own token (`helpers/pingbus/cli.py:1062`); the session's
  `socket.json` holds the inbox socket token at rest, 0600.
- R44. The forge credential in a long-running process: v1 runs `gh auth token` each time it
  builds a forge client (`helpers/pingbus/syncer.py:163-172`, `forge.py:112-136`), which a
  per-command process makes cheap to get right. The worker re-reads it when a check needs it and
  the held copy is older than a bound the stage spec sets (minutes, not the session), and at once
  after a 401; it is never written to disk or logged. The repository-reach TTL of 3600 s stays
  (`forge.py:64,388-392`): a repository moved out of the organisation stops being trusted within
  the hour, however long the worker runs.

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
- R26. The status line shows `👥 team (role) (down)` rows, refreshed at the kit's interval
  (`refreshInterval: 25` since Plan 00213 Task 1.3, D16), always exits 0, wraps the session's own status line
  [P 5.19], and keeps every rule of v1's (`helpers/pingbus/statusline.py`): the inner command
  runs through `/bin/sh -c` in its own process group, killed whole on its 10 s timeout or when
  the status line is cancelled, including a cancel that lands before the child's PID is known
  (`statusline.py:187-224`); it never runs itself (`PINGBUS_STATUS_LINE_INNER`, `:59,200`); rows
  wrap at `COLUMNS` after a comma and stay one row with no usable `COLUMNS` (`:16-18,340-342`); an
  unknown role shows `(role?)`; a `down/<team>` mark is honoured for 300 s (`:88`). It never
  opens the inbox, the member code or the network: it reads one file (8.2).
- R45. Hooks \[P 4\]: SIGINT and SIGHUP ignored while the goodbye is posted
  (`helpers/pingbus/hooks.py:701`, `cli.py:2611`); the goodbye posts to every team in parallel
  within a 4 s budget; `stop_hook_active` respected (`hooks.py:967,1011`); the fixed
  `inject_frame` explanation given once at SessionStart.

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
- R46. Conversation recording and restart: the SessionStart hook writes the session's
  `conversation.json` (`{v, session, session_id, source, written_ms}`,
  `helpers/pingbus/hooks.py:35,213`) beside its report, and `ccy-teams restart` resumes by it
  (`claude --resume <recorded id>`, never `--continue`, `src-go/internal/teams/restart/restart.go:93`)
  [L 3.5]. `relaunch.json` in the membership directory is the host's request that a session
  relaunch once (R32), written by ccy-teams (`src-go/internal/teams/membership/desired.go:20,123-131`)
  and read by the ccy supervisor; it is already Go and outside the bus binaries, but it is a kept
  file (5.2) and its reader is part of the coexistence tests.
- R47. Cross-process locks: every lock a v1 process takes on a member's files is taken by v2 at
  the same path, with the same holder kind and the same order, for as long as a v1 process can
  share those files, which is until stage 8 (section 7). For LXC and host members
  `PINGBUS_HOME` is `~/.config/pingbus`, shared by every session of that user, so a v1 and a v2
  session of one user share one `state/` from stage 2 until stage 8.

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
- R48. LXC detail (`helpers/agent_bus_lxc/members.py`) \[L 1\]: an entry with `service: true` gets
  a supervised session unit inside the container, its attach command, and a restart only when
  the unit's text changed (L 1.7, `members.py:21,353,390,489-493`); pre-checks 4 and 5 for every
  running container before any change (gh installed and logged in to github.com as the agent
  user, `:360,1001-1003`; the container's source address on a bridge inside the team's
  `allow_from`, `:13,819-821`); the `absent` phase runs before any team is removed (`:3`); the
  record sweep is scoped by `agent_bus_only_teams`; a member record's `since` is kept while its
  status and handle stay (`:730,743`).
- R49. A session runs one kit version from launch to exit. The host and LXC kits are replaced in
  place today (`/usr/local/share/agent-bus/kit`, L 1.3 step 08), so a session launched on one version
  runs the next version's hooks and status line after a play. v2 installs each kit under a
  versioned directory and a session resolves it once at launch (B9); the ccy image is already one
  version per container.

### 2.7 The owner's views (ccy-teams)

- R35. The board: each team (homeserver state, port, APPLIED, health, description), its seats
  (holder, restore), its LXC and other members (active, parked, deferred), its people (in the room,
  how many Element sessions) [G 1, docs/agent-bus.md].
- R36. `ccy-teams --list`, the picker, choose-teams, describe-seats, the login flow, settings,
  create, apply, lxc-add, session add/remove/move/role/repair, shutdown, restart, backup, restore,
  remove: every flow keeps its behaviour and wording unless a numbered departure changes it.
- R50. Element profiles: `play-agent-bus-element.yml` writes each `agent_bus_element_teams`
  entry's locked-down Element Desktop profile, seeds the spell checker off, grants the keyring,
  and writes a launcher, through `helpers.agent_bus.element check|apply` run as the desktop user
  (H 7.37, `helpers/agent_bus/element.py:1-23`, the play's lines 58 and 107); it refuses a user
  who holds pingbus bundles, as `pingbus config check` and `agent-bus-claude` refuse a `host`
  member beside an Element profile, after the `element-accepted` consent (Plan 00161 D26) [P 2a].
  This is R37's mechanism; v2 ports it (stage 5, D19) before stage 8 deletes `helpers/agent_bus`.

### 2.8 Security invariants (from Plan 00161 section 9, unchanged)

- R37. No agent runs as a user that holds a human's Matrix session (dedicated agent user, ccy
  container, or the accepted `cc` exception).
- R38. Root on the homeserver host, and whoever holds a backup, can instruct every agent; v2
  must not widen who that is.
- R39. Container-written files are never authority: the inbox, a seat's report, a checkout's
  `ccy.env.local` are data to validate, never instructions. The same holds for room state a
  member controls (`m.room.member` display names and membership): a host process that holds an
  admin token and reads it grammar-checks every field it passes on (4.2).

---

## 3. Shape of v2

```text
  host
    owner ── ccy-teams (Go, as the owner)
               ├─ in-process: bus/view (host_vars, stamps, health, LXC records, seats, units)
               └─ sudo -n -u agent-bus agent-bus view ── one process for every team,
                    parallel loopback HTTP ──▶ agent-bus-hs@<team> (Tuwunel, user agent-bus)
    sudo agent-bus <change>  (Go CLI as root, the owner's password)
               └─ child as agent-bus: bus/admin over the registry flock ──▶ the team's homeserver
    agent-bus-install (Go, root): software, team, remove, backup, restore, check,
                                   health-check, lxc-members; one install lock
  in a ccy / cc / LXC / docker / VM member
    pingbus (Go, static): keeper (locks, supervision) ── worker (sync, deliver, status row)
                          hooks, status line, recv/wait/send/say: separate short processes
```

No process is resident on the host for the bus but the homeservers, as in v1.

### 3.1 One codebase, three binaries

All in `src-go`, built by `tasks/build-go-command.yml`, versioned by source digest (Plan 00206):

| Binary              | Runs as                                                                                                                                                 | Where                                                                                                                                                                                                             | Replaces                                                                                                                                                                    |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agent-bus`         | the owner (`seat`, `health`, `element`, `status`); `agent-bus` by the NOPASSWD Runas rule (`view` only); root by sudo with the password (admin changes) | host                                                                                                                                                                                                              | the bash root wrapper, its `runuser` and tar dance, the admin half of `agent-bus.pyz`, the seat commands, `health status`, the `list` / `human devices` reads, `element.py` |
| `agent-bus-install` | root                                                                                                                                                    | host                                                                                                                                                                                                              | the bash installer (Plan 00192 Task 3.2's port), `render`, `bridge.py`, `config_restart`, `timer_state`, the health check, `members.py`                                     |
| `pingbus`           | the member's user                                                                                                                                       | every member: ccy container, cc on the host, LXC, docker, VM; also deployed on the host for `seat disconnected`, `farewell`, `retire`, `reach`, and so a deployed tool tracked by `qa/deployeddrift` (Plan 00206) | `pingbus.pyz` and the kit's Python                                                                                                                                          |

Each `agent-bus` mode refuses the wrong caller, as v1's wrapper does (exit 77): `view` runs only
with real and effective uid `agent-bus`, an admin change only as root, and `seat`, `health`,
`element` and `status` only as a user who is neither.

`ccy-teams` (and the ccy launcher's Go helpers) link the same packages and call them as functions;
the one bus process an open starts is the reader (4.2).

### 3.2 Packages

Under `src-go/internal/bus/`, each behind an interface with a fake, test first:

| Package             | Owns                                                                                                                                                                                                                                     |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `bus/proto`         | the wire protocol v1: identifiers and handle grammar, event content types, ping, verbs, references, human and agent text, team record, power levels, limits, validation, drop reasons. The one home of every grammar, used by both sides |
| `bus/state`         | every on-disk schema (section 5): strict decoders with version ranges, atomic writers, the path layout (one home for `/var/lib/agent-bus/<team>/...`, unit names, seat paths), and the cross-process locks of section 7                  |
| `bus/matrix`        | a small Matrix client: one transport (no proxy, no redirects, token only in an unredirected header, bounded bodies, errors without server text), sync, send with txn reuse, state, receipts, messages, admin endpoints                   |
| `bus/matrix/fakehs` | an in-memory homeserver for tests: the endpoints both sides use, with Tuwunel's recorded quirks as switchable behaviours (section 11)                                                                                                    |
| `bus/forge`         | the GitHub checks (R16), their cache and the credential's lifetime (R44)                                                                                                                                                                 |
| `bus/admin`         | the team admin operations (bootstrap, members, roles, humans) over `bus/matrix` and `bus/state`; used by the `agent-bus` admin child and the reader                                                                                      |
| `bus/view`          | the read model: the types ccy-teams reads, assembled from in-process sources and one reader call (section 4)                                                                                                                             |
| `bus/seat`          | a checkout's seats: list, choose, take, remove, the allocation lock, bundle placement by fd                                                                                                                                              |
| `bus/declare`       | the host_vars declarations: read, check and edit `agent_bus_teams`, `agent_bus_lxc_members`, `agent_bus_element_teams`, `agent_bus_address` (replaces `declare.py`)                                                                      |
| `bus/element`       | the Element Desktop profiles (R50; replaces `element.py`)                                                                                                                                                                                |
| `bus/member`        | the member client: config and bundle checks, inbox and outbox, sync engine, delivery (envelope, socket, transcript), hooks, status line, keeper, membership                                                                              |
| `bus/lxc`           | LXC member derivation and apply (R33, R48), guest operations through the kit's `pingbus guest`                                                                                                                                           |
| `bus/install`       | what is today `internal/businstall` on the 00192 branch, plus the install lock and the health check                                                                                                                                      |

The packages are not built ahead of use: each stage of section 10 brings the decoders, grammars
and corpus cases its callers need, so no stage ships code nothing calls.

The exec, marker-line and exit-code layers of today's `internal/agentbus` and `internal/declare`
disappear for every in-process call [G 6]. Marker lines and exit codes stay where a process
boundary stays: the CLIs a human, a play or the bash launcher reads, and the reader's one JSON
answer.

### 3.3 Why no daemon in this plan

The first draft put a resident `agent-busd` on the host, holding every team's admin session and
serving the board over Unix sockets. Fable's review found that its three reasons do not hold for
this plan, and they are set out here so that D1 is decided on the evidence:

1. **A live read model.** Nothing on the board needs sub-second freshness; a redraw re-reads.
2. **One writer.** v1 already serialises every registry change under `registry.lock` [H 7.09]; a
   `flock` across CLI processes is single-writer discipline. The daemon would also have shared
   that lock with the Python tool through stage 5, so it would not have been the single writer
   during the stages that introduced it.
3. **The seat broker's home.** Plan 00197's broker is deferred to its own plan (D11). Building its
   host now is speculative.

Against it, the operational cost of a new long-running service: a unit and sandbox to maintain;
a new failure mode in which the board cannot show members at all; freshness semantics
(`observed_at`, stale marks) the board must render; a version skew check between daemon and
ccy-teams; inotify on the registry during coexistence; an audit log; two sockets; an owner file;
and a resident process reachable by local users that holds every team's admin token (6.4). Each
is small; together they were the bulk of the first draft's stage-4 and stage-5 code, for a read
that one short-lived reader does in about 0.1 s [G 5 step 3].

So the read path is one reader process for all teams, run as `agent-bus` by a NOPASSWD Runas
rule, with no root at any point (4.2, D1 option (c')); admin changes stay per-command processes
under v1's registry flock (section 7). If the broker plan needs a daemon, its approval flow is
the reason to build one, and 6.4 records what that daemon's sockets and sandbox must be. The rest
of this design is written to that recommendation; if the owner chooses the daemon in D1, sections
4.2, 6 and 7 are revised from 6.4 before stage 4's spec is written.

---

## 4. The read model ccy-teams uses

### 4.1 Sources, privilege and cost

| Fact on the board                                        | Source                                                                                                   | Read by                                                                     | Privilege                                      | v1 cost per open                                           | v2 cost                      |
| -------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- | ---------------------------------------------- | ---------------------------------------------------------- | ---------------------------- |
| declared teams, descriptions, LXC entries, Element teams | host_vars `localhost.yml`                                                                                | `bus/declare`, in-process                                                   | the user                                       | 7 Python starts (~3.7 s)                                   | one YAML parse               |
| APPLIED (current / out of date / not built / decide)     | stamps `/var/lib/agent-bus-applied/<t>.json` (0640, stamp group) + the deployed binaries' source digests | `bus/view`, in-process (one `declare overview` until stage 3)               | stamp group (the user)                         | inside `overview`, with a zipapp rebuild per call (183 ms) | file reads                   |
| health, backups                                          | `/var/lib/agent-bus-applied/health/<t>.json`                                                             | in-process (already Go)                                                     | stamp group                                    | none                                                       | none                         |
| held-back LXC members                                    | `/var/lib/agent-bus-applied/lxc/<t>.json`                                                                | in-process (already Go)                                                     | stamp group                                    | none                                                       | none                         |
| homeserver unit state                                    | systemd                                                                                                  | one `systemctl show` for every unit, or D-Bus                               | the user                                       | 2-3 `systemctl` runs                                       | one call                     |
| a checkout's seats, held or free                         | `<checkout>/.claude/ccy/pingbus/seats/<t>/<s>/member.json` + a `flock` probe of `seat.lock`              | `bus/seat`, in-process                                                      | the user                                       | 1 Python start per checkout (~2.8 s for 6)                 | file reads                   |
| members, roles, parked, room membership                  | registry + homeserver room state                                                                         | `agent-bus view`, started by `sudo -n -u agent-bus`, one process, all teams | `agent-bus`, by a NOPASSWD Runas rule; no root | sudo->bash->runuser->Python + HTTP per team (~2 s for 3)   | one process, about 0.1-0.2 s |
| a person's Element sessions                              | homeserver admin API                                                                                     | the same reader, in parallel                                                | `agent-bus`                                    | the same per person (~3.5 s for 6)                         | in that process              |

The one systemd call keeps v1's rule that each unit type is asked only its own properties and a
missing one is refused by name [H 7.24]; it was a real bug and survives the consolidation.

So one open is: the YAML parse, a handful of file reads, one systemd call, the git and tmux calls
the board already makes, and one reader process.

### 4.2 The reader

`agent-bus view` is the one privileged read. It takes no argument, no stdin and no environment
from its caller (sudo's `env_reset`; the reader `chdir`s to `/` and refuses any argument), and
refuses to run unless its real and effective uid are `agent-bus`'s. For every team under
`/var/lib/agent-bus/` it reads the registry as v1 reads it [H 7.09] and the admin token, checks the
token with `whoami`, and then, one goroutine per team with a bounded number of requests in flight
to each homeserver, reads the room membership of every member and human and each human's devices.
It prints one JSON object on stdout: for each team its members (handle, role, membership, parked),
its humans (membership, device count, newest last-seen time) and, when that team failed, its error;
a failed team does not stop the others, and the exit status names that some team failed. Device IDs
are not in it: `ccy-teams` uses only the count [G 2], so the reader gives less than today's
`human devices` does (B2).

**Who may run it.** The sudoers rule becomes, in the same file:

```text
<owner> ALL=(agent-bus) NOPASSWD: /usr/local/bin/agent-bus ^view$
```

replacing the two `(root)` lines for `list` and `human devices` (6.3). The binary and its directory
are root-owned and not writable by the owner or by `agent-bus`: whoever could replace it would run
code as the user that holds every admin token.

**What it forwards.** Every field that comes from a homeserver (handles, display names, membership
values, counts, timestamps) is checked against `bus/proto`'s grammars before it is printed, and a
field that fails is that team's error, never passed on: `m.room.member` content is written by
members (R39). `ccy-teams` decodes the answer strictly (`bus/state`, with corpus cases).

**Seat rows are cross-checked.** A seat row is built from `member.json` and `seat.lock`, which a
container can write. v1 has the same exposure; v2 has the roster beside it, so a seat whose handle
the registry does not hold for that team is shown as unknown, not as a member (B8).

**Freshness.** The reader runs once per open. A redraw reuses the last answer while it is younger
than a bound the stage spec sets (it replaces v1's `Collector.devices` and `listCache` with one
rule), and the login flow re-reads after a sign-in or a new password.

**Failure.** A reader that cannot run (`sudo -n` refused because the rule is missing or stale, a
homeserver down, a bad answer) is shown as v1 shows a refused `sudo -n agent-bus list` today, with
v1's wording and exit status carried by the stage spec (D14). It never falls back to Python.

**Who learns what.** Any process of the owner's uid can run the reader, as any can run the two
sudoers-permitted commands today; a `cc` session on the host therefore can, and a ccy container
cannot (it has no sudo for the host). The reader tells that uid what the rule already told it, less
device IDs.

### 4.3 Seat flows and choose

`seat choose` and `seat take` in v1 run a nested `sudo -n agent-bus list` from inside Python
(`helpers/agent_bus/checkout.py:902-917`, [G 6]). In v2 they are `bus/seat` functions that take
the team's roster from the reader's answer: in-process in `ccy-teams`, sharing the board's answer,
and through the `agent-bus seat ...` CLI for the bash launcher, which runs the reader itself, until
the launcher calls Go.

### 4.4 The latency target

**`ccy-teams` (and `--list`) open in about 1 s or less** at six checkouts, three teams and six
people, and a redraw that reuses the reader's answer in under 200 ms, measured on the host (H1,
H10). The budget, from v1's measured parts \[G 5\]:

| Part                                            | v2 estimate                                      |
| ----------------------------------------------- | ------------------------------------------------ |
| process start (Go)                              | ~5 ms                                            |
| host_vars parse + stamps + health + LXC records | ~5 ms                                            |
| systemd unit state, one call                    | ~20 ms                                           |
| git and tmux (as the board already runs)        | ~100-150 ms                                      |
| seats of six checkouts (files and lock probes)  | ~5 ms                                            |
| the reader: sudo and PAM, Go start, HTTP        | ~50-100 ms + ~5 ms + ~0.1 s in parallel (H3, H5) |
| drawing                                         | as today                                         |

About 0.3-0.4 s in all; the target is 1 s so that the git, tmux and podman probes, which this plan
does not rework, have room. Per stage (section 10): stage 1 takes the open from about 12 s to about
6 s and a redraw from about 4.5 s to about 2 s; stage 3 removes the last Python start; stage 4 takes
the open to about 1 s and a redraw to about nothing. Each stage measures before and after (H10).

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
- checkout seats: the bundle (`member.json` protocol 1, `token`, `README`), `seat.lock`
  (`seat <claim_ms>`), `said-goodbye.json` (v1), `state/` (`sync.json`, `inbox/`, `consumed/`,
  `outbox.json`, `team.json`, `about.json`, `room.json`, `untrusted.json`, `forge-cache.json`,
  `dropped.log`, `lock`);
- the member's `PINGBUS_HOME`: `relay/lock` (the delivery lock, R47), `inject/<key>.watch`,
  `told.json` and `told.lock`;
- session: the desired and applied membership (v1), `relaunch.json` (v1, R46),
  `conversation.json` (v1, R46), the session home's `applied.json`, `active`, `socket.json`,
  `keeper.lock`; `goodbye.json` until stage 2 drops it (B7);
- locks: the allocation and change locks under `$XDG_RUNTIME_DIR/ccy/`, the play lock.

Keeping the seat directory's `state/` and the home's lock files exactly is what lets a seat be used
by a v1 session today and a v2 session tomorrow, and back (rollback, section 10.3), and lets a v1
and a v2 session of one user share a `PINGBUS_HOME` (R47).

### 5.3 New files

| Path                                                     | Owner / mode                | Schema                                                             | Why                                                                            |
| -------------------------------------------------------- | --------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| the reader's answer (stdout of `agent-bus view`)         | not a file                  | `{"v":1,"teams":[...]}`, strict, in the corpus                     | the read model (4.2)                                                           |
| `<session home>/status-row.json`                         | the member's user, 0600     | `{"v":1,"segments":["alpha (coordinator)", ...],"written_ms":...}` | the worker writes the status line's rows; the status line reads one file (8.2) |
| `/usr/local/share/agent-bus/kit/<digest>/` and `current` | root, 0755, files 0644/0755 | the kit as today, one directory per source digest                  | a session runs one kit version (R49, B9)                                       |
| `/run/lock/agent-bus-install.lock`                       | root 0600                   | `flock`; content names the holder's command                        | the install lock (7)                                                           |

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
implementation's output. Each stage adds the cases for the files and events it reads or writes.

---

## 6. Privilege: what needs root, and the least way

### 6.1 Who does what

| Operation                                                            | v1                                           | v2                                                                                         | Why that privilege                                                            |
| -------------------------------------------------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| install software, team units, firewall, NetworkManager, restore      | root (installer)                             | root (`agent-bus-install`)                                                                 | writes `/etc`, `/usr/local`, firewalld, other users' directories              |
| health check (probe, restart a homeserver, read root's backup tars)  | root timer, sandboxed with two capabilities  | the same, run as `agent-bus-install health-check`, the same sandbox (R7)                   | restarts a system unit; reads root's 0700 records                             |
| read members, roles, room membership, devices                        | root by NOPASSWD sudo, then `agent-bus`      | `agent-bus` by a NOPASSWD Runas rule, one argv, no root at any point                       | the facts live behind the admin token, which is `agent-bus`'s                 |
| add, park, remove a member; set a role; human password, lock, logout | root by sudo with password, then `agent-bus` | root by sudo with password (`sudo agent-bus ...`), the operation in a child as `agent-bus` | the owner's consent at the time (R13); the admin token stays with `agent-bus` |
| place a bundle in a checkout                                         | the user, from a tar root re-packed          | the user, from the typed answer `sudo agent-bus add-member --out=-` prints                 | root never writes under a user's path (R12)                                   |
| a checkout's seats                                                   | the user                                     | the user, in-process                                                                       | the files are the user's                                                      |
| Element profiles                                                     | the user (`element.py` from the checkout)    | the user (`agent-bus element`, deployed)                                                   | the profile is the user's                                                     |
| LXC member apply                                                     | root (play, `members.py`)                    | root (`agent-bus-install lxc-members`)                                                     | reads container rootfs, runs `lxc-attach`                                     |
| homeserver                                                           | `agent-bus`                                  | unchanged                                                                                  |                                                                               |

### 6.2 Admin changes and sudo

`sudo agent-bus <admin command>` runs the Go CLI as root. It starts the operation as a child with
`agent-bus`'s uid and gid and no supplementary groups (`os/exec` with a `Credential`, replacing
`runuser` and bash), which takes `registry.lock` and calls the homeserver. For `add-member` and
`rotate-token` the child returns the bundle as typed JSON on a pipe; root decodes it strictly (the
one thing root parses) and either prints it on stdout (`--out=-`, for the user-side seat code,
which places the files by fd) or, for `--out=DIR`, writes it in a second child started with the
sudo user's uid and gid and no supplementary groups (v1's `setpriv` invariant, D61). No tar is
unpacked by root and no bash runs.

**Why not let the owner's uid change without sudo.** A `cc --teams` session runs as the owner; a
change authorised by uid would let any agent in such a session add members or change roles with no
one asked. The password is the only presence check the host has (D3).

**Why not `sudo -u agent-bus` for changes too.** With the owner's general sudo and the password,
`sudo -u agent-bus agent-bus add-member` would remove root from changes entirely. `--out=DIR`
needs a write as the sudo user, which `agent-bus` cannot make, and the CLI contract (`sudo agent-bus ...`) is what the launcher, ccy-teams and the documented procedures call. Root's part in v2 is two
credential switches and one strict decode; the stage-5 spec may revisit this (D3).

### 6.3 The sudoers change

At stage 4 the file `/etc/sudoers.d/agent-bus-read-only` changes from two `(root)` lines (`list <team>` and `human devices <team> <name>`, each pinned by an anchored expression) to one
`(agent-bus)` line for `view` with no argument (4.2). Least-privilege argument:

- root leaves the read path: the rule no longer starts anything as root, so no bug in a bash
  wrapper or an argv parser reached by it runs as root;
- one argv shape with no argument, where v1 had two with arguments;
- the reader learns nothing from its caller, so the input it parses is the bus's own homeservers'
  answers on loopback, grammar-checked;
- said plainly: the reader holds every team's admin token for the length of its run, where v1's
  `list` held one team's. A bug in it exposes all teams at once to a caller who could already ask
  all of them, in turn, for the same facts.

The old lines stay beside the new one until the owner accepts stage 4, so a rollback to the
previous `ccy-teams` works; they are removed in the commit after acceptance.

### 6.4 The alternatives D1 weighs

- **(a) A daemon serving a view socket.** If it is ever built (the broker plan), it holds every
  team's admin token in one resident process, so the sockets must be shut by file mode before any
  peer check: `admin.sock` `0600 agent-bus:agent-bus` (root bypasses DAC, so `sudo agent-bus`
  connects and no other uid can), `view.sock` `0660` with the owner's group, or in a `0750`
  directory with an ACL for the owner's uid, and `SO_PEERCRED` kept as defence in depth (a
  `sudo`-started CLI is seen as uid 0, which needs a host check). Its unit needs the homeservers'
  sandbox (`PrivatePIDs`, `ProtectProc=invisible`, `NoNewPrivileges`, read-only system, loopback
  only): the homeservers share its uid, and the per-team `TemporaryFileSystem` exists so one
  process of that uid cannot reach a sibling's secrets, while the daemon is a sibling that holds
  all of them. It grammar-checks every room event it forwards (R39). Its operational cost is 3.3's
  list.
- **(b) A group-readable live file**, `/var/lib/agent-bus-applied/live/<t>.json`, 0640 to the stamp
  group, written by a timer: no socket and no sudo, but freshness is the timer's interval, the
  login flow cannot ask for a refresh, and the access boundary is the user's primary group, which
  is broader than one uid.
- **(c) One Go helper under the existing rule's shape**: root by NOPASSWD sudo, dropping to
  `agent-bus`. It reaches the same latency as (c') but keeps root on the read path for no reason.
- **(c') The reader of 4.2**: `agent-bus` by a NOPASSWD Runas rule, no root, no daemon, about 0.1 s.

---

## 7. Concurrency and locking

- **Registry and homeserver admin**: every admin change takes v1's `registry.lock` `flock`, keeps
  "claim first, then account, undo on failure" and the one-coordinator rule, and re-reads the
  registry under the lock; the Go admin child and the Python tool take turns on it through stage
  5's coexistence. The reader reads the registry as v1's `list` does. The reader and the Python
  tool may use one admin token at once (H4).

- **Installer**: v1's installer takes no lock (B16). v2's takes one host lock,
  `/run/lock/agent-bus-install.lock`, for every mutating command (software, team, remove,
  backup-now, restore, lxc-members, health-check's restart), with a bounded wait that names the
  holder, so the backup timer and a play can no longer interleave (00192 spec D4).

- **The installer and readers**: a team's restart or restore makes its homeserver vanish; the
  reader reports that team's error after its per-team timeout and answers the rest.

- **Seats**: the allocation lock, change lock and seat locks keep v1's places and semantics (all
  `flock`, all out of a container's reach except the seat lock, which a session must hold) [L 3.3].

- **Member side**: v2 keeps **every** v1 member lock at its v1 path, holder kind and order (R47),
  because `recv`, `wait`, `send`, `say` and the hooks stay separate processes, started by the
  agent or by Claude Code, not children of the keeper, and because a v1 process can share the same
  files until stage 8 \[P 4\]:

  | Lock          | Path                           | v2 holders                                                                                                 | Why it stays                                                                                                                                                 |
  | ------------- | ------------------------------ | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
  | sync lock     | `state/lock`                   | the worker (kind `watch`), `wait`, `recv` (one sync)                                                       | one syncer per team                                                                                                                                          |
  | seat lock     | `seats/<t>/<s>/seat.lock`      | the keeper only (close-on-exec)                                                                            | presence (R27)                                                                                                                                               |
  | keeper lock   | `<home>/keeper.lock`           | the keeper; hooks probe it                                                                                 | the "keeper stopped" mark                                                                                                                                    |
  | watch lock    | `inject/<key>.watch`           | the worker, which replaces the supervisor and child                                                        | one deliverer per session socket                                                                                                                             |
  | delivery lock | `relay/lock`                   | the worker's delivery; `recv` and `wait` while they print and consume (`helpers/pingbus/cli.py:1233-1238`) | an item is shown by exactly one of them (R22, SPEC-inject E12, RecvRelayRaceTest); kept at v1's path though relay mode is not built, renamed only at stage 8 |
  | outbox lock   | `flock` on the `state/` dir fd | `send`, `say`, the worker (answers), deliverers (`reported`) (`helpers/pingbus/inbox.py:560-571`)          | every outbox read-modify-write (R42)                                                                                                                         |
  | told lock     | `<home>/told.lock`             | the `prompt` hook and the worker, non-blocking (`helpers/pingbus/membership.py:63,440-457`)                | membership notices once (R30)                                                                                                                                |

  Lock order stays delivery then outbox; an acquire still tries three times 50 ms apart so a
  probe's momentary shared lock does not look like a holder. Inside the worker each acquisition
  opens its own file description, since a `flock` taken on one shared descriptor excludes nothing
  between goroutines. Each rule is an expectation proven with a second real process, and through
  stage 8 also with a real v1 process (section 11).

- **Play lock**: unchanged; held by ccy-teams across the host_vars write and the play (R34).

---

## 8. The member client, pingbus v2

### 8.1 What stays

The CLI surface and its contract: every command of [P 1], its output lines (fields only ever
appended) and its stable exit codes (protocol §13-§15). Agents and the skill call these; another
harness relies on them (00194 decision A). The bundle, the env file, `PINGBUS_*` variables and the
plugin's hooks stay, so a session cannot tell v1 and v2 apart except by speed. Every command stays
its own process.

### 8.2 One resident process tree per session

v1 runs, per session: a seat keeper, a watch supervisor, a watch child, a short-lived Python per
hook, a Python per status-line refresh, and a hook-driven revive path when the watcher dies
[P 4]. The owner rejected the fallback chain once already and asked for the root cause [L 6.12].
v2 has two resident processes, in every kind of member (ccy, `cc`, LXC, docker, VM; D15):

- **the keeper** (`pingbus seat keep`, started by `seat exec` before Claude Code, or by
  `agent-bus-claude` for a member without seats): holds the seat locks and the keeper lock,
  follows the desired membership, dies with its session (`PR_SET_PDEATHSIG`), and supervises the
  worker (restart with backoff, a status mark when it keeps failing). It makes no network calls,
  so it has nothing that fails at run time but its own file reads;
- **the worker**: holds the sync locks and the watch lock; one goroutine per active team for the
  sync loop, one delivery loop, the membership notices, the `listening` status, and the status row.
  Its state is written through to the v1 files in `state/` (5.2) so `recv`, `status` and a v1
  session see the same thing.

`recv`, `wait`, `history`, `send`, `say` and the hooks stay separate short processes and take the
locks of section 7, so they are correct with or without a worker, including a human running
`pingbus recv` by hand on an LXC member. `send` asks the worker for the room's verified state over
a socket in the session home (0600) when one runs, and verifies as v1 does when none does (B5).

**The status line.** The worker writes `status-row.json` (5.3) on every change and at least every
few seconds; the status line reads that one file and probes the keeper lock for its marks, wraps the
segments at `COLUMNS`, and runs the inner command under every rule of R26. Go's start-up is about
5 ms, so the bus's part of a refresh is one `exec` and one file read; the inner status line it wraps
(`/bin/sh -c <command>`, 10 s timeout) is the session's own and dominates, and is not the bus's to
fix. `refreshInterval` is the kit's own choice
(`files/opt/claude-yolo/optional/agent-bus/settings.json`): 25 s since Plan 00213 Task 1.3, inside
the supervisor's 30 s sidecar freshness (D16). Even in Go the
status line never opens the inbox or loads the member code: a binary that counts pending items
every second is v1's mistake in a faster language.

**Hooks** become Go: no import cost, and the SessionEnd goodbye fits Claude Code's default 1.5 s
with room to spare (the 8 s setting stays, as a margin), with every rule of R25, R28 and R45.

**Two steps, one implementation each.** Stage 2 (section 10) ships Go hooks and a Go status line
while the receiver is still v1's Python watcher. In that kit the Go `pingbus` implements `hook` and
`status-line` and executes the kit's `pingbus.pyz` for every other command (B10): each command has
one implementation per kit version, so it is a dispatch, not a fallback. With no worker yet, that
status line derives its rows from v1's files exactly as v1's status line does. Stage 6b brings the
keeper and the worker, switches the status line to `status-row.json` and deletes the derivation in
the same commit. R49 keeps a running session on the kit it started with, so no session sees one
kit's hooks against another kit's receiver.

### 8.3 Delivery

One path: the envelope written to Claude Code's inbox socket (00194's amended decision B), with
every rule of SPEC-inject E1-E26 kept as expectations: re-fetch, delivered once, nonce, the
allow-list renderer, flood budget, held-back line, stall and missing marks, transcript proof,
receipts. Relay mode (the spool for a supervisor plugin that has not shipped) is not built in v2
(D4); its lock file stays as the delivery lock (section 7). When the hooks-daemon plugin API it
waits for ships, the supervisor path is specified against v2 as its own stage.

The envelope's allow-list renderer classifies characters by Unicode block and category. Go's
`unicode` tables (Unicode 15.0 in current Go) are newer than Python 3.11's `unicodedata` (14.0),
so the two renderers can differ. v2 escapes any character its own table does not know, which is the
safe direction, and the corpus (5.4) pins the disagreement instead of leaving it to be found: it
carries the expected bytes from each renderer for characters added in Unicode 15 (which Go knows and
Python does not) and for any character Python knows and Go does not (none today; a case is added if
one appears), as well as for every listed block.

### 8.4 No Python in members, and the ccy image

The kit ships one static `pingbus` (`CGO_ENABLED=0`). Containers, LXC guests, docker and VM
members no longer need Python 3.11 for the bus; the LXC kit sync stops sending Python into the
container (8.5). One build per architecture the host runs; amd64 only until a member on another
architecture exists (D8).

The pingbus zipapp is baked into the ccy image: `play-claude-yolo.yml` builds it from
`helpers/pingbus` into `optional/agent-bus/`, which the Dockerfile copies
(`files/var/local/claude-yolo/Dockerfile:398-407`), and `helpers/agent_bus` rides along
(`helpers/agent_bus_lxc/applied_record.py:4-7`). So:

- every stage that changes what a ccy container runs (2, 6a, 6b and 8) is a container version bump
  (`LABEL claude-yolo-version` and `REQUIRED_CONTAINER_VERSION`, and `CCY_VERSION` when the
  launcher or its `lib/` changes), as `CLAUDE/ContainerRules.md` requires;
- the container-version gate's file set (`scripts/test-ccy-container-version-hook.bash` checks it
  against what the play stages) must cover the Go sources the static `pingbus` is built from, or a
  Go change reaches the image with no bump; that change lands with stage 2;
- "a running session keeps v1 until it restarts" means, for ccy, until its container is rebuilt from
  the new image and the session restarted (D17).

### 8.5 LXC guest operations and the kits

v1 runs `python3 -I -c <script>` inside each container as root and as the agent user, for kit
sync, bundle placement, env merge and the service unit [L 1.3]. v2 copies the kit's static
`pingbus` into the container once (streamed on stdin to
`lxc-attach -- install -m 0755 /dev/stdin <path>.new` then renamed, which needs only coreutils),
then runs `pingbus guest <op>` for each step, with v1's guards as expectations: its own umask first
and every leaf mode named [L 6.02], no-follow opens, `O_EXCL` placement, the managed env lines only,
and every rule of R48. Reading the checkout stays on the host side, as root, from the container's
rootfs, never by running container code (R33).

The host kit under `/usr/local/share/agent-bus/kit` and its mirror in each container become one
directory per source digest with a `current` link (B9); `agent-bus-claude` and `seat exec` resolve
`current` once at launch and pass the resolved path to Claude Code's `--plugin-dir` and
`--settings`, so a play that installs a new kit changes only the sessions launched after it.

---

## 9. The homeserver interaction

- **One transport** (`bus/matrix`) for the admin side and the member side: no proxy, no
  redirects, the token only in an unredirected `Authorization` header, a request timeout, a body
  cap, errors carrying method, path, status and errcode only [P 3, H 3]. The admin side refuses any
  base URL but `http://127.0.0.1:<port>`.
- **Admin session**: the admin child and the reader each load `admin.token` and check it with
  `whoami` once per team per run, as v1 does.
- **Reads for the board**: the reader asks each member's and human's room membership and each
  human's devices (`GET /_synapse/admin/v2/users/<id>/devices`), every team at once (H5).
- **Every Tuwunel quirk v1 found is an expectation** of `bus/admin` and a switch in `fakehs`: no
  `admin` key on account creation (500), password reset with logout as the only revoke, one server
  admin, config keys bare, `max_request_size` floor, PID-1 signal drop before ready, SIGUSR2 backup,
  login buckets in memory [H 3].
- **The member sync loop** keeps v1's filter, first-sync and post-join rules, gap fill, trust
  re-verification, backoff, and the `listening` status cadence [P 3]. In a long-running worker the
  room's verified state is kept in memory, so `send` no longer re-verifies with three GETs per
  message when the worker is running (it asks the worker); a send with no worker verifies as v1 does.
- **The forge** in the worker follows R44.

---

## 10. Migration: v1 and v2 side by side

### 10.1 Principles

- **The wire protocol stays version 1** until every member is v2 and a change has a reason
  (D5): members on other machines may run v1 for a long time.
- **State files and locks stay shared by both** (5.1, R47); a version bump waits for stage 8.
- **One component at a time**, each a deploy of its play and its own acceptance, so a regression
  points at one change. Each stage's acceptance includes the `qa-reviewer` agent over that stage's
  diff, since each is a deploy.
- **The owner's pains first**: the stages that change what the owner sees on every open and in
  every session come before the ones that only remove code (D20).
- **No fallbacks between v1 and v2 at run time.** A stage switches a caller from v1 to v2; it does
  not try v2 and fall back to v1 (fail fast). Rollback is a redeploy of the previous commit.

### 10.2 Cut-over order

| Stage | What moves to v2                                                                                                                                                                                                                                                                       | v1 left running                                                                                     | What the owner sees                                                                                                       |
| ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| 1     | `ccy-teams` in-process reads: `bus/declare` read side (one parse per open), seats in-process, one systemd call; **one** `declare overview` Python call kept for the APPLIED verdict; the decoders and corpus cases for what it reads. No dependency on the installer                   | `declare overview` (APPLIED), `sudo list` / `human devices`, declare's write side, seat take/remove | an open about 6 s shorter (7 + C starts become 1), a redraw from about 4.5 s to about 2 s; H1 first, at an ordinary load  |
| 2     | pingbus hooks and status line in Go, reading v1 files (B10); versioned kits (B9); `goodbye.json` dropped (B7); the gate covering pingbus's Go sources; a ccy image bump                                                                                                                | the watcher, keeper and every other pingbus command (`pingbus.pyz`); v1 in running containers       | a new session starts no Python per second or per hook, and its goodbye fits the default SessionEnd budget                 |
| 3     | the installer (00192 Task 3.2's Go port, merged after its host test), with the install lock; health check in Go; the stamp's software digest from the Go binaries (B4), so the last `declare overview` goes                                                                            | admin tool, pingbus receiver, LXC helper, declare's write side                                      | every team "out of date" once, then current after one apply (H6); otherwise the port's numbered departures                |
| 4     | the reader (4.2); `ccy-teams` and the seat flows read members and devices from it; the Runas sudoers line added; the old lines removed after acceptance                                                                                                                                | admin changes (Python, through the wrapper)                                                         | an open about 1 s; no root at open; a redraw about nothing                                                                |
| 5     | admin changes in the Go `agent-bus` CLI (child as `agent-bus`), the bash wrapper deleted; `bus/declare` write side; seat take/remove in Go; `agent-bus element` replaces `element.py` in the Element play (R50, D19)                                                                   | pingbus receiver, LXC helper                                                                        | nothing but speed                                                                                                         |
| 6     | keeper and worker; the status line reads the worker's row; the remaining pingbus commands; a ccy image bump                                                                                                                                                                            | v1 pingbus in sessions launched before the deploy, and in remote members                            | a new session uses v2 throughout; a running one keeps its kit until it restarts (for ccy, until its container is rebuilt) |
| 7     | LXC member apply in Go, guest ops through `pingbus guest`; the LXC kit sync sends the static binary, not Python                                                                                                                                                                        | v1 pingbus inside containers until the kit sync replaces it                                         | containers need no Python for the bus                                                                                     |
| 8     | retire v1: first the legacy-form migration (D9) runs and reports clean, then the Go decoders drop the legacy forms; then delete `helpers/agent_bus`, `helpers/pingbus`, `helpers/agent_bus_lxc`, the zipapps and their build; a ccy image bump; version bumps and lock renames allowed | v1 pingbus in remote members (protocol 1 still served)                                              | one language                                                                                                              |

Stage 6 is delivered as two stages (PLAN.md, D20): 6a moves every remaining short verb to Go,
`seat disconnected` included, with the Python watcher still the receiver and `watch`, `seat exec`
and `seat keep` still dispatched to `pingbus.pyz`; 6b brings the keeper, the worker, delivery and
the status row. Each has its own ccy image bump (D17). B10 holds until 6b, and H9 passes before 6b
is accepted.

Stage 1 depends on nothing host-side and stage 2 on nothing but the kit, which is why they come
first. Stage 1 keeps exactly one `declare overview` because the APPLIED verdict compares the stamp's
software digest with v1's definition (a hash of the `agent-bus.pyz` it rebuilds and of the
checkout's installer file [H 6, H 8.03]); stage 3 redefines it as the source digest each deployed Go
binary is built with (Plan 00206's version, as the installer port's own D1 chose), the stamp's
`software` field is recomputed from it, and the last Python call goes. The installer's unit files
change when their `ExecStart` does, which is a `software` CHANGED and a `daemon-reload` on the
first stage-3 apply, said in the stage-3 spec.

### 10.3 Rollback

- Stages 1, 3 and 7 change no state format: rolling back is redeploying the previous commit.
- Stages 2, 6a and 6b: no state format changes either; a session started on the new kit or image keeps
  it until it restarts, and the previous commit's kit or image serves sessions started after the
  rollback. A drill (H9) proves a seat moves between a v1 and a v2 session and back before stage 6b
  is accepted.
- Stage 4: the old sudoers lines stay until the owner accepts the stage; rolling back redeploys the
  previous `ccy-teams`.
- Stage 5: the Go admin child and the Python tool take the same registry lock and write the same
  registry version, so redeploying the previous commit restores the Python path with the Go
  changes intact; a host check confirms the registry is still version 2 and the Python tool reads it
  (H12).
- Stage 8 is the one-way step and waits for every earlier stage to be accepted and to have run
  without a rollback for the period the owner sets.

---

## 11. Test strategy

- **Spec first, per stage.** Each stage has a `SPEC-<component>.md` in this plan with numbered
  expectations (E1...), the v1 requirement each one comes from, and departures (B1...) where v1
  was wrong, as `src-go/CLAUDE.md` "How a port is built" says. The v1 code is read for
  requirements and never recorded as an oracle. Each spec is written from section 2, not from
  the package layout, which mirrors v1's modules only as an inventory.
- **Fakes**: `fakehs` (the homeserver, with Tuwunel's quirks as switches and fault injection:
  refused connection, 429 with `Retry-After`, a limited timeline, a restart mid-sync); a fake clock;
  `execx.Fake` for systemctl, lxc, sudo and git; real temporary directories with real `flock` for
  every lock rule (a lock is only proven by a second process, so lock tests start one); a fake
  Claude Code inbox socket and transcript for delivery.
- **Coexistence tests**, each with a second real process, the other side being the v1 Python
  until stage 8:
  - stage 5: the Python admin tool and the Go admin CLI on one registry under one `flock`;
  - stage 2: the host's v1 `pingbus seat disconnected` reading the `said-goodbye.json` a v2
    session's Go SessionEnd hook writes, and the Go hook charging the send gate in `outbox.json`
    under the outbox lock against a v1 `send`; from 6a, when `seat disconnected` is Go: the host's
    v2 `pingbus seat disconnected` reading a v1 session's `seat.lock` claim and
    `said-goodbye.json` (H11 on the host);
  - R47: every member lock of section 7 between a v2 process and a v1 process on one
    `PINGBUS_HOME`, including a v1 `recv` against the v2 worker's delivery (RecvRelayRaceTest's
    case) and a v1 `send` against the v2 worker's outbox writes;
  - R46: `conversation.json` written by v2 hooks and resumed by `ccy-teams restart`;
    `relaunch.json` read by the ccy supervisor with a v2 kit.
- **Shared corpora** (5.4) run by both the Python and the Go tests until stage 8.
- **The real homeserver**: Go tests behind a build tag run against the pinned Tuwunel binary when
  it is present (the host, the VM lab), covering bootstrap, members, the reader and the member sync
  loop; the VM lab gets an end-to-end scenario (a team, two members, a goodbye, a disconnect, a
  restore).
- **Latency**: a test pins the number of processes an open starts (one bus process from stage 4)
  and a host check measures wall time (H1, H10).
- **The bash suites** that drive ccy-teams (`scripts/test-ccy-teams-command.bash`) keep running;
  a behaviour they pin that v2 changes is changed in the suite in the same commit, with the reason.

---

## 12. What v1 does that v2 does not carry

| v1                                                                                                                  | Why not in v2                                                                                                                         |
| ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| A process per read, serial, and the caches built to hide it (`Collector.devices`, `listCache`, the describer) [G 6] | reads are in-process or one reader process; one freshness bound replaces the caches                                                   |
| `declare overview` three times per open, and a zipapp rebuild in each to hash it [G 1d]                             | one parse per open; the digest is the binary's source digest                                                                          |
| Root on the read path: the NOPASSWD `(root)` lines and the bash wrapper's `runuser` [H 5]                           | one `(agent-bus)` Runas line for a reader that takes no argument                                                                      |
| The bash root wrapper, its tar unpack and `setpriv` [H 5]                                                           | the Go CLI: a child as `agent-bus`, a typed bundle, a child as the sudo user for `--out=DIR`                                          |
| `declare.py`'s hand-written YAML-subset line editor [H 8.02]                                                        | a Go YAML node editor over the real document, with comment and vault-tag preservation as expectations (D6)                            |
| ccy-teams and the Element play loading `helpers/agent_bus` from the checkout (Plan 00206 R1)                        | linked into deployed binaries                                                                                                         |
| Python strings run in containers, and the Python 3.11 prerequisite in every member [L 7.01]                         | one static binary in the kit                                                                                                          |
| `render`, `stamp-group`, `bridge.py`, `config_restart`, `timer_state` as separate processes [H 8.01]                | functions of the installer                                                                                                            |
| The watch supervisor, watch child, hook revive and waiter chain [P 4, L 6.12]                                       | one keeper supervising one worker (the locks they took stay, section 7)                                                               |
| Relay mode: the spool, `relayed/`, `sent.jsonl`, `sent-offset`, the `relay=` field [P 6]                            | built for a plugin API that has not shipped (D4); `relay/lock` itself stays as the delivery lock until stage 8                        |
| Two body caps, duplicated constants and regexes [P 6]                                                               | one of each, in `bus/proto` and `bus/state`                                                                                           |
| Two goodbye records                                                                                                 | `said-goodbye.json` (per claim, read by the host's `seat disconnected`) stays; `goodbye.json` (per session home) goes at stage 2 (B7) |
| The status line re-implementing membership logic to save import time [P 6]                                          | the worker writes the row (stage 6b)                                                                                                  |
| `verify_room` (three GETs) on every send [P 6]                                                                      | the worker's verified state                                                                                                           |
| An installer with no lock [H 8.08]                                                                                  | the install lock (7)                                                                                                                  |
| The test-prefix hook in the production installer [H 8.05]                                                           | fakes and build tags                                                                                                                  |
| Root running the checkout's installer in place [H 8.04]                                                             | the play installs a root-owned binary and runs that                                                                                   |
| Legacy forms: role `orchestrator`, registry v1, container-name LXC seats [H 8.13, L 7.03]                           | read until stage 8's migration has run and reported clean, never written; then dropped (D9)                                           |
| A separate LXC record writer kept to avoid a ccy image bump [L 7.05]                                                | one writer in `bus/state`; the image bump is planned (8.4)                                                                            |
| Kits replaced in place under running sessions [L 1.3]                                                               | one directory per kit version (B9)                                                                                                    |
| Constant `TOOL_VERSION = "0.1.0"` [P 6]                                                                             | every binary reports its source digest                                                                                                |

What v2 keeps on purpose, though it looks like overhead: the strict decoders, check-everything
before changing anything, demote before promote, handle stability, locks out of a container's
reach, every member lock while v1 can share the files, record before desired file, the inbox as an
untrusted cache, and the SessionEnd timeout setting [L 7.10].

---

## 13. Observability

- Every binary answers `--version` with its source digest.
- `agent-bus status` (as the owner) runs the reader and prints each team's members and people or
  its error, with exit 1 when any team failed.
- The member side keeps `watch.log` (written by the worker), the status-line marks (`(down)`,
  `(role?)`, keeper stopped) and `pingbus status`.

---

## 14. Owner decisions

Each is defaulted to its recommendation (PLAN.md, "Owner decisions"); the owner may overturn any.
Three are amended there: D16, which Plan 00213 Task 1.3 set to 25 s; D17, whose bumps are at 2,
6a, 6b and 8 since stage 6 is split; and D20, which takes 10.2's order with that split. PLAN.md
also raises D4 (it overrides Plan 00194's owner design, and stage 2 already acts on it) and D15
with the coordinator at M0: the coordinator answers D4 before stage 2's hooks merge, and D15
before 6b merges.

| #   | Question                                                                                                                | Options                                                                                                                                                                                                                                                                                                                      | Recommendation and why                                                                                                                                                                                                                                                                                                                             |
| --- | ----------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | How does ccy-teams read members and devices without Python, and with how little privilege?                              | (a) a resident daemon with a view socket (6.4: sockets 0600/0660, peer check, homeserver sandbox; 3.3's operational cost); (b) a group-readable live file written by a timer; (c) one Go reader under the existing `(root)` rule shape; (c') the same reader run as `agent-bus` by a NOPASSWD Runas rule, no root, no daemon | (c') at stage 4: about 0.1 s for every team [G 5 step 3], no root at any point, one argv, no new service and no listening socket; the daemon deferred to the broker plan (D11), where its approval flow is a reason to exist. Either way the board shows a count of Element sessions and no device IDs (B2), the one place v2 shows the owner less |
| D2  | If D1 is (a): one daemon for the host or one per team?                                                                  | one / per team                                                                                                                                                                                                                                                                                                               | moot under (c'); otherwise one                                                                                                                                                                                                                                                                                                                     |
| D3  | Do admin changes still need the sudo password, and through which user?                                                  | `sudo agent-bus` as root with a child as `agent-bus` / `sudo -u agent-bus` with the password / the owner's uid without a password                                                                                                                                                                                            | the first: the password stays the presence check (a `cc --teams` session runs as the owner's uid), and the CLI contract and `--out=DIR` stay (6.2)                                                                                                                                                                                                 |
| D4  | Relay mode (Plan 00194 Task 2.1's spool for a supervisor plugin)?                                                       | drop in v2 and re-specify when the upstream API ships / port it                                                                                                                                                                                                                                                              | drop: it waits on an unreleased API and is not used; it doubles the delivery code. Its lock stays (section 7)                                                                                                                                                                                                                                      |
| D5  | The wire protocol?                                                                                                      | keep version 1 through and after the migration / plan a protocol 2                                                                                                                                                                                                                                                           | keep 1: members on other machines run v1; a protocol 2 needs a requirement first                                                                                                                                                                                                                                                                   |
| D6  | Where do team declarations live?                                                                                        | host_vars, edited by a Go YAML node editor / a v2 store of its own that the play reads                                                                                                                                                                                                                                       | host_vars: the play stays the one route (IaC rule); needs `go.yaml.in/yaml/v3` in the main module (an allowlist row); comment, ordering, anchor and `!vault` preservation are expectations with real-shaped fixtures (section 15)                                                                                                                  |
| D7  | LXC members?                                                                                                            | port the play-driven apply to Go now / build a reconciler on container start now / wait for the 00197 broker                                                                                                                                                                                                                 | port now (stage 7) with the same play-driven model. This departs from RESEARCH-host 8.12, which recommends designing around the broker: the broker is not started (D11), and LXC members need no Python sooner; a reconciler or the broker can replace the apply later                                                                             |
| D8  | Member binaries: static amd64 only?                                                                                     | amd64 only / amd64 + arm64                                                                                                                                                                                                                                                                                                   | amd64 until a member on another architecture exists                                                                                                                                                                                                                                                                                                |
| D9  | Legacy forms (`orchestrator`, registry v1, container-name LXC seats) at retirement?                                     | drop with a one-off migration / keep reading for ever                                                                                                                                                                                                                                                                        | drop at stage 8, after a migration run that rewrites them and reports clean, and before the Go decoders refuse them                                                                                                                                                                                                                                |
| D10 | 00192 Task 3.2's Go installer?                                                                                          | v2 adopts it as its installer (merge after the owner's root test) / v2 rewrites it                                                                                                                                                                                                                                           | adopt: it is built, reviewed and spec'd from requirements; v2 adds the lock, the health check and LXC apply                                                                                                                                                                                                                                        |
| D11 | Plan 00197's seat broker, and any daemon?                                                                               | its own plan, after stage 5 / inside this plan                                                                                                                                                                                                                                                                               | its own plan: its approval flow and network listener are a separate trust question, and the daemon it may need is decided there (6.4)                                                                                                                                                                                                              |
| D12 | The admin commands no program calls (`human logout-all`, `lock`, `unlock`, `rotate-token`, `remove-member`, `check`)?   | keep / drop                                                                                                                                                                                                                                                                                                                  | keep: they are the documented lost-phone and retire procedures                                                                                                                                                                                                                                                                                     |
| D13 | Is the 1 s open the acceptance bar, at what scale?                                                                      | 1 s at six checkouts, three teams, six people, and a redraw that reuses the reader's answer under 200 ms / another bar                                                                                                                                                                                                       | that bar, measured with `--list` and the interactive open at an ordinary load                                                                                                                                                                                                                                                                      |
| D14 | What does the board show when the reader fails?                                                                         | what v1 shows when `sudo -n agent-bus list` is refused / a new error row and a non-zero `--list`                                                                                                                                                                                                                             | v1's behaviour, carried by the stage-4 spec: the owner knows it, and a failure stays one team's                                                                                                                                                                                                                                                    |
| D15 | A keeper and a worker in every member (LXC, host, docker, VM through `agent-bus-claude`), not only seated ccy sessions? | every member / ccy only                                                                                                                                                                                                                                                                                                      | every member: one model, and the keeper makes no network calls; it is a new resident process on those machines, which is why it is asked                                                                                                                                                                                                           |
| D16 | The status line's `refreshInterval`?                                                                                    | keep 1 s / 2-5 s                                                                                                                                                                                                                                                                                                             | recommended keeping 1 s (the bus's part becomes about 5 ms); amended: 25 s, set by Plan 00213 Task 1.3 (PLAN.md)                                                                                                                                                                                                                                   |
| D17 | ccy image bumps?                                                                                                        | one bump per stage that changes the image (2, 6a, 6b, 8) / batch them                                                                                                                                                                                                                                                        | one per stage: each is a separate acceptance, and a running container keeps its version safely because v1 and v2 share every file and lock                                                                                                                                                                                                         |
| D18 | Versioned kits (B9)?                                                                                                    | one directory per kit version, resolved at launch / replace in place as v1 does                                                                                                                                                                                                                                              | versioned: otherwise a running LXC or host session runs a newer kit's hooks against an older receiver                                                                                                                                                                                                                                              |
| D19 | Element profiles (R50)?                                                                                                 | port `element.py` into `agent-bus element` at stage 5 / keep it as the one Python helper                                                                                                                                                                                                                                     | port: it is part of `helpers/agent_bus`, which stage 8 deletes, and the play runs it from the checkout (Plan 00206)                                                                                                                                                                                                                                |
| D20 | Stage order?                                                                                                            | the order of 10.2 (the owner's two pains first) / the first draft's (foundations, installer, reads, daemon, admin, pingbus, LXC)                                                                                                                                                                                             | 10.2's: stage 1 saves about 6 s per open with no dependency on the installer merge, and stage 2 stops the Python start of every status-line refresh and hook in every new session (a start per second when this was written); stage 6 is split into 6a and 6b (10.2)                                                                               |

---

## 15. Risks

| Risk                                                                             | Mitigation                                                                                                                             |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| v1 and v2 members disagree on a protocol rule and one drops what the other sends | the protocol corpus, run by both test suites; protocol 1 unchanged (D5)                                                                |
| The envelope renders differently (Unicode table versions)                        | each renderer's bytes pinned in the corpus, both directions; unknown characters escaped (8.3)                                          |
| The reader holds every team's admin token at once                                | no argument, no caller input, root-owned binary, one argv in the rule, homeserver fields grammar-checked (4.2, 6.3)                    |
| The reader fails and the owner cannot see members                                | shown as v1 shows a refused sudo (D14); members and messages unaffected                                                                |
| The Go YAML editor loses comments, ordering or vault tags in host_vars           | expectations over real-shaped host_vars fixtures (comments, `!vault`, anchors), a backup before every write as v1 does                 |
| A lock rule lost in the rewrite (the class v1 found most bugs in)                | every member lock kept at v1's path (R47, section 7); each rule proven with a second real process, and with a v1 process until stage 8 |
| A running session mixes kit versions                                             | versioned kits resolved at launch (R49, B9); H13                                                                                       |
| A pingbus change reaches the ccy image without a container version bump          | the gate's file set covers pingbus's Go sources (8.4), landed with stage 2                                                             |
| Coexistence lasts long and doubles maintenance                                   | stages are small and each retires its v1 part; stage 8 is in the plan, not deferred                                                    |
| The software digest change marks every team out of date once                     | said in the stage-3 release note; one apply restamps (H6)                                                                              |
| Scope: this is the largest single change to the bus                              | the owner's decisions first; each stage independently useful (stage 1 alone removes about half the open's cost)                        |

---

## 16. Host checks

Each is run on the host by the owner through `meta-deploy.bash` or by hand where noted, with the
result recorded in the journal.

- H1. Baseline: wall time of `ccy-teams --list` and of an interactive open, with the counts of
  checkouts, teams and people, before stage 1. A first reading of `--list` is recorded (section 1:
  9.3-23.0 s at a load of 30-39 on 22 cores); H1 repeats it at an ordinary load and adds the
  interactive open.
- H2. The per-start cost on the host: `time /usr/bin/python3 -I /usr/local/lib/agent-bus/agent-bus.pyz version`.
- H3. The reader through sudo: `sudo -n -u agent-bus /usr/local/bin/agent-bus view` is accepted by
  the Runas line (sudo's anchored expression, `agent-bus`'s no-login shell), refused with any
  argument, and timed.
- H4. The admin token used at once by the reader and the Python admin tool: nothing in the Python
  tool breaks.
- H5. The device and membership reads for every human and member of every team, in parallel, timed.
- H6. After stage 3's deploy every team reads "out of date" once, and `ccy-teams apply --all`
  makes them current.
- H7. A host `flock` probe of a `seat.lock` held inside a running ccy container reports it held.
- H8. The static `pingbus` runs in the ccy image, in each LXC container, and in a docker member.
- H9. Rollback drill: a seat used by a v2 session, then by a v1 session, then by v2 again, with
  its inbox and history intact.
- H10. After each stage: the H1 timings again.
- H11. On the host, a v2 `pingbus seat disconnected` against a v1 session's claim and
  `said-goodbye.json`, and the reverse: one notice per claim, never both.
- H12. After stage 5, `registry.json` is still version 2 and the Python admin tool reads it.
- H13. A running LXC session keeps its kit version across a play that installs a new kit.

---

## 17. Departures from v1 already chosen by this design

Numbered for the stage specs to carry; each needs no owner decision beyond section 14.

- B1. Reads of members and devices run no process as root (stage 4).
- B2. `human devices` is no longer what ccy-teams reads; the reader carries a count and the newest
  last-seen time. The command stays for a human.
- B3. The installer refuses to run while another installer command holds the install lock, naming
  it, after a bounded wait.
- B4. The applied stamp's software digest is the deployed binaries' source digest.
- B5. `send` with a running worker does not re-verify the room by itself.
- B6. The reader answers every team in one process; one team's failure is reported in its own
  entry and the others are answered.
- B7. `goodbye.json` (per session home) is dropped at stage 2; `said-goodbye.json` stays.
- B8. A seat row whose handle the team's roster does not hold is shown as unknown.
- B9. The host and LXC kits are installed one directory per version and resolved once at launch.
- B10. The stage-2 kit's Go `pingbus` implements `hook` and `status-line` and executes
  `pingbus.pyz` for every other command, until stage 6a; from 6a only `watch`, `seat exec` and
  `seat keep` are executed in `pingbus.pyz`, until 6b.
