# Plan 00209 N0 integrated review: security and privilege

Reviewer concern: security and privilege. Read: DESIGN-v2-FRAME.md, DESIGN-v2.md, C sections 1-5 in
full, A 2, 4.2-7.1, 7.7, 12.3, 13, B 2.2.1 and 2.6 step 01, D 3.5-3.8, 5.1-5.7, and R37-R39 in
DESIGN.md 2.8. One fact was checked against the repository: `environment/localhost/host_vars/` is
visible and writable from inside a ccy container that has this repository checked out (its root is
the owner's uid, K9). Its content was not read.

## Verdict: FIX-FIRST

5 major, 10 minor, 0 blocker.

## Findings

### 1. major: a released session's kept token can silently skip the next holder's messages (DC16 with INT1)

Where: C 3.7.3 (DC16, K13), B 2.6 step 01 (b), DESIGN-v2.md section 7 (DC16, INT1).

What is wrong. K13 describes the cost of DC16 (a) as "may keep a valid copy ... and use it while the
next session holds the seat", meaning it can impersonate the member. It is worse than that. INT1 and
B 2.6 step 01 (b) start a seat with no store from **this member's own latest read receipt**. A read
receipt is set with the token, so whoever holds a copy of the token can set it. The release puts the
session's state in quarantine (DC15), but the token keeps working.

Failure scenario: session S1 holds seat 3 and keeps a copy of `token`. S1 is released. While no
session holds seat 3, the coordinator pings seat 3 twice. S1, from outside or from a leftover
process, posts `m.read` on the newest event in main. S2 takes the seat with the pool's bundle, finds
no store, starts from that receipt, and never delivers the two pings. Nothing is logged and no `gap`
is reported. That breaks "nothing is skipped" in step 01 (b) and the frame's "nothing lost
silently". The old holder can also go on reading all of the seat's traffic, which K13 does not
mention.

Fix.

- Restate K13 to cover both effects: silent skipping and continued reading.
- Bound the resume point by a fact the host wrote. At release, store the release time in the pool
  (`notice.json` or a `released.json`, owner-written, not reachable from any container). At step 01
  (b), the start point is the **earlier** of the member's own read receipt and that release time. An
  item after the last release is new by construction, whatever the receipt says.
- Add a DC16 option (c) and weigh it: the pool holds a per-seat **password** as well as a token
  (owner 0600, never mounted, the same exposure as the token has today under K12). Each take logs
  in a fresh device for the session. Each release deletes that device (UIA with the password), so a
  released session's token dies at release with no owner password at take time. If H-checks show
  Tuwunel supports device deletion with UIA, (c) closes K13 at no extra cost to the owner.

### 2. major: v2's security policy sits in a host_vars file that every ccy session on this repository can write

Where: C 3.8, C 3.5.1 step 2, C 3.6.4, C10, K9.

What is wrong. The team declaration holds the security-relevant policy: `humans`, `coordinator`,
`allow_from`, `listen`, `url` and `trust` (the forge allow-list that decides `trust: trusted` on
every PING). That file lives in the repository checkout. A ccy container working on this repository
mounts the checkout read-write, and its root is the owner's uid. K9 names "programs built from a
checkout"; it does not name the declaration. The play applies whatever the file says with the
owner's become password, and nothing shows the owner the security-relevant difference. So the
"owner's password at the time" (C10) authorises content an agent wrote.

Failure scenario: an agent in a ccy session on the repository edits `teamchat_teams`. It sets
`allow_from: ["0.0.0.0/0"]` and `listen` to a LAN address, adds `example-org/attacker-repo` to
`trust.repos`, and sets `coordinator` to its own handle. The owner later runs meta-deploy for an
unrelated plan, and the full play applies all of it. Every agent now reads the attacker's refs as
`trust: trusted`, the homeserver is on the LAN, and the agent is coordinator, with the questions
room.

Fix.

- `declare check` (C 3.5.1 step 2) compares each team's security fields (`humans`, `coordinator`,
  `allow_from`, `listen`, `url`, `server_name`, `trust`) with `root/<team>/team.json`, the last one
  applied. The play prints every changed field in a task of its own (seen without `-v`), as it does
  for SKIPPED and DEFERRED.
- ccy-teams shows "declaration changed: <fields>" on the team row before it offers apply.
- Add a K row naming the declaration as agent-writable, beside K9.
- Optional, stronger: refuse to widen `allow_from`, `listen` or `trust` in a play that ccy-teams did
  not start, unless the owner passes an explicit acknowledgement variable.

### 3. major: the owner's password does not guard which session holds which seat

Where: C10, C 3.6.6, C 3.7.3 ("Taking a seat"), C 3.7.5, DC16, K3, K12; the frame's "nothing an
agent in a cc session can run changes membership".

What is wrong. Under DC16 (a), taking a seat (copying a pool bundle into a session directory) and
hot join and leave (rewriting `control/<session>/desired.json`) need no password. They need only the
owner's uid. Container agents are kept out by mounts, which is sound. Any process on the host
running as the owner is not: a host Claude Code session, on the bus or not, the common case. Such a
process can copy any pool bundle, the coordinator's included, into any live session's directory and
write that session's `desired.json`. K12 says that such a process can **read** tokens. It does not
say that the password-guarded membership model can be rewired without the password.

Failure scenario: a host Claude Code session (no bus member) copies the coordinator's pool bundle
into worker session W's `sessions/W/<team>/`, raises the generation in `control/W/desired.json`,
and adds the seat. W's keeper joins the team as the coordinator. No sudo prompt appears, and the
board shows a legitimate-looking hold.

Fix.

- Narrow C10 and the frame sentence to what holds: the password guards accounts, roles, humans,
  backups and restores; seat-to-session assignment is guarded only by the owner's uid and the
  container mounts.
- Widen K12 from "can read" to "can read, and can re-seat sessions and change their desired teams".
- Offer, as a decision, the option that closes it: put the pool and `control/` under a uid no owner
  process holds (for example `teamchat-ctl` or a per-host `teamchat-seats` user), with placement by
  a password-sudo verb. That is DC16 (b)'s cost (a password per take), so it is an owner decision,
  not a silent default.

### 4. major: the health tick cannot open `/proc/<init pid>/root` with the capability set it is given

Where: C 3.11.1 (the `teamchat-health.service` sandbox), C 3.11.2 step 3, C 3.9.2 (the L3 probe for
LXC seats), C 3.11.3 (`online`).

What is wrong. Opening `/proc/<pid>/root` needs ptrace read access (`PTRACE_MODE_READ_FSCREDS`).
For the same uid, the kernel's capability check allows it only if the caller's permitted set
contains the target's, or the caller has `CAP_SYS_PTRACE`. The health unit's bounding set is
`CAP_DAC_READ_SEARCH CAP_CHOWN CAP_SETUID CAP_SETGID`. A privileged LXC init has nearly the full
set, so the open returns EPERM.

Failure scenario: every tick fails the `online` probe of every LXC member. Under fail-fast that
fails the tick for that team (no status record gets written, and the record expires on the board).
Otherwise `online` is silently absent. The likely quick repair, adding `CAP_SYS_PTRACE`, would be
made without the review it needs.

Fix. Choose and state one of these:

- add `CAP_SYS_PTRACE` to the health unit only, with the reason given, and keep the probe's
  `openat2` confinement; or
- drop LXC `online` from the per-minute tick, and probe it in `lxc apply` and on the board's
  explicit request through a password-sudo verb.

In both cases, run H-C10 inside the health unit's sandbox, not as plain root.

### 5. major: container-written v1 files choose which events the admin fetches and republishes, and questions-room content moves into main

Where: D 5.2 rows 1, 2 and 4, D 5.2.1, D 5.5 (`ReadSeatState`, `ReadSessionInject`), A 7.7.

What is wrong. Two things.

1. Row 2 takes event ids from `state/inbox/<eid>.json`, and row 4 from `inject/` file names (read
   with `podman exec ... find` inside the container). The member's container wrote both. The admin
   then fetches each event by id with the v1 admin token. The admin is in every room. It reposts the
   event's text in the v2 **main** room as a `teamchat.carried` addressed to that member. Nothing
   checks that the event was addressed to that member at all. That breaks R39 (a container-written
   file decides an action taken with the admin token).
2. Row 1 carries "questions-room items for coordinators", and A 7.7 allows carried items only in
   main. So humans' answers in the questions room, which only humans and the coordinator could
   read, are published to every worker in main, and to every future member through history.

Failure scenario: a worker's container plants `inbox/$<id>.json` naming a human's answer in the v1
questions room. At M4 the admin posts that answer into v2 main, readable by every member.

Fix.

- Treat inbox and inject entries only as candidate ids. Each candidate counts only if the event,
  fetched from the server, passes v1's addressing rule for that member (the same rule row 1 uses).
  The rest are reported, not carried.
- Grammar-check and cap the names `podman exec` returns, and say why running a command inside a
  possibly hostile container as the owner is acceptable there (C 3.9.1 argues the same point for
  LXC).
- Carry questions-room items into the v2 questions room (as thread posts under the carried
  question), or report them. Never post them in main. A 7.7 needs a questions-room carried form, or
  an explicit rule that such items are reported only.

### 6. minor: the launcher's seat-store mount rule is narrower than C25 needs

Where: DESIGN-v2.md seam row 53, C 3.7.1, D 3.6.

What is wrong. The only stated check is "the launcher refuses a checkout that contains the seat
store". A ccy launch can also mount other host paths (extra mounts, configuration directories). A
mount whose source is `~/.local`, `~/.local/state`, or a directory that `$XDG_STATE_HOME` points
into, exposes the whole pool, every coordinator token included.

Fix. N4's acceptance: the launcher refuses any mount whose source is an ancestor of, equal to, or
inside the seat store root, other than the two per-session binds. The check runs on resolved real
paths, and the VM lab has a test for it.

### 7. minor: the restore's staging instance serves the backup's old credentials on a TCP port

Where: C 3.12.2 step 2.

What is wrong. The transient unit serves the restored database, in which tokens and passwords
revoked since the backup are still valid, on a loopback TCP port. Any local process can reach it:
an owner process, or a ccy container through pasta's host mapping. A stolen pre-reissue token can
post events into `db.restore-<N>` that persist after the swap. Step 6 revokes tokens, but events
already posted stay.

Fix. Serve the staging instance on a Unix socket in a root-only 0700 directory (Tuwunel has
`unix_socket_path`), with `IPAddressDeny=any` and no `SocketBindAllow`, and probe it there.

### 8. minor: root's writes into an LXC guest do not refuse bind mounts or non-root directories

Where: C 3.9.1, C 3.9.2 step 4, C 3.9.4.

What is wrong.

- The reads go through the container's own view "bind mounts included". The writes (kit, unit,
  wants link) list `RESOLVE_IN_ROOT|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS`, but not
  `RESOLVE_NO_XDEV`. A guest whose `/usr/local` is a bind of an owner-owned host directory makes
  root write under a user's path, against C24 and R12.
- Nothing refuses an agent user whose uid is 0 (`lxc-attach -u 0` is the CVE-2019-5736 class that
  C 3.9.1 rules out).
- In a privileged container, the agent uid is the same kernel uid as any host user with that
  number.

Fix.

- Add `RESOLVE_NO_XDEV` to the write paths.
- `fstat` each directory on the path: it must be root-owned and not group- or other-writable.
- Refuse uid or gid 0 for the agent user.
- State the same-uid fact as a risk.

### 9. minor: the R37 consent gate for Element profiles never fires in v2

Where: C34, C 3.10, D 3.9 (host members refused until N5).

What is wrong. The refusal keys on "a user who runs host-kind v2 members". v2 refuses host members,
so the check is empty. Meanwhile the same desktop user now holds every pool token (DC5) and runs
host Claude Code sessions, which DESIGN R37 accepts only as "the accepted cc exception".

Fix. Make the consent record required for every Element profile write. Its text should state that
any agent run on the host as this user can read the human's session token and every seat. That
makes the exception an explicit owner act rather than a check that cannot fire.

### 10. minor: the fence's loopback exemption and its lifetime

Where: D 5.4 M2 (d)-(f), D 3.5 (fence objects), D 5.7.

What is wrong.

- The rule exempts `127.0.0.0/8` and `::1`. The design does not say whether a ccy container can
  reach the host's loopback through pasta (a host-loopback mapping). If it can, any container agent
  can reach the fenced v1 server. M3 and M5 detect a post, and R's state check detects a sync, so
  this is detected, not silent.
- `fence.nft` stores the port. If the v1 team's port is changed in `agent_bus_teams` while a
  migration is open, a reboot reloads a fence on the old port, which may by then belong to something
  else.
- An abandoned migration leaves the fence enabled indefinitely.

Fix.

- Add to M2 (f) a connect from a ccy container to the host-loopback-mapped address, if the ccy
  network options allow one.
- `declare check` refuses a port change for a team with an open set.
- The board shows any team whose set is between `fenced` and `committed` as "fenced since <time>"
  on every open.

### 11. minor: one `teamchat-ctl` uid holds every team's admin token

Where: C 3.1, C 3.2.2, DC1 ("admin credential scope: one team"), DC2.

What is wrong. A child started for team A can read `ctl/<team B>/admin.token`. The child is memory
safe Go, so exploiting it from a hostile homeserver is unlikely, but the per-team scope DC1 claims
holds only at the homeserver, not at the controller.

Fix. Either state this in DC1 and DC2, or have root open `ctl/<team>/` (`CAP_DAC_READ_SEARCH`) and
pass it to the child as a directory fd, with the child unable to open any other team's directory
(per-team groups, or a per-team dynamic controller user).

### 12. minor: D's path table contradicts C on where the secrets live

Where: D 3.5, row `/var/lib/teamchat/<team>/`.

What is wrong. The row says "db, backups, secrets, rendered config, team file, registry". C 3.2.2
puts only `db/` and `backups/` there, keeps the config and registration secret root-only, and
passes them by `LoadCredential=`. The same table also says `firewall.rules` where C says
`firewall.json`.

Fix. Copy C's facts into D's row, or point the row at C 3.2.2.

### 13. minor: DC17 needs a rule keeping the YAML parser out of the root binary

Where: DC17, C 3.8.

What is wrong. The YAML parser is meant for ccy-teams (the owner). The declaration types are
shared with `teamchat-admin`, which receives JSON. Nothing stops `go.yaml.in/yaml/v3` being linked
into the root binary by a shared package.

Fix.

- Add a go-deps rule that `src-go/cmd/teamchat-admin` does not depend on `go.yaml.in/yaml/v3`.
- State that `godbus` in root decodes answers from the host's systemd and from a guest's PID 1 (in
  a privileged guest, root-equivalent), with message sizes capped.

### 14. minor: `allow_from` is not a boundary between teams on one host

Where: C2, C 3.2.5 (the live bridge subnet added to every team), A 6.1.

What is wrong. Every LXC guest on the bridge, a member of no team included, and every ccy container
(through the bus address, as host-local traffic) can reach every team's homeserver. This is correct
for members, but the design never says that the token, and the login rate limits for human
passwords, are the only boundary between teams on a host.

Fix. Say so in C 3.2.5 and in A 6.9's threat table. Add the bridge subnet only to teams that
declare an LXC member.

### 15. minor: the supply chain for Tuwunel rests on the sha256 pin alone

Where: C 3.5.1 step 6.

What is wrong. This is correct as stated: the release is unsigned, and the pin is fetched before
decompression. The pin lives in `vars/version-pins.yml`, which an agent can edit (K9, finding 2).

Fix. The `update-versions` flow for this pin cross-checks the upstream published checksum (or
signature, if Tuwunel starts publishing one), and the play prints the pinned version and hash as
its own task when they change.

## Checked and found sound

- **Root operations.** Each is needed for what C9 lists: units, firewall, interfaces, users, LXC
  entry, root records. Every homeserver call runs in the `teamchat-ctl` child. Root never writes
  under `ctl/` directly, restore included (the `restore.swap` and `restore.revert` requests). The
  install reads the build output by fd, with `O_NOFOLLOW`, owner and size checks, and hashes the
  bytes it copies.
- **Root never writes under a user's path, for ccy seats.** The owner's process places every
  bundle, by fd (`openat O_EXCL|O_NOFOLLOW`, `mkdirat`, `renameat2`). The L3 probe uses `openat2`
  with `RESOLVE_BENEATH|NO_SYMLINKS|NO_MAGICLINKS`, `fstat` and `LOCK_NB`. Quarantine is removed by
  a walk that never follows a link. The race analysis in step 4 of the take holds.
- **Secrets.** The registration secret is in one root file and reaches the homeserver only through
  `LoadCredential`. It is passed to the child on stdin for bootstrap step 2 only. Tokens travel by
  pipe, never in argv, environment or logs (C13). Root decodes the child's answer strictly and
  fuzzes the decoder. The child has an empty environment, no supplementary groups and no shell or
  sudoers entry.
- **Per-team DynamicUser.** `User=teamchat-%i` gives the same uid to the staging unit, and the
  sandbox is thorough (empty capability set, `IPAddressDeny=any` with per-team allow,
  `SocketBindDeny`, no D-Bus, a resolver stub). A missing drop-in leaves a unit that cannot start.
- **Sibling ccy isolation.** One session directory is mounted read-write and one control directory
  read-only. The pool and `held.json` are never mounted. Released state is quarantined and never
  read again (DC15). The disconnected notice uses the pool's bundle, never the session's copy, and
  judges "goodbye said" from the server.
- **No sudoers file (DC4).** The status, stamp, LXC and set records are root-written, 0640
  root:`<owner group>`, in a 0750 directory, with `seq` ordering under the team lock and a 300 s
  expiry. The set record, which decides the system, is not forgeable by the owner or by a
  container. The sign-in poll is bounded and ends with `sudo -k`.
- **Restore credentials.** The merged roster re-revokes retired, parked and reissued members, and
  humans whose password changed after the backup, before the team is reported restored. The
  `restore.block` assertion stops a start on a half-swapped database.
- **The fence rule itself.** It uses a TCP reset (not ICMP), an input hook and a v2-owned table;
  an accept elsewhere cannot undo it; loopback members are refused at M0; it is proven on each
  route and compared rule for rule; `fence-status` is checked at the start and end of M3 and M4;
  rollback lifts it before discarding v2; M8 removes it last.
- **Area A trust.**
  - Power levels close everything but `m.room.message` (I1-I4).
  - Kind comes from the roster class, never from content.
  - The forge origin is fixed in code.
  - Admin-only state, with a side-room back-link.
  - Invite acceptance pins the creator and inviter.
  - Display names are never read, and the health check reports a mismatch only after a grammar
    check (R39).
  - A carried item is a NOTICE, never a PING or HUMAN, and passes the secret check.
  - Only `teamchat` admin events can be carried.
- **Envelope un-forgeability (A 13.2, 13.6).** Every slot except two is a full-match ASCII grammar
  with no space. Sender text appears only in JSON strings from one escaper, which escapes line and
  paragraph separators, NEL, quotes and their NFKC look-alikes, controls, bidi and `ESC[201~`. The
  coordinator's headline is in ASCII-only mode and attributed. MEMBERSHIP's one free slot is
  admin-written and excludes `<<` and `>>`.
- **Unicode.** One Unicode version is pinned by a test. Unassigned code points are escaped. The
  strict tokenizer refuses lone surrogates, duplicate keys and non-integer numbers before
  `encoding/json`.
- **Ports and network.** Ports are declared and checked against both systems. Bound addresses are
  asserted after start from `/proc/net/tcp`. Firewall rules are judged by exit status and only
  recorded rules are removed. v2 has its own interface.

## Outside this concern (brief)

- D 3.8 says the port check uses `ss`, and C 3.2.5 says no `ss`, reading `/proc/net/tcp`
  in-process.
