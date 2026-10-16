# Plan 00203: is the agent team bus on the right technology stack?

Read-only research. No code was changed. The owner's question, verbatim: "this is error hiding
and fallback - its a shit pattern - single correct code path - its on us to MAKE THE SERVERS
RELIABLE - why the hell are they crashing? do we need a different tech stack?"

Sources are named inline. Repository sources are paths in this checkout at F44 `741191c3`.
Upstream Tuwunel facts come from its GitHub issues and releases (read through the GitHub API
on 2026-10-16) and from its `v1.9.4` source. Nothing here was measured on the host. The
host's real crash history is a separate triage, and section 6 says which of its findings
decide the question.

## 0. The short answer

- **The servers are not known to be crashing.** Nothing in this repository records a Tuwunel
  crash on the host. Every homeserver restart the journals record was deliberate: a
  `play-agent-bus.yml` run that upgraded Tuwunel or changed a team's config, for example
  Plan 00201's journal on 2026-10-15 ("play-agent-bus.yml (backup, Tuwunel 1.9.4, homeserver
  restarts): PASS"). The robustness audit's "watcher dies when the homeserver restarts" is
  about those restarts. It does not show crashes. The triage may find crashes; until then,
  "why are they crashing" has no evidence behind it either way.
- **The fallback chain is not caused by Matrix.** The session-side chain has five starters:
  the keeper, SessionStart, the UserPromptSubmit/Stop revive, the `--supervise` parent, and
  `pingbus wait`. Their job is to keep a *local watcher process* alive. Any broker needs the
  same session-side process to carry messages into Claude Code's inbox socket. Changing the
  broker does not remove that process, so it does not remove the chain.
- **The "one correct path" already exists, and it is a Matrix feature.** `/sync?since=<token>`
  is reconnect-and-resume-from-a-cursor. Since CCY 3.98.12 (Plan 00194 Task 2.0d, SPEC-inject
  E25) each team's thread in `pingbus watch` runs one loop, start included: sync from the
  stored token, and on a transient fault wait with a backoff and sync again from the same token
  (`helpers/pingbus/cli.py` `_watch_poll`). That is the single path the owner asks for. The
  layers on top of it exist because the watcher *process* has no owner whose life is the
  session's.
- **Recommendation (moderate confidence, about 65%): keep Matrix and Tuwunel for now. Make the
  session side one path, by tying the watcher to a process whose life is the session's, and
  delete the revive and `wait` layers for socket sessions.** Re-decide once the triage reports.
  Two things are Tuwunel risks that the triage should look for: RocksDB recovery and stalls
  on a copy-on-write filesystem, and a full disk (section 2.3).

## 1. What the bus actually needs

Each requirement is stated with where it comes from. "Hard" means the design breaks without
it. "Soft" means it is wanted but could change.

| #   | Requirement                                                                                                                                                                                                     | Source                                                                                                                                                                                                                         | Hard or soft                                                                                     |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| R1  | One machine in v1: the desktop, ccy (rootless podman), cc on the host, LXC, docker and VM guests                                                                                                                | 00161 DESIGN.md "v1 scope" (D46)                                                                                                                                                                                               | hard for v1                                                                                      |
| R2  | Multi-host later, over WireGuard ("elsewhere is just a URL")                                                                                                                                                    | issue #59 section 1; DESIGN.md "later phase"; 00203 Non-Goals exclude it now                                                                                                                                                   | soft: kept as a design, not built                                                                |
| R3  | Per-seat identity and authentication that a session cannot forge. A peer must not post as another seat                                                                                                          | DESIGN.md section 0 (the threat model: every agent holds its own token where it runs arbitrary code)                                                                                                                           | hard                                                                                             |
| R4  | Role authority: who may send which verb; only listed humans' text reaches agents; the record writable only by the team admin                                                                                    | protocol §5, §7, §8 (power levels, `agent_bus.team` record)                                                                                                                                                                    | hard                                                                                             |
| R5  | Durable history and replay. The room, not the local cache, is a seat's record (`pingbus history`), and a session that was down catches up                                                                       | `helpers/pingbus/syncer.py` docstring; protocol §9                                                                                                                                                                             | hard                                                                                             |
| R6  | Delivery: at least once from the server, each item delivered once to the session (de-duplicated by event ID, the sync token saved only after the inbox write is durable)                                        | protocol §9; SPEC-inject E25, R3                                                                                                                                                                                               | hard                                                                                             |
| R7  | Replies and threads (`say --re`, `m.in_reply_to`), read receipts on delivered human messages                                                                                                                    | commit `ddc8275e` (Plan 00201 UX fixes)                                                                                                                                                                                        | soft but in use                                                                                  |
| R8  | **Human participation in a real client**, desktop and phone: read the whole room, write to agents by mention                                                                                                    | issue #59 ("desktop clients so a human can watch"); 00161 PLAN.md ("Humans watch through Element, from a desktop or a phone"); `ccy-teams` "Open every team in Element" (`ddc8275e`); the `research/element-one-window` branch | **hard in practice**: the owner asked for the Element integrations, and the research in progress |
| R9  | Isolation between teams: accounts, admin, backup and placement per team; a team removable or movable without touching another                                                                                   | DESIGN.md section 2 ("One instance per team"); issue #59 ("strong isolation")                                                                                                                                                  | soft: a decision, not a law                                                                      |
| R10 | Nothing leaves the machine; nothing outside reaches in                                                                                                                                                          | issue #59 "Privacy"; DESIGN.md sections 3.3, 9, 10                                                                                                                                                                             | hard                                                                                             |
| R11 | Scale: a handful of sessions per team, a roster limit of 64 agents (H27), a few messages a minute. Pings are pointers, not content                                                                              | SPEC-hot-control H27; issue #59 "pings only"                                                                                                                                                                                   | a fact                                                                                           |
| R12 | Wake an idle Claude Code session with no keypress, through its inbox socket (`crossSessionInbound`)                                                                                                             | DESIGN.md section 6; SPEC-inject                                                                                                                                                                                               | hard, and **independent of the broker**                                                          |
| R13 | Client code under this repository's rules: helpers are stdlib-only Python (`helpers/CLAUDE.md`), and new tooling is Go (CLAUDE.md, "Tooling Is Go"). A standalone Python CLI is ported to Go when next reworked | CLAUDE.md                                                                                                                                                                                                                      | hard constraint on any rewrite                                                                   |

R8 is the requirement that decides most of the comparison. If the owner reads and writes the
bus in Element, on a desktop or a phone, any non-Matrix broker means building and keeping a
human client as well. If the owner does not need that, the comparison changes (section 6).

## 2. Is Matrix with Tuwunel a sound fit?

### 2.1 What Matrix gives for free

Against section 1, Matrix covers R3 to R8 with stock parts:

- per-account tokens (R3);
- room power levels and a state event only the admin can write (R4);
- the room's history, with `/messages` for back-fill (R5);
- `/sync?since=` as a resumable cursor, with transaction IDs for an idempotent send (R6);
- replies and receipts (R7);
- Element on desktop and phone (R8).

R12 is not Matrix's job in any design.

### 2.2 What it costs: the moving parts

These parts belong to the Matrix choice, or to how it was deployed. Each one is a failure
surface that the robustness audit or the journals have met.

1. **One homeserver per team** (R9). N processes, N databases, N backup timers, N health
   timers, N firewall rule sets. A play run that changes the rendered config restarts every
   changed homeserver (`helpers/agent_bus/config_restart.py`). This is the restart that killed
   watchers before E25.
2. **Long-poll HTTP sync.** Each team's connection is dropped on every server restart. This is
   normal for any network broker. The client must reconnect, and since E25 it does.
3. **Tokens minted through the admin API.** A restore from an older backup, or `rotate-token`
   on a held seat, gives a 401 that no retry fixes (audit 3.9, F7). Any broker with per-seat
   credentials has this class. It is not special to Matrix.
4. **Rate limits.** The server's 429 is honoured (`matrix.py`). Tuwunel's login limit
   (`render.LOGIN_LIMITS`) can lock a human out, cannot be read or reset, and is only cleared
   by a restart (server-robustness report, gap 10).
5. **Room trust.** The client re-verifies the room's create event, power levels and team
   record (`syncer.py`), because Matrix lets any account create rooms and invite. This is
   real code, and it is there because of Matrix's open model.
6. **The database (RocksDB, embedded).** See 2.3.
7. **An upstream that releases often, with migrations.** Releases from v1.5.1 (2026-03-07) to
   v1.9.4 (2026-10-10): eleven tags in seven months, from the GitHub releases API. Several
   carried schema migrations (issues #453, #586) or regressions (#598).

### 2.3 Known Tuwunel stability issues, with dates

From `matrix-construct/tuwunel` issues, read on 2026-10-16. Only the issues that bear on this
deployment are listed: federation off, no media, a few users, a static release binary, and
very likely btrfs. The render sets `rocksdb_allow_fallocate = false` (`render.py`), which is
Tuwunel's advice for a copy-on-write filesystem. Fedora installs onto btrfs by default. The
host's filesystem was not checked.

| Issue                                                                                                                                                                                          | Opened, state              | What it says                                                                                                                                                                                                                                                                                                                                                                        | Bearing here                                                                                                                                                                                                                                                                                                                                                                                                           |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [#653](https://github.com/matrix-construct/tuwunel/issues/653) "Startup hangs forever at 100% CPU during WAL recovery after unclean shutdown"                                                  | 2026-10-12, open           | On 1.9.3 and 1.9.4: after a SIGKILL, the next start spins at 100% of one core and never binds its listener. The reporter's database is on btrfs (spinning-disk RAID1). The reporter found a recovery procedure with no data loss: start once without the WAL, stop cleanly, put the WAL back, and replay it with `rocksdb_recovery_mode = 3`. One reporter, no maintainer reply yet | **The most relevant open issue.** It is our pinned version and probably our filesystem type. The new unit (`WatchdogSec=30s`, `TimeoutAbortSec=30s`, the health check's restart) turns a hang into an unclean kill. Under this bug, an unclean kill can turn into a start that never finishes, so every restart would hang again (`Type=notify`; the start would time out at systemd's default and be retried, a loop) |
| [#649](https://github.com/matrix-construct/tuwunel/issues/649) "Full disk: uploads fail as Database error, writes panic, and an acknowledged event can be lost"                                | 2026-10-11, open           | On 1.9.4, near a full disk: messages are still acknowledged for a while, then every write panics a worker (RocksDB `kFatalError` on the WAL). The report says an acknowledged event can be lost                                                                                                                                                                                     | The audit's disk-full row (2.1). With `Restart=always` it becomes a loop that does not end until space is freed. The health record does not watch free space                                                                                                                                                                                                                                                           |
| [#572](https://github.com/matrix-construct/tuwunel/issues/572) "tuwunel locks up / RocksDB Write Stalling"                                                                                     | 2026-09-02, closed         | Three users, a WAL that grows, no messages in or out, a restart needed. The maintainers could not reproduce it. The reporter closed it, blaming "suboptimal zfs configuration" (also copy-on-write)                                                                                                                                                                                 | A hang that the client API probe in `health.py` would catch and restart. Weak evidence: one report, closed as environmental                                                                                                                                                                                                                                                                                            |
| [#598](https://github.com/matrix-construct/tuwunel/issues/598) "1.9.2 ERROR buffer underrun when syncing from Element-X"                                                                       | 2026-09-21, closed 09-23   | A 1.9.2 regression: sync returned 500 for join rows written before 1.4.3.1. Fixed on main, then released                                                                                                                                                                                                                                                                            | Not ours: our databases were created on 1.9.x. It shows how fast regressions arrive with point releases                                                                                                                                                                                                                                                                                                                |
| [#602](https://github.com/matrix-construct/tuwunel/issues/602)                                                                                                                                 | 2026-09-24, closed         | Migrated OAuth sessions were hard-logged-out when 1.9.2 adopted token expiry                                                                                                                                                                                                                                                                                                        | Not ours: our member tokens are minted by the admin API, not OAuth. But it shows a token's lifetime can change in an upgrade, which would make every seat's 401 permanent                                                                                                                                                                                                                                              |
| [#586](https://github.com/matrix-construct/tuwunel/issues/586), [#453](https://github.com/matrix-construct/tuwunel/issues/453), [#452](https://github.com/matrix-construct/tuwunel/issues/452) | 2026-05 to 2026-09, closed | 1.9.0's short-id migration warning; a schema mismatch after migrating from Conduit; a main-thread panic after updating to 1.6.2                                                                                                                                                                                                                                                     | Upgrade risk, already met by the play's backup before each upgrade                                                                                                                                                                                                                                                                                                                                                     |
| [#454](https://github.com/matrix-construct/tuwunel/issues/454)                                                                                                                                 | 2026-05-10, closed         | An asynchronous media upload could deadlock 1.6.x                                                                                                                                                                                                                                                                                                                                   | Not ours: we use no media                                                                                                                                                                                                                                                                                                                                                                                              |

Upstream at the time of reading: 2,629 stars, 66 open issues, last push 2026-10-15. It is
active, and small enough that one person's bug report can sit with no reply (#653).

**My reading.** I found no report of Tuwunel crashing on its own in a deployment like ours
(no federation, no media, light traffic). The open risks are in the storage layer: an
unclean kill (#653), a full disk (#649), and stalls on copy-on-write filesystems (#572,
#653). Those are also exactly what a watchdog that kills, and a restart on every exit, can
make worse.

### 2.4 Verdict on fit

- **Matrix is a sound fit for R3 to R8**, and R8 rules out most of the alternatives on its own.
  It is more protocol than a handful of sessions needs. The extra parts, such as the room
  trust checks and the admin API, are code already written and tested.
- **Tuwunel is a reasonable server but a young one.** Its weak point is its embedded RocksDB
  on btrfs. That is a property of the storage, not of Matrix.
- **One server per team multiplies every server-side part.** It is the part of the current
  design I would question first (alternative A).

## 3. Alternatives, judged against the requirements

The point the owner made, stated exactly: **a client whose correct path is "connect, read from
my cursor, and on any error wait and do it again from the same cursor" has one path.** Its
start, its normal running and its recovery are the same code. A server restart is just one
more trip round the loop. That is not a fallback. A fallback is a *second* mechanism that
takes over when the first one has died: a supervisor that restarts the watcher, a hook that
restarts the supervisor, and then `pingbus wait`, which the agent is told to run when all of
those have failed.

**How the current chain stands against that, honestly:**

- E25 made the Matrix side one path. A homeserver restart, a 5xx, a refused connection, or a
  homeserver that is down when the watcher starts all go round the same loop
  (`_watch_poll`). Only a permanent answer (401, 403, lost trust) leaves it.

- What is left is process supervision, with three layers above that loop:

  1. `watch --supervise` restarts the watcher after any exit that is not final (E26).
  2. UserPromptSubmit and Stop restart the supervisor (E26). This only happens when a hook
     event fires, so an idle session gets none (audit gap 1).
  3. `watcher_failed` tells the agent to run `pingbus wait` (the 00161 M2.3 acceptance proves
     this path).

  Layers 2 and 3 are fallbacks in the owner's sense. Layer 1 is borderline: a supervisor that
  restarts a crashed child hides the crash unless the crash is reported loudly.

- **Why the layers exist:** the watcher is a detached process (`start_new_session=True`). The
  hooks start it, and since M3 the keeper starts it too (H11). Nothing whose life *is* the
  session owns it. So "the watcher is gone and the session is not" is a reachable state. Each
  layer was added to leave that state again. No choice of broker changes this.

For each alternative below: the reliability model, whether "the watcher dies on a server
restart" goes away, and how it meets R3 to R10.

### A. One homeserver for every team (Tuwunel, or Synapse)

- **Model:** one process, one database, one backup, one health check. Teams become rooms with
  their own accounts. A server restart drops every team's long poll at once, and E25 resumes
  each one from its own token.
- **The watcher-death class:** unchanged. It is already closed by E25 for transient faults,
  in any number of servers.
- **Requirements:** R3 to R8 the same. R9 weakens. Today each instance allows exactly one room
  (D63) and a team's token cannot reach another team. On a shared server, an agent's token
  can create rooms and invite any account on the server. Room trust still stops a member from
  acting on a foreign room, but the hard wall between teams becomes a check in our client
  (and room creation would need to be restricted for agents: an unverified Tuwunel config
  question). Moving a team to another host becomes an export instead of moving a directory.
- **Synapse instead of Tuwunel:** the reference server, mature, written in Python. It wants
  PostgreSQL for production, which is one more service. That trades RocksDB's risks for
  PostgreSQL's operations. I have no crash-rate comparison between the two, and I will not
  invent one.
- **Cost:** moderate. `render.py`, `admin.py`, the installer and the backup and health units
  change. pingbus barely changes: the room ID and server URL come from the bundle as now.
- **Judgement:** it lowers the server-side surface N-fold and keeps Element. It costs the
  isolation that issue #59 called "decided". Worth doing **if** the triage shows real
  server-side trouble that scales with the number of instances. Not worth it for restarts
  alone, because the client already survives those.

### B. NATS with JetStream

- **Model:** one Go binary. JetStream streams store messages on disk, and a *durable
  consumer* keeps each reader's acknowledged position on the server, so a client that comes
  back resumes where it left off. Official NATS clients reconnect on their own. Sources: the
  NATS blog on JetStream consumers ([nats.io/blog/jetstream-java-client-03-consume](https://nats.io/blog/jetstream-java-client-03-consume/)),
  which says a restarted consumer "will automatically restart where it left off", and
  Synadia's [reliable delivery post](https://www.synadia.com/blog/jetstream-reliable-delivery-dlq-replay).
  I could not reach the matching pages on docs.nats.io; the site's layout has changed.
- **The watcher-death class:** the server side is the same as Matrix with E25: a cursor and
  a reconnect. The client libraries do the reconnect, so we would write less of it. The
  process-supervision layers are untouched.
- **Requirements:** R3 by per-user credentials (nkeys or JWT) with per-subject publish
  permissions, which is a stronger "who may send what" than Matrix power levels. R4's human
  text authority and the team record must be rebuilt. R5 and R6 yes. **R7 and R8: no.** No
  human client exists; the owner would get a CLI or a TUI that we write. R13: no stdlib
  Python client, so pingbus's transport moves to Go (which CLAUDE.md wants anyway).
  JetStream's file store has its own recovery code paths. Changing stores moves the storage
  risk; it does not remove it.
- **Cost:** high. A new transport, a new trust model, a human client, and a re-proven
  installer and acceptance.

### C. A small purpose-built Go broker over unix sockets, with an append-only log

- **Model:** one host daemon per machine, or a socket per team. Each seat gets its own unix
  socket file, bind-mounted only into that seat's container. Possessing the socket is the
  credential, which fits rootless podman: every ccy container runs as the same host uid, so
  `SO_PEERCRED` cannot tell seats apart, but a per-seat socket path can. The daemon appends
  to an fsync'd log. A client sends `read from offset N` and gets entries, then blocks.
- **Socket activation** (systemd holds the listening socket): during a daemon restart new
  connections wait in the kernel backlog instead of being refused. A restart becomes delay,
  not an error. Connections already open still drop, so the client still loops.
- **The watcher-death class:** the server side shrinks to code we own, with no migrations and
  no rate limits. The client loop is the same shape as E25. The process supervision is the
  same.
- **Requirements:** R1 yes for podman and LXC, which can bind-mount a socket. Docker and VM
  guests need TCP or vsock, so a token comes back for them. R2 (multi-host) needs TCP and TLS:
  a second transport. R3 yes for socket members, by possession. R4 to R6 must be written,
  but they are small at this scale. **R7 and R8: no human client.** R13: Go, which fits.
- **Cost:** the highest in engineering and the lowest in moving parts. We would own fsync,
  log rotation and compaction, the cursor format, and the auth model. Each of those is a place
  a bus written in-house goes wrong in ways a mature server has already met.
- **Judgement:** the most reliable thing *we could control*, if R8 were dropped. With R8 kept,
  it would need a Matrix bridge for humans. That puts Matrix back as a moving part, plus a
  bridge.

### D. Plain files and inotify

- **Model:** no server at all. Each team has a directory of append-only files, one per
  writer. A reader keeps a byte offset per file and waits on inotify. A "server restart"
  cannot happen.
- **The watcher-death class:** the server half goes away completely. The reader is a loop over
  offsets: one path. The process supervision is unchanged.
- **Requirements:** **R3 fails.** All rootless ccy sessions write as the same host uid, so a
  session that can write its own file can write any other seat's file. Per-writer ownership
  needs distinct uids, which rootless podman does not give. Signing every line with a
  per-seat key would restore R3, which is building a protocol again. inotify reaches bind
  mounts on the same kernel (podman, LXC) but not VM guests. No R7 or R8. Concurrency
  (partial lines, append atomicity beyond `PIPE_BUF`, rotation) is ours to get right.
- **Judgement:** it removes the server and fails the threat model. Not credible on its own.

### E. systemd socket activation (a technique, not a broker)

It applies to any of the above, Tuwunel included. **Tuwunel 1.9.4 accepts listeners passed by
systemd**: `src/router/serve.rs` `systemd_listeners()` reads `sd_notify::listen_fds()` and
takes TCP and unix sockets, logging them as `passed:tcp:` and `passed:unix:`. That is read
from source. It has not been tried here. Its interaction with the unit's `SocketBindDeny=any`
and `IPAddressAllow` rules is untested.

- **Gives:** connections made during a restart wait instead of being refused. The window in
  which the client sees errors shrinks. It also orders cleanly at boot: the socket exists
  before the server is ready.
- **Does not give:** the open long poll is still cut, so the client loop is still needed. It
  does nothing for a hung server.
- **Cheap to try** on the current stack.

### F. D-Bus

- A message bus with no persistence. A signal sent while the receiver is down is gone, so R5
  and R6 fail. Reaching a host bus from rootless containers means mounting the bus socket,
  which exposes far more than the bus. **Not a fit.**

### G. Redis streams (Valkey on Fedora)

- **Model:** `XADD` to append. `XREAD BLOCK <ms> STREAMS <key> <last-id>` returns entries after
  the last ID seen, which is a cursor. Consumer groups with `XACK` keep a server-side position
  per reader ([redis.io streams](https://redis.io/docs/latest/develop/data-types/streams/)).
  Persistence is through AOF or RDB. ACLs can limit a user to key patterns. Fedora replaced
  Redis with Valkey from Fedora 41 because of Redis's licence change
  ([Fedora change page](https://fedoraproject.org/wiki/Changes/Replace_Redis_With_Valkey),
  [Phoronix](https://www.phoronix.com/news/Fedora-Replacing-Redis-Valkey)).
- **The watcher-death class:** the same as B. A cursor client is one path. The supervision is
  unchanged.
- **Requirements:** R3 by ACL users per seat, with key-pattern permissions. R4 to be built.
  R5 and R6 yes. No R7 or R8. The RESP protocol is simple enough to write in stdlib Python,
  so R13 does not force Go here. Durability depends on the `appendfsync` setting; the default
  `everysec` can lose about a second on a crash.
- **Judgement:** simpler to run than Tuwunel, with the same human-client gap as B and C.

### Summary table

| Option                          | Server parts            | Restart becomes                  | R3 per-seat auth                     | R8 human client | Session-side chain | Migration cost |
| ------------------------------- | ----------------------- | -------------------------------- | ------------------------------------ | --------------- | ------------------ | -------------- |
| Today: Tuwunel per team         | N servers, RocksDB each | a dropped poll, resumed (E25)    | yes (tokens)                         | Element         | unchanged          | none           |
| A. One Tuwunel or Synapse       | 1 server                | the same                         | yes; team wall becomes a check       | Element         | unchanged          | moderate       |
| B. NATS JetStream               | 1 server, file store    | the same; the library reconnects | yes (nkeys/JWT, subject permissions) | none, build one | unchanged          | high           |
| C. Go broker, unix sockets, log | 1 small daemon we own   | a delay with socket activation   | by socket possession; tokens on TCP  | none, build one | unchanged          | highest        |
| D. Files and inotify            | none                    | cannot happen                    | **fails** (one uid)                  | none            | unchanged          | high           |
| E. Socket activation            | adds a `.socket` unit   | a delay, not an error            | n/a                                  | n/a             | unchanged          | low            |
| F. D-Bus                        | the bus                 | messages lost                    | weak                                 | none            | unchanged          | n/a            |
| G. Valkey streams               | 1 server, AOF           | a dropped read, resumed          | yes (ACL users)                      | none, build one | unchanged          | high           |

The "Session-side chain" column is the same in every row. That is the main finding: **the
fallback chain the owner objects to is a client-side process lifecycle problem, and no broker
removes it.**

## 4. What one correct path on the session side would be

This is not code. It is the shape the owner's rule points to, so the choice of stack is not
mistaken for the fix.

- **One owner for the watcher, whose life is the session's.** In a ccy container, PID 1 is
  `pingbus seat exec`, then the PTY supervisor, then `claude`; the keeper is resident too
  (H11). The watcher should be a child, or a thread, of the process that lives exactly as
  long as the session. In a container, that makes "the watcher is gone, the session is not"
  unreachable, or a crash of the session itself (fail fast, visible). On the host (`cc`), the
  same holds if the owner is the `seat exec` parent that audit F5 proposed.
- **One loop inside it:** E25 as it is now. A transient fault is a trip round the loop and is
  logged once. A permanent one ends that team, loudly, and reaches the owner (audit F7).
- **No revive in hooks and no `pingbus wait` for socket sessions.** For members with no inbox
  socket (relay mode, some non-ccy members), `wait` is *their* single path, not a fallback.
- **An unexpected exception in the watcher is a bug to report, not a thing to restart past.**
  If a restart is kept at all, it should be the owner process restarting its child with each
  crash recorded where the board shows it. It should not be layered.
- **Fix the deliberate restarts at the source.** The play restarts a homeserver only when its
  config or binary changed (`config_restart.py`). That is right, and with E25 it is harmless.

This change touches `helpers/pingbus/cli.py` (`_supervise`, `cmd_watch`), `helpers/pingbus/hooks.py`
(the revive), the keeper in `seat.py`, and SPEC-inject E26. It is about the same size whatever
the broker is.

## 5. The cost of migrating, and what survives

pingbus is about 11,200 lines of Python. agent_bus is about 5,900, and `agent-bus-install`
is about 1,180 lines of bash on the ratchet allowlist.

**Survives any broker:**

- the protocol's validation and grammar: `protocol.py`, `envelope.py`, `limits.py`, the
  forge check `forge.py`;
- the local inbox and its de-duplication: `inbox.py`, `sentlog.py`;
- delivery into Claude Code: `inject.py`, `notify.py`, `relay.py`;
- seats and membership: `seat.py`, `membership.py`, `config.py`, `bundle.py`;
- the hooks and the status line: `hooks.py`, `statusline.py`;
- the ccy launcher, ccy-teams and hot control (Go, `src-go/internal/teams`), apart from the
  login and Element parts.

**Replaced by a non-Matrix broker:**

- `matrix.py` (328 lines) and `syncer.py` (611): the transport and room trust;
- the transport half of `cli.py`;
- `agent_bus/admin.py` (917), `render.py` (222), `element.py` (328), `health.py` (483),
  `config_restart.py`, and most of the installer;
- protocol §8 (the room, power levels and the team record) and §12 (the bundle), rewritten;
- the Element integration, and in its place a human client to design, build and support;
- Plans 00161 and 00203's acceptances, re-proven.

For option A, only the installer, `render.py`, `admin.py` and the backup and health units
change. pingbus is close to unchanged.

Under R13, a broker change also means writing the new transport in Go, so in practice
pingbus is ported to Go as part of it. That is a rewrite of most of the client, not a swap of
one module.

## 6. Recommendation, confidence, and what would change it

**Recommendation.**

1. Keep Matrix and Tuwunel. Do not migrate before the triage reports.
2. Make the session side one path (section 4). That is what the owner's rule asks for, and it
   is needed on every stack.
3. Cover the two storage risks on the current stack:
   - #653: an unclean kill can make the next start hang. Check whether the database is on
     btrfs. If it is, consider a no-copy-on-write directory for the database (`chattr +C` on
     an empty directory, which is what RocksDB's warning is about). Prove on a scratch copy
     that a killed Tuwunel 1.9.4 starts again. The unit sets no `TimeoutStartSec`, so systemd's
     default applies. A start that times out is killed and, under `Restart=always`, started
     again. Record a start timeout in the health record by name, so this loop shows on the
     board as itself and not as a plain restart count.
   - #649: add free space to the health record.
4. Try socket activation (option E) as a cheap way to turn a restart into a delay.

**Confidence: moderate, about 65%.** The argument against migrating is strong on the session
side: the chain is not the broker's fault. It is weaker on the server side, because there is
no host data yet.

**What would change it. Findings from the crash triage that decide the question:**

| Triage finding                                                                                                                   | What it means                                                                      | Decision                                                                                              |
| -------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Every `NRestarts` and every stop in `journalctl -u agent-bus-hs@*` lines up with a play run, a config restart or a reboot        | The servers do not crash. The pain was the client before E25 and the layered chain | Keep the stack. Do section 4                                                                          |
| Tuwunel panics (`panicked at`, a worker panic, exit by signal 6 that is not the watchdog's) in code paths we use, more than once | A server defect we cannot fix                                                      | Report it upstream with the trace. If it repeats across releases, move toward A with Synapse, or C    |
| Watchdog aborts (`Watchdog timeout`), or the health check's restarts, with RocksDB stalls or a growing WAL before them           | Storage stalls, probably copy-on-write                                             | Fix the storage first (no-CoW directory). Re-measure. A different server only if it continues         |
| A start after an unclean kill that never reaches `READY=1` (100% CPU, no listener): the #653 signature                           | The kill-to-recover policy can cause a permanent outage                            | Mitigate as in step 3 now. If it is seen on the host, it weighs heavily against RocksDB-based Tuwunel |
| `No space left on device` near a stop                                                                                            | #649's class                                                                       | Disk monitoring; not a stack question                                                                 |
| Watcher deaths on the session side with the homeserver up the whole time                                                         | Client bugs                                                                        | Section 4, and fix each bug. Not a stack question                                                     |
| Restarts that scale with the number of teams (each instance with its own trouble)                                                | Per-team multiplication                                                            | Option A                                                                                              |

**One owner answer would also change it.** If the owner does not need to read and write the bus
from Element on a desktop or a phone (R8), then option C (a small Go broker with per-seat
sockets and an append-only log) becomes the best long-term fit: the fewest moving parts, all
of them ours, and in the language this repository now requires. Even then, it is a rewrite of
the transport and the trust model, and it would not by itself remove the session-side chain.

## Sources

- Repository: `helpers/pingbus/cli.py` (`_watch_poll`, `_supervise`, `cmd_watch`),
  `helpers/pingbus/matrix.py`, `helpers/pingbus/syncer.py`, `helpers/agent_bus/render.py`,
  `helpers/agent_bus/config_restart.py`, `files/etc/systemd/system/agent-bus-hs@.service`;
  `docs/agent-bus-protocol.md`; Plan 00161 `DESIGN.md` (sections 0 to 2, 6, D46, D63);
  Plan 00194 `SPEC-inject.md` E25, E26; Plan 00203 `PLAN.md`, `SPEC-hot-control.md` H11 to
  H14, H27, `subagent-reports/261016-robustness-audit-opus.md`,
  `subagent-reports/261016-server-robustness-opus.md`, the 2026-10-16 journal; Plan 00201's
  2026-10-15 journal; commit `ddc8275e`.
- GitHub issue LongTermSupport/fedora-desktop#59 (the original proposal; why Matrix: human
  clients, "elsewhere is just a URL", one homeserver per team "decided").
- Tuwunel: issues [#653](https://github.com/matrix-construct/tuwunel/issues/653),
  [#649](https://github.com/matrix-construct/tuwunel/issues/649),
  [#572](https://github.com/matrix-construct/tuwunel/issues/572),
  [#598](https://github.com/matrix-construct/tuwunel/issues/598),
  [#602](https://github.com/matrix-construct/tuwunel/issues/602),
  [#586](https://github.com/matrix-construct/tuwunel/issues/586),
  [#453](https://github.com/matrix-construct/tuwunel/issues/453),
  [#452](https://github.com/matrix-construct/tuwunel/issues/452),
  [#454](https://github.com/matrix-construct/tuwunel/issues/454);
  [releases](https://github.com/matrix-construct/tuwunel/releases);
  `src/router/serve.rs` at `v1.9.4` (`systemd_listeners`).
- NATS: [JetStream consumers, NATS blog](https://nats.io/blog/jetstream-java-client-03-consume/);
  [Synadia, reliable delivery](https://www.synadia.com/blog/jetstream-reliable-delivery-dlq-replay).
- Redis streams: [redis.io](https://redis.io/docs/latest/develop/data-types/streams/).
  Valkey on Fedora: [Fedora change](https://fedoraproject.org/wiki/Changes/Replace_Redis_With_Valkey),
  [Phoronix](https://www.phoronix.com/news/Fedora-Replacing-Redis-Valkey).
