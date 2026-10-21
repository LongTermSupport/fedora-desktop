# Plan 00209 N0, area D: coexistence and migration

This is area D of the v2 design (frame: [DESIGN-v2-FRAME.md](DESIGN-v2-FRAME.md)). It owns the v2
product name, every name and path v2 uses, the proof that a v2 install touches nothing v1 uses,
the per-team migration from v1 to v2, the pilot, and the retirement of v1.

Sources are cited by short key:

- `FRAME` is the frame.
- `DESIGN Rn` is the requirement list in [DESIGN.md](DESIGN.md) section 2. `DESIGN 12` is its section 12.
- `RH`, `RP` and `RL` are `RESEARCH-host.md`, `RESEARCH-pingbus.md` and `RESEARCH-lxc-robustness.md`.
- `S211` is Plan 00211's `SPEC.md`.
- `SPEC-state Sn` and `SPEC-matrix Mn` are the M1 specs.
- `PROTO` is `docs/agent-bus-protocol.md`.
- `INV n` is section n of [RESEARCH-v1-names.md](RESEARCH-v1-names.md), v1's name and path
  inventory, built for this design from the plays, `files/`, `helpers/` and the digests above,
  with `file:line` evidence for every row.
- `TS n` is section n of [RESEARCH-v1-team-state.md](RESEARCH-v1-team-state.md): what a running v1
  team holds (accounts, rooms, member state, in-flight items, Element, Tuwunel facts).
- `LESSON n` is row n of the incident table in TS 7.

Install-specific values are placeholders throughout: `<team>`, `<seat>`, `<user>`, `<home>`,
`<checkout>`, `<container>`, `<version>`.

## 1. Requirements

| ID  | Requirement                                                                                                                                                                                                                    | Source                           |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------- |
| D1  | v2 has a real product name that says what it does to a reader new to it, with no version suffix.                                                                                                                               | owner via lead; qa-reviewer.md B |
| D2  | Every v2 binary, plugin, skill, unit, user, group, directory, file, env variable, port, interface, firewall record, sudoers file, Element profile, host_vars key, play, Go package and Matrix namespace is disjoint from v1's. | FRAME "installs beside v1"       |
| D3  | A v2 install with no teams is inert: no unit active, no port bound, no file written outside v2's own roots.                                                                                                                    | FRAME                            |
| D4  | A test proves D2 and D3 mechanically, both statically (in QA) and dynamically (in the VM lab, against a live v1).                                                                                                              | FRAME; lead                      |
| D5  | A team is on exactly one system at any moment. A session is on exactly one system: it never loads both kits.                                                                                                                   | FRAME "no team mixed"            |
| D6  | Teams move one at a time (or one linked set, D22). The owner starts the move from ccy-teams, and host runs go through meta-deploy.                                                                                             | FRAME; CLAUDE.md                 |
| D7  | The migration is fail-fast. Each step checks its precondition and stops with a named reason. Every step can be re-run: a re-run resumes from the recorded phase and never repeats a side effect.                               | CLAUDE.md fail fast; LESSON 13   |
| D8  | The v1 team stays intact and usable until the migration commits. Rollback before commit restores it, with every queued item still deliverable.                                                                                 | FRAME                            |
| D9  | No silent message loss. Every queued and in-flight item is either carried to its recipient on v2, settled by a notice to its sender, or listed by name in the migration report. Nothing is dropped without an entry.           | FRAME                            |
| D10 | Room history stays readable by the team's humans in Element after the move, and a lossless export of it survives v1's retirement.                                                                                              | lead (3); FRAME                  |
| D11 | Seats, roles, parked members, seat counters and retired (burnt) handles carry over, so a seat number means the same member before and after.                                                                                   | RH 4; DESIGN R-seat rows         |
| D12 | Humans get a v2 account with the same name and a new password through the existing ccy-teams login flow. Their v1 Element profile stays as the archive.                                                                        | RH 1.2; Element digest           |
| D13 | Running ccy sessions move by a restart onto the v2 kit that resumes the same conversation. LXC members move through the v2 apply, and a stopped container is deferred, not a blocker.                                          | RL 3.5, 6.01; LESSON 6           |
| D14 | An acceptance check proves a moved team works before the move is called done.                                                                                                                                                  | FRAME                            |
| D15 | A pilot runs first: a throwaway v2 team, then a throwaway v1 team migrated and rolled back, before any real team.                                                                                                              | lead (4)                         |
| D16 | v1 is retired only after every team has moved, and the retirement is raised with the owner then.                                                                                                                               | FRAME                            |
| D17 | Least privilege: the migration reaches v1 only through v1's own CLI and read-only file reads, in one adapter package. v2 processes cannot see v1's paths.                                                                      | DESIGN 6; R37-R39                |
| D18 | Every cost of running both systems side by side is stated: the processes, ports and units per team, during the migration window and after it.                                                                                  | coordinator                      |

## 2. The product name

### 2.1 Default: `teamchat`

v2 is the system that lets the agents and humans of a team message each other in shared rooms.
The name says that plainly:

- **team** is the unit everything is organised by.
- **chat** is what happens there: pings, replies, reports and questions, read by agents in their
  sessions and by humans in Element.

`teamchat` also works as a prefix everywhere a name is needed: `teamchat-server@<team>.service`,
`TEAMCHAT_`, `teamchat.` in Matrix, `/var/lib/teamchat/`. It has no hits in tracked files (a
`git grep -i` for `teamchat` and `team-chat` finds nothing). It does not collide with `agent-bus`,
`pingbus` or `ccy-*`. It is not a metaphor and it carries no version.

Its weakness is that "chat" undersells pings with acknowledgement deadlines. That is acceptable:
the CLI verbs and the skill say what each message kind does, and a reader new to the system still
guesses its purpose from the name.

### 2.2 Alternatives

| Name        | For                                                                                      | Against                                                                                                                                                               |
| ----------- | ---------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agentroom` | Says where agents meet; free in the repo.                                                | Leaves the humans out, though they are half the users; "room" is also a Matrix term inside the system (main room, reports room), so `agentroom` reads as one of them. |
| `teampost`  | Says that messages are posted to the team; free in the repo; fits reports and questions. | Postal wording suggests slow, one-way mail, the opposite of a ping.                                                                                                   |

Rejected without a row:

- `agent-team` is the generic phrase in 41 v1 files, so it blurs with v1.
- `roster` is already v1's member-list concept (`helpers/agent_bus/declare.py`).
- `teamroom` and `team-room` are v1 test identifiers and v1 prose.
- `switchboard`, `courier` and `intercom` are metaphors that a reader has to decode.
- `teamtalk` is an existing commercial voice product.
- `agent-bus-2` and `pingbus2` carry a version suffix, which the lead ruled out.

**Owner decision OD-D1** (section 9): the name. Default `teamchat`.

The rest of this file uses `teamchat`. If the owner picks another name, the name is replaced
wholesale. Every name in section 3 is derived from the product name by one rule, so the
replacement is mechanical.

## 3. Every name and path v2 uses

### 3.1 The derivation rule

Every v2 name is formed in one of these ways:

- `teamchat`, alone or with a hyphenated suffix (paths, binaries, units, users, plays);
- `teamchat_` (host_vars keys);
- `TEAMCHAT_` (environment variables);
- `teamchat.` (Matrix namespace).

None of these is a prefix of a v1 name, and no v1 name is a prefix of one of them. v1's families
are `agent-bus`, `agent_bus`, `agentbus`, `pingbus`, `PINGBUS_`, `AGENT_BUS_`, `CCY_TEAMS_PINGBUS`
and `CC_TEAMS_*` (INV 0-11). The static gate in section 4.1 tests this property.

Shared surfaces that belong to neither system are used by both, unchanged:

- ccy's session registry `~/.local/state/ccy/sessions/`;
- `ccy`'s `--teams` and `--no-bus` flags, which resolve the system per team (3.9);
- `HOOKS_DAEMON_HOSTNAME`, the install's role name;
- the user play lock `$XDG_RUNTIME_DIR/fedora-desktop-plays.lock`.

### 3.2 The placeholders the other areas use

| Placeholder        | Means                                                    | Value                                                                                               |
| ------------------ | -------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `<v2-bin>`         | the member binary (B)                                    | `teamchat`                                                                                          |
| `<v2-admin>`       | the host admin and installer binary (C)                  | `teamchat-admin`                                                                                    |
| `<v2-unit>`        | the homeserver unit (C)                                  | `teamchat-server@<team>.service`                                                                    |
| `<v2-state>`       | a member's state root for one team (B)                   | per member kind, table 3.6                                                                          |
| `<v2-host-state>`  | a team's homeserver state (C)                            | `/var/lib/teamchat/<team>/`                                                                         |
| `<v2-root-record>` | root-only install records and migration state (C, D)     | `/var/lib/teamchat-admin/<team>/`                                                                   |
| `<v2-status>`      | the records ccy-teams reads without sudo (C)             | `/var/lib/teamchat-status/`                                                                         |
| `<v2-kit>`         | the versioned member kit (B, C)                          | `/usr/local/lib/teamchat/<version>/` (host, LXC); `/opt/claude-yolo/optional/teamchat/` (ccy image) |
| `<v2-ns>`          | the Matrix namespace for custom event types and keys (A) | `teamchat`                                                                                          |
| `<v2-env>`         | the environment variable prefix (B)                      | `TEAMCHAT_`                                                                                         |

### 3.3 Binaries and Go packages

| v2 name                                                                          | Kind                                                                                                                                                                                                                    | v1 counterpart (INV 1)                            |
| -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `teamchat`                                                                       | Static Go member binary: CLI, keeper, worker, hooks, status line (B). On PATH as `/usr/local/bin/teamchat`, a symlink into `<v2-kit>/bin/teamchat`.                                                                     | `pingbus` zipapp                                  |
| `teamchat-admin`                                                                 | Static Go, root, at `/usr/local/sbin/teamchat-admin`: installer, admin verbs, health, backup, restore, migration steps (C, D). C may split the installer out as `teamchat-install`; that name is reserved for that use. | `agent-bus`, `agent-bus-install`, `agent-bus.pyz` |
| `teamchat-claude`                                                                | Launcher for non-ccy members (LXC, host): execs `claude` with the v2 plugin dir and settings (B). On PATH as `/usr/local/bin/teamchat-claude`.                                                                          | `agent-bus-claude`                                |
| `/usr/local/lib/teamchat/server/tuwunel-<version>`                               | v2's own pinned homeserver binary (C), never v1's copy, so a v1 pin bump cannot move v2 and the reverse.                                                                                                                | `/usr/local/lib/agent-bus/tuwunel`                |
| `src-go/cmd/teamchat`, `src-go/cmd/teamchat-admin`, `src-go/cmd/teamchat-claude` | Go entry points.                                                                                                                                                                                                        | `src-go/cmd/pingbus-status-line`                  |
| `src-go/internal/teamchat/...`                                                   | All v2 Go packages. `src-go/internal/teamchat/paths` is the single source of every path and name constant in this section.                                                                                              | `src-go/internal/{agentbus,pingbus}`              |
| `src-go/internal/teamchat/migrate/v1`                                            | The only package that may name v1 (D17, 4.1).                                                                                                                                                                           | (new)                                             |

`ccy-teams` stays `ccy-teams`. It is the owner's tool for both systems (3.9).

### 3.4 Plugin, skill and session surfaces

| v2 name                                                                                                                      | v1 counterpart (INV 2)                                |
| ---------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| Plugin `teamchat` at `<v2-kit>/plugin/teamchat/`, loaded only by `--plugin-dir`                                              | plugin `pingbus`                                      |
| Skill `teamchat:teamchat`                                                                                                    | `pingbus:pingbus`                                     |
| Hook commands `teamchat hook <event>`, gated on `TEAMCHAT_MEMBERSHIP`                                                        | `pingbus hook <event>`, gated on `PINGBUS_MEMBERSHIP` |
| Status line command `teamchat status-line`                                                                                   | `pingbus status-line`                                 |
| Envelope and notice text (A and B own the wording): it must start with `teamchat` and never contain `agent-bus` or `pingbus` | `agent-bus delivery <nonce>:`, `agent-bus: N pending` |
| Session settings file `<v2-kit>/settings.json`; the per-session copy lives in the session home                               | `optional/agent-bus/settings.json`                    |

The skill text must not use v1's trigger phrases. A session never loads both plugins (D5), but the
v1 skill description keys on `agent-bus delivery`, so distinct text keeps the skill loader from
picking the wrong skill in a session where the user loaded one by hand.

### 3.5 Host paths, users, units, network

| v2 path or name                                                                                                          | Owner, mode                       | What                                                                                                                                                                | v1 counterpart (INV 4, 6, 7)                          |
| ------------------------------------------------------------------------------------------------------------------------ | --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| `/usr/local/lib/teamchat/<version>/{bin/teamchat,bin/teamchat-claude,plugin/teamchat/,settings.json,README.*}`           | root 0755                         | The versioned kit. `current` is a symlink to the newest. The two newest versions are kept, so a session still running on the older kit keeps its files (LESSON 10). | `/usr/local/share/agent-bus/kit/` (replaced in place) |
| `/usr/local/lib/teamchat/server/{tuwunel-<version>,tuwunel.pin}`                                                         | root 0755                         | homeserver binary and its pin                                                                                                                                       | `/usr/local/lib/agent-bus/tuwunel`                    |
| `/etc/teamchat/`                                                                                                         | root 0755                         | host-wide v2 config, if C needs any                                                                                                                                 | `/etc/agent-bus/`                                     |
| `/var/lib/teamchat/<team>/`                                                                                              | the team's server user, 0700      | `<v2-host-state>`: db, backups, secrets, rendered config, team file, registry                                                                                       | `/var/lib/agent-bus/<team>/`                          |
| `/var/lib/teamchat-admin/<team>/`                                                                                        | root 0700                         | `<v2-root-record>`: install record, firewall record, state backups, `migration/` (5.3)                                                                              | `/var/lib/agent-bus-install/<team>/`                  |
| `/var/lib/teamchat-status/{<team>.json,health/<team>.json,lxc/<team>.json}`                                              | root:<stamp group> 0640, dir 0750 | `<v2-status>`: applied, health and LXC records, and the migration phase (5.3), read by ccy-teams                                                                    | `/var/lib/agent-bus-applied/`                         |
| System user and group: per team `teamchat-<team>` if C chooses per-team users; one `teamchat` if C chooses a shared user | no login, no home                 | runs the homeserver                                                                                                                                                 | one shared `agent-bus` user                           |
| `teamchat-server@<team>.service` (+ `.d/`)                                                                               | system unit                       | homeserver                                                                                                                                                          | `agent-bus-hs@<team>.service`                         |
| `teamchat-health@<team>.{service,timer}`                                                                                 | system units                      | health record                                                                                                                                                       | `agent-bus-hs-health@`                                |
| `teamchat-backup@<team>.{service,timer}`                                                                                 | system units                      | backups                                                                                                                                                             | `agent-bus-backup@`                                   |
| `/etc/sudoers.d/teamchat-read-only`                                                                                      | root 0440                         | the NOPASSWD read rules ccy-teams needs, pinned to `/usr/local/sbin/teamchat-admin` (C owns the exact rules)                                                        | `/etc/sudoers.d/agent-bus-read-only`                  |
| NM dummy connection and interface `teamchat0`, address `teamchat_address`                                                | NetworkManager                    | v2's bus address                                                                                                                                                    | `agentbus0`, `agent_bus_address`                      |
| firewalld rich rules, recorded in `/var/lib/teamchat-admin/<team>/firewall.rules`                                        | root                              | v2 adds and removes only the rules in its own record                                                                                                                | `.../agent-bus-install/<team>/firewalld.rules`        |
| Homeserver name `<team>.teamchat.internal`                                                                               |                                   | never equal to v1's, so no Element profile, token or bundle can be pointed at the wrong server by mistake                                                           | `<team>.agent-bus.internal`                           |
| Ports: one per team, declared in `teamchat_teams[].port`                                                                 |                                   | 3.8                                                                                                                                                                 | `agent_bus_teams[].port`                              |

The team name keeps v1's grammar `^[a-z][a-z0-9-]{0,23}$` (RH 5). If C chooses per-team users,
the grammar is shortened to `{0,22}` so that `teamchat-<team>` fits the 32-character user name
limit. A migrated team keeps its v1 name (5.1). The v1 lesson "never reuse a team name" (LESSON 14)
does not apply across systems, because the timer, unit and record names differ.

**Why a separate interface `teamchat0`, not v1's `agentbus0`.** v2 could bind its ports on v1's
address. That would make v2 depend on a v1 object that v1's retirement removes, and v1's play owns
that NetworkManager connection. A second dummy interface costs no resident process. The LXC bridge
subnet is added to v2's `allow_from` by v2's play, as v1 does it for v1 (INV 7).

### 3.6 Member paths and environment

| Member kind      | `<v2-state>` for one team                                                                                                                       | v1 counterpart (INV 4, 5)                                      |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| ccy seat         | `<checkout>/.claude/ccy/teamchat/seats/<team>/<seat>/` (ignored by the same `.claude/ccy/.gitignore` mechanism)                                 | `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`          |
| ccy session home | `/run/teamchat/home/` inside the container (tmpfs, 0700). B may choose another place, but never `/tmp/pingbus-home`.                            | `/tmp/pingbus-home`                                            |
| ccy membership   | host `$XDG_RUNTIME_DIR/teamchat/membership/<session>/`, mounted read-only at `/run/teamchat/membership`                                         | `$XDG_RUNTIME_DIR/ccy/membership/...` at `/run/ccy/membership` |
| locks            | `$XDG_RUNTIME_DIR/teamchat/{alloc,change}/`, never in the checkout or a read-only mount (LESSON 9)                                              | `$XDG_RUNTIME_DIR/{ccy,pingbus}/...`                           |
| host and LXC     | `<home>/.config/teamchat/<team>/`, and `<home>/.config/teamchat/env` for the launcher's settings                                                | `<home>/.config/pingbus/...`                                   |
| LXC guest unit   | `teamchat-session-<user>.service`, tmux socket `-L teamchat`, session `teamchat`                                                                | `agent-bus-claude-<user>.service`, `-L agent-bus-claude`       |
| env              | `TEAMCHAT_*` only. B names each variable, for example `TEAMCHAT_MEMBERSHIP` and `TEAMCHAT_HOME`.                                                | `PINGBUS_*`, `CCY_TEAMS_PINGBUS`, `CC_TEAMS_*`                 |
| ccy image kit    | `/opt/claude-yolo/optional/teamchat/{bin/teamchat,plugin/teamchat/,settings.json}`. The entrypoint links `/usr/local/bin/teamchat`.             | `/opt/claude-yolo/optional/agent-bus/`                         |
| LXC guest kit    | `/usr/local/lib/teamchat/<version>/`, linked as `/usr/local/bin/{teamchat,teamchat-claude}`. A static binary, so no Python 3.11 floor (RL 1.3). | `/usr/local/share/agent-bus/kit` copy                          |

Adding the v2 kit to the ccy image is a CCY version bump, because the image changes. The v1 kit in
the image is not touched.

### 3.7 Ansible, docs, tests, Element

| v2 name                                                                                                                                                                                                                         | v1 counterpart (INV 10, 7)                             |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| host_vars `teamchat_teams`, `teamchat_address`, `teamchat_lxc_members`, `teamchat_element_teams` (C owns the fields)                                                                                                            | `agent_bus_*`                                          |
| Play `playbooks/imports/play-teamchat.yml` (and `play-teamchat-element.yml` if C keeps Element separate), tag `teamchat`, run-scope variable `teamchat_only_teams`                                                              | `play-agent-bus.yml`, `agent_bus_only_teams`           |
| Docs `docs/teamchat.md`, `docs/teamchat-protocol.md`, `docs/teamchat-migration.md`                                                                                                                                              | `docs/agent-bus*.md`                                   |
| Tests `scripts/test-teamchat-*.bash`, Go tests beside the packages, VM lab scenarios `teamchat-*`                                                                                                                               | `scripts/test-agent-bus-install.bash` and others       |
| Element profile `--profile teamchat-<team>`, its directory `~/.var/app/im.riot.Riot/config/Element-teamchat-<team>/`, launcher `~/.local/share/applications/teamchat-<team>-element.desktop` with Name `<team> team (teamchat)` | `--profile <team>`, `agent-bus-<team>-element.desktop` |

### 3.8 Ports

v1 ports are declared per team, not computed (`teamfile.py:36-37`). v2 keeps that. A computed
port, for example a base plus an index, would be a second scheme to learn, and it would still need
the same uniqueness check. v2's declaration check (C's validator, run by `teamchat-admin` and by
ccy-teams before any write) refuses a port that:

1. another `teamchat_teams` entry declares;
2. any `agent_bus_teams` entry declares. This reads the declaration, never v1's runtime files;
3. is listening now (`ss`), unless it is this team's own running `teamchat-server@<team>`.

During a migration both servers run, so the v2 team needs a port of its own. ccy-teams proposes
the lowest port above the highest declared port in either list, and the owner can change it. After
v1 retires, v1's ports are free again.

### 3.9 ccy-teams' view of both systems

ccy-teams lists the teams of both systems in one board, with a `system` column that reads
`agent-bus` or `teamchat`. A team name is live on exactly one system. The rule is in the record
ccy-teams already reads:

- **v1-live**: a team is in `agent_bus_teams` and has no committed v2 migration.
- **v2-live**: a team is in `teamchat_teams`, and either it was created on v2 or its record
  `/var/lib/teamchat-status/<team>.json` shows `migration.phase` at `committed` or later.
- **Migrating**: a team in both lists before commit is v1-live. Its v2 side is shown as
  `staged` and cannot be joined by sessions; only the migration's own probe can use it.

`ccy --teams <seat>@<team>[,...]` resolves each team's live system. The decision is made once, at
launch:

- If every team is on one system, the session gets that system's kit, plugin, settings and
  membership mount, and only that system's.
- If the teams are on both systems, the launch is refused (exit 64) with the list of teams by
  system and the two choices: run two sessions, or migrate the remaining v1 team. This is the
  default of **OD-D2**.

The reason for the refusal: two kits in one session would share one status line and one
`crossSessionInbound` key, and two watchers would write to one inbox socket. That socket drops two
messages less than 2 s apart (RP 4), so each system's pacing would cause the other's drops.
