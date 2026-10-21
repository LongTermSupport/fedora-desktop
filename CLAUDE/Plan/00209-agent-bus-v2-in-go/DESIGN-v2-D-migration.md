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

| ID  | Requirement                                                                                                                                                                                                                                                                                                                                                                          | Source                           |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------- |
| D1  | v2 has a real product name that says what it does to a reader new to it, with no version suffix.                                                                                                                                                                                                                                                                                     | owner via lead; qa-reviewer.md B |
| D2  | Every v2 binary, plugin, skill, unit, user, group, directory, file, env variable, port, interface, firewall record, sudoers file, Element profile, host_vars key, play, Go package and Matrix namespace is disjoint from v1's.                                                                                                                                                       | FRAME "installs beside v1"       |
| D3  | A v2 install with no teams is inert: no unit active, no port bound, no file written outside v2's own roots.                                                                                                                                                                                                                                                                          | FRAME                            |
| D4  | A test proves D2 and D3 mechanically, both statically (in QA) and dynamically (in the VM lab, against a live v1).                                                                                                                                                                                                                                                                    | FRAME; lead                      |
| D5  | A team is on exactly one system at any moment. A session is on exactly one system: it never loads both kits.                                                                                                                                                                                                                                                                         | FRAME "no team mixed"            |
| D6  | Teams move one at a time (or one linked set, D19). The owner starts the move from ccy-teams, and host runs go through meta-deploy.                                                                                                                                                                                                                                                   | FRAME; CLAUDE.md                 |
| D7  | The migration is fail-fast. Each step checks its precondition and stops with a named reason. Every step can be re-run: a re-run resumes from the recorded phase and never repeats a side effect.                                                                                                                                                                                     | CLAUDE.md fail fast; LESSON 13   |
| D8  | The v1 team stays intact and usable until the migration commits. Rollback before commit restores it, with every queued item still deliverable.                                                                                                                                                                                                                                       | FRAME                            |
| D9  | No silent message loss. Every queued and in-flight item is either carried to its recipient on v2, settled by a notice to its sender, or listed by name in the migration report. Nothing is dropped without an entry.                                                                                                                                                                 | FRAME                            |
| D10 | Room history stays readable by the team's humans in Element after the move, and a lossless export of it survives v1's retirement.                                                                                                                                                                                                                                                    | lead (3); FRAME                  |
| D11 | Seats, roles, parked members, seat counters and retired (burnt) handles carry over, so a seat number means the same member before and after.                                                                                                                                                                                                                                         | RH 4; DESIGN R-seat rows         |
| D12 | Humans get a v2 account with the same name and a new password through the existing ccy-teams login flow. Their v1 Element profile stays as the archive.                                                                                                                                                                                                                              | RH 1.2; Element digest           |
| D13 | Running ccy sessions move by a restart onto the v2 kit that resumes the same conversation. LXC members move through the v2 apply, and a stopped container is deferred, not a blocker.                                                                                                                                                                                                | RL 3.5, 6.01; LESSON 6           |
| D14 | An acceptance check proves a moved team works before the move is called done.                                                                                                                                                                                                                                                                                                        | FRAME                            |
| D15 | A pilot runs first: a throwaway v2 team, then a throwaway v1 team migrated and rolled back, before any real team.                                                                                                                                                                                                                                                                    | lead (4)                         |
| D16 | v1 is retired only after every team has moved, and the retirement is raised with the owner then.                                                                                                                                                                                                                                                                                     | FRAME                            |
| D17 | Least privilege: the migration reaches v1 only through one adapter package. v2 writes no v1 file and changes no v1 code: every v1 state change is made by a listed existing v1 command, by v1's play, or by a listed `systemctl` act on a v1 unit (5.5). The fence is v2's own nftables table. Reads are read-only and confined against symlinks. v2 services cannot see v1's paths. | DESIGN 6; R37-R39                |
| D18 | Every cost of running both systems side by side is stated: the processes, ports and units per team, during the migration window and after it.                                                                                                                                                                                                                                        | coordinator                      |
| D19 | Teams that share a session (a session seated in both) form a linked set. Because a session is on one system (D5), a linked set moves together, under one commit, or the shared sessions leave the other teams first.                                                                                                                                                                 | D5; FRAME "no team mixed"        |

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

`teamchat0` begins with `team`, like libteam's `teamd`, `teamdctl` and the `team0` devices of
NetworkManager's `team` connection type. It is accepted: the connection's type is `dummy`, so no
`nmcli` filter on the `team` type and no `teamd` config matches it, and only a bare `team*`
interface glob would. P2's `triage.bash` asserts that no Fedora package provides a `teamchat*`
command or unit (`dnf repoquery --whatprovides`) and that no `teamchat*` user or group exists before
v2 is installed (`getent`).

**The migration fence's objects** (5.4 M2, 5.5), present only while a team is fenced:

- the nftables table `inet teamchat_fence_<team>`, v2's own, loaded from
  `/var/lib/teamchat-admin/<team>/migration/fence.nft`;
- `teamchat-fence@<team>.service`, a oneshot system unit with no resident process, which loads the
  table at boot so that a reboot cannot lift the fence.

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

- **v2-live**: the team's status record `/var/lib/teamchat-status/<team>.json` says it was created
  on v2 (it has no set pointer), or the set record its pointer names,
  `/var/lib/teamchat-status/sets/<set-id>.json`, has its phase at `committed` or later.
  "Created on v2" is a positive field, `"origin": "v2"`, written only by `teamchat-admin team create`. A migrated team's record has `"origin": "migration"` and is never inferred to be v2-native
  from a missing field. After a rollback the pointer stays on the rolled-back set, so the team reads
  as v1-live.
- **v1-live**: every other team. The launcher takes v1's path for it as today, so it needs no
  host_vars. This includes a team whose migration has not committed: its v2 side is shown as
  `staged`, sessions cannot join it, and only the migration's own probe uses it.

The phase copies in the team's status record and in root's `migration/state.json` are derived from
the set record and never decide. Section 5.3 says how a disagreement is repaired. The decision is
one Go function shared by ccy-teams and the launcher. Its unit test covers every `origin` and every
set phase, `rolled-back` included.

**No v1 seat changes while a migration is open.** From `preflighted` to `carried`, the launcher and
ccy-teams refuse a new v1 seat, a hot add and a hot remove for the team, and name the migration.
These would run v1's `seat take` (which calls `sudo agent-bus add-member`,
`helpers/agent_bus/checkout.py:243-248`) or `seat retire` (which deletes `sync.json`). The first
reaches the server over loopback, which the fence leaves open, and both would change what M0 and
M2 recorded. A relaunch of an existing seat mints nothing and stays allowed.

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
     v1's own verbs and the listed `systemctl` acts, run with `exec.Command` and argument lists.
     The reads are read-only file opens and read-only HTTP to the v1 server.
   - The fence is not in the adapter. It lives in `src-go/internal/teamchat/migrate`, writes only
     v2 files (`fence.nft`, its own unit), and gets the v1 port from the adapter's
     `ReadTeamFile`. The fence unit's `Before=agent-bus-hs@<team>.service` is the one line
     `allowed.tsv` holds for it.
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
| Room history                               | Exported losslessly and kept, with the stopped v1 server kept as the archive that ccy-teams opens on demand (5.6). Imported into v2 only if OD-D4 is taken.                                                                                                                                                                 | TS 4: no import route is evidenced in Tuwunel.                                                                                    |
| Queued and in-flight items                 | Carried, settled or reported one by one, per 5.2.                                                                                                                                                                                                                                                                           | D9.                                                                                                                               |
| Bundles                                    | New v2 bundles, placed by v2's seat take (ccy) or v2's LXC apply, never into v1 paths.                                                                                                                                                                                                                                      | Bundles pin `base_url`, `user_id` and room (TS 2.1).                                                                              |
| Forge cache                                | Not carried. It is a cache with a TTL and refills on first use.                                                                                                                                                                                                                                                             | YAGNI.                                                                                                                            |
| Presence (`agent_bus.status`)              | Not carried. It is soft state, renewed every 300 s.                                                                                                                                                                                                                                                                         | TS 2.3 item 8.                                                                                                                    |

### 5.2 Queued and in-flight messages: what happens to each

v1 has no outbound send queue. Sends are synchronous and fail loudly (TS 2.3). The migration
fences the v1 server off the network, so that no member or human can reach it (step M2), then
takes the **fence snapshot**: every member's v1 state as it is once nothing can change it. From
the snapshot and the v1 server it builds the **carry plan** (step M3), a JSON file in the
migration state. Each item has exactly one fate:

- **carry**: the item is posted on v2 to its recipient as a carried item (5.2.1);
- **settle**: the sender gets a carried notice that says what happened;
- **report**: the item is listed by name in the migration report the owner reads.

**Why the snapshot is final.** After the fence, a v1 member cannot reach its server. Its watcher's
syncs fail as a transient fault and are retried with a backoff from the stored sync token, without
end and without changing its state (`helpers/pingbus/cli.py:1757-1805`). v1 delivers an inbox item
only after fetching it again from the server (`_refetch` and `_deliver`,
`helpers/pingbus/cli.py:1348-1417`), and that fetch also fails. So `sync.json`, `inbox/` and
`consumed/` in the snapshot cannot change while the fence holds. A fenced `send` changes nothing
either: it calls the server (`verify_room`, `cli.py:1036`) before it admits the send and writes the
outbox (`cli.py:1045-1050`).

One file can change: the outbox. Both delivery paths emit every `TIMEOUT` that is due from local
state: `recv`'s `_deliver` (`cli.py:1381`), and the watcher's injection path that running ccy
sessions use, `_candidates` and `_send_delivery` (`cli.py:1905-1922, 2027-2031`). Both put inbox
items before `TIMEOUT`s, and an item's re-fetch raises on the fenced server (only `NotFound` is
caught, `cli.py:1358-1361`). So a `TIMEOUT` is emitted during the fence only when that member's
inbox is empty, and then the sender's `outbox.json` gains `reported` entries. Row 5 below handles
that case, and R's state check (5.7) allows exactly that change. M2 waits 60 s after the fence,
twice the 30 s long-poll, so that a sync already in flight lands before the snapshot is taken.

| v1 place (TS 2.3)                                                                                                  | How the migration finds it                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Fate                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. **Sent, not yet synced**: on the v1 server, after the member's `next_batch`                                     | The cut point is `sync.json` from the snapshot. The admin pages `/messages` after it in each room, and applies v1's addressing rule (ported from PROTO; risk RK2): pings naming the member, human messages mentioning it or `@room`, and questions-room items for coordinators. A remote member's state cannot be read. Its cut point is its latest public `m.read` receipt, read by the admin over loopback: v1 posts one for each human message whose delivery the transcript confirms (`cli.py:2046-2071`, `matrix.py:301-304`), and delivers a member's items in `origin_server_ts` order (`cli.py:1905-1922`). So every item before it was delivered, consumed as held back, or lost to a missing delivery, as v1 already treats them. Agent items never move the receipt (`cli.py:2037-2039`), so agent items delivered after the last receipted human message lie after the cut and are carried again. A last send is not used: an agent often sends while items synced before it still wait for an idle moment. With no receipt, the cut is the member's join. | **carry**. Every item carried from a `read_receipt` or `join` cut is marked `possible_duplicate`, and the report gives the count per member before M4 posts.                                                                                                                                                                                                                                                                                                                                                                                             |
| 2. **Synced, not delivered**: `state/inbox/<eid>.json`                                                             | From the snapshot (5.5 says how each member kind is read). The event itself is fetched from the v1 server by id with the admin token, over loopback.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | **carry**.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 3. **Consumed undelivered on purpose** (a seat removed while out, H23a)                                            | `consumed/` entries for a seat that is not in the team now.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | **report** only. v1's own rule is that nothing posted while a member is out is delivered on its return. Carrying would change that rule.                                                                                                                                                                                                                                                                                                                                                                                                                 |
| 4. **Delivered, unconfirmed**: written to the session's inbox socket, nonce not yet seen in the transcript         | The `inject/` records in the session home. For a running ccy container the home is `/tmp/pingbus-home` inside it, and ccy-teams lists the records as the desktop user with `podman exec <container> find /tmp/pingbus-home/inject -maxdepth 1 -type f -printf '%f\n'` (names only). For LXC and host members the home is `PINGBUS_HOME`, by default `~<user>/.config/pingbus/`, and the records are in its `inject/` (`helpers/pingbus/config.py:123-138`, `inject.py:32`). They are listed, names only, with the snapshot (`ReadGuestInject`, 5.5). A ccy session that is not running at M2 has no home left.                                                                                                                                                                                                                                                                                                                                                                                                                                                         | **carry** again, marked `possible_duplicate`, for each record found. For each ccy session not running at M2, the report says "unconfirmed deliveries cannot be listed for <session>", and names the member's latest read receipt, before which every delivery is confirmed. That is v1's existing gap (RP 4), named, not hidden.                                                                                                                                                                                                                         |
| 5. **Ping awaiting an acknowledgement**: an `outbox.json` entry with targets not in `answered` or `reported`       | From the snapshot. Built **after** the carry set.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | **settle**, unless the carry set already holds the target's reply to that ping (an `ack`, `nack`, `done` or `blocked` that the sender had not yet synced): then the reply is what the sender receives, and no notice is added. Otherwise, past the deadline, the sender gets the `TIMEOUT` item v1 would have given. Before the deadline, the sender gets a `moved` notice (OD-D7). A settle item for a sender whose session was running at M2 is marked `possible_duplicate`, because v1 may print the same `TIMEOUT` locally before the session moves. |
| 6. **Open questions** (`Q<n>` with no close in the questions room)                                                 | The v1 questions room's history (S211 8).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | **carry** into the v2 questions room as an open question that keeps its number `Q<n>`. The v2 numbering starts above the highest v1 number (assumption on A, section 12).                                                                                                                                                                                                                                                                                                                                                                                |
| 7. **v1's own notices**: the SessionEnd goodbye, `seat farewell` and `seat disconnected` (PROTO table at line 946) | They are sent by v1's hooks and the ccy launcher, not by agents. While the fence holds, each one fails to reach the server. The SessionEnd hook is bounded. `seat disconnected` is bounded by `timeout`, writes its outcome to `disconnect.log`, and never holds up a restart (`files/var/local/claude-yolo/lib/agent-bus-membership.bash:87-111`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | **report**, in the M7 report, from each checkout's `disconnect.log` after the M6 restarts. They are not carried: v2's SessionStart notice and A's presence tell the team the same thing.                                                                                                                                                                                                                                                                                                                                                                 |
| Human messages that mention nobody                                                                                 | Never delivered on v1 (TS 2.3 item 6).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | **report** a count. They are in the export and the archive.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Free text sent by an agent to a human                                                                              | Already in the room, which the humans read.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | Nothing to carry. In the export and the archive.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| A v1 send whose request reached the server but whose response the fence cut                                        | The sender's events within `REQUEST_TIMEOUT_S` (15 s, `matrix.py:32`) before the fence, read from the server.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | **carry**, as any event before `v1_last_event`, marked `possible_duplicate`, because the sender saw the send fail and may send it again on v2. The report lists each one as "the sender may believe this failed".                                                                                                                                                                                                                                                                                                                                        |
| Human messages typed in the v1 Element window from M2 on                                                           | Element keeps them locally as unsent: the fence refuses them, or the logout ends the session. The server never has them.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | **report**: the M2 owner message and the v2 welcome notice tell humans to retype unsent v1 messages in the v2 window. The migration cannot read Element's local queue, and says so in the report.                                                                                                                                                                                                                                                                                                                                                        |
| Human posts in the archive after M8 (5.6)                                                                          | The archive check: v1 events after the archive marker, read over loopback.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | **carry** to v2 with the same mechanism and derived transaction ids, or **report** when the message mentions nobody.                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Any v1 event after `v1_last_event`                                                                                 | Checked in M3, and again in M8 before the fence is lifted.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | In M3 it stops the run and names the sender: something reached the server past the fence. In M8 (after commit) it is carried, through the same mechanism and with derived transaction ids, or reported if it has no recipient.                                                                                                                                                                                                                                                                                                                           |

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
      "session_running_at_fence": true,
      "cut": {"kind": "next_batch", "value": "<token>"},
      "items": [
        {"fate": "carry", "kind": "ping", "verb": "review", "v1_event": "$<id>", "room": "main",
         "from": "<repo>.1+<host>.podman", "ts": 1759999990000, "possible_duplicate": false}
      ]
    }
  ],
  "settled": [
    {"fate": "settle", "kind": "moved", "sender": "<repo>.1+<host>.podman", "v1_event": "$<id>",
     "target": "<repo>.3+<host>.podman", "verb": "review", "deadline_ms": 1760000800000,
     "possible_duplicate": true}
  ],
  "questions": [{"number": 7, "v1_event": "$<id>", "state": "open"}],
  "reported": [
    {"kind": "consumed_while_out", "member": "<handle>", "count": 2},
    {"kind": "unconfirmable_deliveries", "session": "<session>"},
    {"kind": "unaddressed_human_messages", "count": 4}
  ]
}
```

- `state_read_from` is one of `seat`, `lxc-rootfs`, `host-config` or `server-only`.
- `cut.kind` is `next_batch`, `read_receipt` or `join`.
- The `v1_notice_failed` rows (5.2 row 7) are added to the M7 report, not to this plan, because
  they happen during M6.

The migration prints the plan as a human report (counts per member and per fate, and every
`reported` row) at M3, and keeps it with the migration state. The owner sees every item that is not
carried.

### 5.3 The state machine and what "commit" means

**Every migration is a set.** A team that shares no session with another v1 team is a set of one
(5.9). The set record `/var/lib/teamchat-status/sets/<set-id>.json` is root-written and readable by
the stamp group, so ccy-teams and the ccy launcher read it without sudo. It names the teams and
holds the set's phase. Each team's status record `/var/lib/teamchat-status/<team>.json` carries
the id of its current set, and that pointer is the only way to find the deciding set record. A
rolled-back set keeps its record as history. A later migration of the same team writes a new set
id and moves the pointer.

All the steps of one set run under one lock, `flock` on `/var/lib/teamchat-admin/_sets/<set-id>.lock`.
Two `migrate` runs cannot interleave, and the commit's precondition checks and its rename happen
under that lock.

The detail of each team's run lives in `/var/lib/teamchat-admin/<team>/migration/`, root 0700:

- `state.json` holds the team's phase;
- `log.jsonl` gets one line per step, start and end, and one line per change M6 makes to v1;
- `fence.json` is the fence record (5.4 M2): written as an intent before M2's first act, and
  updated as each act completes;
- `snapshot/` is the fence snapshot of member state, copied read-only;
- `carry-plan.json` is the carry plan;
- `v1-export/` holds the export (5.6);
- `footprint-fence.json` and `footprint-archive.json` hold the v1 manifest of 4.3, taken at M2 and
  at M8.

Every state file, the set record included, is written by temporary file, fsync, rename over the
old file, and directory fsync.

```text
declared ──M0──▶ preflighted ──M1──▶ staged ──M2──▶ fenced ──M3──▶ captured ──M4──▶ carried
                     │                 │              │               │              │
                     └─────────────────┴──────────────┴───── R ───────┴──────────────┘
                                                              ▼
                                                         rolled-back
carried ──M5──▶ committed ──M6──▶ moved ──M7──▶ accepted ──M8──▶ archived
```

**Commit** is one atomic act. Under the set lock, a new set record with phase `committed` is
written to a temporary file and renamed over the old record, whose phase was `carried`. That rename
is the moment the set's teams change their live system from v1 to v2. Its preconditions are checked
for every team in the set immediately before it, under the same lock:

1. the v2 team passes acceptance part 1 (6.1);
2. the count of carried events on the v2 server equals the plan's count;
3. the v1 fence still holds, and no v1 event exists after `v1_last_event`;
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
- each v1 verb is skipped when v1 already reports the state it sets: `agent-bus list` for locks and
  parks, and `teamchat-admin migrate fence-status` for the fence;
- M2's backup is skipped when `fence.json` records it done, because each `backup-now` rotates out
  v1's oldest backup (Tuwunel keeps 7, `helpers/agent_bus/render.py:27,83`).

A step that finds the world different from what its phase implies stops with the difference
named, for example "phase fenced but the v1 fence is not in place". It never guesses.

### 5.4 The steps

Who runs a step:

- `ccy-teams migrate` orchestrates, as the desktop user.
- Root steps run as `sudo teamchat-admin migrate <step> --team <team>`, with the owner's sudo
  password, as v1 backups already need.
- v1 verbs run through the adapter's fixed list (5.5), as `sudo agent-bus <verb>`,
  `sudo agent-bus-install <verb>` or as the user, exactly as v1 runs each one today.
- Plays run through `run.bash` single-play mode, under the user play lock, as ccy-teams does for
  team changes today (TS 6). From M2 to M5 `ccy-teams migrate` holds the user play lock itself, so
  no other play runs in that window. A v1 play for the team would otherwise run v1's `bootstrap`,
  which rewrites room state (`files/usr/local/sbin/agent-bus-install:831`,
  `helpers/agent_bus/admin.py:584-591`), and M3 would stop on an event after `v1_last_event`.

The owner confirms once, after M0 has printed the plan of record and before M2. There is no other
prompt, so the window in which agents have no bus stays as short as the steps allow (5.10).

| Step         | Does                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | Fails fast when                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0 preflight | Reads only, then writes the set record and the migration state at `preflighted`. Lists the members: ccy seats from v1's registry and seat dirs; LXC members from `agent_bus_lxc_members` and v1's LXC record, running or stopped; remote members, meaning registry handles that are neither. Lists the team's sessions from the session registry, and computes the set (5.9). Reads the team's Element address from `agent_bus_element_teams`. Checks that v2 software is installed at an accepted version, that the port is free (3.8), that the v1 team is healthy (fresh health record), and that the team name fits v2's grammar (3.5). Prints the plan of record: members, sessions, set, and every gap the report will name.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | v1 is unhealthy; v2 is not installed; the set has more than one team and the owner has not chosen how to handle it (OD-D3); a remote member is not marked "moves by its own IaC" in `teamchat_migrations` (5.8); the team has a host `cc` member while v2 host-member launch is not available (3.9); the team name is too long for per-team server users; an LXC member's rootfs cannot be opened; a readable member bundle's `base_url` is a loopback address (v1 permits it, `helpers/agent_bus/admin.py:667-672`), because the fence cannot cut that member. |
| M1 stage     | Writes the `teamchat_teams` entry, and the team's LXC members into `teamchat_lxc_members` with v2 state `staged`. Runs `play-teamchat.yml` for this team, which accepts the shared team name because the state is at `preflighted`. The play installs the v2 server, rooms and admin, and runs `teamchat-admin migrate stage`, which creates every account, role, parked state, counter and retired handle from the M0 snapshot, and the human accounts with no password. Runs a v2 probe: a temporary account sends to itself and reads it back through the v2 member binary, then is removed. v1 is untouched.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Any v2 creation fails; the probe round trip fails.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| M2 fence     | (a) Write `fence.json` as an intent: the humans, whether they will be logged out, and the fence. (b) `agent-bus-install backup-now --team <team>`, recorded as the last-resort rollback point, unless `fence.json` records it done. (c) Every human is locked (`agent-bus human lock`), so that no new login is possible. If the team's Element address is loopback, every human is also logged out (`human logout-all`), because the network fence cannot stop a loopback client. The owner message says that unsent v1 Element messages must be retyped in the v2 window. (d) `teamchat-admin migrate fence --team <team>` (5.5): v2's own nftables table rejects every packet to the team's v1 port whose source is not loopback, and `teamchat-fence@<team>.service` is enabled so that a reboot reloads it. (e) Wait 60 s (see 5.2). (f) Prove the fence on every route a member uses: a TCP connect to the team port is refused from the host's v1 bus address, from inside one running ccy container (through `podman exec`, so over pasta), and from inside one running LXC guest (through `lxc-attach`, so over the bridge), each where the team has one running, while the admin's `whoami` over loopback answers. The table is compared rule for rule with the one `fence.nft` holds (`nft -j list table`). (g) Take the fence snapshot and footprint-fence, record `v1_last_event` per room, and record which sessions were running. Each act is marked done in `fence.json` as it completes. No notice is sent through v1 (see below). The owner is shown the sessions that are bus-less until M6. | The backup fails; a lock, logout or the fence fails; the fence proof fails on any route; a snapshot read fails (5.5).                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| M3 capture   | Checks `fence-status` at its start and end, so that a fence lost between the snapshot and the carry stops the run before M4. Checks that no v1 event exists after `v1_last_event`. Exports every room's full history (5.6). Builds the carry plan from the snapshot and the server, and prints the report.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | An event exists after `v1_last_event` (the run stops and names the sender); the export's event count disagrees with a second paging pass.                                                                                                                                                                                                                                                                                                                                                                                                                       |
| M4 carry     | Checks `fence-status` at its start and end. Posts every carry and settle item on v2 with derived transaction ids, the open questions, and the welcome notice (5.6).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Any post fails after its retries; the count on the server differs from the plan.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| M5 commit    | Under the set lock: checks the four preconditions for every team in the set (5.3), renames the new set record into place, then advances each team's copies.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Any precondition fails. The set stays at `carried`, and the owner can re-run M5 or roll back.                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| M6 move      | The humans' v2 passwords are set and shown once (ccy-teams login flow). The v2 Element profile is written (v2's Element play). Running ccy sessions restart onto the v2 kit (6.2). For LXC members: the team's `agent_bus_lxc_members` entries are set to `state: absent`, and its `teamchat_lxc_members` entries to `present`. Then v1's play runs for this team. Its absent handling parks the handle, removes the guest's v1 team directory, takes the team out of the guest's v1 env, and stops the v1 service if this was its only team (`helpers/agent_bus_lxc/members.py:643-645,1319-1320`). Then v2's play runs, which places the v2 kit, bundles and service. Stopped containers are deferred (6.3). Every v1 change is logged to `log.jsonl` as M6 makes it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | A team's status record disagrees with the set record (repaired first, 5.3); a ccy session fails to come up on v2 within its bound; a running LXC guest's apply fails. Deferred containers are not failures.                                                                                                                                                                                                                                                                                                                                                     |
| M7 accept    | Acceptance part 2 (6.1), and the M7 report, which adds the v1 notice failures (5.2 row 7).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | Any check fails. Fixed forward on v2, then re-run.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| M8 archive   | In this order, so that no agent can post once the fence is lifted: (1) checks for v1 events after `v1_last_event`, and carries or reports any it finds (5.2, last row); (2) parks every v1 member that `agent-bus list` still shows as active, and removes each ccy seat's v1 directory with v1's `seat remove`, run as the user in each checkout (it parks the handle through sudo before it deletes the directory, `helpers/agent_bus/checkout.py:996-1009`; parking an already parked handle is harmless). This deletes the seat's v1 `state/`, which is safe only now: the snapshot, the export and A8 already hold everything it had; (3) records the archive marker, the last v1 event per room (5.6); (4) under OD-D6's default, stops and disables the team's v1 server, health timer and backup timer (`systemctl disable --now`, root); under OD-D6 (a) it instead unlocks the humans and issues their passwords now, while the server runs; (5) removes the fence (`teamchat-admin migrate unfence`: deletes the table and disables `teamchat-fence@<team>.service`); (6) takes footprint-archive. Under the default the humans stay locked until the first `ccy-teams archive open`, because `agent-bus human unlock` and `password` are server calls (`helpers/agent_bus/admin.py:873-901`); a password is then shown when it is wanted, not months before. Each act is recorded in `log.jsonl` as it completes, and a re-run resumes after the last one done; act (1) is skipped once act (3) has recorded the marker. ccy-teams then shows the team as `teamchat`, and the v1 team as `archive`. | A v1 verb or a `systemctl` act fails.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |

**Why no notice is sent through v1 before the fence.** A notice would have to come from a session
or from the v1 admin, and v1 members' reaction to an admin message is not specified. The obvious
alternative, a hot remove of the team from each session, would make the v1 keeper retire the seat.
`seat retire` deletes `sync.json` (TS 2.2), so unsynced items would be lost on rollback. Agents
learn of the move from the loud failure of their next v1 send, and from the v2 SessionStart notice
in M6.

**Why the fence is a network fence.** Four fences were considered:

1. **Stop every session in the team.** This cannot reach an LXC member run by hand in tmux, a
   stopped container that is started later, or a remote member.
2. **Stop the v1 server.** This also stops the export and the event fetches that M3 needs.
3. **Park every member** (revoke its token). This covers every member kind, but undoing it means
   minting new tokens, and none of v1's return routes keeps the member's state. The wrapper refuses
   an `--out` directory that exists (`files/usr/local/bin/agent-bus:96-99`), so a ccy seat cannot be
   given a new bundle in place. v1's LXC apply removes the guest's whole team directory, `state/`
   included, before it places the new bundle (`helpers/agent_bus_lxc/members.py:285-292,640,1050`).
   A park also ends every watcher for good (exit 8 is final, `helpers/pingbus/cli.py:2118,2152-2155`).
   Rollback would then lose unsynced items and need every session restarted.
4. **Cut the v1 server off the network except loopback, and lock every human.** This covers every
   member kind, running or not, local or remote. It changes no token, no bundle and no member file.
   Watchers survive it, retrying from their stored sync token (`cli.py:1757-1805`), so lifting it
   resumes the team exactly where it stopped. Running sessions keep working on local work and fail
   loudly on a bus send. The migration's own admin calls use loopback and are not affected.

The fourth is chosen. Raising the room's power levels to make it read-only was also rejected as a
fence: every running v1 member would read the room as untrusted and exit 10, which is permanent
(TS 4).

**Where the network fence lives.** It must not change v1: the frame says running v1 teams keep
working untouched and nothing v1 uses is changed. Four places were weighed:

1. **A new v1 verb, `agent-bus-install fence`.** This changes v1 software that every running team
   uses, for the sake of one team's move. It would also grow `agent-bus-install`, a bash file on
   the ratchet allowlist (`scripts/bash-limits.allowlist`), which may shrink but never grow.
   Refused.
2. **A v2-written drop-in on `agent-bus-hs@<team>.service`** that resets `IPAddressAllow=` to
   loopback. It changes no v1 code, but it is a file in v1's unit directory. v1's own installer
   records that the running server takes a new sandbox only on a restart
   (`files/usr/local/sbin/agent-bus-install:816-821`), so it costs one v1 restart at fence and one
   at unfence. Watchers survive a restart (E25). `systemctl set-property` is ruled out: it writes
   `50-IPAddressAllow.conf`, which sorts before v1's `network.conf`, so the next daemon reload would
   lift the fence silently.
3. **v1's own firewalld rich rules.** Not viable: firewalld accepts `lo` before zone rules, ccy
   seats reach the bus address through pasta as host-local traffic, and `sync_firewall` owns v1's
   rules (`agent-bus-install:588-615`).
4. **A v2-owned nftables table**, `inet teamchat_fence_<team>`, with an input-hook chain that
   resets TCP to the team's v1 port unless the source is loopback. `fence.nft` holds exactly one
   rule per family:
   `meta nfproto ipv4 tcp dport <port> ip saddr != 127.0.0.0/8 reject with tcp reset` and
   `meta nfproto ipv6 tcp dport <port> ip6 saddr != ::1 reject with tcp reset`. A bare `reject`
   would send ICMP, which an established TCP socket treats as a soft error. It touches no v1
   file, unit or code. It takes effect at once, on established long-polls too, with no restart. It
   matches packets towards the port only, so a server response already on its way still lands;
   M2's 60 s wait covers that.
   An accept in another table does not undo a reject in this one, so firewalld's own `lo`
   accept does not open it. `reject` (a reset) makes a fenced v1 send fail at once instead of after
   the 15 s request timeout (`helpers/pingbus/matrix.py:32`), and v1 treats a reset as transient
   (`matrix.py:221-222`), so watchers survive it.

The fourth is chosen, with the second as the stated fallback if P1 disproves it. Stopping the v1
unit was weighed as well (fence option 2 above): it needs no human lockout at all, but M3's export
and event fetches and M8's checks need the server, and a restart loopback-only for them brings back
the loopback Element problem.

The fourth's own risks are named for P1 to settle: the table must survive `firewall-cmd --reload`,
Docker's iptables-nft rules and `lxc-docker-user-iptables-reconcile.bash`, and a reboot (through
`teamchat-fence@<team>.service`, a oneshot ordered `After=nftables.service firewalld.service` and
`Before=agent-bus-hs@<team>.service` and wanted by `multi-user.target`; a v2 unit that orders
itself before a v1 unit changes nothing of v1). An `nft flush ruleset` or an `nftables.service`
restart deletes every table, this one included. That cannot be prevented, so it is detected: M3
and M4 check `fence-status` at their start and end, and a lost fence stops the run before any
carry, with the owner told to re-run M2's fence act or roll back.

### 5.5 The v1 adapter's fixed reach

`src-go/internal/teamchat/migrate/v1` is a library used by `teamchat-admin` (root) and by
`ccy-teams` (the desktop user). Each operation runs in the process named, and nothing else in v2
may reach v1.

**Reads**

| Operation                                                             | Runs as | How                                                                                                                                                                                                                                                                                                                                                                   |
| --------------------------------------------------------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `List(team)`, `Health(team)`                                          | user    | v1's CLI, through the existing NOPASSWD rules and `agent-bus health`                                                                                                                                                                                                                                                                                                  |
| `ReadRegistry`, `ReadTeamFile`, `ReadRoomIDs`                         | root    | v1's root-owned files, read-only                                                                                                                                                                                                                                                                                                                                      |
| `ReadSeatState(checkout, team, seat)`                                 | user    | A ccy seat's `state/` in the owner's checkout, read as the owning user, never as root. `openat2` with `RESOLVE_BENEATH \| RESOLVE_NO_SYMLINKS` from a directory fd of the seat dir.                                                                                                                                                                                   |
| `ReadGuestState(container, user, team)`                               | root    | An LXC member's `~<user>/.config/pingbus/<team>/state/` from the container's rootfs, also when the container is stopped. `openat2` with `RESOLVE_IN_ROOT \| RESOLVE_NO_MAGICLINKS \| RESOLVE_NO_XDEV` from a directory fd of the rootfs, so an absolute symlink resolves inside the guest, never on the host.                                                         |
| `ReadSessionInject(container)`                                        | user    | File names only, through `podman exec` (5.2 row 4)                                                                                                                                                                                                                                                                                                                    |
| `ReadGuestInject(container, user)`                                    | root    | File names only in the LXC or host member's `PINGBUS_HOME/inject/` (5.2 row 4), with the same `openat2` confinement as `ReadGuestState`                                                                                                                                                                                                                               |
| `ReadReceipts(team, room)`                                            | root    | Each member's latest `m.read` receipt, read with the admin token over loopback as the `m.receipt` ephemeral events of the admin's own `/sync` of each room, which the admin has joined; no endpoint lists other users' receipts (5.2 row 1). P1 proves the admin can read other accounts' receipts on Tuwunel; if it cannot, every remote cut falls back to the join. |
| `ExportRoom(team, room)`, `FetchEvent(team, id)`, `AdminWhoAmI(team)` | root    | The v1 admin token, read from v1's secrets as root, used only for these GETs over loopback, never logged and never written anywhere                                                                                                                                                                                                                                   |

Every read accepts regular files only, opened with `O_NONBLOCK` so that a FIFO or device cannot
hang it, and caps sizes at v1's own caps (SPEC-state S25-S35). It decodes with v1's strict key sets,
ported as read-only decoders, and never puts file content in an error message. A red test covers a
symlinked `inbox/` entry, a FIFO in place of `sync.json`, and an absolute symlink in a guest
rootfs.

**v1 verbs** (v1's own code makes every write)

| Operation                                 | v1 command                                                                                                              | Runs as                           |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `BackupNow`                               | `agent-bus-install backup-now --team <team>`                                                                            | root                              |
| `LockHuman`, `UnlockHuman`                | `agent-bus human lock` / `unlock`                                                                                       | sudo                              |
| `LogoutHuman`                             | `agent-bus human logout-all` (it also replaces the password with a discarded one, `helpers/agent_bus/admin.py:892-896`) | sudo                              |
| `HumanPassword`                           | `agent-bus human password`, shown once through ccy-teams' login flow                                                    | sudo                              |
| `Park`                                    | `agent-bus park-member`                                                                                                 | sudo                              |
| `ApplyV1Play(team)`                       | `play-agent-bus.yml` with `agent_bus_only_teams`, through `run.bash`                                                    | the user, as ccy-teams does today |
| `SeatRemove(checkout, seat)`              | `agent-bus seat remove`, in the checkout                                                                                | user                              |
| `StopArchive(team)`, `StartArchive(team)` | `systemctl disable --now` / `enable --now` of `agent-bus-hs@<team>.service` and its health and backup timers (M8, 5.6)  | root                              |

No v1 verb is new, and no v1 file or code changes. The fence is not in this table: it is v2's own
nftables table and v2's own unit, made by `teamchat-admin migrate fence|unfence|fence-status` from
the `migrate` package, outside the adapter (4.1 part 2). It is an `nft` call on a v2 object, not a
file write into v1. If P1
disproves the table and the fallback drop-in (5.4) is used, it is the one named exception: a v2 file
`zz-teamchat-fence.conf` in `agent-bus-hs@<team>.service.d/`, written and deleted only by
`teamchat-admin`, listed in 4.1's allowlist and in A8's allowed difference.

D17 therefore reads exactly: v2 writes no v1 file and changes no v1 code. Every v1 state change the
migration causes is made by one of the existing v1 commands above, a listed `systemctl` act on a
v1 unit, or v1's play, driven by a declaration change.

### 5.6 Room history: readable in Element, and kept

Four options were considered:

1. **Export only.** The history is kept as a file, which Element users cannot browse.
2. **Keep the v1 server as an archive.** Element users browse the old rooms in the v1 profile,
   with every message, thread and reaction as they were. A read-only room would need a v1 write
   that v1 itself undoes: every run of `agent-bus-install team` calls `bootstrap`, whose
   `_sync_team` rewrites `m.room.power_levels` to v1's expected levels
   (`files/usr/local/sbin/agent-bus-install:831`, `helpers/agent_bus/admin.py:584-591,629-645`).
   So the archive stays writable, and what a human posts in it is caught by the archive check
   below, never lost.
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

- **Archive** (M8): no v1 member can post any more, and M8 records the archive marker, the last v1
  event per room. Under OD-D6's default the v1 server and its timers are then stopped and
  disabled, so the archive costs nothing resident. `ccy-teams archive open <team>` starts it
  (`StartArchive`, 5.5) for a human who wants to browse, and `archive close` stops it again. A v1
  play run for the team also starts it, because v1's `team` verb enables and starts every declared
  team's server (`agent-bus-install:739-751`). The board shows `archive running` whenever it is up, and `archive stopped`, not a
  health fault, when the stopped server's v1 health record goes stale.

- **The archive check.** `sudo teamchat-admin migrate archive-check --team <team>` reads the v1
  events after the archive marker over loopback, considers only humans' `m.room.message` events
  (display name, avatar and the state `bootstrap` writes are not posts), and carries each one to
  v2 as a carried item (5.2.1), or reports it if it mentions nobody. If the archive server is
  stopped and has not been active since the marker was set (systemd `ActiveEnterTimestamp`), there
  is nothing to check. Otherwise the check starts it if needed and leaves the unit as it found it.
  Each outcome is appended to `/var/lib/teamchat-admin/<team>/migration/archive-check.jsonl`, and
  the board shows the count of reported posts. It runs at `archive close`, at
  `ccy-teams migrate --status`, and at 7.2 step 1 before v1 is retired, and it moves the marker
  forward. So a human post in the archive reaches the team on v2, later but never silently lost.
  `archive open` tells the human so. The first `archive open` also unlocks the humans and, if M2
  logged them out, issues each a new v1 password once through ccy-teams' login flow.

- **Link** (M4): the v2 welcome notice is an admin message in the v2 main room, pinned. It says:

  - the team moved from agent-bus on <date>;
  - earlier history is in `history.md` below, and browsable in the Element window
    `<team> team (Element)` after `ccy-teams archive open <team>`; anything posted there is
    carried here later as a carried item;
  - `history.md` is attached. It is uploaded to v2's media store as an `m.file`, and split into
    parts below the server's upload limit when larger;
  - N items were carried and are marked as carried.

### 5.7 Rollback (before commit)

R is run by `ccy-teams migrate --rollback <team>`, and rolls back the whole set. It can be run from
any phase from `preflighted` to `carried`. Before it acts, it reads `fence.json`,
`teamchat-admin migrate fence-status` and `agent-bus list`, so that it undoes exactly what was
done, even when a crash interrupted M2 before the phase moved to `fenced`.

1. **Check that each member's v1 state is unchanged**, while the fence still holds, against the
   fence snapshot where one was taken. With the fence in place nothing can change it but a due
   `TIMEOUT` (5.2), so the check is not racing the watchers. `sync.json`, `inbox/` and `consumed/`
   must be byte for byte the same. `outbox.json` may differ only by targets added to `reported`.
   If anything else differs, R prints the difference and the carry plan as the list of items to
   tell members by hand, and continues only when the owner re-runs it with `--accept-state-diff`.
   R is never left unfinishable. P1 proves the fence changes nothing else. The result is recorded
   in `fence.json` (`state_check: passed` or `accepted`, with its time) before step 2. A re-run
   skips this step when that record exists and the fence is lifted, because the watchers have
   since synced. A lifted fence with no record means the fence was lost without R, and R stops and
   names it.
2. **Lift the fence**, if `fence.json` or the fence status shows it in place
   (`teamchat-admin migrate unfence`: deletes the table and disables `teamchat-fence@<team>`).
   Every v1 watcher reconnects from its stored sync token on its next retry. No token, bundle or
   member file was changed, so there is nothing to re-mint.
3. **Unlock every human that `fence.json` lists as locked.** If M2 also logged them out, each gets
   a new v1 password once through ccy-teams' login flow (`agent-bus human password`), because
   logout replaced their password (`helpers/agent_bus/admin.py:892-896`).
4. **Check that the team works again.** Every member that was running at the fence must show fresh
   v1 presence (`agent_bus.status`, renewed every 300 s) within 600 s, read by the admin over
   loopback. A member that does not is listed with its restart command (`ccy-teams restart` for a
   ccy session, its unit or tmux session for an LXC member). This step reports; it does not stop R.
5. **Discard the v2 side.** `teamchat-admin team remove --purge <team>` deletes the v2 team and its
   carried events. The `teamchat_teams` and `teamchat_lxc_members` entries are removed. The team's
   status record keeps `"origin": "migration"` and its pointer to the set, whose record is kept
   with phase `rolled-back`, so the team reads as v1-live (3.9). The `teamchat_migrations` entry is
   marked `rolled-back`, and `--declared` skips it until the owner declares the move again.

From `preflighted`, nothing was done to either system, so R only marks the migration state
`rolled-back`. From `staged`, R runs step 5 only, unless `fence.json` shows that M2 had started. A second run of
R finds every step done and changes nothing.

v1's backup from M2 is the last resort if v1's own state is damaged. Its restore is not atomic
(LESSON 13), so it is an owner decision at that moment, never an automatic step.

**Why rollback is safe for messages.** Before commit, the migration only reads v1 member state.
On v1's server it locks humans, possibly logs them out, and cuts the network. None of that touches
a member's token, bundle or state, and the fence stops new events. So the v1 world after R is the v1
world at the fence. The only costs are the time the team was bus-less and, for a team whose Element
address is loopback, a new password for each human.

### 5.8 Members the host cannot reach

- **Stopped LXC containers.** Their v1 state is read from the rootfs (5.5). Their carried items wait
  on the v2 server, and the v2 apply defers them until they start (6.3). They are not a blocker.
- **Remote members on other machines.** They are not managed from this host (TS 5). M0 refuses
  until the owner marks each one in `teamchat_migrations` as "moves by its own IaC", through
  ccy-teams, which then states the v2 bundle the remote member needs. The fence cuts the member off,
  so its v1 sends fail loudly. Its items are carried server-side with the `read_receipt` or `join` cut (5.2 row 1).

### 5.9 Sets of teams

When a session is seated in more than one v1 team, those teams form one set (D19). M0 computes the
set from the session registry, and the default (OD-D3) is to move it together:

- M0 to M4 run per team, all under one set record and one set lock.
- M5 checks every team's preconditions, then renames the set record once. That one rename commits
  the whole set.
- Rollback R applies to the whole set.

The alternative is for the shared sessions to leave the other teams first, with
`ccy-teams session remove`, so that the set shrinks to one team. ccy-teams offers this, and the
owner chooses it at M0. The leave runs before any set record exists, because 3.9 refuses a hot
remove once the set is at `preflighted`: M0 stops with the leave as its named precondition, the
owner does it, and M0 is re-run. Leaving a team through hot control is v1's normal leave, done while v1 is
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
  and neither `accepted` nor `rolled-back`, one set at a time, with the same confirmation. So the owner either runs a
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

| Check                      | Passes when                                                                                                                                                                                                                                                                                                                                   |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A5 sessions on v2          | Every ccy session that was in the team before M2, and is running, has restarted on the v2 kit. Its member's presence on v2 is fresh, and the session's process tree shows no v1 kit process.                                                                                                                                                  |
| A6 carried items delivered | Every carried item addressed to a running member is delivered by B's proof (the transcript nonce), or still pending with B's stated reason. Items for stopped or remote members are pending on the server, and the report lists them by member.                                                                                               |
| A7 a real exchange         | ccy-teams asks the team's coordinator (the first active worker if there is none) for one acknowledged ping through B's check verb. The acknowledgement must arrive within the verb's deadline. This is the one check that disturbs a live agent, and it costs one short turn.                                                                 |
| A8 v1 fenced and unchanged | No v1 event exists after `v1_last_event`. A fresh footprint differs from footprint-fence only by the v1 changes M6 logged in `log.jsonl` (the allowed difference is generated from that log, not written by hand): for example the removed guest team directories, env lines and v1 services, and the handles parked by v1's absent handling. |
| A9 the humans' view        | The owner opens the v2 Element window and sees the welcome notice. The v1 half (the old history in the v1 window) is checked after M8, through `ccy-teams archive open`, because the v1 server is fenced and the humans locked at M7. This is a look at the screen, asked for separately.                                                     |

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
- When the old container exits, the launcher runs v1's `seat disconnected` for its v1 seat, and the
  v1 SessionEnd goodbye runs inside the container. While the fence holds, both fail to reach the
  server. Both are bounded, and v1's disconnected step never holds up a restart
  (`files/var/local/claude-yolo/lib/agent-bus-membership.bash:87-111`). The failures are reported
  in M7 (5.2 row 7). P1 proves that the goodbye does not hold up the exit.

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

| Step                     | Where  | What it proves                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------ | ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1 lab: coexist          | VM lab | Scenario `teamchat-coexist` (4.3).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| P1 lab: migrate          | VM lab | Scenario `teamchat-migrate`. A v1 team is seeded with one item of every kind in the 5.2 table: an unsynced ping, an inbox item, an open ping before and after its deadline, an open question, an unaddressed human message, a consumed-while-out item, a stopped LXC member and a member whose state cannot be read. It also seeds a target's reply that the sender had not yet synced (5.2 row 5), a running ccy session with an unconfirmed delivery, a deadline that passes during the fence in a running ccy session with an empty inbox (5.2), a send whose response the fence cuts, a remote member with a read receipt and one without, a routine v1 play started during M2 to M5 (it must wait on the play lock), a new v1 seat requested during the fence (it must be refused, 3.9), and a symlinked `inbox/` entry and a FIFO in a guest. It then asserts each item's fate, A1 to A8, and no duplicate on a re-run. It probes what `human lock` alone does to a live Element token, and whether the admin can read other accounts' `m.read` receipts on Tuwunel, and records both answers. It runs M8 and the archive check: a human post in the opened archive is carried to v2, and a v1 play run afterwards restarts the archive without losing a post. It proves that a failed v1 goodbye does not hold up the M6 relaunch. Fault injection kills the run at every step, between each pair of M8's acts, and also between the set record's rename and its copies. Each re-run must resume and finish with the same result. |
| P1 lab: rollback         | VM lab | Scenario `teamchat-migrate-rollback`. Rolls back from each of `staged`, `fenced`, `captured` and `carried`. Also rolls back from a crash inside M2, before the phase moved. Asserts that the fence takes effect at once on the running server and on established long-polls, that a connect is refused from the host's bus address, from a ccy container over pasta and from an LXC guest over the bridge, and that the table survives a v1 server restart, `firewall-cmd --reload`, Docker's iptables-nft rules, `lxc-docker-user-iptables-reconcile.bash` and a VM reboot, and that `unfence` removes it. If any of these fails, the fallback drop-in (5.4) is proved the same way instead. Asserts, before the fence is lifted, that each member's v1 `state/` matches the fence snapshot (5.7 step 1), and that a seeded difference stops R until `--accept-state-diff`. After the fence is lifted, every watcher resumes with no restart, every seeded item is delivered by v1 exactly once, and every member's next v1 send succeeds. A second run of R changes nothing, also when R is killed between its steps 2 and 5. An `nft flush ruleset` during M3 stops the run before M4. This settles risk RK1.                                                                                                                                                                                                                                                                                                                                                                                                    |
| P2 host: install         | host   | v2 software through the plan's `deploy.bash` (meta-deploy). The plan's `triage.bash` runs `teamchat-admin footprint` before and after, which must show v1's footprint unchanged and v2 inert (D3).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| P3 host: v2-native pilot | host   | A throwaway v2 team named `teamchat-pilot` (a generic name defined by this design, not by an install), with one ccy session and one LXC member. It runs real work for at least one working session, its resident cost is measured (section 8), and then it is removed with `--purge`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| P4 host: migration pilot | host   | A throwaway v1 team named `migration-pilot`, made with v1, with two ccy sessions, one LXC member stopped during the run, a few messages, an open ping and an open question. It is migrated, rolled back, migrated again, accepted and archived; then both its v1 and v2 sides are removed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| P5 first real team       | host   | The owner picks the team, the smallest one by default. After that, teams move one at a time, at the owner's pace.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |

### 7.2 Retiring v1

When ccy-teams finds no v1-live team (every v1 team is `archived` or was never live), it shows
"every team has moved; v1 can be retired" and the plan raises it with the owner. That is the moment
the frame names. Nothing is retired before it.

The retirement is its own plan, with these steps, in order:

1. Run the archive check (5.6) for each archived team, then stop and disable its v1 server and
   timers. The v1 data stays on disk.
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

| When                                 | Per team                                                                                                                                                                                                                                                                                                                                                                    | Per member                                                              |
| ------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| v2 installed, no team                | Nothing resident. On the host: one dummy interface, the binaries and one sudoers file.                                                                                                                                                                                                                                                                                      | Nothing. The v2 kit is in the ccy image and inert without a membership. |
| During a migration (M1 to M8)        | Two homeservers (v1 and v2), two ports, and both systems' health and backup timers. From M2 to M8, the fence: one nftables table and one enabled oneshot unit, `teamchat-fence@<team>.service`, with no resident process. M2 rotates out v1's oldest backup once (5.3). Under the fallback drop-in, one v1 server restart at fence and one at unfence instead of the table. | One session restart per ccy session. No session ever runs both kits.    |
| After the move (M8 until v1 retires) | Under OD-D6's default, nothing resident: the v1 archive is stopped, and runs only while a human has it open or after a v1 play has started it again (5.6). Under OD-D6 (a), one extra resident Tuwunel and its two timers until v1 retires.                                                                                                                                 | Nothing.                                                                |
| After v1 retires                     | v2 only.                                                                                                                                                                                                                                                                                                                                                                    | v2 only.                                                                |

v1 has no measured memory figure for an idle Tuwunel team server in the repo. P3 measures v2's,
and the same reading on a v1 archive server gives the cost of OD-D6's default. Both are recorded in
the plan's triage output. The ccy image carries both kits until v1 retires. The v2 kit is one
static binary plus the plugin files.

## 9. Decisions for the owner

Each decision has a default, and the work proceeds on it unless the owner says otherwise.

| ID    | Decision                                             | Options                                                                                                                                                                                                             | Default and why                                                                                                                                                                                                                                                                                                                          |
| ----- | ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OD-D1 | The product name                                     | `teamchat`; `agentroom`; `teampost`                                                                                                                                                                                 | `teamchat`: it says what it does for both agents and humans, and it works as a prefix for every name (2.1).                                                                                                                                                                                                                              |
| OD-D2 | A session asked to join teams on both systems        | (a) refuse, with the two choices; (b) allow both kits in one session                                                                                                                                                | (a): two kits would share one status line, one `crossSessionInbound` key and one inbox socket whose 2 s pacing drops the other system's messages (3.9).                                                                                                                                                                                  |
| OD-D3 | Teams linked by a shared session                     | (a) move the linked set together under one commit; (b) refuse until the shared sessions leave the other teams                                                                                                       | (a), with (b) offered in M0: (a) keeps every session working throughout; (b) is available when the owner wants smaller steps.                                                                                                                                                                                                            |
| OD-D4 | Import v1 history into the v2 rooms                  | (a) no import: export plus v1 archive plus link; (b) import through appservice timestamp massaging, if a probe proves Tuwunel supports it                                                                           | (a): import is unverified on Tuwunel, needs puppet accounts and an appservice registration (more privilege), and must land before members join. Revisit only if a probe proves it and the owner wants native history.                                                                                                                    |
| OD-D5 | A reverse move after commit                          | (a) none: fix forward on v2; (b) a v2-to-v1 migration                                                                                                                                                               | (a): before commit, rollback covers every failure. After commit, v1 is a frozen prototype, and building the reverse path doubles the work for a case the pilot is there to prevent.                                                                                                                                                      |
| OD-D6 | How long a moved team's v1 archive server runs       | (a) running until v1 retires; (b) stopped at M8, opened by ccy-teams when a human wants to browse; (c) stopped at M8 for good, export only                                                                          | (b): it answers "can Element users still see old history?" with yes (D10) and costs nothing resident. Under every option the archive stays writable, because making it read-only needs a v1 write that v1's own play undoes (5.6); the archive check carries what is posted there. (a) is the choice if humans browse old history often. |
| OD-D7 | An open ping awaiting an acknowledgement at the move | (a) settle: a `moved` notice to the sender, the ping carried to the target, and the answer delivered to the sender as a reply; (b) carry the deadline into the sender's v2 outbox so that v2 raises `TIMEOUT` later | (a): it needs no cross-system acknowledgement plumbing in B's outbox, and the sender is told plainly. (b) would make v2's outbox know about v1 event ids.                                                                                                                                                                                |

## 10. Risks

| ID   | Risk                                                                                                                                                                                                                         | Mitigation                                                                                                                                                                                                                                                                                                                                                                                    |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| RK1  | The network fence may not take effect on the running server, may not survive a restart or reboot, or may disturb a member's `state/`. Rollback would then lose unsynced items, because v1's first sync uses `limit 0`.       | P1's rollback scenario proves it before any host run. M2's fence proof fails fast if the fence is not in place. R's step 1 compares the state with the fence snapshot while the fence still holds, and stops with the carry plan as the list to tell members until the owner accepts the difference, never silently. If the nftables table does not hold, the fallback drop-in (5.4) is used. A ruleset flush cannot be prevented; M3 and M4 check `fence-status` at their start and end, so a lost fence stops the run before any carry. |
| RK2  | The migration's port of v1's addressing rule differs from v1's own, so items are over-carried or under-carried.                                                                                                              | The rule is ported as a decoder with a parity oracle against v1's Python, per src-go's parity convention. Over-carrying is preferred, and the item is flagged `possible_duplicate`.                                                                                                                                                                                                           |
| RK3  | A large `history.md` exceeds the v2 upload limit.                                                                                                                                                                            | It is split into numbered parts below the limit. The export on disk is whole.                                                                                                                                                                                                                                                                                                                 |
| RK4  | Agents are bus-less from M2 until their restart in M6.                                                                                                                                                                       | M2 to M6 run without stopping after one confirmation. Every v1 send fails loudly, and the owner is shown the bus-less sessions.                                                                                                                                                                                                                                                               |
| RK5  | Humans confuse the two Element windows.                                                                                                                                                                                      | Distinct launcher names (`(teamchat)` and the v1 `(Element)`), the v1 archive stopped unless a human opens it, and the pinned welcome notice.                                                                                                                                                                                                                                                 |
| RK6  | The adapter reads v1's admin token.                                                                                                                                                                                          | Root only, read-only, never logged, used only for the export's paging and event fetches (5.5), within 4.1 part 2's analyzer bounds.                                                                                                                                                                                                                                                           |
| RK7  | A remote member never rejoins on v2.                                                                                                                                                                                         | Its items wait on the server, and ccy-teams lists it as pending in the board until it joins or is removed.                                                                                                                                                                                                                                                                                    |
| RK8  | The footprint diff flags v1's own changes (a backup, a health record).                                                                                                                                                       | Paths that v1 changes on its own are recorded by path and type only (4.3). A difference there that is not type or presence is a real finding.                                                                                                                                                                                                                                                 |
| RK9  | A failed v1 goodbye or `seat disconnected` holds up the old container's exit, and so the M6 relaunch.                                                                                                                        | v1's disconnected step is bounded and never holds up a restart (6.2). P1 proves that the bounded SessionEnd goodbye does not hold up the exit either.                                                                                                                                                                                                                                         |
| RK10 | For a team whose Element address is loopback, the fence logs humans out, and logout replaces their password, so after a rollback or at M8 each needs a new v1 password. `human lock` semantics on Tuwunel are also unproven. | Logout is what makes the fence certain whatever lock does. P1 records what lock alone does, so the cost can be revisited with evidence.                                                                                                                                                                                                                                                       |

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

- `teamchat-admin` has these migration verbs:
  `migrate stage|fence|unfence|fence-status|carry|commit|archive|archive-check`, `footprint`, and
  `team remove --purge`. It owns the fence's nftables table and `teamchat-fence@<team>.service`,
  and makes the `systemctl` acts on v1 units listed in 5.5.
- `teamchat-admin team create` writes `"origin": "v2"` in the status record, and the migration
  writes `"origin": "migration"` (3.9).
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
- The launcher decides a team's system from the status directory alone, without sudo and without
  host_vars (3.9).
- `cc --teams` refuses a v2-live team until B defines the v2 host-member launch (3.9).
- The launcher and ccy-teams refuse a new v1 seat, a hot add and a hot remove for a team whose
  set is between `preflighted` and `carried`, read from the status directory (3.9).
- ccy-teams gains `migrate`, `archive open|close`, and holds the user play lock from M2 to M5.
- These are changes to ccy, not to v1, and each one is a CCY version bump.
