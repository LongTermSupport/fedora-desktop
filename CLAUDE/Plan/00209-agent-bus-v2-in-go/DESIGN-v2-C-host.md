# Plan 00209 N0, area C: the host side of v2

This is area C of the v2 design (see `DESIGN-v2-FRAME.md`, which binds it). It owns the
homeserver instances, the host programs and their privilege, the install lock, admin operations,
seats and the bundle a seat receives, the host_vars declarations, the play, LXC member apply,
Element profiles, health, backups and restore, and what ccy-teams reads.

Placeholders, until area D's names merge:

| Placeholder       | What it names                                                                  |
| ----------------- | ------------------------------------------------------------------------------ |
| `<v2-host>`       | the one host program (root, control-user and owner modes; section 3.3)         |
| `<v2-bin>`        | the member program (area B), also installed on the host and copied into LXC    |
| `<v2-ctl>`        | the system user that holds every team's admin credential (not the homeservers) |
| `<v2-hs@>`        | the per-team homeserver unit template (`<v2-hs@><team>.service`)               |
| `<v2-health>`     | the host-wide health timer and service                                         |
| `<v2-backup>`     | the host-wide backup timer and service                                         |
| `<v2-state>`      | root-owned state root (control state, homeserver state, root records)          |
| `<v2-applied>`    | the owner-group-readable records root writes for the owner (stamps, status)    |
| `<v2-run>`        | the root runtime directory holding the host locks                              |
| `<v2-seats>`      | the owner's seat pool and session seat directories                             |
| `<v2-kit>`        | the versioned member kit root on the host and in an LXC guest                  |
| `<v2-if>`         | the v2 bus address interface (a NetworkManager dummy connection)               |
| `<v2>_teams` etc. | the host_vars keys (section 3.8)                                               |
| `<port>`          | a team's homeserver port, from a v2 range area D assigns                       |

Source keys used in the "from" column and in the text: `R<n>` = `DESIGN.md` section 2; `H 7.nn` =
`RESEARCH-host.md` hard-won behaviours; `L` = `RESEARCH-lxc-robustness.md`; `GC` =
`RESEARCH-go-callers.md`; `M<n>` = `SPEC-matrix.md`; `S<n>`/`L<n>` = `SPEC-state.md`; `P<n>` =
`SPEC-proto.md`; `V<n>` = `SPEC-view.md`; `S11` = Plan 00211 `SPEC.md`; `I` = the installer port's
`SPEC-agent-bus-install.md` (Plan 00192, branch `go-port/agent-bus-install`), its bug list as `I-B<n>`.

## 1. What area C is for, in one paragraph

The host runs one private homeserver per v2 team and the tools that set it up and change it. It
is driven by Ansible from host_vars (IaC), and every change to a team (a member, a role, a human's
password) needs the owner's password at the time. Every read the owner's board makes is a file
read: nothing on the read path runs as root, asks sudo, or starts a process per team, per checkout
or per person. A seat's credential never sits in a git checkout and is never visible to a sibling
session. An LXC member is applied by the play from the host, reading the container from the host
side, and never runs container code as root. A v2 install shares no user, path, unit, port,
interface, firewall record or sudoers file with v1, and is inert with no teams declared.

## 2. Requirements

Each requirement names where it came from. `new` marks a requirement v1 did not have, with what it
fixes in section 6.

### 2.1 Homeserver instances

| ID  | Requirement                                                                                                                                                                                                                                                     | From                   |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| C1  | One private homeserver per team: federation off, registration closed, encryption off, room creation by the team's admin account only, every room invite-only or restricted as area A's layout says.                                                             | R1                     |
| C2  | A team's homeserver listens on loopback and on each declared `listen` address only, on one port. Inbound traffic is admitted only from `allow_from` CIDRs, by both the firewall and the unit's IP filter. The live LXC bridge subnet is admitted automatically. | R3, H 7.21, H 7.36     |
| C3  | A team's `server_name` never changes once the team exists; a change is refused by name before anything is touched.                                                                                                                                              | R4, H 7.08             |
| C4  | Each homeserver runs under its own kernel identity: no homeserver can read another team's files, its own team's admin credential, or any host secret it does not need.                                                                                          | R5 (strengthened), new |
| C5  | Hardened units: no resolver, no D-Bus, no home, private devices and PIDs, strict system protection, `Type=notify` with a watchdog, `Restart=always` with no start limit, dependency on each listen interface.                                                   | R5, H 7.22, H 7.23     |
| C6  | The homeserver restarts only when its config, its binary or its unit text changes; a converged apply restarts nothing and prints nothing.                                                                                                                       | R2, H 7.12             |
| C7  | The resident cost of every homeserver, unit, timer and port is stated per team and measured on the host.                                                                                                                                                        | FRAME                  |

### 2.2 Programs, privilege and locks

| ID  | Requirement                                                                                                                                                                                                 | From                        |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------- |
| C8  | Every host tool is Go, built from this repository, installed root-owned and not writable by the owner or by any service user. No bash wrapper, no Python, no zipapp on the host for v2.                     | BUILD-BRIEF, CLAUDE.md, R49 |
| C9  | Root does only what needs root: system files, units, firewall, interfaces, service users, LXC entry, reading root-only state. Homeserver calls run as `<v2-ctl>`, never as root.                            | DESIGN 6.1                  |
| C10 | Every change to a team's accounts, roles or humans needs the owner's password at the time (password sudo). A caller that used sudo drops the cached credential before it ends, and a failing drop stops it. | R13, DESIGN 6.2, H 7.28     |
| C11 | No read runs as root or through sudo. The owner's board reads files only.                                                                                                                                   | R35, DESIGN 12 (B1), new    |
| C12 | One set of host locks serialises every mutating host operation: the play, the timers and ccy-teams' direct calls all take them. A wait is bounded and a refusal names the holder.                           | I-B16, DESIGN 7, R34        |
| C13 | A secret (admin password or token, registration secret, member token, human password) never appears in argv, environment, a log, an error text or a world- or group-readable file.                          | R11, M3, M9                 |

### 2.3 Admin operations

| ID  | Requirement                                                                                                                                                                                                                                             | From                     |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------ |
| C14 | Bootstrap asserts exactly one server admin on every apply and survives a kill at any point: the admin password is stored before the account is registered and deleted only after the token is stored.                                                   | R40, H 7.02              |
| C15 | Rooms, their state and their power levels are converged to area A's layout on every apply and after every roster change: compare first, write only what differs, kick before invite, and remove a human from every room before any power-level rewrite. | S11 5.1, R40             |
| C16 | A room created but not recorded (a crash between create and record) is adopted, never duplicated.                                                                                                                                                       | S11 5.1, M35             |
| C17 | At most one coordinator per team at every instant: every role change demotes before it promotes, across every member kind.                                                                                                                              | R10, L 1.3               |
| C18 | Handles are `<repo>.<seat>+<host>.<type>`, never the machine hostname. Seat numbers per `<repo>+<host>.<type>` never go down. A parked handle returns with a new token and its account. A retired handle never returns.                                 | R9, P5                   |
| C19 | Park revokes (password reset with device logout, the one revoke the homeserver has) before it marks. Retire kicks from every room, deactivates, and records the handle as never to return.                                                              | R11, H 7.10, M29         |
| C20 | An admin operation that dies part-way is completed by the next run of the same operation (roll forward from a recorded intent), never left half-done and never silently re-minting a credential.                                                        | new (fixes v1 undo gaps) |
| C21 | A lost bundle is recovered by one explicit operation that revokes the lost token and issues a new one for the same handle.                                                                                                                              | L 6 item 8, new          |
| C22 | Humans are declared; a human's password is printed once to the owner and never stored; a human's device count and newest sign-in are visible on the board.                                                                                              | R11, R35, DESIGN B2      |

### 2.4 Seats and bundles

| ID  | Requirement                                                                                                                                                                             | From                                              |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| C23 | One account and one token per seat; two seats never share either.                                                                                                                       | S20                                               |
| C24 | A seat's token is placed by the user who will hold it, by file descriptor, with no-follow and exclusive create, mode 0600, never by root under a user's path.                           | R12, H 7.31-7.33                                  |
| C25 | A seat's credential is never inside a git checkout and is visible only to the session that holds the seat, not to sibling sessions sharing the checkout.                                | new (fixes 00203 D6, S11 security note, 00194 R4) |
| C26 | Seat allocation is serialised per checkout by a lock no container can reach; a seat a running session's desired membership names counts as held; launching into a held seat is refused. | R14, 00203 H20/H21                                |
| C27 | The host can tell whether a seat is held (the member's seat lock) without entering any container.                                                                                       | R27, H7 (DESIGN 16)                               |

### 2.5 Declarations, the play, LXC

| ID  | Requirement                                                                                                                                                                                                                             | From                  |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------- |
| C28 | host_vars stays the IaC store; the play is the one route that turns a declaration into a running team. v2 keys are its own and v1's keys are never read or written by v2.                                                               | D6, R2, FRAME         |
| C29 | Every change that runs a play or writes host_vars takes the play lock first and writes host_vars only once it holds it.                                                                                                                 | R34                   |
| C30 | The declaration has one truth per fact: the team's coordinator is declared in one place, not in both a roster and a member entry.                                                                                                       | L 6 item 6, new       |
| C31 | LXC members are applied by the play from the host. The container's checkout and accounts are read from the host side with no-follow walks and size caps. No container program runs as root, and no container needs Python.              | R33, R48, L 6 item 10 |
| C32 | A stopped LXC container is ordinary (its member held back and recorded); only a missing container, or a failure in a running one, fails the apply. Everything checkable is checked for every running container before anything changes. | R33, R48, L 1.5       |
| C33 | Kits are versioned directories resolved once at launch, never replaced in place under a running session.                                                                                                                                | R49, DESIGN B9        |

### 2.6 Element, health, backups, the board

| ID  | Requirement                                                                                                                                                                                                                                             | From                |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- |
| C34 | Element profiles per team, locked down, written as the desktop user; refused for a user who holds v2 member bundles unless the owner has recorded consent.                                                                                              | R50, R37            |
| C35 | Health: a minutely probe of every team; a homeserver missing three probes is restarted; systemd restarts are noted; backups are checked; room state drift (A's layout) is reported with its repair. Records expire so a stale one is itself the signal. | R7, S11 5.4         |
| C36 | Daily backups of every team's database and control state as one numbered pair; a backup before any apply that would restart a running team; restore validated before anything stops.                                                                    | R6, H 7.14-7.16     |
| C37 | A failed restore leaves the team as it was before the restore, not stopped.                                                                                                                                                                             | new (fixes I-B8)    |
| C38 | The applied stamp lets the board say up to date, out of date, not built, or decide, from files and in-process digests only.                                                                                                                             | R8, D65             |
| C39 | ccy-teams opens in about 1 s and redraws in under 200 ms with any number of teams, checkouts and people: one declaration parse, a fixed number of file reads per team, one systemd query, no sudo, no Python, no per-item process.                      | R35, R36, GC 5, D13 |
| C40 | Nothing v2 installs touches anything v1 uses, and a v2 install with no teams runs no resident process.                                                                                                                                                  | FRAME               |

## 3. Design

### 3.1 Who runs what

| Identity                  | Runs                                                                                                                      | Holds                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| root                      | `<v2-host>` root verbs: install, team apply/remove, backup, restore, health, LXC apply, and the entry of every admin verb | root records, backup tars, rendered homeserver configs (with their secret) |
| `<v2-ctl>` (system user)  | `<v2-host> ctl-child`, started only by root, for every homeserver call                                                    | each team's admin token, roster and room records                           |
| one dynamic uid per team  | that team's homeserver (`DynamicUser=yes`)                                                                                | that team's database only                                                  |
| the owner (desktop user)  | ccy-teams, seat placement, Element profiles, `<v2-host> status`                                                           | the seat pool and session seat directories, Element profiles               |
| an LXC guest's agent user | `<v2-bin>` (area B), and `<v2-bin> guest` operations started by the host through `lxc-attach -u`                          | its own bundles in its home                                                |

Nothing in this table is resident on the host except the homeservers. The health and backup
timers start a short-lived process. There is no daemon, no socket and no reader with a sudoers
rule (decision DC4).

The flows the owner sees, each in one line:

- **Apply** (play, or ccy-teams running the play): render the declaration, `<v2-host> team apply`
  per team as root, which writes system files, starts the homeserver, and runs bootstrap in a
  `<v2-ctl>` child.
- **Change** (ccy-teams, launcher): `sudo <v2-host> member add|park|...` with the owner's password;
  root takes the team lock, runs the change in a `<v2-ctl>` child, prints a typed JSON answer; the
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

The resident memory is the real cost of the chosen design, about 100-135 MB per team. With the
three or so teams a host runs, that is a few hundred megabytes on a desktop with tens of gigabytes.
Everything else in the table favours one homeserver per team. The memory is also tunable: Tuwunel
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

Files of one team (paths relative to `<v2-state>`; D names the root):

| Path                                  | Owner and mode       | Content                                                                                                                            |
| ------------------------------------- | -------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `hs/<team>/` (systemd StateDirectory) | dynamic uid, 0700    | `db/`, `backups/` (Tuwunel's own backup engine output)                                                                             |
| `root/<team>/config.toml`             | root, 0600           | the rendered Tuwunel config, including the registration shared secret; reaches the unit only through `LoadCredential=`             |
| `root/<team>/team.json`               | root, 0600           | the rendered team declaration last applied (canonical JSON); `server_name` is checked against it (C3)                              |
| `root/<team>/firewall.json`           | root, 0600           | the firewall rules this team added, written before any firewall change (H 7.11)                                                    |
| `root/<team>/units.json`              | root, 0600           | digests of the base unit, drop-in and binary last started, to decide a restart (C6)                                                |
| `root/<team>/backups/<n>.tar`         | root, 0600, dir 0700 | the control state paired with database backup `<n>` (3.12)                                                                         |
| `ctl/<team>/`                         | `<v2-ctl>`, 0700     | `admin.token` 0600, `admin.password` 0600 (only during bootstrap), `roster.json` 0600, `rooms.json` 0600, `intent.json` 0600 (3.6) |
| `ctl/<team>/registration.secret`      | `<v2-ctl>`, 0600     | the registration shared secret, for registering the admin account; root renders the same value into the config                     |

`<v2-state>/root` is 0700 root and `<v2-state>/ctl` is 0700 `<v2-ctl>`. Root writes under
`ctl/` only through the `<v2-ctl>` child, never directly, so the control user cannot steer a root
write (the same reasoning that kept v1's root records out of the homeserver user's tree, H 5).

The registration secret is created once by root at the first apply of a team (64 random bytes as
128 lowercase hex, no newline), written to `root/<team>/` as part of the config and handed to the
`<v2-ctl>` child on a pipe, which writes its own copy. Two runs cannot both create it: creation is
link-not-replace (H 7.03), and the team lock (3.4) already excludes a second run.

#### 3.2.3 The unit

The template `<v2-hs@>` (installed by `software`, 0644 root):

```ini
[Unit]
Description=v2 team homeserver %i
StartLimitIntervalSec=0

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
StateDirectory=<v2-hs-state>/%i
StateDirectoryMode=0700
LoadCredential=config.toml:<v2-state>/root/%i/config.toml
Environment=TUWUNEL_CONFIG=%d/config.toml
ExecStart=<v2-lib>/tuwunel
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
BindReadOnlyPaths=<v2-share>/resolv.conf:/run/systemd/resolve/stub-resolv.conf
BindReadOnlyPaths=<v2-share>/resolv.conf:/run/systemd/resolve/resolv.conf
InaccessiblePaths=-/run/dbus
```

The per-team drop-in `<v2-hs@><team>.service.d/network.conf`, rendered by `team apply`:

```ini
[Unit]
After=sys-subsystem-net-devices-<iface>.device
Wants=sys-subsystem-net-devices-<iface>.device

[Service]
MemoryHigh=<from H-C1>
IPAddressAllow=127.0.0.1/32 ::1/128 <each listen /32 or /128> <each allow_from>
SocketBindAllow=tcp:<port>
```

What each choice is for, beyond v1's reasons (H 5, H 7.22-7.23), which all still apply:

- `DynamicUser` and `StateDirectory` replace the shared user, `TemporaryFileSystem` over the
  state root and `BindPaths` (C4).
- `LoadCredential` puts the config, with its secret, in a directory only this unit's uid can read,
  from a root-only file. The homeserver cannot rewrite its own config, which in v1 sat beside its
  database and was readable by it.
- `CapabilityBoundingSet=` empty, `SystemCallFilter=@system-service`, `ProtectProc=invisible` and
  the rest are ordinary systemd hardening v1 did not have. Each is checked by H-C2 against a real
  Tuwunel start (a Rust binary built with jemalloc can trip `MemoryDenyWriteExecute`, so that
  one is left out on purpose).
- The base unit denies all addresses and binds, so a missing drop-in leaves a homeserver that
  cannot be reached rather than one that can be reached from anywhere.

#### 3.2.4 The config

Rendered by `team apply` from the declaration, in Go, as bare top-level keys (H 7.05, a file with
any bare key is read whole as the global section) plus the `[rate_limiting.login.*]` tables. The
key set is v1's (digest 1, section 2.2), kept because each key has a recorded reason:
registration closed, federation off, `trusted_servers = []`, encryption off, room creation by the
admin only, invites not auto-accepted, `login_via_token` and existing-session login off, warn-level
logging with no headers, the login limits, `max_request_size = 10000000` (Tuwunel's floor),
`rocksdb_allow_fallocate = false` on btrfs, `enable_set_displayname = false`,
`admin_signal_execute` set to the backup command, and `error_on_unknown_config_opts = true` so a
key this Tuwunel does not know stops it at start. Added in v2: `database_backup_path` inside the
state directory, `cache_capacity_modifier` from H-C1, and `unix_socket_path` is not used (members
in containers need TCP, and one transport is enough).

The renderer is tested against a vendored list of the pinned Tuwunel version's config keys (v1 kept
one for 1.9.4); a pin bump that drops or renames a key fails the test before the host sees it.

#### 3.2.5 Network

- Each team has one `<port>` from area D's v2 range. `team apply` refuses a port another v2 team
  holds (from the root records), a port anything is listening on while this team's unit is
  inactive, and a port in v1's records (read, never written, so v2 cannot collide with a v1 team
  that is stopped).
- Listen addresses: loopback plus each declared `listen` address, each of which must be on exactly
  one interface. After start, the bound address set is asserted equal to that list before
  bootstrap (H 7.21).
- v2 has its own bus address interface `<v2-if>` (a NetworkManager dummy connection with one
  address), so retiring v1's interface later cannot break a v2 team. Its address comes from the
  declaration (`<v2>_address`).
- Firewall: per-team firewalld rich rules, one per `allow_from` CIDR, in the zone of the interface
  that routes that CIDR (the default zone if none). The rule set is recorded in
  `root/<team>/firewall.json` before any firewall change, and only recorded rules are ever removed
  (H 7.11). Rules are tested for presence with `firewall-cmd --query-rich-rule` by exit status,
  never by reading text (I-B12). Decision DC12 holds the D-Bus alternative.
- The live LXC bridge subnet is added to every team's `allow_from` at render time, computed
  in-process from the bridge interface, as v1's `bridge.py` did (R3).

### 3.3 The host program

One Go binary, `<v2-host>`, built from `src-go/cmd/<v2-host>` by the play as the login user and
installed by root, 0755 root:root, in a root-owned directory (C8). The first thing `main` does is
decide the caller class from the real and effective uid, and each verb belongs to exactly one
class. A verb asked of the wrong class exits 77 naming the class it needs (DESIGN 3.1).

| Caller class | How it is reached                                              | Verbs                                                                                                                                                                                                                    |
| ------------ | -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| root         | the play (`become`), the two timers, the owner's password sudo | `declare check`, `team apply`, `team remove`, `member add\|return\|park\|retire\|reissue`, `role apply`, `human password\|devices\|logout\|lock\|unlock`, `backup`, `backups`, `restore`, `health`, `lxc apply`, `check` |
| `<v2-ctl>`   | only `ctl-child`, started by root with `os/exec` `Credential`  | `ctl-child` (reads one typed request on stdin, answers one typed response on stdout)                                                                                                                                     |
| anyone else  | the owner, the Element play                                    | `element check\|apply`, `status`, `version`                                                                                                                                                                              |

`version` answers for every class and prints the binary's source digest (the scheme of Plan 00192:
`go.mod` plus every non-test file of the command and the module packages it imports, baked in with
`-ldflags -X`). The board compares it with the digest of the checkout (3.13).

Why one binary rather than one per privilege: the root verbs and the `<v2-ctl>` child share almost
every package (declaration types, roster, rooms engine, Matrix client), one binary has one digest
to compare and one install step, and the caller-class check is a single table tested once. The
reason v1's design had a separate reader (a NOPASSWD sudoers rule needs a narrow target) is gone
with DC4. Decision DC3 holds the alternative.

The root and `<v2-ctl>` halves talk only over a pipe in typed JSON:

```json
{"v": 1, "op": "member.add", "team": "<team>", "args": {"prefix": "<repo>+<host>.<type>", "seat": "3", "human_text": true}}
```

```json
{"v": 1, "ok": true, "changed": ["member <handle> added"], "bundle": {"...": "see 3.7"}}
```

Root decodes the answer strictly (unknown or duplicate keys refused, sizes capped), because it is
the one thing root parses that a less-privileged process wrote. The child starts with no
supplementary groups, `chdir /`, an empty environment except `PATH`, and the request on stdin; it
refuses arguments other than `ctl-child`. No other caller can become `<v2-ctl>`: it has no shell,
no password and no sudoers entry.

Exit codes are one table for every verb: 0 done, 1 failed, 64 usage, 69 homeserver unreachable,
70 refused (a rule said no), 75 busy (a lock held, the holder named), 77 wrong caller class, 78
configuration. A child process's exit code is never passed through as one of these (I-B3); its
failure is 1 with a message. Output contract: stdout carries only the payload (`CHANGED<TAB><what>`
lines for root verbs the play runs, one JSON document for verbs ccy-teams runs), everything else
goes to stderr as `<v2-host>: <message>`.

### 3.4 Locks

| Lock                           | Kind and place                                     | Taken by                                                                                                                                                                          |
| ------------------------------ | -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| play lock                      | owner, `flock`, owner runtime directory            | every play run and every host_vars write (C29). It is the repository's lock for every play, not part of v1's bus, so v2 using it touches nothing of v1. Root verbs never take it. |
| team lock                      | root, `flock` on `<v2-run>/team-<team>.lock`, 0600 | exclusive by every root verb that changes or restarts one team: apply, remove, member, role, human, backup, restore, the health restart and status write, LXC apply per team      |
| allocation lock (per checkout) | owner, `flock`, owner runtime directory            | ccy-teams around choosing and taking a seat in one checkout (C26)                                                                                                                 |
| seat lock                      | member, `flock`, in the seat directory             | the member's keeper while it holds the seat (area B); the host only probes it                                                                                                     |

Rules:

- There is no host-wide lock, because nothing host-wide changes outside the play's static tasks
  (3.5.1), and those swap every file atomically: a new binary is a new versioned file and a link
  swap, so a verb already running keeps its open file and the next one starts on the new one.
- Order: play lock, then allocation lock, then (inside root) team locks in team name order. A
  verb that touches several teams (LXC apply, the timers) takes each team's lock in turn, never
  two at once, except where one change spans two teams, which none does today. Root never takes
  an owner lock, and an owner process never takes a root lock, so the two halves cannot deadlock.
- Every root lock is `flock(2)` on a file that is never deleted, opened `O_NOFOLLOW|O_CLOEXEC` in a
  0700 root directory under `/run`, so a crashed holder frees it and no container can reach it.
  The holder writes one line into the file: `<verb> <team> pid=<n> since=<ms>`. A waiter that gives
  up prints that line and exits 75.
- Waits are bounded and depend on the caller: a seat take from a launcher waits up to 60 s; a play
  step and the backup timer wait up to 10 minutes; the health timer never waits (below).
- The health tick takes the team lock without waiting. If another verb holds it, the tick neither
  probes for a restart nor rewrites the status record for that team; it notes `busy: <holder>` in
  its own journal line and exits 0. This is not a skipped failure: the holder is the one
  restarting or changing that team and checks readiness itself, and the status record the board
  reads carries its own expiry (3.11), so a holder that never lets go shows on the board as a
  stale record within five minutes.
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
needs ordering against readiness, typed validation or Matrix calls is `<v2-host>`.

#### 3.5.1 The play's static tasks

The v2 play (`play-<v2>.yml`, area D names it; its own file, so v1's play is untouched) runs, as
root, in this order:

01. `<v2-host> declare check` with the Ansible-parsed `<v2>_teams`, `<v2>_lxc_members` and
    `<v2>_element_teams` as JSON on stdin (3.8). Nothing else runs if it refuses.
02. Packages: `firewalld`, `NetworkManager`, `zstd`, `lxc` only if any LXC member is declared.
    Nothing else: no Python, no `curl`, no `jq`, no `tcpdump` (v1 needed each for its bash or its
    acceptance).
03. The `<v2-ctl>` system user (no home, `nologin`), the `<v2-state>` tree (`root/` 0700 root,
    `ctl/` 0700 `<v2-ctl>`), `<v2-applied>` 0750 root:`<owner group>`, `<v2-etc>/owner-group`
    (root 0644, one group name from `getent` of the desktop user, read by every writer of an
    owner-readable record; no default, as v1's stamp-group file, H 7.18).
04. Tuwunel: `get_url` of the pinned release asset with `checksum: sha256:<pin>` into a root-only
    staging directory, `zstd -d` with `creates:`, install as `<v2-lib>/tuwunel-<version>` 0755, then
    an atomic link swap of `<v2-lib>/tuwunel`. The pin lives in `vars/version-pins.yml` beside every
    other pin, so the `update-versions` skill sees it. The release is unsigned, so the pin is the
    supply-chain check, as in v1.
05. `<v2-host>`: built by `tasks/build-go-command.yml` as the login user (no toolchain runs as
    root), installed by root as `<v2-lib>/<v2-host>-<digest>` 0755 root, link swapped. The member
    program `<v2-bin>` (area B) is built and staged the same way, under `<v2-kit>/<digest>/`
    with a `current` link (C33).
06. Units: `<v2-hs@>.service`, `<v2-health>.{service,timer}`, `<v2-backup>.{service,timer}`, the
    resolver stub, all 0644 root; one `daemon_reload` if any changed. The two timers are enabled
    only while at least one team is present, so a v2 install with no teams starts nothing (C40).
07. The bus address: a NetworkManager dummy connection `<v2-if>` with `<v2>_address`, if declared.
08. `<v2-applied>/installed.json` (0640 root:`<owner group>`): the digest of the installed
    `<v2-host>`, the Tuwunel version and the kit digest, which the board reads (3.13).
09. For each absent team: `<v2-host> team remove`. Then `<v2-host> lxc apply --phase absent`.
10. For each present team: `<v2-host> team apply` with the team's rendered declaration on stdin.
11. `<v2-host> lxc apply --phase present` with the LXC member declaration on stdin.

Every `<v2-host>` step reports changed only on a `^CHANGED\t` line. `<v2>_only_teams`
(D names it) scopes steps 9-11 to the teams ccy-teams is changing, as v1's `agent_bus_only_teams`
does. No step supports `--check` except the first, which is read-only.

There is no sudoers file in v2 (DC4), and there is no v2 change to the ccy image except what area
B needs for the member program and the seat mount (3.7).

#### 3.5.2 `team apply`

Input: one team's declaration as JSON on stdin, decoded strictly against the same Go type the
declaration check uses. Steps, each failing the verb with the step named:

01. Root, Fedora, input valid, the team lock (wait up to 10 minutes).
02. Refusals before any change: a `server_name` different from `root/<team>/team.json` (C3); a port
    held by another v2 team, by a v1 team's record, or by any listener while this unit is inactive;
    a `listen` address on no interface or on more than one; a wildcard `listen`.
03. Render in memory: `config.toml`, the drop-in, the canonical `team.json`. Compare with what is
    installed (`units.json` digests of the base unit text, the drop-in, the binary link target and
    the config). The answer is one of: nothing to do; write without restart (`team.json` only, for
    example a description change); write and restart; first start.
04. First apply only: create the registration secret (3.2.2) and the `root/<team>/` and `ctl/<team>/`
    directories.
05. Firewall: write the union of the old and new rule sets to `firewall.json`, then add the new
    rules (permanent and runtime, no reload), then remove the old ones no longer wanted, then write
    the new set alone (H 7.11).
06. If the answer is "restart" and the homeserver is active: back the team up first (3.12). A failed
    backup stops the apply before anything restarts (R6).
07. Write the config and drop-in (atomic, explicit modes), `daemon-reload` if the drop-in changed,
    start or restart the unit, enable it.
08. Wait ready: the unit must be `active` and `/_tuwunel/server_version` must answer, within 60 s.
    An inactive or failed unit fails at once with the journal tail printed only as explanation; the
    verdict is the unit state, never a journal scan (H 7.13).
09. Assert the bound address set is exactly loopback plus `listen` (H 7.21).
10. Bootstrap in the `<v2-ctl>` child (3.6.2).
11. Write `team.json` and `units.json`, then the stamp `<v2-applied>/teams/<team>/stamp.json` last,
    so a stamp always means fully applied (H 6). Then refresh the status record (3.11).

The stamp:

```json
{"v": 1, "team": "<team>", "config": "<sha256 of canonical team.json>", "software": "<v2-host digest>", "tuwunel": "<version>", "applied_ms": 0}
```

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
`<v2-ctl>` child under the team lock, with the atomic writer of `bus/state` (temp file beside the
target, `O_EXCL|O_NOFOLLOW`, `fchmod`, fsync, rename, directory fsync; S15). It is decoded strictly
(unknown or duplicate keys refused, sizes capped, exit 78 naming the file).

```json
{
  "v": 1,
  "team": "<team>",
  "counters": {"<repo>+<host>.<type>": 3},
  "members": {
    "<handle>": {"state": "active", "kind": "podman", "human_text": true, "since_ms": 0, "token_ms": 0}
  },
  "humans": {"<name>": {"state": "active", "since_ms": 0}},
  "coordinator": "<handle>",
  "pending": null
}
```

- `state` of a member: `active`, `parked`, `retired`, or the intent states below. A `retired`
  member stays in the roster for ever, so its handle can never be issued again without asking the
  homeserver (C18).
- `counters` never go down, even when an account creation fails (R9).
- `coordinator` is the role last applied; the truth is the declaration (3.8, DC6).
- `pending` is the one operation in progress, if any: `{"op": "member.add", "handle": "<h>", "step": "account", "since_ms": 0}`. It is written before the first homeserver call of an
  operation and cleared after the last.

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
4. **Rooms.** Area A's layout, given as a typed value by A's package from the declaration and the
   roster: `Desired(team, roster) []RoomSpec`, each spec naming a purpose, create parameters,
   state events (type, state key, content), the members wanted (join or invite) and the members
   that must not be there. The engine converges every spec:
   1. Find the room: its ID in `rooms.json`; if absent, adopt one the admin has joined whose
      admin-written back-link names this purpose (M35, S11 5.1); if none, write the intent into
      `pending`, create it with its initial state, record the ID, clear `pending`. More than one
      candidate is refused, naming them.
   2. Every removal in every room first: kick each member or human that must not be there, with
      the reason A gives. This includes a removed human from every room before any power-level
      rewrite (C15).
   3. Then every state event whose canonical content differs.
   4. Then every invite.
      The engine knows nothing about what a room is for. A owns the specs, and C owns that they are
      applied idempotently, in this order, and that a failure names the room and the step.
5. **Roles.** If the declared coordinator differs from the roster's, run the role change (3.6.4).

Every change appends a `CHANGED` line, so a converged team prints nothing.

#### 3.6.3 Members

Every member verb runs as root under the team lock, does its homeserver work in the `<v2-ctl>`
child, refreshes the status record at the end, and answers one JSON document on stdout. Verbs and
their state machine:

| Verb                                            | From state         | Steps (each recorded in `pending` before it runs)                                                       | To state  |
| ----------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------- | --------- |
| `member add --prefix P --seat S\|next --kind K` | (none)             | claim the handle in the roster (counter raised); create the account; mint a token; invite per A's specs | `active`  |
| `member return --handle H`                      | `parked`           | revoke (password reset with device logout); mint; re-invite per A                                       | `active`  |
| `member reissue --handle H`                     | `active`           | revoke; mint (the lost-bundle recovery, C21)                                                            | `active`  |
| `member park --handle H`                        | `active`           | revoke; mark                                                                                            | `parked`  |
| `member retire --handle H`                      | `active`, `parked` | kick from every room; deactivate; mark                                                                  | `retired` |

- **Roll forward, never undo** (C20). If an operation dies, `pending` names it and its step. Any
  later verb on that team first completes the pending operation from its step, because every step
  is idempotent: an account that already exists is not created again, a revoke can always be
  repeated, an invite to a member already invited is skipped. The same verb asked again for the
  same handle (the caller retrying) completes it and prints the bundle, minting after a revoke.
  Any other verb completes a pending `add`, `return` or `reissue` to `active` with its token
  revoked and prints no bundle, and the board shows that member as needing `reissue`. So a
  token is printed only to a caller that asked for that handle, and v1's two gaps close: a failed
  account creation that rolled the member back but kept the counter, and an interrupted mint
  that left a valid token nobody held.
- **Mint is always after revoke**, except for a brand-new account, so at most one token is valid
  per member at any time (C23).
- `add` refuses a handle that is `active` (one place at a time, naming `reissue`), `parked` (naming
  `return`), or `retired` (never returns). `--seat next` takes the lowest number no active or parked
  member of that prefix holds (parked ones keep their number for their return).
- `retire` refuses the declared coordinator: the declaration has to change first (C30).
- Park keeps the account joined to its rooms (its history stays attributed) and keeps its role in
  the roster; parking the coordinator is allowed and the board says the coordinator is parked.

The answer of `add`, `return` and `reissue` carries the bundle (3.7); every other answer carries only
`changed`.

#### 3.6.4 Roles

There is one coordinator at most, and it is named in the declaration (DC6). `role apply --team T`
(run by `team apply`, and directly by ccy-teams with the team's declaration on stdin after it has
written host_vars under the play lock) converges the roster to it:

1. If the old coordinator is not the new one: demote the old (A's team record and its room specs
   say what changes, for example removal from the questions room) and apply it fully.
2. Only then promote the new one and apply it.

The demotion completes before the promotion starts, across every member kind (C17). A declared
coordinator that is not an `active` or `parked` handle in the roster is refused before any change;
a coordinator whose bundle says `human_text: false` is refused where A's rules say so, at the
place the role is assigned (S11 4.5).

The hot path is therefore the same convergence as the play's, restricted to one team's roles, so it
cannot diverge from what the next play would apply (the v1 fragility where a live role change was
reverted by the next apply, L 6 item 6).

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

v2 moves seats out of the checkout (C25, decision DC5):

| Path (under `<v2-seats>`, owner 0700) | Holds                                                                                                                          |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `pool/<checkout key>/checkout.json`   | the checkout's real path, so the pool can be listed per checkout (`<checkout key>` = first 32 hex of SHA-256 of the real path) |
| `pool/<checkout key>/<team>/<seat>/`  | a free seat: its bundle and its member state, kept for the next session in that checkout                                       |
| `sessions/<session>/<team>/`          | a seat held by that session (live, or stopped and restorable): the same directory, moved here                                  |

- A ccy session's container gets `sessions/<session>/` bind-mounted read-write at one fixed path
  (area D names it), and nothing else of `<v2-seats>`. A sibling session sees only its own seats.
- A host `cc` session reads its session directory directly; it runs as the owner and has no
  isolation to gain.
- Taking a seat: under the checkout's allocation lock, the owner process `renameat2(..., RENAME_NOREPLACE)`s the seat directory from the pool into the session directory. Joining a team
  mid-session (hot control) is the same rename into a directory the container already has
  mounted, so no new mount is needed in a running container.
- Releasing a seat (the session's farewell, a remove, or a session found dead and not
  restorable): only once the seat lock is free, the same rename back into the pool. After every
  move the host checks the top level of the moved directory: a real directory owned by the owner,
  with `seat.json` and `token` regular files. Anything else is refused and left in place, named on
  the board. A container cannot redirect a rename (rename never follows the destination) and
  cannot plant a destination (`RENAME_NOREPLACE`).
- "Held" now has one meaning: in a session directory whose session is live (its seat lock held) or
  restorable (its session record exists). This replaces v1's two rules (seat lock held, or named by
  a running session's desired membership; Plan 00203 H21).
- LXC members keep their seats inside the guest, in the agent user's own home (3.9), where only
  that user can read them.

#### 3.7.2 What a seat receives

C writes two files into a seat directory; everything else in it (member state, the seat lock) is
area B's.

`token`: the access token alone, printable ASCII, no newline, at most 4096 bytes, mode 0600 (S19).

`seat.json`, mode 0600, written last so its presence means the bundle is complete:

```json
{
  "v": 1,
  "team": "<team>",
  "server_name": "<server_name>",
  "user_id": "@<handle>:<server_name>",
  "admin_id": "@admin:<server_name>",
  "base_url": "http://<address>:<port>",
  "plain_http_hosts": ["<address>"],
  "human_text": true,
  "issued_ms": 0,
  "issuer": "<v2-host digest>"
}
```

- `base_url` is the team's member URL from the declaration (default: `<v2>_address` and the port).
  An `http://` URL is only ever to an IP literal listed in `plain_http_hosts` (M14).
- Room IDs, the team record, limits and trust settings are not in the bundle by default: a member
  reads them from the main room, where the admin keeps them current, so a change never requires
  re-issuing a bundle (the Plan 00211 reasoning). Area A decides; if A wants the main room's ID in
  the bundle (to avoid a lookup at start), it is one more key here.
- v1's `README` file is dropped: it was text for a human that the member never read. `<v2-bin>`
  explains a seat on request (area B).
- Each seat gets its own account and token; two seats never share either (C23).

#### 3.7.3 How it is placed

- The answer of `member add|return|reissue` carries `seat.json`'s content and the token in one JSON
  document on root's stdout. The token passes from the `<v2-ctl>` child to root to the owner's
  process over pipes only; it is never in argv, environment, a log or an error (C13).
- The owner's process (ccy-teams, with `bus/seat`) places it into a seat directory it claimed
  before calling sudo (a fresh `mkdir`; one that appeared meanwhile is refused, so two racing
  launches cannot both reach the homeserver). It opens the directory, then creates `token` and then
  `seat.json` with `openat(O_CREAT|O_EXCL|O_NOFOLLOW)`, mode 0600, `fsync`, then `fsync`s the
  directory (C24). Root never writes here.
- A failure after the sudo call and before `seat.json` leaves a claimed directory without a
  bundle. The next take names it and offers `reissue`, which revokes whatever token was minted
  and issues a new one. The bundle is never silently re-minted (L 6 item 8).
- For an LXC member, placement is the same code inside the guest: `<v2-bin> guest place` run as the
  agent user, reading the same JSON on stdin (3.9).

#### 3.7.4 Seat choice and handles

Unchanged in substance from v1 (R9, R14; digest 1, section 2.10), now computed in-process by
ccy-teams from the pool, the session directories and the status record's roster:

- Handle `<repo>.<seat>+<host>.<type>`: `<repo>` from the checkout's `origin` (last path component
  without `.git`, normalised to `[a-z0-9_-]`, at most 48), else the directory name; `<host>` from
  `HOOKS_DAEMON_HOSTNAME` in the checkout's `.claude/ccy/ccy.env.local`, parsed and never sourced,
  else `local`, never the machine hostname; `<type>` one of `podman lxc docker vm host` (P5).
- The offered seat is the lowest number that is free in this checkout and held by no active member
  of the same prefix; a parked handle's number is offered for its return first.
- `seat take` waits, bounded, for a homeserver that is still starting (up to 180 s, after a boot),
  and tells "built but stopped" from "not a team" with different messages naming `ccy-teams apply`
  or `create` (R14).
