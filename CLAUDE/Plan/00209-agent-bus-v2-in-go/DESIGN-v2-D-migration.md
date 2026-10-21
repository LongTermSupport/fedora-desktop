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

| ID  | Requirement                                                                                                                                                                                                                                                             | Source                           |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| D1  | v2 has a real product name that says what it does to a reader new to it, with no version suffix.                                                                                                                                                                        | owner via lead; qa-reviewer.md B |
| D2  | Every v2 binary, plugin, skill, unit, user, group, directory, file, env variable, port, interface, firewall record, sudoers file, Element profile, host_vars key, play, Go package and Matrix namespace is disjoint from v1's.                                          | FRAME "installs beside v1"       |
| D3  | A v2 install with no teams is inert: no unit active, no port bound, no file written outside v2's own roots.                                                                                                                                                             | FRAME                            |
| D4  | A test proves D2 and D3 mechanically, both statically (in QA) and dynamically (in the VM lab, against a live v1).                                                                                                                                                       | FRAME; lead                      |
| D5  | A team is on exactly one system at any moment. A session is on exactly one system: it never loads both kits.                                                                                                                                                            | FRAME "no team mixed"            |
| D6  | Teams move one at a time (or one linked set, D19). The owner starts the move from ccy-teams, and host runs go through meta-deploy.                                                                                                                                      | FRAME; CLAUDE.md                 |
| D7  | The migration is fail-fast. Each step checks its precondition and stops with a named reason. Every step can be re-run: a re-run resumes from the recorded phase and never repeats a side effect.                                                                        | CLAUDE.md fail fast; LESSON 13   |
| D8  | The v1 team stays intact and usable until the migration commits. Rollback before commit restores it, with every queued item still deliverable.                                                                                                                          | FRAME                            |
| D9  | No silent message loss. Every queued and in-flight item is either carried to its recipient on v2, settled by a notice to its sender, or listed by name in the migration report. Nothing is dropped without an entry.                                                    | FRAME                            |
| D10 | Room history stays readable by the team's humans in Element after the move, and a lossless export of it survives v1's retirement.                                                                                                                                       | lead (3); FRAME                  |
| D11 | Seats, roles, parked members, seat counters and retired (burnt) handles carry over, so a seat number means the same member before and after.                                                                                                                            | RH 4; DESIGN R-seat rows         |
| D12 | Humans get a v2 account with the same name and a new password through the existing ccy-teams login flow. Their v1 Element profile stays as the archive.                                                                                                                 | RH 1.2; Element digest           |
| D13 | Running ccy sessions move by a restart onto the v2 kit that resumes the same conversation. LXC members move through the v2 apply, and a stopped container is deferred, not a blocker.                                                                                   | RL 3.5, 6.01; LESSON 6           |
| D14 | An acceptance check proves a moved team works before the move is called done.                                                                                                                                                                                           | FRAME                            |
| D15 | A pilot runs first: a throwaway v2 team, then a throwaway v1 team migrated and rolled back, before any real team.                                                                                                                                                       | lead (4)                         |
| D16 | v1 is retired only after every team has moved, and the retirement is raised with the owner then.                                                                                                                                                                        | FRAME                            |
| D17 | Least privilege: the migration reaches v1 only through one adapter package. v2 code writes nothing of v1's itself: every v1 write is made by a listed v1 command or by v1's play. Reads are read-only and confined against symlinks. v2 services cannot see v1's paths. | DESIGN 6; R37-R39                |
| D18 | Every cost of running both systems side by side is stated: the processes, ports and units per team, during the migration window and after it.                                                                                                                           | coordinator                      |
| D19 | Teams that share a session (a session seated in both) form a linked set. Because a session is on one system (D5), a linked set moves together, under one commit, or the shared sessions leave the other teams first.                                                    | D5; FRAME "no team mixed"        |

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
| `/var/lib/teamchat-status/sets/<set-id>.json`                                                                            | root:<stamp group> 0640           | the migration set record: the one source of truth for which system a migrating team is live on (5.3)                                                                | (new)                                                 |
| `/etc/teamchat/stamp-group`                                                                                              | root 0644                         | v2's own record of the stamp group. v2 never reads v1's `/etc/agent-bus/stamp-group`.                                                                               | `/etc/agent-bus/stamp-group`                          |
| `SyslogIdentifier=` `teamchat-server`, `teamchat-health`, `teamchat-backup`; `RuntimeDirectory=teamchat/<team>`          | in the units                      | journal names and runtime dirs. There is no logrotate: everything logs to the journal.                                                                              | `agent-bus-*` unit names                              |

The team name keeps v1's grammar `^[a-z][a-z0-9-]{0,23}$` (RH 5). If C chooses per-team users,
the grammar is shortened to `{0,22}` so that `teamchat-<team>` fits the 32-character user name
limit. A migrated team keeps its v1 name (5.1). The v1 lesson "never reuse a team name" (LESSON 14)
does not apply across systems, because the timer, unit and record names differ.

**Why a separate interface `teamchat0`, not v1's `agentbus0`.** v2 could bind its ports on v1's
address. That would make v2 depend on a v1 object that v1's retirement removes, and v1's play owns
that NetworkManager connection. A second dummy interface costs no resident process. The LXC bridge
subnet is added to v2's `allow_from` by v2's play, as v1 does it for v1 (INV 7).

### 3.6 Member paths and environment

| Member kind      | `<v2-state>` for one team                                                                                                                                                                                                                          | v1 counterpart (INV 4, 5)                                      |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| ccy seat         | `<checkout>/.claude/ccy/teamchat/seats/<team>/<seat>/`. Git ignores it through v2's own `<checkout>/.claude/ccy/teamchat/.gitignore`, which holds `*`. v1's `.claude/ccy/.gitignore`, which v1 writes and its `seat take` checks, is never edited. | `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`          |
| status line      | cache and per-session records under the session home (B places them), never `/tmp/pingbus-sessions-<uid>` or `statusline-cache.json` in a v1 directory                                                                                             | `/tmp/pingbus-sessions-<uid>`, `statusline-cache.json`         |
| ccy session home | `/run/teamchat/home/` inside the container (tmpfs, 0700). B may choose another place, but never `/tmp/pingbus-home`.                                                                                                                               | `/tmp/pingbus-home`                                            |
| ccy membership   | host `$XDG_RUNTIME_DIR/teamchat/membership/<session>/`, mounted read-only at `/run/teamchat/membership`                                                                                                                                            | `$XDG_RUNTIME_DIR/ccy/membership/...` at `/run/ccy/membership` |
| locks            | `$XDG_RUNTIME_DIR/teamchat/{alloc,change}/`, never in the checkout or a read-only mount (LESSON 9)                                                                                                                                                 | `$XDG_RUNTIME_DIR/{ccy,pingbus}/...`                           |
| host and LXC     | `<home>/.config/teamchat/<team>/`, and `<home>/.config/teamchat/env` for the launcher's settings                                                                                                                                                   | `<home>/.config/pingbus/...`                                   |
| LXC guest unit   | `teamchat-session-<user>.service`, tmux socket `-L teamchat`, session `teamchat`                                                                                                                                                                   | `agent-bus-claude-<user>.service`, `-L agent-bus-claude`       |
| env              | `TEAMCHAT_*` only. B names each variable, for example `TEAMCHAT_MEMBERSHIP` and `TEAMCHAT_HOME`.                                                                                                                                                   | `PINGBUS_*`, `CCY_TEAMS_PINGBUS`, `CC_TEAMS_*`                 |
| ccy image kit    | `/opt/claude-yolo/optional/teamchat/{bin/teamchat,plugin/teamchat/,settings.json}`. The entrypoint links `/usr/local/bin/teamchat`.                                                                                                                | `/opt/claude-yolo/optional/agent-bus/`                         |
| LXC guest kit    | `/usr/local/lib/teamchat/<version>/`, linked as `/usr/local/bin/{teamchat,teamchat-claude}`. A static binary, so no Python 3.11 floor (RL 1.3).                                                                                                    | `/usr/local/share/agent-bus/kit` copy                          |

Adding the v2 kit to the ccy image is a CCY version bump, because the image changes. The v1 kit in
the image is not touched.

### 3.7 Ansible, docs, tests, Element

| v2 name                                                                                                                                                                                                                         | v1 counterpart (INV 10, 7)                             |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| host_vars `teamchat_teams`, `teamchat_address`, `teamchat_lxc_members`, `teamchat_element_teams` (C owns the fields), and `teamchat_migrations` (D owns it, 5.10)                                                               | `agent_bus_*`                                          |
| Play `playbooks/imports/play-teamchat.yml` (and `play-teamchat-element.yml` if C keeps Element separate), tag `teamchat`, run-scope variable `teamchat_only_teams`                                                              | `play-agent-bus.yml`, `agent_bus_only_teams`           |
| Docs `docs/teamchat.md`, `docs/teamchat-protocol.md`, `docs/teamchat-migration.md`                                                                                                                                              | `docs/agent-bus*.md`                                   |
| Tests `scripts/test-teamchat-*.bash`, Go tests beside the packages, VM lab scenarios `teamchat-*`                                                                                                                               | `scripts/test-agent-bus-install.bash` and others       |
| Element profile `--profile teamchat-<team>`, its directory `~/.var/app/im.riot.Riot/config/Element-teamchat-<team>/`, launcher `~/.local/share/applications/teamchat-<team>-element.desktop` with Name `<team> team (teamchat)` | `--profile <team>`, `agent-bus-<team>-element.desktop` |

**Names derived from the team name.** A v1 team may be named anything that fits
`^[a-z][a-z0-9-]{0,23}$`, so a v1 team named `teamchat-x` would own the v1 profile
`Element-teamchat-x`, which is v2's profile for team `x`. D requires C's validator to refuse a v2
team `x` while a v1 team `teamchat-x` is declared. The paths unit test (4.1 part 3) expands every
`<team>`-derived v2 name against every declared v1 team and fails on any equality. The other
derived names cannot collide, because v1 never puts a team name after a `teamchat-` prefix:
`teamchat-<team>` users, `teamchat-server@<team>` units, and launchers
`teamchat-<team>-element.desktop` against v1's `agent-bus-<team>-element.desktop`.

### 3.8 Ports

v1 ports are declared per team, not computed (`teamfile.py:36-37`). v2 keeps that. A computed
port, for example a base plus an index, would be a second scheme to learn, and it would still need
the same uniqueness check. D requires that C's declaration check (run by `teamchat-admin` and by
ccy-teams before any write) refuse a port that:

1. another `teamchat_teams` entry declares;
2. any `agent_bus_teams` entry declares. This reads the declaration, never v1's runtime files;
3. is listening now (`ss`), unless it is this team's own running `teamchat-server@<team>`.

During a migration both servers run, so the v2 team needs a port of its own. ccy-teams proposes
the lowest port above the highest declared port in either list, and the owner can change it. After
v1 retires, v1's ports are free again.

### 3.9 ccy-teams' view of both systems

ccy-teams lists the teams of both systems in one board, with a `system` column that reads
`agent-bus` or `teamchat`. A team name is live on exactly one system. One record decides it,
readable without sudo by ccy-teams and by the ccy launcher:

- **v2-live**: the team is in `teamchat_teams` and was created on v2, or the migration set record
  `/var/lib/teamchat-status/sets/<set-id>.json` that names the team has its phase at `committed`.
- **v1-live**: every other team in `agent_bus_teams`. This includes a team whose migration has
  not committed: its v2 side is shown as `staged`, sessions cannot join it, and only the
  migration's own probe uses it.

Every other copy of the phase is derived from the set record and never decides: the team's own
status record `/var/lib/teamchat-status/<team>.json`, which points at its set, and root's
`migration/state.json`. Section 5.3 says how a disagreement is repaired.

`ccy --teams <seat>@<team>[,...]` resolves each team's live system. The decision is made once, at
launch:

- If every team is on one system, the session gets that system's kit, plugin, settings and
  membership mount, and only that system's.
- If the teams are on both systems, the launch is refused (exit 64) with the list of teams by
  system and the two choices: run two sessions, or migrate the remaining v1 team. This is the
  default of **OD-D2**.

Host `cc --teams` resolves systems the same way. Until B defines the v2 host-member launch, `cc --teams` refuses
a v2-live team (exit 64) and names the gap. M0 (5.4) then refuses to migrate a team that has a
host `cc` member, so no host session is stranded.

The reason for the refusal: two kits in one session would share one status line and one
`crossSessionInbound` key, and two watchers would write to one inbox socket. That socket drops two
messages less than 2 s apart (RP 4), so each system's pacing would cause the other's drops.

## 4. Proof that v2 touches nothing v1 uses

Three layers prove D2 and D3. Each catches what the one before it cannot.

1. **Static gate.** Catches a v1 name written into v2 code or plays at commit time.
2. **Runtime confinement.** v2's services cannot reach v1's paths even if the code is wrong.
3. **Footprint diff in the VM lab.** Catches what the code does that nobody wrote down, against a
   live v1.

### 4.1 Static gate: `teamchat-keeps-off-v1`

This is a Go QA check, `src-go/qa/teamchatseparate`, wired into `qa-all.bash`, `qa-changed.bash`
and CI. A bash wrapper `scripts/qa-teamchat-separate.bash` execs it, inside the bash limits. The
check has three parts:

1. **v2 files hold no v1 name.**
   - Scope: every file that v2 owns. That is `src-go/cmd/teamchat*`, `src-go/internal/teamchat/`,
     `files/**/teamchat*`, `files/opt/claude-yolo/optional/teamchat/`, `playbooks/imports/play-teamchat*.yml`
     and the task files those plays include.
   - Test: no line matches the v1 name list `src-go/qa/teamchatseparate/v1-names.txt`. That list
     is `agent-bus`, `agent_bus`, `agentbus`, `pingbus`, `PINGBUS_`, `AGENT_BUS_`, `CCY_TEAMS_PINGBUS`,
     `CC_TEAMS_`, `/run/ccy/membership`, `$XDG_RUNTIME_DIR/ccy/membership`, `.claude/ccy/pingbus`,
     `/opt/claude-yolo/optional/agent-bus` and `Element-<team>` without the `teamchat-` part.
   - Deliberate v1 references have named homes, and nothing else may hold one:
     - `src-go/internal/teamchat/v1names`: the v1 constants v2 needs to stay away from v1 or to
       refuse it. These are the paths for the units' `InaccessiblePaths=`, the `agent_bus_teams`
       key the port check reads, and the v1 team-name check of 3.7. The unit templates are
       rendered from it, not written by hand.
     - `src-go/internal/teamchat/migrate/v1/`: the adapter (5.5) and its tests.
     - The allowlist `src-go/qa/teamchatseparate/allowed.tsv`: one line per remaining reference
       (`path`, the literal, the reason), for example the v2 play task that renders a unit from
       `v1names`. A line that no longer matches fails the gate, so the list cannot go stale.
     - Docs are not scanned.
   - Shared code that must name both systems is outside this part's scope: ccy-teams
     (`src-go/internal/teams/`), the ccy launcher and entrypoint, and `cc`. Their v1 behaviour is
     guarded by v1's existing suites (`scripts/test-ccy-agent-bus.bash`, `test-cc-teams.bash`,
     `test-ccy-teams-command.bash`), which must keep passing unchanged, and by 4.3 step 5.
2. **The v1 adapter writes nothing of v1's itself.** A `go/analysis` analyzer, catalogued in
   GoProjectRules.md with its red/green proof, checks `migrate/v1`:
   - it imports no file-writing call: `os.Create`, `os.WriteFile`, `os.OpenFile` with a write
     flag, `os.Remove*`, `os.Rename`, `os.Mkdir*`, `os.Chmod`, `os.Chown`, `os.Symlink`;
   - it reaches v1 only through the fixed, typed list in 5.5. The writes in that list are made by
     v1's own verbs, run with `exec.Command` and argument lists. The reads are read-only file opens
     and read-only HTTP to the v1 server.
3. **The single source holds no v1 prefix.** A unit test in `src-go/internal/teamchat/paths`
   checks every exported path and name against the same v1 list. It also checks that no v1 name is
   a prefix of a v2 name and no v2 name is a prefix of a v1 name. This guards against a careless
   glob or `rm -r <prefix>*`.

The red proof: a fixture under `testdata/` with one v1 path in a v2 play, and one `os.WriteFile`
in an adapter fixture. The gate must fail on each. The green proof: a v2 unit rendered from
`v1names` passes.

### 4.2 Runtime confinement

D requires the following of C's units and state layout. C owns the unit files.

- Every v2 system unit (server, health, backup) carries
  `InaccessiblePaths=-/var/lib/agent-bus -/var/lib/agent-bus-install -/var/lib/agent-bus-applied -/etc/agent-bus -/usr/local/lib/agent-bus -/usr/local/share/agent-bus`.
  The `-` prefix lets the unit start on a host without v1. A wrong path in v2 code then fails with
  `EACCES` instead of touching v1.
- v2 state directories are 0700 and owned by v2's users. v1's `agent-bus` user cannot read them,
  and v2 does not change v1's units to hide them.
- `teamchat-admin` runs as root, so it is not confined by the unit settings. Its v1 reach is
  bounded by 4.1 part 2, and its v2 writes by the paths package.

### 4.3 Footprint diff in the VM lab: scenario `teamchat-coexist`

The scenario runs v1 and v2 side by side and compares v1's footprint before and after. It is
written as a Go acceptance checker in the lab, which is exempt as a test.

1. **Set up v1.** Provision v1 with one team, one ccy seat and one LXC member, and run v1's own
   acceptance.
2. **Take manifest A of v1's footprint.**
   - the file tree under every v1 root in INV 0: path, type, mode, owner, size and sha256. Content
     that v1 itself changes (`db/`, `backups/`, `health/`, logs) is recorded by path and type
     only;
   - `systemctl list-units 'agent-bus*' --all` with each unit's state, and the unit files' hashes;
   - `/etc/sudoers.d/agent-bus-read-only`;
   - `getent passwd agent-bus`, `getent group agent-bus`;
   - `nmcli -t connection show agentbus0`;
   - `firewall-cmd --list-rich-rules` for every zone, keeping only the rules on v1's ports;
   - `ss -ltn` for v1's ports;
   - the v1 Element profile directories and launchers;
   - every checkout's `.claude/ccy/` tree except `.claude/ccy/teamchat/`, including
     `.claude/ccy/.gitignore`;
   - the v1 files in the ccy image: a listing with sha256 of `/opt/claude-yolo/optional/agent-bus/**`
     and of the entrypoint and launcher library files that hold v1's code paths. Image metadata is
     not compared, because adding the v2 kit rebuilds the image;
   - in each LXC guest: `~<user>/.config/pingbus/**`, `/usr/local/share/agent-bus/kit/**`,
     `/usr/local/bin/{pingbus,agent-bus-claude}`, the `agent-bus-claude-<user>.service` unit and
     its state, and the tmux socket `-L agent-bus-claude`.
3. **Install v2 with no team.** Check that it is inert (D3): no `teamchat*` unit is active, `ss`
   shows no new listener, and no file appears outside v2's roots, by a whole-filesystem find
   against a pre-install snapshot, excluding `/proc`, `/sys`, `/run/user` and the journal.
4. **Run the v2 team cycle.** Create a v2-native team, join a seat and an LXC member, exchange
   messages, take a backup, restore it, then remove and purge the team.
5. **Take manifest B and compare it with A.** Any difference fails the scenario. Then run v1's
   acceptance again. It must pass, and the v1 team must still exchange a ping round trip, both
   from a ccy session launched with `ccy --teams` and from a host `cc --teams` session.

The scenario runs whenever the v2 install or play changes, as one of the plan's VM lab gates. Its
manifest code is shared with `teamchat-admin footprint` and with the migration's fence step
(5.4 M2), which records the same footprint as evidence.

**Alternatives considered for the proof.**

- **Audit watches on v1's roots** (`auditctl -w <root> -p wa`) during a v2 install, failing on any
  write. This catches writes as they happen and names the process. But it misses a change made
  through a v1 tool that v2 calls, it misses reads, it needs `auditd` in the lab, and Ansible's
  writes all come from `python3`, so they cannot be told apart.
- **The static gate alone.** Cheap, but blind to anything a play or binary does without naming it.

The manifest diff is chosen as the dynamic layer because it judges the result, whatever caused it.
The static gate stays because it fails at commit time, long before the lab runs.

## 5. The per-team migration

### 5.1 What moves, and how

| Item                                       | What happens                                                                                                                                                                                                                                                                                                                | Why this way                                                                                                                      |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| Team name, description, repos, forge rules | The same team name on v2. M1 copies the v1 declaration into a new `teamchat_teams` entry, which the owner reviewed at M0. C's validator, and `play-teamchat.yml`, accept a team name that is also in `agent_bus_teams` only while that team's migration state (5.3) is at `preflighted` or later. Otherwise they refuse it. | Seat strings like `3@<team>` in the session registry keep working, and no plain v2 deploy can build the team outside a migration. |
| Agent accounts                             | Re-created on v2. Tokens cannot be copied between servers (TS 4). Each v1 handle maps to a v2 handle by A's grammar. If A keeps v1's `<repo>.<seat>+<host>.<type>`, the mapping is the identity. The mapping table is part of the migration state.                                                                          | A fresh account per server is the only route. The mapping keeps forge text, and humans' memory of who is who.                     |
| Roles                                      | Copied from v1's `registry.json` (`coordinator`, `worker`). At most one coordinator, as in v1.                                                                                                                                                                                                                              | RH 4.                                                                                                                             |
| Parked members                             | Created on v2 as parked: account, role and handle are kept, and no token is minted. A return on v2 mints one.                                                                                                                                                                                                               | Keeps the seat meaning, and mints nothing unused.                                                                                 |
| Seat counters, retired handles             | The counters are copied, so v2 never issues a number below v1's. Retired (burnt) v1 handles are recorded as retired on v2 and are never issued.                                                                                                                                                                             | A handle that once meant one member never means another (RH 7.09).                                                                |
| Humans' accounts                           | Created on v2 with the same name and the v2 power level A defines. The password is set after commit, through the existing ccy-teams login flow, and shown once. Humans in `removed_humans` are not created; their names stay burnt.                                                                                         | No password is ever stored (RH 1.2).                                                                                              |
| Humans' Element logins                     | A new profile `teamchat-<team>` and launcher `<team> team (teamchat)`. The v1 profile and launcher stay, as the archive viewer (5.6).                                                                                                                                                                                       | The v1 profile is bound to v1's server (TS 3).                                                                                    |
| Read markers, mutes, notification rules    | Not carried. The welcome notice (5.6) tells humans to re-mute the main room if they followed only the side rooms.                                                                                                                                                                                                           | Account data is per server and small to redo. Carrying it needs per-human writes into account data for little gain.               |
| Room history                               | Exported losslessly and kept, with v1 kept read-only as the archive (5.6). Imported into v2 only if OD-D4 is taken.                                                                                                                                                                                                         | TS 4: no import route is evidenced in Tuwunel.                                                                                    |
| Queued and in-flight items                 | Carried, settled or reported one by one, per 5.2.                                                                                                                                                                                                                                                                           | D9.                                                                                                                               |
| Bundles                                    | New v2 bundles, placed by v2's seat take (ccy) or v2's LXC apply, never into v1 paths.                                                                                                                                                                                                                                      | Bundles pin `base_url`, `user_id` and room (TS 2.1).                                                                              |
| Forge cache                                | Not carried. It is a cache with a TTL and refills on first use.                                                                                                                                                                                                                                                             | YAGNI.                                                                                                                            |
| Presence (`agent_bus.status`)              | Not carried. It is soft state, renewed every 300 s.                                                                                                                                                                                                                                                                         | TS 2.3 item 8.                                                                                                                    |

### 5.2 Queued and in-flight messages: what happens to each

v1 has no outbound send queue. Sends are synchronous and fail loudly (TS 2.3). The migration
fences v1 so that no new item can appear, then takes the **fence snapshot**: every member's v1
state as it is once nothing can change it (step M2). From the snapshot and the v1 server it builds
the **carry plan** (step M3), a JSON file in the migration state. Each item has exactly one fate:

- **carry**: the item is posted on v2 to its recipient as a carried item (5.2.1);
- **settle**: the sender gets a carried notice that says what happened;
- **report**: the item is listed by name in the migration report the owner reads.

**Why the snapshot is final.** After the fence, a v1 member cannot fetch or deliver anything. Its
token is revoked, so its watcher's next sync gets 401 and the watcher stops (exit 8, TS 7 row 3).
v1 prints an inbox item only after fetching the event again from the server (TS 2.3 item 2,
`helpers/pingbus/cli.py:1120-1133`), and that fetch also gets 401. So an inbox item in the
snapshot has not been delivered, and cannot be delivered by v1 later. M2 waits 60 s after the last
park, twice the 30 s long-poll, so that a sync already in flight lands before the snapshot is taken.

| v1 place (TS 2.3)                                                                                                  | How the migration finds it                                                                                                                                                                                                                                                                                                                                                                                                 | Fate                                                                                                                                                                                                                                                                                                                                                                                |
| ------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. **Sent, not yet synced**: on the v1 server, after the member's `next_batch`                                     | The cut point is `sync.json` from the snapshot. The admin pages `/messages` after it in each room, and applies v1's addressing rule (ported from PROTO; risk RK2): pings naming the member, human messages mentioning it or `@room`, and questions-room items for coordinators. A remote member's state cannot be read. Its cut point is its own last event in the v1 rooms (its last send), or its join if it never sent. | **carry**. A cut from a last send is marked `possible_duplicate`, because the member may have read past it.                                                                                                                                                                                                                                                                         |
| 2. **Synced, not delivered**: `state/inbox/<eid>.json`                                                             | From the snapshot (5.5 says how each kind is read). The event itself is fetched from the v1 server by id with the admin token.                                                                                                                                                                                                                                                                                             | **carry**.                                                                                                                                                                                                                                                                                                                                                                          |
| 3. **Consumed undelivered on purpose** (a seat removed while out, H23a)                                            | `consumed/` entries for a seat that is not in the team now.                                                                                                                                                                                                                                                                                                                                                                | **report** only. v1's own rule is that nothing posted while a member is out is delivered on its return. Carrying would change that rule.                                                                                                                                                                                                                                            |
| 4. **Delivered, unconfirmed**: written to the session's inbox socket, nonce not yet seen in the transcript         | The `inject/` records in the session home. For a running ccy container the home is `/tmp/pingbus-home` inside it, and ccy-teams lists the records as the desktop user with `podman exec <container> find /tmp/pingbus-home/inject -maxdepth 1 -type f -printf '%f\n'` (names only). For host and LXC members the home is on disk and is read with the snapshot. A ccy session that is not running at M2 has no home left.  | **carry** again, marked `possible_duplicate`, for each record found. For each ccy session not running at M2, the report says "unconfirmed deliveries cannot be listed for <session>". That is v1's existing gap (RP 4), named, not hidden.                                                                                                                                          |
| 5. **Ping awaiting an acknowledgement**: an `outbox.json` entry with targets not in `answered` or `reported`       | From the snapshot. Built **after** the carry set.                                                                                                                                                                                                                                                                                                                                                                          | **settle**, unless the carry set already holds the target's reply to that ping (an `ack`, `nack`, `done` or `blocked` that the sender had not yet synced): then the reply is what the sender receives, and no notice is added. Otherwise, past the deadline, the sender gets the `TIMEOUT` item v1 would have given. Before the deadline, the sender gets a `moved` notice (OD-D7). |
| 6. **Open questions** (`Q<n>` with no close in the questions room)                                                 | The v1 questions room's history (S211 8).                                                                                                                                                                                                                                                                                                                                                                                  | **carry** into the v2 questions room as an open question that keeps its number `Q<n>`. The v2 numbering starts above the highest v1 number (assumption on A, section 12).                                                                                                                                                                                                           |
| 7. **v1's own notices**: the SessionEnd goodbye, `seat farewell` and `seat disconnected` (PROTO table at line 946) | They are sent by v1's hooks and the ccy launcher, not by agents. After the fence each one fails with 401. The SessionEnd hook is bounded, and `seat disconnected` writes `disconnect.log`.                                                                                                                                                                                                                                 | **report**: the failures are expected, and the report lists them from `disconnect.log`. They are not carried: v2's SessionStart notice and A's presence tell the team the same thing. After commit, the ccy launcher skips v1's farewell and disconnected steps for a team whose set record is committed (a ccy change, section 12), so no failed step can hold up the M6 relaunch. |
| Human messages that mention nobody                                                                                 | Never delivered on v1 (TS 2.3 item 6).                                                                                                                                                                                                                                                                                                                                                                                     | **report** a count. They are in the export and the archive.                                                                                                                                                                                                                                                                                                                         |
| Free text sent by an agent to a human                                                                              | Already in the room, which the humans read.                                                                                                                                                                                                                                                                                                                                                                                | Nothing to carry. In the export and the archive.                                                                                                                                                                                                                                                                                                                                    |
| Any v1 event after `v1_last_event`                                                                                 | Checked in M3, and again at the start of M8.                                                                                                                                                                                                                                                                                                                                                                               | In M3 it stops the run and names the sender: something unfenced is still posting. In M8 (after commit) it is carried, through the same mechanism and with derived transaction ids, or reported if it has no recipient.                                                                                                                                                              |

#### 5.2.1 Carried items on v2

- A carried item is a v2 event sent by the v2 admin into the room where the original was (main,
  reports or questions), addressed to one member.
- It holds the original sender's handle (mapped), the original `origin_server_ts`, the kind (ping
  with verb, text, question, `TIMEOUT`, `moved`), the text, the v1 event id, and the
  `possible_duplicate` flag.
- It is posted with a transaction id derived as `sha256(<v1 event id> | <target> | <kind>)`, so a
  re-run of the carry step never posts a second copy (D7).
- What D needs from A and B is in section 12: A defines the content schema under
  `teamchat.carried` and its trust rule, so that members believe the item only as the admin's
  account of what an old sender said. B renders it with a carried-from-v1 header and delivers it on
  the member's first v2 start.

**Why carried items are posted to the server, not written into members' v2 inboxes.** Writing
straight into a member's on-disk inbox would be simpler for ccy seats. But it would bypass the
server as the source of truth, and v2's member fetches items again from the server before printing,
as v1 does (TS 2.2). It also cannot reach a stopped or remote member. An event on the server
reaches every member kind, survives a member's state being wiped, and is visible to humans as an
audit trail.

#### 5.2.2 The carry plan

The carry plan is written to `/var/lib/teamchat-admin/<team>/migration/carry-plan.json`, 0600:

```json
{
  "v": 1,
  "team": "<team>",
  "set": "<set-id>",
  "fence_ts": 1760000000000,
  "v1_last_event": {"main": "$<id>", "reports": "$<id>", "questions": "$<id>"},
  "members": [
    {
      "v1_handle": "<repo>.3+<host>.podman",
      "v2_handle": "<repo>.3+<host>.podman",
      "role": "worker",
      "state": "active",
      "state_read_from": "seat",
      "cut": {"kind": "next_batch", "value": "<token>"},
      "items": [
        {"fate": "carry", "kind": "ping", "verb": "review", "v1_event": "$<id>", "room": "main",
         "from": "<repo>.1+<host>.podman", "ts": 1759999990000, "possible_duplicate": false}
      ]
    }
  ],
  "settled": [
    {"fate": "settle", "kind": "moved", "sender": "<repo>.1+<host>.podman", "v1_event": "$<id>",
     "target": "<repo>.3+<host>.podman", "verb": "review", "deadline_ms": 1760000800000}
  ],
  "questions": [{"number": 7, "v1_event": "$<id>", "state": "open"}],
  "reported": [
    {"kind": "consumed_while_out", "member": "<handle>", "count": 2},
    {"kind": "unconfirmable_deliveries", "session": "<session>"},
    {"kind": "v1_notice_failed", "notice": "disconnected", "seat": "<seat>@<team>"},
    {"kind": "unaddressed_human_messages", "count": 4}
  ]
}
```

- `state_read_from` is one of `seat`, `lxc-rootfs`, `host-config` or `server-only`.
- `cut.kind` is `next_batch`, `last_send` or `join`.

The migration prints the plan as a human report (counts per member and per fate, and every
`reported` row) at M3, and keeps it with the migration state. The owner sees every item that is not
carried.

### 5.3 The state machine and what "commit" means

**Every migration is a set.** A team that shares no session with another v1 team is a set of one
(5.9). The set record `/var/lib/teamchat-status/sets/<set-id>.json` is root-written and readable by
the stamp group, so ccy-teams and the ccy launcher read it without sudo. It names the teams and
holds the set's phase. It is the one source of truth for which system the teams are live on (3.9).

The detail of each team's run lives in `/var/lib/teamchat-admin/<team>/migration/`, root 0700:

- `state.json` holds the team's phase;
- `log.jsonl` gets one line per step, start and end;
- `fence.json` is the fence record: every human locked and logged out, and every member parked
  with the `--repo`, `--seat`, `--host`, `--type`, `--role` and `--address` values its return
  needs;
- `snapshot/` is the fence snapshot of member state, copied read-only;
- `carry-plan.json` is the carry plan;
- `v1-export/` holds the export (5.6);
- `footprint-fence.json` and `footprint-archive.json` hold the v1 manifest of 4.3 at M2 and M8.

Every state file is written by temporary file, fsync, rename and directory fsync, as every v2
state file is. The team's status record `/var/lib/teamchat-status/<team>.json` carries a pointer
to its set and a copy of the phase for display.

```text
declared ──M0──▶ preflighted ──M1──▶ staged ──M2──▶ fenced ──M3──▶ captured ──M4──▶ carried
                                       │              │               │              │
                                       └──────────────┴───── R ───────┴──────────────┘
                                                              ▼
                                                         rolled-back
carried ──M5──▶ committed ──M6──▶ moved ──M7──▶ accepted ──M8──▶ archived
```

**Commit** is one atomic act: the rename of the set record from phase `carried` to phase
`committed`. That rename is the moment the set's teams change their live system from v1 to v2.
Its preconditions are re-checked for every team in the set immediately before it:

1. the v2 team passes acceptance part 1 (6.1);
2. the count of carried events on the v2 server equals the plan's count;
3. v1 is still fenced, and no v1 event exists after `v1_last_event`;
4. the export's digest is recorded.

**When the copies disagree.** A crash can leave the set record committed and a team's
`state.json` or status record behind. The set record is the truth, and the copies are repaired
from it:

- every step starts by reading the set record and advancing lagging copies to match;
- M6 refuses to restart any session until every team's status record agrees with the set record;
- a copy that is ahead of the set record (for example `committed` while the set says `carried`)
  is impossible by construction, so it stops the run with the difference named.

P1's fault injection kills the run between the set rename and the copies.

- **Before commit**, everything is undone by rollback **R** (5.7), and v1 is the live system.
- **After commit**, the migration only moves forward. A failure in M6 to M8 is fixed on v2 and the
  step is re-run. The v1 team stays fenced and is never live again. A reverse move is not provided
  (OD-D5).

**Alternatives considered for commit.**

- Flipping the declaration, by removing the `agent_bus_teams` entry, as the moment of commit. This
  edits a file through ccy-teams' line editor (with its backup), so it is not atomic with any check.
  The launcher would have to parse host_vars at every launch. And the v1 entry is still needed
  after commit, to drive v1's play for the LXC moves in M6.
- One rename per team, with no set record. Teams that share a session would then commit one by
  one, and the shared session would be mixed between systems in the gap.

The set record rename is chosen because it is one atomic filesystem act, and the launcher already
reads the status directory.

Every step is idempotent. A re-run reads the phases and resumes the current step. Each step's side
effects are guarded:

- creates are "exists with the same content" checks;
- posts use derived transaction ids;
- v1 fence and return verbs are skipped when `agent-bus list` already reports the state they set.

A step that finds the world different from what its phase implies stops with the difference
named, for example "phase fenced but member <handle> is not parked on v1". It never guesses.

### 5.4 The steps

Who runs a step:

- `ccy-teams migrate` orchestrates, as the desktop user.
- Root steps run as `sudo teamchat-admin migrate <step> --team <team>`, with the owner's sudo
  password, as v1 backups already need.
- v1 verbs run through the adapter's fixed list (5.5), as `sudo agent-bus <verb>` or as the user,
  exactly as v1 runs each one today.
- Plays run through `run.bash` single-play mode, under the user play lock, as ccy-teams does for
  team changes today (TS 6).

The owner confirms once, after M0 has printed the plan of record and before M2. There is no other
prompt, so the window in which agents have no bus stays as short as the steps allow (5.10).

| Step         | Does                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Fails fast when                                                                                                                                                                                                                                                                                                                                                                                    |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0 preflight | Reads only, then writes the migration state at `preflighted`. Lists the members: ccy seats from v1's registry and seat dirs; LXC members from `agent_bus_lxc_members` and v1's LXC record, running or stopped; remote members, meaning registry handles that are neither. Lists the team's sessions from the session registry, and computes the set (5.9). Checks that v2 software is installed at an accepted version, that the port is free (3.8), that the v1 team is healthy (fresh health record), and that the team name fits v2's grammar (3.5). Prints the plan of record: members, sessions, set, and every gap the report will name.                                                                                                                                                                                                                                                      | v1 is unhealthy; v2 is not installed; the set has more than one team and the owner has not chosen how to handle it (OD-D3); a remote member is not marked "moves by its own IaC" in `teamchat_migrations` (5.8); the team has a host `cc` member while v2 host-member launch is not available (3.9); the team name is too long for per-team server users; an LXC member's rootfs cannot be opened. |
| M1 stage     | Writes the `teamchat_teams` entry, and the team's LXC members into `teamchat_lxc_members` with v2 state `staged`. Runs `play-teamchat.yml` for this team, which accepts the shared team name because the state is at `preflighted`. The play installs the v2 server, rooms and admin, and runs `teamchat-admin migrate stage`, which creates every account, role, parked state, counter and retired handle from the M0 snapshot, and the human accounts with no password. Runs a v2 probe: a temporary account sends to itself and reads it back through the v2 member binary, then is removed. v1 is untouched.                                                                                                                                                                                                                                                                                    | Any v2 creation fails; the probe round trip fails.                                                                                                                                                                                                                                                                                                                                                 |
| M2 fence     | (a) `agent-bus-install backup-now --team <team>`, recorded as the last-resort rollback point. (b) Every human is locked, then logged out of every session (`agent-bus human lock`, then `human logout-all`), because a lock alone may leave live Element sessions able to post (`docs/agent-bus.md:836-841` pairs them for that reason). (c) Every active member is parked (`agent-bus park-member`). This resets the password with `logout_devices` (`helpers/agent_bus/admin.py:779-789`), so the token is dead on the server. (d) Wait 60 s (see 5.2). (e) Prove the fence: for every member whose bundle is readable, a `/account/whoami` call with its old token gets 401. (f) Take the fence snapshot and footprint-fence, and record `v1_last_event` per room. (g) Write `fence.json`. No notice is sent through v1 (see below). The owner is shown the sessions that are bus-less until M6. | The backup fails; a lock, logout or park fails; a whoami with an old token succeeds; a snapshot read fails (5.5).                                                                                                                                                                                                                                                                                  |
| M3 capture   | Checks that no v1 event exists after `v1_last_event`. Exports every room's full history (5.6). Builds the carry plan from the snapshot and the server, and prints the report.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | An event exists after `v1_last_event` (the run stops and names the sender: something unfenced is still posting); the export's event count disagrees with a second paging pass.                                                                                                                                                                                                                     |
| M4 carry     | Posts every carry and settle item on v2 with derived transaction ids, the open questions, and the welcome notice (5.6).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Any post fails after its retries; the count on the server differs from the plan.                                                                                                                                                                                                                                                                                                                   |
| M5 commit    | Re-checks the four preconditions for every team in the set (5.3), then renames the set record to `committed`, then advances each team's copies.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Any precondition fails. The set stays at `carried`, and the owner can re-run M5 or roll back.                                                                                                                                                                                                                                                                                                      |
| M6 move      | The humans' v2 passwords are set and shown once (ccy-teams login flow). The v2 Element profile is written (v2's Element play). Running ccy sessions restart onto the v2 kit (6.2). For LXC members: the team's `agent_bus_lxc_members` entries are set to `state: absent`, and its `teamchat_lxc_members` entries to `present`. Then v1's play runs for this team, which takes the team out of each running guest's v1 env and stops the v1 service. Then v2's play runs, which places the v2 kit, bundles and service. Stopped containers are deferred (6.3).                                                                                                                                                                                                                                                                                                                                      | A team's status record disagrees with the set record (repaired first, 5.3); a ccy session fails to come up on v2 within its bound; a running LXC guest's apply fails. Deferred containers are not failures.                                                                                                                                                                                        |
| M7 accept    | Acceptance part 2 (6.1).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | Any check fails. Fixed forward on v2, then re-run.                                                                                                                                                                                                                                                                                                                                                 |
| M8 archive   | Checks again for v1 events after `v1_last_event`, and carries or reports any it finds (5.2, last row). Makes the v1 rooms read-only (5.6), and unlocks the v1 humans so that they can read the archive. Takes footprint-archive. Removes each ccy seat's v1 directory with v1's `seat remove`, run as the user in each checkout. This deletes the seat's v1 `state/`, which is safe only now: the fence snapshot, the export and A8 already hold everything it had. ccy-teams then shows the team as `teamchat`, and the v1 team as `archive`. LXC guests' v1 files are left as v1's `absent` handling leaves them, until v1 retires (7.2).                                                                                                                                                                                                                                                         | A v1 verb fails.                                                                                                                                                                                                                                                                                                                                                                                   |

**Why no notice is sent through v1 before the fence.** A notice would have to come from a session
or from the v1 admin, and v1 members' reaction to an admin message is not specified. The obvious
alternative, a hot remove of the team from each session, would make the v1 keeper retire the seat.
`seat retire` deletes `sync.json` (TS 2.2), so unsynced items would be lost on rollback. Agents
learn of the move from the loud failure of their next v1 send, and from the v2 SessionStart notice
in M6.

**Why the fence parks members, instead of stopping sessions or the v1 server.** Three fences were
considered:

1. **Stop every session in the team.** This cannot reach an LXC member run by hand in tmux, a
   stopped container that is started later, or a remote member.
2. **Stop the v1 server.** This also stops the export and the event fetches that M3 needs, and
   restarting the server only for the migration would need a v1 config change.
3. **Park every member, and lock and log out every human.** This works on the server, so it covers
   every member kind, running or not. It uses v1's own verbs, and it is undone by v1's own return
   path (5.7). Running sessions keep working on local work and fail loudly on a bus send.

The third is chosen. Raising the room's power levels to make it read-only was rejected as a fence:
every running v1 member would read the room as untrusted and exit 10, which is permanent (TS 4). It
is used only in M8, when no v1 member is left.

### 5.5 The v1 adapter's fixed reach

`src-go/internal/teamchat/migrate/v1` is a library used by `teamchat-admin` (root) and by
`ccy-teams` (the desktop user). Each operation runs in the process named, and nothing else in v2
may reach v1.

**Reads**

| Operation                                        | Runs as | How                                                                                                                                                                                                                                                                                                           |
| ------------------------------------------------ | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `List(team)`, `Health(team)`                     | user    | v1's CLI, through the existing NOPASSWD rules and `agent-bus health`                                                                                                                                                                                                                                          |
| `ReadRegistry`, `ReadTeamFile`, `ReadRoomIDs`    | root    | v1's root-owned files, read-only                                                                                                                                                                                                                                                                              |
| `ReadSeatState(checkout, team, seat)`            | user    | A ccy seat's `state/` in the owner's checkout, read as the owning user, never as root. `openat2` with `RESOLVE_BENEATH \| RESOLVE_NO_SYMLINKS` from a directory fd of the seat dir.                                                                                                                           |
| `ReadGuestState(container, user, team)`          | root    | An LXC member's `~<user>/.config/pingbus/<team>/state/` from the container's rootfs, also when the container is stopped. `openat2` with `RESOLVE_IN_ROOT \| RESOLVE_NO_MAGICLINKS \| RESOLVE_NO_XDEV` from a directory fd of the rootfs, so an absolute symlink resolves inside the guest, never on the host. |
| `ReadSessionInject(container)`                   | user    | File names only, through `podman exec` (5.2 row 4)                                                                                                                                                                                                                                                            |
| `WhoAmI(base_url, token file)`                   | user    | Read-only `GET /_matrix/client/v3/account/whoami` against the v1 server                                                                                                                                                                                                                                       |
| `ExportRoom(team, room)`, `FetchEvent(team, id)` | root    | The v1 admin token, read from v1's secrets as root, used only for these GETs, never logged and never written anywhere                                                                                                                                                                                         |

Every read accepts regular files only, opened with `O_NONBLOCK` so that a FIFO or device cannot
hang it, and caps sizes at v1's own caps (SPEC-state S25-S35). It decodes with v1's strict key sets,
ported as read-only decoders, and never puts file content in an error message. A red test covers a
symlinked `inbox/` entry, a FIFO in place of `sync.json`, and an absolute symlink in a guest
rootfs.

**v1 verbs** (v1's own code makes every write)

| Operation                    | v1 command                                                                                                                                                                                                             | Runs as                           |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `BackupNow`                  | `agent-bus-install backup-now --team <team>`                                                                                                                                                                           | root                              |
| `LockHuman`, `UnlockHuman`   | `agent-bus human lock` / `unlock`                                                                                                                                                                                      | sudo                              |
| `LogoutHuman`                | `agent-bus human logout-all`                                                                                                                                                                                           | sudo                              |
| `Park`                       | `agent-bus park-member`                                                                                                                                                                                                | sudo                              |
| `ReturnSeat`                 | `agent-bus add-member <team> --repo --seat --host --type --role --address --out=<seat dir>`. v1's wrapper writes only `member.json`, `token` and `README`, as the sudo user (`files/usr/local/bin/agent-bus:8-15,35`). | sudo, from the user               |
| `RotateToken`                | `agent-bus rotate-token <team> <handle> --out=<seat dir>`                                                                                                                                                              | sudo, from the user               |
| `ApplyV1Play(team)`          | `play-agent-bus.yml` with `agent_bus_only_teams`, through `run.bash`                                                                                                                                                   | the user, as ccy-teams does today |
| `SeatRemove(checkout, seat)` | `agent-bus seat remove`, in the checkout                                                                                                                                                                               | user                              |
| `SetRoomsReadOnly(team)`     | a new v1 verb (below)                                                                                                                                                                                                  | sudo                              |

`SetRoomsReadOnly` is the one new v1 verb. It is added to v1's admin tool, which counts as a v1
change, and is tested in v1's suite. It is used only in M8. The alternative was the adapter writing
power levels with v1's admin token, which would give v2 code a write route into v1's server.

D17 therefore reads exactly: v2 code writes nothing of v1's itself. Every v1 write the migration
causes is made by one of the v1 commands above, or by v1's play, driven by a declaration change.

### 5.6 Room history: readable in Element, and kept

Four options were considered:

1. **Export only.** The history is kept as a file, which Element users cannot browse.
2. **Keep the v1 rooms read-only as an archive.** Element users browse them in the v1 profile,
   with every message, thread and reaction as they were. The cost is one resident v1 Tuwunel per
   moved team, until the owner stops it.
3. **Link.** A Matrix link cannot cross into a different server's account, so a link is only
   text. It is cheap, and only useful together with 2.
4. **Import into v2 rooms.** History would be native in the v2 room. This needs appservice
   timestamp massaging, which is UNVERIFIED on Tuwunel (TS 4). It also needs puppet accounts and
   an appservice registration, more privilege surface. And the import must land before any member
   joins, or members would receive it as new.

**Default: 1, 2 and 3 together. 4 only if OD-D4 is taken after a probe proves it.**

- **Export** (M3): `/var/lib/teamchat-admin/<team>/migration/v1-export/`

  - `events-<room>.jsonl` holds every event as the server returns it;
  - `history.md` is a readable rendering: time, sender, text, threads;
  - `digest.json` holds counts and the sha256 of each file.

  The export is root 0700 and is backed up by `teamchat-backup@<team>` with the team's state. It
  outlives v1's retirement. It is v1 Plan 00178's export, built as part of v2's migration.

- **Archive** (M8): the v1 main and side rooms get `events_default` raised to 100, by the new v1
  verb (5.5), on rooms that no v1 member reads any more. The humans are unlocked so that they can
  log in and read. Their v1 Element profile now shows the full old history, read-only.

- **Link** (M4): the v2 welcome notice is an admin message in the v2 main room, pinned. It says:

  - the team moved from agent-bus on <date>;
  - earlier history is in the Element window `<team> team (Element)`, read-only;
  - `history.md` is attached. It is uploaded to v2's media store as an `m.file`, and split into
    parts below the server's upload limit when larger;
  - N items were carried and are marked as carried.

### 5.7 Rollback (before commit)

R runs from any phase from `staged` to `carried`, by `ccy-teams migrate --rollback <team>`. It
rolls back the whole set. What it does depends on how far the run got.

**From `preflighted` or `staged`** (nothing was done to v1): discard the v2 side (step 5 below).

**From `fenced`, `captured` or `carried`**, in order:

1. **Return every member in `fence.json` that `agent-bus list` still shows as parked.**
   - A ccy seat: `ReturnSeat` writes the new bundle into the seat's existing directory, with the
     values recorded in `fence.json`. v1's `seat take` cannot do it, because it creates only seats
     that have no directory (`helpers/agent_bus/checkout.py:224-252`).
   - An LXC member: `ApplyV1Play(team)`. v1's LXC apply returns a parked member with a new bundle,
     because a member that is not active in the registry is not "current"
     (`helpers/agent_bus_lxc/members.py:34-37,635`). `agent_bus_lxc_members` is not changed before
     commit. A stopped container is deferred, as v1 already does, and is returned when it next runs.
   - A remote member: listed for the owner's IaC on that machine, with the `add-member` values.
2. **Prove each returned bundle.** `WhoAmI` with the bundle's token must succeed. A member that is
   active but whose bundle fails (a crash between minting and placing) gets `RotateToken` into its
   seat dir. That makes the return idempotent: a re-run of R mints only for members that are still
   parked or whose bundle does not work.
3. **Unlock every human in `fence.json`.** They log in again in their v1 Element window with their
   existing password, because M2 logged them out.
4. **Check that each member's v1 state is unchanged.** `sync.json`, `inbox/`, `consumed/` and
   `outbox.json` must be byte for byte what the fence snapshot holds. v1 delivers them as usual
   after the restart, so nothing is lost and nothing is duplicated. If a file differs, R stops and
   prints the carry plan as the list of items to tell members by hand. This is an alarm, not an
   expected path: the pilot tests that park and return leave `state/` untouched (P1, risk RK1).
5. **Discard the v2 side.** `teamchat-admin team remove --purge <team>` deletes the v2 team and its
   carried events. The `teamchat_teams` and `teamchat_lxc_members` entries are removed. The set
   record and the migration state are kept, with phase `rolled-back`, as the record.
6. **Restart the team's ccy sessions** with `ccy-teams restart <team>`. They resolve to v1, which is
   still the live system, and pick up their new bundle.

v1's backup from M2 is the last resort if v1's own state is damaged. Its restore is not atomic
(LESSON 13), so it is an owner decision at that moment, never an automatic step.

**Why rollback is safe for messages.** Before commit, the migration only reads v1 member state.
On v1's server it only parks, locks and logs out. The only v1 files written are the three bundle
files that v1's own `add-member --out` replaces in R. The fence stops new events. So the v1 world
after R is the v1 world at the fence, with new tokens, and humans who must log in again.

### 5.8 Members the host cannot reach

- **Stopped LXC containers.** Their v1 state is read from the rootfs (5.5). Their carried items wait
  on the v2 server, and the v2 apply defers them until they start (6.3). They are not a blocker.
- **Remote members on other machines.** They are not managed from this host (TS 5). M0 refuses
  until the owner marks each one in `teamchat_migrations` as "moves by its own IaC", through
  ccy-teams, which then states the v2 bundle the remote member needs. The fence parks the member,
  so its v1 sends fail loudly. Its items are carried server-side with the `last_send` or `join` cut.

### 5.9 Sets of teams

When a session is seated in more than one v1 team, those teams form one set (D19). M0 computes the
set from the session registry, and the default (OD-D3) is to move it together:

- M0 to M4 run per team, all under one set record.
- M5 checks every team's preconditions, then renames the set record once. That one rename commits
  the whole set.
- Rollback R applies to the whole set.

The alternative is for the shared sessions to leave the other teams first, with
`ccy-teams session remove`, so that the set shrinks to one team. ccy-teams offers this, and the
owner chooses it at M0. Leaving a team through hot control is v1's normal leave, done while v1 is
live, with v1's own handling of the items pending at that moment (TS 2.3 item 3). ccy-teams shows
the owner each seat's pending count before the leave, so that nothing goes unseen.

### 5.10 How the owner runs it

- **The one entry point** is `ccy-teams migrate <team>`, on the host, run by the owner. It runs
  M0, prints the plan of record, asks for the one confirmation, and then runs M1 to M8 without
  stopping unless a step fails. `ccy-teams migrate --rollback <team>` and
  `ccy-teams migrate --status <team>` complete the set of commands.
- **Declaring a move** is separate from running it. When the owner picks "move to teamchat" on a
  team in the board, ccy-teams writes an entry into `teamchat_migrations`:
  `{team, remote_members: [{handle, moves_by_own_iac: true}]}`. This list is D's, not C's, so a
  plain v2 deploy never reads it and cannot build the team by mistake (5.1, first row).
- **Through meta-deploy.** A team name is install-specific, so it can never appear in the tracked
  `meta-deploy.list`. The plan queues one generic plan script, `migrate-declared.bash`, which runs
  `ccy-teams migrate --declared`. That command migrates each set declared in `teamchat_migrations`
  and not yet `accepted`, one set at a time, with the same confirmation. So the owner either runs a
  move at once from the board, or declares it there and lets the queued entry run it. Both take
  the same code path.
- **v2 software** reaches the host through the plan's own `deploy.bash`, which runs
  `play-teamchat.yml`. With no team declared, the install is inert. The pilots (7.1) are plan
  scripts on the same queue.

## 6. Moving running sessions, and acceptance

### 6.1 Acceptance

**Part 1 runs before commit (M5's precondition).** It reads v2 only.

| Check                 | Passes when                                                                                                                                                                                                    |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A1 server healthy     | The v2 health record is fresh and the server answers on its port from the bus address.                                                                                                                         |
| A2 membership carried | Every v1 handle in the M0 snapshot has its mapped v2 account, with the same role and parked state. Counters are at or above v1's. Every retired v1 handle is retired on v2. Every listed human has an account. |
| A3 items carried      | The v2 rooms hold exactly the plan's carry and settle items, matched by transaction id, plus the welcome notice.                                                                                               |
| A4 round trip         | A temporary probe account sends a ping to itself and reads it back through the v2 member binary. The account is removed afterwards.                                                                            |

**Part 2 runs after the move (M7).** It proves that the moved team works.

| Check                      | Passes when                                                                                                                                                                                                                                                                   |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A5 sessions on v2          | Every ccy session that was in the team before M2, and is running, has restarted on the v2 kit. Its member's presence on v2 is fresh, and the session's process tree shows no v1 kit process.                                                                                  |
| A6 carried items delivered | Every carried item addressed to a running member is delivered by B's proof (the transcript nonce), or still pending with B's stated reason. Items for stopped or remote members are pending on the server, and the report lists them by member.                               |
| A7 a real exchange         | ccy-teams asks the team's coordinator (the first active worker if there is none) for one acknowledged ping through B's check verb. The acknowledgement must arrive within the verb's deadline. This is the one check that disturbs a live agent, and it costs one short turn. |
| A8 v1 fenced and unchanged | No v1 event exists after `v1_last_event`. A fresh footprint differs from footprint-fence only by the v1 changes M6 lists: the removed LXC env lines and v1 services, and the `state: absent` LXC entries.                                                                     |
| A9 the humans' view        | The owner opens the v2 Element window and sees the welcome notice, and opens the v1 window and sees the old history. This is a look at the screen, asked for separately.                                                                                                      |

The result is written as `migration.accepted` in `/var/lib/teamchat-status/<team>.json`, with the
report path. ccy-teams shows the team as moved only after that.

### 6.2 ccy sessions

M6 uses the restart path ccy-teams already has (`ccy-teams restart`, RL 3.5): a relaunch request
that the ccy supervisor acts on at the session's next idle moment, with `--resume` of the recorded
conversation, never `--continue`. This is not a new mechanism.

What changes is what the relaunch resolves to:

- The session registry still holds `--teams 3@<team>`. Because the team is now v2-live (3.9), the
  launcher takes the v2 seat `3` (the same number, D11), mounts v2's membership directory, and
  loads only the v2 plugin and settings.
- The v2 SessionStart hook tells the agent that the team moved to teamchat, that its v1 sends will
  fail, and how many carried items are waiting. B owns the wording.
- When the old container exits, the launcher would normally run v1's `seat disconnected` for its
  v1 seat. For a team whose set record is committed, the launcher skips v1's farewell and
  disconnected steps, because the team is no longer on v1 and both would fail with 401 (5.2 row 7).
  The v1 SessionEnd goodbye inside the old container also fails with 401. It is bounded by its
  hook timeout, and P1 proves that it does not hold up the exit.

Two other ways to move a session were rejected:

- **Hot add of the v2 team to the running session.** The plugin, hooks and `crossSessionInbound`
  are fixed at launch (TS 5), so a running session cannot load the v2 kit.
- **Shut down and start a fresh session.** This loses the agent's conversation. The restart with
  `--resume` keeps it.

The bounds are ccy-teams' existing ones (300 s for the idle moment, 180 s to come up). A session
that misses a bound is reported and is not killed. A stopped session record moves when it is next
started, and its carried items wait on the server. Host `cc --teams` sessions are not restarted by
ccy-teams (RL 3.5). The report lists them, and the owner relaunches each one.

### 6.3 LXC members

An LXC member moves by declaration, as every LXC change does (RL 1):

1. M6 sets the team's `agent_bus_lxc_members` entries to `state: absent` and makes its
   `teamchat_lxc_members` entries `present`. The v1 entries are deleted only when v1 retires.
2. v1's play then runs for this team. It removes the team from each running guest's
   `~/.config/pingbus/env` and stops `agent-bus-claude-<user>.service` if this was its only team.
3. v2's play runs next. It places the v2 kit, the bundle and the `teamchat-session-<user>.service`.

A stopped container is deferred by both plays, as v1 already does (RL 1.5, LESSON 6). The next run
of both plays with the container up completes the move. ccy-teams shows the member as `deferred`
until then, and its carried items wait on the server. A guest whose session the human runs by hand
in tmux is listed in the report with its restart command, `teamchat-claude`.

The stopped-container path was not yet proven live on the host for v1 (LESSON 6). The pilot P4
includes a stopped LXC member for that reason.

## 7. Pilot, then retirement

### 7.1 Pilot

Each step must pass before the next is run.

| Step                     | Where  | What it proves                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| ------------------------ | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1 lab: coexist          | VM lab | Scenario `teamchat-coexist` (4.3).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| P1 lab: migrate          | VM lab | Scenario `teamchat-migrate`. A v1 team is seeded with one item of every kind in the 5.2 table: an unsynced ping, an inbox item, an open ping before and after its deadline, an open question, an unaddressed human message, a consumed-while-out item, a stopped LXC member and a member whose state cannot be read. It also seeds a target's reply that the sender had not yet synced (5.2 row 5), a running ccy session with an unconfirmed delivery, and a symlinked `inbox/` entry and a FIFO in a guest. It then asserts each item's fate, A1 to A8, and no duplicate on a re-run. It probes what `human lock` alone does to a live Element token, and records the answer. It proves that a failed v1 goodbye does not hold up the M6 relaunch. Fault injection kills the run at every step, and also between the set record's rename and its copies. Each re-run must resume and finish with the same result. |
| P1 lab: rollback         | VM lab | Scenario `teamchat-migrate-rollback`. Rolls back from each of `staged`, `fenced`, `captured` and `carried`. Asserts that each member's v1 `state/` is byte-identical to the fence snapshot. After the restart, every seeded item must be delivered by v1 exactly once, and every member's next v1 send must succeed: a ccy seat through `ReturnSeat`, and an LXC member through v1's play. A second run of R must mint nothing. This settles risk RK1.                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| P2 host: install         | host   | v2 software through the plan's `deploy.bash` (meta-deploy). The plan's `triage.bash` runs `teamchat-admin footprint` before and after, which must show v1's footprint unchanged and v2 inert (D3).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| P3 host: v2-native pilot | host   | A throwaway v2 team named `teamchat-pilot` (a generic name defined by this design, not by an install), with one ccy session and one LXC member. It runs real work for at least one working session, its resident cost is measured (section 8), and then it is removed with `--purge`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| P4 host: migration pilot | host   | A throwaway v1 team named `migration-pilot`, made with v1, with two ccy sessions, one LXC member stopped during the run, a few messages, an open ping and an open question. It is migrated, rolled back, migrated again, accepted and archived; then both its v1 and v2 sides are removed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| P5 first real team       | host   | The owner picks the team, the smallest one by default. After that, teams move one at a time, at the owner's pace.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |

### 7.2 Retiring v1

When ccy-teams finds no v1-live team (every v1 team is `archived` or was never live), it shows
"every team has moved; v1 can be retired" and the plan raises it with the owner. That is the moment
the frame names. Nothing is retired before it.

The retirement is its own plan, with these steps, in order:

1. Stop and disable each archived team's v1 server and timers. The v1 data stays on disk.
2. Run v1's play with every team `absent`, and with `purge` as the owner chooses per team. The
   exports in `/var/lib/teamchat-admin/<team>/migration/v1-export/` are not v1's and stay.
3. Remove v1 software and its host objects: `/usr/local/{bin,sbin,lib,share}/agent-bus*`,
   `/etc/sudoers.d/agent-bus-read-only`, the `agentbus0` connection, the `agent-bus` user, and
   v1's state roots (once the purges are done). This is done by a v1 `remove --software` verb, or
   by the retirement play.
4. Remove v1's Element profiles and launchers, if the owner chooses. They hold nothing that is not
   in the export.
5. Remove the v1 kit from the ccy image (a CCY version bump), and the v1 code, plays, docs and
   tests from the repo.
6. The v2 declaration check then refuses any `agent_bus_*` key. The static gate's v1 name list
   stays, so the names cannot come back. The `migrate/v1` adapter is deleted with v1, and the
   gate's exemption with it.

## 8. Cost of coexistence

| When                                      | Per team                                                                                                                  | Per member                                                              |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| v2 installed, no team                     | Nothing resident. On the host: one dummy interface, the binaries and one sudoers file.                                    | Nothing. The v2 kit is in the ccy image and inert without a membership. |
| During a migration (M1 to M8)             | Two homeservers (v1 and v2), two ports, and both systems' health and backup timers.                                       | One session restart per ccy session. No session ever runs both kits.    |
| After the move, while the v1 archive runs | One extra resident Tuwunel (the read-only v1 archive) and its two timers, until the owner stops it (OD-D6) or v1 retires. | Nothing.                                                                |
| After v1 retires                          | v2 only.                                                                                                                  | v2 only.                                                                |

v1 has no measured memory figure for an idle Tuwunel team server in the repo. P3 measures v2's,
and the same reading on a v1 archive server gives the cost of OD-D6's default. Both are recorded in
the plan's triage output. The ccy image carries both kits until v1 retires. The v2 kit is one
static binary plus the plugin files.

## 9. Decisions for the owner

Each decision has a default, and the work proceeds on it unless the owner says otherwise.

| ID    | Decision                                             | Options                                                                                                                                                                                                             | Default and why                                                                                                                                                                                                       |
| ----- | ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OD-D1 | The product name                                     | `teamchat`; `agentroom`; `teampost`                                                                                                                                                                                 | `teamchat`: it says what it does for both agents and humans, and it works as a prefix for every name (2.1).                                                                                                           |
| OD-D2 | A session asked to join teams on both systems        | (a) refuse, with the two choices; (b) allow both kits in one session                                                                                                                                                | (a): two kits would share one status line, one `crossSessionInbound` key and one inbox socket whose 2 s pacing drops the other system's messages (3.9).                                                               |
| OD-D3 | Teams linked by a shared session                     | (a) move the linked set together under one commit; (b) refuse until the shared sessions leave the other teams                                                                                                       | (a), with (b) offered in M0: (a) keeps every session working throughout; (b) is available when the owner wants smaller steps.                                                                                         |
| OD-D4 | Import v1 history into the v2 rooms                  | (a) no import: export plus read-only v1 archive plus link; (b) import through appservice timestamp massaging, if a probe proves Tuwunel supports it                                                                 | (a): import is unverified on Tuwunel, needs puppet accounts and an appservice registration (more privilege), and must land before members join. Revisit only if a probe proves it and the owner wants native history. |
| OD-D5 | A reverse move after commit                          | (a) none: fix forward on v2; (b) a v2-to-v1 migration                                                                                                                                                               | (a): before commit, rollback covers every failure. After commit, v1 is a frozen prototype, and building the reverse path doubles the work for a case the pilot is there to prevent.                                   |
| OD-D6 | How long a moved team's v1 archive server runs       | (a) running read-only until v1 retires; (b) stopped at M8, started by ccy-teams when a human wants to browse; (c) stopped at M8 for good, export only                                                               | (a): it answers "can Element users still see old history?" with yes, at the cost of one resident server per moved team (section 8). (b) is the choice if that cost matters once measured.                             |
| OD-D7 | An open ping awaiting an acknowledgement at the move | (a) settle: a `moved` notice to the sender, the ping carried to the target, and the answer delivered to the sender as a reply; (b) carry the deadline into the sender's v2 outbox so that v2 raises `TIMEOUT` later | (a): it needs no cross-system acknowledgement plumbing in B's outbox, and the sender is told plainly. (b) would make v2's outbox know about v1 event ids.                                                             |

## 10. Risks

| ID   | Risk                                                                                                                                                                                                                                | Mitigation                                                                                                                                                                                           |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| RK1  | v1's `park-member`, or the return path (`add-member --seat`, v1's LXC apply), may change a member's `state/`, for example by deleting `sync.json`. Rollback would then lose unsynced items, because v1's first sync uses `limit 0`. | P1's rollback scenario proves it before any host run. R's step 4 compares the state byte for byte with the fence snapshot and stops with the carry plan as the list to tell members, never silently. |
| RK2  | The migration's port of v1's addressing rule differs from v1's own, so items are over-carried or under-carried.                                                                                                                     | The rule is ported as a decoder with a parity oracle against v1's Python, per src-go's parity convention. Over-carrying is preferred, and the item is flagged `possible_duplicate`.                  |
| RK3  | A large `history.md` exceeds the v2 upload limit.                                                                                                                                                                                   | It is split into numbered parts below the limit. The export on disk is whole.                                                                                                                        |
| RK4  | Agents are bus-less from M2 until their restart in M6.                                                                                                                                                                              | M2 to M6 run without stopping after one confirmation. Every v1 send fails loudly, and the owner is shown the bus-less sessions.                                                                      |
| RK5  | Humans confuse the two Element windows.                                                                                                                                                                                             | Distinct launcher names (`(teamchat)` and the v1 `(Element)`), the v1 rooms read-only, and the pinned welcome notice.                                                                                |
| RK6  | The adapter reads v1's admin token.                                                                                                                                                                                                 | Root only, read-only, never logged, used only for the export's paging and event fetches (5.5), within 4.1 part 2's analyzer bounds.                                                                  |
| RK7  | A remote member never rejoins on v2.                                                                                                                                                                                                | Its items wait on the server, and ccy-teams lists it as pending in the board until it joins or is removed.                                                                                           |
| RK8  | The footprint diff flags v1's own changes (a backup, a health record).                                                                                                                                                              | Paths that v1 changes on its own are recorded by path and type only (4.3). A difference there that is not type or presence is a real finding.                                                        |
| RK9  | A failed v1 goodbye or `seat disconnected` holds up the old container's exit, and so the M6 relaunch.                                                                                                                               | The launcher skips v1's farewell and disconnected steps for a committed team (6.2), and P1 proves that the bounded SessionEnd goodbye does not hold up the exit.                                     |
| RK10 | Logging humans out at the fence costs them a fresh login on v1 after a rollback, and `human lock` semantics on Tuwunel are unproven.                                                                                                | Logout is what makes the fence certain whatever lock does. P1 records what lock alone does, so the cost can be revisited with evidence.                                                              |

## 11. What D drops from v1, and why

- **Every v1 name.** Coexistence needs disjoint names (D2). v1's names were also not a family;
  they mixed `agent-bus`, `pingbus` and `CCY_TEAMS_PINGBUS`. v2 uses one prefix, so the static
  gate is one list.
- **Replacing the kit in place under running sessions** (LESSON 10). It is replaced by versioned
  kit directories, keeping the two newest.
- **Binding on v1's `agentbus0` address and reusing v1's homeserver binary copy.** Each would tie
  v2 to an object that v1's play owns and that v1's retirement removes.
- **An Element profile named by the bare team name.** v2's profile is `teamchat-<team>`, so the
  v1 archive profile and the v2 profile coexist for the same team.
- **The rule "never reuse a team name"** (LESSON 14) is kept within a system and dropped across
  systems: unit, timer and record names differ, and keeping the name keeps every `<seat>@<team>`
  working.
- **Not carried by the migration** (5.1): presence, the forge cache, read markers and mutes, and
  items consumed while a member was out. That last one is v1's own rule, kept.

## 12. Assumptions on other areas

**A (protocol and rooms)**

- Custom event types and content keys use the `teamchat.` namespace.
- A carried-item kind `teamchat.carried` exists. Only the admin may send it, and members believe it
  as the admin's statement of an old sender's message. It holds the fields of 5.2.1.
- The admin can post a pinned notice in the main room.
- The questions room's numbering can be seeded above v1's highest `Q<n>`, for example by admin
  state or by counting carried questions.
- The handle grammar is stated, so that the v1-to-v2 mapping is defined. The mapping is the
  identity if v1's grammar is kept.
- A's addressing rule (who receives which message) is stated, and the migration ports v1's for
  the carry plan.

**B (member client)**

- B renders a carried item with a header saying it was carried and when the original was sent,
  shows `possible_duplicate`, and delivers it on the member's first start like any pending item.
- An answer to a carried ping is delivered to the original sender as a reply.
- B uses the names and roots of 3.3, 3.4 and 3.6, the `TEAMCHAT_` prefix, and versioned kits.
- B's SessionStart hook says when a team moved.
- B has a check verb with an acknowledgement, for A7.

**C (host side)**

- `teamchat-admin` has these migration verbs: `migrate stage|carry|commit|archive`, `footprint`,
  and `team remove --purge`.
- C chooses per-team or shared server users. 3.5 names both; if C chooses per-team users, the team
  name grammar shortens to 23 characters.
- C's port validator reads `agent_bus_teams` (3.8).
- v2 units carry the `InaccessiblePaths=` of 4.2.
- The status record carries `migration.phase` and `migration.accepted`.
- C's validator and `play-teamchat.yml` accept a team name that is also in `agent_bus_teams`
  only while that team's migration state is at `preflighted` or later (5.1). The migration intent
  itself lives in D's `teamchat_migrations`, which C does not read.
- C's status directory holds the set records `sets/<set-id>.json` (5.3), readable by the stamp
  group.
- Element profiles and launchers are named as in 3.7.

**ccy (shared)**

- The ccy launcher resolves each team's live system, carries both kits in the image until v1
  retires, refuses a session asking for both systems (OD-D2), and mounts only one system's
  membership.
- The launcher reads the set records without sudo, and skips v1's farewell and disconnected
  steps for a team whose set record is committed (6.2).
- `cc --teams` refuses a v2-live team until B defines the v2 host-member launch (3.9).
- These are changes to ccy, not to v1, and each one is a CCY version bump.
