# Plan 00161 design: the agent team bus

Design for [fedora-desktop#59](https://github.com/LongTermSupport/fedora-desktop/issues/59),
as changed by the owner's answers (journal 26-10-06, the 13:35 and 13:50 entries). The wire
rules (event types, ping and human-message forms, verbs, reference grammar, validation,
limits, exit codes, output format, the CLI) are in the protocol spec,
[docs/agent-bus-protocol.md](../../../docs/agent-bus-protocol.md), cited below as
`agent-bus-protocol.md §N` (this plan's `PROTOCOL.md` is only a pointer to it); this file
does not repeat them. Evidence for factual claims is in `subagent-reports/261006-research-*.md`
(cited as `[ccy]`, `[services]`, `[python]`, `[tuwunel]`, `[clients]`, `[wake]`), the U01
probe result on branch `wf-f0f65b6e-87f-2-30211d13` (cited as `[U01]`), and an external
spike in another project (cited as `[spike]`, anonymised: Tuwunel 1.9.3 as a plain process,
the admin API with registration closed, an invite-only room with bots at power level 0, a
held `/sync` returning about 31 ms after a send, a SIGUSR2 backup and a restore, 100-135 MB
RSS; and Claude Code's per-session inbox socket). What changed since the reviewed design, and
why, is in `subagent-reports/261006-design-revision-2-opus-5-5.md`; the seats design
(one member per ccy session, owner decisions of 2026-10-07: sections 5.5, 5.6, U20 and
U29-U33) is in `subagent-reports/261007-seats-design-opus.md`, as reworked for the owner's
answers of the same day in `subagent-reports/261007-seats-design-rework-opus.md` and then
for the team given at launch in `subagent-reports/261007-seats-design-launch-team-opus.md`.

**v1 scope: teams on this machine** (owner, 2026-10-07, D46). In v1 a team's homeserver and
every member run on one machine: the bare desktop, ccy, and LXC, docker and VM guests on
that host. A team spanning machines (a member or a homeserver on another host, reached over
WireGuard) is a later phase: the parts of this design that serve it (the `<wg_ip>` paths,
the placement convention's server case, U24, PLAN.md's deferred success criterion) are kept
as the design for that phase and marked **later phase** where they appear; v1 builds and
proves none of them.

Placeholders: `<team>` (team name), `<port>` (the team's homeserver port), `<sn>` (the
team's `server_name`), `<handle>` (an agent's handle), `<role>` (an install's role, the
value of `HOOKS_DAEMON_HOSTNAME`), `<bus_ip>` (the host's bus address, section 3.3),
`<wg_ip>` (an address on a WireGuard interface), `<cidr>` (a source range allowed to connect),
`<seat>` (a seat name, section 5.5), `<checkout>` (a ccy checkout's path on the host).

## 0. Threat model in one paragraph

Every agent holds its own Matrix access token where it, and any code in its repository,
runs arbitrary commands. So `pingbus` is a convenience, not the boundary: whatever pingbus
declines to send or print, the agent can send or read with `curl`. The hard rules sit where
an agent cannot route around them: **the homeserver host** (the team's secrets belong to a
system user no agent runs as, and the kernel lets the homeserver talk only to the sources the
team allows, and to nothing outside), **the receiver's own checks** (an agent acts only on a
ping whose sender holds a role, whose verb and reference are valid, and whose reference
resolves at the forge to content on a trusted branch; and on free text only when its sender
is one of the team's humans named by the homeserver's team record, which only the team
admin can write), and **the network** (the homeserver answers only on the addresses its
team file lists and only to the source ranges it allows). v1 has **no TLS**: on a network
that is not private (not loopback, not a host-only bridge, not WireGuard), logins, passwords
and tokens travel in clear (section 3.3). Agent free text reaches no agent (an agent may
write text to a human, which every agent ignores); human free text reaches the agents it
addresses, marked as that human's. Two holders can therefore instruct agents on every host
in a team: **root on the homeserver host** (and whoever holds its backups), because it can
mint any account; and **anything that can read a human's Matrix session**, because it can
post as that human. The first is the team's trust root and is stated to every member that
joins (section 9; a member may opt out of human text, section 5.1); the second is why no
agent may run as a user that holds a human's session (section 8).

## 1. Shape in one paragraph

A **team** is themed around a project and is set up by humans. Each team has one Tuwunel
homeserver **instance** and one **team room**. The homeserver is a pinned static binary run
as the `agent-bus` system user under a hardened systemd unit, installed by
`agent-bus-install`, a Bash installer for any Fedora desktop or Fedora server; this
repository's play calls the same installer on every desktop, and other projects call it from
their own IaC. A **member** is one agent: its primary repository, the host it runs on, and
its encapsulation (a ccy session, the bare desktop, LXC, docker, a VM, a server). An agent
may be in several teams: it holds one **member bundle** (config and token) per team.
Members run `pingbus`, one standard-library zipapp, which validates on send and on receive
and keeps a local inbox. Agents exchange **pings** (a closed verb plus a reference); humans
in Element, on a desktop or a phone, write **free text addressed to agents** with a mention.
An idle session is woken through Claude Code's per-session inbox socket by a watcher the
session's own hook starts; where the socket is not available, a background `pingbus wait`
does it. A ccy session is in a team only when it is launched with it:
`ccy --team <team> [--seat <name>]`. A plain `ccy` is in no team, in any checkout. A session
launched with a team sits in one **seat** of its checkout: a durable member identity (an
account per team, a handle, a bundle, pingbus state) that the launch creates on the host the
first time it is asked for, and that every later session in that seat continues. Several
sessions in one checkout are distinct members and can ping each other (sections 5.5, 5.6).

## 2. Teams and members

- **Team.** A name (`[a-z][a-z0-9-]{0,23}`), one homeserver instance with a fixed
  `server_name`, one team room, a list of humans, the repositories and branches pings may
  reference, and path prefixes. Teams are declared in a **team file** (JSON, section 3.4)
  that a human writes and the installer applies. No team detail is ever committed to this
  repository: on desktops the team files are rendered from untracked host_vars; elsewhere
  they come from the other project's own IaC.
- **One instance per team, by convention and by the installer.** The instance name is the
  team name. A host may run several teams' instances (each its own unit, port, data and
  secrets). One instance per team keeps each team's accounts, admin, backup and placement
  separate, so a team can move host without touching another.
- **Placement convention** (documented in `docs/agent-bus.md`): a team whose members all run
  on one laptop runs its homeserver on that laptop (v1's only case, D46); **later phase:** a
  team with a member elsewhere (a data-centre server) runs its homeserver beside that
  member, reached by the others over WireGuard. The play installs the software on every desktop; an instance exists only where a
  team file declares it.
- **Member.** One agent, with one account per team it belongs to. Its handle
  (agent-bus-protocol.md §3) encodes repository, seat (a number or a role name, section
  5.5), host and encapsulation type. `<host>` is the
  install's role, `HOOKS_DAEMON_HOSTNAME`, or a name the human passes as `--host`; never a
  real hostname. Handles end up in public forge text (`done` references, journals), so
  `CCY_HOST_HOSTNAME` and the system hostname, which the hooks daemon falls back to, are
  never used: with no role set and no `--host`, `suggest-handle` refuses. `add-member`
  always requires `--host`: it runs through `sudo`, whose `env_reset` drops the role
  variable, so a default from it could never apply.
  A handle names a **seat**, a role, not one conversation: in a ccy checkout, one of the
  checkout's seats, which one session at a time holds and any later session launched into
  it takes over (section 5.5); elsewhere, one non-ccy install (one `PINGBUS_HOME`). `--new-handle` is a
  remove plus an add (section 4).
- **Roles.** Each agent member is `orchestrator` or `worker` in its team (agent-bus-protocol.md §5
  says which verbs each may send). Roles are runtime data set by `agent-bus add-member` and
  `agent-bus set-role`, published in the team record.
- **Humans.** Named in the team file; each gets one account per team, power level 50 in the
  team room, never server admin. Only these accounts' text reaches agents, and only agents
  whose bundle accepts human text (section 5.1).
- **Multi-team.** A member in several teams has several bundles under one directory
  (`PINGBUS_HOME/<team>/`) and lists the ones it is active in (`PINGBUS_TEAMS`; for a ccy
  session, exactly the teams its launch named, section 5.5). `recv`,
  `wait` and the watcher cover every active team; `send` needs `--team` when more than one is
  active; every output line names its team. A ping's references are checked against its own
  team's allowlists only. Accounts on different teams never share a token.

## 3. The homeserver installer

### 3.1 Two entry points, one implementation

| Entry point                                                                    | Who runs it                            | What it does                                                                                                                                                                                                                                                                                    |
| ------------------------------------------------------------------------------ | -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `files/usr/local/sbin/agent-bus-install` (Bash, as root)                       | any Fedora desktop or server's own IaC | `software`, `team`, `remove`, `backup-now`, `restore`, `check` (below). Non-interactive: arguments only, fails fast, idempotent, marker lines on stdout, diagnostics on stderr.                                                                                                                 |
| `playbooks/imports/play-agent-bus.yml` (core, imported by `playbook-main.yml`) | this repository, every desktop         | runs `agent-bus-install software --source <repo> [--bus-address]` (which installs the dnf dependencies and the installer itself), then `remove` for each `agent_bus_teams` entry with `state: absent`, then `team` for each present one; `remove` comes first so a removed team's port is free. |

The installer is the single source of truth for what a homeserver host looks like; the play
is a thin caller (`command: argv:`, `changed_when` on the installer's `CHANGED` marker), so a
desktop and a data-centre server are configured identically. Another project runs it from a
clone of this repository at a pinned commit:
`sudo <clone>/files/usr/local/sbin/agent-bus-install software --source <clone>`, then
`team --team-file <file>`. It refuses to run unless `/etc/os-release` says `ID=fedora`.
Rendering and validation (team file, `tuwunel.toml`, unit drop-ins) are done by the Python
admin tool (`agent-bus render …`), so Bash only moves files and drives systemd, firewalld
and NetworkManager.

**Why not a container.** The homeserver runs as a static binary under a system unit, not in
rootless podman: a system unit gets the kernel's per-unit IP filter (`IPAddressDeny`/
`IPAddressAllow`, enforced by cgroup BPF), which the user service manager cannot apply
(feasibility review S6); it needs no container engine, no linger and no subuid setup on a
server; and it is the mode the [spike] passed. The Podman-first rule governs choosing an
engine when one is needed; here none is (D1).

### 3.2 `agent-bus-install software`

Idempotent; run on every play run.

- dnf: `python3`, `firewalld`, `NetworkManager`, `zstd`, `curl`, `jq`, `tcpdump` (the last
  for `acceptance.bash` P1/P2; a comment says so, so a YAGNI pass does not remove it).
- System user and group `agent-bus` (`useradd --system`, no login shell, home
  `/var/lib/agent-bus`, mode 0700).
- Tuwunel: version and per-architecture sha256 from `files/usr/local/share/agent-bus/tuwunel.pin`
  (`@see` the release page; registered with `scripts/check-pinned-versions.bash` so the
  `update-versions` skill tracks it). Downloads the static release asset for `uname -m`
  (`x86_64` uses the x86-64-v1 build), verifies the sha256 before anything else touches it,
  decompresses to `/usr/local/lib/agent-bus/tuwunel-<version>` (root-owned, 0755), and
  repoints `/usr/local/lib/agent-bus/tuwunel`. The release is unsigned [tuwunel §1]; the
  pinned hash is the integrity check. Probe H3 records the asset names and hashes. When the
  pinned version is already installed and its recorded hash matches, nothing is downloaded.
- Builds the two zipapps from `--source` (`python3 -m helpers.pingbus.bundle`, reproducible,
  D9): `/usr/local/lib/agent-bus/agent-bus.pyz` and `pingbus.pyz`; symlinks
  `/usr/local/bin/agent-bus` (a wrapper that requires root and drops to the `agent-bus`
  user; for `add-member` it writes the bundle itself, section 5.1) and
  `/usr/local/bin/pingbus`.
- The **member kit**, `/usr/local/share/agent-bus/kit/`: `pingbus` (the zipapp), the Claude
  Code plugin (`plugin/pingbus/`), `settings.json` (`{"crossSessionInbound": "accept"}`),
  and `agent-bus-claude`, the launcher for non-ccy members (section 5.4), linked as
  `/usr/local/bin/agent-bus-claude`; all but `pingbus` are copied from
  `files/opt/claude-yolo/optional/agent-bus/`, the tree the ccy image stages too.
- Units, copied from `files/etc/systemd/system/`: `agent-bus-hs@.service`,
  `agent-bus-backup@.service`, `agent-bus-backup@.timer`; and the resolver stub
  `/usr/local/share/agent-bus/resolv.conf` (section 3.5).
- `--bus-address <bus_ip>` (optional, host-wide): a NetworkManager `dummy` connection
  `agentbus0` holding `<bus_ip>/32`, persistent across boots. This is the address local
  containers, LXC, docker and VMs use to reach this host's homeservers (section 3.3).

### 3.3 Addresses, reachability and the TLS stance

- **Listen.** `tuwunel.toml` `address` is `127.0.0.1` (always; the admin tool and backups
  use it) plus the team file's `listen` list. The homeserver may listen on **any address
  the installer is given** (owner decision, 2026-10-06): `agentbus0`, a WireGuard address, a
  LAN or Wi-Fi address, a bridge; there is no address allowlist and no desktop/server
  distinction. Each listed address must be a well-formed IP literal, and every concrete
  address is allowed. Only the wildcards `0.0.0.0` and `::` are refused (coordinator
  decision, 2026-10-06): a wildcard listens on every interface, including ones nobody named
  in the team file, and P2 cannot check what is bound when the bind names no address.
- **Allow.** The team file's `allow_from` lists the source CIDRs that may connect; any
  well-formed CIDR is accepted. It is the access list both enforcement layers below apply,
  not a judgement of the network.
- **Enforced twice, independently.** firewalld: one rich rule per CIDR accepting
  `tcp/<port>`, in the zone firewalld assigns to the interface carrying that CIDR; nothing
  else is opened. The unit: `IPAddressDeny=any` and `IPAddressAllow=` exactly
  `127.0.0.1/32 ::1/128`, the listen addresses and `allow_from`, in a rendered drop-in
  `/etc/systemd/system/agent-bus-hs@<team>.service.d/network.conf`. The second layer holds
  even where a zone opens a high port range (Fedora Workstation's default zone does). The
  unit filter works in both directions, so it blocks every connection the homeserver could
  start towards anything outside those ranges, but **not** towards hosts inside
  `allow_from` (a WireGuard subnet, a docker bridge). Tuwunel has no reason to connect out
  with federation off; P1 fails on any Tuwunel socket whose local port is not `<port>`.
- **Inside `allow_from`, the network is the access control** (owner, answer 5): any host
  in an allowed CIDR can reach the port; what stops it there is that registration is closed
  and every account needs a token or a generated password.
- **TLS stance: none in v1.** Plain HTTP is private only where the path is inside one kernel
  (loopback, `agentbus0`, a host-only bridge) or encrypted by the network (WireGuard). **On
  any other network (a LAN, Wi-Fi, anything routed beyond the host), every login, password
  and access token travels in clear**, and anyone who can watch that network can read them
  and then act as that human or agent. The installer does not refuse such a team; choosing
  the network is the team's decision, and `docs/agent-bus.md` says this plainly. TLS (U26)
  stays conditional: it is built if probe H7 shows the phone client refuses `http://` even
  over WireGuard. H7 also records whether the phone client trusts a user-installed CA
  (Android apps do not by default); if it does, U26 uses a team-private CA created by the
  installer, and if not, the route (a public DNS-01 certificate, which publishes a name and
  so conflicts with P3) is put to the owner. The rest of the design does not change.
- **Who reaches what.** ccy and other rootless podman containers reach `<bus_ip>` through
  pasta, which connects from the host's own namespace, so the homeserver sees a host
  address (probe H1 records which, on ccy's default network, a named project network and
  `--no-network`; the installer always allows the host's own listen addresses). Docker, LXC and libvirt guests on the same host reach `<bus_ip>` routed
  through their bridge, so their bridge subnet goes in `allow_from`. **Later phase** (D46):
  members on other hosts reach `<wg_ip>` through WireGuard, so the WireGuard subnet (or the
  peer addresses) goes in `allow_from`. A phone reaches `<wg_ip>` the same way (whether the
  phone stays in v1 is owner question 1).

### 3.4 The team file and `agent-bus-install team`

The team file (JSON; on desktops rendered by the play from `agent_bus_teams` in untracked
host_vars, with a commented placeholder in `localhost.yml.dist`), validated by
`agent-bus render check` before anything else runs:

```json
{"team": "<team>", "state": "present",
 "server_name": "<team>.agent-bus.internal",
 "port": <port>,
 "listen": ["<bus_ip>", "<wg_ip>"],
 "allow_from": ["<cidr>"],
 "humans": ["<name>"],
 "repos": [{"repo": "<owner>/<repo>", "branches": ["<default-branch>"]}],
 "path_prefixes": ["CLAUDE/Plan/", "docs/"],
 "forge_api": "https://api.github.com"}
```

`server_name` is optional (that is the default) and never changes for a team: the installer
fails if the one recorded at first install differs (Tuwunel cannot change it without wiping
the database [tuwunel §2]). `.internal` is reserved and never delegated. `port` must be free
and unique on the host. `humans` uses `[a-z][a-z0-9_-]{0,31}`.

`team` then, in order, failing at the first error:

1. Checks listen addresses and `allow_from` (section 3.3) and that `firewalld` and
   NetworkManager are running.
2. `/var/lib/agent-bus/<team>/` (0700, `agent-bus`): `tuwunel.toml` (rendered, section
   3.6), `team.json` (the validated team file), `registry.json` (handles and each
   `<repo>+<host>.<type>` counter, written by `add-member`), `room_id` (the team room,
   written by `bootstrap` when it creates it), `db/`, `backups/`, `secrets/` (0700):
   `registration_shared_secret` (64 random bytes hex, created once, written with no
   trailing newline: the admin tool reads it under the token-file rules, one line of
   printable ASCII with no newline, and refuses anything else).
3. firewalld rules (section 3.3), the unit drop-in, `daemon-reload`, enable
   `agent-bus-hs@<team>.service` and `agent-bus-backup@<team>.timer`, and start the unit;
   it is restarted only when a rendered file (`tuwunel.toml`, the drop-in) or the binary
   changed, and only then does the run print `CHANGED`, so a play run with nothing new
   leaves the homeserver alone.
4. A bounded readiness poll of `http://127.0.0.1:<port>/_tuwunel/server_version` that also
   fails if the unit leaves `active`.
5. Asserts with `ss -ltnH` that `<port>` listens on exactly `127.0.0.1` and `listen`.
6. `agent-bus bootstrap <team>` (idempotent): the admin account, the team room, the human
   accounts, the team record and power levels (section 4); asserts `admin` is the only
   server admin. A new human's account is created with a random password that is
   discarded at once (section 4), so nothing a human logs in with ever passes through the
   installer's or the play's output. Re-running after a team-file change adds new humans,
   deactivates removed ones, and republishes the team record.

`remove --team <team>` stops and disables both units, removes the drop-in and the firewalld
rules; data stays unless `--purge`. `check --team <team>` is read-only and prints each
construction fact P1-P3 check (section 10), for the owner and for other projects' IaC.

### 3.5 The unit, hardened

`agent-bus-hs@.service`: `User=agent-bus`, `Group=agent-bus`,
`Environment=TUWUNEL_CONFIG=/var/lib/agent-bus/%i/tuwunel.toml`,
`ExecStart=/usr/local/lib/agent-bus/tuwunel`,
`TimeoutStopSec=330`, `After=network-online.target`. The restart policy, `Restart=on-failure`,
`RestartSec=5s` and `StartLimitIntervalSec=0` (so a WireGuard address that is not up yet
at boot is bound once it is, instead of the unit hitting the start limit and staying
failed), lives only in the drop-in that `agent-bus render dropin` writes; the base unit
(U16) does not repeat it. The drop-in also adds `After=` and
`Wants=` on `sys-subsystem-net-devices-<if>.device` for every interface carrying a
`listen` address (`agentbus0`, a WireGuard interface, any other). Sandboxing: `NoNewPrivileges`,
`ProtectSystem=strict`,
`ReadWritePaths=/var/lib/agent-bus/%i`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`,
`ProtectKernelTunables`, `ProtectKernelModules`, `ProtectControlGroups`,
`RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `SocketBindDeny=any` with
`SocketBindAllow=tcp:<port>` (in the drop-in), `IPAccounting=yes`, and the IP filter.
**No resolver:** on Fedora `/etc/resolv.conf` is a symlink into `/run/systemd/resolve/`, so
the unit puts `TemporaryFileSystem=/run/systemd/resolve:ro` over that directory and
`BindReadOnlyPaths=` the stub `/usr/local/share/agent-bus/resolv.conf` (`nameserver 127.0.0.1`, where nothing listens and which the IP filter allows only on loopback) onto
`/run/systemd/resolve/stub-resolv.conf` and `/run/systemd/resolve/resolv.conf`, plus
`InaccessiblePaths=-/run/dbus`, so a lookup fails inside the host. Probe H3 confirms, on
Fedora Workstation and Fedora Server both, that `/etc/resolv.conf` inside the unit reads the
stub and that Tuwunel starts with it. `Type=notify` if H3 shows Tuwunel sends readiness;
otherwise `simple` plus the readiness poll.

### 3.6 `tuwunel.toml`, every key

Rendered by `agent-bus render toml` as top-level keys with no `[global]` table, the form
H4 started Tuwunel with; a test (U15) asserts this exact key set and that each key appears
in Tuwunel's example config for the pinned version, whose key names are vendored as
`tests/helpers/agent_bus/fixtures/tuwunel-1.9.3-example-keys.txt` (names only: the
file's defaults hold private address ranges the repository's scanner refuses; the source
URL and its SHA-256 are recorded in `test_render.py`, so a pin bump must regenerate it). By default Tuwunel only
warns about an unknown key and carries on; `error_on_unknown_config_opts = true` makes one
fatal at start (H4: exit 1), so a misspelt or retired key fails the unit and the installer's
readiness step (U16) reports it, with no journal scan. H4 starts Tuwunel with exactly this
key set, which proves the pinned version knows every key.

| Key                               | Value                                                          | Why                                                                |
| --------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------ |
| `server_name`                     | `<sn>`                                                         | Fixed for the team's life.                                         |
| `address`                         | `["127.0.0.1", <listen>…]`                                     | Section 3.3.                                                       |
| `port`                            | `<port>`                                                       | The team's port.                                                   |
| `database_path`                   | `/var/lib/agent-bus/<team>/db`                                 |                                                                    |
| `database_backup_path`            | `/var/lib/agent-bus/<team>/backups`                            | Section 3.7.                                                       |
| `database_backups_to_keep`        | `7`                                                            |                                                                    |
| `allow_registration`              | `false`                                                        | Closed from first boot.                                            |
| `allow_guest_registration`        | `false`                                                        | Explicit.                                                          |
| `registration_shared_secret_file` | `/var/lib/agent-bus/<team>/secrets/registration_shared_secret` | Admin-API account creation; read on every use.                     |
| `grant_admin_to_first_user`       | `false`                                                        | No account becomes admin by order.                                 |
| `login_via_token`                 | `false`                                                        | No token or QR login: humans log in with their password only.      |
| `login_via_existing_session`      | `false`                                                        | No session can mint a login token (default `true`); P7.            |
| `allow_federation`                | `false`                                                        | Issue §1.                                                          |
| `trusted_servers`                 | `[]`                                                           | No key-server queries (default `matrix.org`).                      |
| `federate_admin_room`             | `false`                                                        | Must be set before the admin room exists.                          |
| `admin_escape_commands`           | `false`                                                        | No `\!admin` outside the admin room.                               |
| `allow_encryption`                | `false`                                                        | The team room is not end-to-end encrypted; receivers read it.      |
| `auto_accept_invites`             | `false`                                                        | Members accept after their own checks (section 4).                 |
| `new_user_displayname_suffix`     | `""`                                                           | Display name is exactly the handle.                                |
| `client_sync_timeout_min`         | `0`                                                            | A non-blocking `recv` [tuwunel §5.6].                              |
| `default_room_version`            | `"12"`                                                         | Room trust depends on v12 creator semantics.                       |
| `sentry`                          | `false`                                                        | Explicit.                                                          |
| `log`                             | `"warn"`                                                       | H4 records that no request header (token) is logged at this level. |
| `admin_signal_execute`            | `["server backup-database"]`                                   | What SIGUSR2 runs; empty by default, so no backup without it (H5). |
| `error_on_unknown_config_opts`    | `true`                                                         | An unknown key fails the start instead of a warning (H4).          |
| `rocksdb_allow_fallocate`         | `false`                                                        | Tuwunel's advice on btrfs (Fedora's default); harmless elsewhere.  |

URL-preview allowlists stay at their empty defaults (fetch nothing); `max_request_size`
stays at its default.

### 3.7 Secrets, the admin token, backup

- Everything secret lives in `/var/lib/agent-bus/<team>/secrets/`, owned by `agent-bus`,
  0700/0600: `registration_shared_secret`, `admin.token` and `admin.password` (from the
  shared-secret registration at bootstrap). Never in argv, environment variables, logs or
  marker output. The shared secret can mint a server admin, so it is the team's root of
  trust, and the admin token at rest adds no exposure the secret does not already have.
  **Human passwords are never stored**: `sudo agent-bus human password <team> <name>` sets
  a new 32-character password, logs out that human's other devices, and prints it once to
  the terminal that ran it (its stdout is the payload); losing it means running it again.
- **No agent can read them.** Agents run as the desktop user inside ccy (container root
  maps to it), as a dedicated user on the bare desktop (section 8), as their own users on
  servers, or inside guests; none is `agent-bus`. The admin tool
  reaches them only through `sudo agent-bus …`, which re-executes as `agent-bus`. The admin
  tool talks to `127.0.0.1:<port>` only and refuses any other base URL. The one place this
  boundary does not hold is an agent that can `sudo` without a password: on such a host, a
  bare-host member is trusted like a human (documented).
- The shared-secret registration also sets a generated admin password, kept as
  `secrets/admin.password` so that a later admin-token rotation can log in over loopback;
  the `rotate-admin` command itself is deferred (no success criterion needs it).
  `rotate-token <team> <handle>` logs a member's devices out and writes a new bundle token.
- **Backup** (the [spike]'s managed backup): `agent-bus-backup@<team>.timer` daily runs the
  service, as root, which sends `SIGUSR2` to the unit (`systemctl kill --signal=SIGUSR2`;
  Tuwunel runs `admin_signal_execute`, section 3.6, and does nothing on the signal without
  it), waits a bounded time for a new numbered file in `database_backup_path/meta/` (H5: it
  appears within about a second; fails the unit if none appears), then adds a tar of `secrets/`, `team.json`, `registry.json` and `room_id`, numbered as
  the backup (root, 0600). The tar is kept in root's own `/var/lib/agent-bus-install/<team>/backups/` (0700), not
  beside the database backup: `agent-bus` can write there, and restore extracts the tar as root, so a tar a
  compromised homeserver could swap would plant files with owners and modes of its choosing. A backup holds the team's root of trust (the shared secret and the admin token):
  whoever holds one can instruct every agent in the team, so `docs/agent-bus.md` says so,
  and copying backups off the host is the owner's decision and is not built.
  `agent-bus-install backup-now --team <team>` runs the same once.
- **Restore:** `agent-bus-install restore --team <team> --backup <id>` stops the unit,
  restores the tar (refused unless it holds only those members as plain files and directories with no
  setuid, setgid or sticky bit; extracted without its owners and modes, then given to `agent-bus`),
  starts the binary once with `--restore-backup <id>` as a collected transient unit carrying the
  homeserver unit's whole section 3.5 sandbox (read from the installed unit) with loopback-only
  `IPAddressAllow` and the team's `SocketBindAllow`
  (built into Tuwunel: it restores, then serves; H5 confirmed, with the latest backup, that
  an event sent before it is present and one sent after it is gone), stops it once it
  answers, then starts the unit and runs the readiness
  poll and `bootstrap`'s assertions.

## 4. Accounts and the team room

Accounts, all created by `agent-bus` through the admin API with registration closed from
first boot \[tuwunel §3\]:

| Account           | Localpart  | How created                                                                    | Credential kept                                     |
| ----------------- | ---------- | ------------------------------------------------------------------------------ | --------------------------------------------------- |
| team admin        | `admin`    | `POST /_synapse/admin/v1/register` with the shared-secret HMAC, admin=true     | `secrets/admin.token`, `secrets/admin.password`     |
| each human        | `<name>`   | `PUT /_synapse/admin/v2/users/...` (no `admin` key), random password discarded | none: `human password` sets one and prints it once  |
| each agent member | `<handle>` | `add-member`: same, password discarded, then `.../users/{id}/login` mint       | the member's bundle only (`token`), never on the HS |

Agent handles always contain `+`; `admin`, `conduit` and human localparts never do. H4
(run in the container against Tuwunel 1.9.3) created real `+` handles through both
`v1/register` and `PUT v2/users`, so the separator stays `+` (one constant in
`protocol.py`). Both password resets (`v1/reset_password` and `PUT v2/users` with a new
password and `logout_devices`) log the old token out. **`PUT v2/users` must not carry an
`admin` key when it creates an account**: with `"admin": false` Tuwunel answers 500 ("was
never an admin"); without the key the account is created as a non-admin (U15).

**The team room** (one per team, created by `admin` at bootstrap; room version 12, so
`admin`, as creator, holds creator power and is the only account that can change state):
name = the team name, a fixed topic, invite-only, not in the directory. Power levels are in
agent-bus-protocol.md §8: humans 50, agents 0; agents and humans can send messages, nobody but
`admin` can set state, invite, kick or redact. `@room` is not limited by the server
(`notifications.room` governs push notifications only, so any member can set
`m.mentions.room`); receivers act on `@room` only from a listed human, because the sender
check comes first. The **team record**
(`agent_bus.team`, state key `""`, agent-bus-protocol.md §8) holds the humans, the agents' roles, the
repository allowlist and path prefixes. `agent_bus.status` is open to every member at power
0, and the server forces only `@`-prefixed state keys to equal the sender, so receivers
ignore any status whose state key is not its sender's user ID.

**Joining.** pingbus accepts an invite on its own (deterministic, no model involved) only
when the inviter and the stripped `m.room.create` sender are the bundle's `admin` and the
room ID is the bundle's `room`. After joining, and on every change to these state events, it
re-reads `m.room.create?format=event` (sender `admin`, version 12), `m.room.power_levels`
(exactly agent-bus-protocol.md §8 for the current humans) and `agent_bus.team` (sender `admin`, lists
itself with a role); on a mismatch it stops treating the room as trusted (exit 10, `status`
says why) and receives nothing from it. Joined members who are neither listed humans, role
holders nor `admin` are reported by `pingbus status` and everything they send is dropped
(`sender`).

`agent-bus` commands (all arguments, `--opt=value` for any value from outside, validated
before any call, never prompting): `bootstrap`, `add-member <team> --repo=… [--seat=<seat>] --host=… --type=… --role=… --address=<IP> [--no-human-text] --out=<dir>` (creates the account, mints its token,
invites it, updates the team record, and writes the bundle, section 5.1; `--seat` names the
handle's seat, section 5.5, and without it the registry's counter numbers it; `--seat` on a
**parked** seat returns it: the same account, role and room membership, a newly minted
token, the bundle written again; `--address` is the
homeserver address the bundle's `base_url` names: one of the team's listen addresses, for a
podman member too, its bus address `<bus_ip>`; section 5.3 holds the open fallback, which
U19 settles),
`park-member <team> <handle>` (the seat's token revoked, everything else kept, section 5.5:
the password reset with `logout_devices` that `rotate-token` already uses, recorded by H4,
then the handle marked parked in the registry),
`remove-member <team> <handle>` (kicks, deactivates, removes the role; the handle never
returns, because Tuwunel keeps the deactivated account and `add-member` refuses a handle
that has one, as `bootstrap` already does for a removed human),
`set-role`, `rotate-token` (the new token as a one-file tar on stdout, placed by the
wrapper), `list` (members, roles, state; no tokens),
`human password|devices|logout-all|lock|unlock <team> <name>`, and
`render check|toml|dropin` for the installer, which read the team file on **stdin**
(the `agent-bus` user cannot read root's copy of it; `render check --previous=<team.json>`
names the installed one, which must then exist, and is left out on a first install).
Three outputs carry a secret, each as its command's stdout payload only: the
`add-member` bundle tar, the `rotate-token` tar, and `human password`'s password.
`rotate-admin` is deferred (section 3.7).

**The human's account, locked down** (owner, answer 5): never server admin; power 50 in one
room only; a 32-character generated password, set and printed once by `sudo agent-bus human password` (its output is the payload; nothing is kept); password login only
(`login_via_token = false` and `login_via_existing_session = false`, so no session can mint
a login token; Tuwunel 1.9.3 still lists `m.login.token` in `GET /login`, H4, so P7 proves
the refusal rather than the list), and only over the addresses section 3.3 allows; no guest access, no third-party identifiers, no
identity server; `human devices` lists sessions and `logout-all` ends them; `lock` blocks a
lost phone's account at once. Tuwunel 1.9.3 has no login rate limit [tuwunel §6]; the
password length makes guessing infeasible, and the rate limit of the next release is
switched on when the pin moves. Matrix offers no second factor without an external identity
provider; that limitation is documented, not engineered around.

## 5. Joining a team, per encapsulation

### 5.1 What every member gets

`agent-bus add-member … --out=<dir>` produces a **bundle**: `member.json` (agent-bus-protocol.md §12:
team, user ID, `server_name`, `base_url`, the plain-HTTP host list, `admin`, `room`,
`human_text`), `token`, and a short `README` naming the next steps for the member's type.
The `agent-bus` user cannot write into a human's 0700 home, so the work is split: the
`agent-bus` side writes the bundle to its stdout as a tar stream (its payload; nothing else
on stdout), and the root wrapper unpacks it into a fresh private temporary directory and
places each file with `install -d -o "$SUDO_UID" -m 0700` and `install -o "$SUDO_UID" -m 0600`
under `--out`. Root never chowns a directory it did not create: `add-member` refuses an
`--out` that already exists (a symlink included), and `rotate-token` writes only into a
real directory (not a symlink) owned by the user who ran `sudo`, leaving its mode alone. **`--no-human-text`** writes `"human_text": false`: the member accepts pings
only and drops every human message. It is the choice for a member joining a team whose
homeserver someone else runs (section 9). It can only narrow what the member accepts, so a
bundle writable by its agent does not weaken it. The human copies the bundle to `PINGBUS_HOME/<team>/` on the member,
by hand or by that project's IaC; a ccy checkout's seat bundles are written on the host by
the launch that first seats it in a team (section 5.6), never by hand. The member also needs: `pingbus` on `PATH` (the kit, or
the ccy image), the Claude Code plugin, and its session started with the plugin and the
inbox-socket setting (section 6). `pingbus suggest-handle` prints the `add-member` arguments
a member's environment implies (repository from the git remote, else the directory; host
from `HOOKS_DAEMON_HOSTNAME`, refusing when it is unset; type from the environment), so the
human does not guess.

### 5.2 Per encapsulation

| Type                               | Where the bundle goes                                                                                                                                                       | How opt-in is expressed                                                                            | How it reaches the homeserver                                | `allow_from` needs             | pingbus and plugin from           |
| ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ | ------------------------------ | --------------------------------- |
| `podman` (ccy)                     | `<checkout>/.claude/ccy/pingbus/seats/<seat>/<team>/` (git-ignored by ccy's `.claude/ccy/.gitignore`), written by the launch that first seats it in that team (section 5.6) | per session, at launch: `ccy --team <team> [--seat <seat>]` (section 5.5); nothing in the checkout | `<bus_ip>` (same host, via pasta)                            | nothing extra (host addresses) | the ccy image                     |
| `host` (bare desktop)              | a dedicated agent user's `~/.config/pingbus/<team>/` (never the human's user, section 8)                                                                                    | `PINGBUS_TEAMS` in `~/.config/pingbus/env`                                                         | `<bus_ip>` or `<wg_ip>`                                      | nothing extra                  | `/usr/local/bin/pingbus`, the kit |
| `host` (a server, always-on agent) | the agent user's `~/.config/pingbus/<team>/`                                                                                                                                | the same env file                                                                                  | `<bus_ip>` beside it (the placement convention) or `<wg_ip>` | nothing extra                  | the kit (copied by its IaC)       |
| `lxc`                              | inside the LXC, the agent user's `~/.config/pingbus/<team>/`                                                                                                                | the same env file                                                                                  | `<bus_ip>` via the LXC bridge, or its own WireGuard          | the LXC bridge subnet          | the kit, copied in                |
| `docker`                           | copied, or bind-mounted read-write (pingbus keeps state in it), at `PINGBUS_HOME/<team>`                                                                                    | `PINGBUS_TEAMS` in the container environment                                                       | `<bus_ip>` via the docker bridge                             | the docker network subnet      | the kit, copied in or mounted     |
| `vm`                               | inside the VM, as `lxc`                                                                                                                                                     | the same env file                                                                                  | `<bus_ip>` via the libvirt bridge, or `<wg_ip>`              | the libvirt network subnet     | the kit, copied in                |

What differs beyond the table:

- **Python.** The zipapp needs Python 3.11 or later; pingbus checks at start (exit 78). The
  installer's hosts and the ccy image have it; a docker or non-Fedora guest image must.
- **Role variable.** Non-ccy members export `HOOKS_DAEMON_HOSTNAME=<role>` in the env file,
  which names the handle's `<host>` and the hooks daemon's role from one value; with none
  set, `--host` is passed explicitly (section 2). A ccy seat's `<host>` is the checkout's
  role: the `HOOKS_DAEMON_HOSTNAME` its `ccy.env.local` sets, which the launcher reads on
  the host by parsing the file (never sourcing it) when it creates a seat's bundle, refusing
  when the file does not set it (section 5.6). The hooks daemon inside the checkout's
  sessions names itself from the same line, so a seat's handle and its session's role
  agree, and nothing new is written into the checkout.
- **Writable bundle.** Every bundle is writable by code running as its agent. That is no new
  exposure: the token is the agent's own, the bundle holds no allowlist or human list
  (those come from the team record, which only `admin` can write), and its one switch,
  `human_text`, can only be set back to the default; so code in an agent's checkout can at
  most misdirect that agent, which it can already do by editing files the agent reads.
- **Never ignored by accident.** A ccy bundle sits under `.claude/ccy/`, which ccy's
  generated `.gitignore` ignores except for named files; P8 asserts `git check-ignore` for
  every bundle path the acceptance members use.
- **A dedicated agent user's bundle takes two steps.** `add-member --out` gives the bundle
  to the user who ran `sudo`, and `rotate-token --out` accepts only that user's own
  directory. For a `host` member run by a dedicated agent user, the human writes the bundle
  to their own directory, then installs it into the agent user's `~/.config/pingbus/<team>/`
  (owned by that user, `0700` directory, `0600` files) and deletes their copy; a token
  rotation repeats that step. The install's IaC does this; `docs/agent-bus.md` says so.
- **One session per seat at a time.** In a ccy checkout every session claims its own seat by
  the seat lock (section 5.5), so two live sessions never share an account; a later session
  may take the seat over. A non-ccy install is one seat
  (one `PINGBUS_HOME`); two sessions using one such bundle share an account, and the sync
  lock (agent-bus-protocol.md §12, §14 exit 75) makes the second session's watcher exit busy
  and its SessionStart hook say that another process holds this seat's sync lock.

### 5.3 The ccy member

- **Team membership is per launch, and explicit** (owner, 2026-10-07, D44: "ccy doesnt join
  teams automatically - if a session is in a team it must be launched with team. if it is
  not then it is not on the team"). `ccy --team <team>` puts that one session in that team;
  a plain `ccy` is in no team, in every checkout, whatever seats the checkout has: no seat
  is claimed, no bundle is read, no watcher starts and the bus plugin is not loaded. The
  checkout carries no team setting: nothing about the bus is written into `ccy.env.local`,
  and there is no checkout-level opt-in command (U19's `PINGBUS_TEAMS` in `ccy.env.local`
  and the earlier `agent-bus join` are gone, D33).
- **The team and the seat are decided on the host, never in the container.** The launcher
  passes `PINGBUS_TEAMS` and `PINGBUS_SEAT` into the container from its own flags. Nothing
  the session can write decides either: the launcher reads no team or seat from the
  checkout, and the entrypoint refuses (below) a launch in which `ccy.env` (tracked, so
  writable by the session) or `ccy.env.local` assigns a bus variable. The one value the
  launcher does take from the checkout, the seat handle's `<host>` (the
  `HOOKS_DAEMON_HOSTNAME` line of `ccy.env.local`, section 5.2), comes from the file ccy
  binds read-only over the workspace since 3.84.0 (Plan 00160 Task 3.3), so a session cannot
  change it; it only labels a seat when the seat is first created in a team, and the
  launcher prints each handle on stderr before it creates the account.
- **The template is ccy's.** Since ccy 3.83.0 (Plan 00160 Task 3.2) ccy writes the tracked
  `.claude/ccy/ccy.env.local.dist` itself from `ccy_env_local_dist_text` in
  `files/var/local/claude-yolo/lib/common.bash`, versioned by `CCY_ENV_LOCAL_DIST_VERSION`;
  it carries the `HOOKS_DAEMON_HOSTNAME` placeholder, which a seat's handle now uses. U19
  added a commented `#export PINGBUS_TEAMS=<team>[,<team>]` block (dist version 2); U31
  removes it (a team is given at launch, so the file has nothing to say about the bus),
  raises `CCY_ENV_LOCAL_DIST_VERSION` to 3, bumps `CCY_VERSION`, and updates
  `scripts/test-ccy-env-local-dist.bash`. Installs whose `ccy.env.local` names the older
  dist version then get ccy's launch warning, the intended prompt to drop the block.
- **The entrypoint** (U31 reshapes U19's block) records `PINGBUS_TEAMS`, `PINGBUS_SEAT` and
  `PINGBUS_HOME` as the launcher passed them before it sources `ccy.env` and
  `ccy.env.local`, and afterwards refuses to start if either file set or changed any of
  them, naming `ccy --team`/`--seat` as the only way to choose a team or seat. It refuses
  `PINGBUS_TEAMS` without `PINGBUS_SEAT` or the reverse (the launcher always passes both,
  section 5.5, so one alone means a launcher older than the image). When `PINGBUS_TEAMS`
  is set, it symlinks `/usr/local/bin/pingbus`; adds `--plugin-dir /opt/claude-yolo/optional/agent-bus/plugin/pingbus`
  (the plugin's own root, as U01 loaded its probe plugin, not the directory above it)
  and `--settings /opt/claude-yolo/optional/agent-bus/settings.json` to the `claude`
  arguments (inside any supervisor wrapper's `--`); and puts
  `/opt/claude-yolo/optional/agent-bus/pingbus seat exec --` in front of the final `exec`
  (both of the entrypoint's final `exec` lines), which claims the seat, runs `config check`
  for the launch's teams and **refuses to start** with the reason if either fails (a launch
  that asked for a team and cannot have it is an error, not a silent no-op), then execs the
  wrapper and `claude` (section 5.5). When `PINGBUS_TEAMS` is not set (a plain `ccy`),
  nothing is installed or added: the image's copy is inert.
- **Settled: the ccy `base_url` is `http://<bus_ip>:<port>`.** Probe H1 (JOURNAL
  2026-10-06) found that, without a bus address, a ccy container reaches a same-host
  homeserver only through `host.containers.internal`, a hostname, which agent-bus-protocol.md
  §12 and U04's `config.check_base_url` (which U09's client also applies) refuse for
  `http://`: they allow only an IP literal in `plain_http_hosts`. H1's dummy leg, re-run
  against `agentbus0` once U16 created it (JOURNAL 2026-10-07), reached `<bus_ip>` from the
  ccy image on the default, a named and the pasta network alike, with the source seen as
  `<bus_ip>`; the host's primary address was refused from all three. So the bus address is
  the ccy address and §12 is unchanged; the `host.containers.internal` exception is not
  needed.
- **The launcher** (U31) gains `--team <team>` and `--seat <seat>`, picks and creates the
  seat on the host before the container starts (section 5.5), labels the container
  `ccy-seat=<seat>`, and gains one behaviour for headless launches: with
  `--headless` and no launch-choice flag (`--token`, `--ssh-key`, `--ssh-agent`, `--no-ssh`,
  `--network`, `--no-network`), the checkout's Quick Launch choices
  (`.claude/ccy/.last-launch.conf`: `LAST_TOKEN`, `LAST_SSH_KEYS`, `LAST_NETWORK`) are taken
  without the Quick Launch prompt, as a session restore already takes them, and a headless
  launch with no saved choices is refused instead of prompting. Today that prompt reads
  stdin, which for a headless session driven through a pipe is the session's own input. A
  key in `LAST_SSH_KEYS` that would need a passphrase fails a headless launch before the
  container starts, naming `ssh-add` (a headless launch never unlocks a key).
  Nothing else: no host-side credential store, no deny list entry (the team's secrets belong
  to another user), no extra network (the container's usual route reaches `<bus_ip>`
  through pasta, probe H1, and would reach WireGuard addresses the same way in the later
  phase).
- **Image:** `optional/agent-bus/` (zipapp, plugin, settings) staged by
  `play-claude-yolo.yml`, which builds the zipapp itself from `helpers/` with the same
  reproducible builder (so the core ccy play never depends on the bus play); the Dockerfile
  adds `chmod 0755` for the bin. One minor `CCY_VERSION` bump, one container version bump
  (`LABEL` and `REQUIRED_CONTAINER_VERSION` together), a `docs/ccy-changelog.md` entry, and
  two rows in `docs/ccy.md`'s "What the container CAN reach" table (the bundle, the bus
  address).

### 5.4 Non-ccy members

`agent-bus-claude` (from the kit) reads `~/.config/pingbus/env` (or `PINGBUS_ENV`), exports
`PINGBUS_HOME` and `PINGBUS_TEAMS`, checks every listed team's config, refuses (section 8)
when the running user has an Element profile, and execs
`claude --plugin-dir <kit>/plugin/pingbus --settings <kit>/settings.json "$@"`. `<kit>` is
the directory the launcher resolves into (a symlink on `PATH` works), and the kit's own
`pingbus`, when present, goes first on `PATH`, because the hooks run `pingbus` by name. The
env file is sourced with every assignment exported; it may also set `PINGBUS_HOME` and
`HOOKS_DAEMON_HOSTNAME`. The Element check looks in `$HOME`, at the paths `config.py`
names, before the env file is read. The source is
`files/opt/claude-yolo/optional/agent-bus/agent-bus-claude`, so the kit and the ccy image
take one tree; `agent-bus-install software` copies it, the settings and the plugin into the
kit and links `/usr/local/bin/agent-bus-claude`. On a desktop the
human starts it as the dedicated agent user (for example `sudo -iu <agent-user> agent-bus-claude`);
creating that user is the install's IaC, documented in `docs/agent-bus.md`, not built in v1. A headless agent driven by a script (`claude -p` in a
loop, as a server-side triage agent may be) needs no socket: its driver runs `pingbus wait`
between turns.

### 5.5 Seats: several ccy sessions in one checkout

Owner decisions 2026-10-07 (D32, D40): "one seat per session - can be numeric could also be
role based"; and a seat is a role, not a session: "it's the role that's important, not the
specific agent session". Each ccy session launched with a team is its own team member, so
sessions in one checkout can ping each other. Only a session launched with `--team` sits in
a seat (D44, section 5.3); a plain `ccy` takes none.

- **A seat** is a durable member identity of a ccy checkout: one name, and under it one
  account and one bundle per team it has been launched in, and its own pingbus state
  (inbox, consumed items, outbox, room view). The only exclusivity is one live session per
  seat at a time (the seat lock below), whatever teams that session named. A seat outlives
  sessions, reboots and days: a session launched into the seat continues as that member in
  each team its launch names, with its handle, role, pending items and history
  (`pingbus history`, below). Seats are created on first use by the launch (section 5.6) and
  stay until the human removes one.

- **One seat across teams, not one per team** (D45, owner answer B: "yes for v1"). Seat
  `dev` is one name and one `PINGBUS_HOME` in the checkout; in each team it holds a separate
  account, all with the same handle `<repo>.dev+<host>.podman` on their own homeservers. A
  launch with `--team a --team b --seat dev` is one member in both teams, with one seat lock
  and one watcher that covers both, which is pingbus's multi-team model unchanged. The
  alternative, a seat per (team, name), would give a two-team session two seats, two
  `PINGBUS_HOME`s, two locks and possibly two numbers, none of which pingbus has, and buys
  nothing a v1 checkout needs. A seat's bundle for a team the current launch does not name
  sits idle (not synced, not watched); a later launch naming that team picks up from the
  bundle's saved sync position, so the role's pending traffic in that team is still
  delivered. A bundle for a team that has since been purged is harmless until a launch
  names that team again, when `config check` refuses it, naming `agent-bus seat remove`. The registry's counter key `<repo>+<host>.<type>`, which `registry.py` also
  calls a seat, is renamed `prefix` by U29 so the word has one meaning.

- **Seat names**: a number, `[1-9][0-9]{0,5}`, or a role name, `[a-z][a-z0-9]{0,11}` (`dev`,
  `audit`, `pm`). No `-`, `_` or `.`: a seat sits between `.` and `+` in the handle. Numbered
  seats are the pool an unnamed launch draws from; a role seat is taken only by name.

- **The handle** (U29, agent-bus-protocol.md §3) is `<repo>.<seat>+<host>.<type>`: the `<n>`
  part widens to `(?P<seat>[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11})`. Every existing handle,
  bundle, registry and team record stays valid unchanged: a counter-numbered handle is a
  numeric seat. The owner's `{repo}-dev` is written `<repo>.dev+<host>.podman`: `.` is the
  separator the grammar already has, and a `-` would be ambiguous, since repository names
  contain them. `add-member --seat=<seat>` builds the handle from the seat given; without it
  the registry's counter numbers it, as now (every non-ccy member).

- **Reuse: park, return, retire** (D40, owner answer 2). A seat's name is reused by design:
  the same name in the same checkout is the same member every time, so pings, outbox entries
  and forge text naming its handle stay meaningful to whoever sits in it next.

  - **Park.** Removing a seat (`agent-bus seat remove`, section 5.6) revokes its token in
    every team it has a bundle for (`park-member`, section 4) and deletes its local
    directory; its account, room membership and role stay, and the registry marks the
    handle parked.
  - **Return.** Launching into the same name again (`ccy --team <team> --seat <seat>`) runs
    `add-member --seat` on the parked handle in each team the launch names, which mints a new token for the same
    account and writes the bundle: the same identity, and its history, since it never left
    the room.
  - **Why park rather than deactivate and reactivate.** Tuwunel keeps a deactivated
    account, and nothing recorded brings one back: H4 recorded account creation, the
    password reset with `logout_devices` (fixtures 029, 030: the old token then fails
    `whoami`) and the admin login mint (016, 018), but no reactivation, and `bootstrap`
    already treats a deactivated human as unable to return. Parking and returning use only
    those recorded calls, the pair `rotate-token` already uses.
  - **One place at a time.** `add-member --seat` refuses a handle that is a current, unparked
    member, so two checkouts of one repository on one host asking for one seat name cannot
    share it: the second is refused, naming `agent-bus seat remove` in the first or another
    name. A lost bundle (the directory deleted by accident) is recovered the same way:
    `agent-bus seat remove <seat>`, then launch again, which returns the identity with a
    new token.
  - **Retire.** `remove-member` stays for a member that must never come back: it deactivates
    the account, and `add-member` refuses a handle with an account, so the handle cannot
    return.
  - **Registry v2** (U29): `parked`, the parked handles; the counter skips numbers issued with
    `--seat`; a v1 registry loads as v2 with `parked` empty.

- **Protocol version: the grammar widens inside version 1** (D37, owner answer 1). Version 1 has run in no
  team but this plan's acceptance team, which is created and purged on every run; H4 already
  reserved a pre-release change to the handle grammar; and every v1 handle stays valid. A
  team record listing a role seat is not valid to a pingbus built before U29, which then
  treats the room as untrusted (exit 10, `status` says why): loud, not silent. In this
  repository every pingbus (the kit and the ccy image) comes from one build; a member of
  another project re-copies the kit before a role seat joins its team, and
  `docs/agent-bus.md` says so.

- **Layout** in a checkout, all under the git-ignored `.claude/ccy/pingbus/`, all written
  on the host by the launch (section 5.6) and then by the seat's own pingbus:

  ```
  .claude/ccy/pingbus/seats/<seat>/              PINGBUS_HOME of the session holding <seat>
  .claude/ccy/pingbus/seats/<seat>/seat.lock     the seat lock
  .claude/ccy/pingbus/seats/<seat>/<team>/       one bundle (member.json, token, state/) per team
  .claude/ccy/pingbus/seats/<seat>/stop-guard.json, watch.log    per seat, as protocol §12 has them
  ```

  The bundle format and pingbus's state are unchanged; only where `PINGBUS_HOME` points
  moves. Nothing reads the pre-seats ccy layout (`.claude/ccy/pingbus/<team>/`, which only
  this plan's throwaway U20 checkouts ever had), so nothing refuses it either.

- **The seat record is the directory.** A seat exists when `seats/<seat>/` exists, and is in
  the teams it holds a bundle for; a launch completes it for every team the launch names. No
  list of seats or of a checkout's teams is kept anywhere else, so nothing can disagree with
  the files. A session can write the directory (it is in its workspace) but cannot create an
  account or choose a team: minting needs `sudo agent-bus` on the host, and the teams come
  only from the launcher's flags. Every seat's token is
  readable by every session in the checkout (one user, one workspace): a seat is an
  identity for coordination, not a boundary between the checkout's sessions (section 9).

- **Launch** (U31, D44, D36). The flags:

  - `--team <team>`, repeatable (`--team a --team b`), puts the session in those teams and
    no others; the launcher passes them as `PINGBUS_TEAMS=a,b`. Each name is checked
    against the team grammar and must be a team whose homeserver runs on this host
    (`systemctl is-active agent-bus-hs@<team>.service`, which any user may ask; D46), else
    the launch is refused naming the play or `agent-bus-install team`.
  - `--seat <seat>` names the seat. Without `--team` it is a usage error (exit 64, before
    anything runs: "a seat is a place in a team: add --team <team>"), never ignored, since
    ignoring it would start a session the human thinks is on the bus.
  - Neither is a Quick Launch choice or read from the checkout: a team is given by each
    launch or not at all. `--no-bus` is not built: a launch without `--team` is already off
    the bus.

  With `--team`, the launcher picks the seat on the host, before the container starts, with
  `agent-bus seat take [<seat>] --team <team>…` (section 5.6), which creates or completes
  the seat for the named teams and prints its name:

  - `ccy --team <team> --seat <seat>`: that seat, created on first use. Held by another
    session: refused before any container starts, exit 75, "seat `<seat>` is held by another
    session".
  - `ccy --team <team>` with no `--seat`: the lowest-numbered free seat of the checkout,
    whatever teams it already has (a launch adds the bundle for any named team it lacks),
    created when no numbered seat is free (`1`, then `2`, …). Numbers are reused, never
    bumped per session: after a reboot the first such session sits in seat 1 again. A role
    seat is never taken by an unnamed launch. The human asked for the team, so the launch
    creates a seat rather than refusing (owner question 1 of the rework, settled by D44).
  - The launcher then passes `PINGBUS_TEAMS` and `PINGBUS_SEAT=<seat>` into the container,
    labels it `ccy-seat=<seat>`, and puts `--team <team>…` and `--seat <seat>` in the
    session's restart and restore arguments, so a restarted or restored session comes back
    in its teams and its seat, and a plain session comes back plain.
  - Creating a seat, or a team bundle an existing seat lacks, runs `sudo agent-bus add-member` once per team, naming the team and the handle on stderr first. An
    interactive launch lets sudo ask for the password; a headless launch never prompts: it
    creates only with sudo's cached credential (`sudo -n`), and is otherwise refused naming
    `sudo -v`.
  - Two launches that pick the same free seat at the same moment: the second container's
    claim fails with exit 75 and the human launches again. Nothing is shared meanwhile.

- **Claim** (U30, `pingbus seat exec [--] CMD…`, run by the entrypoint as its final `exec`):

  1. reads `PINGBUS_SEAT`, refusing (exit 78) a missing or malformed seat or one with no
     directory (the host side creates seats, never the container);
  2. takes the seat's lock without waiting; held: exit 75, "seat `<seat>` is held by another
     session";
  3. writes kind `seat` and the claim time into `seat.lock` (U30 moves `inbox.py`'s
     `acquire_lock`/`probe_lock` onto a path, so the seat lock and the sync locks share one
     primitive and one rule: `flock`, the kind in the file, never a PID), and marks the
     descriptor inheritable;
  4. sets `PINGBUS_HOME=/workspace/.claude/ccy/pingbus/seats/<seat>` and runs `config check`
     for every team in `PINGBUS_TEAMS` in-process (a missing bundle names
     `ccy --team <team> --seat <seat>` on the host, which completes it; refused: 78, and the
     container does not start, as U19's check);
  5. prints `✓ agent team bus: seat <seat> (<handle>[, <handle>…]), teams <team>[,…]` on
     stderr and `execvp`s the rest.

- **Release.** The seat lock is an `flock` on an open file description that every process of
  the session inherits, so it is released when the last of them exits. In ccy that is the
  container's end: PID 1 is the session, and `podman run --rm` takes every process with it,
  so a detached child cannot keep a seat past its session, and a session killed outright
  releases it the same way. `/clear` keeps it (the session goes on).

- **A launch that names a team is in it, or does not start** (D36). Starting a session the
  human launched with `--team` but without the bus would look like success and leave it
  unreachable, the "skip and warn" pattern CLAUDE.md's fail-fast rule bans. So every
  failure to seat a session (a team not running here, a named seat held, a seat that cannot
  be created, a bundle that fails `config check`) stops the launch, naming the cause and the
  ways on: end that session, choose another seat, or launch without `--team`. An unnamed
  launch is never refused for want of a seat, since it creates the next number. Headless
  and restored launches get the same refusals; the exit code and stderr reach whatever
  started them.

- **Hooks** (U30): the mechanism is unchanged, because each seat is its own `PINGBUS_HOME`:
  the watcher, the sync locks, the Stop guard and its memory are per seat. SessionStart's
  context gains one line naming the session's seat and handles, and the checkout's other
  seats with `held` or `free` and their handles, read offline from each seat's lock and
  `member.json`, so a session knows which siblings it can ping; and, when the seat has
  consumed items or outbox entries, that `pingbus history` shows what this seat received and
  sent before. The `seat_held` template stops suggesting another session: a busy sync lock
  now means another process of this seat (a `wait` or `recv` the session started, or a
  host-side run against its `PINGBUS_HOME`). The skill (U18's `SKILL.md`) gains a short
  seats section: the session's own handle, how to find its siblings, `pingbus history` on
  taking over a seat, and that a sibling launched without `--team` is on no team and cannot
  be pinged.

- **Status** (U30): `pingbus status` gains `SEAT` lines when the checkout has a `seats/`
  directory, one per seat: `SEAT`, seat, `held` or `free`, `self` or `-`, its teams, its
  handles (comma-joined), appended to agent-bus-protocol.md §13 and §15 as a new line type.
  On the host, `agent-bus seat list` prints the same lines from the same code (`seat.py`,
  which the `agent-bus` zipapp also carries). The room needs nothing new: each seat is its
  own account, so Element and the `MEMBER` lines show each seat's handle, and its
  `agent_bus.status` `listening` comes from its own watcher.

- **History** (U33, D43; owner answer 2: "an agent who comes back and takes on a role should
  be able to read the historic messages as well"). `pingbus history [--team <team>] [--limit N]` prints what this seat received and sent, newest first, at most `N` items
  (default 50, at most 500). The source is the room, not the local cache, so it is the same
  after a seat was parked and returned (its local state starts again) or taken over from a
  fresh clone: the room's timeline paged backwards with `/messages` (fixture 072), keeping
  the pings this seat sent or that name it in `to`, and the human text that mentions it or
  `@room`, each through the checks `recv` applies (the validator, the sender's role, the
  forge check, D20). The team room is created with the `private_chat` preset, whose
  `history_visibility` is `shared`, and a parked seat never leaves the room, so a returning
  seat reads everything since it first joined. One new line type, `HISTORY` (direction
  `in` or `out`, then the fields of the `PING`, `HUMAN` or `SENT` line it records), goes into
  agent-bus-protocol.md §13 and §15. `history` never moves an item between inbox and
  `consumed/`, never acks, and the skill says a `HISTORY` line is a record, never a request
  to act: work comes from `recv`.

- **Non-ccy members** stay one seat per `PINGBUS_HOME`, with section 5.2's busy rule.
  Several sessions as one agent user means several `PINGBUS_HOME`s, each its own member;
  `agent-bus-claude` claims no seat in v1, since nothing asks for it.

### 5.6 Seats on the host: created by the launch, listed and removed by two commands

Owner answers of 2026-10-07 (D33, D41, D44): provisioning is organic ("I never really
figured that we'd need IAC just to provision seats"), team membership is given at launch,
and "tieing in seat creation to ccy command is excellent idea". So a checkout needs no
setup step: the first `ccy --team <team> [--seat <seat>]` in it creates what it needs, with
one `sudo agent-bus add-member` per new (seat, team) pair.

- **Teams** are created as they already are: by `play-agent-bus.yml` from `agent_bus_teams`,
  or by the human running the standalone installer, `sudo agent-bus-install team --file <team.json>` (section 3.4). Nothing new is needed. A launch names only a team whose
  homeserver runs on this host (D46): `add-member` runs on the homeserver host, and teams
  spanning machines are a later phase.

- **No checkout-level opt-in** (D33). `agent-bus join` and `leave`, `PINGBUS_TEAMS` and
  `PINGBUS_HOST` in `ccy.env.local`, and `agent-bus seat add` are not built: with the team
  given at launch they would only record a choice the launch already makes, and a recorded
  team list is a second source that could disagree with the flag (YAGNI). A role other than
  `worker` is set with the existing `sudo agent-bus set-role <team> <handle> <role>`, the
  handle read from `agent-bus seat list`.

- **Seat commands** (U32), run by the checkout's owner on the host, in the checkout (the
  working directory's git top level). The `agent-bus` wrapper runs them as the calling user
  and refuses them as root, since root never writes in a user's checkout. Each is
  non-interactive apart from sudo's own prompt, prints `CHANGED <what>` marker lines on
  stdout (except `seat take`, whose payload is the seat name) and diagnostics on stderr:

  - `agent-bus seat take [<seat>] --team <team>… [--no-prompt]`, the launcher's call
    (section 5.5). It checks the checkout (a git checkout owned by the user; `.claude/ccy/`
    a real directory; `git check-ignore` of `.claude/ccy/pingbus/`, P8's rule, before any
    token can land) and each team (its homeserver active here). It picks the seat (the named
    one, or the lowest-numbered free one), refuses a held named seat (exit 75), and for each
    named team the seat has no bundle for runs `sudo agent-bus add-member <team> --repo=<repo> --seat=<seat> --host=<role> --type=podman --role=worker --address=<bus_ip> --out=<checkout>/.claude/ccy/pingbus/seats/<seat>/<team>`, a new member or a parked
    one returning (`--no-prompt` runs it as `sudo -n`). `<role>` is the
    `HOOKS_DAEMON_HOSTNAME` that `ccy.env.local` assigns, found by parsing the file, which
    must be a regular file owned by the user and never a symlink; refused when it is not
    assigned, and refused when the seat's existing handles carry a different `<host>`,
    since a seat is one handle in every team. `<repo>` comes from the checkout's remote by
    `registry.repo_from_remote`, the rule `suggest-handle` uses; `<bus_ip>` is `agentbus0`'s
    address (the ccy address, section 5.3), refused when that interface is absent. It
    prints the seat's name on stdout, its payload.
  - `agent-bus seat list`: one `SEAT` line per seat (section 5.5), with its teams and
    handles.
  - `agent-bus seat remove <seat>`, refused while the seat is held: parks its handle in every
    team it has a bundle for, deletes its directory, and deletes `seats/` and then
    `.claude/ccy/pingbus/` when they are left empty, so a checkout whose last seat is
    removed carries nothing of the bus.

  The decisions are pure Python in `helpers/agent_bus/checkout.py` (reading the role from
  `ccy.env.local`, seat picking, the actions for an observed state), run by a thin executor;
  it reaches the root side only through `sudo agent-bus add-member|park-member`, so the
  wrapper's placement rules (section 5.1) hold unchanged.

- **The launcher calls only `seat take`** (U31), and only when the launch has `--team`.
  Without `agent-bus` on the host it refuses, naming `play-agent-bus.yml`, which installs
  `agent-bus-install software` on every desktop.

- **IaC is optional, and v1 adds none.** Nothing needs a play step (YAGNI), and
  `play-agent-bus.yml` and `localhost.yml.dist` gain nothing for checkouts.

## 6. Waking sessions

**Choice: the inbox socket as the primary path, a background `pingbus wait` as the fallback,
a Stop hook as the guard on both.**

- **Primary (socket).** A session started with `crossSessionInbound: accept` passed through
  `--settings` (the ccy entrypoint and `agent-bus-claude` both do it) has
  `CLAUDE_CODE_MESSAGING_SOCKET` and its token. The plugin's SessionStart hook starts
  `pingbus watch` detached (it inherits those variables). The watcher holds every active
  team's sync lock and long-polls every team at once, one thread per team (polling them in
  turn would add up to 30 s per extra team); a team whose lock another process holds is
  reported busy on its own, and the others carry on. It writes valid items to the inbox
  and, whenever an item is pending that was not pending at its last look, writes one
  fixed-template message to the socket (a count alone can hide a `recv` that emptied the
  inbox and a reply that landed just after it):
  "agent-bus: N pending (H from humans, P pings), notice S. Run `pingbus recv`." Only
  counts, never content. `S` is the watcher's own monotonic counter: the socket drops a
  message identical to an earlier one, and without `S` the sequence "1 pending", `recv`,
  "1 pending" would lose the second and leave the session asleep. U01 measured that dedupe
  window (an identical body from the same sender) as longer than 20 s and at most 33 s. The watcher exits when the session's socket goes away, which also
  keeps it running across `/clear`, where SessionEnd fires and the session goes on, so
  `pingbus hook session-end` does nothing and the plugin does not register it (U18). A team that fails (homeserver unreachable, trust lost) is
  reported and dropped like a busy one, and the others carry on; the watcher exits with
  the first failure's code only once no team is left. While it runs it publishes the §8
  `agent_bus.status` `listening` for 600 s, renewed every 300 s; `wait` publishes it until
  its own deadline. Nothing else publishes a status.
- **Liveness is a lock, never a PID.** `/workspace` is shared across container namespaces,
  where a PID in a file means nothing. Each team's `lock` file is held with `flock` by the
  watcher or a waiter, which writes its kind (`watch` or `wait`) into the file, or briefly
  by a `recv` syncing once (kind `recv`, which is not a waker); a waker is live exactly
  when a non-blocking `flock` on `lock` fails and the kind is `watch` or `wait`. A dead holder's lock is released
  by the kernel, so a SessionStart finding the lock free starts a new watcher, and finding
  it held by another process reports it (`seat_held`). In a ccy checkout that process is of
  the same session, or a host-side run against its seat, because a seat's own lock
  (`seat.lock`, kind `seat`, section 5.5) keeps a second session out of it.
- **Why the socket.** It wakes a truly idle session with no discipline from the agent (the
  main weakness of the waiter [wake §1]); it is available now, unlike the supervisor
  plugin route, which needs an unreleased upstream template [wake §3]; latency is the sync
  (about 31 ms after a send [spike]) plus delivery; it works in every encapsulation where
  `claude` is started by our launchers, not only under ccy's supervisor. Its framing
  ("Another Claude session sent a message") is not literally true, but the text is a fixed
  template naming the bus, and the agent then reads validated items through `pingbus recv`.
- **The socket's cost.** It admits any process of the same user in the container or on the
  host. That crosses no boundary: such a process can already edit the session's settings
  and hooks. It needs `--settings` (a project setting is not honoured [spike]); identical
  repeats are dropped (hence `S`) and distinct ones batched, which suits a notice per new item
  (**pending the U01 host run**: on Claude Code 2.1.291 two distinct notices 50 ms apart
  arrived as two entries and two turns, not one batch, and the dedupe window is a
  server-side flag of 0-600 s, 30 s today; see the plan journal). It is a recent
  Claude Code mechanism, so probe U01 pins its behaviour per Claude Code version, and
  `pingbus status` says which wake path is live.
- **Fallback (`wait`).** Where `CLAUDE_CODE_MESSAGING_SOCKET` is absent (a session started
  by hand, an older Claude Code), the skill tells the agent to run `pingbus wait` with
  `run_in_background`: it long-polls and exits on the first batch with at least one valid
  item (or at `--timeout`, default 1500 s, exit 3, "re-arm"). Drops alone never end it. If
  the watcher holds the lock, `wait` exits busy (75) and is not needed.
- **Guard (Stop hook).** Reads only the local inbox and probes each active team's `lock`.
  It re-validates every inbox file offline and ignores failures, blocks once with a
  fixed-template reason carrying only counts when valid items are pending, or when some
  active team's lock is free, so no watcher or waiter covers it ("no waker: run `pingbus wait` in the background"),
  never when `stop_hook_active` is true, and blocks for "no waker" at most once per 600 s so
  it cannot loop against the hooks daemon's own Stop handlers. A broken CLI or bundle gives
  a fixed template naming the failure class, under the same once-only rule. Its memory is
  `PINGBUS_HOME/stop-guard.json` (the pending set it last blocked for, and when it last
  blocked for a failure and for no waker). If that file cannot be written the guard does
  not block, since a guard that cannot remember would block every turn. Every hook exits 0
  with hook JSON, even when reporting its own failure fails.
- **Context.** UserPromptSubmit adds "N pending: run `pingbus recv`"; SessionStart loads
  every active team's bundle offline, starts the watcher when the session has the socket
  and some team has no waker, and reports the wake path, the pending counts and any failure
  class (a missing bundle is the `config` class, pointing at `pingbus config check`); in a
  ccy checkout it also names the session's seat and the checkout's other seats (section 5.5).
- **Delivery to the agent.** `recv` drains the inbox; before printing an item it re-runs
  the offline validator and re-fetches the event with `GET /rooms/{room}/event/{event}`
  (no lock needed), printing from the fetched copy: the inbox is a cache, never the
  authority. Hooks never touch the network.
- **Offline `status` and `inbox`.** `status` reads members and statuses from
  `state/room.json`, which the syncer writes (the first sync, and the sync after a join,
  load every member), roles from the cached team record and the wake path from the lock.
  `inbox` cannot print text without treating the cache as an authority, so it prints only
  `PENDING` lines (team, event ID, sender, time, verb or `human`). In both, a team whose
  state fails is reported on stderr and the others still print.
- **Install route (from [U01]).** A plugin copied into the config directory the phpantom-lsp
  way registered no hooks; user-level `settings.json` hooks and `--plugin-dir` both fired
  SessionStart and UserPromptSubmit. So the plugin is loaded with `--plugin-dir`, which also
  avoids mutating settings files; the U01 rerun confirms Stop and SessionEnd fire that way.
- **Dropped:** the supervisor plugin route (superseded by the socket), session crons and
  the file mailbox [wake §4, §5].

## 7. Messages: agent pings and human text

Both travel as `m.room.message` in the team room (agent-bus-protocol.md §4, §7):

- **A ping** is an `m.notice` whose `body` is a fixed rendering of its fields and whose
  structured form is under the `agent_bus.ping` key. Humans see every ping as a notice in
  any Element client, phone included, with the agents it addresses mentioned; no relay is
  needed. Receivers act only on the structured form, and drop a ping whose `body` is not
  exactly the rendering of it, so nothing can hide in the text.
- **Human text** is an ordinary `m.text` written in Element, addressed by mentioning agents
  (Element's mention pill, which sets `m.mentions`), or `@room` for every agent.

**How an agent tells them apart**, all at the receiver, from data only `admin` controls:

| Sender (by user ID)                                           | Event                                                            | Outcome                                                                                               |
| ------------------------------------------------------------- | ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| a human listed in the team record                             | `m.text` that mentions this agent or `@room`                     | a `HUMAN` line: team, event ID, the human's localpart, time, text (a reply's quoted fallback removed) |
| a human listed in the team record                             | anything else, or not addressed                                  | ignored (not addressed) or dropped (`schema`, `edit`)                                                 |
| a human listed in the team record, bundle `human_text: false` | anything                                                         | dropped (`sender`)                                                                                    |
| an agent holding a role                                       | `m.notice` with a valid `agent_bus.ping` addressed to this agent | a `PING` line (after the forge check)                                                                 |
| an agent holding a role                                       | a message carrying `agent_bus.text` (its text to humans)         | ignored (`agent-text`): not a drop, so no `dropped.log` line, no `DROPPED` count, no exit status      |
| an agent holding a role                                       | any other message (free text without the ping key included)      | dropped (`schema` / `body`): agent text never reaches an agent                                        |
| anyone else (a non-team account, `admin`, the server user)    | anything                                                         | dropped (`sender`)                                                                                    |

The two line types are different on the wire and in the skill: a `HUMAN` line is a request
from that named human, to be weighed as the session's own owner would weigh a teammate's
request; a `PING` is a closed verb about a committed artefact, and the referenced file is a
document to read, never a command. The server cannot stop an agent account posting free
text (power levels are per event type, not per `msgtype`), so a misbehaving agent's text is
visible to humans in Element, under its handle; it reaches no agent. The same holds for an
agent's `@room` (`m.mentions.room` is not limited by the server, section 4): the sender
check drops it before addressing is looked at. Every agent account
receives the room's human text, including text addressed to other agents, so the guarantee
"reaches only the addressed agent" holds at pingbus, not at the account; human text is
trusted input, so this is a privacy limit between teammates, not an injection path, and is
documented. **Quoted agent text.** A human's reply may carry a fallback that quotes the
event it answers (`> <@agent…> text`), which would deliver an agent's words inside a
`HUMAN` line; when `m.relates_to.m.in_reply_to` is present the receiver removes the
leading block of `>`-prefixed lines (and the blank line after it) before delivery.

**Agent text to a human** (owner decision, 2026-10-06). An agent may write free text to a
team human, for example a coordinator answering the owner, with `pingbus say --to HUMAN[,HUMAN…]`, the text on stdin (agent-bus-protocol.md §7, §13): an `m.notice` whose
structured form is the `agent_bus.text` key and whose `m.mentions` names only listed humans
(never `@room`, never an agent). It prints a `SENT` line and draws on the same send bucket
as `send`. **Codes.** On send, the §7 checks refuse with exit 4 and the reason code; the
send-only code `secret` refuses text in which the spec's documented secret-shape set
(private-key blocks and known token formats: GitHub, AWS, Slack, JWT, bearer, Ansible
Vault, URL credentials, credential assignments) finds a match, and U11 adds the member's
own token to what `say` refuses. On receive, every agent **ignores** agent text
(`agent-text`, D30) whatever its addressing: it is traffic for the humans, not a fault, so
it writes no `dropped.log` line, adds to no `DROPPED` count and never makes `recv` exit 6.
The text reaches the human in Element and no agent's context. Like every pingbus rule, the
secret check guards against mistakes and is not a boundary, because an agent can post with
`curl` (section 0). Pings remain the way to
answer with a verb (`ack`, `nack`, `done`, `blocked` may be addressed to a human and may
answer a human's message by `re`).

## 8. Element

- **Desktop:** `playbooks/imports/optional/common/play-agent-bus-element.yml` (optional, not
  imported by `playbook-main.yml`): Element Desktop Flatpak
  `im.riot.Riot` (system-wide, the `play-comms.yml` pattern) and, per team in
  `agent_bus_element_teams` (`team`, `base_url`, optional `server_name`; written by
  `helpers/agent_bus/element.py` as `user_login`),
  `~/.var/app/im.riot.Riot/config/Element-<team>/config.json` with `base_url` the
  team's address (`<bus_ip>` or `<wg_ip>`), the locked-down keys of [clients §1.4]
  (pinned homeserver, `disable_custom_urls`, no well-known lookups, no identity server,
  integrations, Jitsi, Element Call, maps, analytics, sentry, rageshake, URL previews, room
  directory or update URL), a spell-check seed when absent, and a launcher running
  `flatpak run im.riot.Riot --profile <team>`. A container test asserts every key. The
  Flatpak is not confined (D17); P4 proves its traffic.
- **The human's session token is the human.** Element keeps its access token in the
  user's home (`~/.var/app/im.riot.Riot/`), readable by every process of that user, and
  with it anything can post `@room` text that every agent in the team receives as a
  `HUMAN` line. So **no agent runs as a user that holds a human's Matrix session**, in any
  client: bare-desktop (`host`) members run as a dedicated agent user (section 5.2). This is
  enforced where it can be: `agent-bus-claude` and `pingbus config check` for a `host`
  member refuse (exit 78) when the running user has an Element profile directory, and
  `play-agent-bus-element.yml` refuses to configure a profile for a user who has
  `~/.config/pingbus/`. A ccy member runs as the desktop user but sees only the mounted
  checkout, not `~/.var`; P8 proves that from inside a session. A browser-based Matrix client
  cannot be detected and is covered by the documented rule only.
- **Phone:** the human installs Element (or Element X) and WireGuard; the homeserver is
  `http://<wg_ip>:<port>`, logging in with the password from `human password`. Probe H7
  records which phone client accepts plain HTTP over WireGuard, whether it trusts a
  user-installed CA, the homeserver's sync flavour, and whether its mention pill sets
  `m.mentions` (if it does not, a phone message reaches no agent, and agent-bus-protocol.md §7 gains a
  leading `<handle>:` addressing form before v1 is frozen); if no client accepts plain
  HTTP, U26 adds TLS (section 3.3). The phone holds a session token: losing it means
  `agent-bus human lock` then `logout-all`.

## 9. Security model

| Who                                                                                              | Trusted with                                                                                                                                          | Held back by                                                                                                                                         |
| ------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| root on the homeserver host (the operator), or anyone holding its backups                        | everything: the team's root of trust, so it can instruct every member's agent on every host (it can mint a human account or post as `admin`'s record) | nothing (this is the trust root); a member that does not trust it joins with `--no-human-text`, and still acts on pings only within the forge checks |
| a team human                                                                                     | free text to agents; reading the room                                                                                                                 | power 50: no state, invites, kicks or redactions; no server admin; account lock-down (section 4)                                                     |
| anything that can read a human's Matrix session (Element's profile, a browser)                   | the same as that human                                                                                                                                | no agent runs as that user (section 8); `config check` and `agent-bus-claude` refuse a `host` member beside an Element profile; P8                   |
| an agent                                                                                         | its own token; pings in its role; its own status; text to humans (refused when secret-shaped), which every agent ignores                              | receivers' validation; the forge provenance check; no access to `/var/lib/agent-bus`; cannot change membership, roles or the allowlist               |
| code in an agent's checkout                                                                      | the same as that agent (it runs as it)                                                                                                                | the same; it can misdirect its own agent only, as it already can, because it runs as a user that holds no human's session (section 8)                |
| another process of the same user                                                                 | the same as that user's agents (it can edit their settings)                                                                                           | nothing new; the inbox socket adds no boundary                                                                                                       |
| a host inside `allow_from` (a WireGuard peer, a bridge guest, a LAN host if the team allows one) | reaching the port                                                                                                                                     | closed registration, tokens, 32-character passwords, the admin endpoints' HMAC and admin token                                                       |
| anything that can watch a non-private network the team listens on (no TLS in v1)                 | every login, password and token that crosses it, so it can act as those humans and agents                                                             | nothing in v1: choosing the network is the team's decision (section 3.3); TLS is U26, conditional                                                    |
| anything else on any network                                                                     | nothing                                                                                                                                               | listen rules, firewalld, the unit's IP filter                                                                                                        |
| the homeserver process itself                                                                    | its database                                                                                                                                          | the unit sandbox: no connection beyond loopback and `allow_from` (section 3.3), no resolver, writes only its own directory                           |
| a forge user outside the team                                                                    | writing issue and PR text                                                                                                                             | `issue:` is a status pointer only; `pr:` must be same-repo, by an owner, member or collaborator, at its head; content must be on a trusted branch    |

Known limits, documented rather than built around: **joining a team gives the operator of
its homeserver the power to instruct your agent** (`docs/agent-bus.md` says so where it
explains joining a team hosted elsewhere, with `--no-human-text` as the narrower choice;
that switch drops human text only: the operator's `admin` still writes the repository
allowlist, so it can still send the agent to any document in a repository it lists, which
the agent reads as a document, never as instructions);
**v1 has no TLS, so a team listening on a network that is not private (not loopback, not a
host-only bridge, not WireGuard) sends every login, password and token across it in
clear**; backups hold the root of trust; every agent account can read the whole
team room (section 7); an agent can show humans free text under its own handle (allowed,
and refused by pingbus only when secret-shaped; a misbehaving agent can skip pingbus);
members on one private network or bridge can reach each other, and the homeserver can open
connections to them; an agent on a host where it can `sudo` without a password reaches the
root of trust; a human's session in a browser cannot be detected beside a `host` member;
Matrix has no second factor here. The seats of one ccy checkout are not isolated from each
other: every session there can read every seat's token, so a seat is an identity for
coordination, not a boundary (section 5.5). A session cannot choose its own team or seat:
both come from the launcher's flags, and the entrypoint refuses a bus variable set by the
checkout's files (section 5.3). In a checkout with no `ccy.env.local` (so nothing is bound
read-only), a session could create one assigning `HOOKS_DAEMON_HOSTNAME`, which would label
the `<host>` of a seat first created in a team by a later launch; the launcher prints each
handle before it creates the account, and a seat's existing handles fix it thereafter. The forge cache (`forge-cache.json`, protocol §6) lives in
the member's state directory and is trusted as that directory is: the agent it serves can
write it, so a planted positive entry skips the forge check for that member's own sends and
receives only. That weakens nothing beyond what the agent can already do (skip pingbus, or
act on a ping it chose to trust); other members re-check every reference against their own
cache.

## 10. Privacy acceptance checks and how each is proven

All in this plan's `acceptance.bash`, host only, against a dedicated acceptance team
(reserved name `acceptance`, created by `acceptance.bash` itself with `agent-bus-install team` and removed with `remove --purge` on the way out, also after a failure, and first
when an interrupted run left it; U17). Its `host` members are not accounts on the host:
each pingbus command runs as a transient `systemd-run` service with `DynamicUser=yes` and
one `User=` name per member, its `PINGBUS_HOME` the service's `StateDirectory`, so nothing
persists in `/etc/passwd` and no member runs as the desktop user. Each check prints
PASS/FAIL with its evidence file under `untracked/plan-runs/`.

| Id  | Check                                                                                                                                                         | Proof                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| P1  | No outbound traffic from the homeserver                                                                                                                       | Construction: `systemctl show` of the unit gives exactly the rendered `IPAddressDeny`/`IPAddressAllow`, `RestrictAddressFamilies`, the resolver bind and `IPAccounting`; `allow_federation` false and `trusted_servers` empty in the running config. Enforcement on this kernel: a transient unit with the same IP properties fails to reach a public address. Behaviour: during a scripted session (bootstrap, join, 20 pings, acks, human messages) `ss -tnp` sampled for the Tuwunel PID lists only allowed peers and FAILS on any Tuwunel socket whose local port is not `<port>` (an outbound connection, which the filter would still allow towards `allow_from`), and a host capture on port 53 shows no query from the session.                                                                                                                                                                                                                                                                |
| P2  | Reachable only where the team allows                                                                                                                          | `ss -ltnH`: `<port>` bound on exactly `127.0.0.1` and `listen`; firewalld rich rules only for `allow_from`; from a test VM (owner answer 7: counts as another machine) with a route added to each host address, a connection from a source outside `allow_from` fails on every address, and one from an allowed source succeeds. The VM leg reports SKIPPED-NEEDS-OWNER if no VM is available, and the plan cannot close then.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| P3  | Names are not published                                                                                                                                       | `server_name` ends in `.internal`; `getent ahosts <sn>` fails; the P1 capture shows no query for `<sn>` or `<team>`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| P4  | Element's team profile talks only to its HS                                                                                                                   | The scripted Element session (start, log in, open the team room, receive a ping notice, send an addressed message, idle 120 s) runs as `pasta --pcap <file> -T <port> -- flatpak run im.riot.Riot --profile acceptance`; FAIL on any packet but the forwarded connection to the homeserver. Plus the static key check of section 8.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| P5  | Secrets never reach logs                                                                                                                                      | For every secret (shared secret, admin token and password, member tokens, the human password the script set with `human password`), a `grep -rqF -f` over: the backup tar's member list (no human password file), the deploy and installer logs, `untracked/plan-runs/`, `journalctl` for the units, `systemctl show` output, every pingbus stdout and stderr captured, the transcript and state trees of the sessions used, the socket messages sent, shell history, the Element profile's logs. Reports the file name of a hit only.                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| P6  | pingbus delivers agent text to no agent, human text only to the agents it addresses, non-team text to none (every account still receives the room: section 7) | During the session: an agent sends text to the human with pingbus (the human sees it in Element) and a secret-shaped text is refused; an agent account posts, with raw `curl`, an `m.text`, an `m.text` with `m.mentions.room: true`, a notice whose `body` differs from its ping, and a ping-less notice; the human posts one message addressed to agent A only, one `@room`, a reply to an agent's ping whose fallback quotes that ping, and a forged ping notice; a non-team test account (made by `admin`, invited for the test) posts text; a human removed from the team record posts an addressed message. Member C's bundle has `human_text: false`. Then A's, B's and C's `pingbus recv` output and inbox files: A has the addressed message, the `@room` and the reply with the quote removed; B only the `@room`; C none of them; none has any agent, non-team, removed-human or forged-ping text; each `dropped.log` names the drops and none lists the `say` text (ignored, not dropped). |
| P7  | Admin endpoints and the human's limits hold                                                                                                                   | `/_synapse/admin/v1/register` with a wrong MAC is refused from a member; a member token on an admin endpoint is refused; the admin tool refuses a non-loopback base URL; the running config has `login_via_token` and `login_via_existing_session` false, and `POST /_matrix/client/v1/login/get_token` from the human's session is refused (Tuwunel 1.9.3 lists `m.login.token` in `GET /login` regardless, H4, so the flow list is recorded, not asserted); the human's token cannot set state, invite or redact in the team room, and cannot join the admin room.                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| P8  | Bundles stay private; no agent holds a human's session                                                                                                        | Every bundle path the acceptance members use, the ccy seats' `.claude/ccy/pingbus/seats/<seat>/<team>/` in this repository's own checkout among them (U20), is `git check-ignore`d in its checkout; from inside a ccy acceptance session the Element profile directory is not reachable; with an Element profile directory planted in a test agent user's home, `pingbus config check` for that user's `host` bundle and `agent-bus-claude` both refuse (78); `play-agent-bus-element.yml` refuses a user with `~/.config/pingbus/`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |

Every leg of P1-P8 and of U23 that needs something the host lacks (a VM, docker, LXC)
reports SKIPPED-NEEDS-OWNER, never PASS, and the plan cannot close while any leg is
skipped. U24's WireGuard leg is a later phase (D46) and is not run in v1.

## 11. Where each thing can be verified

| Container (this checkout, no podman, no systemd)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Host only, through `meta-deploy.bash`                                                                                                                                     |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| U02-U15, U18, U19, U29-U33 against fakes built from H4's recorded responses: validator, limits, config, CLI, inbox, forge, Matrix client, syncer, hooks, watcher and socket client (against a fake socket), zipapp, team file, registry, admin tool, renders, plugin contract, ccy entrypoint; seat handles, park and return, the seat claim between real processes (`flock`), `seat exec`, `history`, the launcher's seat picking, `--team`/`--seat` and headless Quick Launch, `agent-bus seat take`/`list`/`remove` against a scratch checkout and a fake `sudo agent-bus` | The installer for real: user, binary, units, sandbox, firewalld, NetworkManager, readiness (U16), then the play (U22)                                                     |
| `ruff`, `qa-helper-tests.bash`, `qa-python.bash`, `bash -n`, `ansible-playbook --syntax-check` for the plays, `scripts/test-agent-bus-install.bash` (the installer's pure parts against a temporary root), `scripts/test-ccy-agent-bus.bash`, `scripts/test-ccy-env-local-dist.bash`                                                                                                                                                                                                                                                                                          | Tuwunel itself (H3-H5), then M1 for real; reachability from containers, guests and a VM (H1, H2, P2)                                                                      |
| Claude Code behaviour: plugin loading and the inbox socket (U01, a child `claude` on the ccy token, as `CLAUDE_CODE_OAUTH_TOKEN`; on the host)                                                                                                                                                                                                                                                                                                                                                                                                                                | Element desktop (H6, P4); the phone (H7, the owner's phone); ccy sessions in this checkout launched with `--team` onto seats the launch creates, woken for real (M2, U20) |

## 12. Build order

Each unit is one agent, on its own branch, tests first (the TDD hook enforces it). A unit
lists the units it needs; units with no path between them run in parallel. "C" = fully
verifiable in the container, "H" = needs a host run through `meta-deploy.bash`.

**Wave-1 branches.** `wf-f0f65b6e-87f-2-30211d13` (U01 plugin probe): reused as U01's base;
its finding already decides the install route (`--plugin-dir`), and U01 extends it with the
Stop/SessionEnd and inbox-socket legs; the wave-1 run had no child credential, so Stop
never fired and the socket's "one turn starts" leg needs a logged-in session.
`wf-f0f65b6e-87f-3-061c2adc` (U02 protocol and validator): **not merged**. That branch
turned `PROTOCOL.md` into a stub and wrote its own protocol doc under the old name, so U02
takes only `helpers/` (`protocol.py`), `tests/` and the `link_check.py` code-span fix from
it, and writes `docs/agent-bus-protocol.md` afresh from this `PROTOCOL.md`. What stands in
the taken code: the reference grammar, forge-facing reference forms, handle and ID
grammars, verb table, `Refusal` and content checks, `test_protocol.py`'s table style and
`test_protocol_doc.py`'s doc-equals-constants pattern; the namespace, the event shape, the
room/control/roles markers, the note, `on_behalf_of` and the warden sender class are
replaced. `wf-f0f65b6e-87f-1-be409213` (U00 test only):
the shared-secret MAC, scrubber, log-scan and pcap tests are reused in U00; the namespace,
subnet-picker, room-pair power-level and old `tuwunel.toml` tests are dropped or re-pointed.

| Id  | Title                                                                           | Creates / changes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | Tests                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Needs                   | Where                              |
| --- | ------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------- | ---------------------------------- |
| U00 | Host probes                                                                     | `triage.bash` + `triage_probe.py` (H1-H6, section 13), scrubbed Tuwunel responses for U08                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | `test_triage_probe.py` (from the wave-1 branch, re-pointed); results journalled                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | -                       | H                                  |
| U01 | Claude Code probes                                                              | the wave-1 probe scripts, extended: Stop and SessionEnd under `--plugin-dir`; a session with `--settings` `crossSessionInbound: accept`, a hook-spawned detached process writing to the socket                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | which hooks fired; the socket's wire format; one turn started; framing; identical repeats dropped and the dedupe window's length, distinct ones batched; behaviour without bypass mode; checker table test                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | -                       | C, or H without a child credential |
| U02 | Protocol spec and pure validator                                                | `docs/agent-bus-protocol.md` (written from `PROTOCOL.md`), `helpers/pingbus/protocol.py` (taken from the wave-1 branch, not merged)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | `test_protocol.py`: every verb x ref form, grammars, the handle separator as one constant, canonical body, mentions equality, human-message rules (addressing, `@room` only from humans, reply-fallback removal, edits, size), team record and status parsing (state key must equal sender, size cap), power-level equality, every drop reason, agent text to a human (mentions only listed humans, secret-shaped text refused); `test_protocol_doc.py`                                                                                                                                                                                                                                                                                                                                                                                                                              | -                       | C                                  |
| U03 | Limits                                                                          | `helpers/pingbus/limits.py`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `test_limits.py`: token bucket, duplicate window, receive flood, ack deadlines, stale ping and human ages, out-of-bounds overrides refused; injected clock                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | U02                     | C                                  |
| U04 | Member config and bundles                                                       | `helpers/pingbus/config.py`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `test_config.py`: schema incl. `human_text`, token file relative to the bundle and mode > 0600 refused, plain HTTP only to listed IP literals, `PINGBUS_HOME`/`PINGBUS_TEAMS` resolution, multi-team rules, Python version gate, a `host` bundle refused beside an Element profile                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | U02                     | C                                  |
| U05 | CLI offline parts                                                               | `helpers/pingbus/cli.py` (dispatch, exit codes, line formatter, `validate`, `config check`, `suggest-handle`, `version`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | `test_cli_offline.py`: subprocess runs for 0, 4, 64, 78; stdout/stderr split; exit-code table == doc; `validate --event` reports an agent text as ignored (`agent-text`), never as a drop; `suggest-handle` uses `HOOKS_DAEMON_HOSTNAME` only and refuses without it                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | U02, U04                | C                                  |
| U06 | Inbox, outbox, lock                                                             | `helpers/pingbus/inbox.py`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `test_inbox.py`: event ID validated before it names a file, dedupe, atomic writes, per-team dirs, re-validation on read, sync token saved after inbox fsync, second locker busy, liveness by non-blocking `flock` with the holder's kind in the file, outbox ack tracking and TIMEOUT, the send gate (U03's token bucket and duplicate window) saved between `send` processes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | U02, U03                | C                                  |
| U07 | Forge check and provenance                                                      | `helpers/pingbus/forge.py`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `test_forge.py` with an injected opener (as the reviewed design; unchanged rules)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | U02, U04                | C                                  |
| U08 | Fake homeserver                                                                 | `tests/helpers/pingbus/fake_client_api.py`, `fake_admin_api.py`, `fixtures/tuwunel/` (from H4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `test_fakes.py`: replays every recorded flow; enforces `@` state keys and power levels per event type, and lets a power-0 member write `agent_bus.status` under a non-`@` key (as the server does)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | U00                     | C                                  |
| U09 | Matrix client                                                                   | `helpers/pingbus/matrix.py`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `test_matrix.py`: bearer header unredirected, no proxy, no redirects, txn ID reuse, 401/403/429 mapping, token absent from every error                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | U04, U08                | C                                  |
| U10 | Sync engine and room trust                                                      | `helpers/pingbus/syncer.py`; `inbox.py` `team.json`, `untrusted.json` (the loss-of-trust reason `status` reads), `dropped.log`; `matrix.Client.leave`; recording received acks and nacks in the outbox                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `test_syncer.py` vs fakes: filter, first sync takes `next_batch` only, gap fill (bounded pages), invite rule (others rejected with `leave`), team-record and power-level verification (create event ID = room ID, fixture 034) and loss of trust (reason saved, cleared on re-trust; statuses of members dropped from `roles` pruned), both receive pipelines, `human_text: false`, reply fallback removed, an agent's `@room` dropped, a foreign-key status ignored, forge on receive, drops logged, status written; a missing team record or room (U09's `matrix.NotFound`, exit 7 by itself) maps to exit 10                                                                                                                                                                                                                                                                      | U06, U07, U09           | C                                  |
| U11 | CLI core: send, say, recv, wait                                                 | `cli.py`: `send`, `say` (agent text to humans, section 7), `recv`, `wait`, multi-team (one long-poll thread per team)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | `test_cli.py`: one test per exit code; only `PING`/`HUMAN`/`TIMEOUT`/`SENT` on stdout; recv re-fetches before printing; `wait` not ended by drops; `say` reads stdin, takes humans only, refuses `secret` and the member's own token (exit 4) and shares the send bucket; a received agent text adds no `DROPPED` count and no exit 6; one busy team does not block the others; token on neither stream                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | U05, U07, U10           | C                                  |
| U12 | Hooks, watcher, socket, status (seat lines: U30)                                | `helpers/pingbus/hooks.py`, `helpers/pingbus/notify.py` (socket client, wire format from U01), `cli.py`: `watch`, `hook …`, `inbox`, `status` (members and roles from the cached team record, wake path, unexpected members)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | `test_hooks.py`, `test_notify.py`, `test_cli_status.py`: block-once rules, no-waker throttle, `stop_hook_active`, planted inbox text never printed, counts-only templates with a rising notice number, notify only when a new item is pending (a recv then a new item at the same count still notifies), lock-based liveness (a stale PID file means nothing), exit on socket loss; one failing team neither ends the watcher nor hides the others in `status`; an unwritable guard file never blocks and the hook still exits 0; no network in hooks; every printed status field grammar-validated                                                                                                                                                                                                                                                                                  | U01, U06, U11           | C                                  |
| U13 | Zipapps                                                                         | `helpers/pingbus/bundle.py` (builds `pingbus` and `agent-bus`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `test_bundle.py`: byte-identical rebuild, every module included, archives run `version`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | U05                     | C                                  |
| U14 | Team file, registry, handles                                                    | `helpers/agent_bus/teamfile.py`, `registry.py` (`/var/lib/agent-bus/<team>/registry.json`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `test_teamfile.py`, `test_registry.py`: schema, address and CIDR shape rules, `<n>` never reused, round trip, `--host` required when no role is given                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | U02                     | C                                  |
| U15 | Admin tool and renders                                                          | `helpers/agent_bus/admin.py`, `render.py`, `cli.py`; wrapper `files/usr/local/bin/agent-bus`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | vs fake admin API: HMAC, bootstrap idempotent with a single server admin, human and member accounts created with a discarded password and no `admin` key, `human password` sets and prints once and stores nothing, team room levels and record exactly agent-bus-protocol.md §8, `add-member` writes the bundle as a tar on stdout and the wrapper places it with `install -o "$SUDO_UID"` (modes asserted), `--no-human-text`, remove/rotate-token/human commands, sync-team on change, a failed removal retried by the next run; `tuwunel.toml` key set, each key in the vendored example config; drop-in render (IP filter, restart policy, device dependencies); wrapper refuses an existing `add-member --out` and a symlinked or foreign `rotate-token --out`; no secret on any stream but the three payloads                                                                 | U09, U13, U14           | C                                  |
| U16 | Installer                                                                       | `files/usr/local/sbin/agent-bus-install`, `files/etc/systemd/system/agent-bus-*`, `files/usr/local/share/agent-bus/{tuwunel.pin,resolv.conf}`, the version-pin registration                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `scripts/test-agent-bus-install.bash` (argument parsing, malformed and wildcard listen addresses and malformed CIDRs refused, file layout in a temporary root, no download when the pinned hash matches, no restart and no `CHANGED` when nothing rendered changed, a unit that fails to start, as an unknown key makes it, fails the readiness step); host: `software` and `team` on the desktop, a second run reports nothing changed, `check`, `remove`                                                                                                                                                                                                                                                                                                                                                                                                                           | U00, U15                | C + H                              |
| U17 | M1: host-to-host                                                                | `acceptance.bash` first slice, against the installer run directly                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | host: two `host` members (transient `DynamicUser` services, section 10) of the acceptance team: `review` sent, received by `wait`, `ack`; `TIMEOUT` when unanswered; a human message (as the human account, by `curl`) delivered only by the addressed member's pingbus                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | U11, U16                | H                                  |
| U18 | Plugin, skill, launcher (skill's seats section: U30)                            | `files/opt/claude-yolo/optional/agent-bus/{plugin/pingbus/**,settings.json,agent-bus-claude}`; `agent-bus-install software` copies them into the kit (installer test extended, drift pairs added)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | `test_plugin_contract.py`: `hooks.json` commands are real `pingbus hook` subcommands; SKILL.md names only real commands and carries the HUMAN/PING rules; launcher argv test; launcher refuses beside an Element profile                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | U12                     | C                                  |
| U19 | ccy: image, entrypoint and the dist (reshaped by U31: the team given at launch) | `Dockerfile`, `entrypoint.sh`, `play-claude-yolo.yml` (stages, builds the zipapp); `lib/common.bash` `ccy_env_local_dist_text` gains the commented `PINGBUS_TEAMS` block and `CCY_ENV_LOCAL_DIST_VERSION` goes up; `CCY_VERSION` and container version bumps, changelog, `docs/ccy.md` rows                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `scripts/test-ccy-agent-bus.bash` (a `qa-all.bash` gate): opt-in from `ccy.env.local` only, refusal on a broken bundle, args added inside the wrapper's `--`, inert when unset; `scripts/test-ccy-env-local-dist.bash` extended for the new block and version; U21's `README.podman` (entrypoint opt-in, refusal) checked against the entrypoint as built                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | U13, U18                | C                                  |
| U20 | M2: ccy seats in this checkout, woken                                           | `acceptance.bash` slice M2, reworked: `_acceptance-u20.inc.bash` (`ccy --team acceptance` launches in this checkout, named and unnamed, a plain `ccy` launch, `agent-bus seat list`/`remove`, `set-role`, containers by `ccy-seat` label), `u20_check.py` (transcripts by session ID, the notice-before-turn judgement, `HISTORY` lines, the checkout-unchanged check)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | host, in this repository's own checkout (section "U20" below), with no checkout setup step: three headless ccy sessions at once, launched with `--team acceptance` on role seats that their first launch creates (M2.0); a `--seat` without `--team` refused; two of them exchange `review` and `ack`, the idle one woken by the socket, including a second notice with the same count; one whose watcher is gone is woken by `wait` after the Stop guard; a human message reaches the addressed one; a launch on a held seat is refused; the seats free at session end; a later session in a seat is the same member and reads its history, also after the seat was removed and added again; unnamed `--team` launches take seats 1 and 2 and then 1 again; a plain `ccy` launch takes no seat and has no bus; `seat remove` of every acceptance seat leaves the checkout as it was | U17, U19, U29-U33       | H                                  |
| U21 | Non-ccy members and docs                                                        | kit README per type; `docs/agent-bus.md` (teams, placement convention, installer for other projects, joining per type, the dedicated agent user, what joining a team hosted elsewhere grants and `--no-human-text`, backups as the root of trust, human lock-down, limits), `docs/README.md` index                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | `qa-docs.bash`; bundle README per type                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | U15, U18                | C                                  |
| U22 | Play                                                                            | `playbooks/imports/play-agent-bus.yml` (imported by `playbook-main.yml`), `localhost.yml.dist` placeholder, drift pairs                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | `ansible-playbook --syntax-check` as a gate; host: play run with a scratch team present then absent, a second run reports no change                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | U16                     | C + H                              |
| U23 | M3a: other encapsulations, same host                                            | `acceptance.bash` slice                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | host: an LXC, a docker (docker CLI) and a VM member join the desktop's team and ping; each leg SKIPPED-NEEDS-OWNER when its engine or a VM is missing, which blocks plan close                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | U20, U21, U22           | H                                  |
| U24 | M3b: another host (**later phase**, D46; not built in v1)                       | `acceptance.bash` slice                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | host: the standalone installer run inside a Fedora test VM; a member on the desktop pings that VM's team over a WireGuard link with a fixed test name that the script creates, removes in an `EXIT` trap, and removes at start if a failed run left it; SKIPPED-NEEDS-OWNER without a VM                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | U23                     | H                                  |
| U25 | Element desktop                                                                 | `play-agent-bus-element.yml`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | `config.json` key test; refusal for a user with `~/.config/pingbus/`; `--syntax-check`; host: profile resolves, launcher works                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | U00, U22                | C + H                              |
| U26 | TLS (only if H7 needs it)                                                       | the installer's TLS: a team-private CA, or the route the owner picks (section 3.3)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | decided with the unit, if built                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | U16, H7                 | C + H                              |
| U27 | M4: deploy and acceptance                                                       | `deploy.bash`, `acceptance.bash` (P1-P8, backup and restore round trip), `meta-deploy.bash` entry                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | host run                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | U23, U25 (U26 if built) | H                                  |
| U28 | Review and end-to-end                                                           | fixes only                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `qa-all.bash` (coordinator), `qa-reviewer`; the PLAN.md success criteria                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | all                     | H                                  |
| U29 | Seat handles, registry v2, park and return                                      | `helpers/pingbus/protocol.py` (handle `<seat>`: `[1-9][0-9]{0,5}` or `[a-z][a-z0-9]{0,11}`; `Handle.seat`, `format_handle` taking a seat), `docs/agent-bus-protocol.md` §3 (and §8's examples), `helpers/agent_bus/registry.py` (v2: `parked`, explicit seats, the counter key renamed `prefix`, v1 loaded as v2), `admin.py`/`cli.py` `add-member --seat` (a new seat, or a parked one returning) and `park-member`, the `agent-bus` wrapper's usage line                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `test_protocol.py` (both seat forms, every pre-U29 handle still parses, `-`/`_`/`.` and a leading `0` refused), `test_protocol_doc.py`; `test_registry.py` (v1 file loads with `parked` empty, park then return, the counter skips an explicitly issued number); `test_admin.py` (`--seat` builds the handle; a parked handle returns with the same account and role and a new token, the old token refused; a current, unparked handle and a deactivated one refused, no account created then; `park-member` revokes the token and keeps role and room membership)                                                                                                                                                                                                                                                                                                                  | U14, U15                | C                                  |
| U30 | Seat claim, status and hooks                                                    | `helpers/pingbus/seat.py` (seat names, a checkout's seats read from its directory, the lowest free numbered seat, claim, probe, `seat exec`, `SEAT` lines; carried by the `agent-bus` zipapp too), `inbox.py` (`acquire_lock`/`probe_lock` on a path; kind `seat`), `cli.py` (`seat exec`, `SEAT` lines in `status`), `hooks.py` (seat context at SessionStart, with the `history` pointer; `seat_held` reworded), the plugin's `SKILL.md` seats section; agent-bus-protocol.md §12-§15 (seat layout, `PINGBUS_SEAT`, `seat exec`, `SEAT` lines, exit 75 for a held seat)                                                                                                                                                                                                                                                                                                                                                                                   | `test_seat.py`: `PINGBUS_SEAT` missing, malformed or with no directory (78), a seat held by another real process (exit 75, not waiting), the lowest free numbered seat (a held number skipped, a gap filled, a role seat never picked), the descriptor survives `exec` and the lock is free once the last holder exits, `config check` refusal (78), environment set for the child; `test_cli_status.py` (`SEAT` lines, `self`); `test_hooks.py` (seat line at SessionStart, offline); `test_plugin_contract.py` (the skill names only real commands)                                                                                                                                                                                                                                                                                                                                | U12, U18, U29           | C                                  |
| U31 | ccy: `--team`, `--seat`, headless Quick Launch                                  | `claude-yolo` (`--team` repeatable and validated, `--seat` validated and refused without `--team` (exit 64); with `--team`, the seat picked and created by `agent-bus seat take --team …` (`--no-prompt` when headless) and passed with the teams as `PINGBUS_TEAMS` and `PINGBUS_SEAT`; without `--team`, no bus variable passed and nothing called; label `ccy-seat=<seat>`; `--team …` and `--seat <picked seat>` kept in restart and restore arguments; neither a Quick Launch choice; the flag list; headless with no launch-choice flag takes `.last-launch.conf` without the prompt, refused when there is none, and a key needing a passphrase refused before launch naming `ssh-add`), `entrypoint.sh` AGENT-BUS block (section 5.3: bus variables only from the launcher), `lib/common.bash` dist text (version 3, U19's `PINGBUS_TEAMS` block removed), `CCY_VERSION` minor bump, container version bump, `docs/ccy-changelog.md`, `docs/ccy.md` | `scripts/test-ccy-agent-bus.bash` reworked: a bus variable set or changed by `ccy.env` or `ccy.env.local` refused, `PINGBUS_TEAMS` without `PINGBUS_SEAT` and the reverse refused, inert when neither is passed, `seat exec` in front of both final `exec` lines; `scripts/test-ccy-env-local-dist.bash` (version 3, no bus block); a launcher test with a fake `agent-bus`: `--seat` without `--team` refused before anything runs, `seat take` called only with `--team` and with every named team, its refusal stopping the launch, a plain launch passing no bus variable even in a checkout with seats, the label and the restore arguments (plain stays plain), `agent-bus` missing refused; headless Quick Launch with and without a saved file (no `read` reached), a passphrase key refused                                                                                 | U19, U30                | C                                  |
| U32 | Host seat commands for a ccy checkout: take, list, remove                       | `helpers/agent_bus/checkout.py` (pure: the role read from `ccy.env.local`, seat picking, the actions for an observed state; its thin executor), `cli.py` (`seat take`, `seat list`, `seat remove`), the `agent-bus` wrapper (those run as the calling user and are refused as root; the rest unchanged), `docs/agent-bus.md` and the kit's `README.podman` (a ccy session joins a team only by `ccy --team`, seats, reuse, `--seat`, `history`; U19's `ccy.env.local` opt-in text removed)                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `test_checkout.py`: the role parsed from `ccy.env.local` (never sourced), refused when unassigned, assigned twice, symlinked or foreign-owned, and when a seat's existing handles carry another `<host>`; seat picking; the `add-member` and `park-member` calls for each state (new, a named team's bundle missing, complete, parked, held), only for the teams named; a team not running here refused; `seat remove` deletes `seats/` and `pingbus/` once empty; a second identical `seat take` creates nothing. The wrapper's test: user commands refused as root, admin ones refused without it. `test_member_docs.py`                                                                                                                                                                                                                                                           | U16, U29, U30           | C (host: in U20)                   |
| U33 | Seat history                                                                    | `cli.py` `history`, `syncer.py` (backward paging through `/messages`, filtered to the seat, every event through the receive checks), agent-bus-protocol.md §13 and §15 (`HISTORY` lines), the skill's line on it                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | `test_cli_history.py` against the fake homeserver: sent and received items newest first, only those naming or sent by the seat, the limit (default and cap), a forged or role-less event never printed, nothing moved between inbox and `consumed/`, a seat returned with fresh local state still sees its earlier items                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | U11, U29                | C (host: in U20)                   |

Deferred (no success criterion needs them): `agent-bus rotate-admin`, `pingbus show`,
`peers` and `tail`.

Parallel waves: {U00, U01, U02} → {U03, U04, U08, U14} → {U05, U06, U07, U09} →
{U10, U13} → {U11, U15} → {U12, U16} → {U17, U18, U22} → {U19, U21, U25, U26 if needed} →
{U23, U29} → {U30, U33} → {U31, U32} → {U20} → {U27} → {U28}; U24 is a later phase (D46)
and is in no v1 wave. (U23 was built
before the seats design; it needs U20 only for its place in the original order, so its host
run does not wait for U29-U33.)

### Milestones

| Milestone                                   | Units                      | Proven by                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------------------------------------- | -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0 probes                                   | U00, U01                   | journalled probe results; fixtures recorded                                                                                                                                                                                                                                                                                                                                                     |
| M1 host-to-host ping                        | U02-U11, U13-U17           | U17: a real Tuwunel from the installer, two members, a ping, a human message                                                                                                                                                                                                                                                                                                                    |
| M2 ccy members, woken                       | U12, U18-U20, U29-U33      | U20: three ccy sessions at once in this repository's own checkout, each launched with `--team` onto a seat the launch creates, ping each other, the idle one woken by the socket; a held seat refused; a later session in a seat continues as its member and reads its history; numbered seats reused; a plain `ccy` is on no team; removing the acceptance seats leaves the checkout as it was |
| M3 other encapsulations and hosts, the play | U21-U23 (U24: later phase) | U23: LXC, docker, VM members on this host; the play's host run (U22). U24 (the standalone installer on another machine, over WireGuard) is not part of v1 (D46)                                                                                                                                                                                                                                 |
| M4 Element, deploy and acceptance           | U25-U28                    | P1-P8, backup and restore, the PLAN.md success criteria                                                                                                                                                                                                                                                                                                                                         |

### U20: M2 in this repository's own checkout

Owner decisions 2026-10-07 (D34, D44): U20's ccy sessions run in the real local
fedora-desktop checkout, several at once on distinct seats, each launched with
`--team acceptance`, and the checkout comes out exactly as it went in (`ccy.env.local`,
`.claude/ccy/pingbus/`, `git status`, `HEAD`). There is no checkout setup step: the first
launch into each seat creates it. This replaces the throwaway `git init` checkouts.

- **Where and who.** `PLAN_REPO_ROOT`, the checkout `acceptance.bash` runs from. The
  acceptance team as now (created by `acceptance.bash` with the installer, after M1, purged
  on the way out). Three role seats: `acca` (orchestrator), `accb` and `accc` (workers);
  role seats, so no unnamed launch could take one. Later, numbered seats `1` and `2`
  (M2.9). Their handles' `<host>` is this checkout's own role (the `HOOKS_DAEMON_HOSTNAME`
  its `ccy.env.local` assigns, section 5.2), so nothing is written into the checkout to
  name them; the evidence holds the handles and stays under `untracked/plan-runs/`.
- **The sessions.** Launched one after another (so no two pick a seat at the same moment),
  then running at once, from the repository root:
  `ccy --headless --no-restore --no-supervise --team acceptance --seat <seat> --prompt <text> -- --input-format stream-json --output-format stream-json --verbose --model haiku`,
  stdin a fifo as now. No `--token`, `--ssh-key`, `--no-ssh` or `--no-network`: U31's
  headless Quick Launch takes this checkout's `LAST_TOKEN`, `LAST_SSH_KEYS` and
  `LAST_NETWORK` without asking, so the sessions run with the owner's own token and SSH
  identity and the harness never handles a secret (it reads only `LAST_TOKEN`'s name, for
  the scrub, as now). `--no-restore`: no acceptance session comes back after a reboot.
  Every container is found by its `ccy-seat=<seat>` label, never by the project label,
  which the owner's own sessions in this checkout carry too.
- **M2.0, seats created by the launch.** Before it, the harness records `ccy.env.local` (or
  its absence), the list of `.claude/ccy/pingbus/` and `git status --porcelain=v1` with
  `HEAD`, and refreshes sudo's credential (`sudo -v`), so each first launch creates its
  seat with `sudo -n`. The first launches of `acca`, `accb` and `accc` create them, each in
  the acceptance team only; then `sudo agent-bus set-role acceptance <acca's handle> orchestrator`, before any session is given its orders. Checks: `ccy.env.local` unchanged;
  each seat's bundle directory is 0700 and its files 0600, owned by the owner,
  `git check-ignore`d; `sudo agent-bus list` shows exactly three new members with the
  expected roles; `agent-bus seat list` shows each held, in team `acceptance` only.
- **The orders** tell each session to run only `pingbus` commands, never to edit, commit or
  push, and to end every turn with `STOPPING BECAUSE: waiting on the agent team bus`, which
  the hooks daemon in this checkout requires of a stop.
- **Checks** (each judged from the room's events, `agent-bus seat list` and `sudo agent-bus list` on the host, the containers' labels, and the sessions' transcripts, never from
  prose):
  - **M2.1** `acca` is told to send `accb` a `review`: two seats of one checkout. `accb`,
    given no input after its orders, acks it. Judged by the ping and the ack in the room and,
    in `accb`'s transcript, the watcher's notice preceding the turn that ran `pingbus recv`.
    The hooks daemon may add turns of its own, so a turn count alone is never the evidence:
    "no harness input after the orders" is (its `stdin.jsonl` holds one line).
  - **M2.2** the same-count second notice, as built.
  - **M2.3** the watcher-lost fallback. Once `accc`'s watcher is live, the harness stops it
    (`podman exec` into the container labelled `ccy-seat=accc`, a signal to the watcher
    process found by its argv), then sends `accc` its orders, which give it nothing to do. At
    that turn's end the Stop guard blocks once ("no waker"), and `accc` runs `pingbus wait`
    in the background as the skill says; M1's member a sends it a `review`; the waiter ends
    and `accc` acks; no notice in `accc`'s transcript after the stop. This replaces
    `CLAUDE_CODE_HARBOR_KITE=0`, which needed a tracked `ccy.env` written into the checkout
    and an undocumented Claude Code switch: ccy always gives a socket now, so the fallback a
    ccy session really meets is a watcher that ended.
  - **M2.4** a human message mentioning `acca` only, as built.
  - **M2.5** while the three run, `agent-bus seat list` shows all three held, and
    `ccy --headless --no-restore --team acceptance --seat accb …` exits 75 with the
    held-seat refusal before any container starts; `ccy --headless --no-restore --seat accb …` with no `--team` exits 64 with the "add --team" refusal, and `agent-bus seat list` and `sudo agent-bus list` are unchanged by either.
  - **M2.6** after the sessions end, `agent-bus seat list` shows the three free again.
  - **M2.7, a seat outlives its session** (owner answer 2). A new session launched with
    `--team acceptance --seat accb`, told to run `pingbus history` and stop: the same handle (`sudo agent-bus list` shows the same
    members as before, no new account), and its transcript holds a `HISTORY in` line for
    `acca`'s M2.1 review and a `HISTORY out` line for its own ack.
  - **M2.8, removed and added again.** `agent-bus seat remove accc` (its directory gone;
    `sudo agent-bus list` still shows `accc`'s handle with its role), then a new session by
    `ccy --team acceptance --seat accc`, which returns the seat (no new member), told to run
    `pingbus history` and stop: its transcript holds a `HISTORY in` line for M2.3's review
    and a `HISTORY out` line for its ack, read from the room after its local state started
    again. This is also the host proof that the team room's history is visible to a
    returning seat (section 5.5).
  - **M2.9, numbered seats are reused** (owner answer 2). Launches with `--team acceptance`
    and no `--seat`, each told to run `pingbus status` and stop: one, then a second once
    `agent-bus seat list` shows seat `1` held; their containers are labelled `ccy-seat=1`
    and `ccy-seat=2`. After both end, a third such launch is labelled `ccy-seat=1` again,
    and no seat `3` exists.
  - **M2.10, a plain `ccy` is on no team** (D44). While the acceptance seats exist, a launch
    with the same arguments but no `--team` and no `--seat`, told to run `pingbus status`
    and stop: its transcript's tool result shows `pingbus` not found (exit 127), its
    `init` line lists no `pingbus` plugin or skill, no container is labelled with a seat it
    took, and `agent-bus seat list` and `sudo agent-bus list` are unchanged.
  - **M2.11** after the cleanup below: `ccy.env.local`, the list of `.claude/ccy/pingbus/`,
    `git status --porcelain=v1` and `HEAD` are as recorded before M2.0. The sessions ran
    with bypass permissions in the owner's checkout; their orders forbid edits, and this
    check proves none happened. `ccy.env.local` is also checked unchanged at M2.0, since no
    step of the design writes it.
- **Cleanup**, on success and through `plan_on_cleanup` on any early stop, in this order:
  close each session's input, then stop any container still labelled with an acceptance
  seat; keep the evidence: each session's transcript, found by the `session_id` in its
  stream's `init` line, is moved from `.claude/ccy/projects/` into the run directory (the
  owner's own transcripts there are never read), with the watcher logs;
  `agent-bus seat remove` for each acceptance seat (`acca`, `accb`, `accc`, `1`, `2`; each
  parked, its directory deleted, and `seats/` and `.claude/ccy/pingbus/` deleted once
  empty); then the team purge, as now. The ccy token's value is scrubbed from the evidence
  as built.
- **What the owner must have**, each checked before M1 spends time, failing with an
  `OWNER:` line: ccy at U31's version and its image rebuilt (`deploy.bash`'s
  `play-claude-yolo.yml` step); ccy launched interactively in this checkout once with the
  token and SSH choice to reuse (`.claude/ccy/.last-launch.conf`), the token unexpired;
  every key in `LAST_SSH_KEYS` usable with no prompt (the agent route, or a key the
  ssh-agent of the terminal running `meta-deploy.bash` holds; otherwise the run fails before
  launch naming `ssh-add`, owner answer 5); `agent_bus_address` set; this checkout's
  `ccy.env.local` assigning `HOOKS_DAEMON_HOSTNAME` (the seats' `<host>`, section 5.2); no
  `.claude/ccy/pingbus/seats/` in it, so the numbered seats M2.9 expects are free; and,
  during the run, no `ccy --team acceptance` launch of its own in this checkout. The owner's
  plain `ccy` sessions, running or launched during the run, take no seat and are
  unaffected (D44).
- **To confirm first, when U20 is reworked:** that `--no-supervise` beats this checkout's
  `ccy.env`, which arms the supervisor wrapper unless `CCY_CLAUDE_WRAPPER` is already set
  (if it does not, U31 makes it); and that haiku follows the orders under this repository's
  `CLAUDE.md` and hooks daemon (if not, sonnet).

## 13. Probes

- **H1** From a ccy-image container on ccy's usual network: reach a test listener on a host
  `dummy` address and on host loopback via `host.containers.internal`; record which connect
  and the source address the listener sees. Same from a container on one of ccy's named
  project networks, and from a `--no-network` (pasta) container.
- **H2** From a docker container, an LXC guest and a libvirt guest (each where installed):
  reach the `dummy` address; record source addresses and each bridge's firewalld zone. A
  libvirt guest's own connection needs a guest shell, so it is an owner step, listed in the
  report, not a failure of the leg.
- **H1/H2 as built (U00).** The triage changes nothing on the host, so it cannot create
  the `dummy`: H1 and H2 test the host's primary address and, with `--bus-address=`, an
  address already assigned. Without one they record "dummy/bus address not tested" and the
  leg fails, because a dummy's firewalld zone and routing can differ from the primary
  address's. The dummy leg is therefore settled only once an `agentbus0` exists (U16's
  `software --bus-address`), then re-run with `--bus-address=<bus_ip>`.
- **H3** The Tuwunel static asset for this architecture: name, sha256, decompression; it
  starts under the section 3.5 unit with the resolver stub (transient `systemd-run` with the
  same properties), binds the listed addresses, and whether it sends sd_notify readiness.
  The resolver leg runs on Fedora Workstation and Fedora Server (a test VM): inside the unit
  `/etc/resolv.conf` reads the stub and a lookup fails.
- **H4** A throwaway Tuwunel with the section 3.6 config: `curl` runs every call the design
  makes and saves the responses (tokens and IDs scrubbed): shared-secret register (a wrong
  MAC refused), `PUT v2/users`, both with a real `<repo>.<n>+<host>.podman` handle (section
  4: if `+` is refused, the separator becomes `=`), the password reset and logout of a
  human's devices, the login mint, the single-admin query, `createRoom` v12 with
  agent-bus-protocol.md §8's power levels and `agent_bus.team` initial state, invite and the stripped
  state, join, a ping notice with the `agent_bus.ping` key and `m.mentions`, an
  `agent_bus.status` with the sender's key (another user's `@` key refused, a non-`@` key
  accepted), `/sync` with the filter and `timeout=0`, a limited sync and `/messages`,
  `GET /event`, `GET /login`'s flows, `POST /login/get_token` from a human session; every
  section 3.6 key accepted (the main run has `error_on_unknown_config_opts = true`);
  whether any header is logged at `warn`; and one deliberately unknown key added, once with
  `error_on_unknown_config_opts = false` (the warning's wording) and once as rendered (the
  start fails).
- **H5** Backup by `SIGUSR2` into `database_backup_path` (how completion shows), then the
  restore procedure on a copy (`--restore-backup`), and the restored server answering.
- **H4/H5 results so far** (U00, in the container against Tuwunel 1.9.3; the host run
  repeats them): `+` accepted; `PUT v2/users` with `"admin": false` answers 500; `GET /login`
  lists `m.login.token` with `login_via_token = false`, and `get_token` answers 403; SIGUSR2
  does nothing without `admin_signal_execute`; `--restore-backup` rolls back to the backup;
  an unknown key with `error_on_unknown_config_opts = true` exits 1; on btrfs Tuwunel advises
  `rocksdb_allow_fallocate = false`. All are folded into sections 3.6, 3.7 and 4 and P7.
- **H6** Element Desktop under `pasta --pcap <file> -T <port> -- flatpak run im.riot.Riot --profile <x>`: the capture holds its traffic; the profile path resolves; plain HTTP to the
  bus address works.
- **H7** (owner, with the phone) Element and Element X on the phone over WireGuard to
  `http://<wg_ip>:<port>`: which log in, sync, show ping notices and offer mention pills;
  whether a pill sets `m.mentions` (read from the event's source); whether the app trusts a
  user-installed CA.
- **U01** Claude Code, with a child `claude` authenticated as ccy does (on the host, with the
  ccy token ccy last launched the checkout with, as `CLAUDE_CODE_OAUTH_TOKEN`): hooks under `--plugin-dir` (all four events), and the
  inbox socket as section 6 uses it, including how long identical messages are deduplicated.

## Decisions

| #   | Decision                                                                                                                                                                                                                                                                                                                                                                                                                               | Reason                                                                                                                                                                                                                                                                                                                                                                     |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | The homeserver is a pinned static Tuwunel binary under a hardened system unit as the `agent-bus` user, not a container                                                                                                                                                                                                                                                                                                                 | The kernel IP filter and resolver lock-out need a system unit; no engine, linger or subuid on servers; the mode the spike proved.                                                                                                                                                                                                                                          |
| D2  | One installer (`agent-bus-install`), called by this repository's core play on every desktop and by other projects' IaC                                                                                                                                                                                                                                                                                                                 | Owner, answer 4; one implementation keeps every homeserver host identical.                                                                                                                                                                                                                                                                                                 |
| D3  | One instance and one room per team; instance name = team name                                                                                                                                                                                                                                                                                                                                                                          | Keeps a team's accounts, admin, backup and placement self-contained.                                                                                                                                                                                                                                                                                                       |
| D4  | Listen on loopback plus any address the team file names (no allowlist, no desktop/server distinction; every concrete address allowed, the wildcards `0.0.0.0` and `::` refused); connections only from `allow_from`                                                                                                                                                                                                                    | Owner, answer 5 and the 2026-10-06 decision; the team chooses its network. A wildcard listens on interfaces nobody named, and P2 cannot check what is bound (section 3.3).                                                                                                                                                                                                 |
| D5  | Reachability enforced by firewalld and, independently, by the unit's `IPAddressDeny`/`IPAddressAllow`                                                                                                                                                                                                                                                                                                                                  | Either layer alone holds; the unit layer also stops egress.                                                                                                                                                                                                                                                                                                                |
| D6  | No TLS in v1; TLS only if probe H7 shows the phone client needs it                                                                                                                                                                                                                                                                                                                                                                     | Private paths (in-kernel, WireGuard) need none; on any other network credentials travel in clear, documented (section 3.3).                                                                                                                                                                                                                                                |
| D7  | Members of every encapsulation are first-class; one bundle shape for all                                                                                                                                                                                                                                                                                                                                                               | Owner, answer 2 (reverses the deferral).                                                                                                                                                                                                                                                                                                                                   |
| D8  | The team's secrets belong to the `agent-bus` system user; admin commands go through `sudo agent-bus`                                                                                                                                                                                                                                                                                                                                   | No agent runs as that user, so no host or container member can read the root of trust.                                                                                                                                                                                                                                                                                     |
| D9  | pingbus and the admin tool ship as reproducible zipapps built from `helpers/` by the installer and by the ccy play independently                                                                                                                                                                                                                                                                                                       | One validator everywhere; the core ccy play never depends on the bus.                                                                                                                                                                                                                                                                                                      |
| D10 | `admin` creates the team room and is its only state writer; the team record (humans, roles, allowlists) lives in room state                                                                                                                                                                                                                                                                                                            | One place for humans to change membership; receivers trust only what `admin` wrote, and member bundles carry no allowlist to tamper with.                                                                                                                                                                                                                                  |
| D11 | Pings are `m.notice` with a structured `agent_bus.ping` key and a body that must equal its rendering                                                                                                                                                                                                                                                                                                                                   | Humans see pings in every client, phone included, with no relay; nothing can hide in the text.                                                                                                                                                                                                                                                                             |
| D12 | Human free text reaches an agent when its sender is a listed human and it mentions that agent or `@room`                                                                                                                                                                                                                                                                                                                               | Owner, answer 3 (reverses issue #59's rule and the control-room split).                                                                                                                                                                                                                                                                                                    |
| D13 | Agent free text without the ping key is dropped by every receiver; the server cannot block it; an agent may send text to a human (`pingbus say`), refused on send when secret-shaped (`secret`), and ignored by every agent that receives it (D30)                                                                                                                                                                                     | Power levels are per event type, not per `msgtype`; agent-to-agent traffic stays pings only; the owner allowed agent-to-human text (2026-10-06).                                                                                                                                                                                                                           |
| D14 | No warden                                                                                                                                                                                                                                                                                                                                                                                                                              | Humans address agents directly and see pings as notices, so neither translation nor mirroring is left for it to do.                                                                                                                                                                                                                                                        |
| D15 | Event prefix `agent_bus.` (agent-bus-protocol.md §2)                                                                                                                                                                                                                                                                                                                                                                                   | Owner, answer 6: clearly not a domain name; inside the Matrix identifier grammar.                                                                                                                                                                                                                                                                                          |
| D16 | Wake by the inbox socket through a hook-started watcher (counts plus a rising notice number; liveness by `flock`, one thread per team); `pingbus wait` as fallback; a Stop hook as guard; plugin loaded by `--plugin-dir`                                                                                                                                                                                                              | Section 6 and [U01].                                                                                                                                                                                                                                                                                                                                                       |
| D17 | Element is not confined; P4 proves its traffic by a pasta capture of that profile alone                                                                                                                                                                                                                                                                                                                                                | The user service manager cannot apply cgroup IP filters to a Flatpak.                                                                                                                                                                                                                                                                                                      |
| D18 | A ccy session's teams come only from its launch (`ccy --team`, D44), passed by the launcher as `PINGBUS_TEAMS`; the entrypoint refuses a bus variable set by `ccy.env` or `ccy.env.local`; one bundle per seat and team in the checkout's ignored `.claude/ccy/pingbus/seats/` (D32, D41)                                                                                                                                              | Owner, 2026-10-07 (D44), replacing answer 8's `ccy.env.local` opt-in; a session must never choose its own team, and `ccy.env` is writable by the session; the handle names the checkout's seat (owner, answer 7).                                                                                                                                                          |
| D19 | The handle's `<host>` is the install's role (`HOOKS_DAEMON_HOSTNAME`) or an explicit `--host`; never `CCY_HOST_HOSTNAME` or the hostname                                                                                                                                                                                                                                                                                               | One value names the install for the hooks daemon and the bus; handles reach public forge text, so a real hostname must never be the fallback.                                                                                                                                                                                                                              |
| D20 | The forge and provenance check runs on send and before the inbox write; hooks and inbox reads stay offline; `recv` re-fetches before printing                                                                                                                                                                                                                                                                                          | A sender can skip pingbus; the inbox is a cache.                                                                                                                                                                                                                                                                                                                           |
| D21 | References use full 40-hex SHAs and lowercase `owner/repo`; `pr:` carries its head SHA; each verb accepts only agent-bus-protocol.md §5's forms                                                                                                                                                                                                                                                                                        | Unambiguous; issue and PR text is writable by outsiders.                                                                                                                                                                                                                                                                                                                   |
| D22 | The protocol spec lives at `docs/agent-bus-protocol.md`, tied to the constants by a contract test                                                                                                                                                                                                                                                                                                                                      | The issue wants a versioned spec that outlives this plan.                                                                                                                                                                                                                                                                                                                  |
| D23 | No terminal Matrix client                                                                                                                                                                                                                                                                                                                                                                                                              | Element (desktop or phone) is the human's view; a terminal view (`pingbus tail`) is deferred.                                                                                                                                                                                                                                                                              |
| D24 | `wait` default timeout 1500 s                                                                                                                                                                                                                                                                                                                                                                                                          | Under the Monitor tool's 30-minute cap; bounds an orphaned waiter.                                                                                                                                                                                                                                                                                                         |
| D25 | Human passwords are set and printed once by `human password` and never stored; human accounts start with a discarded password                                                                                                                                                                                                                                                                                                          | Nothing that lets a holder post as a human sits at rest or in a backup; the installer's output never carries one.                                                                                                                                                                                                                                                          |
| D26 | No agent runs as a user holding a human's Matrix session; bare-desktop agents use a dedicated user                                                                                                                                                                                                                                                                                                                                     | Element's token in the home is the human: any same-user process could post `@room` to every agent (security review 2, B1).                                                                                                                                                                                                                                                 |
| D27 | A member may join with `human_text: false` (pings only)                                                                                                                                                                                                                                                                                                                                                                                | Joining a team gives its homeserver's operator the power to instruct the agent; this narrows it for a team hosted elsewhere.                                                                                                                                                                                                                                               |
| D28 | U23 proves same-host encapsulations (U24, the other host, is a later phase, D46); a leg lacking its engine or VM reports SKIPPED-NEEDS-OWNER and blocks close                                                                                                                                                                                                                                                                          | Owner answer 2 makes docker, LXC and VM members first-class, so a silent skip would hide a missing guarantee.                                                                                                                                                                                                                                                              |
| D29 | The receive flood count lives in the receiving process only; each `recv` with no watcher running starts from zero (agent-bus-protocol.md §10)                                                                                                                                                                                                                                                                                          | A flood is at most 60 per sender per run, every event still passes every other check, and persisting the count would add state to the inbox.                                                                                                                                                                                                                               |
| D30 | An agent receiving agent-to-human text (content key `agent_bus.text`) ignores it silently (`agent-text`): no `dropped.log` line, no `DROPPED` count, no non-zero `recv` exit (agent-bus-protocol.md §7, §9)                                                                                                                                                                                                                            | Coordinator decision, 2026-10-06: the text is legitimate traffic for the humans, not a fault; counting it as a drop would raise `DROPPED` and `recv` exit 6 on every answer a coordinator gives the owner.                                                                                                                                                                 |
| D31 | A homeserver 429 asking for a wait over 60 s is not slept: the client reports exit 9 at once, as a forge rate limit does (agent-bus-protocol.md §10, `limits.SERVER_429_WAIT_MAX_S`)                                                                                                                                                                                                                                                   | U09 review: an uncapped `Retry-After` would hold a `send`, a `wait` (at most 1790 s) or a 30 s long-poll for as long as the server says, hanging the process.                                                                                                                                                                                                              |
| D32 | One seat per ccy session: each session in a checkout claims its own seat, a member with its own account per team; seats are numbers or role names, and the handle carries the seat (`<repo>.<seat>+<host>.<type>`)                                                                                                                                                                                                                     | Owner decision 2026-10-07, verbatim: "yes one seat per session - can be numeric could also be role based - eg {repo}-dev, {repo}-audit, {repo}-pm". Replaces "one seat per bundle" for ccy checkouts, so sessions in one checkout can ping each other (section 5.5).                                                                                                       |
| D33 | A ccy checkout needs no setup to be used in a team: no `agent-bus join`/`leave`, nothing about the bus in `ccy.env.local`, no `agent-bus seat add`, no play step; teams are created by the play or the standalone installer as before                                                                                                                                                                                                  | Owner answer 4, 2026-10-07 ("can also be a little bit organic"), then the team given at launch (D44): a checkout-level team list would only record what the launch already says, and could disagree with it (YAGNI; section 5.6).                                                                                                                                          |
| D34 | U20 runs in this repository's own checkout with no setup step: every session launched with `--team acceptance` (named seats created by their first launch, unnamed launches on numbered seats); three sessions at once on role seats, a later session in a seat, a seat removed and returned, numbered seats reused, a plain `ccy` on no team; `agent-bus seat remove` of every acceptance seat returns the checkout exactly as it was | Owner answers of 2026-10-07: the real local checkout, several sessions at once on distinct seats, the acceptance team, `ccy.env.local`, `.claude/ccy/pingbus/`, `git status` and `HEAD` unchanged; sessions launched with `--team acceptance --seat <name>` (D44; section "U20" under 12).                                                                                 |
| D35 | A seat is claimed by `flock` on `seats/<seat>/seat.lock`, taken by `pingbus seat exec` in front of the entrypoint's final `exec` and inherited by every process of the session; the launcher picks the seat on the host and passes it as `PINGBUS_SEAT`                                                                                                                                                                                | The sync lock is per team and held only while a waker runs, so it cannot mark a session's seat from start to end; the inherited descriptor ends with the container, which is the session, with no PID recorded (section 6). Picking on the host lets the launcher create the seat and record it in the restart and restore arguments.                                      |
| D36 | `ccy --team <team> --seat <seat>` takes that seat, creating it on first use; `ccy --team <team>` alone takes the lowest-numbered free seat, creating the next number when none is free, and never a role seat; `--seat` without `--team` is a usage error (64); a team not running here, a held named seat, or any failure to seat the session refuses the launch; no `--no-bus` (a launch without `--team` is off the bus)            | CLAUDE.md's fail-fast rule: starting a session the human launched into a team without the bus, or ignoring a `--seat`, is the banned "skip and warn". The human asked for the team, so an unnamed launch creates a seat rather than refusing (rework question 1, settled by D44). Lowest-free numbering keeps numbers stable across sessions and reboots (owner answer 2). |
| D37 | The handle grammar widens inside protocol version 1                                                                                                                                                                                                                                                                                                                                                                                    | Owner answer 1, 2026-10-07; v1 has run in no team but the acceptance team, which is purged per run, and every v1 handle stays valid.                                                                                                                                                                                                                                       |
| D38 | A headless ccy launch with no launch-choice flag takes the checkout's Quick Launch choices (token and SSH keys by name only) without asking, and is refused when there are none; a key that would need a passphrase refuses it before launch, naming `ssh-add`                                                                                                                                                                         | Owner answer 5, 2026-10-07: U20's sessions are launched exactly as the owner launches; the harness never handles a secret; the prompt otherwise reads the session's own stdin.                                                                                                                                                                                             |
| D39 | U20's wait-fallback leg stops a live watcher instead of starting a session with no inbox socket                                                                                                                                                                                                                                                                                                                                        | ccy always gives a socket now; the switch that removed it (`CLAUDE_CODE_HARBOR_KITE=0`) needed a tracked `ccy.env` written into the checkout and is undocumented. A watcher that ended is the fallback a ccy session really meets.                                                                                                                                         |
| D40 | A seat is a durable role identity: its account, handle, role, bundle and pingbus state persist across sessions and reboots; any later session that claims it continues as that member; the only exclusivity is one live session per seat; removing a seat parks it (token revoked, account, role and room membership kept) and adding the same name returns it; `remove-member` retires a member for good                              | Owner answer 2, 2026-10-07 (reverses "never reissue"): "it's the role that's important, not the specific agent session". Parking uses only calls H4 recorded (password reset with `logout_devices`, the admin login mint); Tuwunel keeps a deactivated account and nothing recorded reactivates one (section 5.5).                                                         |
| D41 | A seat is created on first use by the launcher on the host (`agent-bus seat take`, which runs `sudo agent-bus add-member` once per named team the seat lacks); the seat's directory is its record, and a seat is in the teams it holds a bundle for                                                                                                                                                                                    | Owner answer 4, 2026-10-07: the ccy command creates the seat on first use ("tieing in seat creation to ccy command is excellent idea"), then later launches just claim it. A directory as the record leaves nothing to drift, and no team list is kept beside it.                                                                                                          |
| D42 | A ccy launch names only teams whose homeserver runs on this host; the launcher refuses another (the v1 scope, D46)                                                                                                                                                                                                                                                                                                                     | Owner answer 3, 2026-10-07: "too early to decide anything about other machine stuff"; `add-member` runs on the homeserver host.                                                                                                                                                                                                                                            |
| D43 | `pingbus history` reads a seat's past pings and human text from the room, through the same checks as `recv`, and prints `HISTORY` lines that are a record, never work                                                                                                                                                                                                                                                                  | Owner answer 2, 2026-10-07: an agent taking over a role should read its history. The room, not the local cache, is the source, so history survives parking, a lost directory and a fresh clone.                                                                                                                                                                            |
| D44 | A ccy session is in a team only when launched with it: `ccy --team <team>` (repeatable) `[--seat <seat>]`; a plain `ccy` is in no team in any checkout (no seat, no bundle, no watcher, no plugin); the teams and the seat are decided on the host by the launcher and passed into the container, never read from anything the session can write                                                                                       | Owner, 2026-10-07, verbatim: "ccy doesnt join teams automatically - if a session is in a team it must be launched with team. if it is not then it is not on the team"; and "tieing in seat creation to ccy command is excellent idea". Removes `agent-bus join`/`leave`, `PINGBUS_TEAMS`/`PINGBUS_HOST` in `ccy.env.local`, `seat add` and `--no-bus` (D33, D36).          |
| D45 | A seat is one name and one `PINGBUS_HOME` across teams, with an account per team under the same handle; one seat lock whatever teams a launch names; an unnamed launch takes the lowest free numbered seat whatever teams it already holds                                                                                                                                                                                             | Owner answer B, 2026-10-07 ("yes for v1"), re-read under D44: one identity across teams is pingbus's existing multi-team model and keeps the durable role (D40) one thing; a seat per (team, name) would need several homes, locks and numbers per session for no v1 need (section 5.5).                                                                                   |
| D46 | v1 teams are hosted on this machine only: the homeserver and every member (bare desktop, ccy, LXC, docker and VM guests) on one host. U24 (a team on another machine over WireGuard) and success criterion 3 (an agent on another host) are a later phase, their design kept and marked; U23 stays in v1                                                                                                                               | Owner, 2026-10-07: "yes lets start v1 with basic on this laptop teams, then we will roll out to cross machine/network teams". The plan can close without a second machine.                                                                                                                                                                                                 |

## Owner questions

Answered so far: the three after the first revision (2026-10-06: agent text to a human, D13;
any listen address with no TLS in v1, D4, D6; `ccy.env.local` bound read-only, ccy 3.84.0);
the five of the seats design (2026-10-07: D37, D40, D42, D43, D38, success criterion 1);
and the three of the rework (2026-10-07, journal): an unnamed launch's seat is settled by
the team given at launch (D44, D36: it creates the next number); every seat of a checkout
in every team is replaced by one seat identity across the teams its launches name (D45);
and U24 and success criterion 3 move to a later phase (D46).

Open (2026-10-07), with the recommended answer:

1. **Does the phone (probe H7, and U26 if H7 needs TLS) leave v1 with cross-machine
   teams?** A phone is a human's client, not a member, but it reaches the homeserver over
   WireGuard, the network path D46 defers, and H7 is what decides U26 and whether
   agent-bus-protocol.md §7 gains a `<handle>:` addressing form. Recommended: yes, defer
   H7 and U26 with U24; v1 humans use Element Desktop on this machine (U25, H6, P4), which
   success criterion 2 already names, and the §7 addition, if a phone ever needs it, is
   additive.
