# SPEC: N3, the member client, LXC members first (Plan 00209 Task 3.1)

The contract for N3's build slices. Sources: area B (`DESIGN-v2-B-member.md` 2.1-2.15), C 3.9 (LXC
members) with the parts of C 3.5.1, 3.7.5, 3.11.2 step 6 and 3.14 it names, and A where B cites it.
It is specified against the packages N1 and N2 built (`paths`, `proto`, `matrix` with `fakehs`,
`seat`, `notice`, `health`, `state`, `teamlock`, `status`, `execx`); section 9 names every gap
in their APIs and how it is closed. v2 shares nothing with v1: v1's Python is a prototype, so
nothing here is a parity rule against it.

Each expectation is numbered and gets a Go test written first and seen to fail (B 2.15), on the
fakes of B 2.15 with an injected clock; no test sleeps. A test against a second real process
re-runs the test binary (section 9, API gap 8). `-race` throughout, 90% coverage per package.

**Gates** (PLAN.md Task 0.5). Everything in N3 is inert code until the owner confirms: nothing
installs, nothing runs as root on the host, no unit is placed. An expectation marked **[A]** has
a host part (a play task, a unit, a `teamchat` name written on the host or into a guest on the
host) that runs only in the VM lab until gate A is cleared; its code merges inert. **[B]** marks
a host run that gives a process a pool seat's password, which waits for gate B. N3's LXC path
never does: an LXC seat is a token seat (C 3.6.3 `--credential token`), so the only **[B]**
expectation here is the hot reissue's host run for a ccy seat (LN9), which is Task 4.4's.

## 0. The slices, in order

| Slice | Lands                                                                                                                                                                                                                                                           | What works after it                                                                                                           | Needs                 |
| ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | --------------------- |
| n3a   | `cmd/teamchat`; `member/cli` (the whole verb table, the parser, records, errors, exit codes, `version`, `help`, the launch-line parser); `member/state` (every member file's codec and the store writer); `member/lock`; the guest operations; `seat set-aside` | the guest side of an LXC member: bundles and desired membership placed and removed through `lxc-attach` as the agent user     | N1, N2                |
| n3b   | `teamchat-admin lxc apply` and `lxc restart` (`host/lxc`): the guest root, the kit and unit writes, the attach, the guest's systemd over its private socket, the LXC record, the host's desired copy; `health.Guests`'s production probe                        | an LXC member placed end to end by `lxc apply` in the VM lab, its unit rendered and started                                   | n3a                   |
| n3c   | `run` (the keeper), the link, the link socket, the store engine, the sync engine, `recv`, `wait`, `inbox`, `status`, `verify`, `history`, the ends and their notices, `seat disconnected`, hot control                                                          | an LXC member under `run --plain` receives a human's and a peer's items end to end (VM lab, with n3b)                         | n3a                   |
| n3d   | the sends: `reply`, `say`, `ping`, the send gate, the outbox and TIMEOUTs, the forge check                                                                                                                                                                      | the LXC member answers and starts conversations: a whole exchange with `--plain`                                              | n3c                   |
| n3e   | delivery into Claude Code (inbox socket, envelope, transcript proof, read receipts), the hooks, the status line, the kit's files (plugin, `hooks.json`, `settings.json`, the skill)                                                                             | an LXC Claude Code session under `teamchat-session-<user>.service`, woken by deliveries: Task 3.4's LXC member on a real team | n3d; n3b for the unit |
| n3f   | the side rooms of Plan 00211 as verbs: `report`, `ask`, `thread`, `questions`, the questions room's sync rules                                                                                                                                                  | the full verb set                                                                                                             | n3d                   |

n3b and n3c are independent once n3a has merged, and may be built at once. Each slice is
reviewed to MERGE on its own and merges into the plan branch; the stack reaches F44 with its
first installed caller (Task 3.3 or 4.1), under gate A. Task 3.4 (acceptance) follows n3e for
the LXC exchange and n3f for the full suite.

## 1. Rules every slice keeps

- **TB1. Opening a guest's or a member's file from the host.** Host code opens a path below a
  guest only through n3b's guest root (LX3): `openat2` from the root fd with
  `RESOLVE_IN_ROOT|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS` (and `RESOLVE_NO_XDEV` for a write),
  a clean absolute path with no `..` component. A read opens `O_PATH`, `fstat`s it, refuses
  anything but a regular file by name, then reopens that inode through its own `/proc/self/fd/<n>`
  `O_RDONLY|O_NONBLOCK|O_CLOEXEC` and checks device and inode again; it reads at most the cap
  from that fd. So no host code ever opens a FIFO, socket or device for reading or writing. Test:
  a fixture tree with a FIFO, a socket and a symbolic link at every path a host step reads, and
  a writer that polls the FIFO with `O_WRONLY|O_NONBLOCK` throughout: it gets `ENXIO` every time
  (no reader ever existed), and each step refuses by name.
- **TB2. Nothing a member writes decides a host action**, beyond the four bounded effects below,
  each tested by a table that plants hostile values and asserts the host's actions:
  1. the seat lock's record (`run`, `launch_failed_ms`, B 2.5) decides only whether the host posts
     that member's own `disconnected` and its transaction id's seed, a notice the member's own
     token could post or pre-empt anyway (n3-D9); the run id is used only after its grammar;
  2. a guest's `session.pid` records decide only that a kit digest is kept (C 3.9.2 step 4);
  3. the checkout's `.git/config` and `.claude/ccy/ccy.env.local` decide the handle at a member's
     first join only; once the LXC record holds a handle, a derivation that differs is refused
     by name and never makes a new member (n3-D10);
  4. a guest operation's `CHANGED` lines are reported only; its exit code can only fail or defer.
     The agent's desired file is never read by the host (the host copy is the authority, C 3.9.3).
     No member-written value names a path the host opens, a uid it attaches as, a unit's text, or
     another member.
- **TB3. Root never runs a guest program and never writes under a home.** A guest operation is
  `lxc-attach -u <uid> -g <gid> --clear-env` with uid and gid nonzero and the uid not the desktop
  user's (C 3.9.1, K15), the program the host-written `kit/<digest>/bin/teamchat`.
- **TB4. The member's own files.** Every member file is opened `O_NOFOLLOW|O_CLOEXEC`, a read
  with `O_NONBLOCK` and an `fstat` regular-file check (`state.ReadFile`), written by `O_EXCL`
  temp file, `fsync`, rename, directory `fsync` (`state.WriteFile` or the store writer); the run
  and state directories must be the uid's, 0700, else 78.
- **CK1. One clock seam per package**: `CLOCK_BOOTTIME` milliseconds for deadlines and backoff,
  wall milliseconds for anything compared with `origin_server_ts`; a freeze is a gap between them
  (SY12).

## 2. n3a: the binary, member state, locks and the guest operations

- **CL1. One binary.** `cmd/teamchat/main.go` is thin over `member/cli`; built `CGO_ENABLED=0`
  (the build task gains the input, **[A]** for the play change); `teamchat --version` and
  `teamchat version` print the source digest. The binary links no YAML and no D-Bus module (a
  go-deps `keepout` row, as n2b's for `teamchat-admin`).
- **CL2. The verb table** is B 2.2.1's, every verb and flag, flags only after the verb (a flag
  before it is 64), no global flags, no v1 alias. A verb not yet built exits 1 `internal` naming
  the slice; every slice that builds one removes it from that list.
- **CL3. Records.** JSON Lines, `"kind"` first then `"v":1`, every record kind of B 2.2.2
  (`team-failed` included) with its fields as typed structs; text only as JSON strings. A test
  writes a body holding tabs, newlines and a forged `teamchat delivery` line and reads back one
  record.
- **CL4. Errors and exits.** B 2.2.4's table is data: each exit's `code` set and class; the error
  object is the last stderr line; `message` is a fixed template filled with grammar-checked names
  only; `help exit-codes` prints the table from the same data. A multi-team verb's partial answer
  is exit 5 with a `team-failed` record (B 2.2.3).
- **CL5. The launch line.** `run`'s parser (B 2.4.1 step 5): exactly one of `--claude`/`--plain`;
  `--kind`; under `--claude` the first argument whose base name is `claude`, at most one bare `--`
  and only directly before it; `--settings`, `--plugin-dir` or another `--` anywhere is 64;
  `--resume-recorded` with `--resume` or `--continue` is 64. A test parses every launch line the
  design spells: C 3.9.4's `ExecStart` after tmux's own arguments, ccy's
  `-- <supervisor> --arm -- claude`, host `cc`'s, and B 2.11's kinds table.
- **MS1. The member files**, each a strict codec (exact keys, `v` 1, integers not booleans or
  floats, duplicate keys refused, no file value quoted in an error, B10): `store.json` (B 2.3.2,
  with every cap), `session.json`, `keeper.json`, `report.json`, `status.json`, `conversation.json`,
  `session.pid` (B 2.3.3, 2.4.1 step 4), the seat lock record (B 2.5, both shapes), `desired.json`
  (B 2.10's keys exactly; C 3.7.5), and the bundle (MS3). Each round-trips; each cap fires before
  the 2 MiB file cap.
- **MS2. The store writer.** One atomic replace per change (B 2.3.2 option 3); the store is
  written only through a type that holds the seat's lock description (LK3). `pending` keeps 256
  entries for HUMAN and TIMEOUT items; every other full list refuses the growth it would need.
- **MS3. The bundle** (`seat.json` and `token`, B11): read by the link only, on each use; `token`
  owned by the uid, 0600, one printable line; `seat.json` in exactly one of the two shapes `seat`
  writes (a guest's `Bundle`; a session's `HeldBundle` with `claim` and `released_ms`); a password
  bundle, a duplicate user id across held seats, or a mismatch is 78 `bundle`. The token never
  reaches argv, a log or an error.
- **LK1. Three locks** (B 2.5): `flock` on files opened `O_NOFOLLOW|O_CLOEXEC|O_CREAT` 0600, each
  acquire on its own description, `LOCK_EX|LOCK_NB`; `keeper.lock` and `link.lock` tried three
  times 50 ms apart; held is a typed `Busy`; released by `LOCK_UN` then close (seat: close only).
- **LK2. Order by type.** `KeeperLock.TakeSeat(team, seat)` is the only way to a `SeatLock`; its
  description can be duplicated for the link and is never `LOCK_UN`ed.
- **LK3. Never unlinked.** No code path unlinks or renames `seat.lock`; every deletion of seat
  state (removal, the keeper's timed-out deletion, `guest remove`, `seat set-aside`) holds or
  takes the seat lock first (75 if held) and touches only the other files. Tests against a
  second real process, including a `SIGKILL`ed holder; `guest remove` and `seat set-aside`
  against a held seat leave `seat.lock` the same inode (T1).
- **LK4. The record.** After `TakeSeat`, `{"v":1,"run":..,"started_ms":..}` is written through
  the held description; on a launch failure it is rewritten with `launch_failed_ms`, still held.
- **SA1. `seat set-aside --team T`**: takes the seat lock, renames `store.json` to
  `store.json.aside-<ms>`, releases; a held seat is 75; no store is a success with no line.
- **GU1. Every guest operation** (B 2.13): refuses uid 0, any supplementary group but the user's
  own (`getgroups`), a `--user` that is not the caller (by uid and passwd name); sets umask 077
  first; names every leaf mode; opens through no link from the home's directory fd; places files
  `O_EXCL` then rename; refuses a path owned by another uid; prints `CHANGED<TAB><what>` per
  change and nothing else on stdout; exits 0, 64, 75 (`remove` only) or 78. Each through
  `execx.Fake` for `gh`, then in the VM lab from nothing (T15).
- **GU2. `guest check --user U --team T`**, stdin `{"v":1,"team":..,"base_url":..,"plain_http_hosts":[..]}`
  (n3-D6): a home; `gh auth status --hostname github.com` exits 0 as the user; pidfd and inotify
  usable; `tmux` and `claude` executable files on exactly `<home>/.local/bin:/usr/local/bin:/usr/bin:/bin`;
  `reach` (3 s `GET /_matrix/client/versions`). Each miss is 78 with the check's name.
- **GU3. `guest place-bundle --user U`** (C 3.9.2 step 7) on a token bundle with its team: a
  seat directory that does not exist is placed by `<team>.new-<claim>/` renamed in with
  `RENAME_NOREPLACE`; one that exists keeps its `store.json` and `seat.lock`, and `token` then
  `seat.json` are each replaced by `O_EXCL` temp and rename inside it (n3-D1). `seat.PlaceGuest`'s
  directory exchange is replaced by this. A password bundle is 78.
- **GU4. `guest membership --user U`** writes `desired.json` (MS1's codec, `session` `lxc-<user>`)
  into `~/.config/teamchat/membership/` (0700, file 0600) atomically; the same bytes again print
  nothing.
- **GU5. `guest remove --user U --team T`** takes the seat lock (75 if held), removes every file
  of the seat directory but `seat.lock`, releases; nothing to remove prints nothing.
- **PA1. `paths`** gains the inbox socket form (`<runtime>/cc-socks/<pid>.sock`), the aside name,
  `teamchat.floor`, the guest's unit and wants-link paths (n3-D3), and the guest runtime parents.

## 3. n3b: LXC apply from the host and the session unit (C 3.9)

All in `host/lxc` behind seams (`execx` for `lxc-info` and `lxc-attach`, a guest-root opener, a
guest systemd client, the child for roster changes), tested on fixture root trees and fakes; the
real-kernel `openat2` test is Linux-only. Running it on the host is **[A]**.

- **LX1. Input.** `lxc apply --phase absent|present [--only-teams T,..]` reads the declared
  members as JSON on stdin in `declare.LXCMember`'s shape (n3-D11) and holds the approval gate
  (70 on an unapproved entry, C 3.14). Every entry is validated before any container is touched
  (C 3.9.2 step 01).
- **LX2. Containers.** `lxc-info -sH`: missing fails the present phase, not running is held back
  (C 3.9.3: `SKIPPED` with a recorded handle, else `DEFERRED`); a missing container in the
  absent phase is parked from the host as a stopped one is and leaves the record (n3-D12).
- **LX3. The guest root.** The init pid from `lxc-info -pH`; `pidfd_open` on it; `/proc/<pid>/root`
  opened as a directory fd; the pidfd checked alive and `lxc-info -pH` asked again: a different or
  dead pid refuses the container for this phase (n3-D7). Every read and write below it keeps TB1.
- **LX4. The reads** (step 03): the container config (`lxc.idmap` refused, network link,
  `lxc.start.auto`); for `service: true` the start-auto setting, systemd as init, `tmux` on the
  guest `PATH`; `/etc/passwd` (1 MiB; uid and gid nonzero, uid not the desktop user's, name and
  home by their grammars); the checkout's `.git/config` (`origin`, `insteadOf` refused) and
  `.claude/ccy/ccy.env.local` (owned by the agent uid), 64 KiB each; the handle (TB2.3); the
  source address against `allow_from` on a host bridge; a migration-origin team's v1 holdback
  through D's adapter (a seam until N6). Any refusal fails the phase before any change.
- **LX5. The kit** (step 04): a guest lacking the digest gets `/usr/local/lib/teamchat/kit/<digest>/`
  from the host's kit, root-owned, dirs and binaries 0755, files 0644, under a temporary name
  renamed in; then `current` and `/usr/local/bin/teamchat` swapped by `symlinkat` to a temporary
  name and `renameat2`. Missing parents below `/usr/local/lib` are made 0755 root; an existing
  directory on the path that is not root-owned or is group or other writable refuses the write by
  name; a bind-mounted `/usr/local` is refused (`RESOLVE_NO_XDEV`). An old digest is deleted only
  when it is not `current` and no live session names it: each `session.pid` under
  `/run/user/<uid>/teamchat/run/*/` and `/run/teamchat-session-<user>/teamchat/run/*/` (n3-D8),
  strict and capped, its pid's start time checked in the guest's `/proc`; a forged or undecodable
  record keeps the digest.
- **LX6. The guest operations' calls** (steps 05, 07, 08): exactly C 3.9.1's `lxc-attach` line with
  the digest path, never `current`, each with a 60 s bound; 64 or 78 fails the phase naming the
  op and its error line; the absent phase's 75 is a held seat (LX8).
- **LX7. Roster changes and membership** (steps 06, 08): demotions of every team, then joins
  (`member add|return|reissue --credential token`, `--seat next` for LXC only), then promotions,
  through the child under each team's lock; then, once per user after every placement, the host
  copy `/var/lib/teamchat-admin/_lxc/<container>/<user>.json` written first (generation one up),
  then `guest membership`. A user's seats include its teams outside `--only-teams` (n3-D13). A
  converged apply writes nothing and attaches for nothing but `check`.
- **LX8. The absent phase** (C 3.9.2): under the team lock, `member park` first; then the desired
  file rewritten with a `farewell` `parked`; the unit stopped, disabled and removed if the user
  holds no team; the L3 probe every 1 s for at most 60 s; `guest remove`. A seat still held is
  `DEFERRED<TAB>..<TAB>seat still held by run <id>; token revoked` (the id only if it passes its
  grammar), the record entry kept `left`, the phase not failed, steps 2-5 retried next apply.
- **LX9. Output and the record** (C 3.9.3): `CHANGED|SKIPPED|DEFERRED` lines and one
  `LXC-SUMMARY`; `/var/lib/teamchat-status/lxc/<team>.json` 0640 root:`<owner group>`, atomic,
  only on change, written by each phase that changed it (n3-D12); `since_ms` kept while status and
  handle stay.
- **SU1. The unit.** `teamchat-session-<user>.service` rendered from C 3.9.4's text exactly, its
  `<user>`, `<group>`, `<home>`, `<checkout>` grammar-checked; written to
  `/etc/systemd/system/` with its wants link in `multi-user.target.wants/` by LX5's rules (n3-D3);
  its `ExecStart` parsed by CL5. Unchanged text is not restarted. **[A]** for placing it.
- **SU2. The guest's systemd** over `/proc/<pid>/root/run/systemd/private`, peer-to-peer D-Bus
  (godbus, DC17, its message size capped below godbus's default): `Reload`, `StartUnit`,
  `StopUnit`, `ResetFailedUnit`, each with a 30 s bound, on a fake bus in tests; H-C9 in the VM
  lab. If H-C9 fails, DC13 (b): the unit starts at the next container start only, and
  `lxc restart` refuses naming a container restart.
- **SU3. `lxc restart --container C --user U`**: refuses a member with no unit; else
  `ResetFailedUnit` then a restart over SU2.
- **LP1. The L3 probe as `health.Guests`**: (team, handle) to (container, user) from the LXC
  record; the seat lock under the user's seat directory through the guest root, by TB1's open and
  `LOCK_SH|LOCK_NB`; a stopped container's seat is not held; the free lock's record returned for
  the tick. H-C10 (inside the health unit's sandbox, refused without `CAP_SYS_PTRACE`) in the VM
  lab, **[A]** on the host.

## 4. n3c: the keeper, the link and receiving

- **RN1. `run`** follows B 2.4.1 steps 1-8: the runtime parent (`XDG_RUNTIME_DIR` set, the uid's,
  0700, else 78 `runtime`; ccy's private `/tmp`); the kit resolved once and `run --kit <kit>`
  re-exec'd, `--kit` never reading `current` (T7); leftover runtimes removed only under their
  `keeper.lock` when keeper and session are both gone (T8); `<v2-run>` made 0700 refusing an
  existing path; `session.pid`; the per-launch settings (T17: `crossSessionInbound` always
  written, `refuse` with no `--desired`, `--plain` gets nothing); `--resume-recorded` appends
  `--resume <id>` only for a recorded session whose transcript exists in the derived project
  directory; a missing or undecodable `desired.json` under `--desired` is 78 `membership`.
- **RN2. Seats and the link.** Every desired seat's lock is taken (a held one fails the launch,
  75); the link is started (own session, stdin `/dev/null`, output to `link.log`, control
  socketpair as fd 3) with or without a seat (T6) and waited for its `ready` up to 30 s, else 78
  naming `link.log`; CMD is never started beside a link that did not come up.
- **RN3. CMD.** Started in the keeper's process group with `TEAMCHAT_RUNTIME`; `session.pid`
  rewritten with its pid and start time; a `--claude` CMD that cannot start or ends before the
  first SessionStart reached the link is `launch-failed` (78 `launch`, no notice, LK4's record).
- **KP1. The keeper's loop** over its seven sources (B 2.4.2); `SetGCPercent(-1)` and a 16 MiB
  limit; no network call; `SIGINT`/`SIGQUIT` ignored; `SIGTERM`/`SIGHUP` forwarded once, by
  pidfd to Claude Code when its pid is known else to CMD, then `SIGTERM` at 5 s and `SIGKILL` at
  7 s to whatever still runs; the end only when CMD and Claude Code have both gone (3 s, then the
  same escalation). T21's wrapper cases.
- **KP2. Supervising the link** (B48): restarts after 1, 2, 4, 8, 16 s, a 10-minute run resets;
  the 6th exit or one exit 78 is `failed`, seat descriptors closed, `keeper.json` and every
  `report.json` saying why (T23); a deliberate kill (LN8) is no exit for the count.
- **KP3. The end.** `end` with `launch-failed`, `stop`, `exit` or `killed` (and `restart`, RS2);
  up to 5 s for the link, then `SIGKILL`; seat locks closed only then (or when the owed leave
  notice is posted or its 30 s bound passed); `<v2-run>` removed; exit with CMD's status or
  128+n. A restarting link is started once more with only the seats and `end`.
- **LN1. The link's start** (B 2.4.3): `link.lock` (held is 78); `session.json` read back; every
  store loaded; `handed` items returned `repeat`; then `link.sock` bound (a stale socket unlinked
  under the lock), `ready` sent. A connect before that is refused. The link ignores `SIGTERM`,
  `SIGINT`, `SIGHUP`; a closed control socket (the keeper's death, its pidfd checked against
  `getppid`) posts `disconnected` for every held seat within 5 s and exits (T20).
- **LN2. The socket protocol** (B 2.4.4): `SO_PEERCRED` uid; one request per connection, at most
  64 KiB (else 10); a `kit` digest not the link's is 78; every request carries `deadline_ms`
  (`CLOCK_BOOTTIME`), set by the table (hooks 300 ms, local reads 2 s, sends 30 s, `history` and
  `questions` 60 s, `wait` its timeout); a request past its deadline is discarded unacted (T4).
- **LN3. No link.** A client that cannot connect or loses the connection before `done` reads
  `keeper.json` (4 KiB, strict) and probes `keeper.lock`: 69 `no-link` with the restart time,
  69 `link-failed` with the kind's recovery, 69 `no-session` without `TEAMCHAT_RUNTIME`; under
  300 ms. `wait` alone reconnects (1 s doubling to 5 s) with its one deadline (T3).
- **LN4. `recv`, `wait`, `inbox`** (B 2.2.1, 2.4.4): caps of 8 entries and 12 KiB then `more`;
  items `handed` and persisted before any record is sent; delivered only on the CLI's `ack`
  after it flushed stdout; no `ack` in 10 s, a closed connection or a failed stdout returns them
  `repeat`; a handed item is taken by nothing else; an `ack` for a removed team is ignored and
  recreates no store (T2, P11).
- **LN5. `status`** prints `team`, `member`, `room`, `outbox` (n3d) and `runtime` records, the
  latter from `/proc` of the link and its parent keeper (B40); per team its counts: ignored,
  dropped, `unheard` (UH1), gaps, redacted, `seen twice`.
- **LN6. `verify <id>`** re-fetches the event, judges it by A's rules and prints `verified` with
  the full text and its SHA-256; `history` reads the room backwards, consumes nothing.
- **LN7. One Claude Code per run**, the session facts (B 2.4.4): hook ops served only once the
  keeper gave the session pid, only to a peer that descends from it (64 steps of `/proc` ppid with
  start times); the inbox socket path only in Claude Code's form for a pid on that chain; the
  transcript only in the derived project directory; a new session id only on SessionStart
  `clear`/`resume` or a new link's first op; each fact written to `session.json` before the
  answer (T5). The hook ops' bodies are n3e's; their peer and path rules are here.
- **LN8. A removal** (B 2.10, 2.5): the link stops the team, drops its pending, handed and awaiting
  items, deletes the store (not `seat.lock`), closes its description, answers; then, within 30 s
  from memory, presence offline and `left released` when the `farewell` says `released`, nothing
  for `removed` or `parked`. No answer in 10 s: the keeper kills the link, deletes the store under
  the lock, restarts the link at once (no count) with the owed notice, posted once by its recorded
  transaction id. The keeper keeps the seat until the notice is posted or 30 s pass (T16).
- **LN9. A changed bundle** under a held seat (only events for `seat.json` and `token`) re-reads
  it and retries a team stopped on a permanent failure. The pool-seat hot reissue of a held ccy
  session (H-C15's clause) is tested on fakehs here; its host run is Task 4.4's **[B]**.
- **SY1. Start** (B 2.6 step 01): (a) a store: room verified, `restore_epoch` compared before the
  first sync, the position kept only when equal, else (b); (b) no store: resume from the earlier
  of the `teamchat.floor` marker (no marker only on a 404 `M_NOT_FOUND`, an undecodable one is
  none and said) and `released_ms`, `/messages` backwards under the page bound with the oldest
  left out, items at or before `released_ms` or the member's own latest `left`/`disconnected`
  marked `restored`, persisted `fresh: false`; (c) a re-add: timeline limit 0. Then the previous
  run closed (EN3) and `back` posted when due (EN4). T24, T11's resume rows.
- **SY2. The long-poll.** `since`, `timeout=30000`, the filter as `proto.SyncFilter` gives it,
  inline (n3-D5), 45 s deadline, one kept-alive connection per team, the default `set_presence`.
- **SY3. A limited timeline** is filled backwards from `prev_batch`, 100 per page, at most 20
  pages, ledger ids skipped; past the bound the oldest are left, the position moves on, and the
  team's next delivery carries `gap:`; `status` counts it (BD15).
- **SY4. Each event** gets `proto.Judge`'s verdict; accept queues, ignore counts, drop counts and
  logs one line; an answer to an own ping updates the outbox by its `origin_server_ts` (n3d).
  The flood budget is applied at admission (T19); an admin redaction removes a pending or handed
  item. Nothing is written by these steps but counters and log lines.
- **SY5. Persist, then announce**: once the answer passed SY6's check, one store write holds its
  items, `next_batch`, the floors and the outbox changes, then the delivery goroutine is woken. A
  crash before it re-fetches from the old position; ids pending or in the ledger are skipped.
- **SY6. The epoch check** (B 2.6 step 08): the roster read by `GET /rooms/<main>/state/teamchat.roster/`
  after every sync answer before SY5, and before the first long-poll after a start, a failure, a
  stopped team's resume or a paused sync's resume. Equal: persist. Different (lower included):
  discard the answer, re-verify the room, drop the position, resume as SY1 (b), or (c) for a
  `fresh` re-added store, and store the new epoch with the new position. A transient failure of
  the read persists nothing and sends no long-poll; a missing, undecodable or refused roster is a
  trust failure.
- **SY7. A resent `/sync` is safe** (Task 3.1). Go's transport may resend a `/sync` GET on a fresh
  connection when a reused kept-alive one closed before the answer (an idempotent request); the
  link neither prevents nor relies on it. Within one database a sync from one `since` is
  idempotent: the resend brings the same events from the same position or later, and SY5 skips
  every id already pending or in the ledger, so no item is doubled and the position is persisted
  once. Across a restore, the resend may reach the restored database, which holds a `since` ahead
  of it to its timeout and answers its own lower `next_batch` (Tuwunel 1.9.4, H-C14); no answer
  is persisted before SY6's check, which finds the epoch changed and discards it. Tests, on
  fakehs (whose restore stands in until C 3.12 is built, answering as Tuwunel does):
  1. `DropAfter` on a `/sync` over a kept-alive connection: fakehs records the same `since`
     twice, each event is pending once, `next_batch` is written once;
  2. a kept-alive connection closed while idle (`Restart` between polls): the next answer is
     checked and persisted as any other;
  3. a restore under an in-flight sync with `DropAfter`: the resent answer, the lower
     `next_batch` after the wait, is discarded, the stored position is never overwritten by it,
     the seat resumes from its marker and `released_ms`, and a post made after the restore is
     delivered;
  4. a ping's reply that only the discarded answer held leaves the ping unanswered and its
     TIMEOUT is made;
  5. a restore landing between the epoch read and the long-poll; a team paused on a full queue
     that resumes after a restore; a restore between SY1 (b)'s verification and its first
     persist: each caught by the next answer's check;
  6. the epoch read failing transiently: nothing persisted, no long-poll sent;
  7. a restarted LXC store whose `restore_epoch` differs from the roster's resumes as a seat
     with no store before its first sync.
     Task 3.4's `-tags tuwunel` suite runs case 3 against the pinned Tuwunel with its database
     restored from a copy taken earlier in the test.
- **SY8. Failures** (B 2.6 step 08): transient (unreachable, timeout, 5xx, 429 past retries)
  retried from the stored position after 1 s doubling to 30 s with jitter, `down` after 15 s,
  one log line at each end; 401, 403 or lost trust stop the team (21 or 22 for its verbs, one
  hook line, a NOTICE), its store and owed TIMEOUTs kept, retried on a bundle change, a desired
  generation change or the next start (T10). No team's failure touches another's.
- **SY9. Presence**: only the team sync marks the member online; no request ever sends
  `set_presence=offline`; at a clean end and a removal `PUT /presence/<me>/status` offline.
- **SY10. The resume marker** `teamchat.floor` `{"v":1,"floor_ms":N,"restore_epoch":N}` is put after
  a store write that moved a floor, at most once per room per 10 s coalesced, and at a clean end
  and a removal before the store goes; a failed PUT blocks nothing.
- **SY11. Invites and roles** (B 2.6 step 10, main room): an invite only to a room the verified
  record names for this role, from the admin; one not named yet left pending and logged; any
  other rejected. A role change rebuilds the filter. (The questions room's rules are n3f's.)
- **SY12. A freeze or suspend** (a deadline long overdue, or a `CLOCK_BOOTTIME`/`CLOCK_MONOTONIC`
  gap): drop the connection, re-verify, new long-poll, one line with the pause's length (B37).
- **SY13. A full disk**: a failed store write stops that team taking items (position held), said
  once, retried with the transient backoff.
- **RC1. Hot control** (B 2.10): inotify on `<v2-desired>` (`IN_CLOSE_WRITE`, `IN_MOVED_TO`,
  `IN_CREATE`, `IN_DELETE`), an overflow re-reads; a change applied within 2 s (T9); an
  undecodable file keeps the last good membership, `desired.state` `refused` with its `code` in
  `report.json`, `keeper.json`, `status` and once in a hook line; an add of a held seat is
  refused with the holder's run id in `report.json` and a NOTICE, the others carry on; the
  keeper remembers every seat it removed, so a re-add is SY1 (c). A session launched `refuse`
  runs a team it is given in `wait` mode with `needs_relaunch: true`.
- **EN1. Which notice** (B 2.11's table): every `end` by every SessionEnd reason; `ended` recorded
  `attempted` with its transaction id before each send and `sent` on the answer; all held teams in
  parallel inside the keeper's 5 s; a `probe` seat posts nothing; a SessionEnd alone posts nothing.
- **EN2. Who is told**: A's `to` list (humans and the coordinator, never the subject); bodies are
  A's templates with grammar-checked names.
- **EN3. Closing the previous run** at a seat's take: `attempted` re-sent by its transaction id;
  `null` judged from the room since `run.started_ms`, one `disconnected` as `tc-disc-<ended run id>`
  only when the latest own lifecycle notice is neither `left` nor `disconnected` (T14 with the
  host's call for the same run: one post).
- **EN4. `back`** once per run when the latest own lifecycle notice is `disconnected`, after the
  first SessionStart (at once for `--plain`); never after `launch-failed`.
- **EN5. `seat disconnected --bundle DIR --since MS --seed ID`**: the host-held bundle through MS3's
  reader, `notice.Disconnected`, nothing else read (no store, no container file); its stderr
  buffered and `EIO`/`EBADF`/`EPIPE` ignored once settled.

## 5. n3d: the sends

- **SD1. `reply <id>`** decides team, room, thread and addressee from the ledger, or from the
  homeserver for an unknown id (23 if none): in main a reply, `--verb` an answer in the ping's
  thread with `--ref` where the verb takes one; a carried ping answered to its original sender.
- **SD2. `say` and `ping`**: `--to` names or `--to-coordinator`, resolved per team; a name in two
  teams with no `--team` is 64; `ping` without a verb is 64 naming the verbs and `status`.
- **SD3. The send gate and `sending`**: the token charged and the transaction id recorded in one
  store write before the network call; a restart re-sends `sending` by the same id; three tries
  in the 30 s deadline (M22); a duplicate within the window is 15; rates are 14.
- **SD4. The outbox and TIMEOUTs**: pings recorded with `due_ms`; answers by `origin_server_ts`
  even when read late; one TIMEOUT per silent target from the state goroutine's timer, never a
  tick; `outbox` records in `status`.
- **SD5. The forge check** (B 2.6 step 11) for a ping's reference, sent or received: `gh auth token --hostname github.com` through `execx` when needed, held at most 10 minutes, dropped on a 401,
  positive results kept 3600 s; a missing or logged-out `gh` is 78 `forge-login` on the verb that
  needs it; a failed check is 13.

## 6. n3e: delivery into Claude Code, the hooks, the status line, the kit

- **DV1. The inbox socket** (B 2.7.1): one connection per delivery, `auth` then `user` line,
  half-close, read to EOF, 5 s per phase, through fakeinbox; `ENOENT`, `ECONNREFUSED`, `ENOTSOCK`
  stop delivery; never to a session launched `refuse`; no socket delivery before a hook gave the
  socket and a transcript.
- **DV2. The envelope** is A 13's, rendered byte for byte by `proto.Delivery`; A 14's corpus is the
  renderer's golden test; a slot that fails moves the item to `unusable.render`, a HUMAN one with
  a `not shown:` line.
- **DV3. The states** (B 2.7.3): an `awaiting` entry `written: "unknown"` persisted before the user
  line, `"yes"` with the ledger after it; one team per delivery, the team with the oldest pending
  HUMAN item first; one delivery per 2 s; the 120 s stall; the failpoints of B 2.15 between each
  pair of writes (T18, P1-P6).
- **DV4. Transcript proof** (B 2.7.4): the needle `teamchat delivery <nonce>:` scanned in new bytes
  with overlap; the quiet clock from idle-since-write (`stop_ms`); 180 s quiet or 1 h is `missing`,
  items back `repeat` and held, out of the ledger in the same write; a late find in 24 h; a
  shrunk or moved transcript rescanned from 0 (P8-P10, faketranscript).
- **DV5. Read receipts** for proven HUMAN items, persisted, retried, dropped with a removed team.
- **HK1. The hooks** (B 2.8): stdin at most 1 MiB, UTF-8, a JSON object, else `"payload": "unusable"`
  counted (T13); one JSON line out, exit 0 always (64 for a bad event); no `TEAMCHAT_RUNTIME` is
  `{}`; the four events' answers as B's table, each text from a fixed template; a link not
  answering in 300 ms gives the hook's own recovery line on SessionStart and UserPromptSubmit,
  nothing on Stop and SessionEnd, and no file written.
- **HK2. Stop** blocks only on `stop_hook_active` literally `false`, once per distinct pending set,
  never when `session.json` cannot be written.
- **HK3. SessionEnd** records the reason only; its round trip under 300 ms (B40's bar).
- **SL1. The status line** (B 2.9): the inner command as v1's rules (its own process group, 10 s,
  128+signal and nothing on stdout on cancel, never cached); the team segment from one read of
  `status.json` and one `link.lock` probe, the red forms from `keeper.json`; empty stays empty;
  exit 0. The link writes `status.json` on change at most once a second and at least once a
  minute while a team is held (T12).
- **PK1. The kit's files**, rendered by `member/kit` for a kit directory (n3-D4): `plugin/teamchat/`
  with `hooks.json` naming `<kit>/bin/teamchat hook <event>` by absolute path, `settings.json`
  with the status line at `refreshInterval` 25 (a test pins it below 30) and no inbound setting
  and no SessionEnd timeout. Placing the host kit is **[A]**.
- **PK2. The skill** (B 2.12), embedded in the binary, and the text test: every verb, flag,
  record kind and exit class it names exists, none an agent can meet is missing; every `teamchat`
  command in A 13's templates, the frame text and the hook texts parses with CL2's parser, the
  `reply:` line once per answer verb.

## 7. n3f: the side rooms (Plan 00211)

- **SR1. `report`**, `ask`, `thread`, `questions` as B 2.2.1, A 10 and Plan 00211's SPEC as
  behaviour: the reports room verified at each send and never synced; `ask` by the coordinator
  only (11), `--asked-by` and `--re` resolved (64, 10 `re`); the question index from proto.
- **SR2. The questions room** in a coordinator's filter; its invite left pending while `human_text`
  is false (said in `status` and the frame); a demotion stops syncing it at once and drops its
  pending items; a human's edit there is an item with `revises`.

## 8. The requirements carried from Plan 00194

**2.0e: a scheduled restart posts no goodbye.** In v2 the decision is made at the keeper's `end`,
after CMD has exited, so CMD's status is known; ccy's supervisor exits 75 for a relaunch it acted
on (B 2.11). That status alone is not proof (Claude Code's own status passes through the
supervisor), so the switch needs hooks-daemon #127's record too.

- **RS1. Until #127 ships**, a ccy relaunch posts `left exit` as any `/exit` does, and a test pins
  it.
- **RS2. The switch.** `run` reads a `RestartRecord` seam at the end. Its production adapter is
  written when #127 ships, to #127's format, and is wired for `--kind ccy` only (LXC and host
  kinds run no supervisor). When CMD exited 75 and the record names a restart recorded for this
  run's Claude Code (its pid and start time equal `session.json`'s `claude`) and not cleared, the
  end is `restart`: the link posts nothing and records
  `run.ended = {"notice":"none","reason":"restart","state":"skipped","txn":null,"at_ms":N}`
  (n3-D2); the next link on the seat closes it with no post and no `back`. A 75 with no record, a
  record without 75, a cleared record, or one for another Claude Code posts `left exit`. Test: the
  table of those cases with a fake seam.
- **RS3. The host half is N4's** (Task 4.2): the ccy launcher, which performs the relaunch, skips
  its `seat disconnected` call for the run it relaunches and records it in `held/notice.json`, so
  the owner's sweep skips it too. RS2's adapter is not wired before RS3 lands, since a member that
  skips `left` while the host still posts `disconnected` would post a worse notice than today's.
  **Dependency:** hooks-daemon #127 (upstream, unreleased) for RS2; Task 4.2 for RS3.

**2.0g: is `empty` unheard?** Yes.

- **UH1.** A human's message ignored with reason `type`, `not-addressed` or `empty` counts as
  unheard: each member counts it once per event in its team's `unheard` count in `status` and logs
  one `link.log` line with the event id and the reason, never the body. Why: `proto.DecodeHuman`
  decides `empty` before any addressing rule, so no member can tell whether it was the
  addressee; the case 2.0g raises (an addressed reply that is only the quote fallback) reached
  no agent, which is what unheard counts; counting it at every member is A 7.5's rule for an
  unaddressed message (P41), and leaving it out is the gap 2.0g found, logged by no one. Test: an
  `m.emote`, an unaddressed message and a fallback-only reply to this member each count once and
  log without the body; a reply with text after the fallback is delivered and not counted.

## 9. Gaps in the built APIs, and how each is closed

01. `paths` has no inbox socket form, aside name, floor type, guest unit and wants-link paths, or
    guest runtime parents: PA1 adds them.
02. `seat.PlaceGuest` exchanges a whole seat directory and removes the old one, which deletes
    `store.json` and unlinks a `seat.lock` a live keeper may hold, against B 2.5: GU3 replaces it
    (n3-D1).
03. There is no member-side bundle reader, and two `seat.json` shapes: MS3 reads both through
    `state.ReadFile` and `seat`'s decoders (an exported `seat.DecodeMemberBundle` accepting exactly
    the two).
04. `seat.LockRecord` has a decoder only: n3a moves the record's codec to `member/state` (one owner,
    both sides import it) with an encoder, `seat` and `health` reading it from there.
05. `matrix.SyncRequest` takes a `RoomFilter` with no presence senders and no zero timeline limit,
    while `proto.SyncFilter` is the filter's truth: the request gains the filter as compact JSON
    from proto (a test pins the query byte-equal to proto's); proto gains the re-add form with
    limit 0 (n3-D5).
06. `fakehs` has no restore: it gains `Snapshot()` and `RestoreTo(snapshot)`, which roll its
    database back (room account data included) while its listener and open connections stay, so
    SY7's cases run; the test raises the roster's `restore_epoch` as C 3.12.2 would.
07. `execx` runs commands to their end or execs over the process; the keeper needs a started
    child it waits on: `execx` gains `Start` (a handle with the pid, a pidfd, `Wait`, extra files,
    `Setsid`), and `Fake` a scripted form.
08. `cmdmaintest` is under `qa/internal`, which `internal/teamchat` cannot import: a re-run helper
    for the teamchat packages' real-process tests (locks, keeper, link, failpoints), or the helper
    moved to `internal/`.
09. Failpoints: the `busfailpoints` build tag does not exist; n3c adds it, with the crash points of
    B 2.15 F6, compiled out of every non-test build (a gate test asserts the release binary has
    none).
10. `health.Guests` has no production form: LP1 is it.
11. `proto` has no unheard count: the member counts by reason (UH1); nothing in proto changes.

## 10. Decisions where the design is silent or wrong

| ID     | Decision                                                                                                                                                                                                                                                                                                    |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n3-D1  | A guest bundle is placed into an existing seat directory file by file (`token`, then `seat.json`), keeping `store.json` and `seat.lock`; the directory swap is for a seat that has none. C 3.9.2 step 7's "swapped in" would delete the store and unlink a held lock, against B 2.5 and B 2.6 step 01 (a).  |
| n3-D2  | `run.ended.notice` gains `none` (with `reason` `restart`, `state` `skipped`, `txn` null) for RS2; closing a run acts only on `attempted` and `null`, so nothing else changes.                                                                                                                               |
| n3-D3  | The guest unit is `/etc/systemd/system/teamchat-session-<user>.service`; its wants link is an absolute link in `/etc/systemd/system/multi-user.target.wants/`, made by `symlinkat` to a temporary name and `renameat2`; the host writes both (C 3.9.4), so DC13 (b) still starts the unit at the next boot. |
| n3-D4  | The kit's plugin and settings are rendered by `member/kit` for the kit's absolute path; the host kit's files are valid in a guest unchanged because the guest path is the same; ccy's image kit is rendered for its own path (N4).                                                                          |
| n3-D5  | The sync filter is sent inline on every long-poll, not uploaded (B 2.6 step 02 says uploaded and kept by id): a filter id uploaded after a backup is unknown after a restore, and inline needs no state.                                                                                                    |
| n3-D6  | `guest check` reads `{"v":1,"team","base_url","plain_http_hosts"}` on stdin for `reach`, from the team's declaration; `gh` is checked by `gh auth status --hostname github.com` exiting 0 as the user.                                                                                                      |
| n3-D7  | The guest's init is pinned by a pidfd: open it, open `/proc/<pid>/root`, then check the pidfd alive and `lxc-info -pH` unchanged; K10's before-and-after pid check is kept and the pidfd closes the reuse window.                                                                                           |
| n3-D8  | Kit collection reads both runtime parents, `/run/user/<uid>/teamchat/run/*/` and the unit's `/run/teamchat-session-<user>/teamchat/run/*/`; an absent parent is no session.                                                                                                                                 |
| n3-D9  | The seat lock record's `launch_failed_ms` and run id stay member-written: they can only stop the host speaking about that member, which its own token can do; the host never acts on them otherwise (TB2.1).                                                                                                |
| n3-D10 | The handle derived from a guest checkout is used for the first join only; afterwards the LXC record's handle is kept and a different derivation is refused by name ("the checkout's origin changed; retire and add the member to change its handle"), so a member's file never makes a new account.         |
| n3-D11 | `lxc apply`'s stdin is the declared members in `declare.LXCMember`'s shape, read by `declare`'s strict reader and held by its gate.                                                                                                                                                                         |
| n3-D12 | The absent phase treats a missing container as a stopped one (park from the host, entry leaves the record); each phase writes the record when it changed it, the present phase only after it succeeded.                                                                                                     |
| n3-D13 | `--only-teams` limits which entries a phase acts on; a user's desired file still lists every team the user is placed in, from the host copy.                                                                                                                                                                |
| n3-D14 | One open rule below a guest root: `RESOLVE_IN_ROOT` (C 3.9.1, 3.9.2, K10, H-C10), not the `RESOLVE_BENEATH` of C 3.11.1's text; with `RESOLVE_NO_SYMLINKS` and clean paths they resolve the same, and one rule is one test.                                                                                 |
| n3-D15 | A unit stop signals every process (default `KillMode`), so Claude Code may get `SIGTERM` from systemd and from the keeper; the keeper forwards once whatever it received, and the second signal changes nothing. T22's LXC rows prove it.                                                                   |
| n3-D16 | A missing `desired.json` under a given `--desired` is 78 `membership`: the host writes it for every launch (C 3.7.5), so its absence is a fault, not "no team".                                                                                                                                             |

## 11. B 2.15's tests, where each lives

| B 2.15                                                      | Here                                                                                                                                |
| ----------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| F1 fakehs (with H-C14, HC-A1, H-C13)                        | built (N1); restore added (gap 6)                                                                                                   |
| F2 fakeinbox, F3 faketranscript                             | n3e (DV1, DV4)                                                                                                                      |
| F4 clock                                                    | CK1                                                                                                                                 |
| F5 `gh`, `lxc-attach` fakes                                 | GU1, LX6, SD5                                                                                                                       |
| F6 real processes and failpoints                            | gaps 8, 9; LK3, KP1-KP3, LN8, DV3                                                                                                   |
| P1-P7                                                       | DV3 (P1-P3, P6), LN8 (P4), EN1/EN3 (P5), LN2 (P7)                                                                                   |
| P8-P11                                                      | DV4 (P8-P10), LN4 (P11)                                                                                                             |
| T1 single writer                                            | LK3, LN8                                                                                                                            |
| T2 `recv`, T3 `wait`, T4 no listener                        | LN4, LN3, LN2                                                                                                                       |
| T5 hook peers                                               | LN7                                                                                                                                 |
| T6 no team, T7 kit upgrade, T8 leftovers                    | RN2, RN1                                                                                                                            |
| T9 reconcile                                                | RC1                                                                                                                                 |
| T10 permanent failure                                       | SY8                                                                                                                                 |
| T11 replay and restore                                      | SY1, SY5, SY6, SY7; B44's floors in MS1/DV3                                                                                         |
| T12 heartbeat                                               | SL1                                                                                                                                 |
| T13 unusable inputs                                         | HK1, DV2                                                                                                                            |
| T14 `seat disconnected` and the tick                        | EN3, EN5; LP1 for the tick's probe                                                                                                  |
| T15 guest operations                                        | GU1-GU5                                                                                                                             |
| T16 removal                                                 | LN8                                                                                                                                 |
| T17 per-launch settings                                     | RN1, CL5                                                                                                                            |
| T18 one team per delivery, T19 flood budget                 | DV3, SY4                                                                                                                            |
| T20 keeper lost, T21 the end, T23 crash loop                | LN1, KP1, KP3, KP2                                                                                                                  |
| T22 every member kind's ends                                | VM lab, Task 3.4 (LXC rows) and Task 4.4 (ccy rows)                                                                                 |
| T24 resume bound                                            | SY1                                                                                                                                 |
| T25 runtime wakeups, 2.14's bars                            | KP1; `acceptance-member-cost.bash`, Task 3.4                                                                                        |
| E1 `-tags tuwunel` suite, E2 VM lab EL9 LXC, E3 host checks | Task 3.4 (E3's H1-H7 re-runs per pinned Claude Code version are host checks, **[A]** where they need the kit installed on the host) |
