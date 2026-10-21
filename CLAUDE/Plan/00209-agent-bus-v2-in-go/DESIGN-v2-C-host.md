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
| Units                                    | one service per team (the timers are host-wide, 3.12)                               | one service                                                                                                                            |
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
| `root/<team>/backups/<n>.tar`         | root, 0600, dir 0700 | the control state paired with database backup `<n>` (3.13)                                                                         |
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

| Caller class | How it is reached                                              | Verbs                                                                                                                                                                                                               |
| ------------ | -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| root         | the play (`become`), the two timers, the owner's password sudo | `software`, `team apply`, `team remove`, `member add\|return\|park\|retire\|reissue`, `role apply`, `human password\|devices\|logout\|lock\|unlock`, `backup`, `backups`, `restore`, `health`, `lxc apply`, `check` |
| `<v2-ctl>`   | only `ctl-child`, started by root with `os/exec` `Credential`  | `ctl-child` (reads one typed request on stdin, answers one typed response on stdout)                                                                                                                                |
| anyone else  | the owner, the Element play                                    | `element check\|apply`, `status`, `version`                                                                                                                                                                         |

`version` answers for every class and prints the binary's source digest (the scheme of Plan 00192:
`go.mod` plus every non-test file of the command and the module packages it imports, baked in with
`-ldflags -X`). The board compares it with the digest of the checkout (3.14).

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

| Lock                           | Kind and place                               | Taken by                                                                                                                                                                     |
| ------------------------------ | -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| play lock (v1's, unchanged)    | owner, `flock`, owner runtime directory      | every play run and every host_vars write (C29). Root verbs never take it.                                                                                                    |
| host lock                      | root, `flock` on `<v2-run>/host.lock`, 0600  | shared by every team-scoped root verb; exclusive by `software`                                                                                                               |
| team lock                      | root, `flock` on `<v2-run>/team-<team>.lock` | exclusive by every root verb that changes or restarts one team: apply, remove, member, role, human, backup, restore, the health restart and status write, LXC apply per team |
| allocation lock (per checkout) | owner, `flock`, owner runtime directory      | ccy-teams around choosing and taking a seat in one checkout (C26)                                                                                                            |
| seat lock                      | member, `flock`, in the seat directory       | the member's keeper while it holds the seat (area B); the host only probes it                                                                                                |

Rules:

- Order: play lock, then allocation lock, then (inside root) host lock, then team locks in team
  name order. Nothing ever takes them in another order. Root never takes an owner lock, and an
  owner process never takes a root lock, so the two halves cannot deadlock each other.
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
  reads carries its own expiry (3.12), so a holder that never lets go shows on the board as a
  stale record within five minutes.
- The team lock is held across the admin child's homeserver calls on purpose: it is what makes a
  roster change single-writer. Each call is bounded (M6: 30 s, three tries on 429), so a hold is
  bounded too.
- No lock is held while the owner types a password: ccy-teams makes sudo ready first (the
  `internal/sudo` path), and only then takes the allocation lock and calls root.

Decision DC11 holds the simpler alternative (one host-wide lock for everything).
