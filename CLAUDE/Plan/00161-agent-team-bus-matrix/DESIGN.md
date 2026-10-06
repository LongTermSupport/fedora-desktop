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
why, is in `subagent-reports/261006-design-revision-2-opus-5-5.md`.

Placeholders: `<team>` (team name), `<port>` (the team's homeserver port), `<sn>` (the
team's `server_name`), `<handle>` (an agent's handle), `<role>` (an install's role, the
value of `HOOKS_DAEMON_HOSTNAME`), `<bus_ip>` (the host's bus address, section 3.3),
`<wg_ip>` (an address on a WireGuard interface), `<cidr>` (a source range allowed to connect).

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
does it. A ccy session opts in through its untracked `.claude/ccy/ccy.env.local`.

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
  on one laptop runs its homeserver on that laptop; a team with a member elsewhere (a
  data-centre server) runs its homeserver beside that member, reached by the others over
  WireGuard. The play installs the software on every desktop; an instance exists only where a
  team file declares it.
- **Member.** One agent, with one account per team it belongs to. Its handle
  (agent-bus-protocol.md §3) encodes repository, `<n>`, host and encapsulation type. `<host>` is the
  install's role, `HOOKS_DAEMON_HOSTNAME`, or a name the human passes as `--host`; never a
  real hostname. Handles end up in public forge text (`done` references, journals), so
  `CCY_HOST_HOSTNAME` and the system hostname, which the hooks daemon falls back to, are
  never used: with no role set and no `--host`, `suggest-handle` and `add-member` refuse.
  A handle names a seat (a
  checkout, or one non-ccy install), not one conversation; `--new-handle` is a remove plus an
  add (section 4).
- **Roles.** Each agent member is `orchestrator` or `worker` in its team (agent-bus-protocol.md §5
  says which verbs each may send). Roles are runtime data set by `agent-bus add-member` and
  `agent-bus set-role`, published in the team record.
- **Humans.** Named in the team file; each gets one account per team, power level 50 in the
  team room, never server admin. Only these accounts' text reaches agents, and only agents
  whose bundle accepts human text (section 5.1).
- **Multi-team.** A member in several teams has several bundles under one directory
  (`PINGBUS_HOME/<team>/`) and lists the ones it is active in (`PINGBUS_TEAMS`). `recv`,
  `wait` and the watcher cover every active team; `send` needs `--team` when more than one is
  active; every output line names its team. A ping's references are checked against its own
  team's allowlists only. Accounts on different teams never share a token.

## 3. The homeserver installer

### 3.1 Two entry points, one implementation

| Entry point                                                                    | Who runs it                            | What it does                                                                                                                                                                                                       |
| ------------------------------------------------------------------------------ | -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `files/usr/local/sbin/agent-bus-install` (Bash, as root)                       | any Fedora desktop or server's own IaC | `software`, `team`, `remove`, `backup-now`, `restore`, `check` (below). Non-interactive: arguments only, fails fast, idempotent, marker lines on stdout, diagnostics on stderr.                                    |
| `playbooks/imports/play-agent-bus.yml` (core, imported by `playbook-main.yml`) | this repository, every desktop         | installs dnf dependencies, copies the installer, runs `agent-bus-install software --source <repo>`, then `team` for each `agent_bus_teams` entry with `state: present` and `remove` for each with `state: absent`. |

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
  and `agent-bus-claude`, the launcher for non-ccy members (section 5.3).
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
  through their bridge, so their bridge subnet goes in `allow_from`. Members on other
  hosts reach `<wg_ip>` through WireGuard, so the WireGuard subnet (or the peer addresses)
  goes in `allow_from`. A phone reaches `<wg_ip>` the same way.

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
   `<repo>+<host>.<type>` counter, written by `add-member`), `db/`, `backups/`, `secrets/`
   (0700): `registration_shared_secret` (64 random bytes hex, created once).
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
`ExecStart=/usr/local/lib/agent-bus/tuwunel`, `Restart=on-failure`, `RestartSec=5s` and
`StartLimitIntervalSec=0` (so a WireGuard address that is not up yet at boot is bound once
it is, instead of the unit hitting the start limit and staying failed),
`TimeoutStopSec=330`, `After=network-online.target`; the drop-in adds `After=` and
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

Rendered by `agent-bus render toml`; a test (U15) asserts this exact key set and that each
key appears in Tuwunel's example config for the pinned version. By default Tuwunel only
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
  appears within about a second; fails the unit if none appears), then adds a tar of `secrets/`, `team.json` and `registry.json` beside it (root,
  0600). A backup holds the team's root of trust (the shared secret and the admin token):
  whoever holds one can instruct every agent in the team, so `docs/agent-bus.md` says so,
  and copying backups off the host is the owner's decision and is not built.
  `agent-bus-install backup-now --team <team>` runs the same once.
- **Restore:** `agent-bus-install restore --team <team> --backup <id>` stops the unit,
  restores the tar, starts the binary once as `agent-bus` with `--restore-backup <id>`
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
before any call, never prompting): `bootstrap`, `add-member <team> --repo=… --host=… --type=… --role=… [--no-human-text] --out=<dir>` (creates the account, mints its token,
invites it, updates the team record, and writes the bundle, section 5.1),
`remove-member <team> <handle>` (kicks, deactivates, removes the role; `<n>` never reused),
`set-role`, `rotate-token`, `list` (members, roles, state; no tokens),
`human password|devices|logout-all|lock|unlock <team> <name>`, and
`render check|toml|dropin` for the installer. `rotate-admin` is deferred (section 3.7).

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
under `--out`. **`--no-human-text`** writes `"human_text": false`: the member accepts pings
only and drops every human message. It is the choice for a member joining a team whose
homeserver someone else runs (section 9). It can only narrow what the member accepts, so a
bundle writable by its agent does not weaken it. The human copies the bundle to `PINGBUS_HOME/<team>/` on the member,
by hand or by that project's IaC. The member also needs: `pingbus` on `PATH` (the kit, or
the ccy image), the Claude Code plugin, and its session started with the plugin and the
inbox-socket setting (section 6). `pingbus suggest-handle` prints the `add-member` arguments
a member's environment implies (repository from the git remote, else the directory; host
from `HOOKS_DAEMON_HOSTNAME`, refusing when it is unset; type from the environment), so the
human does not guess.

### 5.2 Per encapsulation

| Type                               | Where the bundle goes                                                                    | How opt-in is expressed                                                 | How it reaches the homeserver                                             | `allow_from` needs             | pingbus and plugin from           |
| ---------------------------------- | ---------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------ | --------------------------------- |
| `podman` (ccy)                     | `<checkout>/.claude/ccy/pingbus/<team>/` (git-ignored by ccy's `.claude/ccy/.gitignore`) | `PINGBUS_TEAMS` in the checkout's untracked `.claude/ccy/ccy.env.local` | `<bus_ip>` (same host, via pasta) or `<wg_ip>` (via the host's WireGuard) | nothing extra (host addresses) | the ccy image                     |
| `host` (bare desktop)              | a dedicated agent user's `~/.config/pingbus/<team>/` (never the human's user, section 8) | `PINGBUS_TEAMS` in `~/.config/pingbus/env`                              | `<bus_ip>` or `<wg_ip>`                                                   | nothing extra                  | `/usr/local/bin/pingbus`, the kit |
| `host` (a server, always-on agent) | the agent user's `~/.config/pingbus/<team>/`                                             | the same env file                                                       | `<bus_ip>` beside it (the placement convention) or `<wg_ip>`              | nothing extra                  | the kit (copied by its IaC)       |
| `lxc`                              | inside the LXC, the agent user's `~/.config/pingbus/<team>/`                             | the same env file                                                       | `<bus_ip>` via the LXC bridge, or its own WireGuard                       | the LXC bridge subnet          | the kit, copied in                |
| `docker`                           | bind-mounted read-only, or copied, at the path `PINGBUS_HOME` names                      | `PINGBUS_TEAMS` in the container environment                            | `<bus_ip>` via the docker bridge                                          | the docker network subnet      | the kit, copied in or mounted     |
| `vm`                               | inside the VM, as `lxc`                                                                  | the same env file                                                       | `<bus_ip>` via the libvirt bridge, or `<wg_ip>`                           | the libvirt network subnet     | the kit, copied in                |

What differs beyond the table:

- **Python.** The zipapp needs Python 3.11 or later; pingbus checks at start (exit 78). The
  installer's hosts and the ccy image have it; a docker or non-Fedora guest image must.
- **Role variable.** ccy members set `HOOKS_DAEMON_HOSTNAME=<role>` in `ccy.env.local`;
  other members export it in the env file. It names the handle's `<host>` and the hooks
  daemon's role from one value. With none set, `--host` is passed explicitly (section 2).
- **Writable bundle.** Every bundle is writable by code running as its agent. That is no new
  exposure: the token is the agent's own, the bundle holds no allowlist or human list
  (those come from the team record, which only `admin` can write), and its one switch,
  `human_text`, can only be set back to the default; so code in an agent's checkout can at
  most misdirect that agent, which it can already do by editing files the agent reads.
- **Never ignored by accident.** A ccy bundle sits under `.claude/ccy/`, which ccy's
  generated `.gitignore` ignores except for named files; P8 asserts `git check-ignore` for
  every bundle path the acceptance members use.
- **One seat per bundle.** Two sessions using one bundle share an account; the sync lock
  (agent-bus-protocol.md §12, §14 exit 75) makes the second session's watcher exit busy and its SessionStart hook
  say that another session holds the seat.

### 5.3 The ccy member

- **Opt-in is `ccy.env.local`** (owner, answer 8): the untracked
  `.claude/ccy/ccy.env.local` (sourced after `ccy.env` since ccy 3.80.0) carries
  `export PINGBUS_TEAMS=<team>[,<team>]` and, where the install has a role,
  `export HOOKS_DAEMON_HOSTNAME=<role>`. `ccy.env.local` is placed by the install's own IaC,
  never by hand and never by an agent in the checkout.
- **The template is ccy's.** Since ccy 3.83.0 (Plan 00160 Task 3.2) ccy writes the tracked
  `.claude/ccy/ccy.env.local.dist` itself from `ccy_env_local_dist_text` in
  `files/var/local/claude-yolo/lib/common.bash`, versioned by `CCY_ENV_LOCAL_DIST_VERSION`;
  it already carries the `HOOKS_DAEMON_HOSTNAME` placeholder. U19 adds a commented
  `#export PINGBUS_TEAMS=<team>[,<team>]` block to that text, raises
  `CCY_ENV_LOCAL_DIST_VERSION`, bumps `CCY_VERSION`, and extends
  `scripts/test-ccy-env-local-dist.bash`. Installs whose `ccy.env.local` names the older
  dist version then get ccy's launch warning, which is the intended prompt to update.
- **Read-only, or the role is not a role.** A session that can rewrite its own
  `ccy.env.local` can change its role and its active teams for its next launch. Since ccy
  3.84.0 (Plan 00160 Task 3.3, done) ccy binds an existing `ccy.env.local` read-only over
  the workspace, so a session cannot. Where no `ccy.env.local` exists nothing is bound, and
  a session could create one; that install has not opted in, and the bundle it would need
  is placed by a human, so U19 relies on the bind as built.
- **The entrypoint**, after sourcing `ccy.env` and `ccy.env.local`, when `PINGBUS_TEAMS` is
  set: exports `PINGBUS_HOME=${PINGBUS_HOME:-/workspace/.claude/ccy/pingbus}`; runs
  `pingbus config check` for every listed team and **refuses to start** with the reason if
  one fails (opted in but broken is an error, not a silent no-op); symlinks
  `/usr/local/bin/pingbus`; and adds `--plugin-dir /opt/claude-yolo/optional/agent-bus/plugin`
  and `--settings /opt/claude-yolo/optional/agent-bus/settings.json` to the `claude`
  arguments (inside any supervisor wrapper's `--`). When it is not set, nothing is installed
  or added: the image's copy is inert.
- **Open, blocks U19: the ccy `base_url`.** Probe H1 (JOURNAL 2026-10-06) found that a ccy
  container reaches a same-host homeserver only through `host.containers.internal`, a
  hostname, while agent-bus-protocol.md §12 and U04's `config.check_base_url` (which U09's
  client also applies) allow `http://` only to an IP literal in `plain_http_hosts`. No ccy
  bundle naming that address passes. U04 and U19 settle it here before U19 is built: the
  literal address that alias resolves to, a named exception to §12, or the dummy
  `<bus_ip>` once H1's dummy leg is re-run.
- **Nothing else in ccy changes.** No launcher flag, no host-side credential store, no deny
  list entry (the team's secrets belong to another user), no extra network (the container's
  usual route reaches `<bus_ip>` and WireGuard addresses through pasta, probe H1).
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
`claude --plugin-dir <kit>/plugin --settings <kit>/settings.json "$@"`. On a desktop the
human starts it as the dedicated agent user (for example `sudo -iu <agent-user> agent-bus-claude`);
creating that user is the install's IaC, documented in `docs/agent-bus.md`, not built in v1. A headless agent driven by a script (`claude -p` in a
loop, as a server-side triage agent may be) needs no socket: its driver runs `pingbus wait`
between turns.

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
  and, whenever the pending count rises, writes one fixed-template message to the socket:
  "agent-bus: N pending (H from humans, P pings), notice S. Run `pingbus recv`." Only
  counts, never content. `S` is the watcher's own monotonic counter: the socket drops a
  message identical to an earlier one, and without `S` the sequence "1 pending", `recv`,
  "1 pending" would lose the second and leave the session asleep. U01 measures how long
  that dedupe window is. The watcher exits when the session's socket goes away; SessionEnd
  stops it.
- **Liveness is a lock, never a PID.** `/workspace` is shared across container namespaces,
  where a PID in a file means nothing. Each team's `lock` file is held with `flock` by the
  watcher or a waiter, which writes its kind (`watch` or `wait`) into the file, or briefly
  by a `recv` syncing once (kind `recv`, which is not a waker); a waker is live exactly
  when a non-blocking `flock` on `lock` fails and the kind is `watch` or `wait`. A dead holder's lock is released
  by the kernel, so a SessionStart finding the lock free starts a new watcher, and finding
  it held by another session's watcher reports that the seat is taken.
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
  repeats are dropped (hence `S`) and distinct ones batched, which suits a rising count. It is a recent
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
  a fixed template naming the failure class, under the same once-only rule.
- **Context.** UserPromptSubmit adds "N pending: run `pingbus recv`"; SessionStart runs
  `config check` and reports the wake path or a missing bundle.
- **Delivery to the agent.** `recv` drains the inbox; before printing an item it re-runs
  the offline validator and re-fetches the event with `GET /rooms/{room}/event/{event}`
  (no lock needed), printing from the fetched copy: the inbox is a cache, never the
  authority. Hooks never touch the network.
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

- **Desktop:** `play-agent-bus-element.yml` (optional): Element Desktop Flatpak
  `im.riot.Riot` (system-wide, the `play-comms.yml` pattern) and, per team the host's humans
  use, `~/.var/app/im.riot.Riot/config/Element-<team>/config.json` with `base_url` the
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
explains joining a team hosted elsewhere, with `--no-human-text` as the narrower choice);
**v1 has no TLS, so a team listening on a network that is not private (not loopback, not a
host-only bridge, not WireGuard) sends every login, password and token across it in
clear**; backups hold the root of trust; every agent account can read the whole
team room (section 7); an agent can show humans free text under its own handle (allowed,
and refused by pingbus only when secret-shaped; a misbehaving agent can skip pingbus);
members on one private network or bridge can reach each other, and the homeserver can open
connections to them; an agent on a host where it can `sudo` without a password reaches the
root of trust; a human's session in a browser cannot be detected beside a `host` member;
Matrix has no second factor here. The forge cache (`forge-cache.json`, protocol §6) lives in
the member's state directory and is trusted as that directory is: the agent it serves can
write it, so a planted positive entry skips the forge check for that member's own sends and
receives only. That weakens nothing beyond what the agent can already do (skip pingbus, or
act on a ping it chose to trust); other members re-check every reference against their own
cache.

## 10. Privacy acceptance checks and how each is proven

All in this plan's `acceptance.bash`, host only, against a dedicated acceptance team
(reserved name `acceptance`, created by `deploy.bash` through the play and removed with
`state: absent` and `purge` at the end). Each check prints PASS/FAIL with its evidence file
under `untracked/plan-runs/`.

| Id  | Check                                                                                                                                                         | Proof                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| P1  | No outbound traffic from the homeserver                                                                                                                       | Construction: `systemctl show` of the unit gives exactly the rendered `IPAddressDeny`/`IPAddressAllow`, `RestrictAddressFamilies`, the resolver bind and `IPAccounting`; `allow_federation` false and `trusted_servers` empty in the running config. Enforcement on this kernel: a transient unit with the same IP properties fails to reach a public address. Behaviour: during a scripted session (bootstrap, join, 20 pings, acks, human messages) `ss -tnp` sampled for the Tuwunel PID lists only allowed peers and FAILS on any Tuwunel socket whose local port is not `<port>` (an outbound connection, which the filter would still allow towards `allow_from`), and a host capture on port 53 shows no query from the session.                                                                                                                                                                                                                                                                |
| P2  | Reachable only where the team allows                                                                                                                          | `ss -ltnH`: `<port>` bound on exactly `127.0.0.1` and `listen`; firewalld rich rules only for `allow_from`; from a test VM (owner answer 7: counts as another machine) with a route added to each host address, a connection from a source outside `allow_from` fails on every address, and one from an allowed source succeeds. The VM leg reports SKIPPED-NEEDS-OWNER if no VM is available, and the plan cannot close then.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| P3  | Names are not published                                                                                                                                       | `server_name` ends in `.internal`; `getent ahosts <sn>` fails; the P1 capture shows no query for `<sn>` or `<team>`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| P4  | Element's team profile talks only to its HS                                                                                                                   | The scripted Element session (start, log in, open the team room, receive a ping notice, send an addressed message, idle 120 s) runs as `pasta --pcap <file> -T <port> -- flatpak run im.riot.Riot --profile acceptance`; FAIL on any packet but the forwarded connection to the homeserver. Plus the static key check of section 8.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| P5  | Secrets never reach logs                                                                                                                                      | For every secret (shared secret, admin token and password, member tokens, the human password the script set with `human password`), a `grep -rqF -f` over: the backup tar's member list (no human password file), the deploy and installer logs, `untracked/plan-runs/`, `journalctl` for the units, `systemctl show` output, every pingbus stdout and stderr captured, the transcript and state trees of the sessions used, the socket messages sent, shell history, the Element profile's logs. Reports the file name of a hit only.                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| P6  | pingbus delivers agent text to no agent, human text only to the agents it addresses, non-team text to none (every account still receives the room: section 7) | During the session: an agent sends text to the human with pingbus (the human sees it in Element) and a secret-shaped text is refused; an agent account posts, with raw `curl`, an `m.text`, an `m.text` with `m.mentions.room: true`, a notice whose `body` differs from its ping, and a ping-less notice; the human posts one message addressed to agent A only, one `@room`, a reply to an agent's ping whose fallback quotes that ping, and a forged ping notice; a non-team test account (made by `admin`, invited for the test) posts text; a human removed from the team record posts an addressed message. Member C's bundle has `human_text: false`. Then A's, B's and C's `pingbus recv` output and inbox files: A has the addressed message, the `@room` and the reply with the quote removed; B only the `@room`; C none of them; none has any agent, non-team, removed-human or forged-ping text; each `dropped.log` names the drops and none lists the `say` text (ignored, not dropped). |
| P7  | Admin endpoints and the human's limits hold                                                                                                                   | `/_synapse/admin/v1/register` with a wrong MAC is refused from a member; a member token on an admin endpoint is refused; the admin tool refuses a non-loopback base URL; the running config has `login_via_token` and `login_via_existing_session` false, and `POST /_matrix/client/v1/login/get_token` from the human's session is refused (Tuwunel 1.9.3 lists `m.login.token` in `GET /login` regardless, H4, so the flow list is recorded, not asserted); the human's token cannot set state, invite or redact in the team room, and cannot join the admin room.                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| P8  | Bundles stay private; no agent holds a human's session                                                                                                        | Every bundle path the acceptance members use is `git check-ignore`d in its checkout; from inside a ccy acceptance session the Element profile directory is not reachable; with an Element profile directory planted in a test agent user's home, `pingbus config check` for that user's `host` bundle and `agent-bus-claude` both refuse (78); `play-agent-bus-element.yml` refuses a user with `~/.config/pingbus/`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |

Every leg of P1-P8 and of U23/U24 that needs something the host lacks (a VM, docker, LXC, a
WireGuard peer) reports SKIPPED-NEEDS-OWNER, never PASS, and the plan cannot close while
any leg is skipped.

## 11. Where each thing can be verified

| Container (this checkout, no podman, no systemd)                                                                                                                                                                                                                                     | Host only, through `meta-deploy.bash`                                                                                 |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- |
| U02-U15, U18, U19 against fakes built from H4's recorded responses: validator, limits, config, CLI, inbox, forge, Matrix client, syncer, hooks, watcher and socket client (against a fake socket), zipapp, team file, registry, admin tool, renders, plugin contract, ccy entrypoint | The installer for real: user, binary, units, sandbox, firewalld, NetworkManager, readiness (U16), then the play (U22) |
| `ruff`, `qa-helper-tests.bash`, `qa-python.bash`, `bash -n`, `ansible-playbook --syntax-check` for the plays, `scripts/test-agent-bus-install.bash` (the installer's pure parts against a temporary root), `scripts/test-ccy-agent-bus.bash`, `scripts/test-ccy-env-local-dist.bash` | Tuwunel itself (H3-H5), then M1 for real; reachability from containers, guests and a VM (H1, H2, P2)                  |
| Claude Code behaviour: plugin loading and the inbox socket (U01, a logged-in child `claude`; on the host if the container cannot give the child a credential)                                                                                                                        | Element desktop (H6, P4); the phone (H7, the owner's phone); a ccy session woken for real (M2)                        |

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

| Id  | Title                                | Creates / changes                                                                                                                                                                                                                                                                                  | Tests                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | Needs                   | Where                              |
| --- | ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------- | ---------------------------------- |
| U00 | Host probes                          | `triage.bash` + `triage_probe.py` (H1-H6, section 13), scrubbed Tuwunel responses for U08                                                                                                                                                                                                          | `test_triage_probe.py` (from the wave-1 branch, re-pointed); results journalled                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | -                       | H                                  |
| U01 | Claude Code probes                   | the wave-1 probe scripts, extended: Stop and SessionEnd under `--plugin-dir`; a session with `--settings` `crossSessionInbound: accept`, a hook-spawned detached process writing to the socket                                                                                                     | which hooks fired; the socket's wire format; one turn started; framing; identical repeats dropped and the dedupe window's length, distinct ones batched; behaviour without bypass mode; checker table test                                                                                                                                                                                                                                                                                                                                                                                                                      | -                       | C, or H without a child credential |
| U02 | Protocol spec and pure validator     | `docs/agent-bus-protocol.md` (written from `PROTOCOL.md`), `helpers/pingbus/protocol.py` (taken from the wave-1 branch, not merged)                                                                                                                                                                | `test_protocol.py`: every verb x ref form, grammars, the handle separator as one constant, canonical body, mentions equality, human-message rules (addressing, `@room` only from humans, reply-fallback removal, edits, size), team record and status parsing (state key must equal sender, size cap), power-level equality, every drop reason, agent text to a human (mentions only listed humans, secret-shaped text refused); `test_protocol_doc.py`                                                                                                                                                                         | -                       | C                                  |
| U03 | Limits                               | `helpers/pingbus/limits.py`                                                                                                                                                                                                                                                                        | `test_limits.py`: token bucket, duplicate window, receive flood, ack deadlines, stale ping and human ages, out-of-bounds overrides refused; injected clock                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | U02                     | C                                  |
| U04 | Member config and bundles            | `helpers/pingbus/config.py`                                                                                                                                                                                                                                                                        | `test_config.py`: schema incl. `human_text`, token file relative to the bundle and mode > 0600 refused, plain HTTP only to listed IP literals, `PINGBUS_HOME`/`PINGBUS_TEAMS` resolution, multi-team rules, Python version gate, a `host` bundle refused beside an Element profile                                                                                                                                                                                                                                                                                                                                              | U02                     | C                                  |
| U05 | CLI offline parts                    | `helpers/pingbus/cli.py` (dispatch, exit codes, line formatter, `validate`, `config check`, `suggest-handle`, `version`)                                                                                                                                                                           | `test_cli_offline.py`: subprocess runs for 0, 4, 64, 78; stdout/stderr split; exit-code table == doc; `validate --event` reports an agent text as ignored (`agent-text`), never as a drop; `suggest-handle` uses `HOOKS_DAEMON_HOSTNAME` only and refuses without it                                                                                                                                                                                                                                                                                                                                                            | U02, U04                | C                                  |
| U06 | Inbox, outbox, lock                  | `helpers/pingbus/inbox.py`                                                                                                                                                                                                                                                                         | `test_inbox.py`: event ID validated before it names a file, dedupe, atomic writes, per-team dirs, re-validation on read, sync token saved after inbox fsync, second locker busy, liveness by non-blocking `flock` with the holder's kind in the file, outbox ack tracking and TIMEOUT, the send gate (U03's token bucket and duplicate window) saved between `send` processes                                                                                                                                                                                                                                                   | U02, U03                | C                                  |
| U07 | Forge check and provenance           | `helpers/pingbus/forge.py`                                                                                                                                                                                                                                                                         | `test_forge.py` with an injected opener (as the reviewed design; unchanged rules)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | U02, U04                | C                                  |
| U08 | Fake homeserver                      | `tests/helpers/pingbus/fake_client_api.py`, `fake_admin_api.py`, `fixtures/tuwunel/` (from H4)                                                                                                                                                                                                     | `test_fakes.py`: replays every recorded flow; enforces `@` state keys and power levels per event type, and lets a power-0 member write `agent_bus.status` under a non-`@` key (as the server does)                                                                                                                                                                                                                                                                                                                                                                                                                              | U00                     | C                                  |
| U09 | Matrix client                        | `helpers/pingbus/matrix.py`                                                                                                                                                                                                                                                                        | `test_matrix.py`: bearer header unredirected, no proxy, no redirects, txn ID reuse, 401/403/429 mapping, token absent from every error                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | U04, U08                | C                                  |
| U10 | Sync engine and room trust           | `helpers/pingbus/syncer.py`                                                                                                                                                                                                                                                                        | `test_syncer.py` vs fakes: filter, first sync takes `next_batch` only, gap fill, invite rule, team-record and power-level verification and loss of trust, both receive pipelines, `human_text: false`, reply fallback removed, an agent's `@room` dropped, a foreign-key status ignored, forge on receive, drops logged, status written; a missing team record or room (U09's `matrix.NotFound`, exit 7 by itself) maps to exit 10                                                                                                                                                                                              | U06, U07, U09           | C                                  |
| U11 | CLI core: send, say, recv, wait      | `cli.py`: `send`, `say` (agent text to humans, section 7), `recv`, `wait`, multi-team (one long-poll thread per team)                                                                                                                                                                              | `test_cli.py`: one test per exit code; only `PING`/`HUMAN`/`TIMEOUT`/`SENT` on stdout; recv re-fetches before printing; `wait` not ended by drops; `say` reads stdin, takes humans only, refuses `secret` and the member's own token (exit 4) and shares the send bucket; a received agent text adds no `DROPPED` count and no exit 6; one busy team does not block the others; token on neither stream                                                                                                                                                                                                                         | U05, U07, U10           | C                                  |
| U12 | Hooks, watcher, socket, status       | `helpers/pingbus/hooks.py`, `helpers/pingbus/notify.py` (socket client, wire format from U01), `cli.py`: `watch`, `hook …`, `inbox`, `status` (members and roles from the cached team record, wake path, unexpected members)                                                                       | `test_hooks.py`, `test_notify.py`, `test_cli_status.py`: block-once rules, no-waker throttle, `stop_hook_active`, planted inbox text never printed, counts-only templates with a rising notice number, notify only on a rising count, lock-based liveness (a stale PID file means nothing), exit on socket loss; no network in hooks; every printed status field grammar-validated                                                                                                                                                                                                                                              | U01, U06, U11           | C                                  |
| U13 | Zipapps                              | `helpers/pingbus/bundle.py` (builds `pingbus` and `agent-bus`)                                                                                                                                                                                                                                     | `test_bundle.py`: byte-identical rebuild, every module included, archives run `version`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | U05                     | C                                  |
| U14 | Team file, registry, handles         | `helpers/agent_bus/teamfile.py`, `registry.py` (`/var/lib/agent-bus/<team>/registry.json`)                                                                                                                                                                                                         | `test_teamfile.py`, `test_registry.py`: schema, address and CIDR shape rules, `<n>` never reused, round trip, `--host` required when no role is given                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | U02                     | C                                  |
| U15 | Admin tool and renders               | `helpers/agent_bus/admin.py`, `render.py`, `cli.py`; wrapper `files/usr/local/bin/agent-bus`                                                                                                                                                                                                       | vs fake admin API: HMAC, bootstrap idempotent with a single server admin, human and member accounts created with a discarded password and no `admin` key, `human password` sets and prints once and stores nothing, team room levels and record exactly agent-bus-protocol.md §8, `add-member` writes the bundle as a tar on stdout and the wrapper places it with `install -o "$SUDO_UID"` (modes asserted), `--no-human-text`, remove/rotate-token/human commands, sync-team on change; `tuwunel.toml` key set; drop-in render (IP filter, restart policy, device dependencies); no secret on any stream but the two payloads | U09, U13, U14           | C                                  |
| U16 | Installer                            | `files/usr/local/sbin/agent-bus-install`, `files/etc/systemd/system/agent-bus-*`, `files/usr/local/share/agent-bus/{tuwunel.pin,resolv.conf}`, the version-pin registration                                                                                                                        | `scripts/test-agent-bus-install.bash` (argument parsing, malformed and wildcard listen addresses and malformed CIDRs refused, file layout in a temporary root, no download when the pinned hash matches, no restart and no `CHANGED` when nothing rendered changed, a unit that fails to start, as an unknown key makes it, fails the readiness step); host: `software` and `team` on the desktop, a second run reports nothing changed, `check`, `remove`                                                                                                                                                                      | U00, U15                | C + H                              |
| U17 | M1: host-to-host                     | `acceptance.bash` first slice, against the installer run directly                                                                                                                                                                                                                                  | host: two `host` members (dedicated test users) of the acceptance team: `review` sent, received by `wait`, `ack`; `TIMEOUT` when unanswered; a human message (as the human account, by `curl`) delivered only by the addressed member's pingbus                                                                                                                                                                                                                                                                                                                                                                                 | U11, U16                | H                                  |
| U18 | Plugin, skill, launcher              | `files/opt/claude-yolo/optional/agent-bus/{plugin/pingbus/**,settings.json}`, the kit's `agent-bus-claude`                                                                                                                                                                                         | `test_plugin_contract.py`: `hooks.json` commands are real `pingbus hook` subcommands; SKILL.md names only real commands and carries the HUMAN/PING rules; launcher argv test; launcher refuses beside an Element profile                                                                                                                                                                                                                                                                                                                                                                                                        | U12                     | C                                  |
| U19 | ccy: image, entrypoint and the dist  | `Dockerfile`, `entrypoint.sh`, `play-claude-yolo.yml` (stages, builds the zipapp); `lib/common.bash` `ccy_env_local_dist_text` gains the commented `PINGBUS_TEAMS` block and `CCY_ENV_LOCAL_DIST_VERSION` goes up; `CCY_VERSION` and container version bumps, changelog, `docs/ccy.md` rows        | `scripts/test-ccy-agent-bus.bash` (a `qa-all.bash` gate): opt-in from `ccy.env.local` only, refusal on a broken bundle, args added inside the wrapper's `--`, inert when unset; `scripts/test-ccy-env-local-dist.bash` extended for the new block and version                                                                                                                                                                                                                                                                                                                                                                   | U13, U18                | C                                  |
| U20 | M2: ccy members, woken               | `acceptance.bash` slice                                                                                                                                                                                                                                                                            | host: two ccy sessions in different projects exchange `review` and `ack`; the idle one woken by the socket, including a second notice with the same count; a session without the socket woken by `wait`; a human message reaches the addressed one                                                                                                                                                                                                                                                                                                                                                                              | U17, U19                | H                                  |
| U21 | Non-ccy members and docs             | kit README per type; `docs/agent-bus.md` (teams, placement convention, installer for other projects, joining per type, the dedicated agent user, what joining a team hosted elsewhere grants and `--no-human-text`, backups as the root of trust, human lock-down, limits), `docs/README.md` index | `qa-docs.bash`; bundle README per type                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | U15, U18                | C                                  |
| U22 | Play                                 | `playbooks/imports/play-agent-bus.yml` (imported by `playbook-main.yml`), `localhost.yml.dist` placeholder, drift pairs                                                                                                                                                                            | `ansible-playbook --syntax-check` as a gate; host: play run with a scratch team present then absent, a second run reports no change                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | U16                     | C + H                              |
| U23 | M3a: other encapsulations, same host | `acceptance.bash` slice                                                                                                                                                                                                                                                                            | host: an LXC, a docker (docker CLI) and a VM member join the desktop's team and ping; each leg SKIPPED-NEEDS-OWNER when its engine or a VM is missing, which blocks plan close                                                                                                                                                                                                                                                                                                                                                                                                                                                  | U20, U21, U22           | H                                  |
| U24 | M3b: another host                    | `acceptance.bash` slice                                                                                                                                                                                                                                                                            | host: the standalone installer run inside a Fedora test VM; a member on the desktop pings that VM's team over a WireGuard link with a fixed test name that the script creates, removes in an `EXIT` trap, and removes at start if a failed run left it; SKIPPED-NEEDS-OWNER without a VM                                                                                                                                                                                                                                                                                                                                        | U23                     | H                                  |
| U25 | Element desktop                      | `play-agent-bus-element.yml`                                                                                                                                                                                                                                                                       | `config.json` key test; refusal for a user with `~/.config/pingbus/`; `--syntax-check`; host: profile resolves, launcher works                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | U00, U22                | C + H                              |
| U26 | TLS (only if H7 needs it)            | the installer's TLS: a team-private CA, or the route the owner picks (section 3.3)                                                                                                                                                                                                                 | decided with the unit, if built                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | U16, H7                 | C + H                              |
| U27 | M4: deploy and acceptance            | `deploy.bash`, `acceptance.bash` (P1-P8, backup and restore round trip), `meta-deploy.bash` entry                                                                                                                                                                                                  | host run                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | U24, U25 (U26 if built) | H                                  |
| U28 | Review and end-to-end                | fixes only                                                                                                                                                                                                                                                                                         | `qa-all.bash` (coordinator), `qa-reviewer`; the PLAN.md success criteria                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | all                     | H                                  |

Deferred (no success criterion needs them): `agent-bus rotate-admin`, `pingbus show`,
`peers` and `tail`.

Parallel waves: {U00, U01, U02} → {U03, U04, U08, U14} → {U05, U06, U07, U09} →
{U10, U13} → {U11, U15} → {U12, U16} → {U17, U18, U22} → {U19, U21, U25, U26 if needed} →
{U20} → {U23} → {U24} → {U27} → {U28}.

### Milestones

| Milestone                                   | Units            | Proven by                                                                                     |
| ------------------------------------------- | ---------------- | --------------------------------------------------------------------------------------------- |
| M0 probes                                   | U00, U01         | journalled probe results; fixtures recorded                                                   |
| M1 host-to-host ping                        | U02-U11, U13-U17 | U17: a real Tuwunel from the installer, two members, a ping, a human message                  |
| M2 ccy members, woken                       | U12, U18-U20     | U20: two ccy sessions, the idle one woken by the socket                                       |
| M3 other encapsulations and hosts, the play | U21-U24          | U23: LXC, docker, VM members; U24: the standalone installer on another machine over WireGuard |
| M4 Element, deploy and acceptance           | U25-U28          | P1-P8, backup and restore, the PLAN.md success criteria                                       |

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
- **U01** Claude Code, with a logged-in child `claude` (in the container if it can be given
  a credential, else on the host): hooks under `--plugin-dir` (all four events), and the
  inbox socket as section 6 uses it, including how long identical messages are deduplicated.

## Decisions

| #   | Decision                                                                                                                                                                                                                                                                  | Reason                                                                                                                                                                                                     |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | The homeserver is a pinned static Tuwunel binary under a hardened system unit as the `agent-bus` user, not a container                                                                                                                                                    | The kernel IP filter and resolver lock-out need a system unit; no engine, linger or subuid on servers; the mode the spike proved.                                                                          |
| D2  | One installer (`agent-bus-install`), called by this repository's core play on every desktop and by other projects' IaC                                                                                                                                                    | Owner, answer 4; one implementation keeps every homeserver host identical.                                                                                                                                 |
| D3  | One instance and one room per team; instance name = team name                                                                                                                                                                                                             | Keeps a team's accounts, admin, backup and placement self-contained.                                                                                                                                       |
| D4  | Listen on loopback plus any address the team file names (no allowlist, no desktop/server distinction; every concrete address allowed, the wildcards `0.0.0.0` and `::` refused); connections only from `allow_from`                                                       | Owner, answer 5 and the 2026-10-06 decision; the team chooses its network. A wildcard listens on interfaces nobody named, and P2 cannot check what is bound (section 3.3).                                 |
| D5  | Reachability enforced by firewalld and, independently, by the unit's `IPAddressDeny`/`IPAddressAllow`                                                                                                                                                                     | Either layer alone holds; the unit layer also stops egress.                                                                                                                                                |
| D6  | No TLS in v1; TLS only if probe H7 shows the phone client needs it                                                                                                                                                                                                        | Private paths (in-kernel, WireGuard) need none; on any other network credentials travel in clear, documented (section 3.3).                                                                                |
| D7  | Members of every encapsulation are first-class; one bundle shape for all                                                                                                                                                                                                  | Owner, answer 2 (reverses the deferral).                                                                                                                                                                   |
| D8  | The team's secrets belong to the `agent-bus` system user; admin commands go through `sudo agent-bus`                                                                                                                                                                      | No agent runs as that user, so no host or container member can read the root of trust.                                                                                                                     |
| D9  | pingbus and the admin tool ship as reproducible zipapps built from `helpers/` by the installer and by the ccy play independently                                                                                                                                          | One validator everywhere; the core ccy play never depends on the bus.                                                                                                                                      |
| D10 | `admin` creates the team room and is its only state writer; the team record (humans, roles, allowlists) lives in room state                                                                                                                                               | One place for humans to change membership; receivers trust only what `admin` wrote, and member bundles carry no allowlist to tamper with.                                                                  |
| D11 | Pings are `m.notice` with a structured `agent_bus.ping` key and a body that must equal its rendering                                                                                                                                                                      | Humans see pings in every client, phone included, with no relay; nothing can hide in the text.                                                                                                             |
| D12 | Human free text reaches an agent when its sender is a listed human and it mentions that agent or `@room`                                                                                                                                                                  | Owner, answer 3 (reverses issue #59's rule and the control-room split).                                                                                                                                    |
| D13 | Agent free text without the ping key is dropped by every receiver; the server cannot block it; an agent may send text to a human (`pingbus say`), refused on send when secret-shaped (`secret`), and ignored by every agent that receives it (D30)                        | Power levels are per event type, not per `msgtype`; agent-to-agent traffic stays pings only; the owner allowed agent-to-human text (2026-10-06).                                                           |
| D14 | No warden                                                                                                                                                                                                                                                                 | Humans address agents directly and see pings as notices, so neither translation nor mirroring is left for it to do.                                                                                        |
| D15 | Event prefix `agent_bus.` (agent-bus-protocol.md §2)                                                                                                                                                                                                                      | Owner, answer 6: clearly not a domain name; inside the Matrix identifier grammar.                                                                                                                          |
| D16 | Wake by the inbox socket through a hook-started watcher (counts plus a rising notice number; liveness by `flock`, one thread per team); `pingbus wait` as fallback; a Stop hook as guard; plugin loaded by `--plugin-dir`                                                 | Section 6 and [U01].                                                                                                                                                                                       |
| D17 | Element is not confined; P4 proves its traffic by a pasta capture of that profile alone                                                                                                                                                                                   | The user service manager cannot apply cgroup IP filters to a Flatpak.                                                                                                                                      |
| D18 | ccy opt-in is `PINGBUS_TEAMS` in the untracked `ccy.env.local`, placed by IaC and bound read-only by ccy since 3.84.0 (Plan 00160 Task 3.3); its placeholder is in ccy's own `ccy_env_local_dist_text`; the bundle lives in the checkout's ignored `.claude/ccy/pingbus/` | Owner, answer 8; no host-side launcher change; the handle names the checkout's seat (owner, answer 7).                                                                                                     |
| D19 | The handle's `<host>` is the install's role (`HOOKS_DAEMON_HOSTNAME`) or an explicit `--host`; never `CCY_HOST_HOSTNAME` or the hostname                                                                                                                                  | One value names the install for the hooks daemon and the bus; handles reach public forge text, so a real hostname must never be the fallback.                                                              |
| D20 | The forge and provenance check runs on send and before the inbox write; hooks and inbox reads stay offline; `recv` re-fetches before printing                                                                                                                             | A sender can skip pingbus; the inbox is a cache.                                                                                                                                                           |
| D21 | References use full 40-hex SHAs and lowercase `owner/repo`; `pr:` carries its head SHA; each verb accepts only agent-bus-protocol.md §5's forms                                                                                                                           | Unambiguous; issue and PR text is writable by outsiders.                                                                                                                                                   |
| D22 | The protocol spec lives at `docs/agent-bus-protocol.md`, tied to the constants by a contract test                                                                                                                                                                         | The issue wants a versioned spec that outlives this plan.                                                                                                                                                  |
| D23 | No terminal Matrix client                                                                                                                                                                                                                                                 | Element (desktop or phone) is the human's view; a terminal view (`pingbus tail`) is deferred.                                                                                                              |
| D24 | `wait` default timeout 1500 s                                                                                                                                                                                                                                             | Under the Monitor tool's 30-minute cap; bounds an orphaned waiter.                                                                                                                                         |
| D25 | Human passwords are set and printed once by `human password` and never stored; human accounts start with a discarded password                                                                                                                                             | Nothing that lets a holder post as a human sits at rest or in a backup; the installer's output never carries one.                                                                                          |
| D26 | No agent runs as a user holding a human's Matrix session; bare-desktop agents use a dedicated user                                                                                                                                                                        | Element's token in the home is the human: any same-user process could post `@room` to every agent (security review 2, B1).                                                                                 |
| D27 | A member may join with `human_text: false` (pings only)                                                                                                                                                                                                                   | Joining a team gives its homeserver's operator the power to instruct the agent; this narrows it for a team hosted elsewhere.                                                                               |
| D28 | U23 and U24 split same-host encapsulations from the other host; a leg lacking its engine or VM reports SKIPPED-NEEDS-OWNER and blocks close                                                                                                                               | Owner answer 2 makes docker, LXC and VM members first-class, so a silent skip would hide a missing guarantee.                                                                                              |
| D29 | The receive flood count lives in the receiving process only; each `recv` with no watcher running starts from zero (agent-bus-protocol.md §10)                                                                                                                             | A flood is at most 60 per sender per run, every event still passes every other check, and persisting the count would add state to the inbox.                                                               |
| D30 | An agent receiving agent-to-human text (content key `agent_bus.text`) ignores it silently (`agent-text`): no `dropped.log` line, no `DROPPED` count, no non-zero `recv` exit (agent-bus-protocol.md §7, §9)                                                               | Coordinator decision, 2026-10-06: the text is legitimate traffic for the humans, not a fault; counting it as a drop would raise `DROPPED` and `recv` exit 6 on every answer a coordinator gives the owner. |
| D31 | A homeserver 429 asking for a wait over 60 s is not slept: the client reports exit 9 at once, as a forge rate limit does (agent-bus-protocol.md §10, `limits.SERVER_429_WAIT_MAX_S`)                                                                                      | U09 review: an uncapped `Retry-After` would hold a `send`, a `wait` (at most 1790 s) or a 30 s long-poll for as long as the server says, hanging the process.                                              |

## Owner questions

None open. The three left after the revision were answered on 2026-10-06: an agent may send
text to a human (section 7, D13); a homeserver may listen on any address it is given, with
no TLS in v1 (section 3.3, D4, D6); and ccy binds an existing `ccy.env.local` read-only
(section 5.3; Plan 00160 Task 3.3, ccy 3.84.0).
