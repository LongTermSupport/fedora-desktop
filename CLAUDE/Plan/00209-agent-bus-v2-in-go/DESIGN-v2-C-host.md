# Plan 00209 N0, area C: the host side of v2

This is area C of the v2 design (see `DESIGN-v2-FRAME.md`, which binds it). It owns the
homeserver instances, the host programs and their privilege, the install lock, admin operations,
seats and the bundle a seat receives, the host_vars declarations, the play, LXC member apply,
Element profiles, health, backups and restore, and what ccy-teams reads.

Names (area D, merged). Every name this area used as a placeholder is now area D's final name
(`DESIGN-v2-D-migration.md` section 3.2, and the name table of the N0 integration), written out
in full below. The host program is `teamchat-admin` (root, control-user and owner modes; section
3.3), the member program `teamchat`, the control user `teamchat-ctl`, the homeserver unit
`teamchat-server@<team>.service`, the host-wide timers `teamchat-health` and `teamchat-backup`, and
the host_vars keys `teamchat_*` (section 3.8). The only placeholders left are values:

| Placeholder      | What it names                                                                             |
| ---------------- | ----------------------------------------------------------------------------------------- |
| `<team>`         | the team id (A §2.2 grammar, at most 22 characters), fixed at creation                    |
| `<server_name>`  | the team's Matrix server name, `<team>.teamchat.internal` by default                      |
| `<port>`         | a team's homeserver port, declared per team in `teamchat_teams[].port`; there is no range |
| `<owner group>`  | the desktop user's primary group, recorded by the play in `/etc/teamchat/owner-group`     |
| `<iface>`        | the interface that carries a team's `listen` address (often `teamchat0`)                  |
| `<bus_ip>`       | the v2 bus address, the value of `teamchat_address`                                       |
| `<staging port>` | a free loopback port chosen by `restore` for its staging instance                         |

Source keys used in the "from" column and in the text: `R<n>` = `DESIGN.md` section 2; `H 7.nn` =
`RESEARCH-host.md` hard-won behaviours; `LR` = `RESEARCH-lxc-robustness.md`; `GC` =
`RESEARCH-go-callers.md`; `M<n>` = `SPEC-matrix.md`; `S<n>`/`L<n>` = `SPEC-state.md`; `P<n>` =
`SPEC-proto.md`; `V<n>` = `SPEC-view.md`; `S11` = Plan 00211 `SPEC.md`; `I` = the installer port's
`SPEC-agent-bus-install.md` (Plan 00192, branch `go-port/agent-bus-install`), its bug list as `I-B<n>`.

## 1. What area C is for, in one paragraph

The host runs one private homeserver per v2 team and the tools that set it up and change it. It
is driven by Ansible from host_vars (IaC), and every change to a team (a member, a role, a human's
password) needs the owner's password at the time. Every read the owner's board makes is a file
read: nothing on the read path runs as root, asks sudo, or starts a process per team, per checkout
or per person. A seat's credential never sits in a git checkout and is never visible to a sibling
ccy session. An LXC member is applied by the play from the host, reading the container from the host
side, and never runs container code as root. A v2 install shares no user, path, unit, port,
interface, firewall record or sudoers file with v1, and is inert with no teams declared.

## 2. Requirements

Each requirement names where it came from. `new` marks a requirement v1 did not have, with what it
fixes in section 6.

### 2.1 Homeserver instances

| ID  | Requirement                                                                                                                                                                                                                                                                       | From                   |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| C1  | One private homeserver per team: federation off, registration closed, encryption off, room creation by the team's admin account only, every room invite-only or restricted as area A's layout says.                                                                               | R1                     |
| C2  | A team's homeserver listens on loopback and on each declared `listen` address only, on one port. Inbound traffic is admitted only from `allow_from` CIDRs, by both the firewall and the unit's IP filter. The live LXC bridge subnet is admitted automatically.                   | R3, H 7.21, H 7.36     |
| C3  | A team's id and its `server_name` never change once the team exists; a change is refused by name before anything is touched. A changed id is a removal and a new team.                                                                                                            | R4, H 7.08             |
| C44 | A team can be renamed: its display name and description change, and nothing else does.                                                                                                                                                                                            | 00201 Task 2.16; new   |
| C45 | Every ccy-teams flow the owner uses for a v1 team (create, add and remove members and humans, roles, hot add and remove, sign-in, backup, restore, LXC add) exists for a v2 team, with the same steps from the owner's side; its wording changes only where a v2 rule changes it. | R36                    |
| C4  | Each homeserver runs under its own kernel identity: no homeserver can read another team's files, its own team's admin credential, or any host secret it does not need.                                                                                                            | R5 (strengthened), new |
| C5  | Hardened units: no resolver, no D-Bus, no home, private devices and PIDs, strict system protection, `Type=notify` with a watchdog, `Restart=always` with no start limit, dependency on each listen interface.                                                                     | R5, H 7.22, H 7.23     |
| C6  | The homeserver restarts only when its config, its binary or its unit text changes; a converged apply restarts nothing and prints nothing.                                                                                                                                         | R2, H 7.12             |
| C7  | The resident cost of every homeserver, unit, timer and port is stated per team and measured on the host.                                                                                                                                                                          | FRAME                  |

### 2.2 Programs, privilege and locks

| ID  | Requirement                                                                                                                                                                                                 | From                        |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------- |
| C8  | Every host tool is Go, built from this repository, installed root-owned and not writable by the owner or by any service user. No bash wrapper, no Python, no zipapp on the host for v2.                     | BUILD-BRIEF, CLAUDE.md, R49 |
| C9  | Root does only what needs root: system files, units, firewall, interfaces, service users, LXC entry, reading root-only state. Homeserver calls run as `teamchat-ctl`, never as root.                        | DESIGN 6.1                  |
| C10 | Every change to a team's accounts, roles or humans needs the owner's password at the time (password sudo). A caller that used sudo drops the cached credential before it ends, and a failing drop stops it. | R13, DESIGN 6.2, H 7.28     |
| C11 | The board's open and redraw run nothing as root and nothing through sudo: they read files only. (Reads a human asks for on purpose, `human devices` and `backups`, stay password-sudo verbs.)               | R35, DESIGN 12 (B1), new    |
| C12 | One set of host locks serialises every mutating host operation: the play, the timers and ccy-teams' direct calls all take them. A wait is bounded and a refusal names the holder.                           | I-B16, DESIGN 7, R34        |
| C13 | A secret (admin password or token, registration secret, member token, human password) never appears in argv, environment, a log, an error text or a world- or group-readable file.                          | R11, M3, M9                 |

### 2.3 Admin operations

| ID  | Requirement                                                                                                                                                                                                                                                                                              | From                     |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------ |
| C14 | Bootstrap asserts exactly one server admin on every apply and survives a kill at any point: the admin password is stored before the account is registered and deleted only after the token is stored.                                                                                                    | R40, H 7.02              |
| C15 | Rooms, their state and their power levels are converged to area A's layout on every apply and after every roster change: compare first, write only what differs, kick before invite, and remove a human from every room before any power-level rewrite.                                                  | S11 5.1, R40             |
| C16 | A room created but not recorded (a crash between create and record) is adopted, never duplicated.                                                                                                                                                                                                        | S11 5.1, M35             |
| C17 | At most one coordinator per team at every instant: every role change demotes before it promotes, across every member kind.                                                                                                                                                                               | R10, LR 1.3              |
| C18 | Handles follow area A's grammar, `<repo>.<seat>+<site>.<type>` (A §2.3; v1's P5 shape, with `site` never the machine hostname). Seat numbers per prefix never go down, a restore included. A parked handle returns with a new token and its account. A retired handle never returns, a restore included. | R9, P5                   |
| C19 | Park revokes (password reset with device logout, the one revoke the homeserver has) before it marks. Retire kicks from every room, deactivates, and records the handle as never to return.                                                                                                               | R11, H 7.10, M29         |
| C20 | An admin operation that dies part-way is completed by the next run of the same operation (roll forward from a recorded intent), never left half-done and never silently re-minting a credential.                                                                                                         | new (fixes v1 undo gaps) |
| C21 | A lost bundle is recovered by one explicit operation that revokes the lost token and issues a new one for the same handle.                                                                                                                                                                               | LR 6 item 8, new         |
| C22 | Humans are declared; a human's password is printed once to the owner and never stored; a human's device count and newest sign-in are visible on the board.                                                                                                                                               | R11, R35, DESIGN B2      |

### 2.4 Seats and bundles

| ID  | Requirement                                                                                                                                                                                                                                                  | From                                              |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| C23 | One account and one token per seat; two seats never share either.                                                                                                                                                                                            | S20                                               |
| C24 | A seat's token is placed by the user who will hold it, by file descriptor, with no-follow and exclusive create, mode 0600, never by root under a user's path.                                                                                                | R12, H 7.31-7.33                                  |
| C25 | A seat's credential is never inside a git checkout, and no ccy session can see another session's seat. (A host `cc` session runs as the owner and can read every file the owner has; see K12.)                                                               | new (fixes 00203 D6, S11 security note, 00194 R4) |
| C26 | Seat allocation is serialised per checkout by a lock no container can reach; a seat a live or restorable session holds counts as held; launching into a held seat is refused.                                                                                | R14, 00203 H20/H21                                |
| C27 | The host can tell whether a seat is held (the member's seat lock) without entering any container.                                                                                                                                                            | R27, H7 (DESIGN 16)                               |
| C41 | Nothing a container wrote is trusted by the host or handed to another session: a seat's bundle is the host's copy, and a session's member state never travels to another session.                                                                            | R39, 00203 D2/D3, new                             |
| C42 | A session's teams are changed while it runs (join and leave) through a desired-membership record the host writes and the session can only read; the host writes the session record before the desired record; an active team is one both seated and desired. | R31, R32, 00203 H5/H8                             |
| C43 | When a session ends without saying goodbye on a team, the member's own disconnected notice is posted for that seat, by the host as the owner with the seat's token, once per claim, never as well as a goodbye.                                              | R29                                               |

### 2.5 Declarations, the play, LXC

| ID  | Requirement                                                                                                                                                                                                                              | From                   |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| C28 | host_vars stays the IaC store; the play is the one route that turns a declaration into a running team. v2 keys are its own; v1's keys are never written by v2, and the one read is `agent_bus_teams[].port` by `declare check` (D §3.8). | D6, R2, FRAME          |
| C29 | Every change that runs a play or writes host_vars takes the play lock first and writes host_vars only once it holds it.                                                                                                                  | R34                    |
| C30 | The declaration has one truth per fact: the team's coordinator is declared in one place, not in both a roster and a member entry, and a coordinator can be declared before its member exists.                                            | LR 6 item 6, new       |
| C31 | LXC members are applied by the play from the host. The container's checkout and accounts are read from the host side with no-follow walks and size caps. No container program runs as root, and no container needs Python.               | R33, R48, LR 6 item 10 |
| C32 | A stopped LXC container is ordinary (its member held back and recorded); only a missing container, or a failure in a running one, fails the apply. Everything checkable is checked for every running container before anything changes.  | R33, R48, LR 1.5       |
| C33 | Kits are versioned directories resolved once at launch, never replaced in place under a running session.                                                                                                                                 | R49, DESIGN B9         |

### 2.6 Element, health, backups, the board

| ID  | Requirement                                                                                                                                                                                                                                             | From                |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- |
| C34 | Element profiles per team, locked down, written as the desktop user; refused for a user who runs host-kind v2 members unless the owner has recorded consent.                                                                                            | R50, R37            |
| C35 | Health: a minutely probe of every team; a homeserver missing three probes is restarted; systemd restarts are noted; backups are checked; room state drift (A's layout) is reported with its repair. Records expire so a stale one is itself the signal. | R7, S11 5.4         |
| C36 | Daily backups of every team's database and control state as one numbered pair; a backup before any restart that changes the homeserver's binary or config, whatever starts it; restore validated before anything stops.                                 | R6, H 7.14-7.16     |
| C37 | A failed restore leaves the team as it was before the restore, not stopped.                                                                                                                                                                             | new (fixes I-B8)    |
| C38 | The applied stamp lets the board say up to date, out of date, not built, or decide, from files and in-process digests only.                                                                                                                             | R8, D65             |
| C39 | ccy-teams opens in about 1 s and redraws in under 200 ms with any number of teams, checkouts and people: one declaration parse, a fixed number of file reads per team, one systemd query, no sudo, no Python, no per-item process.                      | R35, R36, GC 5, D13 |
| C40 | Nothing v2 installs touches anything v1 uses, and a v2 install with no teams runs no resident process.                                                                                                                                                  | FRAME               |

## 3. Design

### 3.1 Who runs what

| Identity                     | Runs                                                                                                                           | Holds                                                                      |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------- |
| root                         | `teamchat-admin` root verbs: install, team apply/remove, backup, restore, health, LXC apply, and the entry of every admin verb | root records, backup tars, rendered homeserver configs (with their secret) |
| `teamchat-ctl` (system user) | `teamchat-admin ctl-child`, started only by root, for every homeserver call                                                    | each team's admin token, roster and room records                           |
| one dynamic uid per team     | that team's homeserver (`DynamicUser=yes`)                                                                                     | that team's database only                                                  |
| the owner (desktop user)     | ccy-teams, seat placement, Element profiles, `teamchat-admin status`                                                           | the seat pool and session seat directories, Element profiles               |
| an LXC guest's agent user    | `teamchat` (area B), and `teamchat guest` operations started by the host through `lxc-attach -u`                               | its own bundles in its home                                                |

Nothing in this table is resident on the host except the homeservers. The health and backup
timers start a short-lived process. There is no daemon, no socket and no reader with a sudoers
rule (decision DC4).

The flows the owner sees, each in one line:

- **Apply** (play, or ccy-teams running the play): render the declaration, `teamchat-admin team apply`
  per team as root, which writes system files, starts the homeserver, and runs bootstrap in a
  `teamchat-ctl` child.
- **Change** (ccy-teams, launcher): `sudo teamchat-admin member add|park|...` with the owner's password;
  root takes the team lock, runs the change in a `teamchat-ctl` child, prints a typed JSON answer; the
  owner's process places any bundle itself.
- **Read** (ccy-teams): host_vars, the per-team stamp and status records, one systemd query, the
  owner's own seat directories. No sudo.

### 3.2 The homeserver instance

#### 3.2.1 One homeserver per team, or one for every v2 team

The owner asked for this to be weighed honestly. Both designs, side by side:

| Concern                                  | One homeserver per team (chosen)                                                    | One homeserver for every v2 team                                                                                                       |
| ---------------------------------------- | ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Resident memory                          | one Tuwunel per team, 100-135 MB RSS each as measured in the 00161 spike (C7, H-C1) | one Tuwunel, roughly the cost of one team plus a little per room                                                                       |
| Ports and firewall rule sets             | one per team                                                                        | one                                                                                                                                    |
| Units                                    | one service per team (the timers are host-wide, 3.11)                               | one service                                                                                                                            |
| Blast radius of a compromised homeserver | that team                                                                           | every team: every agent of every team can be instructed by it                                                                          |
| Admin credential scope                   | one team                                                                            | one admin token commands every team                                                                                                    |
| Leaks between teams                      | none: different servers                                                             | the user directory, profile lookups, presence and room previews cross teams unless each is configured off and kept off across upgrades |
| Restore                                  | restores one team                                                                   | Tuwunel's backup is the whole database: restoring one team rolls back every team                                                       |
| Remove a team                            | stop it and delete its directory                                                    | delete rooms and deactivate accounts inside a shared database; Matrix has no clean purge, so the team's history stays in the database  |
| Restart for a config change              | that team only                                                                      | every team's sessions see the homeserver vanish                                                                                        |
| Sign-in throttling buckets               | per team                                                                            | shared: one team's failures can lock another's human out                                                                               |
| Element for a human in N teams           | N profiles, N windows                                                               | one profile, one window                                                                                                                |
| Per-team migration from v1               | a team arrives with its own server name and leaves nothing behind on rollback       | every migrated team shares the server name; a rollback has to delete one team's rooms and accounts from a shared database              |

The resident memory is the real cost of the chosen design, about 100-135 MB per team at the
default cache size, growing linearly with the number of teams (H-C1 measures it per team and per
member; 3.15). Everything else in the table favours one homeserver per team. The memory is also tunable: Tuwunel
sizes its caches from `cache_capacity_modifier`, and a team with a handful of members needs far
less than the default. The first host check (H-C1) measures RSS at the default and at a reduced
modifier with a real team's load, and the chosen value goes into the rendered config. The unit
also carries `MemoryHigh=` set from that measurement, so one homeserver cannot crowd the
desktop. There is deliberately no `MemoryMax=`: a hard cap turns a busy moment into a kill and a
restart loop, which is worse than a throttle. Decision DC1 records the choice.

Rejected as well: starting a team's homeserver only on demand. Humans' Element windows and every
member's keeper hold a long-poll sync, so a team in use is up all the time anyway, and Tuwunel has
no socket activation to make the first connection cheap.

#### 3.2.2 Identity and files

v1 ran every team's homeserver as one user and kept them apart with a mount trick
(`TemporaryFileSystem` over the shared state root plus `PrivatePIDs`, H 7.22): without it a sibling
homeserver could reach another's secrets through `/proc/<pid>/root`. v2 gives each homeserver its
own kernel identity instead (C4): `DynamicUser=yes` with `StateDirectory=`. systemd allocates a
stable uid per unit name, owns the state directory by it, and puts it under the root-only
`/var/lib/private`. Two homeservers then differ by uid, so isolation no longer rests on a sandbox
detail that one missing line would undo. Decision DC2 holds the alternative (one shared service
user, v1's way).

The homeserver no longer holds the team's admin credential either. In v1 the admin token sat in
the homeserver user's own directory, so a compromised homeserver held the admin token of its team.
A compromised homeserver can forge anything in its own team anyway, so this is defence in depth,
not a boundary. It still matters: the token and the registration secret survive in backups and
are copied into restores, and there is no reason for the network-facing process to hold the
credential that every admin operation and every backup trusts.

Files of one team. They live under the two root-owned state roots, `/var/lib/teamchat-admin/`
(root's records) and `/var/lib/teamchat-ctl/` (the control user's). Here and below `root/<team>/`
stands for `/var/lib/teamchat-admin/<team>/` and `ctl/<team>/` for `/var/lib/teamchat-ctl/<team>/`.
The homeserver's own state is the exception: systemd places it at
`/var/lib/private/teamchat/<team>/`, reached through the `/var/lib/teamchat/<team>` link it
maintains.

| Path                                                                                   | Owner and mode       | Content                                                                                                                                                                                                                 |
| -------------------------------------------------------------------------------------- | -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/var/lib/private/teamchat/<team>/` (systemd StateDirectory, under neither state root) | dynamic uid, 0700    | `db/`, `backups/` (Tuwunel's own backup engine output)                                                                                                                                                                  |
| `root/<team>/config.toml`                                                              | root, 0600           | the rendered Tuwunel config, including the registration shared secret; reaches the unit only through `LoadCredential=`                                                                                                  |
| `root/<team>/team.json`                                                                | root, 0600           | the rendered team declaration last applied (canonical JSON); `server_name` is checked against it (C3)                                                                                                                   |
| `root/<team>/firewall.json`                                                            | root, 0600           | the firewall rules this team added, written before any firewall change (H 7.11)                                                                                                                                         |
| `root/<team>/units.json`                                                               | root, 0600           | the restart and reload digest sets (3.5.2), and the Tuwunel version, port and listen set last started, so a team brought back can be started on its old version first (a port another team now holds refuses it, named) |
| `root/<team>/backups/<n>.tar`                                                          | root, 0600, dir 0700 | the control state paired with database backup `<n>` (3.12)                                                                                                                                                              |
| `root/<team>/server-name`                                                              | root, 0600           | the team's `server_name`, written before the homeserver's first start; C3 is checked against it (3.5.2)                                                                                                                 |
| `ctl/<team>/`                                                                          | `teamchat-ctl`, 0700 | `admin.token` 0600, `admin.password` 0600 (only during bootstrap), `roster.json` 0600 (its `pending` is the recorded intent, 3.6), `rooms.json` 0600                                                                    |

`/var/lib/teamchat-admin` is 0700 root and `/var/lib/teamchat-ctl` is 0700 `teamchat-ctl`. Root writes under
`ctl/` only through the `teamchat-ctl` child, never directly, so the control user cannot steer a root
write (the same reasoning that kept v1's root records out of the homeserver user's tree, H 5).
That holds for a restore as well (3.12.2).

The registration secret is created once by root at the first apply of a team (64 random bytes as
128 lowercase hex, no newline) and lives in one place only: `root/<team>/config.toml`. Creation is
link-not-replace (H 7.03), and the team lock (3.4) already excludes a second run. The `teamchat-ctl`
child never stores it. Root reads it from the config and passes it on the child's stdin, inside
the one request, only for the operation that registers the admin account (bootstrap step 2,
3.6.2); every other request carries no secret but the child's own admin token, which it reads
itself.

#### 3.2.3 The unit

The template `teamchat-server@` (installed by `software`, 0644 root):

```ini
[Unit]
Description=v2 team homeserver %i
StartLimitIntervalSec=0
AssertPathExists=!/var/lib/teamchat-admin/%i/restore.block

[Service]
Type=notify
NotifyAccess=main
WatchdogSec=30s
TimeoutAbortSec=30s
TimeoutStopSec=330
Restart=always
RestartSec=5s
RestartSteps=5
RestartMaxDelaySec=60s
DynamicUser=yes
User=teamchat-%i
StateDirectory=teamchat/%i
StateDirectoryMode=0700
LoadCredential=config.toml:/var/lib/teamchat-admin/%i/config.toml
Environment=TUWUNEL_CONFIG=%d/config.toml
UMask=0077
NoNewPrivileges=yes
CapabilityBoundingSet=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
PrivatePIDs=yes
PrivateIPC=yes
ProtectProc=invisible
ProcSubset=pid
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
IPAccounting=yes
IPAddressDeny=any
SocketBindDeny=any
TemporaryFileSystem=/run/systemd/resolve:ro
BindReadOnlyPaths=/usr/local/share/teamchat/resolv.conf:/run/systemd/resolve/stub-resolv.conf
BindReadOnlyPaths=/usr/local/share/teamchat/resolv.conf:/run/systemd/resolve/resolv.conf
InaccessiblePaths=-/run/dbus
```

The base unit has no `ExecStart=`. The per-team drop-in `teamchat-server@<team>.service.d/team.conf`,
rendered by `team apply`, names the binary by version:

```ini
[Unit]
After=sys-subsystem-net-devices-<iface>.device
Wants=sys-subsystem-net-devices-<iface>.device

[Service]
ExecStart=/usr/local/lib/teamchat/server/tuwunel-<version>
MemoryHigh=<from H-C1>
IPAddressAllow=127.0.0.1/32 ::1/128 <each listen /32 or /128> <each allow_from>
SocketBindAllow=tcp:<port>
```

What each choice is for, beyond v1's reasons (H 5, H 7.22-7.23), which all still apply:

- `DynamicUser` and `StateDirectory` replace the shared user, `TemporaryFileSystem` over the
  state root and `BindPaths` (C4). `User=teamchat-%i` fixes the dynamic user's name. Without it
  systemd derives the name from the unit name, which holds an `@` and so falls back to a hashed
  name. More to the point, the restore's transient unit (3.12.2) names the same user and so gets
  the same uid, and nothing is ever chowned. A team id is at most 22 characters (A §2.2), so
  `teamchat-<team>` fits systemd's 31-character limit; `declare check` refuses a longer team id by
  name.
- The binary is named per team, in the drop-in, by version. A team's homeserver changes binary
  only when `team apply` rewrites that team's drop-in, and `team apply` backs the team up first
  (3.5.2 step 6). A crash restart, a watchdog kill, the health tick's restart, an apply that
  fails early, and every team a scoped play does not apply all restart onto the binary the
  drop-in already names. A unit with no drop-in cannot start at all, because it has no
  `ExecStart=`. An old `tuwunel-<version>` is deleted by `team apply` or `team remove` only when
  no drop-in names it, no `units.json` of a team that still has a database (one removed without
  `--purge` included) records it, and it is not the pinned version.
- The base unit is shared by every team, so its text cannot be pinned per team. A play that
  changes the base unit's text therefore applies every present team, whatever `teamchat_only_teams`
  says, and prints that it did so and why (3.5.1); otherwise an unscoped team would first meet a
  new hardening directive at an unplanned crash restart.
- `LoadCredential` puts the config, with its secret, in a directory only this unit's uid can read,
  from a root-only file. The homeserver cannot rewrite its own config, which in v1 sat beside its
  database and was readable by it.
- `CapabilityBoundingSet=` empty, `SystemCallFilter=@system-service`, `ProtectProc=invisible` and
  the rest are ordinary systemd hardening v1 did not have. Each is checked by H-C2 against a real
  Tuwunel start (a Rust binary built with jemalloc can trip `MemoryDenyWriteExecute`, so that
  one is left out on purpose).
- The base unit denies all addresses and binds, and has no `ExecStart=`, so a missing drop-in
  leaves a homeserver that cannot start rather than one that can be reached from anywhere.
- `AssertPathExists=!.../restore.block` stops any start, a boot's included, while a restore has
  the database directory in pieces (3.12.2); an assertion fails the start visibly, where a
  condition would skip it silently.

#### 3.2.4 The config

Rendered by `team apply` from the declaration, in Go, as bare top-level keys (H 7.05, a file with
any bare key is read whole as the global section) plus the `[rate_limiting.login.*]` tables. The
key set is v1's (digest 1, section 2.2), kept because each key has a recorded reason:
registration closed, federation off, `trusted_servers = []`, encryption off, room creation by the
admin only, invites not auto-accepted, `login_via_token` and existing-session login off, warn-level
logging with no headers, the login limits, `database_backups_to_keep = 7` (restore depends on
the order this forces, 3.12.2), `max_request_size = 10000000` (Tuwunel's floor),
`rocksdb_allow_fallocate = false` on btrfs, `enable_set_displayname = false`,
`admin_signal_execute` set to the backup command, and `error_on_unknown_config_opts = true` so a
key this Tuwunel does not know stops it at start. Added in v2: `database_backup_path` inside the
state directory, `cache_capacity_modifier` from H-C1, and `unix_socket_path` is not used (members
in containers need TCP, and one transport is enough).

The renderer is tested against a vendored list of the pinned Tuwunel version's config keys (v1 kept
one for 1.9.4); a pin bump that drops or renames a key fails the test before the host sees it.

#### 3.2.5 Network

- Each team has one `<port>`, declared per team in `teamchat_teams[].port`; there is no range.
  `declare check` refuses, by D §3.8's rule, a port that another `teamchat_teams` entry declares,
  that any `agent_bus_teams` entry declares (the declaration only, never v1's runtime files, so
  v2 cannot collide with a v1 team that is stopped), or that is listening now and is not this
  team's own `teamchat-server@<team>`. `team apply`, which sees one team, checks again at its
  step 2 against the ports other v2 teams' `units.json` record and against the live listeners.
- Listen addresses: loopback plus each declared `listen` address, each of which must be on exactly
  one interface. After start, the bound address set is asserted equal to that list before
  bootstrap (H 7.21). Both this check and the port checks above read `/proc/net/tcp` and
  `/proc/net/tcp6` in-process (state `LISTEN`, address and port decoded from the fixed hex
  columns); no `ss` and no text from another program is parsed.
- v2 has its own bus address interface `teamchat0` (a NetworkManager dummy connection with one
  address), so retiring v1's interface later cannot break a v2 team. Its address comes from the
  declaration (`teamchat_address`).
- Firewall: per-team firewalld rich rules, one per `allow_from` CIDR, in the zone of the interface
  that routes that CIDR (the default zone if none). The rule set is recorded in
  `root/<team>/firewall.json` before any firewall change, and only recorded rules are ever removed
  (H 7.11). Rules are tested for presence with `firewall-cmd --query-rich-rule` by exit status,
  never by reading text (I-B12): 0 is present, 1 is absent, and any other code fails the verb
  with the code named. The permanent configuration (`--permanent`) and the runtime are queried
  separately, and each is converged on its own. Decision DC12 holds the D-Bus alternative.
- The live LXC bridge subnet is added to every team's `allow_from` at render time, computed
  in-process from the bridge interface, as v1's `bridge.py` did (R3).

### 3.3 The host program

One Go binary, `teamchat-admin`, built from `src-go/cmd/teamchat-admin` by the play as the login user and
installed by root, 0755 root:root, in a root-owned directory (C8). The first thing `main` does is
decide the caller class from the real and effective uid, and each verb belongs to exactly one
class. A verb asked of the wrong class exits 77 naming the class it needs (DESIGN 3.1).

| Caller class   | How it is reached                                              | Verbs                                                                                                                                                                                                                                                                                                               |
| -------------- | -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| root           | the play (`become`), the two timers, the owner's password sudo | `team apply`, `team remove`, `member add\|return\|park\|retire\|reissue`, `role apply`, `human password\|devices\|logout\|lock\|unlock`, `backup`, `backups`, `restore`, `health`, `lxc apply`, `check`, `migrate stage\|fence\|unfence\|fence-status\|carry\|commit\|archive\|archive-check --team T`, `footprint` |
| `teamchat-ctl` | only `ctl-child`, started by root with `os/exec` `Credential`  | `ctl-child` (reads one typed request on stdin, answers one typed response on stdout)                                                                                                                                                                                                                                |
| anyone else    | the owner, the play before any root task, the Element play     | `declare check`, `element check\|apply`, `status`, `version`                                                                                                                                                                                                                                                        |

The `migrate` verbs and `footprint` behave as area D says (D §5.4, §5.5); C provides them in the
same binary, and each `migrate` verb takes the team lock like every root verb.

`version` answers for every class and prints the binary's source digest (the scheme of Plan 00192:
`go.mod` plus every non-test file of the command and the module packages it imports, baked in with
`-ldflags -X`). The board compares it with the digest of the checkout (3.13).

Why one binary rather than one per privilege: the root verbs and the `teamchat-ctl` child share almost
every package (declaration types, roster, rooms engine, Matrix client), one binary has one digest
to compare and one install step, and the caller-class check is a single table tested once. The
reason v1's design had a separate reader (a NOPASSWD sudoers rule needs a narrow target) is gone
with DC4. Decision DC3 holds the alternative.

The root and `teamchat-ctl` halves talk only over a pipe in typed JSON:

```json
{"v": 1, "op": "member.add", "team": "<team>", "args": {"prefix": "<repo>+<site>.<type>", "seat": "3", "human_text": true}}
```

```json
{"v": 1, "ok": true, "changed": ["member <handle> added"], "bundle": {"...": "see 3.7"}}
```

Root decodes the answer strictly (unknown or duplicate keys refused, sizes capped), because it is
the one thing root parses that a less-privileged process wrote. The child starts with no
supplementary groups, `chdir /`, an empty environment except `PATH`, and the request on stdin; it
refuses arguments other than `ctl-child`. No other caller can become `teamchat-ctl`: it has no shell,
no password and no sudoers entry.

Exit codes are one table for every verb: 0 done, 1 failed, 64 usage, 69 homeserver unreachable,
70 refused (a rule said no), 75 busy (a lock held, the holder named), 77 wrong caller class, 78
configuration. A child process's exit code is never passed through as one of these (I-B3); its
failure is 1 with a message. Output contract: stdout carries only the payload (`CHANGED<TAB><what>`
lines for root verbs the play runs, one JSON document for verbs ccy-teams runs), everything else
goes to stderr as `teamchat-admin: <message>`.

### 3.4 Locks

| Lock                           | Kind and place                                                        | Taken by                                                                                                                                                                                                                        |
| ------------------------------ | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| play lock                      | owner, `flock`, owner runtime directory                               | every play run and every host_vars write (C29). It is the repository's lock for every play, not part of v1's bus, so v2 using it touches nothing of v1. Root verbs never take it.                                               |
| team lock                      | root, `flock` on `/run/teamchat-admin/team-<team>.lock`, 0600         | exclusive by every root verb that changes or restarts one team: apply, remove, member, role, human, backup, restore, the health restart and status write, LXC apply per team, each `migrate` verb (inside D's set lock, D §5.3) |
| allocation lock (per checkout) | owner, `flock`, `$XDG_RUNTIME_DIR/teamchat/alloc/<checkout key>.lock` | ccy-teams around choosing and taking a seat in one checkout (C26)                                                                                                                                                               |
| seat lock                      | member, `flock`, in the seat directory                                | the member's keeper while it holds the seat (area B); the host only probes it                                                                                                                                                   |

Rules:

- There is no host-wide lock, because nothing host-wide changes outside the play's static tasks
  (3.5.1), and those swap every file atomically: a new `teamchat-admin` is a new versioned file and a
  link swap, so a verb already running keeps its open file and the next one starts on the new
  one. A new Tuwunel is only a new versioned file; no homeserver uses it until its own team's
  apply names it (3.2.3).
- Order: play lock, then allocation lock, then (inside root) D's migration set lock where a
  `migrate` verb holds one, then team locks in team id order. A
  verb that touches several teams (LXC apply, the timers) takes each team's lock in turn, never
  two at once, except where one change spans two teams, which none does today. Root never takes
  an owner lock, and an owner process never takes a root lock, so the two halves cannot deadlock.
- Every root lock is `flock(2)` on a file that is never deleted, opened `O_NOFOLLOW|O_CLOEXEC` in a
  0700 root directory under `/run`, so a crashed holder frees it and no container can reach it.
  The holder writes one line into the file: `<verb> <team> pid=<n> since=<ms>`. A waiter that gives
  up prints that line and exits 75.
- Waits are bounded and depend on the caller: a seat take from a launcher waits up to 60 s; a play
  step and the backup timer wait up to 10 minutes; the health timer never waits (below).
- The health tick reads and probes without the team lock: its probes and its read-only child
  (3.11.2) change nothing. It takes the team lock, without waiting, only to restart a homeserver
  and to write the status record, and it writes the record only if the record's own `seq`
  (3.11.3) is the one it read before probing. Every writer of the record (every change verb,
  `human devices`, apply, backup, restore) raises `seq` under the team lock, so a writer that ran
  in between has written a fresher record, and the tick's view is dropped. If another verb holds the lock, the tick
  neither restarts nor writes for that team; it notes `busy: <holder>` in its own journal line
  and exits 0. This is not a skipped failure: the holder is the one restarting or changing that
  team and checks readiness itself, and the status record the board reads carries its own expiry
  (3.11), so a holder that never lets go shows on the board as a stale record within five
  minutes. Because the probes run outside the lock, a launcher's seat take never waits behind a
  tick's homeserver calls.
- A verb that holds a team lock and needs a backup (`team apply`, 3.5.2 step 6) calls the backup
  as an internal function that takes the held lock as an argument; it never opens and locks the
  file a second time, which would block on its own lock.
- The team lock is held across the admin child's homeserver calls on purpose: it is what makes a
  roster change single-writer. Each call is bounded (M6: 30 s, three tries on 429), so a hold is
  bounded too.
- No lock is held while the owner types a password: ccy-teams makes sudo ready first (the
  `internal/sudo` path), and only then takes the allocation lock and calls root.

Decision DC11 holds the alternative (one host-wide lock for everything, as v1's never-built D4
proposed): simpler to state, but a daily backup or a slow apply of one team would then block a
seat take in another.

### 3.5 Install and apply

v1 put everything in its installer, static install included, because ccy-teams and the timers
called it too. v2 splits by what each part is: what Ansible does well (packages, users,
directories, a checksummed download, unit files, a group record) is plain play tasks, and what
needs ordering against readiness, typed validation or Matrix calls is `teamchat-admin`.

#### 3.5.1 The play's static tasks

The v2 play (`play-teamchat.yml`, its own file, so v1's play is untouched) runs in
this order. Steps 1 and 2 run as the login user and change nothing outside the build cache; every
later step is root.

01. `teamchat-admin` and `teamchat`: built by `tasks/build-go-command.yml` as the login user (no
    toolchain runs as root), into the user's build output, not yet installed.
02. The freshly built `teamchat-admin declare check`, run unprivileged (it is an "anyone" verb, 3.3),
    with the Ansible-parsed `teamchat_teams`, `teamchat_lxc_members` and `teamchat_element_teams`, and the
    ports of `agent_bus_teams`, as JSON on stdin (3.8). So the binary that will apply the schema is the one that judges it, on a fresh
    host as on an upgrade. Nothing after this step runs if it refuses.
03. Install: root copies the built `teamchat-admin` to `/usr/local/lib/teamchat/teamchat-admin-<digest>` 0755 root and
    swaps the link `/usr/local/sbin/teamchat-admin`; `teamchat` goes to
    `/usr/local/lib/teamchat/kit/<digest>/` with a `current` link (C33). (The ccy image carries its
    own single kit at `/opt/claude-yolo/optional/teamchat`, area B; the play does not write it.) Root
    opens each build output with `O_NOFOLLOW|O_NONBLOCK`, `fstat`s it (a regular file owned by
    the login user, under a size cap), and hashes the bytes it copies from that one fd, so the
    copy can never be turned into a read of a root-only file. The hash is checked against the one
    the build step reported; both come from the owner's side, so this guards against a torn or
    swapped file, not against the owner (K9). No root step below runs an older `teamchat-admin`.
04. Packages: `firewalld`, `NetworkManager`, `zstd`, `lxc` only if any LXC member is declared.
    Nothing else: no Python, no `curl`, no `jq`, no `tcpdump` (v1 needed each for its bash or its
    acceptance).
05. The `teamchat-ctl` system user (no home, `nologin`), the two state roots
    (`/var/lib/teamchat-admin/` 0700 root, `/var/lib/teamchat-ctl/` 0700 `teamchat-ctl`), `/var/lib/teamchat-status` 0750 root:`<owner group>` (it holds `installed.json`,
    `teams/<team>/stamp.json` and `status.json`, `lxc/<team>.json`, and `sets/<set-id>.json`,
    which D's migration verbs write, D §5.3), `/etc/teamchat/owner-group`
    (root 0644, one group name from `getent` of the desktop user, read by every writer of an
    owner-readable record; no default, as v1's stamp-group file, H 7.18).
06. Tuwunel: `get_url` of the pinned release asset with `checksum: sha256:<pin>` into a root-only
    staging directory, `zstd -d` with `creates:`, install as `/usr/local/lib/teamchat/server/tuwunel-<version>` 0755
    (atomic rename of a complete file). There is no shared link: no homeserver runs it until its
    own team's `team apply` names it in that team's drop-in, after that team's backup (3.2.3,
    3.5.2). The pin lives in `vars/version-pins.yml` beside every other pin, so the
    `update-versions` skill sees it. The release is unsigned, so the pin is the supply-chain
    check, as in v1.
07. Units: `teamchat-server@.service`, `teamchat-health.{service,timer}`, `teamchat-backup.{service,timer}`, the
    resolver stub, all 0644 root; one `daemon_reload` if any changed. The two timers are enabled
    only while at least one team is present, so a v2 install with no teams starts nothing (C40).
08. The bus address: a NetworkManager dummy connection `teamchat0` with `teamchat_address`, if declared.
09. `/var/lib/teamchat-status/installed.json` (0640 root:`<owner group>`): the digest of the installed
    `teamchat-admin`, the Tuwunel version and the kit digest, which the board reads (3.13).
10. `teamchat-admin lxc apply --phase absent` with the LXC member declaration on stdin, while every
    team's homeserver still runs, so a member that leaves is parked on a live server (3.9.2,
    LR 1.6).
11. For each absent team: `teamchat-admin team remove`.
12. For each present team: `teamchat-admin team apply` with the team's rendered declaration on stdin.
13. `teamchat-admin lxc apply --phase present` with the LXC member declaration on stdin.

Every `teamchat-admin` step reports changed only on a `^CHANGED\t` line. `teamchat_only_teams`
scopes steps 10-13 to the teams ccy-teams is changing, as v1's `agent_bus_only_teams`
does. A team outside the scope keeps its drop-in, and so its Tuwunel version, until a play
applies it. The one exception: when step 7 changed the base unit's text, steps 12 and 13 run for
every present team, scope ignored, and the play prints a task saying so (3.2.3). No step supports `--check` except step 2, which is read-only.

There is no sudoers file in v2 (DC4), and there is no v2 change to the ccy image except what area
B needs for the member program and the seat mount (3.7).

#### 3.5.2 `team apply`

Input: one team's declaration as JSON on stdin, decoded strictly against the same Go type the
declaration check uses. Steps, each failing the verb with the step named:

01. Root, Fedora, input valid, the team lock (wait up to 10 minutes).
02. Refusals before any change: a `server_name` different from `root/<team>/server-name` (C3); a
    port another v2 team's `units.json` records, or that is listening and is not this team's own
    `teamchat-server@<team>` (the declared collisions, v1's included, are `declare check`'s, 3.2.5,
    D §3.8); a `listen` address on no interface or on more than one; a wildcard `listen`.
03. Render in memory: `config.toml`, the drop-in, the canonical `team.json`. Compare with what is
    installed. `units.json` keeps two digest sets. The restart set is the base unit text, the
    drop-in's `ExecStart=` (the Tuwunel version) and the config: a change there needs a restart.
    The reload set is the drop-in's `IPAddressAllow=`, `SocketBindAllow=`, `MemoryHigh=` and its
    `[Unit]` ordering: cgroup properties take effect on `daemon-reload`, and ordering does not
    touch a running unit, so a change there needs only `daemon-reload` and drops nobody's sync (C6).
    The answer is one of: nothing to do; write without restart (`team.json` only, for example a
    rename or a description change, below); write and reload; write and restart; first start.
04. First apply only, before the homeserver ever starts: create the `root/<team>/` and
    `ctl/<team>/` directories, the registration secret (3.2.2), and `root/<team>/server-name`
    (link-not-replace). From this point the `server_name` is fixed, whether or not the rest of
    the apply succeeds, so a first apply that fails after the database was created cannot be
    followed by a run with a different name.
05. Firewall: write the union of the old and new rule sets to `firewall.json`, then add the new
    rules (permanent and runtime, no reload), then remove the old ones no longer wanted, then write
    the new set alone (H 7.11).
06. If a database exists and the restart set differs from `units.json` (a "restart", or a "first
    start" of a team removed without `--purge` and brought back): back the team up first (3.12),
    through the internal backup function under the lock this verb holds (3.4). A homeserver that
    is inactive is started first on the version `units.json` records (re-writing that drop-in
    if `team remove` deleted it), so the backup is taken by the binary that wrote the database. A failed backup stops the apply before anything is rewritten
    or restarted (R6).
07. Write the config and drop-in (atomic, explicit modes), `daemon-reload` if the drop-in changed,
    then start the unit (first start), restart it (restart set changed) or leave it running
    (reload set only), and enable it.
08. Wait ready: the unit must be `active` and `/_tuwunel/server_version` must answer, within 60 s.
    An inactive or failed unit fails at once with the journal tail printed only as explanation; the
    verdict is the unit state, never a journal scan (H 7.13).
09. Assert the bound address set is exactly loopback plus `listen` (H 7.21).
10. Bootstrap in the `teamchat-ctl` child (3.6.2).
11. Write `team.json` and `units.json`, then the stamp `/var/lib/teamchat-status/teams/<team>/stamp.json` last,
    so a stamp always means fully applied (H 6). Then refresh the status record (3.11). The first
    apply of a team writes `"origin": "v2"` in it, or `"origin": "migration"` when the team is
    under a migration (`root/<team>/migration/state.json` exists, D §3.9, §5.3); later applies
    keep the value.

The stamp:

```json
{"v": 1, "team": "<team>", "config": "<sha256 of canonical team.json>", "software": "<teamchat-admin digest>", "tuwunel": "<version>", "applied_ms": 0}
```

**A rename** (C44, Plan 00201 Task 2.16). The owner edits `name` (and, if wanted, `description`)
in host_vars and runs the play. `team apply` sees the changed `name` or `description` in step 3 as
a write without restart, and in bootstrap's rooms step (3.6.2) rewrites A's `teamchat.roster` event (`rev` + 1, A §5.1 `name`), the
three rooms' `m.room.name` (and the main room's topic for a description), then in step 11 the
status record. The Element play rewrites the launcher's `Name=` (3.10). Nothing restarts, and no
account, token, bundle, path or unit changes: the id, `server_name` and every name built from them
stay as they are.

#### 3.5.3 `team remove [--purge]`

Stop and disable the unit, remove the recorded firewall rules only, the drop-in, the stamp and the
status record; with `--purge` also `systemctl clean --what=state` for the homeserver's state
directory, `root/<team>/` and `ctl/<team>/`. A team that is not installed is a success that prints
nothing (the play relies on it; I-B18 kept). Without `--purge`, the team's data stays and a later
`state: present` brings it back with its accounts and history.

#### 3.5.4 `check --team T`

Prints `CHECK<TAB><fact><TAB>ok|FAIL|info<TAB><value>` for the unit's state and sandbox
properties, the drop-in, the firewall rules, the bound addresses and the rooms' drift (3.11), and
exits 1 on any `FAIL`. It is what the acceptance runs; it changes nothing.

### 3.6 Admin operations

#### 3.6.1 The roster

Each team's runtime account state lives in `ctl/<team>/roster.json`, written only by the
`teamchat-ctl` child under the team lock, with the atomic writer of `internal/teamchat/state` (temp
file beside the target, `O_EXCL|O_NOFOLLOW`, `fchmod`, fsync, rename, directory fsync; S15). It is
decoded strictly (unknown or duplicate keys refused, sizes capped, exit 78 naming the file).

```json
{
  "v": 1,
  "team": "<team>",
  "seq": 41,
  "rev": 12,
  "counters": {"<repo>+<site>.<type>": 3},
  "members": {
    "<handle>": {"state": "active", "kind": "podman", "human_text": true, "since_ms": 0, "cred_seq": 39, "cred_ms": 0, "needs": null}
  },
  "humans": {"<name>": {"state": "active", "since_ms": 0, "revoked_seq": 17, "needs": null}},
  "coordinator": "<handle>",
  "coordinator_wanted": null,
  "pending": null
}
```

- `seq` is raised by one on every write of the file and never goes down, a restore included
  (3.12.2). It is the clock the host compares by: a wall-clock time can step backwards (an NTP
  correction after a suspend), a counter cannot.
- `cred_seq` is the `seq` of the write that issued the member's current credential (its password
  for a pool seat, its token for a guest seat, 3.6.3), and `cred_ms` the wall time of it, shown on
  the board only. `revoked_seq` is the `seq` of the human's last password set or reset with device
  logout (`human password`, `human logout`, account creation). A restore decides by these (3.12.2).
- `needs` of a member is `null` or `"reissue"`: its credential is not one the homeserver will
  accept (a roll-forward that revoked without printing a bundle, 3.6.3, or a restore, 3.12.2). The
  board names `member reissue` for it; `reissue` clears it. A human's `needs` is `null` or
  `"password"` (set by a restore; `human password` clears it).
- `state` of a member: `active`, `parked`, `retired`, `lost` (its account is gone after a restore,
  3.12.2), or the intent states below. A `retired` member stays in the roster for ever, so its
  handle can never be issued again without asking the homeserver (C18).
- `counters` never go down, even when an account creation fails (R9).
- `coordinator` is the role last applied; the truth is the declaration (3.8, DC6).
  `coordinator_wanted` is a declared coordinator whose handle is not yet in the roster, or is
  `lost` (3.6.4): a recorded pending promotion, shown on the board as a problem with its fix.
- `pending` is the one operation in progress, if any:
  `{"op": "member.add", "handle": "<h>", "step": "account", "since_ms": 0}`. It is written before
  the first homeserver call of an operation and cleared after the last.
- `rev` is the `rev` of the last `teamchat.roster` state event the child wrote (below).

**A's roster event is derived from this file, never edited on its own.** The `teamchat.roster`
state event in the main room (A §5.1) is `proto.Roster(roster.json, declaration, rooms)`: A's
function, given C's file, the team's declaration (its humans, coordinator, trust and limits) and
the room IDs of `rooms.json`. The child validates the result with A's own decoder before it writes
it, so an invalid roster is refused before it reaches the room (seam row 17). It is written by the
rooms step of bootstrap (3.6.2 step 4), which runs at the end of every apply and of every member,
role and human verb (3.6.3-3.6.5), so the event follows each change at once rather than at the next
play. The event is compared without its `rev`; when anything else differs, it is written with
`rev` one above the larger of the event's current `rev` and the file's, and the file's `rev` is
updated after the server accepted it. A crash in between is harmless: the next write reads the
current event first. (A treats `rev` as diagnostic and lets a restore lower it, A §5.1; the host
never does lower it, 3.12.2.)

#### 3.6.2 Bootstrap

Run by `team apply` after the homeserver is ready. In order, each step comparing first and writing
only what differs:

1. **The admin account.** `admin.token` present: `whoami` must answer `@admin:<server_name>`.
   Absent but `admin.password` present: a previous bootstrap died after registering, so log in with
   it, store the token (link-not-replace), delete the password. Both absent: write a fresh password
   (link-not-replace), register `@admin` as server admin with the registration secret (the nonce
   and HMAC of M27), log in, store the token, delete the password (C14, H 7.02). A token that is
   lost while the account exists is refused, naming `restore` (the homeserver offers no other way
   back).
2. **One server admin.** `GET /_synapse/admin/v2/users?admins=true` must list exactly `@admin`.
3. **Humans.** Each declared human has an account (created with a random password that is not
   kept, so nobody can sign in until the owner asks for `human password`; created without the
   `admin` key, M40). A human in the roster but no longer declared is removed from every room
   first (step 4), then locked (DC9).
4. **Rooms.** Area A's layout, given as a typed value by A's package from the declaration, the
   roster and the room IDs known so far: `Desired(team, roster, rooms) []RoomSpec`, each spec
   naming a purpose, create parameters, state events (type, state key, content), the members
   wanted (join or invite) and the members that must not be there. The engine converges every
   spec:
   1. Find the room: its ID in `rooms.json`; if absent, adopt one the admin has joined whose
      admin-written back-link names this purpose (M35, S11 5.1); if none, write the intent into
      `pending`, create it with its initial state, record the ID, clear `pending`. More than one
      candidate is refused, naming them. After every create or adoption the engine asks
      `Desired` again with the new ID, so a spec that names another room's ID (the side rooms'
      back-link to main, the roster's `rooms`) is never built before that room exists.
   2. Every removal in every room first: kick each member or human that must not be there, with
      the reason A gives. This includes a removed human from every room before any power-level
      rewrite (C15).
   3. Then every state event whose canonical content differs, the roster event by the rule of
      3.6.1.
   4. Then every invite.
      The engine knows nothing about what a room is for. A owns the specs, and C owns that they are
      applied idempotently, in this order, and that a failure names the room and the step. On a
      new team the order this gives is the one A §5.2 needs: main is created with no roster, the
      side rooms are created with their back-links to main, the roster is written with every
      room's ID, and only then do invites go out.
5. **Roles.** If the declared coordinator differs from the roster's, run the role change (3.6.4).

Every change appends a `CHANGED` line, so a converged team prints nothing.

#### 3.6.3 Members

Every member verb runs as root under the team lock, does its homeserver work in the `teamchat-ctl`
child, and ends the same way: it converges the team's rooms from the changed roster (3.6.2 step 4,
which rewrites the roster event and the membership of each room, the questions room's form
included), posts the admin's notice for the change (below), refreshes the status record, and
answers one JSON document on stdout. Verbs and their state machine:

| Verb                                                                                        | From state         | Steps (each recorded in `pending` before it runs)                                                                                                  | To state  |
| ------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- | --------- |
| `member add --prefix P --seat S\|next --kind K --human-text B --credential password\|token` | (none)             | claim the handle in the roster (counter raised); create the account; issue the credential; converge the rooms (invite per A's specs); post `added` | `active`  |
| `member return --handle H --credential password\|token`                                     | `parked`, `lost`   | revoke (password reset with device logout), or for `lost` create the account; issue the credential; converge the rooms; post `added`               | `active`  |
| `member reissue --handle H --credential password\|token`                                    | `active`           | revoke; issue the credential (the lost-bundle recovery, C21)                                                                                       | `active`  |
| `member park --handle H`                                                                    | `active`           | revoke; mark; converge the rooms; post `parked`                                                                                                    | `parked`  |
| `member retire --handle H`                                                                  | `active`, `parked` | mark; converge the rooms (kick from every room); deactivate; post `removed`                                                                        | `retired` |

**The credential** (decision DC16). A seat's bundle carries one of two credentials, chosen by the
caller:

- `password`, for a pool seat (ccy-teams, 3.7): the child sets a fresh random password with device
  logout (for a new account, creates it with that password) and answers the password. No token is
  minted. Each session that takes the seat logs in with it as the seat's one device, which ends
  the previous session's token (3.7.3).
- `token`, for a guest seat (`lxc apply`, 3.9.2): the child sets a fresh random password that is
  not kept, logs in once as the seat's device and answers the token. It also keeps a copy of the
  token as `ctl/<team>/guest/<handle>.token` (0600), which the health tick posts the member's
  `disconnected` notice with (3.11.2). That adds no exposure: the control user already holds the
  team's admin token, which can do anything the member's can.

Either way the issue is a password reset with device logout, so every earlier credential of the
member is dead the moment the new one exists, and at most one credential is valid per member at
any time (C23).

- **Roll forward, never undo** (C20). If an operation dies, `pending` names it and its step. Any
  later verb on that team first completes the pending operation from its step, because every step
  is idempotent: an account that already exists is not created again, a revoke can always be
  repeated, an invite to a member already invited is skipped, a notice is posted with a
  transaction id derived from the operation (`pending`'s handle and `since_ms`), so a re-post is
  stored once (M22). The same verb asked again for the same handle (the caller retrying)
  completes it and prints the bundle, issuing after a revoke. Any other verb completes a pending
  `add`, `return` or `reissue` to `active` with its credential revoked and prints no bundle, and
  sets its `needs` to `reissue` (3.6.1), which the board shows. So a credential is printed only to
  a caller that asked for that handle, and v1's two gaps close: a failed account creation that
  rolled the member back but kept the counter, and an interrupted mint that left a valid token
  nobody held.
- `add` refuses a handle that is `active`, `parked` (naming `return`), `lost` (naming `return`) or
  `retired` (never returns). For `active` the message says the seat number is taken, perhaps just
  now by another checkout of the same repository, and to choose again; it names `reissue` only for
  a bundle this checkout has lost. ccy-teams, on that refusal for a seat it chose itself, chooses
  again from the fresh status record, at most three times (seat numbers are counted per prefix
  across checkouts, while the allocation lock is per checkout, 3.7.4).
- ccy-teams always passes an explicit `--seat S`: the pool directory it claims before calling
  sudo is named by the seat (3.7.3). `--seat next` is for `lxc apply` only, where root chooses
  under the team lock: the lowest number no active, parked or lost member of that prefix holds
  (parked and lost ones keep their number for their return).
- `--human-text` sets the member's `human_text` in the roster, which is its one home: A's verdict
  and B's invite rule read it from the roster event (A §5.1), and the bundle does not carry it
  (3.7.2). Changing it is `member set --handle H --human-text B`, which converges the rooms like
  every member verb and needs no new bundle.
- A `member add` or `return` whose handle is the team's `coordinator_wanted` promotes it at its
  end (3.6.4). If the promotion is refused (A's rule on `human_text: false`), the add still
  succeeds and prints its bundle, `coordinator_wanted` stays, and the status record carries the
  refusal as a problem with its fix (change the declaration or the member's `human_text`).
- `retire` refuses the declared coordinator: the declaration has to change first (C30).
- **Park keeps the account joined to its rooms** (its history stays attributed), except where A's
  specs remove a parked member: a parked coordinator is kicked from the questions room and the
  questions room takes its closed form (A §4.3), by the rooms step the verb ends with, so members
  see the change at once. Park keeps the member's role in the roster; parking the coordinator is
  allowed and the board says the coordinator is parked.

**What the admin's account posts**, all of it, in the main room, with A's types and templates
(A §7.4, 7.7): `added` (end of `add` and `return`), `parked` (end of `park`), `removed` (end of
`retire`), `role` (each demotion and promotion, 3.6.4), and, during a migration only, area D's
carried items and its welcome notice (D §5.6, through `migrate carry`). Nothing else. A member's
`left` and `disconnected` are always posted with the member's own credential (3.7.5).

The answer of `add`, `return` and `reissue` carries the bundle (3.7); every other answer carries only
`changed`.

#### 3.6.4 Roles

There is one coordinator at most, and it is named in the declaration (DC6). `role apply --team T`
(run by `team apply`, and directly by ccy-teams with the team's declaration on stdin after it has
written host_vars under the play lock) converges the roster to it:

1. If the old coordinator is not the new one: demote the old (A's team record and its room specs
   say what changes, for example removal from the questions room) and apply it fully.
2. Only then promote the new one and apply it.

The demotion completes before the promotion starts, across every member kind (C17). "Apply it
fully" is the rooms step of 3.6.2 run for the team from the changed roster: the roster event,
the questions room's membership and its open or closed form (open while an active coordinator is
joined, closed otherwise, A §4.3), and the re-invite of a coordinator that left the questions
room. Each demotion and each promotion then posts the admin's `role` notice (A §7.4). A
coordinator whose roster entry says `human_text: false` is refused where A's rules say so, at the
place the role is assigned (S11 4.5).

Bring-up order. A declared coordinator may not exist yet: an LXC member of a new team is created
by `lxc apply --phase present`, after `team apply` (3.5.1), and a ccy member's handle exists only
from its first seat take (3.7). So the declaration can name a coordinator before its member
exists (C30), and `role apply` handles it as follows:

- A demotion is never deferred. If the old coordinator is not the declared one, it is demoted
  now, whether or not the new one exists, so C17 holds at every instant.
- A declared coordinator that is an `active` or `parked` handle is promoted now.
- A declared coordinator that is not in the roster, or is `lost` (3.12.2), is recorded as
  `coordinator_wanted` (3.6.1),
  and the status record lists it as a problem with its fix ("coordinator `<handle>` declared but
  not yet a member: start its session, run the LXC apply, or `member return` it"). The team runs with no coordinator
  meanwhile. This is a recorded pending state the board shows, not a skip.
- The promotion is done at the end of the operation that creates that handle: `member add` or
  `member return` (3.6.3), or `lxc apply` (3.9.2 step 6), each under the team lock, demotion
  first as above. That operation clears `coordinator_wanted`.
- A declared coordinator whose handle is `retired` is refused before any change: it can never
  exist (C18).

Decision DC6 records this. Area D's migration declares coordinators the same way, with no
ordering rule of its own.

The hot path is therefore the same convergence as the play's, restricted to one team's roles, so it
cannot diverge from what the next play would apply (the v1 fragility where a live role change was
reverted by the next apply, LR 6 item 6).

#### 3.6.5 Humans

- Declared in the team's `humans`; created and removed by `team apply` (3.6.2 step 3).
- `human password --team T --name N`: a random 32-character password, set with device logout, and
  printed once in the JSON answer; never stored (R11).
- `human devices`, `human logout` (password reset with device logout, the lost-phone procedure),
  `human lock`, `human unlock`: kept from v1 because the documented procedures use them (DESIGN
  D12).
- The board's per-human device count and newest sign-in come from the status record (3.11), not
  from a call per person.

#### 3.6.6 The presence check

Every verb in 3.6 and every `backup`, `backups` and `restore` call from ccy-teams is reached through
`sudo` with the owner's password (C10). ccy-teams makes sudo ready once per change (the
`internal/sudo` path, which handles hosts where only some sudoers entries are NOPASSWD), runs the
calls, and ends with `sudo -k`, and a failing `sudo -k` stops it. Root cannot tell a password sudo
from a blanket NOPASSWD rule: an owner who gives themselves passwordless sudo makes every agent
running as them as trusted as they are. That caveat is v1's, and is stated in the user docs, not
solved here (see risk K3).

### 3.7 Seats and the bundle

#### 3.7.1 Where a seat lives

In v1 a seat (bundle, token, member state, seat lock) sits inside the git checkout, under
`.claude/ccy/pingbus/seats/`. Every session sharing the checkout can read every seat's token,
including the coordinator's (Plan 00203 D6, the Plan 00211 security note), and a sibling container
can consume another seat's inbox (Plan 00194 threat R4). Keeping the token out of git needed a
`git check-ignore` proof before every placement, and root writing into a checkout needed a chain
of fd tricks.

v2 moves seats out of the checkout (C25, decision DC5), and keeps the authoritative bundle where
no container can write it (C41, decision DC15):

| Path (under `~/.local/state/teamchat/seats`, owner 0700, never mounted whole) | Holds                                                                                                                                                                                       |
| ----------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pool/<checkout key>/checkout.json`                                           | the checkout's real path, so the pool can be listed per checkout (`<checkout key>` = first 32 hex of SHA-256 of the real path)                                                              |
| `pool/<checkout key>/<team>/<seat>/`                                          | the seat's authoritative record: `seat.json` and `password` (DC16), `released.json` after a release, and `held/` while a session holds it. Never mounted into any container                 |
| `pool/<checkout key>/<team>/<seat>/held/`                                     | the holding session's bundle as the pool keeps it: `held.json`, `token` (this session's login), `seat.json` (the session copy's text), `notice.json` once a disconnected notice is recorded |
| `sessions/<session>/<team>/`                                                  | the copy of the bundle a session holds, plus area B's member state and seat lock. Mounted read-write into that session's container                                                          |
| `control/<session>/desired.json`                                              | the session's desired membership (3.7.5). Mounted read-only into that session's container                                                                                                   |
| `released/sessions/<session>/<team>-<claim>/`, `released/pool/...`            | a released session seat directory, moved out of reach of every session and never read again except to be removed (quarantine, pruned after 7 days)                                          |

- A ccy session's container gets `sessions/<session>/` bind-mounted read-write at
  `/run/teamchat/seats` and `control/<session>/` read-only at `/run/teamchat/membership`,
  and nothing else of `~/.local/state/teamchat/seats`. A sibling session sees only its own seats.
  The launcher refuses any other mount whose real source (symlinks resolved) is an ancestor of,
  equal to, or inside the seat store root: a mount of `~/.local`, `~/.local/state`, or a directory
  `$XDG_STATE_HOME` points into would expose the whole pool. This is N4's acceptance, with a VM-lab
  test that tries each of the three.
- A host `cc` session uses its session directory directly. It runs as the owner, so it can read
  every file under `~/.local/state/teamchat/seats`, the pool included; that is risk K12, not a property this design
  gives.
- `held/held.json` (`{"v": 1, "session": "<session>", "claim": "<claim id>", "since_ms": 0}`, 0600) is
  the holding record. It is written by the owner process under the checkout's allocation lock,
  in the pool, where no container can reach it. A seat is held when its `held.json` names a
  session that is live (its seat lock is held) or restorable (its session record exists, 00203
  H5). This replaces v1's two rules (seat lock held, or named by a running session's desired
  membership; Plan 00203 H21) with one record no container can forge.
- Nothing a container wrote is ever moved back into the pool, read by the host as a bundle, or
  handed to another session (C41). The session's copy is a copy: the host never reads it back.
  Member state (area B's files) stays with the session that wrote it, for as long as that session
  is live or restorable, and goes to quarantine when it is released. The next session to take the
  seat starts with a fresh login from the pool's password, no member state, and the last release
  time (`released_ms`, 3.7.2), which bounds where area B's store-less start may begin.
- LXC members keep their seats inside the guest, in the agent user's own home (3.9), where only
  that user can read them. The host never moves an LXC seat; it only probes it (3.9.2).

#### 3.7.2 What a seat receives

The pool seat directory holds `password` and `seat.json`; a session seat directory receives
`token` and `seat.json`; everything else in a session seat directory (member state, the seat lock)
is area B's.

`password`: the seat account's password alone, printable ASCII, no newline, 0600, in the pool
only. It is never copied to a session and never mounted (DC16).

`token`: the access token of this session's login, printable ASCII, no newline, at most 4096
bytes, mode 0600 (S19). Its one other copy is in the pool's `held/`, for the release.

`seat.json`, mode 0600, written last so its presence means the bundle is complete:

```json
{
  "v": 1,
  "team": "<team>",
  "server_name": "<server_name>",
  "user_id": "@<handle>:<server_name>",
  "admin_id": "@admin:<server_name>",
  "main_room": "!<main room ID>",
  "base_url": "http://<address>:<port>",
  "plain_http_hosts": ["<address>"],
  "device_id": "TCSEAT",
  "issued_ms": 0,
  "issuer": "<teamchat-admin digest>",
  "claim": "<claim id>",
  "released_ms": 0
}
```

- `claim` and `released_ms` are written only into the copies a take makes (the session's and
  `held/seat.json`); the pool's own `seat.json` has neither. `claim` is the take's claim id.
  `released_ms` is the time of this seat's last release, from the pool's `released.json`, or
  `null` for a seat never released. Area B starts a seat that has no member store from the earlier
  of the member's own read receipt and `released_ms` (B 2.6 step 01): a receipt is set with a
  token, so a receipt alone is not a fact the host wrote, and an item after the last release is new
  by construction (security 1).
- `human_text` is not in the bundle: it is read from the roster in the main room, where the admin
  keeps it current, like every other roster fact (coherence 15).
- `device_id` is the seat's one fixed device id. Every take logs in with it (3.7.3).
- `released.json` (`{"v": 1, "claim": "<claim id>", "released_ms": 0}`, 0600, pool only) is
  written at release step 4 and replaced at each release.
- `base_url` is the team's member URL from the declaration (default: `teamchat_address` and the port).
  An `http://` URL is only ever to an IP literal listed in `plain_http_hosts` (M14).
- The main room's ID is one of A's anchors (A §6.2), so it is in the bundle as `main_room`. The
  other room IDs, the roster, limits and trust settings are read from the main room, where the
  admin keeps them current, so a change to them never re-issues a bundle (the Plan 00211
  reasoning).
- The member's handle is the localpart of `user_id`; its seat number is the handle's `<seat>`.
- v1's `README` file is dropped: it was text for a human that the member never read. `teamchat`
  explains a seat on request (area B).
- Each seat gets its own account and token; two seats never share either (C23).

#### 3.7.3 How it is placed

- The answer of `member add|return|reissue` carries `seat.json`'s content and the credential (the
  password for a pool seat, the token for a guest seat, 3.6.3) in one JSON document on root's
  stdout. The credential passes from the `teamchat-ctl` child to root to the owner's process over
  pipes only; it is never in argv, environment, a log or an error (C13).
- The owner's process (ccy-teams, with `internal/teamchat/seat`) always places a pool record into
  a new directory and then swaps it into place, so `add`, `return`, `reissue` and the recovery
  below are one path. The whole placement, from the held check through the swap, runs under the
  checkout's allocation lock (the root call is already inside it, 3.4), so no take can create
  `held/` between the check and the swap; that lock, not the directory name, is what stops two
  racing launches reaching the homeserver. Before calling sudo it claims
  `pool/<checkout key>/<team>/<seat>.new-<claim id>` with `mkdirat`, and refuses if any
  `<seat>.new-*` already exists, naming it: a leftover is a placement that died, recovered only by
  `reissue` (below), which moves it to quarantine first. A stale `held/` (its session over) is
  released, with its notice and logout, before a swap, so the old directory carries no live
  record into quarantine. After the answer it opens that directory, creates `password` and then
  `seat.json` with `openat(O_CREAT|O_EXCL|O_NOFOLLOW)`, mode 0600, `fsync`, and `fsync`s the
  directory (C24).
  Then, by directory fds: if `<seat>/` does not exist, `renameat2(<seat>.new-<claim>, <seat>, RENAME_NOREPLACE)`; if it does, `renameat2(..., RENAME_EXCHANGE)` and the old bundle, now
  under the `.new-` name, is moved to `released/pool/<checkout key>/<team>/<seat>-<claim>/` (the
  quarantine has two namespaces, `released/pool/` and `released/sessions/`, so no session id can
  collide with the pool's) and its `password` unlinked. Root never writes here.
- A refusal from root (for example a seat number another checkout just took, 3.6.3) removes the
  empty claimed directory, which only this process ever saw.
- A failure after the sudo call and before the swap leaves a `.new-` directory without a complete
  record. The next take names it and offers `reissue`, which resets whatever password was set
  and issues a new one into a fresh `.new-` directory. The credential is never silently re-minted
  (LR 6 item 8).
- A `reissue` or `return` is refused while the seat is held (`held/held.json` names a live or
  restorable session), naming the leave that releases it: the holding session's copy would be
  revoked with nothing to deliver the new one. The owner leaves the team from that session, which
  releases the seat (below), reissues, and joins again, which is a fresh take.
- For an LXC member, placement is the same code inside the guest: `teamchat guest place-bundle` run as the
  agent user, reading the same JSON on stdin, into `~/.config/teamchat/seats/<team>.new-<claim>/`
  and swapped the same way, the old bundle removed (3.9). The held-seat refusal above is for pool
  seats; `lxc apply` reissues a guest's seat only when the guest's bundle is missing or its token
  refused, so no keeper is running on it.

**Taking a seat for a session** (ccy-teams or the launcher, as the owner, under the checkout's
allocation lock):

1. The pool seat has a complete record (`password`, `seat.json`) and no `held/`, or its `held/`
   names a session that is over, which is released first (below). A `held/` with no complete
   `held.json` is a take that died before step 2 finished: it is moved to quarantine, and its
   token, if one was written, dies at this take's login (step 3). The handle's state, from the
   status record's roster, decides what follows: `active` with no `needs` goes on; `parked` or
   `lost` goes to `member return` and `needs: reissue` to `member reissue` (each a placement,
   above, and asking the owner's password); `retired` is refused and its pool directory moved to
   quarantine.
2. `mkdirat(held)` in the pool seat directory and write `held/held.json` with a fresh claim id
   (`openat(O_CREAT|O_EXCL|O_NOFOLLOW)`, 0600, fsync each and the directory).
3. Log in as the seat (`POST /login`, `m.login.password` with the pool's `password`, `device_id`
   from `seat.json`, no refresh token), over the member URL. The Matrix spec has a login with an
   existing device id replace that device's access token, so the previous holder's token dies here
   (host check H-C13 proves it on Tuwunel). A login refused with `M_FORBIDDEN` means the password
   was reset outside this pool: the take stops naming `member reissue`, and `held/` is removed.
   A homeserver that is still starting is waited for as 3.7.4 says.
4. Write `held/token`, then `held/seat.json` (the pool's `seat.json` with `claim` and
   `released_ms`, 3.7.2), `O_EXCL|O_NOFOLLOW`, 0600, fsync each and the directory. `held/` is now
   a complete bundle that only the pool can reach, used by the release (below).
5. Open `sessions/<session>/` by directory fd with `O_DIRECTORY|O_NOFOLLOW` from the fd of
   `sessions/`, which no container sees (the container sees only the inside of its own session
   directory, never the directory entry itself).
6. `mkdirat(<team>)` relative to that fd. `EEXIST` is refused, naming the path: something already
   there, whoever made it, is never written into. Open the new directory `O_DIRECTORY|O_NOFOLLOW`,
   `fstat` it (a directory, owned by the owner, mode 0700) and check it is empty.
7. Write copies of `token`, then `seat.json`, from `held/`, with
   `openat(O_CREAT|O_EXCL|O_NOFOLLOW)` 0600 relative to that fd, fsync each and the directory.

A failure at any step leaves `held/` naming a session that holds nothing yet; the session's own
start fails fast, the launcher removes the session record, and the next take finds the session
over and releases it (below), the logout included.

A container that races step 6 (swaps in a directory of its own, or a link) gains nothing: a link
fails `O_NOFOLLOW`, and a directory of its own, if empty and owner-owned, only receives the copy
the session was about to get anyway, by `openat` with no path walk. Joining a team mid-session
(hot control) is the same take into a directory the container already has mounted, so no new
mount is needed in a running container; 3.7.5 says how the session learns of it.

**Releasing a seat** (a leave, a remove, or a session that is over), as the owner. The waits and
the homeserver calls run without the allocation lock, so a launcher's take in the same checkout
never queues behind a draining keeper; the lock is taken only for steps 2 and 5.

1. Wait, bounded (60 s), for the session's seat lock to be free, or for the session to be over.
   The probe is L3, made safe against the container that owns the path: from the dirfd of
   `sessions/<session>/`, `openat2(<team>/<seat lock>, O_RDONLY|O_NONBLOCK, RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)`, then `fstat` a regular file, then
   `LOCK_SH|LOCK_NB`. A link anywhere on the path, a FIFO or a device is refused, never followed,
   so a planted link cannot point the owner's lock probe at the play lock or any other owner
   file. A wait that ends with the lock still held exits 75 naming the holder; the seat stays
   held and the board shows it as leaving. The board (in the background, 3.13) re-runs the release for every seat whose
   `held/held.json` names a live session that no longer desires it (a timed-out leave).
2. Under the allocation lock: `renameat2(sessions/<session>/<team>, released/sessions/<session>/<team>-<claim id>, RENAME_NOREPLACE)` by directory fds. The rename moves
   whatever is there, a link included, without following it. From that moment the session sees
   nothing of that seat.
3. If the session is over rather than leaving, the disconnected notice (3.7.5), with `held/`'s
   bundle, outside the lock; skipped when `held/notice.json` already records it for this claim.
   Its outcome decides the next step:
   - posted, or a goodbye found: go on;
   - not due, or not possible for good: the handle is not `active` in the status record's roster
     (parked, retired, lost, or with `needs: reissue`), the homeserver answers `M_UNKNOWN_TOKEN`
     or `M_FORBIDDEN`, the team is declared absent, or its homeserver is not installed. The
     reason is recorded as `held/notice.json` (`{"v": 1, "claim": "<claim id>", "posted": false, "why": "<reason>"}`), the board shows "notice not posted: <why>", and the
     release goes on;
   - a transient failure (unreachable, a 5xx, a 429 after M6's retries): the release stops here,
     `held/` stays, the board shows the seat as leaving, and the next release retries from
     this step (step 2 finds the session seat already gone and moves on).
4. `POST /logout` with `held/token`, outside the lock. `M_UNKNOWN_TOKEN` counts as done (the
   token is already dead); a transient failure stops the release here as in step 3. From this
   moment the token the session held, and every copy of it, is dead.
5. Under the allocation lock: write `released.json` (this claim, now), then move `held/` to
   `released/pool/<checkout key>/<team>/<seat>-<claim id>/` and unlink its `token`. The pool's
   `password` and `seat.json` are unchanged and authoritative.

A session is over when its session record is gone (00203 H5): the launcher removes the record
when the session ends by the owner's choice (a normal exit or `ccy-teams` stop), and then runs
the release. A container that dies with its record still in place (a crash, a reboot, an age
restart) is restorable: its seats stay held, and its member state stays in its session directory
for the restore, which is the same session.

The quarantine is removed by an fd-relative walk that never follows a link (Go's
`os.RemoveAll` on Linux opens with `O_NOFOLLOW`), by ccy-teams after 7 days. Its content is never
read: it exists so that a hostile or broken session's last state can be looked at by the owner,
not reused.

Decision DC16 holds what a pool seat keeps across sessions. Three designs were weighed:

- (a) the pool keeps a token, reused by each session that takes the seat. A session start needs
  no password, but a released hostile session keeps a valid copy: it can go on reading the seat's
  traffic, and set the member's read receipt so the next holder's store-less start silently skips
  what arrived in between (security 1). Rejected.
- (b) a reissue on every take: no released session keeps a valid token, but every session start
  asks the owner's password. Rejected for the cost to the owner.
- (c') chosen: the pool keeps a password, and each take logs in with the seat's one fixed device
  id; each release logs that token out. A session start needs no owner password, and a released
  session's token is dead at release. A hostile holder cannot keep the seat: changing the password
  or adding a device needs the password (UIA), which never leaves the pool. The
  `released_ms` bound (3.7.2) holds whatever the receipt says.

If H-C13 shows that Tuwunel does not replace a device's token on a new login with the same device
id, the fallback is a device id per claim (`TC<claim>`) and, at release, `DELETE /devices/<id>`
with UIA by the pool's password, in place of the logout; and if Tuwunel supports neither, (a) with
the `released_ms` bound, K13 open.

#### 3.7.4 Seat choice and handles

The handle's grammar, its parts and the set of member types are area A's ("identities and
handles" in the frame); C takes A's parser and formatter as a package and restates none of it.
What C owns is seat numbering and allocation, unchanged in substance from v1 (R9, R14; digest 1,
section 2.10), now computed in-process by ccy-teams from the pool's `held/held.json` records, the
session records and the status record's roster:

- The prefix a seat number is counted under names the handle's repo, site and type
  (`<repo>+<site>.<type>`, the form `member add --prefix` takes); the seat number is given
  separately (`--seat`) or counted. A §2.3 owns the grammar, and A's formatter joins the two.
- The offered seat is the lowest number that is free in this checkout and held by no active member
  of the same prefix; a parked handle's number is offered for its return first.
- `seat take` waits, bounded, for a homeserver that is still starting (up to 180 s, after a boot),
  and tells "built but stopped" from "not a team" with different messages naming `ccy-teams apply`
  or `create` (R14).

#### 3.7.5 Hot control and the host's notices

A running session's teams change through a record the host writes and the session can only read,
as in v1 (R31, R32, 00203 H5/H8; C42). Two designs were weighed: (a) the presence of a seat
directory in the session directory is the whole signal; (b) a separate desired-membership file,
host-written, mounted read-only. (a) has no way to tell a session to leave: a release waits for
the seat lock to come free, and nothing would ask the keeper to free it. (b) is chosen.

`control/<session>/desired.json`, 0600, written atomically by ccy-teams as the owner:

```json
{"v": 1, "session": "<session>", "generation": 3, "written_ms": 0, "seats": [{"team": "<team>", "seat": "2"}], "farewell": [{"team": "<team>", "seat": "1", "reason": "released"}], "inbound": "accept"}
```

- `inbound` is `accept` or `refuse`, written for every launch (B's BD10): a session launched on
  no team refuses inbound messages until its first join relaunches it.
- Order: ccy-teams writes the session record first (00203 H5), then the seat (3.7.3 take), then
  `desired.json` with `generation` raised. A reboot restore or an age restart replays the record.
- A team is active in the session when it has a seat directory in the session directory and is
  in `desired.json`'s `seats` (R31). Either alone is not active: a seat placed but not yet
  desired waits, and a desired seat with no directory is a fault the session reports.
- **Join:** take the seat (3.7.3), then write `desired.json` with it. A session started on no team
  is relaunched once on its first join (R32, `relaunch.json` beside `desired.json`, kept from v1).
- **Leave:** write `desired.json` without the seat and with a `farewell` entry for it. Area B's
  worker sees the change and posts the goodbye as that member (A's `left`, reason `released`: the
  member stays in the team, A §7.4), and the session's processes for that seat end, which frees
  the seat lock (B 2.10; the keeper makes no network call). ccy-teams then releases the seat
  (3.7.3 release). The host never posts a goodbye or a `left`: the member says it. There is no
  admin-posted farewell and no "farewell owed" record.
- **Disconnected (R29, C43):** when the host releases a seat whose session ended (not a leave),
  it posts A's `disconnected` notice as the subject itself (A §7.4), by running area B's
  `teamchat seat disconnected --bundle <pool seat dir>/held --claim <claim id>` as the owner:
  `held/`'s bundle, never the session's copy, and the claim id from `held/held.json`. The admin's
  account never posts it. Whether a goodbye was already said is
  judged from the room on the server, from the member's own events since the claim, never from a
  file the container wrote. The notice is sent with a Matrix transaction id derived from the
  claim id (`tc-disc-<claim id>`), so a second post for the same claim cannot land twice
  (transaction ids are scoped to one token, which is one claim's login; across claims the room
  check covers it). Only a transient failure
  holds the seat for a retry; a notice that is not due or can never be posted (the handle no
  longer active, the token refused, the team gone) is recorded and the release goes on, so a seat
  is never held for ever by a notice (3.7.3 release step 3).
- When a session is over (its record removed), the launcher runs the release for its seats, and
  the next take, or the board in the background after it has drawn, that finds a seat held by a
  session that is over does the same.
- **A crashed session (ops 2):** a container that dies with its record still in place (a crash,
  an OOM kill, a podman failure) is restorable and keeps its seats, but its members are announced
  at once, as v1's launcher did. When `podman` returns and the session's record remains, the
  launcher, and the board in the background for a launcher that died too, runs the same
  `teamchat seat disconnected --bundle <pool seat dir>/held --claim <claim id>` for each seat
  whose seat lock is free (the L3 probe, 3.7.3 release step 1), records `held/notice.json`, and
  keeps the seat held. The same transaction id means a later release for the same claim cannot
  post it again, and step 3 of that release skips it. A restored session posts A's `joined` back
  (area B, with A's content; section 7). A reboot is the same case: the board posts on its first
  open after it.
- **An LXC member** is announced by the health tick, which holds a copy of the guest's token
  (3.6.3, 3.11.2).

B owns the content of the goodbye and the disconnected message (A's types), the keeper side of
leave, and the decision that a member's own events show a goodbye; section 7 lists these as
assumptions on B.

### 3.8 The declarations in host_vars

host_vars stays the IaC store (C28, DESIGN D6). v2 has its own keys, so v1's play, v1's
`declare.py` and the stage-1 line reader on `feat/bus-v2-s1-declare` (which reads v1's keys while
v1 lives) never see them, and v2 never writes `agent_bus_*`. The one v1 value v2 reads is each
`agent_bus_teams` entry's `port`, which `declare check` refuses to reuse (3.2.5, D §3.8), and
its `team`: a `teamchat_teams` entry whose id is also a v1 team is accepted only while that team's
migration set is at `preflighted` or later (D §5.1), and a v2 team `x` is refused while a v1 team
`teamchat-x` is declared (D §3.7). Only `src-go/internal/teamchat/migrate/v1` names v1's keys (D
§4.1), so this read lives there.

```yaml
teamchat_address: <bus_ip>                  # optional; the teamchat0 dummy address

teamchat_teams:
  - team: <team>                        # the team id: ^[a-z][a-z0-9-]{0,21}$ (A §2.2), unique, never changes
    name: <display name>                # optional; default: the id; display only
    state: present                      # present | absent
    purge: false                        # only with absent
    port: <port>                        # declared per team; there is no range (D §3.8)
    server_name: <server_name>          # optional; default <team>.teamchat.internal; never changes
    description: "<what the team is for>"   # 1-120 characters
    listen: ["<bus_ip>"]
    allow_from: ["<cidr>"]
    url: http://<bus_ip>:<port>         # optional; the members' base URL; default from teamchat_address
    humans: ["<name>"]                  # 1-16
    coordinator: <handle>               # optional; at most one per team
    trust:                              # area A owns the meaning AND the shape; this shape is an example
      org: <owner>
      exclude: ["<repo>"]
      repos:
        - repo: <owner>/<repo>
          branches: ["<branch>"]
      path_prefixes: ["docs/"]
      forge_api: https://api.github.com

teamchat_lxc_members:
  - team: <team>                        # a team declared present
    container: <container>
    user: <agent-user>
    checkout: /home/<agent-user>/<repo> # read from the host side (3.9)
    site: <site>                        # optional; the handle's site (A §2.3)
    seat: <n>                           # optional
    human_text: true                    # optional, default true
    service: false                      # optional: a supervised session in the guest
    state: present                      # present | absent

teamchat_element_teams:
  - team: <team>
    base_url: http://<bus_ip>:<port>    # optional; default the team's url
```

What changed from v1's keys, and why:

- `team` holds the team id (A §2.2: at most 22 characters, so the homeserver's dynamic user
  `teamchat-<team>` fits systemd's limit, 3.2.3), chosen at creation and never changed. It is the
  entry's identity: `declare check` matches entries by `team`, so a changed `team` is a removal
  and a new team, never a rename. `name` is the display name (A §2.2 grammar: 1-64 Unicode scalar
  values, no control, format or newline characters; default: the id). It is display only, never
  an identifier and never compared, and changing it is a rename (C44, 3.5.2). A migrated v1 team
  keeps its v1 name as its id when the name fits the grammar, and otherwise takes the short id
  declared with its move; its `name` starts as the v1 name (D 3.5, 5.10).
- `coordinator` is a team-level key, the one place the role is declared (C30, DC6). LXC entries
  lose `role`. A ccy member's role used to live only in the registry, set by `set-role`, and an LXC
  member's role in its entry: two truths that a live change and the next apply could fight over.
- LXC entries lose `repo`, which existed only to park a destroyed container's handle. The LXC
  apply record keeps the (container, user, team) to handle map on the host (3.9), so the handle is
  known without it.
- `removed_humans` is gone: removing a name from `humans` kicks and locks that human, and adding
  it back unlocks (DC9).
- The trust settings are grouped under `trust`, so the keys A owns are visibly A's. The keys
  inside `trust` above are v1's, shown as an example: their shape is area A's to decide (an
  assumption on A, section 7), and C decodes `trust` with A's Go type.
- `url` makes the members' base URL explicit when a team is reached through something other than
  the bus address.

Reading and writing:

- The play gets the values as Ansible parsed them and hands them to `teamchat-admin declare check` as
  JSON on stdin (3.5.1), so what is applied is judged by the Go validator, not by Jinja.
- ccy-teams reads the same file with a Go YAML decode (`go.yaml.in/yaml/v3`, added to the
  production module by decision DC17). host_vars is one file, `host_vars/<host>.yml`; a
  `host_vars/<host>/` directory is refused by name, since ccy-teams could not then know which
  file to edit. The file is decoded to a `yaml.Node` tree first, and the strict rules apply only
  to the subtrees under the v2 top-level keys (`teamchat_address`, `teamchat_teams`, `teamchat_lxc_members`,
  `teamchat_element_teams`). Every other top-level key is ignored whatever it holds: `!vault` values,
  `yes`/`no`, anchors and v1's keys are not v2's business. Within the v2 subtrees: known fields
  only, duplicate keys refused, and, so that the two parsers cannot read one file two ways, only
  strings, integers, `true`/`false`, lists and mappings. The Go reader refuses there what YAML 1.1
  (Ansible's) and YAML 1.2 read differently in a typed field: `yes`/`no`/`on`/`off`, leading
  zeros, `0x` and `0o`, sexagesimal numbers, and any tag, anchor or alias. A `!vault` value
  inside a v2 subtree is refused by name: no v2 key holds a secret, so a vaulted value there is a
  mistake.
- ccy-teams writes v2 keys by splicing bytes, not by re-encoding the file: a `yaml.v3` round trip
  would change indentation, quoting and comment placement in keys v2 does not own, `!vault`
  blocks included. The node tree gives the byte range of each v2 top-level key (its line span,
  starting at the first line of the key's own head comment and ending at the last line of its
  value node, so a comment block heading the next, non-v2 key stays outside it); the writer
  renders only that key's new block and
  replaces only that range, or appends the block at the end for a key not yet present. Every
  byte outside the v2 ranges is identical before and after, and a test asserts it over fixture
  files with vault blocks, anchors and comments. The write is under the play lock, after an
  `O_EXCL` backup copy of the file (as v1's `declare.py` does), and is read back to check it
  gives the intended value.

### 3.9 LXC members

#### 3.9.1 Shape

`teamchat-admin lxc apply --phase absent|present [--only-teams T,...]`, as root, run by the play (3.5.1)
with the declared members as JSON on stdin. ccy-teams' `lxc-add` edits host_vars under the play
lock and runs the play scoped to the team, as v1 does. The play stays the one route (C31).

The rules the apply keeps from v1 (R33, R48, LR 1, LR 6): the checkout is read from the host side;
everything checkable is checked for every running container before anything changes; demotions
before promotions; a stopped container is ordinary and a missing one is an error; a running and
correct member is left alone (no new token on every run).

What changes: no container program ever runs as root, and no container needs Python. There are
two write mechanisms, one per kind of path, and no path is written by both:

- **root writes the guest's system paths** (the kit under `/usr/local/lib/teamchat/kit/`, the
  session unit and its wants link), from the host side, through the container's root directory
  fd. Nothing in the guest runs for it.
- **the agent user writes its own home** (bundles, the desired membership, a team's removal) by
  `teamchat guest <op>`, run as that user through `lxc-attach -u`. Root never writes under a
  user's home (C24), in a guest as on the host.

| v1                                                                                            | v2                                                                                                                                                                                                                         |
| --------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `python3 -I -c <script>` run inside the guest as root (kit sync, user lookup, env file, unit) | no guest program run as root; root writes only root-owned system paths of the guest, from the host side, with `openat2(RESOLVE_IN_ROOT\|RESOLVE_NO_SYMLINKS\|RESOLVE_NO_MAGICLINKS\|RESOLVE_NO_XDEV)` and the checks below |
| `python3 -I -c` as the agent user (bundle, `pingbus config check`)                            | `teamchat guest <op>` as the agent user (B 2.13), through `lxc-attach -u <uid> -g <gid> --clear-env` with `HOME`, `USER`, `LOGNAME` and `PATH` set, reading typed JSON on stdin                                            |
| the env file naming the user's teams, written as root                                         | the desired membership `~/.config/teamchat/membership/desired.json`, written by `guest membership` as the agent user (3.9.2 step 8)                                                                                        |
| `getent passwd` inside the guest                                                              | the guest's `/etc/passwd` read from the host side, size-capped                                                                                                                                                             |
| the source-address check by a UDP connect inside the guest                                    | the guest's addresses from `lxc-info -iH` and its link from the container config, checked against `allow_from` on the host                                                                                                 |
| a kit copied file by file into one directory, replaced in place                               | the kit written as `/usr/local/lib/teamchat/kit/<digest>/` from the host side and a `current` link swapped atomically; a digest kept while any live session names it (C33, 3.9.2 step 4)                                   |
| Python 3.11 or later required in every guest                                                  | one static `teamchat`; the guest needs no interpreter and no library                                                                                                                                                       |

**Why the desired membership is the agent's own file.** In a ccy session the control directory is
mounted read-only, so the member cannot write its own desired membership. In a guest, a root-owned
directory inside the agent's home gives no such guarantee: the agent owns the parent and can
rename the directory away and make its own, and root writing there would be root writing under a
user's path. It is also not a boundary worth having. The desired file asks an obedient member which
of its seats to use; a hostile member holds the tokens of those seats and can use them whatever the
file says. What ends a member's access is its token (`member park` or `retire` revokes it), never
the file. So the agent user writes its own desired file, and the host keeps the authoritative copy
(3.9.3).

**Rules for every root write into a guest** (S8 of the security review):

- the path is resolved from the root fd with `RESOLVE_IN_ROOT|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS|RESOLVE_NO_XDEV`, so a link
  is refused and the write cannot leave the guest's root filesystem: a guest whose `/usr/local`
  is a bind mount of a host directory is refused by name, never written through;
- every directory on the path is opened `O_DIRECTORY|O_NOFOLLOW` and `fstat`ed: it must be owned by
  root and not writable by group or other, else the write is refused by name;
- files are created `O_CREAT|O_EXCL|O_NOFOLLOW` under a temporary name with an explicit mode,
  `fsync`ed, and renamed into place by `renameat2` relative to the directory fd.

**The agent user** must have a uid and a gid other than 0: an entry whose user resolves to uid 0
is refused before any container is touched, since `lxc-attach -u 0` is the exec-as-root this
design rules out. In a privileged container the agent's uid is the same kernel uid as any host
user with that number, so a guest agent user and a host user that share a uid can reach each
other's files; that is risk K15, and the apply refuses an agent uid equal to the desktop user's.

Why `lxc-attach -u` is safe where `lxc-attach` as root was not: an exec as uid 0 into a guest is
how a hostile guest binary can reach the host (the class of CVE-2019-5736, where a container binary
overwrote the host runtime through `/proc/self/exe`). As the agent uid it can do only what that
user can already do inside its own guest, and the program run is the one root wrote from the host
side a moment before.

How each `lxc-attach` is made: `lxc-attach -n <container> -u <uid> -g <gid> --clear-env --set-var HOME=<home> --set-var USER=<user> --set-var LOGNAME=<user> --set-var PATH=/usr/local/bin:/usr/bin:/bin -- /usr/local/lib/teamchat/kit/<digest>/teamchat guest <op>`, with uid, gid and
home from the guest's `/etc/passwd` as read on the host side, and the kit path resolved once by
the host (never `current`, so a swap mid-phase cannot change what runs). `--clear-env` alone would
leave `HOME` unset, and `guest place-bundle` and `guest check` need it to find `~/.config/...` and the
forge CLI's login. Whether `-u`/`-g` drop the attaching process's supplementary groups is checked
by host check H-C10; `teamchat guest` also checks `getgroups()` at start and refuses to run with
any group but the agent user's own, and refuses to run as uid 0.

#### 3.9.2 Order of one present phase

01. Validate every entry (types, `(team, container, user)` unique, team declared present and its
    homeserver active) before touching any container.
02. For each container: `lxc-info -sH`. Missing fails the apply. Not `RUNNING` is held back (3.9.3).
03. For each running container, read from the host side and change nothing yet:
    - the container config (a host file, root-owned): network link, `lxc.start.auto`; an
      unprivileged (`lxc.idmap`) container is refused by name, as v1 does;
    - for an entry with `service: true`, v1's pre-checks, all host-side: the config has
      `lxc.start.auto = 1` (so the supervised session comes back with the host), the guest's init
      (`/proc/<init pid>/exe` resolved in the guest) is systemd, and `tmux` is in the guest's
      `PATH` directories. A miss fails the phase naming the setting and the play that owns the
      container's config (section 7, D7); the apply never edits the container config;
    - every guest file below is read through one path: `/proc/<init pid>/root` of the running
      container, opened once as a directory fd, with `openat2(RESOLVE_IN_ROOT|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)`
      relative to it. That is the container's own view, its bind mounts included, whatever the
      store type, so the design neither reads the rootfs directly nor re-implements LXC's mounts.
      Every open adds `O_NONBLOCK` and is followed by `fstat`: anything but a regular file (a FIFO
      would stall a blocking open) is refused by name. A stopped container is not read at all
      (3.9.3);
    - the agent user from `/etc/passwd` (uid, gid, home), at most 1 MiB;
    - the checkout: `.git/config` (`origin`; `insteadOf` rewrites refused), and
      `.claude/ccy/ccy.env.local` (owned by the agent user's uid, else refused), each at most
      64 KiB;
    - derive the handle (3.7.4); the seat is the entry's, else the one the LXC record holds, else the
      lowest free;
    - check the source address: one of the guest's addresses inside the team's `allow_from` (which
      includes the live bridge subnet), on a link that is a host bridge;
    - for a team whose `origin` is `migration` (D §3.9), whether the guest still runs this member on
      v1: the guest's v1 session unit enabled, or v1's env file naming the team, read through area
      D's adapter (`migrate/v1`, D §5.5) from the same root fd. Either holds the member back as
      `DEFERRED (v1 not yet absent)` (3.9.3), so a container that was stopped during the move and
      started later never runs a v1 and a v2 session side by side; ccy-teams' heal for it runs v1's
      play, then v2's.
      Any refusal here fails the phase before any container is changed.
04. Kit: for each running container that lacks the installed kit digest, write
    `/usr/local/lib/teamchat/kit/<digest>/` (`bin/teamchat`, `bin/teamchat-claude`, `plugin/`,
    `settings.json`) from the host's own kit directory, from the host side by 3.9.1's rules
    (root-owned, 0755 directories, 0755 binaries, 0644 files, modes explicit so no inherited umask
    can matter, LR 6 item 2), under a temporary directory name renamed into place once complete;
    then swap `current` and the `/usr/local/bin/{teamchat,teamchat-claude}` links. The bytes
    written are the host's, whose digest the build recorded, so there is nothing to check inside
    the guest and no guest program runs. An old digest is deleted only when it is not `current`
    and no live session names it: the host reads each `session.pid` (B 2.4.1) under the guest's
    runtime parents (`/run/user/<uid>/teamchat/run/*/` and the session unit's runtime directory,
    3.9.4) through the root fd, decoded strictly and size-capped, and checks the recorded pid's
    start time in the guest's own `/proc`. The records are the guest's writing, which is safe in
    this one direction: a forged or undecodable record can only make the host keep a digest
    (costing disk), never delete one a session uses. The same rule, over the owner's runtime
    parent, keeps the host's own `/usr/local/lib/teamchat/kit` digests (C33). A session resolves
    its kit once at launch (area B), so a digest it uses outlives every swap.
05. Guest check: `teamchat guest check --user <user> --team <team>` as the agent user in each
    running container (what the member needs to work, for example the forge CLI signed in; area B
    defines the list, B 2.13). Any failure fails the phase before any roster change.
06. Roster changes, under each team's lock in turn: every demotion of every team first, then every
    join (`member add`, `return` or `reissue` with `--credential token`, as the state requires,
    3.6.3), then every promotion, including each team's `coordinator_wanted` that one of these
    joins has just created (3.6.4).
07. Place each new bundle: `teamchat guest place-bundle --user <user>` as the agent user, bundle
    (with its team) on stdin; it writes `~/.config/teamchat/seats/<team>.new-<claim>/` (0700) with
    `token` and `seat.json` (0600) and swaps it in as `~/.config/teamchat/seats/<team>/`, by the
    same code as 3.7.3, and the member's own config (area B).
08. Membership: for each guest user whose desired membership differs from the host's record of it
    (3.9.3), write the record first, then run `teamchat guest membership --user <user>` as the agent
    user with the whole `desired.json` on stdin (3.7.5's schema): `session` is `lxc-<user>`,
    `generation` one above the record's last, `seats` every team the user is placed in, `farewell`
    empty, `inbound` `accept`. The op writes `~/.config/teamchat/membership/desired.json` (0700
    directory, 0600 file) atomically, and the guest's keeper reconciles it at once (B 2.10). A
    converged apply writes nothing. A user's seats span teams, so this step runs once per user
    after every team's placement, under the play lock that every apply runs under (3.5.1).
09. Supervised session (`service: true`): see 3.9.4.
10. Write the LXC record (3.9.3), only after the phase succeeded.

**The absent phase** runs first in the play, before any team is removed (3.5.1 step 10), so a
leaving member is parked on a live server. A member leaves when its entry says `state: absent`,
or when the LXC record names a (container, user, team) that no entry declares any more. For each
leaving member of a running container, under its team's lock, which the health tick then cannot
take (3.11.2):

1. Rewrite the user's desired membership without the seat and with a `farewell` entry for it,
   reason `parked` (record first, then `guest membership`, as step 8). The keeper removes the team
   and posts nothing for it, because the admin's `parked` notice announces it (B 2.10).
2. Wait for the seat to be free: the L3 probe below, every second, at most 60 s (B's removal bound
   of 10 s and its leave-notice bound of 30 s, with margin). A seat still held after that fails the
   phase, naming the container, the user and the holder's run id (read from the seat lock's
   content, B 2.5, and printed only if it passes its grammar).
3. `teamchat guest remove --user <user> --team <team>` as the agent user (B 2.13); it takes the
   seat lock itself, so it cannot race a keeper that took the seat again.
4. `member park` (3.6.3), which revokes the token and posts the admin's `parked` notice.
5. If the user now holds no team, its supervised session unit is stopped, disabled and removed
   (3.9.4).

For a stopped container, the handle the record holds is parked from the host side at once
(revoking its token, so a guest started later cannot use it) and the guest's files are left: the
next phase that finds the container running runs steps 1-3 for them, with the token already dead.

Whether an LXC seat is held (C27) is probed from the host through the same `/proc/<init pid>/root`
fd: the member's seat lock under the agent user's seat directory, by the L3 probe (open
`O_RDONLY|O_NOFOLLOW|O_NONBLOCK` with `RESOLVE_IN_ROOT`, `fstat` a regular file,
`LOCK_SH|LOCK_NB`). The host never enters the container for it. A stopped container's seat is not
held. Opening another process's `/proc/<pid>/root` needs ptrace read access to it, which root has
in the play and which the health unit is given for this one purpose (3.11.1).

#### 3.9.3 Stopped containers and the record

A container that exists but is not running is never entered and never fails the apply (C32):

- present, with a handle the record knows: `SKIPPED` (its seat is still claimed, so no one else gets
  the number);
- present, with no known handle: `DEFERRED` (the team's seat choice then refuses to hand out a
  number that could be this member's, as v1's `unheld_ok` did);
- absent: parked from the host side, `DEFERRED` for the guest's files.

Output: `CHANGED|SKIPPED|DEFERRED<TAB><team><TAB><user>@<container><TAB><why>` lines and one
`LXC-SUMMARY<TAB><text>`; the play shows the held-back lines in a task of their own, so they show
without `-v`.

The record `/var/lib/teamchat-status/lxc/<team>.json`, 0640 root:`<owner group>`, written atomically and only on
change:

```json
{"v": 1, "team": "<team>", "members": [{"container": "<c>", "user": "<u>", "handle": "<h>", "status": "joined", "since_ms": 0, "why": ""}]}
```

`status` is `joined`, `stopped`, `deferred` or `left`; `since_ms` stays while status and handle stay.
The record is the host's map from (container, user, team) to handle, so the guest's bundle is a copy
of what the host knows, not the only place it is known (LR 7 item 4). A record ccy-teams cannot read
fails `apply` and `lxc-add` and shows on the team row.

The host's copy of each guest user's desired membership is
`/var/lib/teamchat-admin/lxc/<container>/<user>.json` (root 0600):
`{"v": 1, "generation": 4, "seats": [{"team": "<team>", "seat": "2"}], "farewell": []}`. It is
written before the guest's file (3.9.2 step 8 and the absent phase), so `generation` never goes
down, a converged apply compares against it and writes nothing, and a guest file the agent changed
is put back at the next apply. It is the one place the generation is kept.

A deferred member heals only when the play runs again. A container-start hook that applies the last
rendered declaration would heal it on its own; it is held as decision DC10 and not built now.

#### 3.9.4 The supervised session in a guest

`service: true` gives the agent user a supervised session: B's launcher `teamchat-claude` under
tmux, restarted on exit (area B owns what runs). v1 wrote a system unit with a guest script as root
and ran the guest's `systemctl` as root. v2 writes the unit file and its
`multi-user.target.wants` link into the guest from the host side (3.9.1's rules), and asks the
guest's own systemd to reload and start it over the guest's private manager socket, reached as
`/proc/<init pid>/root/run/systemd/private` from the host: a host process speaking peer-to-peer
D-Bus (DC17) to a process that already runs as the guest's root, with no guest program run. Host
check H-C9 proves this on a real privileged container. If it cannot be made to work, the fallback
is decision DC13 (start at the next container start only); running the guest's `systemctl` as root
is not an option. A unit whose text is unchanged is not restarted; a user in no team has the unit
stopped, disabled and removed. The unit text is C's alone (area B places no unit):

```ini
[Unit]
Description=teamchat session for <user>
After=network-online.target
Wants=network-online.target

[Service]
Type=forking
User=<user>
Group=<group>
WorkingDirectory=<checkout>
RuntimeDirectory=teamchat-session-<user>
RuntimeDirectoryMode=0700
Environment=XDG_RUNTIME_DIR=/run/teamchat-session-<user> HOME=<home>
ExecStart=/usr/bin/tmux -L teamchat new-session -d -s teamchat /usr/local/bin/teamchat-claude
KillMode=mixed
TimeoutStopSec=30
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

- `RuntimeDirectory=` gives the session a runtime directory without a login session, and is one of
  the runtime parents 3.9.2 step 4 reads.
- `KillMode=mixed` sends the stop's SIGTERM to the tmux server only, so the pane's hangup ends
  Claude Code in the ordinary way and the keeper and worker are not signalled by systemd while the
  goodbye is posted; the SIGKILL of what remains comes only after the main process has gone.
  This relies on area B's `run` waiting for the keeper before it exits (section 7, B item 10), so
  the pane, and with it the tmux server, outlives the goodbye. `TimeoutStopSec=30` is above B's
  5 s end bound with margin.
- `<checkout>`, `<user>`, `<group>` and `<home>` are the host-side reads of 3.9.2 step 3, each
  checked against its grammar before it is rendered into the unit.

### 3.10 Element profiles

`teamchat-admin element check|apply`, run as the desktop user by the optional Element play (its own file,
`become: false`), with the profiles as JSON on stdin (R50). What it writes is v1's, under v2's own
names so no v1 profile is touched: one Element Desktop profile per team (`config.json` 0600 in a
0700 directory, every outbound key overridden explicitly because Element shallow-merges over its
bundled config, the spell checker seeded off once because Chromium fetches dictionaries, the keyring
grant written before the first start) and one launcher per team (0644), whose `Name=` is the
team's display name, so a rename (C44) rewrites only that line. Each team opens in its own
window because each team is its own server. Writes are temporary file plus rename, only on change.

The refusal stays (R37): an Element profile holds the human's session token where every process of
that user can read it, so `apply` refuses a user who runs `host`-kind members (a host `cc` session,
which runs as the owner with no container between it and the profile), unless the owner has recorded
consent in `~/.config/teamchat/element-consent` (0600 in a 0700 directory, v1's `element-accepted` rule, Plan 00161 D26).
ccy sessions do not count: a container sees only what is mounted into it, and no profile is.

### 3.11 Health and the status record

#### 3.11.1 One timer for every team

`teamchat-health.timer` (`OnActiveSec=30s`, `OnUnitActiveSec=60s`, `AccuracySec=5s`) starts
`teamchat-health.service`: `Type=oneshot`, `ExecStart=/usr/local/sbin/teamchat-admin health --all`,
`TimeoutStartSec=120`, `NoNewPrivileges`, `ProtectSystem=strict`, `ReadWritePaths=/var/lib/teamchat-status /run/teamchat-admin`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, `ProtectKernel*`, `ProtectControlGroups`,
`CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_CHOWN CAP_SETUID CAP_SETGID` (to read the root-only
records, to set the record's group, and to start the `teamchat-ctl` child), `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `IPAddressDeny=any`, `IPAddressAllow=localhost`.

v1 had one health timer and one backup timer per team. One of each for the host is fewer units, one
process a minute instead of one per team, and no per-team timer state for a removed team to leak into
a new team of the same name (H 7.17). Each team is still checked on its own, in parallel with a
bound, and one team's failure is that team's record and never stops another's (DC7).

#### 3.11.2 What a tick does, per team

`health --all` enumerates only teams that have a stamp, and re-checks the stamp under the lock
before it writes, so a tick that overlaps a `team remove` never writes a record for a removed
team. Steps 1-4 run without the team lock (3.4) and change nothing. A restart in step 2 writes
its event ("restarted after three missed probes") into the status record at once, under the
lock it already holds and with `seq` raised, so the event is never lost with a dropped view.
Step 3 also probes each LXC member's seat lock (3.9.2) for its `online` entry.

1. Unit state over D-Bus (DC17): `ActiveState`, `SubState`, `InvocationID`, `NRestarts`, asking the
   service unit only for properties a service has (H 7.24). Read the status record's `seq`.
2. Probe `/_matrix/client/versions` on loopback. After a 60 s start grace, three misses 10 s apart,
   with the same `InvocationID` still active, restart the unit: the team lock is taken without
   waiting only for the restart, and a busy lock means the holder owns the team's state (3.4). A
   rise in `NRestarts` is recorded once and kept for 24 hours; a fall means a manual restart.
3. Roster and room view, in the `teamchat-ctl` child, read-only: the roster; each member's and each
   human's membership of each room; each human's device count and newest sign-in; and the drift of
   every room against A's specs (3.6.2), each drift with its repair (`ccy-teams apply <team>`). The
   read-only child never completes a `pending` operation (3.6.3): it reports it as a problem whose
   fix is the verb that rolls it forward.
4. Backups: the newest usable backup (both halves present) and its age; a backup older than 1.5 days,
   or a failed last run, is a problem.
5. Take the team lock without waiting; if the record's `seq` is still the one step 1 read, write
   the status record with `seq` raised, else drop this tick's view for the team (another writer
   has written a fresher one).

#### 3.11.3 The status record

`/var/lib/teamchat-status/teams/<team>/status.json`, 0640 root:`<owner group>`, written atomically by the tick
and again at the end of every change verb, every apply, `human devices`, every backup and every
restore, so the board sees a change at once:

```json
{
  "v": 1,
  "team": "<team>",
  "name": "<display name>",
  "origin": "v2",
  "set": null,
  "migration": null,
  "seq": 812,
  "written_ms": 0,
  "expires_ms": 0,
  "homeserver": {"unit": "active", "since_ms": 0, "nrestarts": 0, "probe": "ok"},
  "members": [
    {"handle": "<handle>", "kind": "podman", "role": "coordinator", "state": "active", "rooms": {"main": "join", "reports": "join", "questions": "join"}},
    {"handle": "<handle>", "kind": "lxc", "role": "worker", "state": "active", "online": true, "rooms": {"main": "join", "reports": "join"}}
  ],
  "humans": [
    {"name": "<name>", "state": "active", "rooms": {"main": "join", "reports": "invite"}, "devices": 2, "last_seen_ms": 0}
  ],
  "backup": {"newest": {"id": 7, "time_ms": 0}, "last_run": "ok"},
  "problems": [{"what": "<what is wrong>", "fix": "<what to run>"}],
  "events": [{"at_ms": 0, "what": "restarted after three missed probes"}]
}
```

- `name` is the team's display name (3.8), rewritten on a rename (C44).
- `origin` is `v2` for a team created on v2 or `migration` for one a migration brought (D §3.9),
  written at the team's first apply (3.5.2 step 11) and never inferred from a missing field.
- `set` is the id of the team's current migration set, or null; `migration` is
  `{"phase": "<phase>", "accepted": <acceptance result>}` or null (D §3.9, §5.3). Both are
  written by D's `migrate` verbs. They are copies and never decide: the set record
  `/var/lib/teamchat-status/sets/<set-id>.json` does.
- `online` is present on LXC member entries only: the tick's probe of the member's seat lock
  (3.11.2), up to about 70 s old.
- `seq` is raised by one on every write, by every writer, always under the team lock. A writer
  that computed its view before taking the lock (only the tick does) writes only if `seq` is
  still the one it read, so the record never goes backwards (3.4). A record that is missing or
  unreadable counts as `seq` 0.
- `expires_ms` is `written_ms` plus 300 s. A record past it is itself shown as a problem ("health has
  not run since ..."), so a dead timer cannot look healthy.
- Room purposes are A's; the record carries whatever purposes A's specs name. The names `main`,
  `reports` and `questions` above are v1's, used as examples only.
- No token, password or device ID is in it (device IDs stay behind `human devices`, DESIGN B2).
- `teamchat-admin status [<team>...]` prints these records for a human, and exits 1 if any has a problem.

This record replaces v1's NOPASSWD reader design (DC4). Checked against what the owner needs:

- **Opening in about 1 s.** The board reads the record as a file, with no sudo and no process
  (3.13). A reader through NOPASSWD sudo would cost a sudo start and a child per team on every
  open, and the homeserver calls behind it scale with members and humans; that is what made v1's
  board slow.
- **Freshness, and what refreshes it.** The record is rewritten (a) at the end of every change
  verb and every apply, under the team lock, so a change the owner just made shows at once; and
  (b) by the health tick every 60 s (timer accuracy 5 s, plus the tick's own run). Facts that
  change with no verb, a member joining or leaving a room by itself or a human signing in from a
  new device, are therefore at most about 70 s old. The record expires 300 s after it was written,
  and an expired record is shown as a problem, never as current.
- **What is never taken from the record.** Whether a ccy or host member is online now is its
  seat lock, probed live by the board (3.13). An LXC member's seat lock is reachable only through
  `/proc/<init pid>/root` of a root-owned container, which the owner cannot open, so the tick
  (root) probes it (3.9.2) and records `online` per LXC member in the record: LXC online-ness is
  up to about 70 s old, and the board says so. whether a homeserver is up now is the live unit state from the
  board's one systemd query. Only membership, roles, room drift, device counts and backups come
  from the record.
- **The login flow re-reads.** After `human password` prints a password, ccy-teams keeps the same
  password-sudo session (the change is not over) and polls
  `sudo -n teamchat-admin human devices --team T --name N` every 2 s. The poll is bounded: it ends when
  a new device appears, when the owner ends the wait, when 5 minutes have passed since the
  password was typed, or when `sudo -n` fails. Whichever ends it, ccy-teams runs `sudo -k` (C10),
  and on a timeout or a failed `sudo -n` it says that the sign-in was not seen yet and the board
  will show it from the next tick. So the poll never keeps sudo usable beyond one bounded wait.
  `human devices` rewrites that human's entry in the status record under the team lock, so the
  board shows the sign-in at once.

What (b) of DC4 would add is device and membership facts fresher than about 70 s on every open,
at the price of a NOPASSWD sudoers rule and a process per team per open. Nothing the owner does
on the board needs that: the one moment that needs fresh device facts, the sign-in, re-reads
explicitly.

### 3.12 Backups and restore

#### 3.12.1 Backups

`teamchat-backup.timer` (`OnCalendar=daily`, `RandomizedDelaySec=1h`, `Persistent=true`) starts
`teamchat-backup.service`: `Type=oneshot`, `ExecStart=/usr/local/sbin/teamchat-admin backup --all`,
`TimeoutStartSec=30min`, with the health unit's sandbox (3.11.1) except `ReadWritePaths=/var/lib/teamchat-admin /var/lib/teamchat-status /run/teamchat-admin`
(the tars and the record) and the same capability set: it reads `ctl/<team>/` with no-follow
opens of regular files only (reading is not steered by the control user the way a write would
be), and it starts the `teamchat-ctl` child for the status refresh. `backup --team T` is also run by ccy-teams. Inside
`team apply` (3.5.2 step 6) the backup is an internal function called under the team lock the
apply already holds (3.4), never a second `flock`. Otherwise, per team, under its team lock
(waiting up to 10 minutes):

1. The homeserver must be active and ready (a signal sent before Tuwunel installs its handler is
   dropped, because it is PID 1 of its PID namespace; H 5).
2. Send `SIGUSR2` to the main process over D-Bus (`KillUnit`, whom `main`; DC17), which runs Tuwunel's
   database backup (`admin_signal_execute`), and wait up to 60 s for a new numbered backup.
3. Write the control state paired with it, `root/<team>/backups/<n>.tar` (0600): from `ctl/<team>/`
   the admin token, roster and rooms record, and from `root/<team>/` the config and `team.json`.
   Sources are opened without following links and must be regular files (I-B6).
4. Read the tar back with the parser restore uses (one parser for both, I-B5), and fail if it
   does not validate.
5. Delete tars whose database backup Tuwunel has pruned (`database_backups_to_keep`, seven).

A pair (database backup `<n>`, tar `<n>`) is taken under one team lock, so no admin change can fall
between its halves. Copying backups off the machine is not built, as the owner decided for v1 (DC14).

#### 3.12.2 Restore

`teamchat-admin restore --team T --backup N` (password sudo from ccy-teams), under the team lock. v1's
restore was not atomic: after it stopped the homeserver, any failure left the team down (I-B8). v2
stages first and swaps (C37, DC8):

A restore replaces the database only. The config, the drop-in, the firewall record and the
binary stay the ones the team runs now, because they were rendered for today's declaration and
Tuwunel version: a backup's config may bind a port the drop-in now denies, or carry a key the
pinned Tuwunel refuses (`error_on_unknown_config_opts`). The tar's `config.toml` and `team.json`
are used only as evidence that the backup belongs to this team.

1. Validate the tar fully (exact member set, regular files, sizes, strict JSON for the roster and
   rooms record), that database backup `N` exists, and that the tar's `server_name` equals
   `root/<team>/server-name`. Refuse, naming them, any leftover `db.previous`, `db.restore-*`,
   `config.restore.toml` or control `*.previous` file when no `restore.json` exists (below).
   Nothing stops before this passes; the tar is held in memory from here on.
2. Stage: a transient unit (`systemd-run --collect`) with the base unit's whole sandbox and the
   same identity, `DynamicUser=yes`, `User=teamchat-<team>` and `StateDirectory=teamchat/<team>`,
   so it gets the live unit's uid and its state directory, and nothing is chowned (systemd shares a
   dynamic uid between units that name the same user). It runs the binary the team's drop-in
   names, with `SocketBindAllow=tcp:<staging port>` and `IPAddressAllow=127.0.0.1/32 ::1/128`
   (the base unit denies every bind), and `LoadCredential=` from `root/<team>/config.restore.toml`
   (0600): the team's current config re-rendered with only two keys changed, `database_path` is
   `db.restore-<N>` and the port is the free loopback staging port. It runs
   `tuwunel --restore-backup N`, which restores into the staging path and then serves; once it
   answers, it is stopped. The live homeserver keeps serving meanwhile.
3. Only now back up the team as it is, through the internal backup function under the held team
   lock (3.4), while the live homeserver serves (C36). The order matters: Tuwunel keeps
   `database_backups_to_keep` backups (3.2.4) and prunes the oldest when it takes one, so taking
   `M` before staging could delete `N` itself when `N` is the oldest. Staged first, `N` is already
   copied into `db.restore-<N>` and its tar is in memory, and its pruning no longer matters. `M`
   is in the restore's answer, so a restore of the wrong `N` is undone by one `restore --backup M`.
4. Build the merged roster in memory (below).
5. Write the restore record `root/<team>/restore.json` (`{"v": 1, "backup": N, "pre": M, "step": "swap"}`, 0600, fsync) and the start block `root/<team>/restore.block` (empty, 0600).
   While the block exists the homeserver cannot start: the base unit carries
   `AssertPathExists=!/var/lib/teamchat-admin/%i/restore.block`, so a boot, a crash restart or a stray
   start fails visibly instead of creating an empty database where `db` is missing. Stop the live
   homeserver. In the state directory, opened as a directory fd with
   `O_DIRECTORY|O_NOFOLLOW` (the homeserver's uid owns its contents), rename `db` to
   `db.previous` and `db.restore-<N>` to `db`, each by `renameat2(RENAME_NOREPLACE)` relative to
   that fd. Replace the control state through the `teamchat-ctl` child, never by root directly
   (3.2.2): root sends the tar's `admin.token` and `rooms.json` and the merged roster on the
   child's stdin in one `restore.swap` request, and the child writes them with its atomic writer,
   keeping the current files as `*.previous`. Root's own state is not touched. Once every swap is
   on disk, update the record to `"step": "bootstrap"` (fsync), and only then remove the block.
6. Start, wait ready, assert listening, bootstrap. Bootstrap converges the restored database to
   the merged roster: every handle the roster holds `retired` is kicked and deactivated again,
   every handle it holds `parked` is revoked again, every member with `needs: reissue` is revoked
   (password reset with device logout), every human whose `revoked_ms` is after the backup's time
   is revoked the same way and set to `needs: password`, every human the roster holds `locked` is
   locked again, and every room is converged (a room created after the backup is gone from the
   database and is recreated or adopted, 3.6.2). So no token or password that was revoked after
   the backup works again.
7. On any failure in 5-6: write the block again and the record with `"step": "revert"`, stop,
   swap every renamed directory and file back (the control files by a `restore.revert` request to
   the child), remove `config.restore.toml`, the record and then the block, start, wait ready,
   and report that the restore failed and the team is as it was. Only a failure of this
   swap-back leaves the team down, and it is reported as that, with the record and the block left
   in place.
8. On success: delete `db.previous`, `config.restore.toml` and the `*.previous` files
   (fd-relative removal that never follows a link), remove the record, refresh the status record,
   and print the answer: `M`, and the handles and humans that need `reissue`, `return` or
   `human password`. The stamp stays valid, since the config is the current one.

A kill or power loss at any point of steps 5-8 leaves `restore.json` behind. Every root verb on
that team (`team apply`, `team remove`, `restore`, `backup`, the member, role and human verbs, and
the health tick's restart) first looks for it and refuses with exit 70, naming
`restore --team T --resume`. The resume reads the record and goes on from what is on disk, each
rename checked against the directory before it is made, so a second resume is harmless:

- `"step": "swap"` or `"revert"` (the block is still there): roll the swap back to the state
  before step 5, remove `config.restore.toml`, the record and then the block, and start the team;
  the restore is reported as not done.
- `"step": "bootstrap"` (the block is gone, the restored database is in place): start if needed,
  run step 6's bootstrap, then step 8. The revocations of step 6 are done before the team is
  reported restored.

The status record shows the team as "restore interrupted" with that fix. So no crash leaves a
team serving an empty database, no revoked credential stays valid after a resumed restore, and
no state needs a hand-made rename to recover.

The merged roster. A backup's roster is older than the team, and restoring it whole would break
C18 (counters falling, a retired handle coming back) and would forget every member added since.
So the current roster stays the truth for handles, and the database is brought to it:

- `counters`: per prefix, the larger of the current and the backup's value.
- A handle `retired` in either roster is `retired`.
- A handle in the current roster but not in the backup's has no account in the restored
  database: it becomes `lost`, and the board names `member return` as its fix, which creates the
  account again and issues a new bundle (`member return` accepts `parked` and `lost`).
- A handle `active` in both, whose current `token_ms` is after the backup's time, holds a token
  the restored database never issued, and the database holds the older token that a reissue had
  revoked: it gets `needs: reissue`, and step 6 revokes the older token before anything else can
  use it.
- A human whose current `revoked_ms` is after the backup's time had a password set or a device
  logged out since: the restored database would accept the old password and the lost device
  again, so step 6 revokes them and the human gets `needs: password` (`human password` clears it).
- Every other handle keeps its current state; `coordinator` and `coordinator_wanted` are the
  current ones, and `pending` must be empty (a restore refuses a team with a pending operation,
  naming the verb that completes it).
- A human `locked` in the current roster (`human lock`, or removed from the declaration, DC9) is
  locked again by step 6, since the restored database may have the account unlocked.
- If the current roster cannot be read, the restore refuses unless the owner passes
  `--roster-from-backup`. That takes the backup's roster, then raises its counters and its
  retired set from every handle named anywhere the host still has: the status record, the LXC
  records and the pool's `seat.json` files (a handle seen there and retired in no record is
  `lost`). So C18 holds even then. What remains unknown is printed.

Host check H-C5 proves that `--restore-backup` writes into a different `database_path` than the one
the backup was taken from, and that the transient instance can serve on a different port; if it
cannot, the stage step becomes "restore into the live path after the stop", with the swap-back of
step 7 still covering failure, and the decision is raised. H-C5 also checks that the transient
unit runs under the live unit's uid and that no file in the state directory changes owner.

### 3.13 What ccy-teams reads

An open (picker or `--list`) reads, all in-process, as the owner:

| Read                                                          | Cost on a desktop     | Notes                                                                                                                                                                                                                                                                     |
| ------------------------------------------------------------- | --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| host_vars: one strict decode of the v2 keys                   | ~2 ms                 | once per open, shared by every view (fixes v1's three `declare overview` runs)                                                                                                                                                                                            |
| `/var/lib/teamchat-status/installed.json`                     | \<1 ms                | installed `teamchat-admin` digest, Tuwunel version, kit digest                                                                                                                                                                                                            |
| per team: `stamp.json`, `status.json`, `lxc/<team>.json`      | \<1 ms each           | three small files per team, whatever the number of members or people                                                                                                                                                                                                      |
| one D-Bus `ListUnitsByPatterns(['teamchat-server@*'])` (DC17) | ~5 ms                 | live unit states, in case the status record is up to a minute old                                                                                                                                                                                                         |
| the seat store: pool and session directories                  | ~1 ms per seat        | the pool's `seat.json`, `released.json` and `held/held.json` of each seat, and the L3 probe of the seat lock in the holding session's directory, through `openat2` with `RESOLVE_BENEATH\|RESOLVE_NO_SYMLINKS` from that directory's fd, `O_NONBLOCK` and `fstat` (3.7.3) |
| the checkout's source digest                                  | ~5 ms, cached by stat | for "software out of date" (below)                                                                                                                                                                                                                                        |
| session records, tmux list, git probes for "join here"        | ~30 ms                | as the stage-1 board already does                                                                                                                                                                                                                                         |

Seat releases the board finds due (a timed-out leave, a session that is over, 3.7.3) are never
run on the open path: the board draws from files first, shows those seats as "leaving" or
"releasing", and only then runs the releases in the background, redrawing when each ends. The
open never waits on a seat lock or on a homeserver call.

That is roughly 0.1 s at any scale, against v1's 9-23 s (22 Python starts and T+H sudo calls), with
no sudo, no Python, no HTTP and no process per checkout, team or person (C39). A redraw re-reads only
files whose size or mtime changed and asks systemd again, well under 200 ms.

The APPLIED verdict per team, computed in-process with the same Go code `team apply` uses:

- **up to date**: the stamp's `config` equals the digest of the team's declaration rendered now
  (including the live bridge subnet), and its `software` equals both the installed digest and the
  checkout's source digest;
- **out of date** (config, software, or both, said which);
- **not built**: declared present, no stamp;
- **decide**: a stamp for a team declared absent or not declared;
- **unreadable**: a record ccy-teams cannot decode, shown on the team row.

The checkout's source digest is computed over a tracked list of the files `teamchat-admin` is built from
(its package closure), which a QA gate keeps exact against `go list -deps`, so ccy-teams hashes the
right files without running the Go toolchain on every open (the "software digest needs a v2
definition" gap of SPEC-view 3.7).

Changes go the other way: ccy-teams takes the play lock for anything that edits host_vars or runs
the play, and calls `sudo teamchat-admin <verb>` with the owner's password for member, role, human,
backup and restore verbs, parsing the one JSON answer each prints.

### 3.14 How each part is tested

| Part                      | Tests                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| config and drop-in render | golden files; every key checked against the vendored key list of the pinned Tuwunel; a pin bump without a refreshed list fails                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| declaration               | table tests of every refusal; the YAML 1.1/1.2 ambiguity list; the same fixture through the Go reader and through Ansible's parser in the VM lab, which must agree                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| caller classes            | one table test over every verb and every class; exit 77 and the message                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| root and child contract   | strict decode of every message type; a fuzz test of root's decoder of the child's answer                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| roster and member verbs   | every transition of 3.6.3 against `fakehs`; an injected failure after every step, then a rerun, must reach the same end state with no second valid token; counters never fall                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| rooms engine              | against `fakehs` with A's specs: create, adopt after a crash between create and record, more than one candidate refused, kicks before state before invites, a removed human gone from every room before power levels change                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| locks                     | two real processes per lock; the holder line; bounded waits; the health tick never waiting; every writer of `status.json` (each change verb, `human devices`, apply, backup, restore) interleaved with a tick, and the record never going backwards                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `team apply` and `remove` | a fake systemd D-Bus connection; `execx.Fake` for `firewall-cmd` (exit 0, 1 and any other code), `nmcli`, `ip`; `/proc/net/tcp{,6}` fixtures; the restart set and the reload set each changed alone; a first apply that fails after the first start, then a run with a different `server_name`, refused; an `os.Root` tree in `t.TempDir()`; Plan 00192's sweeps (every external call failing in turn must fail the verb with a message; every working directory replaced by a file)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| backups and restore       | the tar parser shared by write and validate, against hostile tars (links, devices, `..`, setuid, extra members, oversize); restore's swap-back with a failure injected at each step; the merged roster (counters never fall, a handle retired after the backup stays retired, one added after it is `lost`, one reissued after it needs `reissue` and its older token is refused after the restore, a human whose password was reset after it has the old password refused); the control swap done only by the child; a restore after a port change and after a pin bump that dropped a config key, both succeeding with the current config; the pre-restore backup `M` named in the answer and restorable; a restore of the oldest backup with the keep count full; a kill after each step of 5-8, then `--resume`, ending rolled back (block present) or completed with step 6's revocations (block gone), and every other verb refused meanwhile; a start attempted while the block exists failing its assertion |
| seats                     | take into a session directory where a destination is already planted (a directory, a symlink, a FIFO), a planted non-empty directory, a foreign owner, and a race that swaps the new directory for a link; release moves to quarantine and never into the pool, the pool bundle byte-identical after a session rewrote its copy; `held.json` with `O_EXCL`; a release whose notice fails transiently keeps `held.json` and the retry posts once; a notice refused for good (handle parked, retired or lost, `M_UNKNOWN_TOKEN`, team absent) is recorded and the release completes; a seat lock path made a symlink to an owner file, a FIFO or a device, refused by the probe; a placement into an existing seat by `RENAME_EXCHANGE`, the old bundle quarantined; a `reissue` of a held seat refused; a leave that times out re-run by the board                                                                                                                                                                   |
| hot control               | `desired.json` order after the session record; active = seated and desired, each alone not active; leave with a farewell waits for the seat lock and exits 75 when it stays held                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| LXC read side             | fixture rootfs trees: `dir`, bind mounts, a symlink at every component, an oversize file, an unprivileged config refused; `openat2` on the real kernel (Linux-only test)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| real Tuwunel              | behind `-tags tuwunel` with `FAKEHS_TUWUNEL_BIN` (M44): bootstrap, the rooms of A's layout, the whole member lifecycle, a restricted join, a power-0 refusal, restore into a staging path                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| VM lab                    | the play on a fresh VM: no teams (nothing resident, no timer enabled); one team; two teams; an LXC member running, then stopped, then started; a new team with an LXC coordinator in one play; a coordinator declared before its ccy member exists, promoted at its first seat take; a Tuwunel pin bump with a play scoped to one team, which leaves the other teams' drop-ins on the old version and backs up the scoped team before its restart; a restore that succeeds and one forced to fail; v1 installed beside v2 and untouched (the list of everything v2 creates, which area D's proof uses)                                                                                                                                                                                                                                                                                                                                                                                                              |
| host checks               | section 8, through meta-deploy                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |

### 3.15 Resident cost, honestly

| Scope            | Resident                                                                                                                          | Units, ports, other                                                                                                                                            |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| v2 with no teams | nothing                                                                                                                           | unit files installed but nothing enabled; no timer runs; no port; no firewall rule                                                                             |
| per team         | one Tuwunel, 100-135 MB RSS as measured in the 00161 spike (to be re-measured, and reduced if the cache modifier allows, H-C1)    | one service and its drop-in; one TCP port on loopback and each `listen` address; one firewall rule set; no timer of its own; one `teamchat-ctl` child per tick |
| host, with teams | nothing                                                                                                                           | two timers: one oneshot a minute, one a day. Each minute: one `teamchat-admin` process, plus one `teamchat-ctl` child per team                                 |
| per member       | the homeserver's memory for the member's long-poll sync and its share of the caches (inside the team's Tuwunel, measured by H-C1) | the pool bundle and a session copy; the member's keeper and worker are area B's cost and are measured there                                                    |
| per human        | the same, while the human's Element is open                                                                                       | an account on the team's homeserver                                                                                                                            |
| per LXC guest    | nothing beyond what area B runs                                                                                                   | each kit digest a guest process still runs, the seat directories, and with `service: true` one unit in the guest                                               |

The health tick's homeserver calls grow as teams × rooms × (members + humans) per minute: one
membership read per room and person, and one device read per human, all on loopback. With a team
of a few members and A's few rooms that is tens of calls a minute; it grows linearly in each
factor, and the first host check (H-C1) records the tick's run time and the homeserver's CPU per
tick, per team, at the team sizes the owner runs, so the figure is measured rather than guessed.

Against v1 per team: the same one homeserver, one service instead of a service and two timers, and
no sudo or Python process on any read.

## 4. Decisions for the owner

Each has a recommended default, taken unless the owner says otherwise ("DO NOT GATE ON ME"). None
is irreversible before v2 is built; DC1, DC5 and DC15 become expensive to change once teams run
on v2.

| ID   | Decision                                         | Options                                                                                                                                                                                                                                                                                                                                                                                                                                                    | Default and why                                                                                                                                                                                                                                                                                             |
| ---- | ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| DC1  | Homeserver topology                              | (a) one per team; (b) one for every v2 team                                                                                                                                                                                                                                                                                                                                                                                                                | (a): isolation, per-team restore and removal, per-team throttling, clean per-team migration and rollback; the cost is 100-135 MB per team, tunable (3.2.1)                                                                                                                                                  |
| DC2  | Homeserver identity                              | (a) `DynamicUser` per team, admin credential held by `teamchat-ctl`; (b) one service user for every homeserver, kept apart by mounts as v1                                                                                                                                                                                                                                                                                                                 | (a): separation by uid rather than by a sandbox detail; the network-facing process no longer holds the admin credential (3.2.2)                                                                                                                                                                             |
| DC3  | Host programs                                    | (a) one `teamchat-admin` with caller classes; (b) one binary per privilege (installer, admin, reader)                                                                                                                                                                                                                                                                                                                                                      | (a): one digest, one install, shared packages; the narrow-target reason for (b) went with DC4 (3.3)                                                                                                                                                                                                         |
| DC4  | How the board reads members, rooms and devices   | (a) a status record root writes each minute and after each change, read with no privilege; (b) a reader run as `teamchat-ctl` through a NOPASSWD Runas sudoers line (v1 DESIGN D1); (c) a daemon with a socket                                                                                                                                                                                                                                             | (a): no sudoers file at all, no process holding every admin token run on an agent's request, and the open needs no PAM or process per team; the cost is that a change made outside any verb (a member's own room join, a new device) shows within about 70 s; the sign-in flow re-reads explicitly (3.11.3) |
| DC5  | Where seats live                                 | (a) a seat store outside the checkout, with one session directory mounted per ccy session; (b) in the checkout, as v1                                                                                                                                                                                                                                                                                                                                      | (a): a coordinator's token is no longer readable by workers sharing the checkout, no inbox can be consumed by a sibling, and no token can be committed; needs the launcher to mount one directory (3.7.1)                                                                                                   |
| DC6  | Where the coordinator is declared                | (a) team-level `coordinator` in host_vars, converged by apply and by `role apply`; (b) the roster as truth, set by `set-role`, and LXC entries' `role`, as v1                                                                                                                                                                                                                                                                                              | (a): one truth that the hot path and the play converge to alike. A coordinator declared before its member exists is recorded as `coordinator_wanted`, shown with its fix, and promoted by the operation that creates the handle; demotions are never deferred (3.6.4)                                       |
| DC7  | Timers                                           | (a) one health timer and one backup timer for the host; (b) one of each per team, as v1                                                                                                                                                                                                                                                                                                                                                                    | (a): fewer units and processes, no timer state to leak into a new team of an old name (3.11.1)                                                                                                                                                                                                              |
| DC8  | Restore                                          | (a) stage into a new database path, swap, swap back on failure; (b) v1's restore in place after the stop                                                                                                                                                                                                                                                                                                                                                   | (a), subject to H-C5: a failed restore leaves the team as it was (3.12.2)                                                                                                                                                                                                                                   |
| DC9  | Removing a human                                 | (a) kick from every room and lock the account; adding the name back unlocks it; (b) v1's `removed_humans`, the name never returns                                                                                                                                                                                                                                                                                                                          | (a): reversible and one key fewer; v1's rule came from deactivating, which v2 does only for retired agents                                                                                                                                                                                                  |
| DC10 | Healing a deferred LXC member                    | (a) only when the play next runs; (b) an LXC start hook applies the last rendered declaration for that container                                                                                                                                                                                                                                                                                                                                           | (a) for now: the play stays the one route and the board shows the deferred member with its action; (b) is a later plan if deferred members turn out to be common                                                                                                                                            |
| DC11 | Lock model                                       | (a) one lock per team; (b) one host-wide lock                                                                                                                                                                                                                                                                                                                                                                                                              | (a): one team's backup never blocks another team's seat take (3.4)                                                                                                                                                                                                                                          |
| DC12 | Firewall calls                                   | (a) `firewall-cmd` judged only by exit status (`--query-rich-rule`); (b) firewalld's D-Bus API from Go                                                                                                                                                                                                                                                                                                                                                     | (a): firewall changes happen only at apply and remove, rarely, and v1's two firewall bugs were both text parsing, which (a) no longer does; a CLI judged by exit status (0, 1, anything else fails) is enough there, while D-Bus (DC17) is kept for the per-minute and per-open calls                       |
| DC13 | Starting a supervised session in a running guest | (a) the guest's systemd over its private socket from the host; (b) the unit starts at the next container start only                                                                                                                                                                                                                                                                                                                                        | (a) if H-C9 proves it and DC17 is (a); else (b). Running the guest's `systemctl` as root is ruled out                                                                                                                                                                                                       |
| DC14 | Backups off the machine                          | (a) not built; (b) a copy to a declared destination                                                                                                                                                                                                                                                                                                                                                                                                        | (a): the owner's decision for v1 stands; a backup is the root of trust (R38) and copying it widens who holds it                                                                                                                                                                                             |
| DC15 | What travels between sessions on a seat          | (a) only the host-held bundle in the pool travels; a session's member state is archived to quarantine on release; (b) member state travels too, after a whole-tree check (regular files and directories only, owner-owned, no links)                                                                                                                                                                                                                       | (a): no byte a container wrote reaches another session, so a hostile session cannot plant content (an inbox entry, a cursor, a config) in the next one; B resumes from the server's state instead (3.7.1, C41)                                                                                              |
| DC16 | The token across sessions                        | (a) the pool's token is reused by each session that takes the seat; (b) a reissue on every take                                                                                                                                                                                                                                                                                                                                                            | (a): a session start needs no password; the cost is K13, a released hostile session may keep a valid copy until the owner runs `reissue` or `park` (3.7.3)                                                                                                                                                  |
| DC17 | systemd and YAML mechanisms                      | (a) add `github.com/godbus/dbus/v5` and `go.yaml.in/yaml/v3` to the production module `src-go/go.mod`, with Plan 00204 allowlist rows; D-Bus for host systemd (unit properties, `KillUnit` main SIGUSR2, `ListUnitsByPatterns`) and the guest's private socket (peer-to-peer dial and auth, no `Hello`); (b) `systemctl show -p` key=value blocks and `systemctl kill --kill-whom=main` through `execx`, judged by exit status, and DC13 falls back to (b) | (a): typed properties instead of parsed text on the per-minute and per-open paths, no `systemctl` process per call, and the only way to reach a guest's systemd without running a guest program; both modules are pure Go and widely used                                                                   |

## 5. Risks

| ID  | Risk                                                                                                                                                                                                                          | What contains it                                                                                                                                                                                                                             |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| K1  | A Tuwunel per team costs 100-135 MB each                                                                                                                                                                                      | H-C1 measures and tunes; `MemoryHigh` throttles one homeserver before it crowds the desktop; DC1 states the trade                                                                                                                            |
| K2  | A hardening directive new in v2 stops Tuwunel starting, or a later Tuwunel needs a system call the filter denies                                                                                                              | H-C2 on every pin bump; the readiness wait fails the apply at once with the journal tail                                                                                                                                                     |
| K3  | An owner with passwordless sudo makes every agent running as them able to change a team                                                                                                                                       | stated in the user docs, as in v1; nothing in v2 widens it, and v2 has no NOPASSWD rule of its own                                                                                                                                           |
| K4  | The seat store is new: a seat could stay held by a dead session's `held.json`, or a launcher could fail to mount the session or control directory                                                                             | the board lists every seat with its holder and offers release; any take or open that finds the holder dead and not restorable releases it; a launcher that cannot mount refuses to start the session (fail fast)                             |
| K5  | The status record is up to about 70 s old for changes made outside admin verbs (a member's own room join, a new device)                                                                                                       | every admin verb and apply rewrites it at once; the sign-in flow re-reads (3.11.3); the record carries its write time and expiry, and the board shows its age                                                                                |
| K6  | Ansible's YAML 1.1 and Go's YAML 1.2 could read one declaration two ways                                                                                                                                                      | the schema avoids every differing scalar and the Go reader refuses them; the VM lab runs one fixture through both                                                                                                                            |
| K7  | Reaching a guest's systemd from the host over its private socket is unproven                                                                                                                                                  | H-C9; DC13's fallback                                                                                                                                                                                                                        |
| K8  | Restoring into a staging database path is unproven                                                                                                                                                                            | H-C5; the swap-back covers failure either way                                                                                                                                                                                                |
| K9  | Root installs programs built from a checkout the owner can write, and every agent running as the owner (a host `cc` session, and ccy containers, whose root is the owner's uid) can change that checkout before the next play | the repository-wide boundary of every play, unchanged from v1 and stated, not solved here; the build runs as the user so no toolchain runs as root                                                                                           |
| K10 | `openat2` through `/proc/<pid>/root` of a running guest: the root itself is a magic link                                                                                                                                      | the directory fd is opened first with a plain `open` of `/proc/<pid>/root` (pid checked against `lxc-info` before and after); every later path is resolved from it with `RESOLVE_IN_ROOT\|RESOLVE_NO_SYMLINKS\|RESOLVE_NO_MAGICLINKS`; H-C10 |
| K11 | One host-wide health process: a crash in it leaves every team unchecked                                                                                                                                                       | the unit fails visibly, and every team's status record expires within five minutes and shows as a problem on the board                                                                                                                       |
| K12 | A host `cc` session runs as the owner with no container, so it can read every seat under `~/.local/state/teamchat/seats`, the pool's coordinator token included, and every Element profile                                    | stated, as for Element (3.10): C25's isolation is between ccy sessions only. The option offered, not built: refuse a coordinator seat in a checkout while a host-kind member runs there                                                      |
| K13 | With DC16 (a), a session that held a seat and was hostile can keep a copy of its token after release, and use it while the next session holds the seat                                                                        | the board shows when each seat's token was last issued (the roster's `token_ms`); `member reissue` or `park` revokes the token; DC16 (b) closes it at the price of a password per session start                                              |

## 6. What v2 drops from v1, and what is new

### 6.1 Dropped

| v1                                                                                                                           | Why it goes                                                                                                                                                                                                                                                                               |
| ---------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| the bash installer, the bash root wrapper, the Python zipapps, `runuser`, `setpriv`, the tar hand-over                       | one Go program (C8); a typed JSON pipe between root and `teamchat-ctl` instead of a tar root unpacks                                                                                                                                                                                      |
| the installer's `software` command, its `curl` download and self-copy                                                        | plain play tasks (`get_url` with a checksum, `copy`), which Ansible does better (3.5.1)                                                                                                                                                                                                   |
| `AGENT_BUS_INSTALL_TEST_PREFIX` and every test hook in production code                                                       | fakes and build tags only                                                                                                                                                                                                                                                                 |
| one `agent-bus` user for every homeserver, with `TemporaryFileSystem` and `BindPaths` to keep them apart                     | `DynamicUser` per team (DC2)                                                                                                                                                                                                                                                              |
| per-team backup and health timers, and the timer-state clean-up for removed teams                                            | one of each for the host (DC7)                                                                                                                                                                                                                                                            |
| the NOPASSWD sudoers rule for `list` and `human devices`, and v1 DESIGN's planned `view` Runas reader                        | the status record (DC4)                                                                                                                                                                                                                                                                   |
| seats inside the checkout, the `git check-ignore` proof, the seat `README`                                                   | the seat store (DC5); `teamchat` explains a seat on request                                                                                                                                                                                                                               |
| undo on a failed member change                                                                                               | roll forward from `pending` (C20)                                                                                                                                                                                                                                                         |
| `rotate-token`                                                                                                               | `reissue`, which revokes before it mints                                                                                                                                                                                                                                                  |
| `set-role` as a write of its own, and the LXC entry's `role`                                                                 | the declared `coordinator` and `role apply` (DC6)                                                                                                                                                                                                                                         |
| `removed_humans`                                                                                                             | lock and unlock (DC9)                                                                                                                                                                                                                                                                     |
| a seat's member state kept for the next session in the checkout, and the container-written `said-goodbye.json` the host read | the pool bundle only travels (DC15); a goodbye is judged from the room on the server (3.7.5)                                                                                                                                                                                              |
| the LXC entry's `repo`                                                                                                       | the LXC record's handle map                                                                                                                                                                                                                                                               |
| Python run inside guests, as root and as the agent user; the Python 3.11 prerequisite                                        | host-side writes and `teamchat guest` as the agent user (3.9.1)                                                                                                                                                                                                                           |
| `bridge.py`, `config_restart.py`, `timer_state.py`, `render`, `stamp-group` as separate processes                            | functions of `teamchat-admin`, or gone with DC7                                                                                                                                                                                                                                           |
| the play's backup of every running team before a software swap                                                               | the binary is named per team in its drop-in, so it changes only when that team's `team apply` rewrites the drop-in, and that apply backs the team up first (3.2.3, 3.5.2 step 6); a crash, watchdog or health restart, and every team a scoped play leaves alone, stays on its old binary |
| the packages `python3`, `curl`, `jq`, `tcpdump`, `util-linux` for the bus                                                    | nothing on the v2 path calls them                                                                                                                                                                                                                                                         |
| v1's legacy forms (`orchestrator`, registry v1, container-name LXC seats)                                                    | v2 never reads them; area D's migration maps what it carries over                                                                                                                                                                                                                         |

### 6.2 Kept on purpose

The Tuwunel config keys and their reasons, the unit's sandbox and restart policy, the firewall
record written before any change, bootstrap's crash-safe order, shared-secret registration of the
admin, park as revoke, the handle grammar and seat numbering, the allocation lock, the stopped
container rules and the LXC output lines, `CHANGED` output and the stamp written last, restore
validated before anything stops, and the Element profile's lock-down and refusal.

### 6.3 New, and what each fixes

| New                                              | Fixes                                                                                                                     |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------- |
| per-team uid and `LoadCredential` for the config | isolation resting on a mount detail; the homeserver holding the admin credential and able to rewrite its own config       |
| team locks taken by every mutating verb          | v1's installer had no lock (I-B16): the play, the backup timer and ccy-teams' direct calls could interleave               |
| `pending` and roll forward                       | half-done member changes, orphan counters, minted tokens nobody holds                                                     |
| `reissue`                                        | a lost bundle needed a park and a return by hand                                                                          |
| the seat store and per-session mounts            | coordinator tokens readable by workers; sibling sessions consuming each other's inbox; tokens one `git add` from a commit |
| the pool bundle, `held.json` and the quarantine  | a container-written bundle or member state reaching the next session in the same checkout                                 |
| the Tuwunel version in each team's drop-in       | a homeserver restarted onto a new binary (and a one-way migration) without its team's backup                              |
| the status record                                | a board that needed root, sudo and a process per team and per person to draw                                              |
| staged restore with swap-back                    | a failed restore leaving the team down (I-B8)                                                                             |
| the declared coordinator                         | a live role change reverted by the next apply                                                                             |
| no container code as root                        | the exec-into-guest-as-root class of escape, and the Python prerequisite in every guest                                   |

## 7. Assumptions on other areas

Each assumption below is settled in [DESIGN-v2.md](DESIGN-v2.md) section 3, the seam contract.

**Area A (protocol and rooms).**

1. A's package provides `Desired(team, roster) []RoomSpec`, a typed value naming each room's purpose,
   create parameters, state events (type, key, content), wanted members (join or invite), unwanted
   members and the kick reason. C's engine (3.6.2) applies it and knows nothing of what a room is
   for.
2. The bundle carries the main room's ID (`main_room`, A §6.2's anchor); members find the other
   room IDs, the roster and limits in the main room, not in the bundle.
3. The team record's roles are derived from the roster and the declared coordinator. A defines
   the record, and both the meaning and the shape of the `trust` block; the shape in 3.8 is v1's,
   shown as an example, and C decodes the block with A's Go type.
4. The admin account's localpart stays `admin`, and the drift check in the status record uses A's
   specs as its reference. The room names in 3.11.3 are examples; the record carries A's purposes.
5. A's package owns the handle grammar, its parser and formatter, and the member type set (3.7.4).
   A's team id grammar (A §2.2) allows at most 22 characters, so `teamchat-<team>` fits systemd's
   31-character user-name limit (3.2.3). A team's display name (`name`) is display only and never
   part of a user, unit or path name.
6. A defines the goodbye and disconnected message types that B posts (3.7.5).

**Area B (member).**

1. `teamchat` has B's guest operations (B 2.13): `guest check`, `guest place-bundle`,
   `guest membership` and `guest remove`, every one run as the agent user (`--user` must name the
   caller) with typed JSON on stdin; none runs as root, and the kit and the session unit are C's
   host-side writes (3.9.2, 3.9.4). The code `guest place-bundle` uses for files is the same
   `internal/teamchat/seat` code ccy-teams uses.
2. B reads `seat.json` and `token` and owns everything else in a session seat directory, including
   the seat lock, which is a `flock` the host can probe with a shared non-blocking lock (L3). B
   never expects the host to read back anything B wrote there.
3. B finds the session's seats at `/run/teamchat/seats` and its desired membership at
   `/run/teamchat/membership`; a seat may appear while the session runs (hot join), and a team is active
   only when it is both seated and desired (3.7.5).
4. B's keeper watches `desired.json`: on a `farewell` entry it posts the goodbye as that member,
   drains its outbox, and only then releases the seat lock.
5. A session that takes a seat starts with no member state. B resumes from the server's state
   (its own read receipts and the room timeline), never from state another session left (DC15).
6. B provides `teamchat seat disconnected`, run by the host as the owner with the pool bundle and
   the claim id, which judges from the room whether this member already said goodbye since the
   claim and otherwise posts the disconnected notice once, with a transaction id derived from the
   claim id (C43).
7. A session resolves its kit path once at launch and runs from that digest to its end (C33).
8. B decides the member's own config in an LXC guest's home and the program a supervised session
   runs.
9. A member treats a refused connection to its homeserver as transient, because `team apply`,
   health and restore restart homeservers (LR 6 item 11).

**Area D (coexistence and migration).**

1. Settled: D's names are merged throughout this file (D §3.2). There is no port range: ports are
   declared per team (D §3.8). The server-name domain is `teamchat.internal`.
2. D's migration of a team uses C's `migrate` verbs and `footprint` (3.3, behaviour in D §5.4,
   §5.5), plus `team apply` for the v2 team, `member add --seat <n>` to keep each v1 handle's seat
   number, `human password` for each human's new Element sign-in, and `team remove` for rollback.
3. C supplies, for D's proof that v2 does not touch v1, the full list of what v2 creates: users,
   paths, units, ports, firewall records and interfaces, from the play's tasks and `team apply`'s
   writes, checked in the VM lab.
4. The ccy launcher (D's, with B) mounts `~/.local/state/teamchat/seats/sessions/<session>/` read-write at
   `/run/teamchat/seats` and `~/.local/state/teamchat/seats/control/<session>/` read-only at `/run/teamchat/membership`, and
   nothing else of `~/.local/state/teamchat/seats`. Neither `~/.local/state/teamchat/seats` itself nor the owner runtime directory that
   holds the play lock and the allocation locks may lie under any path a ccy container mounts:
   C25, C26 and C41 rest on it. D's names put the seat store at `~/.local/state/teamchat/seats`
   (`$XDG_STATE_HOME` honoured) and the allocation locks under `$XDG_RUNTIME_DIR/teamchat/alloc/`,
   both on no ccy mount; ccy's mount list is checked against them.
5. When a ccy session ends by the owner's choice, the launcher removes its session record and
   then runs ccy-teams' seat release for that session (3.7.3), so the disconnected notice is
   posted then, not at the next board open. A container that dies with its record in place is
   restorable and is not released (3.7.1).
6. D's migration declares coordinators like any other, before or after their members exist
   (3.6.4).
7. D names the play that owns an LXC container's config (the repository's LXC container play,
   or none). The `service: true` pre-check (3.9.2 step 3) names `lxc.start.auto = 1` and that
   play's variable for it; if no play owns the container's config, D says so and the message
   names the setting only, as v1 did. v2 never edits a container config itself.

## 8. Host checks

Each runs on the host through meta-deploy once the code exists; each states what it proves.

| ID    | Check                                                                                                                                                                                                                                                                                                               | Proves                                   |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| H-C1  | RSS of one Tuwunel per team at the default and at a reduced `cache_capacity_modifier`, idle and under a burst of member traffic, reported per team and per connected member; the health tick's run time and the homeserver's CPU per tick, per team                                                                 | C7, the `MemoryHigh` and modifier values |
| H-C2  | the hardened unit starts the pinned Tuwunel, with each directive new in v2 (empty capability set, system call filter, `PrivateIPC`, `ProtectProc`, `LoadCredential` with `%d`)                                                                                                                                      | 3.2.3                                    |
| H-C3  | the `DynamicUser` uid named by `User=teamchat-<team>` is stable across restart and reboot, is shared with a transient unit naming the same user, and no file changes owner; `systemctl clean --what=state` removes the state                                                                                        | DC2                                      |
| H-C4  | `SIGUSR2` through D-Bus `KillUnit` (whom `main`) to the main process yields a numbered backup within 60 s                                                                                                                                                                                                           | 3.12.1                                   |
| H-C5  | `--restore-backup` into a staging `database_path` on another loopback port under the live unit's uid (no chown), then the swap, and a forced failure swapped back; after the restore, a member token reissued after the backup and a human password reset after it are both refused                                 | DC8, C37                                 |
| H-C6  | `firewall-cmd --query-rich-rule` exit status, and the zone chosen for a CIDR whose interface has no zone                                                                                                                                                                                                            | DC12                                     |
| H-C7  | `ccy-teams --list` and the interactive open timed with several teams, checkouts and humans; a redraw timed; an open with a seat release due, which must draw before the release ends                                                                                                                                | C39                                      |
| H-C8  | a seat taken into a running ccy container's mounted session directory (mkdirat and openat copies) is visible inside it; `desired.json` in the read-only control mount cannot be written from inside; the host's probe of a seat lock held inside reports held; a release leaves nothing of that seat visible inside | DC5, C27                                 |
| H-C9  | from the host, the guest's systemd over `/proc/<pid>/root/run/systemd/private`: reload, start, stop a unit                                                                                                                                                                                                          | DC13                                     |
| H-C10 | `openat2` with `RESOLVE_IN_ROOT` through `/proc/<pid>/root` of a running overlay-store guest; `lxc-attach -u -g --clear-env --set-var` running the host-written `teamchat` with `HOME` set and no supplementary group (`getgroups()` empty but the agent's own)                                                     | 3.9.1, K10                               |
| H-C11 | the play with no v2 team: nothing resident, no timer enabled, no port bound; v1's units, users, paths and rules unchanged                                                                                                                                                                                           | C40                                      |
| H-C12 | under the real Tuwunel: a restricted join to a room A's layout restricts, a power-0 post refused, pending invites left pending                                                                                                                                                                                      | the S11 probes, carried                  |
