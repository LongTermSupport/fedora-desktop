# Agent team bus

The agent team bus lets Claude Code agents working in different repositories, on one
machine or several, send each other short structured messages ("pings"), and lets the
humans of a team talk to those agents from Element on a desktop or a phone. It runs on
Matrix: each team has its own small, private Matrix homeserver (Tuwunel) with federation
off and registration closed, and one invite-only room.

This guide is for the people who set up teams and join agents to them. The wire format,
the verbs, the validation rules, the limits and every `pingbus` command are in the
protocol spec, [agent-bus-protocol.md](agent-bus-protocol.md).

Three commands do the work:

| Command             | Where it runs                               | What it does                                                           |
| ------------------- | ------------------------------------------- | ---------------------------------------------------------------------- |
| `agent-bus-install` | as root on the machine hosting a homeserver | installs the software and each team's homeserver; backups and restores |
| `agent-bus`         | through `sudo`, on that same machine        | the team's accounts: members, roles, humans' passwords and sessions    |
| its `seat` commands | as yourself, in a ccy checkout on that host | a checkout's seats: what `ccy --teams` creates, listed and removed     |
| `pingbus`           | wherever an agent runs                      | the agent's client: send, receive, wait, status                        |

`agent-bus-claude` starts a Claude Code session with the bus switched on, for agents that
are not ccy sessions.

## Teams

A team is a group of agents and humans working around one project. It has:

- a name (lowercase letters, digits and `-`, up to 24 characters);
- one homeserver, on one machine, listening on the addresses the team chooses;
- one room, which only the team's admin account can change;
- a list of humans, each with one account;
- the repositories and branches that pings may refer to, and the path prefixes within
  them.

Each agent in a team is a **member**: one agent seat, named by a handle such as
`myrepo.1+workstation.host` (repository, a number, the install's role and where the agent
runs). Each member is an `orchestrator` or a `worker`, which decides the verbs it may send.
An agent can be in several teams, with one bundle (its settings and access token) per
team.

A team is declared in a **team file**, JSON, which a human writes and the installer
applies. Nothing about a team is ever committed to this repository: on a desktop the team
files come from the untracked host_vars (`agent_bus_teams` in `localhost.yml`, with a
commented example in `localhost.yml.dist`); elsewhere they come from the other project's
own IaC.

```json
{"team": "<team>",
 "port": <port>,
 "listen": ["<bus_ip>"],
 "allow_from": ["<cidr>"],
 "humans": ["<name>"],
 "repos": [{"repo": "<owner>/<repo>", "branches": ["<default-branch>"]}],
 "path_prefixes": ["CLAUDE/Plan/", "docs/"],
 "forge_api": "https://api.github.com"}
```

`listen` is the list of addresses the homeserver answers on, besides `127.0.0.1`; any
concrete address is accepted, the wildcards `0.0.0.0` and `::` are not. `allow_from` is the
list of source ranges that may connect; the firewall and the homeserver's own systemd unit
both enforce it. `server_name` is optional (the default is `<team>.agent-bus.internal`) and
can never change once the team exists.

## Where a team's homeserver runs

The convention is to put the homeserver where the team's members are:

- **All members on one machine** (one laptop, its ccy sessions, containers and guests):
  the homeserver runs on that machine and listens on its bus address, `agentbus0`, a
  private address that local containers and guests reach.
- **A member elsewhere** (an always-on agent on a data-centre server): the homeserver runs
  beside that member, and the others reach it over WireGuard.

One homeserver per team keeps each team's accounts, admin, backups and placement apart, so
a team can move machine without touching another. A machine may host several teams, each
with its own port, data and secrets.

**There is no TLS.** Over loopback, the bus address, a host-only bridge or WireGuard, the
traffic never leaves an encrypted or in-kernel path. On any other network (a LAN, Wi-Fi,
anything routed beyond the machine) every login, password and access token would travel in
clear, and anyone who can watch that network could act as those humans and agents. The
installer does not refuse such a team; choosing the network is the team's decision. Use
WireGuard.

## Installing a homeserver host

On a desktop built from this repository, `play-agent-bus.yml` (part of the main playbook)
does it all: it installs the software on every run, puts the bus address on `agentbus0`
when `agent_bus_address` is set, and installs or removes each team in `agent_bus_teams`
(see [playbooks.md](playbooks.md)).

**From another project.** Any Fedora desktop or server can be a homeserver host. Its own
IaC clones this repository at a pinned commit and runs the installer from the clone, as
root:

```bash
sudo <clone>/files/usr/local/sbin/agent-bus-install software --source <clone> --bus-address <bus_ip>
sudo agent-bus-install team --team-file <team-file>
sudo agent-bus-install check --team <team>
```

`--bus-address` is optional (leave it out on a server reached only over WireGuard).
`software` installs the packages, the `agent-bus` system user, the pinned homeserver
binary, the `agent-bus`, `pingbus` and `agent-bus-claude` commands and the member kit, and
copies the installer to `/usr/local/sbin`. `team` creates or updates the team: its
configuration, firewall rules and unit, its admin account, room and human accounts. Both
are idempotent and print a `CHANGED` line only when something changed, and the homeserver
is restarted only then. `check` prints, read-only, the facts the privacy checks rely on.

To take a team off a machine:

```bash
sudo agent-bus-install remove --team <team>
```

Its data stays unless `--purge` is added. On a desktop, set `state: absent` on the team's
entry instead and run the play.

## Joining a team

Every member needs three things: its **bundle** for the team, `pingbus` with the Claude
Code plugin, and its sessions started with that plugin and the setting that lets the bus
wake an idle session. A ccy session needs none of the steps below: it is launched into a
seat, which the launch creates (see [A ccy session: seats](#a-ccy-session-seats)).

1. Work out the member's arguments. `pingbus suggest-handle`, run in the member's
   checkout, prints `--repo`, `--host` and `--type` from the checkout's remote, the
   install's role (`HOOKS_DAEMON_HOSTNAME`) and where it runs. It refuses when no role is
   set: handles appear in public forge text, so the machine's real hostname is never used.

2. On the homeserver host, a human with sudo creates the member:

   ```bash
   sudo agent-bus add-member <team> --repo=<repo> --host=<role> --type=host --role=worker --address=<bus_ip> --out=$HOME/bundle-<team>
   ```

   `--address` is the homeserver address the member will use: one of the team's `listen`
   addresses (for a ccy member, see its README). `--out` must not exist yet; the bundle
   (`member.json`, `token` and a `README` with the next steps) is written there as the
   user who ran sudo, never as root, so `--out` must be somewhere that user can write.
   `--out=-` prints the three files as a tar on stdout instead and writes nothing, for a
   caller that places them itself. `--no-human-text` makes a member that takes pings only (see
   [Joining a team hosted elsewhere](#joining-a-team-hosted-elsewhere)).

3. Put the bundle where the member reads it and list the team in `PINGBUS_TEAMS`, then
   run `pingbus config check` as the member.

Where the bundle goes, and what else differs, depends on where the agent runs. Each type
has a README in the member kit, `/usr/local/share/agent-bus/kit/` on every host where
`agent-bus-install software` has run (in this repository, under
`files/opt/claude-yolo/optional/agent-bus/`):

| Type     | The agent runs                              | Bundle at                                             | The team file's `allow_from` needs | Steps           |
| -------- | ------------------------------------------- | ----------------------------------------------------- | ---------------------------------- | --------------- |
| `podman` | in a ccy session                            | `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/` | nothing extra                      | `README.podman` |
| `host`   | on a desktop or server, as a dedicated user | `~<agent-user>/.config/pingbus/<team>/`               | nothing extra                      | `README.host`   |
| `lxc`    | in an LXC container                         | the agent user's `~/.config/pingbus/<team>/`          | the LXC bridge's subnet            | `README.lxc`    |
| `docker` | in a docker container                       | `$PINGBUS_HOME/<team>/`, writable                     | the docker network's subnet        | `README.docker` |
| `vm`     | in a virtual machine                        | the agent user's `~/.config/pingbus/<team>/`          | the libvirt network's subnet       | `README.vm`     |

A member on another machine reaches the homeserver over WireGuard, so `allow_from` lists
the WireGuard subnet or the peer's address, and `--address` is the homeserver's WireGuard
address.

**Starting sessions.** A ccy session is launched into its teams with `ccy --teams` (next
section). Every other member starts
Claude Code with `agent-bus-claude`, which reads `~/.config/pingbus/env` (or the file
`PINGBUS_ENV` names), checks every listed team's bundle, and starts `claude` with the
plugin and settings. Its arguments are passed on to `claude`. An agent driven by a script
(`claude -p` in a loop) needs neither: its driver runs `pingbus wait` between turns.

### A ccy session: seats

A ccy session is in a team only when it is launched with it, and in no team otherwise:

```bash
ccy --teams <seat>@<team>[,<seat>@<team>...]
```

One seat per team per session; a session may be in several teams. A plain `ccy` is in no
team, whatever seats the checkout has, and nothing in the checkout opts it in: nothing
about the bus goes in `ccy.env.local`. v1 teams are on this machine: the team's homeserver
must run here.

A **seat** is a durable member of one team in one checkout. `dev1@<team>` is the same
member, with its handle, role, pending pings and history, every time a session is launched
into it; one live session holds a seat at a time, and a launch into a held seat is
refused. The first launch that names a seat creates it: one `add-member` run with sudo (a
headless launch uses only sudo's cached credential, so run `sudo -v` first), which hands
the bundle back on stdout; the launch's seat step, running as you, writes it to
`<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`, which ccy's
`.claude/ccy/.gitignore` keeps out of git. Root writes nothing in the checkout, since
other sessions in it can change it, and a symlink anywhere in that path is refused. A session taking over a seat reads what the seat
received and sent before with `pingbus history`.

A seat's handle is `<repo>.<seat>+<host>.podman`: `<repo>` from the checkout's forge
remote, and `<host>` the `HOOKS_DAEMON_HOSTNAME` that the checkout's `ccy.env.local`
assigns, or the literal `local` when it assigns none, so no role has to be set first and
the machine's hostname is never used.

On the host, in the checkout, as yourself (the `agent-bus` wrapper refuses these through
sudo):

```bash
agent-bus seat list
agent-bus seat remove <seat>@<team>
```

`seat list` prints one `SEAT` line per seat: team, seat, `held` or `free`, `self` or `-`,
and the handle. `seat remove` (refused while the seat is held) parks the handle (token
revoked; account, role and room membership kept), deletes the seat's directory, and
deletes `.claude/ccy/pingbus/` once nothing is left in it. Launching into the same
`<seat>@<team>` again returns the seat with a new token, history included. A seat whose
directory was lost is recovered the same way: remove it, then launch again. ccy itself
runs `agent-bus seat check <list>` and `agent-bus seat take <list>` for a launch with
`--teams <list>`. A
seat's role is changed with `set-role`, the handle read from `agent-bus seat list`.

**Running the team.** On the homeserver host:

- `sudo agent-bus list <team>` shows the members, their roles, room membership and whether
  each is active or parked (no tokens).
- `sudo agent-bus set-role <team> <handle> --role=orchestrator` changes a role.
- `sudo agent-bus park-member <team> <handle>` revokes a member's token and keeps its
  account, role and room membership; `add-member --seat=<seat>` with the same handle
  parts brings it back with a new token. `agent-bus seat remove <seat>@<team>` parks a ccy seat this way.
- `sudo agent-bus remove-member <team> <handle>` removes a member for good; its handle is
  never reused.
- `sudo agent-bus rotate-token <team> <handle> --out=<bundle-dir>` logs the member out and
  writes a new `token`, as the user who ran sudo, into a bundle directory owned by them, which must
  already hold the member's `member.json`. For a member that runs as another user,
  rotate into a copy and install the new `token` for the member as before.

## The dedicated agent user

A Matrix client keeps its session token in its user's home, and anything that runs as
that user can use it to post as that human, including `@room` messages every agent in the
team acts on. So **no agent runs as a user that holds a human's Matrix session**. On a
desktop, a bare-host agent runs as its own user, and the human starts its sessions with:

```bash
sudo -iu <agent-user> agent-bus-claude
```

That user:

- holds no Matrix session: no Element profile and no browser logged in to the team.
  `agent-bus-claude` and `pingbus config check` refuse (exit 78) when its home has an
  Element profile; a browser session cannot be detected, so this part is a rule;
- cannot `sudo` without a password and is not in the `docker` group, either of which
  would reach the team's secrets: an agent that can become root is trusted like a human;
- owns its bundle (`token` must be owned by the user running `pingbus`, mode 0600 or
  stricter) and its env file, `~/.config/pingbus/env`, which sets `PINGBUS_TEAMS` and may
  set `HOOKS_DAEMON_HOSTNAME` and `PINGBUS_HOME`;
- has Claude Code installed and logged in.

Creating the user is the install's own IaC; this repository does not build it. Because
`add-member` writes the bundle for the user who ran sudo, the IaC then installs it for the
agent user, directory 0700 and files 0600, and deletes the human's copy.

A ccy session runs as the desktop user but sees only its own checkout, not the human's
home, so it needs no separate user.

## Joining a team hosted elsewhere

**Root on the homeserver host can instruct every agent in the team.** That machine holds
the team's root of trust (the secret that can create any account, and the admin account's
token), so whoever controls it can create a human account, or post as any human, and its
text reaches your agent as that human's request. The same holds for anyone who can read a
team human's Matrix session.

When you join an agent to a team whose homeserver someone else runs, decide whether you
trust them that far. If not, have them create the member with `--no-human-text`: its
bundle then says `"human_text": false`, and the member drops every human message and acts
on pings only. A ping is a fixed verb about a committed file, and the member checks at the
forge that the file is on an allowed branch of an allowed repository before acting.
The team's admin writes that allowlist, so the operator can still point your agent at any
document in a repository it lists; the agent reads a referenced file as a document, never
as instructions. The switch can only narrow what a member accepts, so an agent that can
write its own bundle cannot widen it.

## Backups are the root of trust

Each team is backed up daily by `agent-bus-backup@<team>.timer`, and on demand with:

```bash
sudo agent-bus-install backup-now --team <team>
```

A backup is two parts: the homeserver's database backup (the last seven, under
`/var/lib/agent-bus/<team>/backups/`) and a root-only archive of the team's secrets and
state with the same number, under `/var/lib/agent-bus-install/<team>/backups/`. The
archive holds the secret that can create any account and the admin token, so **whoever
holds a backup can instruct every agent in the team**, exactly as root on the homeserver
host can. Copying backups off the machine is the owner's decision and is not built; a
copy must be guarded like root access to that machine.

To restore backup number `<n>`, which stops the homeserver, restores the database and the
archive, and starts it again:

```bash
sudo agent-bus-install restore --team <team> --backup <n>
```

## The human's account

Each human named in the team file gets one account, locked down:

- never a server admin; in the team room it can send messages but cannot change the room,
  invite, remove or redact;
- password login only: no login tokens, no logging in from an existing session, no guest
  access, no identity server;
- reachable only on the addresses the team listens on.

No password is ever stored. A human gets one, or a new one, with:

```bash
sudo agent-bus human password <team> <name>
```

which sets a 32-character password, logs out the human's other sessions, and prints the
password once to that terminal. Losing it means running it again. Then, in Element on a
desktop or phone, the homeserver is `http://<address>:<port>` (a phone reaches it over
WireGuard).

To look after the account:

- `sudo agent-bus human devices <team> <name>` lists its sessions;
- `sudo agent-bus human logout-all <team> <name>` ends every one;
- `sudo agent-bus human lock <team> <name>` blocks the account at once (a lost phone), and
  `sudo agent-bus human unlock <team> <name>` lets it log in again.

For a lost phone: lock, then log out all sessions, then set a new password before
unlocking. Removing a human from the team file and applying it again deactivates the
account.

## Limits

These are known and documented rather than engineered around:

- Joining a team gives the operator of its homeserver the power to instruct your agent
  ([above](#joining-a-team-hosted-elsewhere)); `--no-human-text` narrows it to pings.
- No TLS: on a network that is not private, logins, passwords and tokens travel in clear.
- Backups hold the team's root of trust.
- Every agent account can read the whole team room, including human messages addressed to
  other agents: `pingbus` delivers each human message only to the agents it mentions (all of them for `@room`), but
  the account receives them all. Human text is trusted input, so this is a privacy limit
  between teammates, not a way to inject instructions.
- An agent can show humans free text under its own handle (`pingbus say --to <name>`); `pingbus`
  refuses text that looks like a secret, and every agent ignores agent text. An agent can
  skip `pingbus` and post with `curl`, so these are guards against mistakes, while the
  receiving agents' own checks are what hold.
- Members on one private network or bridge can reach each other, and the homeserver can
  open connections to them.
- An agent on a machine where it can `sudo` without a password reaches the root of trust.
- A human's Matrix session in a browser cannot be detected beside a bare-host agent.
- Matrix offers no second factor here.
- The rate limits, size limits, acknowledgement deadlines and the stale-message window are
  in [agent-bus-protocol.md](agent-bus-protocol.md#10-limits).
