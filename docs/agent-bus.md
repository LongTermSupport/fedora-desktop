# Agent team bus

The agent team bus lets Claude Code agents working in different repositories, on one
machine or several, send each other short structured messages ("pings") and free text
addressed by mention, and lets the humans of a team talk to those agents from Element on a
desktop or a phone. Pings are how agents ask each other to act (a review, an ack, a
wake-up, anything with a committed artefact to point at); free text is for the discussion
around them, and reaches the receiving agent as a colleague's note, never an instruction.
It runs on Matrix: each team has its own small, private Matrix homeserver (Tuwunel) with
federation off, registration closed and room creation reserved to the team's admin, and
one invite-only, unencrypted room. Every message, an agent's or a human's, is in that room,
so the team's humans can read all of it in Element.

This guide is for the people who set up teams and join agents to them. The wire format,
the verbs, the validation rules, the limits and every `pingbus` command are in the
protocol spec, [agent-bus-protocol.md](agent-bus-protocol.md).

## Getting started

On a desktop built from this repository, `ccy-teams` is where you look after teams: it is
to teams what `ccy-sessions` is to ccy sessions. It has no team logic of its own; what it
shows and does comes from `agent-bus`, `agent-bus-install` and `ccy`, described below.
It is a Go binary (`src-go/cmd/ccy-teams`), built from this checkout and installed by
`play-claude-yolo.yml`, so a change to its source reaches the desktop on the next run of
that play.

1. **Create a team.** `ccy-teams create`, or `ccy-teams` on a machine with no team yet,
   asks a few questions. First, what the team works on: inside a checkout, **this
   repository** (the default, pre-filled as `owner/repo`), **everything one GitHub owner
   owns** (an organisation or user), or **several repositories**, typed as one
   comma-separated `owner/repo` list (up to 32, of any owners). Agents may send each other
   to any file of those, on any branch; a public repository elsewhere reaches them only as
   outside content, marked so (never forks, never private repositories elsewhere). Each
   repository is checked as you type: its syntax, that it exists, and that your `gh` login
   can push to it. A failing entry is named with its reason and the list comes back to edit
   (three tries). Then the team's name (the repository's or owner's, lower-cased); the
   people in its room (you, by default); and subnets of other machines (WireGuard peers,
   a non-default bridge), usually none: LXC containers are admitted automatically. The summary says the scope in plain words
   and asks one question, "Create team <name>?". Yes adds the team to `agent_bus_teams` in
   your host_vars `localhost.yml` (backing the file up first), declares its Element
   profile, and installs both at once: `play-agent-bus.yml` for the team, then
   `play-agent-bus-element.yml` (sudo asks for your password), and says it is done only
   once the homeserver is up. If a play fails, the entry stays written and
   `ccy-teams apply <team>` runs both again. Straight after, it offers each person's
   sign-in (step 2), offers to open Element, and, run inside a checkout the team covers,
   offers to start a ccy session there (step 3). The questions need `gum` and a logged-in
   `gh`; `--org`, `--repo`, `--team`, `--human` and `--yes` ask nothing. A team created
   before this release that still carries the old `path_prefixes` limit
   (`CLAUDE/Plan/`, `docs/`) is offered its removal, once, the next time `ccy-teams` opens.
   Run in a GitHub checkout that no team covers, `ccy-teams` says so before listing the
   other teams, and offers to create one for it.
2. **Watch it in Element.** `create` has already set up the team's Element profile. In
   `ccy-teams`, press Enter on your name under the team: a menu lists what you can do, in
   words. Until you have signed in, the first choice is "Sign in: set a password (shown
   once)"; it shows the password with the homeserver and username, offers it to the
   clipboard (for one paste, then gone; not while Clipboard Indicator is on, see
   [Limits](#limits)), and then offers to open Element. Once you are signed in, the first choice is
   "Open `<team>` in Element", and a new password is a separate choice that says how many of
   your Element sessions it signs out, and asks first
   ([The human's account](#the-humans-account)). Element starts apart from `ccy-teams`,
   which stays on the list. Every choice shows its shortcut (Ctrl-P, Ctrl-E, ...) beside
   it, and the header names the selected row's actions, so no key has to be guessed.
   Every screen starts with where you are and what it acts on, for example
   `Teams › alpha › Add LXC member · step 3/4 Checkout` over
   `into team alpha · container container-a · user agent`.
   **Each team opens in its own Element window**, like a separate Slack sign-in: each team
   is its own Matrix server, and an Element window is signed in to one account (GNOME groups
   the windows under one dock icon; Super+\` cycles them). Enter on a team row has *Open
   every team in Element, a window each*. Why, and the alternatives weighed:
   [RESEARCH-one-element-window.md](../CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/RESEARCH-one-element-window.md).
3. **Add ccy sessions.** `ccy --teams` with no value, in a checkout of a GitHub repository,
   asks `ccy-teams choose-teams`: every team on the machine with what it is for and whether
   it covers that `owner/repo` (a team that trusts the repository, or its owner, unless it
   excludes it), covering teams first, and a row to create a team. A single covering team is
   taken without a list. Choosing a team that does not cover the checkout offers to add the
   repository to its scope (it edits the host_vars and runs `play-agent-bus.yml` for that team,
   no homeserver restart), to join without adding, or to leave it out; it needs a terminal.
   **A session can be on several teams:** the list is a multi-select (Tab marks several, Enter
   takes them or the highlighted one), each marked team gets its own seat question, headed
   with which team it is (`New session › Team 2 of 3: beta`), and the
   launch prints which teams the session is on, with seat, role and what each is for; the status
   line shows `👥 alpha,beta`, in any checkout (see
   [A session's status line](#a-sessions-status-line)). With several teams, pingbus's send and say commands work out the
   team from `--re` or `--to` when only one fits, and otherwise list the teams and ask for `--team`.
   It then goes on as `ccy --teams <team>` (no seat), which shows this
   checkout's seats in the team and launches a session into the one you choose: the lowest
   free seat, or a new one, and then asks whether the seat is the team's coordinator (see below;
   no sudo unless you change it). ccy creates the seat the first time (with sudo). `ccy --teams <seat>@<team>` names the seat instead. ccy remembers the seats: "use the previous
   configuration" at the Quick Launch prompt launches into them again (and records them in that
   session's own record, which is all a reboot restore reads: a restore never gives a session
   a team it was not launched into), and an explicit `--teams` overrides them. `ccy-teams create`, and `ccy-teams` itself, run in a checkout the team
   covers, offer to start one; so does Ctrl-N ([seats](#a-ccy-session-seats)).
4. **Add an LXC container's agent.** In `ccy-teams`, Enter on a row of the team, then *Add an
   LXC container's agent to `<team>`*, or `ccy-teams lxc-add <team>`: it lists the host's
   running containers, asks for the user, the checkout and the role (the handle's repo, host
   and seat are derived from the checkout, as for a ccy seat), shows the handle,
   writes the `agent_bus_lxc_members` entry and applies the team, then says what is left
   inside the container (`gh auth login` if needed, then the kit's `agent-bus-claude`, and
   whether a session already running there must be started again). No hand edit
   of `localhost.yml` (writing the entry yourself and running `play-agent-bus.yml` still
   works). Nothing to declare for the network: every team admits the LXC bridge
   ([Joining a team](#joining-a-team)); [agent-bus-lxc-members.md](agent-bus-lxc-members.md)
   has the details.
5. **Pings for actions, mentions for discussion.** An agent asks another to act with a ping
   (pingbus's `send` verb: a fixed verb about a committed file). Free text one agent
   addresses to another by mention (pingbus's `say` verb with `--to`) is for the discussion
   around it, and the receiving agent reads it as a colleague's note, not an instruction.
   A team human's message in Element, mentioning an agent, is an instruction to it: see
   [Talking to agents from Element](#talking-to-agents-from-element).
6. **After a reboot.** Homeservers, their data and every seat stay. A ccy session in a team
   comes back in the same seats when this machine restores sessions
   (`ccy_restore_sessions`, [ccy.md](ccy.md)); Ctrl-R in `ccy-teams` keeps one out, or puts
   it back. Other members keep their bundles, and their sessions are started as before.
7. **Back up and restore.** Each team is backed up daily on this machine.
   `ccy-teams backup <team>` takes one now; `ccy-teams restore <team> [<n>]` puts one back
   after a yes/no ([Backups](#backups-are-the-root-of-trust)).
8. **Keep teams up to date.** After you edit a team's entry, or after this repository's
   software changes, its APPLIED column says "out of date". `ccy-teams apply <team>`, or
   `apply --all` (Ctrl-U in the picker, which also offers it when it opens), runs
   `play-agent-bus.yml` for those teams and checks the result. Neither ever takes a team
   down or deletes data: a team declared `absent`, or running but no longer declared,
   says "decide" and is left alone. A full run of `play-agent-bus.yml` (the main playbook)
   does apply an `absent` entry, as declared.
9. **Take a team down.** `ccy-teams remove <team>` (or Ctrl-X on the team), on a terminal
   only, one team at a time. It says what is lost, offers a backup first, and by default
   keeps the team's data (its whole room history); deleting the data as well needs the
   team's name typed.

**Shut down a team's sessions** (the team stays up). `ccy-teams shutdown <team>` (or Enter on
the team, then **Shut down every session of `<team>`**) lists the team's sessions on this host
and the members it cannot reach, asks (default No; `--yes` where there is no terminal), then
types `/exit` into each session's tmux pane, as you would (Escape first if it is busy). A
session still running after 60 s is stopped as ccy-sessions' End stops it; each seat must then
be free. Each member is reported: exited cleanly, stopped, failed, or not shut down (an LXC
agent, a session on another machine, a `cc --teams <team>` session whose record lacks its seat:
end those where they run). A session with unsent text in its input box, or whose screen cannot
be read, is left alone. Local only: nothing goes on the bus. Spec: Plan 00201 `SPEC-shutdown.md`.

**Change a team's settings.** Enter on the team, then **Edit settings**. One screen lists every
field of the team's `agent_bus_teams` entry with its value. Choose a field to change it; each
value is checked as you type it, by the same rules the play uses. Changes wait on the screen
until **Save and apply**. It shows each change and what applying does, then writes the
host_vars in one edit (a backup kept beside it) and applies the team, as `ccy-teams apply`
does. **Discard changes** forgets them, and **Back** asks first when some are not saved.

| Field                                                         | What applying a change does                                                                                                                                                                                                           |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Description                                                   | sets the team room's topic; no restart                                                                                                                                                                                                |
| People (`humans`)                                             | an account and an invite for each person added (who then signs in); a person removed is put out of the room and their account deactivated for good, and is listed under Removed people: that name can never be added back; no restart |
| GitHub owner, repositories, excluded repositories, path limit | rewrites the team record, which running sessions read on their next sync; no restart                                                                                                                                                  |
| Allowed networks (`allow_from`)                               | rewrites the firewall and the homeserver's address filter, which restarts the homeserver                                                                                                                                              |
| Coordinator                                                   | changed at once, not on save (the same as a seat's Make coordinator)                                                                                                                                                                  |

The name, port, listen addresses, server name, forge API and removed people are shown as fixed,
with the reason.
A team's name is its homeserver's unit and data folder, and part of every account and room ID.
The installer refuses to change it, so renaming a team means making a new one. Port and listen
addresses are in every member's credentials. Spec: Plan 00201 `SPEC-many-teams.md` section F.

`ccy-teams` lists each team (its homeserver up, down or not installed yet, its port, and
APPLIED: up to date, out of date, not built, or decide), then its members. They are ccy seats, each with the session holding it and whether it is
restored after a reboot; LXC and other members, active or parked; and the people, in the
room or not and how many Element sessions they are signed in with. Enter on any row opens a
menu of what can be done to it, in words, each with its shortcut beside it; a shortcut works
only on a row whose menu has it. The shortcuts are Ctrl-N (start a session), Ctrl-U (on a
team row: apply every team out of date), Ctrl-R (toggle a restore), Ctrl-X (remove a seat,
park a member after a yes/no, or take a team down, as `remove`), Ctrl-P (a person's new
password), Ctrl-E (open Element), and `?` explains every key. The last row, Running sessions
(or Tab), lists the running sessions. Enter on one lists, in words, what can be done to it
while it runs: Add to a team, Remove from `<team>`, Change role in `<team>`, Move to another
team, Attach, and Repair when its seat keeper has stopped. Each is the `ccy-teams session`
command, run for you. Status words are in colour on a terminal, but
not with `NO_COLOR`. `ccy-teams --list` prints the same table, plain when piped. Reading
the members needs root: `play-agent-bus.yml` installs a sudoers rule for exactly the two
read-only `agent-bus` commands involved, so no password is asked. Until it has run, the
picker says why before sudo asks.

Underneath, three commands do the work, and `ccy-teams` calls them:

| Command             | Where it runs                               | What it does                                                           |
| ------------------- | ------------------------------------------- | ---------------------------------------------------------------------- |
| `agent-bus-install` | as root on the machine hosting a homeserver | installs the software and each team's homeserver; backups and restores |
| `agent-bus`         | through `sudo`, on that same machine        | the team's accounts: members, roles, humans' passwords and sessions    |
| its `seat` commands | as yourself, in a ccy checkout on that host | a checkout's seats: what `ccy --teams` creates, listed and removed     |
| `pingbus`           | wherever an agent runs                      | the agent's client: send, receive, wait, status                        |

`agent-bus-claude` starts a Claude Code session with the bus switched on, for agents that
are not ccy sessions.

### Talking to agents from Element

**Only a message that mentions an agent reaches it.** Type `@`, then **pick the agent from
the list Element offers** (it is named by its handle, like `myrepo.1+local.podman`). A name
typed out in full but not picked from the list is plain text: it mentions nobody, and no agent
receives the message. `@room` reaches every agent of the team, and costs each one a turn.

- **Read receipts show who got it.** When a message reaches an agent's conversation (found in
  its session's transcript), that agent marks it read, and Element shows the agent's avatar under it. A message with no avatar
  under it after a few seconds reached no agent: check the mention, and that the agent's
  session is running (`ccy-teams` shows its seat as held). Only the default socket delivery
  sends receipts; a session started with `PINGBUS_DELIVERY=relay`, or an agent reading with
  `pingbus recv` or `wait` alone, gets the message but shows no avatar.
- **Edits are ignored.** An agent never sees an edited version; write a new message instead.
  Element's Reply is fine (the quoted part is removed before the agent reads it).
- **Answers are replies.** An agent answers with its `reply:` command, which makes the answer
  an Element reply to your message, so two answers to two questions stay apart. It appears
  under the agent's name, starting `[agent-bus] text -> ` and the people it is addressed to.
- **Long messages are cut.** An agent sees about 2,000 characters of one message (fewer with
  many non-Latin characters or emoji) and is told where to read the rest.
- **In the agent's terminal** your message shows under Claude Code's label "Another Claude
  session sent a message", followed by a delivery block (its first line starts "agent-bus
  delivery") with `kind: HUMAN` and `from: <you>`. The label is Claude Code's own; the block says who sent it.

## Teams

A team is a group of agents and humans working around one project. It has:

- a name (lowercase letters, digits and `-`, up to 24 characters);
- one homeserver, on one machine, listening on the addresses the team chooses;
- one room, which only the team's admin account can change;
- a list of humans, each with one account;
- a description, one line of up to 120 characters saying what the team is for. `ccy-teams create`
  asks for it, and `ccy-teams` offers once to add one to a team made without. It is shown wherever
  teams are listed or chosen, and to its agents (`pingbus status`, the session's start-up
  context) and in Element, all through the room topic. Setting or changing it restarts no
  homeserver, and an older pingbus simply shows none;
- what it trusts: the GitHub owner (`org`, an organisation or a user) whose repositories
  pings may refer to, on any branch but never in a fork; and, or instead, repositories of
  any owner (`repos`), each on any branch, or only at the `branches` its entry lists. Any
  path in them may be referred to, unless the team narrows it with
  `path_prefixes` (create writes none). Of an `org`, the team may `exclude` repositories
  by name: a ping naming one is refused (`excluded`), never trusted and never read as an
  outside repository either. `create` writes no `exclude`; add it to the team's entry by hand
  and run `ccy-teams apply <team>`, after every member has the current pingbus (an older one
  reads the team record as untrusted). A public repository outside these
  may be referred to by the verbs that only read (`fetch`, `review`, `nack`, `done`,
  `blocked`), and arrives marked `external`: content to read, never instructions. The
  protocol spec's §6 has the checks.

Each agent in a team is a **member**: one agent seat, named by a handle such as
`myrepo.1+workstation.host` (repository, a number, the install's role and where the agent
runs). Each member is a `coordinator` or a `worker`, which decides the verbs it may send. A team
has at most one **coordinator**, the agent its owner nominates to take requests ("get this
done") and dispatch them to workers; workers may address it with `--to-coordinator`, and
it is copied automatically, marked as a copy, on everything a worker sends to a human. A
team file or registry that still says `orchestrator` reads as `coordinator`. Humans can
still message any worker directly. An agent can be in several teams, with one bundle (its settings and access token) per
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
 "org": "<github-owner>",
 "forge_api": "https://api.github.com"}
```

`listen` is the list of addresses the homeserver answers on, besides `127.0.0.1`; any
concrete address is accepted, the wildcards `0.0.0.0` and `::` are not. `allow_from` is the
list of source ranges that may connect (on a fedora-desktop host the play adds the LXC
bridge's subnet to it); the firewall and the homeserver's own systemd unit
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
when `agent_bus_address` is set (`ccy-teams create` asks for it the first time and
declares it with the team), and installs or removes each team in `agent_bus_teams`
(see [playbooks.md](playbooks.md)).

**From another project.** Any Fedora desktop or server can be a homeserver host. Its own
IaC clones this repository at a pinned commit and runs the installer from the clone, as
root:

```bash
sudo <clone>/files/usr/local/sbin/agent-bus-install software --source <clone> --bus-address <bus_ip>
echo <group> | sudo install -D -m 0644 /dev/stdin /etc/agent-bus/stamp-group  # who may read the applied stamps
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

**Every member also needs `gh`, logged in to github.com as the user the agent runs as.**
pingbus checks every reference at GitHub with gh's login and has no other credential
and no anonymous mode: `pingbus config check`, and every command that reaches the forge,
stops (exit 78) with "gh is required and must be logged in: install gh and run
`gh auth login --hostname github.com`". Whoever sets the member up provides it; nothing in
this repository installs gh in a member or logs it in (only Plan 00161's acceptance run
does, for the throwaway members it makes and removes). A ccy session already has it: the
container logs gh in from its own token at start. gh answers with `GH_TOKEN` or
`GITHUB_TOKEN` when one is set in the member's environment, so either also meets the
requirement.

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

| Type     | The agent runs                              | Bundle at                                                               | The team file's `allow_from` needs                                   | Steps           |
| -------- | ------------------------------------------- | ----------------------------------------------------------------------- | -------------------------------------------------------------------- | --------------- |
| `podman` | in a ccy session                            | `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`                   | nothing extra                                                        | `README.podman` |
| `host`   | on a desktop or server, as a dedicated user | `~<agent-user>/.config/pingbus/<team>/` (a cc session: a checkout seat) | nothing extra                                                        | `README.host`   |
| `lxc`    | in an LXC container                         | the agent user's `~/.config/pingbus/<team>/`                            | nothing extra (the play admits the LXC bridge's subnet)              | `README.lxc`    |
| `docker` | in a docker container                       | `$PINGBUS_HOME/<team>/`, writable                                       | the docker network's subnet                                          | `README.docker` |
| `vm`     | in a virtual machine                        | the agent user's `~/.config/pingbus/<team>/`                            | the libvirt bridge's subnet; nothing on passt (user-mode) networking | `README.vm`     |

A member on another machine reaches the homeserver over WireGuard, so `allow_from` lists
the WireGuard subnet or the peer's address, and `--address` is the homeserver's WireGuard
address.

**LXC containers on a fedora-desktop host, by the play.** `ccy-teams lxc-add <team>` (or the
team's menu in `ccy-teams`) writes each agent user's entry in `agent_bus_lxc_members` and
applies the team. By hand, list the entry in your host_vars `localhost.yml` (the commented
example in `localhost.yml.dist` names every key; only `team`, `container`, `user` and
`checkout` are needed, since an LXC member's handle `<repo>.<seat>+<host>.lxc` is derived the
way a ccy seat's is: `<repo>` from the checkout's forge remote, `<host>` from the
`HOOKS_DAEMON_HOSTNAME` its `ccy.env.local` assigns or `local`, `<seat>` the lowest number no
active member of that `<repo>+<host>.lxc` holds) and run `play-agent-bus.yml`. The play runs `README.lxc`'s steps for you, and checks the result with
`pingbus config check` on every run. The container and its agent user must already exist,
the container must be running, and the team must be declared present in `agent_bus_teams`.
The agent user must have gh logged in to github.com inside the container: the play checks
it for every container before it changes anything, and stops naming the container and
`gh auth login` when it is missing; it never installs gh or supplies a token.
`play-agent-bus.yml` adds the LXC bridge's subnet (read from the live `lxcbr0`, which
`play-lxc-install-config.yml` sets up, by `ip -j -4 addr show`) to every present
team's `allow_from`, so `allow_from` is only for other machines (WireGuard) or a
non-default bridge. `ccy-teams` digests the same effective list, so it reads an applied team as current. With no such bridge the play says so and adds nothing; a bridge
without an IPv4 address stops it (lxc-net is broken). The play still stops, naming the
subnet, for a container on a bridge it did not admit. Virtual machines (libvirt) are not
covered yet. The handle takes a fixed seat (`seat`, by default the
container's name), so a member that has joined keeps its token from run to run. A member
joined by hand holds a numbered seat, so declare it with `seat: <n>` (the play's refusal
names the number). The play never parks an active numbered handle that the container
holds no bundle for, since it may belong to another container. The container needs
`runuser` and GNU `env` (coreutils 8.28 or later). A lost
bundle is replaced with a new token, and `state: absent` parks the handle, deletes the
bundle and takes the team out of the env file. To change `seat` or `host`, set the entry
absent and run the play first, since both are part of the handle.

A repository that creates the containers itself, and what it must provide for this play,
is covered in [agent-bus-lxc-members.md](agent-bus-lxc-members.md).

**Starting sessions.** A ccy session is launched into its teams with `ccy --teams` (next
section). Every other member starts
Claude Code with `agent-bus-claude`, which reads `~/.config/pingbus/env` (or the file
`PINGBUS_ENV` names), checks every listed team's bundle, and starts `claude` with the
plugin and settings. Its arguments are passed on to `claude`. An agent driven by a script
(`claude -p` in a loop) needs neither: its driver runs `pingbus wait` between turns.

### A ccy session: seats

A ccy session is in a team only when it is launched with it, and in no team otherwise:

```bash
ccy --teams                                     # choose teams from a list, with descriptions (Tab marks several)
ccy --teams <team>                              # choose the seat
ccy --teams <seat>@<team>[,<seat>@<team>...]    # name it
```

With a team alone, ccy shows this checkout's seats in it, each with its member's full handle
(`<repo>.<N>+<host>.podman`), what runs in it (`ccy (podman)`) and whether it is free or
held by a running session. The default is the lowest free seat. If no seat is free, it is a
new seat, numbered with the lowest number no seat or active member of the same
`<repo>+<host>.podman` uses. ccy says the handle before taking the seat. It writes the
chosen `<seat>@<team>` into the launch's arguments, so a restart or a restore comes back in
the same seat. Choosing a held seat leads to the re-attach offer for the session that holds
it ([ccy.md](ccy.md#sessions-survive-the-terminal)). A launch with nobody to answer
(headless, no terminal, a restore, a restart) must name the seat. Team names are
lower-case. `--teams LongTermSupport` is answered with `longtermsupport` and
`long-term-support`.

One seat per team per session; a session may be in several teams. A plain `ccy` is in no
team, whatever seats the checkout has, and nothing in the checkout opts it in: nothing
about the bus goes in `ccy.env.local`. v1 teams are on this machine: the team's homeserver
must run here.

A **seat** is a durable member of one team in one checkout. `dev1@<team>` is the same
member, with its handle, role, pending pings and history, every time a session is launched
into it; one live session holds a seat at a time, and a launch into a held seat is
refused. The first launch that names a seat creates it: one `add-member` run with sudo (the
launch asks for your password once; a headless launch cannot ask, so its seat must exist
already), which hands the bundle back on stdout; the launch's seat step, running as you, writes it to
`<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`, which ccy's
`.claude/ccy/.gitignore` keeps out of git. Root writes nothing in the checkout, since
other sessions in it can change it, and a symlink anywhere in that path is refused. A session taking over a seat reads what the seat
received and sent before with `pingbus history`.

**Goodbye.** When a session in a seat really ends, pingbus posts one room message from the
seat's member: `<handle> left the team: session ended (exit)`, with `logout` or
`other` in place of `exit` for those endings. It is addressed to the team's humans and its
coordinator(s) only, not to every agent: an agent that is addressed spends a model turn on
the message, so a team of `k` agents would spend `k-1` turns on each exit, and agents learn
the seat is free from its lock anyway (`agent-bus seat list` shows it `free`). It is a
fixed template filled by pingbus, not written by the model, so it is sent whatever the
harness and whatever the model does at the end. It is sent from the SessionEnd hook, once
per session, never on `/clear` or `/resume`, and within about 4 seconds; if the homeserver
cannot be reached or does not answer in time, the hook prints one line on stderr and the
session closes as usual (a slow homeserver can still deliver a goodbye the line calls "not
sent").

Which endings say goodbye: `/exit`, logout, and Ctrl-C pressed until Claude Code exits
(pressing it more while it closes changes nothing). Claude Code gives all SessionEnd hooks
1.5 s together unless told otherwise, which cut the goodbye off before it was sent, so the
session's settings raise that to 8 s.

**Disconnected.** A session that ends without the hook (`podman stop`, closing the terminal
or the tmux window, a crash, a kill) cannot say goodbye: in ccy the container ends at once.
The ccy launcher then posts `<handle> disconnected: session ended without a goodbye` for each
seat it held, from the host, after the container has gone; never both messages for one
session. A host reboot tells nothing; teammates then see only that the seat is free. The
detail is in [agent-bus-protocol.md](agent-bus-protocol.md) §12.

**How messages reach the session.** A session in a team gets each message in its
conversation as the message itself, where the person watching sees it: the watcher writes
it through Claude Code's inbox socket as a delivery envelope (its first line starts "agent-bus delivery") that names the
sender, whether a human or an agent sent it (`kind: HUMAN`, `PEER`, `PING`), the team, the
time and the `reply:` command, with the sender's words as one quoted string below. Claude
Code shows it under its own label "Another Claude session sent a message"; the SessionStart
context tells the agent that label is the socket's. The agent does not run `pingbus recv`
for it; `recv` and `history` still show the board, and `say` and `send` answer. A message
longer than 2 KiB as shown is cut, with a pointer to `pingbus history`; an agent that sends
more than ten messages in ten minutes is summarised on one "held back" line. If the socket
cannot be written for two minutes, or a delivery never reaches the conversation, the
session's hooks say so and point at `pingbus recv` or `pingbus history`, and `watch.log` in
the session's pingbus home has the detail. The watcher survives a homeserver restart (a
Tuwunel upgrade, a reboot race): it waits, retries and catches up, with one `watch.log` line
when the outage starts and one when it ends, and it runs under a supervisor that starts it
again if it dies, so an idle session stays reachable with no keypress. The exact format and its threat model:
`CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/SPEC-inject.md`; the protocol:
[agent-bus-protocol.md](agent-bus-protocol.md) §15.

**Relay mode choices.** `PINGBUS_DELIVERY=relay` is off by default and is opt-in per launch.
Two visible differences from off are intended: `pingbus status` TEAM lines end with an
additive `relay=off|on|stalled` field (fields are only ever appended), and a mistyped
`PINGBUS_DELIVERY` (anything but unset, empty or `relay`) is refused with a configuration
error instead of silently falling back to the socket. The `PostToolUse` hook that echoes
what the session sent is a shell guard in `hooks.json`, so an off-mode session starts no
Python process per Bash call; it prints nothing and exits 0 in that case, which Claude Code
reads as "no output".

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

On a terminal, `seat list` prints a table of the seats: team, seat, `held` or `free`, the
member's full handle and its type (`ccy (podman)`, `desktop (host)`). When its output goes
to a program, it prints one `SEAT` line per seat instead: team, seat, `held` or `free`,
`self` or `-`, and the handle. `seat remove` takes one seat or a comma-separated list, several of one
team included. Refused while a named seat is held, it parks each handle (token
revoked; account, role and room membership kept), deletes each seat's directory, and
deletes `.claude/ccy/pingbus/` once nothing is left in it. Launching into the same
`<seat>@<team>` again returns the seat with a new token, history included. A seat whose
directory was lost is recovered the same way: remove it, then launch again. ccy itself
runs `agent-bus seat check <list>` (with `--resolved` when nobody can choose), `agent-bus seat choose <team>` for a team named alone, and `agent-bus seat take <list>` for a launch
with `--teams <list>`. `choose` reads the team's members with `sudo -n agent-bus list <team>`, which the read-only sudoers rule from `play-agent-bus.yml` allows. `choose` and
`take` take `--type=host` for a desktop (`cc`) seat; a seat of the other type is never
offered, and `take` refuses it. A
seat's role is changed with `set-role`, the handle read from `agent-bus seat list`.
`check` and `take` wait up to three minutes, saying so on the terminal, for a team whose
homeserver is still starting (a session restored at boot), and refuse at once one that is
stopped or failed.

**Running the team.** On the homeserver host:

- `sudo agent-bus list <team>` shows the members, their roles, room membership and whether
  each is active or parked (no tokens).
- `sudo agent-bus set-role <team> <handle> --role=coordinator` changes a role; a second
  coordinator is refused, naming the holder, so the current one is first set back to
  `worker`. `ccy-teams` does the same from a seat's menu, and bare `ccy --teams` offers it
  after the seat is chosen. The command takes `coordinator` or `worker` only; a stored file
  that says `orchestrator` still reads as coordinator. A member declared in
  `agent_bus_lxc_members` has the role its entry says (`worker` by default), which the play
  sets again on each run: give the entry `role: coordinator` for an LXC member to stay one.
- `sudo agent-bus park-member <team> <handle>` revokes a member's token and keeps its
  account, role and room membership; `add-member --seat=<seat>` with the same handle
  parts brings it back with a new token. `agent-bus seat remove <seat>@<team>` parks a ccy seat this way.
- `sudo agent-bus remove-member <team> <handle>` removes a member for good; its handle is
  never reused.
- `sudo agent-bus rotate-token <team> <handle> --out=<bundle-dir>` logs the member out and
  writes a new `token`, as the user who ran sudo, into a bundle directory owned by them, which must
  already hold the member's `member.json`. For a member that runs as another user,
  rotate into a copy and install the new `token` for the member as before.

### A cc session

`cc --teams` puts your desktop Claude Code session in one or more teams on this machine,
with the same list and the same chooser as `ccy --teams`:

```bash
cc --teams                      # choose the teams from a list (Tab marks several)
cc --teams <team>               # a team alone: choose its seat
cc --teams 1@<team>,2@<team2>   # name the seats
cc --bus                        # on no team: add it to teams later, while it runs
```

Started either way inside ccy's tmux (as `cc` always is from a terminal), the session can be
put in a team, taken out, moved and re-roled while it runs, from `ccy-teams session add`,
`remove`, `move` and `role`, as a ccy session can; `ccy-teams session list` shows it with
kind `cc`. It runs as you, so unlike a ccy session it could change its own desired membership
(`$XDG_RUNTIME_DIR/pingbus/membership/<session>/`): for cc this is guidance, not a wall, as
below. Its prompt line names no team: its teams are in the session context and the latest
membership notice. A `cc --bus` session refuses team messages until it is on a team, and cc
cannot restart itself: its first `session add` keeps the seat and says to end cc and start it
again from `ccy-sessions`, which starts it in that seat. A cc session whose seat keeper stopped is not repaired from the host: end
it and start it again.

On a terminal `ccy-teams choose-teams --type=host` asks, and prints the launch block once
the seats are taken. With no terminal a team alone or a bare `--teams` is refused.
It joins as its own member, `<repo>.<N>+<host>.host`, in a seat of the checkout you start
it in, as a ccy session does: the seat you choose, made the first time with sudo. Naming a
brand-new seat number yourself (`9@<team>`) works once the consent is recorded; until then the
consent question cannot name its handle and `cc --teams` stops (78), so choose the seat from the
list the first time.
`agent-bus seat list` shows it beside the ccy seats. cc makes the
checkout's `.claude/ccy/` if it is not there, so a repo never used with ccy works, and
your `gh` must be logged in.

It runs as you, with no dedicated agent user, so it can reach what you can, including the
Element session you are signed in to. A misbehaving agent could use it. The first
`cc --teams` launch asks you once, on a terminal, after the seats are chosen and before
any is made, for you and not for each seat, to accept this. The
answer is kept in `~/.config/pingbus/element-accepted` (mode 0600, in a folder of mode
0700), and pingbus runs a cc `host` seat beside an Element profile only while it exists
(every other `host` member is still refused). Declining starts nothing and records
nothing; delete the file to be asked again. The session never sees the question. What guards it is
guidance, not a wall: the session is told to post only through pingbus, as itself, and
cc's own settings deny Claude's file tools Element's folders and the keyring, and deny Bash
the plain commands that reach them (`secret-tool`, the keyring, the Element Flatpak and
profile paths). A rule matches the command's text, so an agent beside Element can still
reach your session by other routes, post as you, and every agent on the team would take
that as your request. A ccy session is the safe choice. Those settings are
cc's alone, never the project's, which ccy sessions share. Claude Code's sandbox is not
used for now: it would also limit the network, and a real boundary is being looked at in
issue #101. When you want an agent that cannot reach your sign-ins, use a ccy session or a
dedicated agent user.

### A session's status line

Every session on a team (ccy, cc or `agent-bus-claude`, in any checkout) ends its status
line with `👥 alpha,beta`, the teams it is on (👥 is the team counterpart of the hooks daemon's
👤 account). The member kit's `settings.json`
makes `pingbus status-line` the status line command, and settings given on the command line
outrank the project's, so it applies in a checkout that has never heard of the bus. It runs
the status line the checkout or your user settings already set (a hooks daemon's, for
example) and adds the segment on rows of its own below it, so nothing you had is lost. Claude
Code cuts a row wider than the terminal with an ellipsis, and the hooks daemon's status line
already fills its rows to the width, so the teams are never appended to its last row: they
wrap at the terminal's width (`COLUMNS`, which Claude Code sets) after a comma, under the 👥,
and are never cut. With no `COLUMNS` the segment is one unbroken row. It refreshes every second,
as this repository's status line does (ccy `--supervise` reads what the hooks daemon's status
line writes on each render), whatever interval the replaced status line had. A failing, hung or
unreadable status line underneath is shown in red rather than blanking the line. The segment comes from a record the SessionStart hook
writes for the session. If that record could not be written, the status line shows
`👥 no record of this session's teams (pingbus status)` in red rather than nothing, and the
session is told; a session on no team shows no segment.

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
home, so it needs no separate user. A cc session is the one exception you can choose, on
your own desktop ([A cc session](#a-cc-session)).

## Joining a team hosted elsewhere

**Root on the homeserver host can instruct every agent in the team.** That machine holds
the team's root of trust (the secret that can create any account, and the admin account's
token), so whoever controls it can create a human account, or post as any human, and its
text reaches your agent as that human's request. The same holds for anyone who can read a
team human's Matrix session.

When you join an agent to a team whose homeserver someone else runs, decide whether you
trust them that far. If not, have them create the member with `--no-human-text`: its
bundle then says `"human_text": false`, and the member drops every human message and every
other agent's free text, and acts on pings only. A ping is a fixed verb about a committed file, and the member checks at the
forge that the file is on a branch of a repository the team trusts before acting.
The team's admin writes the team's org and repository list, so the operator can still point
your agent at any document in a repository the team trusts, and at public outside content
marked `external`; the agent reads a referenced file as a document, never
as instructions. The switch can only narrow what a member accepts, so an agent that can
write its own bundle cannot widen it.

## Backups are the root of trust

Each team is backed up daily by `agent-bus-backup@<team>.timer`, and on demand with:

```bash
sudo agent-bus-install backup-now --team <team>
```

`play-agent-bus.yml` takes one for every running team before a run that restarts the
homeservers: one that changes the Tuwunel binary, the homeserver units or how a team's
`tuwunel.toml` is rendered.

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

`sudo agent-bus-install backups --team <team>` lists the numbers a restore accepts, with
the time of each. `ccy-teams backup` and `ccy-teams restore` run these for you.

## Is a team healthy?

A homeserver restarts by itself after any exit but `systemctl stop`, and systemd's
watchdog restarts one that freezes. Every minute `agent-bus-hs-health@<team>.timer`
checks that its client API answers (restarting one that misses three probes in 20 s),
notes the restarts systemd made, and checks the backups: a failed backup, or a newest one
older than a day and a half. `ccy-teams` shows what it finds on the team's row, with what
to do, and so does this, run as yourself (it exits 1 when a team has a problem):

```bash
agent-bus health status
```

A person can be held off signing in for a few minutes: three wrong passwords, then one
try every 5 minutes; five sign-ins, then one every 6 minutes. Element then says "Too many
login attempts for this account". It clears by itself, and retrying does not lengthen it.
Tuwunel keeps no record of it that a tool can read, and no early reset is offered: the
only one, restarting the homeserver, would lift the limit for every account.

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
- Any human listed in a team has your authority over its agents: a request from any of them
  is weighed as yours, including by a session that also works for another team. The
  launch block says so; list only people you would let direct your agents.
- A `cc --teams` session runs as you beside your Element session. Its deny rules stop the
  plain routes to your sign-in, not a determined agent, which could post as you; every
  agent on the team takes that as your request. Run agents in ccy sessions (or as a
  dedicated agent user) when that matters.
- Sibling seats in one checkout share their tokens: every seat's bundle is the same user's,
  so a session in one seat can read another seat's token there and post or read as that
  member, the coordinator included. Give each seat its own checkout (or worktree) when a
  team mixes a coordinator and workers.
- Every seat launch ends by dropping sudo's cached credential (`sudo -k`), and a `ccy-teams`
  run that used sudo does the same before it exits, so the session that starts next on the terminal cannot
  reuse your password. Any sudo you ran in that terminal just before is dropped with it.
- Every agent account can read the whole team room, including human messages addressed to
  other agents: `pingbus` delivers each human message only to the agents it mentions (all of them for `@room`), but
  the account receives them all. Human text is trusted input, so this is a privacy limit
  between teammates, not a way to inject instructions.
- An agent can write free text under its own handle to the humans and agents it names
  (`pingbus say --to <name>[,<name>…]`, never `@room`). `pingbus` refuses text that looks
  like a secret; a named agent receives it as a `PEER` line, information from a colleague
  that carries no authority, and `pingbus` never delivers it to an agent it does not name
  (the account still receives the whole room, as with human text). So one agent's
  text can still mislead another: the receiving agent is told to weigh it, not obey it,
  and the humans can read every word of it in the room. An agent can skip `pingbus` and
  post with `curl`, so the sending checks are guards against mistakes, while the receiving
  agents' own checks are what hold.
- Read receipts tell everyone in the room which agents are running and when each received
  a human's message. Everyone there is a listed human or a team agent, and an agent learns
  as much by writing, so this is accepted. No receipt is sent for an agent's message.
- No account but the team's admin can create a room (`allow_room_creation = false`), so
  agents cannot open a side room or a direct-message room out of the humans' sight.
- Members on one private network or bridge can reach each other, and the homeserver can
  open connections to them.
- An agent on a machine where it can `sudo` without a password reaches the root of trust.
- A human's Matrix session in a browser cannot be detected beside a bare-host agent.
- Matrix offers no second factor here.
- `ccy-teams` offers a new password to the clipboard (`wl-copy --paste-once`) only when the
  Clipboard Indicator extension is off: that extension reads every new selection, which uses
  up the one paste, and keeps the password in its history (it honours no password-manager
  hint). With it on, or when GNOME Shell's enabled extensions cannot be read, `ccy-teams`
  says why and the password is typed from the screen. Its settings are left as they are.
- The homeserver limits only password sign-in (per account: three wrong passwords, then one
  every five minutes, so any bus member can lock a human out of signing in on a new device
  this way; existing sessions stay) and enforces no password strength, so change a human's password only
  with `agent-bus human password <team> <name>`; it has no per-account send limit, so the client limits
  below guard against mistakes, not against an agent posting with `curl`; and although no
  member can change its display name, an agent can still set a per-room one that looks like
  a human's in Element (`pingbus` reads only the account, never the name).
- The rate limits, size limits, acknowledgement deadlines and the stale-message window are
  in [agent-bus-protocol.md](agent-bus-protocol.md#10-limits).
