# Plan 00161 design: the agent team bus

Design for [fedora-desktop#59](https://github.com/LongTermSupport/fedora-desktop/issues/59).
The wire rules (verbs, event schema, reference grammar, validation, limits, exit codes,
output format) are in [PROTOCOL.md](PROTOCOL.md); this file does not repeat them. Evidence
for every factual claim is in `subagent-reports/261006-research-*.md` (cited below as
`[ccy]`, `[services]`, `[python]`, `[tuwunel]`, `[clients]`, `[wake]`). The three design
reviews and what was done with each finding are in
`subagent-reports/261006-design-revision-opus-5-5.md`.

Placeholders used throughout: `<team>` (team name), `<port>` (team's host loopback port),
`<sn>` (team `server_name`), `<handle>` (agent handle), `<hash>` (ccy per-project hash),
`<ns>` (event namespace, PROTOCOL.md section 2), `<subnet>` and `<hs_ip>` (the team
network's subnet and the homeserver's fixed address on it).

## 0. Threat model in one paragraph

Every agent holds its own Matrix access token in a container where it, and any code in
its repository, runs arbitrary commands. So `pingbus` is a convenience, not the boundary:
anything pingbus declines to print or send, the agent can fetch or send with `curl`. The
design therefore puts every hard rule where the agent cannot route around it:
**the homeserver's membership and auth rules** (an agent account is never a member of a
room a human types in, so human text is never delivered to it), **the receiver's own
checks** (a ping is shown only if its reference resolves, at the forge, to content on a
trusted branch of an allowlisted repository), and **host-only credentials** (the team's
admin, steward and warden secrets live in a tree no container can mount). Free text an
agent could act on exists nowhere an agent can read it.

## 1. Shape in one paragraph

Each team is one Tuwunel container, rootless under podman, managed by a Quadlet unit, on its
own `--internal` podman network with DNS off and a fixed subnet, published to the host on
`127.0.0.1` only. Team definitions live in untracked host_vars; the play renders everything
per team and can remove a team. Accounts are runtime data, created through Tuwunel's
Synapse-compatible admin API by a host-only `agent-team` command (registration is never
open). Rooms come in pairs, both created by a host-only **steward** account at a human's
request: a **bus room** (agents and the warden, no humans) and its **control room** (humans
and the warden, no agents). Members run `pingbus`, one standard-library zipapp, which
validates on send and on receive (the forge check runs on both sides) and keeps a durable
local inbox. A ccy session joins a team with `ccy --team <team>`: the launcher gets the
handle and a freshly rendered member config from the host, mounts them read-only, and
attaches the team network as a second network. The image always ships pingbus and a
Claude Code plugin (skill and hooks), inert until `--team`. An idle session is woken by its
own background `pingbus wait`; a Stop hook keeps it from going idle with pings pending or no
waiter armed. Humans use Element (one Flatpak profile per team, locked to the homeserver)
in control rooms; the warden turns their `!` commands into pings in the paired bus room and
mirrors bus traffic back as notices.

## 2. Components and where they live

| Component                         | Repo path                                                                                                                                                                        | Runs where                           | Built in unit |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ | ------------- |
| Protocol spec (SSoT)              | `docs/agent-team-bus-protocol.md` (moved from this plan's `PROTOCOL.md` in U02; the plan file then links to it)                                                                  | n/a                                  | U02           |
| Validator (pure)                  | `helpers/pingbus/protocol.py`                                                                                                                                                    | everywhere (inside the zipapp)       | U02           |
| Limits arithmetic (pure)          | `helpers/pingbus/limits.py`                                                                                                                                                      | everywhere                           | U03           |
| Member config                     | `helpers/pingbus/config.py`                                                                                                                                                      | everywhere                           | U04           |
| CLI front end (dispatch only)     | `helpers/pingbus/cli.py`                                                                                                                                                         | everywhere                           | U05, U11, U19 |
| Durable inbox, outbox, lock       | `helpers/pingbus/inbox.py`                                                                                                                                                       | everywhere                           | U06           |
| Forge check and provenance        | `helpers/pingbus/forge.py`                                                                                                                                                       | everywhere (syncer and send)         | U07           |
| Fake homeserver (tests)           | `tests/helpers/pingbus/fake_client_api.py`, `tests/helpers/pingbus/fake_admin_api.py`, fixtures `tests/helpers/pingbus/fixtures/tuwunel/`                                        | container tests                      | U08           |
| Matrix client (urllib)            | `helpers/pingbus/matrix.py`                                                                                                                                                      | everywhere                           | U09           |
| Sync engine and room trust        | `helpers/pingbus/syncer.py`                                                                                                                                                      | everywhere                           | U10           |
| Hook entry points                 | `helpers/pingbus/hooks.py`                                                                                                                                                       | everywhere (offline)                 | U12           |
| Zipapp builder                    | `helpers/pingbus/bundle.py`                                                                                                                                                      | host, at play time (both plays)      | U13           |
| Team registry and handles (pure)  | `helpers/agent_team/registry.py`                                                                                                                                                 | host                                 | U14           |
| Provisioning executor and CLI     | `helpers/agent_team/provision.py`, `helpers/agent_team/cli.py`, wrapper `files/home/.local/bin/agent-team`                                                                       | host                                 | U15           |
| Room pairs (steward)              | `helpers/agent_team/rooms.py`                                                                                                                                                    | host                                 | U16           |
| Homeserver play                   | `playbooks/imports/optional/common/play-agent-team-bus.yml` + templates under `playbooks/imports/optional/common/templates/agent-team/`                                          | host                                 | U17, U26      |
| Warden logic (pure)               | `helpers/agent_team/commands.py`                                                                                                                                                 | host                                 | U24           |
| Warden executor                   | `helpers/agent_team/warden.py`, wrapper `files/home/.local/bin/agent-team-warden`, unit `files/home/.config/systemd/user/agent-team-warden@.service`                             | host, `systemd --user`, per team     | U25           |
| Claude Code plugin (skill, hooks) | `files/opt/claude-yolo/optional/team-bus/plugin/pingbus/` (`.claude-plugin/plugin.json`, `hooks/hooks.json`, `skills/pingbus/SKILL.md`)                                          | ccy image; section 8 members copy it | U20           |
| ccy opt-in                        | `files/var/local/claude-yolo/claude-yolo`, `lib/common-pure.bash`, `lib/team-bus.bash` (new), `entrypoint.sh`, `Dockerfile`; staging in `playbooks/imports/play-claude-yolo.yml` | host launcher + container            | U21, U22, U23 |
| Desktop viewing play              | `playbooks/imports/optional/common/play-agent-team-desktop.yml`                                                                                                                  | host                                 | U27           |
| User docs and member contract     | `docs/agent-team-bus.md` (+ rows in `docs/ccy.md`, index in `docs/README.md`)                                                                                                    | n/a                                  | U28           |
| Plan scripts                      | this plan's `triage.bash`, `deploy.bash`, `acceptance.bash`                                                                                                                      | host                                 | U00, U18, U29 |

Tests mirror the source: `tests/helpers/pingbus/test_<mod>.py`,
`tests/helpers/agent_team/test_<mod>.py`. The two fake modules are not `test_*`, so the
runner leaves them alone [python §7]; they replay responses recorded from a real Tuwunel by
probe H4, and they enforce the two Matrix rules the design leans on (a state event whose
`state_key` starts with `@` must equal its sender; power levels gate every event type), so
a container test fails the way the real server would. Bash parts of ccy get
`scripts/test-ccy-team-bus.bash` (precedent `scripts/test-ccy-host-hostname.bash`).

### What each play installs, and where

**`play-agent-team-bus.yml`** (optional; not imported by `playbook-main.yml`; depends on the
core `play-systemd-user-tweaks.yml` for linger and fails if the user manager is unreachable
[services §1.5]):

- Asserts `container_engine == 'podman'` (Quadlet and the rootless model need it) [services §2].
- dnf: `tcpdump` and `passt` (for `pasta`). Both exist for `acceptance.bash` (P1, P4); a
  comment in the play says so, so a later YAGNI pass does not remove them (a missing tool
  is an IaC gap).
- Helpers, explicit file list, into `/usr/local/lib/ccy-helpers/helpers/pingbus/` and
  `.../helpers/agent_team/`; wrappers `~/.local/bin/agent-team`, `~/.local/bin/agent-team-warden`.
- Builds the zipapp with `command: argv: [python3, -m, helpers.pingbus.bundle, --out, …]`
  (`chdir: root_dir`) and installs it 0755 at `~/.local/bin/pingbus` and at the published
  artefact path `~/.local/share/pingbus-dist/pingbus`, with the plugin directory beside it
  (`.../pingbus-dist/plugin/pingbus/`) for section 8 members. The published tree is outside
  `~/.local/share/agent-teams/` so the ccy deny list (section 5) needs no exception.
- Image: `tuwunel_version` (with `@see` to the release page and a row in
  `vars/version-pins.yml` naming `matrix-construct/tuwunel`, so
  `scripts/check-pinned-versions.bash` and the `update-versions` skill track it) and
  `tuwunel_image_digest` beside it. Rendered as
  `ghcr.io/matrix-construct/tuwunel:{{ tuwunel_version }}@{{ tuwunel_image_digest }}`.
  Pre-pulled by a `command: argv: [podman, pull, <ref>]` task (`changed_when` on its output,
  the `play-unifi-controller.yml` pattern) before any unit starts.
- Per team in `agent_teams` with `state: present` (untracked host_vars, section 4):
  - Fails if the `server_name` recorded in an existing `team.json` differs from the one about
    to be rendered (Tuwunel cannot change it without wiping the database [tuwunel §2]).
  - Fails if `<subnet>` overlaps any existing podman network's subnet.
  - `~/.config/agent-teams/<team>/` 0700: `team.json` (rendered), `tuwunel.toml` (rendered,
    section 2a), `secrets/registration_shared_secret` (64 random bytes hex, generated once,
    `creates:`, 0600, `no_log`), `secrets/forge_token` (from the vault variable
    `agent_team_forge_token`, 0600, `no_log`).
  - `~/.local/share/agent-teams/<team>/db/` 0700 (Tuwunel's `/var/lib/tuwunel`).
  - Quadlets in `~/.config/containers/systemd/`:
    - `agent-team-<team>.network`: `NetworkName=agent-team-<team>`, `Internal=true`,
      `DisableDNS=true`, `Subnet=<subnet>`, `Options=isolate=true`.
    - `agent-team-<team>.container`: `Image=` the pinned reference, `Pull=never`,
      `ContainerName=agent-team-<team>-hs`, `Network=agent-team-<team>.network` only,
      `IP=<hs_ip>`, `PublishPort=127.0.0.1:<port>:8008`,
      `Environment=TUWUNEL_CONFIG=/etc/tuwunel/tuwunel.toml`, config and secret mounted
      `:ro,Z`, data `:Z`, `HealthCmd=["/usr/bin/tuwunel","--health-check"]`,
      `StopTimeout=300`, `[Service] TimeoutStopSec=330`, `WantedBy=default.target`
      [tuwunel §2, §4]. `Notify=healthy` replaces the readiness poll if probe H3 shows the
      installed Quadlet supports it.
  - daemon-reload (scope user), start, restart on change; otherwise a bounded readiness poll
    of `/_tuwunel/server_version` that also checks the unit is still active
    (`ready-wait-ignores-child-exit`).
  - Asserts with `ss -ltnH` that `<port>` listens on `127.0.0.1` only, never `0.0.0.0`/`::`.
  - Runs `agent-team bootstrap <team>` (`command: argv:`; idempotent; marker lines): admin,
    steward, warden and human accounts; asserts `admin` is the only server admin.
  - Enables `agent-team-warden@<team>.service` (added by U26, after the warden exists, so
    the homeserver part of the play does not wait for the warden).
- Per team with `state: absent`: stops and disables the warden instance and the units,
  removes the Quadlets and daemon-reloads. `~/.config/agent-teams/<team>/` and the data
  directory are kept unless `purge: true`, which removes both and the team's registry.
- `scripts/qa-deployed-drift.bash` gets `EXTRA_PAIRS` for both helper trees.

**`play-agent-team-desktop.yml`** (optional): Element Desktop Flatpak `im.riot.Riot`
(system-wide, the `play-comms.yml` pattern); per team:
`~/.var/app/im.riot.Riot/config/Element-<team>/config.json` (section 9),
`electron-config.json` seeded with `{"spellCheckerEnabled": false}` only when absent, and a
launcher `~/.local/share/applications/agent-team-<team>-element.desktop` running
`flatpak run im.riot.Riot --profile <team>`. A terminal Matrix client is deferred (D24).

**`play-claude-yolo.yml`** (existing, core): stages `files/opt/claude-yolo/optional/team-bus/`
and builds the zipapp itself into the build context (`optional/team-bus/bin/pingbus`) with
the same `helpers.pingbus.bundle` call; removes stale staged files (the warning at
`play-claude-yolo.yml:293-300`) [ccy §3]. It never reads the optional play's output, so a
machine that never runs the bus play still builds the image. The build is reproducible
(D11), so both plays produce identical bytes.

### 2a. `tuwunel.toml`, every key

Rendered from the template; the template test (U17) asserts this exact key set and that
each key appears in Tuwunel's example config for the pinned version.

| Key                               | Value                                     | Why                                                                             |
| --------------------------------- | ----------------------------------------- | ------------------------------------------------------------------------------- |
| `server_name`                     | `<sn>`                                    | Fixed for the team's life (drift check above).                                  |
| `address`                         | `["0.0.0.0"]`                             | Inside the container; the host bind is `PublishPort=127.0.0.1:…` [tuwunel §2].  |
| `port`                            | `8008`                                    | Upstream default; mapped, never changed.                                        |
| `database_path`                   | `/var/lib/tuwunel`                        | The data volume.                                                                |
| `allow_registration`              | `false`                                   | Closed from first boot (D3).                                                    |
| `registration_shared_secret_file` | `/run/secrets/registration_shared_secret` | Admin-API account creation; read on every use.                                  |
| `grant_admin_to_first_user`       | `false`                                   | No account becomes admin by order; a bootstrap retry cannot promote the warden. |
| `allow_federation`                | `false`                                   | Issue §1.                                                                       |
| `trusted_servers`                 | `[]`                                      | No key-server queries (default is `matrix.org`).                                |
| `federate_admin_room`             | `false`                                   | Tidiness; must be set before the admin room exists.                             |
| `admin_escape_commands`           | `false`                                   | No `\!admin` in ordinary rooms.                                                 |
| `allow_encryption`                | `false`                                   | The bus is not end-to-end encrypted; the warden must read commands.             |
| `auto_accept_invites`             | `false`                                   | Members accept invites themselves, after their checks (section 7).              |
| `new_user_displayname_suffix`     | `""`                                      | Display name is exactly the handle.                                             |
| `client_sync_timeout_min`         | `0`                                       | Makes `recv`'s `timeout=0` real [tuwunel §5.6]; probe H4.                       |
| `default_room_version`            | `"12"`                                    | Explicit; room trust depends on v12 creator semantics.                          |
| `sentry`                          | `false`                                   | Explicit, though it is the default.                                             |
| `log`                             | `"warn"`                                  | H4 records that no request header (token) is logged at this level.              |

`max_request_size` is left at its default (24 MiB; Tuwunel refuses to start below 10 MB).
URL-preview allowlists are left at their empty defaults, which fetch nothing; P1 proves it.
`client_sync_timeout_min = 0` lets a member busy-loop `/sync`; on one host that costs only
local CPU, and the member can be removed.

## 3. Interfaces between components

```
 host_vars agent_teams ──play──▶ team.json, tuwunel.toml, Quadlets, warden unit
                                     │
 agent-team (host) ──admin API──▶ Tuwunel ◀──client API── warden (host, 127.0.0.1:<port>)
   │ writes                          ▲  ▲
   │ registry.json, tokens           │  └── Element (host, 127.0.0.1:<port>), control rooms only
   ▼                                 │
 handle + token + member.json ──ro──▶ pingbus in ccy (team network, http://<hs_ip>:8008)
                                     │
                     durable inbox ◀─┘──▶ plugin hooks (offline: inbox counts + waiter status)
```

| Interface                  | Form                                                                                                                                                                                                                                                                                                           |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `team.json` (play → tools) | JSON: `name`, `server_name`, `port`, `network` (`agent-team-<team>`), `subnet`, `hs_ip`, `local_base_url` (`http://127.0.0.1:<port>`), `member_base_url` (`http://<hs_ip>:8008`), `humans` (names), `repos` (objects, section 4), `path_prefixes`, `forge_api`, optional `limits`. Read-only to tools.         |
| `registry.json`            | JSON, written by `agent-team` only (atomic `O_EXCL` temp + rename): `members` by handle (`user_id`, `state` active/removed, `repo_source` remote/dir), `counters` keyed `<repo>+<host>.<type>` → last `<n>`, `rooms` (bus room ID → control room ID, name). Entries are never deleted.                         |
| `member.json`              | JSON, what every pingbus reads (PROTOCOL.md §11). Rendered by `agent-team member-config` from the current `team.json` and registry, never stored as the source of truth, so allowlist and human changes reach every member on its next start.                                                                  |
| token files                | One line, the access token, no trailing newline, mode 0600, created with `O_EXCL` and replaced only by atomic rename.                                                                                                                                                                                          |
| pingbus state dir          | `PINGBUS_STATE_DIR`, default `~/.local/state/pingbus/<team>/<handle>/`: `inbox/` (one JSON file per event ID, note stripped), `consumed/`, `outbox.json`, `sync.json`, `waiter.json`, `forge-cache.json`, `dropped.log` (event ID, sender user ID, reason code, time; nothing else), `lock`.                   |
| agent-team → ccy           | `agent-team add-member <team> --remote-url=<url> --dir-name=<dir> --host=<host> --type=podman --out=<dir>` prints one marker line `MEMBER\t<handle>\t<user_id>\t<network>` on stdout; `agent-team member-config <team> <handle> --where=team-network --out=<dir>` writes `member.json`. Diagnostics on stderr. |
| hooks → pingbus            | `pingbus hook stop`, `pingbus hook prompt`, `pingbus hook session-start`: read the Claude Code hook JSON on stdin, write hook JSON on stdout, never touch the network, print only fixed templates with integers.                                                                                               |
| warden → agents            | Ordinary pings in the bus room with `on_behalf_of` set (PROTOCOL.md §4). The warden mirrors every **valid** bus-room ping into the paired control room as an `m.notice` (section 8).                                                                                                                           |

## 4. Team definitions and accounts

`environment/localhost/host_vars/localhost.yml` (untracked) holds the list; the tracked
`localhost.yml.dist` gets a commented placeholder only:

```yaml
agent_teams:
  - name: <team>                 # [a-z][a-z0-9-]{0,23}
    state: present               # or absent; purge: true also deletes data
    port: <port>                 # host loopback port, unique per team
    subnet: <subnet>             # a /24 used by no other podman network; hs_ip is .10
    server_name: <team>.agent-team.internal   # optional; this is the default
    humans: [<name>]             # [a-z][a-z0-9_-]{0,31}; localpart is the name
    repos:                       # forge repositories pings may reference; required
      - repo: <owner>/<repo>
        branches: [<default-branch>]          # trusted branches; required, non-empty
    path_prefixes: [CLAUDE/Plan/, docs/]      # required; no default
    forge_api: https://api.github.com          # optional; this is the default
```

`agent_team_forge_token` (vault-encrypted, shared by the teams) is what the warden and the
host-side acceptance members use for forge checks.

Accounts, all created by `agent-team` through the admin API with registration closed from
first boot \[tuwunel §3\]:

| Account           | Localpart  | How created                                                                | Credential kept at                                               |
| ----------------- | ---------- | -------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| team admin        | `admin`    | `POST /_synapse/admin/v1/register` with the shared-secret HMAC, admin=true | `~/.config/agent-teams/<team>/admin.token`                       |
| steward           | `steward`  | `PUT /_synapse/admin/v2/users/...` then `.../login` mint                   | `~/.config/agent-teams/<team>/steward.token`                     |
| warden            | `warden`   | same                                                                       | `~/.config/agent-teams/<team>/warden.token`                      |
| each human        | `<name>`   | same, with a generated password for Element logins                         | `~/.config/agent-teams/<team>/humans/<name>.password` (no token) |
| each agent member | `<handle>` | `add-member`: same, random password discarded, display name = handle       | ccy: host-side per-project store (section 5); others: `--out`    |

Agent handles always contain `+`, the reserved and human localparts never do, so they
cannot collide. `remove-member` deactivates the account and marks the registry entry
removed; its `<n>` is never reused. `rotate-token` logs out the member's devices through the
admin API and mints a new token in place (atomic rename); a running ccy session picks it up
on its next launch [ccy §2]. `rotate-token` is the documented response to a P5 hit.

**The team's root of trust is the directory `~/.config/agent-teams/<team>/`, not one file
in it.** The shared secret there can register a new server admin, so the admin token at
rest adds no exposure the secret does not already have. The protection is that the tree is
0700, host-only, and on the ccy deny list (section 5), and that `host` members are trusted
like humans (D7).

`agent-team` commands: `bootstrap <team>`, `add-member`, `member-config`,
`remove-member <team> <handle>`, `rotate-token <team> <handle|steward|warden>`,
`list <team>` (members, state, where each handle's `<repo>` came from; no tokens),
`room create`, `room add-worker`, `room list` (section 7). Every command takes arguments
only (`--opt=value` form for every value that comes from outside, so a value starting with
`-` cannot be read as an option), validates them before any call, and never prompts.

## 5. ccy opt-in

- **Setting:** `ccy --team <team>` (persisted) and `ccy --no-team` (clears). The source of
  truth is the host-only file `~/.claude-tokens/ccy/projects/<hash>/team` holding the team
  name; restart and reboot-restore read it, so they need no change [ccy §1.3, §2]. It is
  never in `.claude/ccy/ccy.env` (read too late) or the tracked mounts file.
- **One team seat per checkout** (D8): at most one running container per checkout may carry
  `--team`; the launcher refuses a second (labels `ccy-team=<team>`,
  `ccy-team-project=<hash>`). The seat's handle is created on the first `--team` launch and
  reused by every later session in that checkout, restarts and restores included. A handle
  therefore names a checkout's seat on the team, not one conversation (owner question 5).
  `ccy --team <team> --new-handle` retires the old handle (`remove-member`) and allocates the
  next `<n>`, for when a new piece of work should not inherit the old identity. Stale
  replays are cut off by the receive rules (PROTOCOL.md §8, `stale`).
- **Handle:** built on the host by `agent-team add-member` from the remote URL's repository
  basename (fallback: the directory name), `CCY_HOST_HOSTNAME`, and `CONTAINER_ENGINE`
  (PROTOCOL.md §3 gives the grammar and the mapping). The launcher validates both inputs on
  the host and passes them in `--opt=value` form. The remote URL comes from the checkout's
  `.git/config`, which the container can write, so `agent-team list` shows each handle's
  source and `room create` refuses a handle that is not `active`; humans pick handles.
- **Credential store:** `~/.claude-tokens/ccy/projects/<hash>/team-bus/<team>/` (`handle`,
  `token`), inside the existing 0700 tree. Only these two persist.
- **Each launch:** `agent-team member-config` renders a fresh `member.json`; the launcher
  copies it and the token with `install -m 0600` into
  `mktemp -d "$XDG_RUNTIME_DIR/ccy-team.<container>.XXXXXX"` and mounts that `:ro,Z` (`:ro`
  without SELinux) at `/etc/pingbus`. Cleanup removes the copy; each launch also sweeps
  staging directories whose container no longer exists.
- **Deny list** (`lib/common-pure.bash`): add `~/.config/agent-teams`,
  `~/.local/share/agent-teams` and `~/.config/pingbus`. Also refuse any mount source that is
  an **ancestor** of a denied path (for example `~/.config` or `~/.local`). Today only `/`
  and the home directory are refused as ancestors, so `~/.config` would expose
  `~/.config/gh` as well; the ancestor rule closes that for every entry.
- **Environment** (`-e`, values are paths and names, never the token):
  `PINGBUS_CONFIG=/etc/pingbus/member.json`,
  `PINGBUS_STATE_DIR=/workspace/.claude/ccy/pingbus/<handle>` (host-persisted, git-ignored by
  `.claude/ccy/*`), `PINGBUS_HANDLE=<handle>` (informational; the CLI checks it equals
  `member.json`). The session's existing `GH_TOKEN`, if any, serves the forge check on
  github.com; without one, only public repositories resolve.
- **Network:** the session keeps its primary network (`podman` or the project network);
  the team network is attached as a second network. Because the team network has DNS off,
  the container's `resolv.conf` is unchanged (probe H1 confirms) and pingbus reaches the
  homeserver by `<hs_ip>`, so no name on another network can impersonate it. The team
  network is kept out of `SELECTED_NETWORK`, the internet preflight, `ensure_network_dns`,
  `save_network_preference`, `LAST_NETWORK` and `save_launch_config`; `--disconnect` refuses
  it [ccy §1]. Attach method: a second `--network` at `run` if probe H1 passes, else
  `network connect` right after start, before the entrypoint reaches `claude` (precedent
  `network-management.bash:465`).
- **Image:** `optional/team-bus/` (zipapp + plugin) copied by the existing `optional/` copy;
  the Dockerfile adds a `chmod 0755` for the bin. The entrypoint, when `PINGBUS_CONFIG` is
  set: symlinks `/usr/local/bin/pingbus`; installs the plugin by the route probe U01 chose
  (section 6). When it is not set: removes the plugin copy only if its `plugin.json` `name`
  is the shipped one, and drops the `enabledPlugins` key (the child-claude lesson) [ccy §4].
- **Version gate:** the launcher refuses `--team` when the image's `claude-yolo-version`
  label is older than the version that adds team support.
- **Engines:** v1 supports `podman` members only. A `docker` ccy session with `--team` is
  refused at launch with a message (Docker cannot see a rootless podman network) [ccy §1].
- **Versions:** U21–U23 are stacked on one branch and merged together with one minor
  `CCY_VERSION` bump, one container version bump (`LABEL` and `REQUIRED_CONTAINER_VERSION`
  together) and one `docs/ccy-changelog.md` entry; two rows in the "What the container CAN
  reach" table in `docs/ccy.md` (team credential, team network).

## 6. Waking sessions [wake]

- **Primary:** the agent runs `pingbus wait` with `run_in_background` (or under Monitor).
  `wait` holds the account lock, long-polls `/sync` in chunks of 30 s, writes `waiter.json`
  (`listening_until`) and the room `<ns>.status` state, and exits on the first batch that
  yields at least one valid ping or `TIMEOUT` (exit 0, one line each) or at `--timeout`
  (default 1500 s, exit 3, "re-arm"). A batch of drops alone never ends `wait`: drops go to
  `dropped.log` and one aggregated stderr line per batch, so a flooding member cannot keep
  waking its peers. The harness re-invokes the agent on exit; the skill tells it to handle
  the pings then re-arm.
- **Guard:** the plugin's Stop hook (`pingbus hook stop`) reads only the local inbox and
  `waiter.json`. It re-validates every inbox file (PROTOCOL.md §8, offline steps) and
  ignores any that fail, so a file planted by repository code cannot put text in front of
  the agent. It blocks the stop once, with a fixed-template reason carrying only a count
  ("2 pings pending: run `pingbus recv`", "no waiter armed: run `pingbus wait` in the
  background"), when valid pings are pending or no live waiter is recorded; it never blocks
  when `stop_hook_active` is true, and blocks for "no waiter" at most once per 600 s, so it
  cannot loop against the hooks daemon's own Stop handlers. A broken CLI or missing config
  produces a fixed template naming the failure class (never exception text) under the same
  once-only rule.
- **Context:** `pingbus hook prompt` (UserPromptSubmit) adds "N pings pending: run
  `pingbus recv`"; it never prints ping lines. `pingbus hook session-start` runs
  `config check` and reminds the agent to arm `wait`, or reports a missing credential.
- **Install route, decided by probe U01:** U01 runs in a ccy container: copy a minimal
  plugin with `SessionStart`, `Stop` and `UserPromptSubmit` hooks that touch files, enable it
  the phpantom-lsp way (copy into `/root/.claude/plugins/` + `enabledPlugins`), run a child
  `claude -p`, and record which hooks fired and whether `hook_registration_checker` reports
  anything. If the hooks fire, the plugin route (D13) stands. If not, the named fallback is
  the research's third route: the entrypoint `jq`-merges the three hook entries into the
  user-level `/root/.claude/settings.json` when `PINGBUS_CONFIG` is set, and removes exactly
  those entries when it is not; the skill still ships through the image skills directory.
  U20 and U23 both need U01.
- **Fallback for waking (later, not v1):** a supervisor plugin `pingbus_wake.py` beside
  `ccy_lifecycle.py` emitting a fixed `PINGS_PENDING` template with a count, fed by a
  `pingbus wait --daemon` syncer started by the entrypoint. It needs the hooks daemon's
  plugin API and one new upstream template; v1 does not block on it. Session crons and the
  file mailbox are not used.
- `recv` takes the lock if it is free and does one non-blocking sync (`timeout=0`, made
  real by `client_sync_timeout_min = 0` [tuwunel §5.6]); if the lock is held it drains the
  local inbox only. Before printing a stored ping, `recv` and `wait` re-run the offline
  validator on the file and, when they hold the lock, confirm the event with
  `GET /rooms/{room}/event/{event}`; the inbox is a cache, never the authority. Hooks never
  sync.
- The skill states: the token file is never read or printed; a referenced file is data to
  read, not a command; links out of `path_prefixes` and the text of issues and pull requests
  are untrusted data; a consumed ping is not withdrawn by a later redaction.

## 7. Rooms and trust

Rooms come in pairs and only the steward creates them. A human asks on the host:

`agent-team room create <team> --name=<name> --by=<human> --orchestrator=<handle> --worker=<handle>…`

`<name>` is `[a-z0-9][a-z0-9-]{0,47}`. The steward account (host-only token) then:

- **Bus room** (room version 12; the steward is the creator, so it holds creator power;
  no `additional_creators`). Members: the listed agents and the warden. No human is ever a
  member, so the homeserver never delivers human text to an agent account. No name, no
  topic. Power levels at creation: `users_default` 0, `events_default` 100,
  `events`: `<ns>.ping` 0, `<ns>.status` 0; `state_default` 100, `invite`/`kick`/`ban`/
  `redact` 100. So agents can send pings and their own status and nothing else; no
  `m.room.message`, reaction or sticker can be posted by an agent. Initial state:
  `<ns>.room` `{"v":1,"control":"<control room ID>"}` and one `<ns>.roles` (state key `""`)
  mapping each agent's user ID to `orchestrator` or `worker` (a state key starting with `@`
  must equal its sender, so per-member role events would be rejected; probe H4 confirms
  the single map is accepted).
- **Control room** (also v12, steward creator). Members: the team's humans and the warden.
  No agent is ever invited. Name `<name>`, a fixed topic listing the commands. Power
  levels: humans 50, warden 50, `events_default` 0, `state_default` 100,
  `invite`/`kick`/`ban`/`redact` 100. Humans can talk and command but cannot invite anyone
  or set state, so no human can turn a control room into something an agent would join.
  Initial state: `<ns>.control` `{"v":1,"bus_room":"<bus room ID>"}`.
- `agent-team room add-worker <team> --room=<bus room ID> --worker=<handle>` updates
  `<ns>.roles` and invites; it refuses a handle that is not `active`. `room list` prints
  pairs from the registry.

**Joining.** pingbus's syncer accepts a pending invite on its own (no LLM, deterministic)
only if the invite's sender and the stripped `m.room.create` sender are the configured
steward. After joining it re-reads `m.room.create?format=event` (sender steward, room
version 12), `m.room.power_levels` (exactly the levels above), `<ns>.room` and `<ns>.roles`
(both sent by the steward, and listing itself), and leaves on any mismatch. Any other
invite is rejected and logged by room ID only. The warden follows the same rule for both
kinds of room. A room made in Element lacks the markers and a steward creator, so no
member ever acts in it. `pingbus room list` prints only room IDs, roles and membership,
never names, topics or reasons; `room leave` is explicit.

**What agents receive.** The sync filter asks for the bus rooms' `<ns>.ping`,
`<ns>.roles`, `<ns>.room`, `<ns>.status`, `m.room.create`, `m.room.power_levels` and
`m.room.member` only, with presence, account data, ephemeral events and to-device messages
excluded; the syncer ignores those sections even if the server sends them. Display names
are never read or printed.

## 8. The warden

Host Python, `agent-team-warden@<team>.service`, standard library only, token from
`warden.token`, base URL `local_base_url`. The unit has `Requires=` and `After=` on
`agent-team-<team>.service` and `Restart=on-failure` with `StartLimitBurst=5` in
`StartLimitIntervalSec=300`, so a homeserver that stays down leaves a visible failed unit.
The forge token reaches it as `Environment=PINGBUS_FORGE_TOKEN_FILE=%h/.config/agent-teams/%i/secrets/forge_token`
(a path, never the token, so `systemctl --user show` reveals nothing). State in
`~/.local/state/agent-teams/<team>/warden/`, using pingbus's own inbox and outbox code.

Pure decision function in `commands.py`:
`(event, control room → bus room, roles, sender) -> Ping | Reply | Ignore`. Rules (all
tested as a table):

- It reads `m.room.message` (`msgtype` `m.text`) in **control rooms** only, from configured
  humans only; everything from the server user (`@conduit:<sn>`), the steward and itself is
  ignored. Edits (`m.relates_to.rel_type == "m.replace"`), redactions and replies with no
  valid command get the command list.
- Grammar: PROTOCOL.md §15. Targets are agent handles (or their full user IDs) written in
  the message, each checked against the handle grammar and against the paired bus room's
  `<ns>.roles`; any `m.mentions.user_ids` present must equal the parsed targets, else the
  command list (fail closed). Anything that is not exactly a command: fixed command-list
  reply, nothing sent.
- `!halt all` → every worker in the bus room; targets → those accounts, each must hold a
  role or the reply names the problem and nothing is sent; no target and no `all` → the bus
  room's orchestrator; `!status` and `!help` are answered by the warden from validated
  `<ns>.status` fields (state enum and time only; stale "listening" shown as stale).
- `<ref>` passes the same `protocol.py` validation and the same forge and provenance check
  (`forge.py`) as a pingbus send; a refusal names the validator's reason code.
- Each accepted command becomes one ping in the bus room with `on_behalf_of` = the human,
  then a reply naming the full user IDs it went to ("halt sent to @a…, @b…"). Per-human
  limit: PROTOCOL.md §9.
- **Delivery:** warden pings go through the outbox; each `TIMEOUT` becomes a control-room
  notice naming the silent target's user ID, so a human learns when a halt was not
  received.
- **Mirror:** every bus-room ping that passes the full receive validation becomes one
  control-room notice: sender and target user IDs, verb, ref, `re`, and, if the ping
  carried a note, the note marked "untrusted agent note". Invalid pings are not mirrored.
- **Flood alert:** a sender over `recv_per_sender_minute` gets one control-room notice per
  minute naming its user ID, so a human can `remove-member`.

## 9. Element profiles

The profile `config.json` is the one in [clients §1.4] with `base_url`
`http://127.0.0.1:<port>`: pinned homeserver, `disable_custom_urls`,
`enable_client_well_known_lookups: false`, identity server, integrations, Jitsi (pointed at
`127.0.0.1` because the object cannot be nulled), Element Call, maps, posthog, sentry,
rageshake, URL previews, room directory and update URL all switched off explicitly, because
the bundled element.io config fills any omitted key [clients §1.3]. A container test (U27)
asserts the rendered file holds every key in [clients §1.4]. The launcher is plain
`flatpak run`; confinement is not attempted (D25). The Flatpak is not pinned; P4 records
the installed version with its result.

## 10. Privacy acceptance checks and how each is proven

All in this plan's `acceptance.bash`, **host only**, against a dedicated acceptance team:
reserved name `acceptance`, its own `agent_teams` entry with `--type host` members, a fixed
public reference in this repository on its allowlist, created by `deploy.bash` through the
play and removed with `state: absent, purge: true` at the end, so no real team gains
accounts. Each check prints PASS/FAIL with its evidence file under `untracked/plan-runs/`.

| Id  | Check                                     | Proof                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| --- | ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1  | No outbound traffic from the homeserver   | Construction: `podman network inspect` shows the team network `internal: true`, `dns_enabled: false`, and the container's only network is it. Behaviour: `tcpdump -Z root` in the container's network namespace (`podman unshare nsenter -t <pid> -n`) for a scripted session (bootstrap, room create, join, 20 pings, ack, warden command); FAIL on any packet to an address other than members on the team subnet, and on any DNS query at all.                                                                                                                                                                                                                                       |
| P2  | Not reachable from the network            | `ss -ltnH`: `<port>` bound on `127.0.0.1` only; `net.ipv4.ip_forward` recorded; `net.ipv4.conf.*.route_localnet` all 0 (FAIL otherwise); `firewall-cmd --get-active-zones` and the zone of every podman bridge interface recorded, FAIL on a zone with `<port>` open; a `--no-network` (pasta) ccy session cannot reach `<port>`. Off-machine: from a vm-test-lab VM, with a route to the host added, `curl` to every host address on `<port>` must fail. If no VM is available the check reports SKIPPED-NEEDS-OWNER and the plan cannot close.                                                                                                                                        |
| P3  | Names are not published                   | `server_name` ends in `.internal` (reserved, never delegated); `getent ahosts <sn>` fails; a host `tcpdump` on port 53 during the P1 and P4 sessions shows no query for `<sn>` or `<team>`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| P4  | Element team profile talks only to its HS | The scripted Element session (start, log in, open the control room, receive a mirror notice, idle 120 s) runs as `pasta --pcap <file> -T <port> -- flatpak run im.riot.Riot --profile acceptance`, so every packet in the capture came from that profile and nothing else on the desktop. FAIL on any packet other than the forwarded connection to `<port>`. Plus the static key check of section 9.                                                                                                                                                                                                                                                                                   |
| P5  | Tokens never reach logs                   | For every secret (admin, steward and warden tokens, member tokens, the shared secret, human passwords, the forge token), with trailing newlines stripped into a temporary pattern file, `grep -rqF -f` over: the deploy run logs, `untracked/plan-runs/`, `journalctl --user` for the team units, `podman inspect` and `podman logs` of the homeserver, `systemctl --user show` of the warden, every pingbus stdout/stderr captured during the session, the ccy transcript and state trees of the sessions used in U30, shell history, the Element profile's logs. Report only the file name of a hit, never the line. Also `test_cli.py` asserts tokens on neither stream (container). |
| P6  | Human text never reaches an agent account | During the scripted session a human posts free text and an invalid command in the control room; then the raw `/sync` of each agent account (not pingbus output) is captured and FAIL on any `m.room.message`, any room the account is joined to that lacks the bus marker, or any human user ID as a member of a bus room.                                                                                                                                                                                                                                                                                                                                                              |
| P7  | The admin register endpoint holds         | A request to `/_synapse/admin/v1/register` from a member container with a wrong MAC is refused. Member-to-member reachability on the team network is recorded (known limitation, D4).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |

## 11. Where each thing can be verified

| Container (this checkout, no podman)                                                                                                                                                                                                                 | Host only                                                                                                                                  |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| U02–U16, U19, U24, U25 entirely, against the fakes built from H4's recorded responses: validator, limits, config, CLI (every exit code), inbox, forge (injected opener), matrix client, syncer, hooks, zipapp, registry, provisioning, rooms, warden | Rootless podman: Quadlet generation (H3), internal network with DNS off, publish on `127.0.0.1` (H2), second network on a ccy session (H1) |
| `ruff`, `qa-helper-tests.bash`, `qa-python.bash`, `bash -n` and `scripts/test-ccy-team-bus.bash` for pure bash                                                                                                                                       | Tuwunel itself: every admin and client flow the design uses (H4), then M1 for real                                                         |
| Quadlet and `tuwunel.toml` template render tests (every key in the documented key lists)                                                                                                                                                             | Element: profile path in the Flatpak, plain HTTP to loopback, spell-check seed honoured, pasta capture (H5), P1–P7                         |
| Plugin hook loading (U01, child `claude -p` in a ccy container); plugin JSON shape test                                                                                                                                                              |                                                                                                                                            |

## 12. Build order

Each unit is one agent, on its own branch, tests first (the TDD hook enforces it), except
U21–U23, which stack on one ccy branch (section 5). A unit lists the units it needs; units
with no path between them run in parallel. "C" = fully verifiable in the container, "H" =
needs a host run through `meta-deploy.bash`.

| Id  | Title                            | Creates / changes                                                                                                                                                        | Tests                                                                                                                                                                                                                                                                                                                                               | Needs              | Where |
| --- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ----- |
| U00 | Host probes                      | `triage.bash` (read-only probes H1–H6, section 13); scrubbed Tuwunel responses under the run log for U08                                                                 | the script itself; results journalled                                                                                                                                                                                                                                                                                                               | –                  | H     |
| U01 | Plugin hook loading probe        | a throwaway plugin under `untracked/`; result journalled; decides the D13 route                                                                                          | child `claude -p` in a ccy container: which hooks fired; checker output                                                                                                                                                                                                                                                                             | –                  | C     |
| U02 | Protocol spec and pure validator | `docs/agent-team-bus-protocol.md` (from `PROTOCOL.md`), `helpers/pingbus/protocol.py`                                                                                    | `test_protocol.py`: table-driven, every verb × ref form (allowed and refused), ref grammar edges incl. segment limits, note boundaries and every forbidden char, handle grammar, ID grammar, schema, every drop reason code; `test_protocol_doc.py`: doc tables == constants                                                                        | owner Q1           | C     |
| U03 | Limits                           | `helpers/pingbus/limits.py`                                                                                                                                              | `test_limits.py`: token bucket, duplicate window, receive flood, ack deadlines, stale age, overrides outside bounds refused; injected clock                                                                                                                                                                                                         | U02                | C     |
| U04 | Member config                    | `helpers/pingbus/config.py`                                                                                                                                              | `test_config.py`: schema, token file mode > 0600 refused, plain HTTP only to loopback or `local_hosts`, empty `repos`/`branches`/`path_prefixes` refused, `PINGBUS_HANDLE` mismatch refused, forge token source rules (fixtures use `192.0.2.1`, `server.test`)                                                                                     | U02                | C     |
| U05 | CLI offline parts                | `helpers/pingbus/cli.py` (argparse with `SystemExit` remap, `EXIT_*`, line formatter, `validate`, `config check`, `version`)                                             | `test_cli_offline.py`: subprocess runs for 0, 4, 64, 78; stdout/stderr split; exit-code table == doc                                                                                                                                                                                                                                                | U02, U04           | C     |
| U06 | Inbox, outbox, lock              | `helpers/pingbus/inbox.py`                                                                                                                                               | `test_inbox.py`: event ID validated before it names a file, dedupe, atomic writes, note never stored, consume/peek, re-validation on read, sync token saved only after inbox fsync, second locker gets busy, outbox ack tracking and TIMEOUT emission                                                                                               | U02, U03           | C     |
| U07 | Forge check and provenance       | `helpers/pingbus/forge.py`                                                                                                                                               | `test_forge.py` with injected opener: each ref form resolves; 404; issue-that-is-a-PR; path is a directory; SHA not reachable from a trusted branch; PR from a fork or with a disallowed author; PR head moved; rate limit → `forge-rate`; credential set unredirected; redirects refused cross-host; GitHub token only for `api.github.com`; cache | U02, U04           | C     |
| U08 | Fake homeserver                  | `tests/helpers/pingbus/fake_client_api.py`, `fake_admin_api.py`, `fixtures/tuwunel/` (from H4, scrubbed)                                                                 | `test_fakes.py`: replays every recorded flow; rejects an `@` state key not equal to its sender; enforces power levels; full client and admin surface defined here, so later units only consume it                                                                                                                                                   | U00                | C     |
| U09 | Matrix client                    | `helpers/pingbus/matrix.py`                                                                                                                                              | `test_matrix.py`: bearer header unredirected, no proxy, no redirects followed, txn ID reuse on retry, 401/403/429 (`Retry-After`, `retry_after_ms`) mapping, token absent from every error string                                                                                                                                                   | U04, U08           | C     |
| U10 | Sync engine and room trust       | `helpers/pingbus/syncer.py`                                                                                                                                              | `test_syncer.py` vs fakes: filter, first sync takes `next_batch` only, limited sync gap fill via `/messages`, invite checks and auto-join, post-join verification and leave, receive pipeline incl. forge and `stale`, drops logged minimally, status state written                                                                                 | U06, U07, U09      | C     |
| U11 | CLI core: send, recv, wait       | `cli.py`: `send`, `recv`, `wait`                                                                                                                                         | `test_cli.py`: real CLI vs fakes; one test per exit code these commands produce; stdout carries only `PING`/`TIMEOUT`/`SENT` lines; drops only as the aggregate stderr line; `wait` not ended by drops alone; token on neither stream                                                                                                               | U05, U07, U10      | C     |
| U12 | Hook subcommands                 | `helpers/pingbus/hooks.py`, one dispatch line in `cli.py`                                                                                                                | `test_hooks.py`: block once on pending, no-waiter throttle, `stop_hook_active` never blocks, planted inbox file with free text never printed, fixed templates only, no network access (fakes fail the test if contacted)                                                                                                                            | U06, U11           | C     |
| U13 | Zipapp                           | `helpers/pingbus/bundle.py`                                                                                                                                              | `test_bundle.py`: byte-identical rebuild, every module in the package included, archive runs `version` and `validate`                                                                                                                                                                                                                               | U05                | C     |
| U14 | Registry and handles             | `helpers/agent_team/registry.py`                                                                                                                                         | `test_registry.py`: repo name from remote URL forms and fallback, source recorded, lowercasing and mapping, `<n>` never reused after remove, rooms map, round trip                                                                                                                                                                                  | U02                | C     |
| U15 | Provisioning CLI                 | `helpers/agent_team/provision.py`, `cli.py`, `files/home/.local/bin/agent-team`                                                                                          | `test_provision.py` vs fake admin API: HMAC matches Synapse's construction, bootstrap idempotent and asserts a single server admin, add/remove/rotate, `member-config` reflects a changed `team.json`, `--opt=value` parsing, files 0600 via `O_EXCL`, marker lines, no token on any stream                                                         | U09, U14           | C     |
| U16 | Room pairs                       | `helpers/agent_team/rooms.py`; `agent-team room create/add-worker/list`; `pingbus room list/leave` in `cli.py`                                                           | `test_rooms.py` vs fakes: both rooms' power levels and initial state exactly as section 7, no human in the bus room, no agent in the control room, refuses non-active handles; `room list` prints IDs only                                                                                                                                          | U10, U15           | C     |
| U17 | Homeserver play                  | `play-agent-team-bus.yml` (helpers, zipapp, per-team HS, bootstrap, `state: absent`/`purge`), templates, `localhost.yml.dist` placeholder, drift pairs, version pin rows | template render tests (Quadlet key lists, `tuwunel.toml` key set); host: deploy, unit active, `ss` assert, bootstrap markers, removal of a scratch team                                                                                                                                                                                             | U00, U13, U15      | C + H |
| U18 | M1: host-to-host ping            | `acceptance.bash` first slice                                                                                                                                            | host: two `--type host` members of the acceptance team, `room create`, `review` sent, received by `wait`, `ack` answered, `TIMEOUT` when unanswered                                                                                                                                                                                                 | U11, U16, U17      | H     |
| U19 | CLI report commands              | `cli.py`: `inbox`, `show`, `status`, `peers`, `tail`                                                                                                                     | `test_cli_reports.py`: every printed field is grammar-validated; no names, topics, display names or notes                                                                                                                                                                                                                                           | U11                | C     |
| U20 | Plugin and skill                 | `files/opt/claude-yolo/optional/team-bus/plugin/pingbus/**` (or the fallback hook fragment, per U01)                                                                     | `test_plugin_contract.py`: `hooks.json` parses, each command is a real `pingbus hook` subcommand; SKILL.md names only real commands and carries the section 6 rules                                                                                                                                                                                 | U01, U12           | C     |
| U21 | ccy: opt-in, seat and credential | `claude-yolo`, `lib/team-bus.bash`, `lib/common-pure.bash` (deny list + ancestor rule), version bumps, changelog                                                         | `scripts/test-ccy-team-bus.bash`: flag parse and persistence, one seat per checkout, `--new-handle`, staging argv and sweep, denied paths and ancestors refused, image label gate, docker refusal                                                                                                                                                   | U15                | C     |
| U22 | ccy: team network                | `lib/team-bus.bash`, `network-management.bash` exclusions                                                                                                                | `test-ccy-team-bus.bash`: team network never in saved prefs, preflight or DNS fix-up, `--disconnect` refuses it; host: H1 shape                                                                                                                                                                                                                     | U00, U21           | C + H |
| U23 | ccy: image and entrypoint (M2)   | `Dockerfile`, `entrypoint.sh`, `play-claude-yolo.yml` (stages plugin, builds zipapp)                                                                                     | entrypoint gate in `test-ccy-team-bus.bash` (install/remove by name, symlink); host: image build; two ccy sessions in different projects exchange `review` and `ack`, the idle one woken                                                                                                                                                            | U13, U20, U22      | C + H |
| U24 | Warden logic                     | `helpers/agent_team/commands.py`                                                                                                                                         | `test_commands.py`: every rule in section 8 and PROTOCOL.md §15 as a table, incl. mention mismatch and per-human limit                                                                                                                                                                                                                              | U02, U03           | C     |
| U25 | Warden executor                  | `helpers/agent_team/warden.py`, wrapper, `agent-team-warden@.service`                                                                                                    | `test_warden.py` vs fakes: human free text never produces a ping; commands do; only valid pings mirrored, IDs only, note marked; TIMEOUT notice; invite rule; flood alert; forge token read from the file path                                                                                                                                      | U06, U07, U10, U24 | C     |
| U26 | Warden in the play (M3)          | `play-agent-team-bus.yml`: warden unit per team                                                                                                                          | host: warden active; `!halt` from a control room reaches the target as a `halt` ping                                                                                                                                                                                                                                                                | U17, U25           | H     |
| U27 | Desktop play                     | `play-agent-team-desktop.yml`                                                                                                                                            | `config.json` key test (container); host: profile dir resolves, launcher works                                                                                                                                                                                                                                                                      | U00, U17           | C + H |
| U28 | Docs and member contract         | `docs/agent-team-bus.md`, `docs/ccy.md` rows, `docs/README.md` index                                                                                                     | `qa-docs.bash`                                                                                                                                                                                                                                                                                                                                      | U23, U26, U27      | C     |
| U29 | Deploy and acceptance (M4)       | `deploy.bash`, `acceptance.bash` (P1–P7 and success criteria), `meta-deploy.bash` entry                                                                                  | host run                                                                                                                                                                                                                                                                                                                                            | U18, U23, U26, U27 | H     |
| U30 | Review and end-to-end            | fixes only                                                                                                                                                               | `qa-all.bash` (coordinator), `qa-reviewer`; host: the PLAN.md success criteria                                                                                                                                                                                                                                                                      | all                | H     |

Parallel waves: {U00, U01, U02} → {U03, U04, U08, U14} → {U05, U06, U07, U09, U24} →
{U10, U13, U15} → {U11, U16, U17, U21, U25} → {U12, U18, U19, U22, U26, U27} →
{U20} → {U23} → {U28, U29} → {U30}.

### Milestones

| Milestone                  | Units            | Proven by                                                      |
| -------------------------- | ---------------- | -------------------------------------------------------------- |
| M0 probes                  | U00, U01         | journalled probe results; fixtures recorded                    |
| M1 host-to-host ping       | U02–U11, U13–U18 | U18 on the host: a real Tuwunel, two members, `review` + `ack` |
| M2 ccy-to-ccy ping         | U12, U19–U23     | U23 on the host: two ccy sessions, the idle one woken          |
| M3 warden and control room | U24–U26          | U26 on the host: `!halt` from Element reaches its target       |
| M4 desktop and acceptance  | U27–U30          | P1–P7 and the PLAN.md success criteria                         |

PLAN.md's Phase 2–3 tasks are to be replaced by these milestones (each listing its units)
so plan state can follow merges.

## 13. Probes (U00 host, U01 container; read-only)

- **H1** podman accepts two `--network` flags at `run`; a ccy-image container on `podman`
  plus a test `--internal` network with DNS off prints its `resolv.conf` (unchanged),
  resolves and reaches the Claude API host and `api.github.com`, and reaches a peer on the
  internal network by fixed IP.
- **H2** a container on an `--internal` network with `PublishPort=127.0.0.1:<p>:8008` is
  reachable from the host on `127.0.0.1:<p>`. **If not**, the fallback keeps "no route out":
  the homeserver stays on the internal network only, and a second container
  `agent-team-<team>-gw` (a socat image pinned by version and digest) sits on the internal
  network and on a publishing network and forwards `127.0.0.1:<port>` to `<hs_ip>:8008`.
  Only the forwarder has a publishing network; P1 still runs in the homeserver's namespace.
- **H3** `quadlet -dryrun -user` on rendered samples accepts every key used:
  `NetworkName=`, `Internal=`, `DisableDNS=`, `Subnet=`, `Options=`, `IP=`, `Pull=`,
  `StopTimeout=`, `Environment=`, and whether `Notify=healthy` is supported.
- **H4** a throwaway Tuwunel at the pinned version with the section 2a config: it starts;
  then `curl` runs every call the design makes and saves the response bodies (tokens and IDs
  scrubbed): shared-secret register (and a wrong MAC refused), `PUT v2/users`,
  `users/{id}/login`, the single-admin query, `createRoom` v12 with the section 7 power
  levels and `<ns>.roles` initial state (and a per-member `@` key refused), invite and the
  stripped state the invitee sees, join, send of the ping type, `/sync` with the filter and
  `timeout=0`, a limited sync and `/messages`, `GET /event`. Records whether any header is
  logged at `log = "warn"`.
- **H5** Element under `pasta --pcap <file> -T <port> -- flatpak run im.riot.Riot --profile <x>`:
  the Flatpak process keeps pasta's network namespace, the capture holds its traffic, and
  the profile's `config.json` path resolves as section 9 expects.
- **H6** the homeserver container on a DNS-off internal network has no working resolver
  (a lookup from inside fails without a packet leaving the namespace).
- **U01** (container) plugin hook loading, as section 6 describes.

## Decisions

| #   | Decision                                                                                                                                            | Reason                                                                                                                                                                                                                                                                                                                                                 |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| D1  | Teams are declared in untracked host_vars and the play renders, and removes, all per-team system state                                              | IaC rule: system changes go through Ansible; host_vars is already untracked, so nothing team-specific is committed.                                                                                                                                                                                                                                    |
| D2  | `agent-team` handles accounts and rooms only (runtime data), never units                                                                            | Accounts are application data like ccy's own per-project state; the issue's `create` becomes the play.                                                                                                                                                                                                                                                 |
| D3  | Registration closed from first boot; accounts via shared-secret register and the admin API; `grant_admin_to_first_user = false`                     | Removes the "first account is admin" race and keeps passwords out of the admin room and logs.                                                                                                                                                                                                                                                          |
| D4  | One `--internal` podman network per team, DNS off, fixed subnet, homeserver at a fixed IP, published on `127.0.0.1` only                            | No outbound route or resolver by construction; members reach it by IP, so no other network's name can intercept the token. Members on one team network can reach each other, as on today's shared `podman` bridge; per-member isolation is deferred (it needs per-member networks connected at runtime to a Quadlet container, which a restart drops). |
| D5  | `server_name` defaults to `<team>.agent-team.internal`, and never changes for a team                                                                | `.internal` is reserved and never delegated; Tuwunel cannot change it without wiping data.                                                                                                                                                                                                                                                             |
| D6  | Plain HTTP only to loopback or the member config's `local_hosts`; HTTPS otherwise                                                                   | The issue's host-local rule without guessing from `is_private`, which would bless LAN addresses.                                                                                                                                                                                                                                                       |
| D7  | v1 members: rootless podman containers (ccy or not) and the host; a `host` member is trusted like a human; docker, lxc and vm deferred              | A host member can read the team's root of trust; the others cannot join a rootless podman network without a host-address publish.                                                                                                                                                                                                                      |
| D8  | One team seat per ccy checkout; the handle names the seat; `--new-handle` retires it                                                                | ccy's container slot is the first free name, so keying on it would hand one session's identity to another.                                                                                                                                                                                                                                             |
| D9  | Opt-in persisted in `~/.claude-tokens/ccy/projects/<hash>/team`, set by `--team`/`--no-team`                                                        | It must be read on the host before the container exists; restart and restore inherit it.                                                                                                                                                                                                                                                               |
| D10 | Handle built on the host and passed in; the CLI only checks it                                                                                      | The container cannot see the engine; one builder means one grammar.                                                                                                                                                                                                                                                                                    |
| D11 | pingbus ships as one reproducible zipapp built from `helpers/pingbus/`, by the bus play and by the ccy play independently                           | One validator everywhere; byte-identical builds keep drift checks honest; the core play never depends on the optional one.                                                                                                                                                                                                                             |
| D12 | Registry and member config are JSON                                                                                                                 | The standard library cannot write TOML.                                                                                                                                                                                                                                                                                                                |
| D13 | Wake: background `wait` plus a Stop-hook guard, shipped as a Claude Code plugin unless U01 shows plugin hooks do not load, then as user-level hooks | Works in any session with or without the hooks daemon; the route is chosen by a probe before anything depends on it.                                                                                                                                                                                                                                   |
| D14 | Hooks never sync and print only counts; one syncer per account by `flock`                                                                           | Hooks stay fast and offline and cannot replay planted text; the issue's one-syncer rule holds.                                                                                                                                                                                                                                                         |
| D15 | The note is stripped before the inbox write; only humans see it, in the control-room mirror, marked untrusted                                       | The issue allows a note; this keeps the only free-form field out of every agent's reach.                                                                                                                                                                                                                                                               |
| D16 | Rooms are created in pairs by the host-only steward account at a human's request; agents act only in steward-created rooms with the markers         | v12 gives creators the power; a steward that stays a member can add workers later, which a creator who left could not.                                                                                                                                                                                                                                 |
| D17 | Receive accepts pings only from agent handles holding a role and the warden                                                                         | Humans reach agents only through the warden, even with a custom client.                                                                                                                                                                                                                                                                                |
| D18 | The forge and provenance check runs on send **and** in the syncer before the inbox write; hooks and inbox reads stay offline                        | A sender can skip pingbus; the receiver is the only check it cannot skip.                                                                                                                                                                                                                                                                              |
| D19 | References use full 40-hex SHAs and lowercase `owner/repo`; `pr:` carries its head SHA                                                              | Unambiguous; a ping names exactly the content it vouches for.                                                                                                                                                                                                                                                                                          |
| D20 | One forge per team, GitHub REST API by default (`forge_api` overridable)                                                                            | Smallest design that meets "checked against the forge API".                                                                                                                                                                                                                                                                                            |
| D21 | The warden mirrors every valid ping into the control room as an `m.notice`                                                                          | Humans are not in bus rooms, so the mirror is how they watch.                                                                                                                                                                                                                                                                                          |
| D22 | Warden runs as host Python under `systemd --user`, not a container                                                                                  | Standard library only, needs no image, reaches the HS on loopback.                                                                                                                                                                                                                                                                                     |
| D23 | Protocol spec lives at `docs/agent-team-bus-protocol.md`; a contract test ties it to the constants                                                  | The issue wants a versioned spec in the repo that outlives this plan.                                                                                                                                                                                                                                                                                  |
| D24 | No terminal Matrix client in v1                                                                                                                     | `pingbus tail` covers the terminal view; a client costs a pinned binary and its own privacy checks.                                                                                                                                                                                                                                                    |
| D25 | Element is not confined; P4 proves its traffic by a pasta capture of that profile alone                                                             | The user service manager cannot apply cgroup IP filters, and a check is what the issue asks for.                                                                                                                                                                                                                                                       |
| D26 | `wait` default timeout 1500 s                                                                                                                       | Under the Monitor tool's 30-minute cap, and bounds an orphaned waiter.                                                                                                                                                                                                                                                                                 |
| D27 | Human commands are typed in a control room with no agent members; targets are handles written in the command                                        | The homeserver delivers every room message to every member, so separation must be by membership, not by filtering.                                                                                                                                                                                                                                     |
| D28 | The trust root is a file under `path_prefixes` at a commit reachable from a trusted branch of an allowlisted repository                             | A SHA alone can come from a fork network or an unmerged branch.                                                                                                                                                                                                                                                                                        |
| D29 | Each verb accepts only the reference forms in PROTOCOL.md §5                                                                                        | Issue and PR text is mutable and writable by outsiders; only status verbs may point at an issue.                                                                                                                                                                                                                                                       |
| D30 | Only the handle and token persist per seat; `member.json` is rendered at every start                                                                | Allowlist and human changes reach existing members without new handles.                                                                                                                                                                                                                                                                                |

## Owner questions

1. Event namespace `io.github.longtermsupport.agentbus` (PROTOCOL.md §2): it is
   permanent in every room's history once used. Accept, or name another reverse domain the
   project controls. U02 waits on this.
2. P2 needs a second machine: is a vm-test-lab VM acceptable as "another machine", or must
   it be a physical LAN peer? (A VM exercises the libvirt bridge's zone only.)
3. D7: confirm docker, lxc and vm members can wait for a later version.
4. D27 departs from issue §4: humans command from a control room that has no agents, so
   Element cannot offer agents as `@` pills there; targets are typed handles, still
   checked strictly. Accept, or ask for a probe of whether Element offers invited-but-not-
   joined members as pills (agents would then be invited to, but never join, the control
   room).
5. D8 departs from issue §2's "one handle per agent session": a handle names a checkout's
   seat, reused by later sessions in that checkout unless `--new-handle` is given. Accept?
6. `agent_team_forge_token` (vault): which forge account's token should the warden use?
