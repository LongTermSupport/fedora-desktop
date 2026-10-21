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
| D6  | Teams move one at a time (or one linked set, D19). The owner starts the move from ccy-teams, and host runs go through meta-deploy.                                                                                             | FRAME; CLAUDE.md                 |
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
| D19 | Teams that share a session (a session seated in both) form a linked set. Because a session is on one system (D5), a linked set moves together, under one commit, or the shared sessions leave the other teams first.           | D5; FRAME "no team mixed"        |

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
   - Exempt: only `src-go/internal/teamchat/migrate/v1/` and its tests, and the docs.
2. **The v1 adapter writes no file.** A `go/analysis` analyzer, catalogued in
   GoProjectRules.md with its red/green proof, checks `migrate/v1`:
   - it imports no file-writing call: `os.Create`, `os.WriteFile`, `os.OpenFile` with a write
     flag, `os.Remove*`, `os.Rename`, `os.Mkdir*`, `os.Chmod`, `os.Chown`, `os.Symlink`;
   - it reaches v1 only through a fixed, typed list of v1 CLI calls (5.5), run with `exec.Command`
     and argument lists, and through read-only file opens.
3. **The single source holds no v1 prefix.** A unit test in `src-go/internal/teamchat/paths`
   checks every exported path and name against the same v1 list. It also checks that no v1 name is
   a prefix of a v2 name and no v2 name is a prefix of a v1 name. This guards against a careless
   glob or `rm -r <prefix>*`.

The red proof: a fixture under `testdata/` with one v1 path in a v2 play, and one `os.WriteFile`
in an adapter fixture. The gate must fail on each.

### 4.2 Runtime confinement

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
   - the v1 kit in the ccy image, by `podman image inspect` and a listing of
     `/opt/claude-yolo/optional/agent-bus` in the image.
3. **Install v2 with no team.** Check that it is inert (D3): no `teamchat*` unit is active, `ss`
   shows no new listener, and no file appears outside v2's roots, by a whole-filesystem find
   against a pre-install snapshot, excluding `/proc`, `/sys`, `/run/user` and the journal.
4. **Run the v2 team cycle.** Create a v2-native team, join a seat and an LXC member, exchange
   messages, take a backup, restore it, then remove and purge the team.
5. **Take manifest B and compare it with A.** Any difference fails the scenario. Then run v1's
   acceptance again; it must pass, and the v1 team must still exchange a ping round trip.

The scenario runs whenever the v2 install or play changes, as one of the plan's VM lab gates. Its
manifest code is shared with the migration's capture step (5.4 M3), which records the same
footprint as evidence.

## 5. The per-team migration

### 5.1 What moves, and how

| Item                                       | What happens                                                                                                                                                                                                                                       | Why this way                                                                                                                                          |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| Team name, description, repos, forge rules | The same team name on v2. The declaration is copied into a new `teamchat_teams` entry with a `from_v1: true` marker, and the owner reviews it in ccy-teams before staging.                                                                         | Seat strings like `3@<team>` in the session registry keep working. The marker lets the validator accept a name that also exists in `agent_bus_teams`. |
| Agent accounts                             | Re-created on v2. Tokens cannot be copied between servers (TS 4). Each v1 handle maps to a v2 handle by A's grammar. If A keeps v1's `<repo>.<seat>+<host>.<type>`, the mapping is the identity. The mapping table is part of the migration state. | A fresh account per server is the only route; the mapping keeps forge text and humans' memory of who is who.                                          |
| Roles                                      | Copied from v1's `registry.json` (`coordinator`, `worker`). At most one coordinator, as in v1.                                                                                                                                                     | RH 4.                                                                                                                                                 |
| Parked members                             | Created on v2 as parked: account, role and handle are kept, and no token is minted. A return on v2 mints one.                                                                                                                                      | Keeps the seat meaning; mints nothing unused.                                                                                                         |
| Seat counters, retired handles             | The counters are copied, so v2 never issues a number below v1's. Retired (burnt) v1 handles are recorded as retired on v2 and are never issued.                                                                                                    | A handle that once meant one member never means another (RH 7.09).                                                                                    |
| Humans' accounts                           | Created on v2 with the same name and the v2 power level A defines. The password is set at commit, through the existing ccy-teams login flow, and shown once. Humans in `removed_humans` are not created; their names stay burnt.                   | No password is ever stored (RH 1.2).                                                                                                                  |
| Humans' Element logins                     | A new profile `teamchat-<team>` and launcher `<team> team (teamchat)`. The v1 profile and launcher stay untouched, as the archive viewer (5.6).                                                                                                    | The v1 profile is bound to v1's server (TS 3).                                                                                                        |
| Read markers, mutes, notification rules    | Not carried. The welcome notice (5.6) tells humans to re-mute the main room if they followed only the side rooms.                                                                                                                                  | Account data is per server and is small to redo; carrying it needs per-human writes into account data for little gain.                                |
| Room history                               | Exported losslessly and kept, with v1 kept read-only as the archive (5.6). Imported into v2 only if OD-D4 is taken.                                                                                                                                | TS 4: no import route is evidenced in Tuwunel.                                                                                                        |
| Queued and in-flight items                 | Carried or settled one by one, per 5.2.                                                                                                                                                                                                            | D9.                                                                                                                                                   |
| Bundles                                    | New v2 bundles. They are placed by v2's seat take (ccy) or v2's LXC apply, never into v1 paths.                                                                                                                                                    | Bundles pin `base_url`, `user_id` and room (TS 2.1).                                                                                                  |
| Forge cache                                | Not carried. It is a cache with a TTL and refills on first use.                                                                                                                                                                                    | YAGNI.                                                                                                                                                |
| Presence (`agent_bus.status`)              | Not carried. It is soft state, renewed every 300 s.                                                                                                                                                                                                | TS 2.3 item 8.                                                                                                                                        |

### 5.2 Queued and in-flight messages: what happens to each

v1 has no outbound send queue. Sends are synchronous and fail loudly (TS 2.3). In-flight items
therefore sit in six places. The migration first fences v1 so that no new item can appear (step
M2), then reads every place (step M3). The result is a **carry plan**, a JSON file in the
migration state. Each item has exactly one fate:

- **carry**: the item is posted on v2 to its recipient as a carried item (5.2.1);
- **settle**: the sender gets a carried notice that says what happened;
- **report**: the item is listed by name in the migration report the owner reads.

| v1 place (TS 2.3)                                                                                                                     | How the migration finds it                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Fate                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. **Sent, not yet synced**: on the v1 server, after the member's `next_batch`                                                        | Reading the member's `sync.json` gives the cut point. The admin pages `/messages` after it in each room and applies the v1 addressing rule (A states it for v2; v1's is in PROTO): pings naming the member, human messages mentioning it or `@room`, and questions-room items for coordinators. If the member's state cannot be read (a remote member), the cut point is the member's device `last_seen_ts` minus 60 s (twice the long-poll), taken from the v1 admin device list. | **carry**. When the cut point came from `last_seen_ts`, the item is marked `possible_duplicate`.                                                                                                                                                                                                                                                                                                                                   |
| 2. **Synced, not delivered**: `state/inbox/<eid>.json`                                                                                | Read from disk: a ccy seat's state in `<checkout>` (user-readable); an LXC member's state from the container's rootfs as root, read-only, also when the container is stopped (as v1's LXC apply reads checkouts, RL 1). The event is fetched again from the v1 server by id.                                                                                                                                                                                                       | **carry**.                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 3. **Consumed undelivered on purpose** (a seat removed while out, H23a)                                                               | `consumed/` entries for a seat that is not in the team now.                                                                                                                                                                                                                                                                                                                                                                                                                        | **report** only. v1's own rule is that nothing posted while a member is out is delivered on return. Carrying would change that rule.                                                                                                                                                                                                                                                                                               |
| 4. **Delivered, unconfirmed**: written to the session's inbox socket, nonce not yet seen in the transcript, or `inject/<key>.missing` | `inject/` records where they survive on disk. They live in the session home: a ccy container's home is gone once the container exits, but a host or LXC member's is not.                                                                                                                                                                                                                                                                                                           | **carry** again, marked `possible_duplicate`, when a record is found. When no record can exist (a ccy home after the session ended), the report says "unconfirmable deliveries cannot be listed for <session>". This is v1's existing gap (RP 4), named, not hidden.                                                                                                                                                               |
| 5. **Ping awaiting an acknowledgement**: an `outbox.json` entry with targets not in `answered` or `reported`                          | Read from disk with the inbox.                                                                                                                                                                                                                                                                                                                                                                                                                                                     | **settle**. If the deadline has passed, the sender gets the `TIMEOUT` item v1 would have given. If it has not passed, the sender gets a `moved` notice: "your <verb> to <target> was not answered before the team moved; it was carried to <target> on teamchat; its answer will reach you as a reply." The ping itself reaches the target as item 1 or 2 if it was not delivered. If it was delivered, the target already has it. |
| 6. **Open questions** (`Q<n>` with no close in the questions room)                                                                    | Read from the v1 questions room's history (S211 8).                                                                                                                                                                                                                                                                                                                                                                                                                                | **carry** into the v2 questions room as an open question that keeps its number `Q<n>`. The v2 numbering starts above the highest v1 number (assumption on A, section 12).                                                                                                                                                                                                                                                          |
| Human messages that mention nobody                                                                                                    | They were never delivered on v1 (TS 2.3 item 6).                                                                                                                                                                                                                                                                                                                                                                                                                                   | **report** a count. They are in the export and the archive.                                                                                                                                                                                                                                                                                                                                                                        |
| Free text sent by an agent to a human                                                                                                 | Already in the room. Humans read the room.                                                                                                                                                                                                                                                                                                                                                                                                                                         | Nothing to carry. In the export and the archive.                                                                                                                                                                                                                                                                                                                                                                                   |

#### 5.2.1 Carried items on v2

- A carried item is a v2 event sent by the v2 admin into the room where the original was (main,
  reports or questions), addressed to one member.
- It holds the original sender's handle (mapped), the original `origin_server_ts`, the kind (ping
  with verb, text, question, `TIMEOUT`, `moved`), the text, the v1 event id, and the
  `possible_duplicate` flag.
- A defines the content schema under `teamchat.carried` (section 12). The trust rule is short:
  only the admin may send it, and members believe it as the admin's statement of what an old v1
  sender said, never as that sender speaking.
- It is posted with a transaction id derived as `sha256(<v1 event id> | <target> | <kind>)`, so a
  re-run of the carry step never posts a second copy (D7).
- B renders it into the session with a header that says it was carried from v1 and when the
  original was sent. It is delivered on the member's first v2 start, like any pending item.

**Why carried items are posted to the server, not written into members' v2 inboxes.** Writing
straight into a member's on-disk inbox would be simpler for ccy seats. But it would bypass the
server as the source of truth, and v2's member re-fetches items from the server before printing,
as v1 does (TS 2.2). It also cannot reach a stopped or remote member. An event on the server
reaches every member kind, survives a member's state being wiped, and is visible to humans as an
audit trail.

#### 5.2.2 The carry plan

The carry plan is written to `/var/lib/teamchat-admin/<team>/migration/carry-plan.json`, 0600:

```json
{
  "v": 1,
  "team": "<team>",
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
    {"kind": "unaddressed_human_messages", "count": 4}
  ]
}
```

- `state_read_from` is one of `seat`, `lxc-rootfs`, `host-config` or `server-only`.
- `cut.kind` is `next_batch` or `last_seen`.

The migration prints the plan as a human report (counts per member and per fate, and every
`reported` row) at the dry run and again at commit. The owner sees every item that is not carried.

### 5.3 The state machine and what "commit" means

The migration's state lives in `/var/lib/teamchat-admin/<team>/migration/`, root 0700:

- `state.json` holds the phase. It is written by temporary file, fsync, rename and directory
  fsync, as every v2 state file is.
- `log.jsonl` gets one line per step, start and end.
- `carry-plan.json` is the carry plan.
- `v1-export/` holds the export (5.6).
- `footprint-before.json` and `footprint-after.json` hold the v1 manifest of 4.3.

The phase is also published, without secrets, as `migration.phase` in
`/var/lib/teamchat-status/<team>.json` for ccy-teams.

```text
declared ──M0──▶ preflighted ──M1──▶ staged ──M2──▶ fenced ──M3──▶ captured ──M4──▶ carried
                                       │              │               │              │
                                       └──────────────┴───── R ───────┴──────────────┘
                                                              ▼
                                                         rolled-back
carried ──M5──▶ committed ──M6──▶ moved ──M7──▶ accepted ──M8──▶ archived
```

**Commit** is one atomic act: renaming `state.json` from phase `carried` to phase `committed`. That
rename is the moment the team's live system changes from v1 to v2 (3.9). Its preconditions are
re-checked immediately before it:

1. the v2 team passes acceptance part 1 (6.1);
2. the count of carried events on the v2 server equals the plan's count;
3. v1 is still fenced, and no v1 event exists after `v1_last_event`;
4. the export's digest is recorded.

- **Before commit**, everything is undone by rollback **R** (5.7), and v1 is the live system.
- **After commit**, the migration only moves forward. A failure in M6 to M8 is fixed on v2 and the
  step is re-run. The v1 team stays fenced and is never live again. A reverse move is not provided
  (OD-D5).

Every step is idempotent. A re-run reads the phase and resumes the current step. Each step's
side effects are guarded:

- creates are "exists with the same content" checks;
- posts use derived transaction ids;
- v1 fence verbs are skipped when v1 already reports the state they set.

A step that finds the world different from what its phase implies stops with the difference
named, for example "phase fenced but member <handle> is not parked on v1". It never guesses.

### 5.4 The steps

Who runs a step:

- `ccy-teams migrate` orchestrates, as the desktop user. Root steps run as
  `sudo teamchat-admin migrate <step> --team <team>`, with the owner's sudo password, as v1 backups
  already need. v1 steps run as `sudo agent-bus <verb>`, through the adapter's fixed list (5.5).
- Plays run through `run.bash` single-play mode, under the user play lock, as ccy-teams does for
  team changes today (TS 6).

| Step         | Does                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Fails fast when                                                                                                                                                                                                                                                                                                                                |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0 preflight | Reads only. Lists the members: ccy seats from v1's registry and seat dirs; LXC members from `agent_bus_lxc_members` and v1's LXC record, with running or stopped state; remote members, meaning registry handles that are neither of those. Lists the sessions in the team from the session registry. Computes the **linked set**: the team plus every v1 team that shares a session with it (D19, OD-D3). Checks that v2 software is installed and its version is accepted, that the port is free (3.8), that the v1 team is healthy (fresh health record), and that a v1 backup can be taken. Prints the plan of record. | v1 is unhealthy; v2 is not installed; the linked set has more than one team and the owner has not chosen to move it together (OD-D3); a remote member has not been acknowledged by the owner as "moves by its own IaC" (5.8); an LXC member's rootfs cannot be read; a v1 state file fails to parse (a v1 StateError is a stop, never a skip). |
| M1 stage     | Writes `teamchat_teams[<team>]` with `from_v1: true` and the LXC members into `teamchat_lxc_members` with v2 state `staged`. Runs `play-teamchat.yml` for this team. That play installs the v2 server, rooms and admin, and runs `teamchat-admin migrate stage`, which creates every account, role, parked state, counter and retired handle from the M0 snapshot and the human accounts (no passwords). Runs a v2 probe: a temporary probe account sends to itself and reads it back, then is removed. v1 is untouched.                                                                                                   | Any v2 creation fails; the probe round trip fails.                                                                                                                                                                                                                                                                                             |
| M2 fence v1  | (a) `agent-bus-install backup-now --team <team>`, recorded as the rollback point. (b) A notice to the team's ccy sessions, through the existing hot-control path, saying the team is moving and sends will fail until the restart. (c) Every human is locked on v1 (`agent-bus human lock`). (d) Every active member is parked on v1 (`agent-bus park-member`); this revokes the token on the server, so a running watcher gets 401 and stops, and any send fails loudly. (e) The fence record lists exactly what was locked and parked, for R.                                                                            | The backup fails; a park or lock fails; after the fence, a v1 member's device still syncs (checked through the admin device list).                                                                                                                                                                                                             |
| M3 capture   | Waits until no v1 event has appeared for 60 s. Fixes `v1_last_event` per room. Exports every room's full history (5.6). Reads each member's v1 state read-only (5.2 table). Writes the carry plan and footprint-before. Prints the report and asks the owner to confirm, unless run with `--yes`.                                                                                                                                                                                                                                                                                                                          | A v1 event appears after the fence (something unfenced is still posting: stop, name the sender); a member's state cannot be read and the member is not marked `server-only`; the export's event count disagrees with a second paging pass.                                                                                                     |
| M4 carry     | Posts every carry and settle item on v2 with derived transaction ids, and the open questions. Posts the welcome notice (5.6).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Any post fails after its retries; the count on the server differs from the plan.                                                                                                                                                                                                                                                               |
| M5 commit    | Re-checks the four preconditions (5.3) and renames the phase to `committed`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Any precondition fails. The migration stays at `carried`, and the owner can re-run M5 or roll back.                                                                                                                                                                                                                                            |
| M6 move      | The humans' passwords are set and shown once (ccy-teams login flow). The v2 Element profile is written (v2's Element play). Running ccy sessions restart onto the v2 kit (6.2). LXC members: their `agent_bus_lxc_members` entries are removed and the `teamchat_lxc_members` entries made `present`; then v1's play runs for this team, which removes the team from each guest's v1 env and stops the v1 service, followed by v2's play, which places the v2 kit, bundles and service. Stopped containers are deferred (6.3).                                                                                             | A ccy session fails to come up on v2 within its bound; a running LXC guest's apply fails. Deferred containers are not failures.                                                                                                                                                                                                                |
| M7 accept    | Acceptance part 2 (6.1).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Any check fails. Fixed forward on v2, then re-run.                                                                                                                                                                                                                                                                                             |
| M8 archive   | Makes the v1 rooms read-only (5.6), unlocks the v1 humans so that they can read the archive, and writes footprint-after. Removes the team's v1 seat bundles through v1's own `seat remove` (members stay parked on the server). ccy-teams then shows the team as `teamchat`, and the v1 team as `archive`.                                                                                                                                                                                                                                                                                                                 | A v1 verb fails.                                                                                                                                                                                                                                                                                                                               |

**Why the fence parks members instead of stopping sessions or the v1 server.** Three fences were
considered:

1. **Stop every session in the team.** This cannot reach an LXC member run by hand in tmux, a
   stopped container that is started later, or a remote member.
2. **Stop the v1 server.** This also stops the export and the state re-fetch that M3 needs, and
   restarting it only for the migration would need a v1 config change.
3. **Park every member and lock every human.** This works on the server, so it covers every member
   kind, running or not. It uses v1's own verbs, and it is undone by v1's own `return` and `unlock`.
   Running sessions keep working on local work and fail loudly on a bus send.

The third is chosen. Changing the room's power levels to read-only was rejected as a fence: every
running v1 member would read the room as untrusted and exit 10, which is permanent (TS 4). It is
used only in M8, when no v1 member is left.

### 5.5 The v1 adapter's fixed reach

`src-go/internal/teamchat/migrate/v1` exposes exactly these operations, each a typed Go method:

- **Reads**
  - `List(team)`, `HumanDevices(team, name)` and `Health(team)`, through the existing v1 CLI;
  - `ReadRegistry(team)`, `ReadTeamFile(team)` and `ReadRoomIDs(team)`, reading the root-owned v1
    files read-only, as root;
  - `ReadMemberState(path)`, which reads a member's v1 `state/` read-only, with v1's strict
    parsers ported as read-only decoders;
  - `ExportRoom(team, room)`, which uses the v1 admin token read-only and pages `/messages`.
- **v1 verbs** (as `sudo agent-bus ...`, `sudo agent-bus-install ...`, or ccy-teams' existing
  calls)
  - `BackupNow(team)`;
  - `LockHuman` and `UnlockHuman`;
  - `Park` and `Return`;
  - `SetRoomsReadOnly(team)`.

`SetRoomsReadOnly` is the one new v1 verb. It is added to v1's admin tool, which counts as a v1
change. It is used only after commit, and it is tested in the v1 test suite. The alternative was
the v2 adapter writing power levels with v1's admin token, which would give the adapter a write
route into v1's server.

Every other v1 write the migration causes goes through v1's existing verbs and v1's existing play,
driven by declaration changes in `host_vars`.

### 5.6 Room history: readable in Element, and kept

Four options were considered:

1. **Export only.** The history is kept as a file, which Element users cannot browse.
2. **Keep the v1 rooms read-only as an archive.** Element users browse them in the v1 profile,
   with every message, thread and reaction as they were. The cost is one resident v1 Tuwunel per
   moved team, until the owner stops it.
3. **Link.** A Matrix link cannot cross into a different server's account, so a link is only
   text. It is cheap and only useful together with 2.
4. **Import into v2 rooms.** History would be native in the v2 room. This needs appservice
   timestamp massaging, which is UNVERIFIED on Tuwunel (TS 4). It also needs puppet accounts and
   an appservice registration, more privilege surface. The import must also land before any member
   joins, or members would receive it as new.

**Default: 1, 2 and 3 together. 4 only if OD-D4 is taken after a probe proves it.**

- **Export** (M3): `/var/lib/teamchat-admin/<team>/migration/v1-export/`

  - `events-<room>.jsonl` holds every event as the server returns it;
  - `history.md` is a readable rendering: time, sender, text, threads;
  - `digest.json` holds counts and the sha256 of each file.

  The export is root 0700 and is backed up by `teamchat-backup@<team>` with the team's state. It
  outlives v1's retirement. It is v1 Plan 00178's export, built as part of v2's migration.

- **Archive** (M8): the v1 main and side rooms get `events_default` raised to 100. This uses the
  new v1 verb from 5.5, on rooms that no v1 member reads any more. The humans are unlocked so that
  they can read. Their v1 Element profile now shows the full old history, read-only.

- **Link** (M4): the v2 welcome notice is an admin message in the v2 main room, pinned. It says:

  - the team moved from agent-bus on <date>;
  - earlier history is in the Element window `<team> team (Element)`, read-only;
  - `history.md` is attached. It is uploaded to v2's media store as an `m.file`, and split into
    parts below the server's upload limit when larger;
  - N items were carried and are marked as carried.

### 5.7 Rollback (before commit)

R can be run from any phase from `staged` to `carried`, by `ccy-teams migrate --rollback <team>`.
It runs the fence record in reverse, then discards the v2 side:

1. **Return every member parked by M2** (`agent-bus return`; v1 mints a new token). ccy seats get
   their new bundle by v1's seat take when their session is restarted. LXC members get theirs from
   v1's play. `agent_bus_lxc_members` was never changed before commit, so that play rejoins them.
2. **Unlock every human locked by M2.**
3. **Check that each member's v1 state is unchanged.** `sync.json`, `inbox/` and `outbox.json`
   must be byte for byte what M3 read. v1 delivers them as usual after the restart, so nothing is
   lost and nothing is duplicated. If a file differs, R stops and prints the carry plan as the list
   of items to tell members by hand. This is an alarm, not an expected path: the pilot tests that
   park and return leave `state/` untouched (P3).
4. **Restart the team's ccy sessions** with `ccy-teams restart <team>`. They resolve to v1, which
   is still the live system.
5. **Discard the v2 team.** `teamchat-admin team remove --purge <team>` deletes it and its carried
   events, and the `teamchat_teams` entry is removed. The migration state directory is kept, with
   phase `rolled-back`, as the record.

v1's backup from M2 is the last resort if v1's own state is damaged. Its restore is not atomic
(LESSON 13), so it is an owner decision at that moment, never an automatic step.

**Why rollback is safe for messages.** Before commit the migration only reads v1 member state and
only parks and locks on v1's server. No v1 file is written, and the fence stops new events, so the
v1 world after R is the v1 world at the fence, with new tokens.

### 5.8 Members the host cannot reach

- **Stopped LXC containers.** Their v1 state is read from the rootfs (M3). Their carried items wait
  on the v2 server, and the v2 apply defers them until they start (6.3). They are not a blocker.
- **Remote members on other machines.** They are not managed from this host (TS 5). M0 refuses
  until the owner marks each one in ccy-teams as "moves by its own IaC". The marker is a field in
  the `teamchat_teams` entry, and ccy-teams states the v2 bundle the remote member needs. The fence
  parks the member, so its v1 sends fail loudly. Its items are carried server-side with the
  `last_seen` cut.
