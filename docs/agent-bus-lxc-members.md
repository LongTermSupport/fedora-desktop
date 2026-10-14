# Agent team bus: LXC containers made by another repository

This page is for a repository that creates and fills LXC containers (its own Ansible, its
own launcher) and wants the Claude Code agent inside a container to be a member of an
[agent team](agent-bus.md) hosted on a fedora-desktop host. It says what the provisioner
must provide, what the desktop does, and how a session in the container starts as a member.
Names below are placeholders: `<team>`, `<container>`, `<agent-user>`, `<role>`, `<repo>`,
`<bridge>`.

## Who does what

The route this page recommends splits the work so the member's token never leaves the
desktop's tooling:

| The fedora-desktop host does                                                                                                                                 | The provisioner repository does                                                                 |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------- |
| Hosts the team and mints the member: an account and a token, together the **bundle**.                                                                        | Creates the container and starts it.                                                            |
| Pushes the member kit, the bundle and the env file into the container (`lxc-attach`, as root and as the agent user) and puts the bundle in the agent's home. | Creates the agent user and its checkout; installs Python, Claude Code, gh and a few base tools. |
| Checks the container before it changes anything, and runs `pingbus config check` in it on every run.                                                         | Logs gh in to github.com as the agent user.                                                     |
| Keeps the member's handle fixed, and parks it when the member leaves.                                                                                        | Starts the agent's sessions with the bus switched on ([Starting sessions](#starting-sessions)). |

The desktop's side is `agent_bus_lxc_members` in the desktop's `localhost.yml`, applied by
`play-agent-bus.yml` (`helpers/agent_bus_lxc/members.py`). It never creates a container or a
user, never installs gh and never supplies a token for gh. A container that is missing
something stops the play with a message naming it; nothing is changed in any container until
every declared container has passed the checks.

For a container on another machine the desktop cannot reach with `lxc-attach`, join it by
hand: `README.lxc` in the member kit (`/usr/local/share/agent-bus/kit/` on the desktop, or
`files/opt/claude-yolo/optional/agent-bus/README.lxc` in the fedora-desktop repository) lists
the steps, and the bundle is made with `sudo agent-bus add-member` on the homeserver host
and carried across. That route moves a token between machines, so prefer the play wherever
the container is on the desktop.

## What the provisioner must provide

The play checks these in this order, for every declared container, before it changes
anything. The "stops with" column is the message to look for; the play exits 78 and prints it
to stderr.

| Prerequisite                                                                                                                                      | Checked by                                                              | A failure says                                                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| The container exists, in the desktop's LXC (`lxc-ls`)                                                                                             | `lxc-ls`, `lxc-info`                                                    | `LXC container <container> does not exist: this play joins existing containers, it never makes one`                         |
| It is running                                                                                                                                     | `lxc-info -n <container> -sH`                                           | `LXC container <container> is STOPPED: start it, then run the play again`                                                   |
| The agent user exists (`getent passwd` gives a home)                                                                                              | `lxc-attach`                                                            | `LXC container <container> has no user <agent-user>: make the agent user in the container first`                            |
| gh is installed and logged in to github.com as the agent user                                                                                     | `gh auth token --hostname github.com`, as the agent user                | `LXC container <container>: gh is required and must be logged in for <agent-user>: install gh in the container and run ...` |
| The container reaches the homeserver host's bus address through an LXC bridge of the desktop                                                      | The container's source address towards the bus address                  | `... is on no network of this host` or `... reaches <bus> through <dev>, which is not a bridge of this host`                |
| The team's `allow_from` covers that source address                                                                                                | The team file                                                           | `team <team>'s allow_from (...) does not cover LXC container ... add "<cidr>" to allow_from ...`                            |
| `python3` 3.11 or later, on `/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin` for root (the play runs `python3 -I` in the container) | The kit sync, as root                                                   | `pingbus needs Python 3.11 or later; this container has <version>`                                                          |
| `runuser` (util-linux) and GNU `env` with `-C` (coreutils 8.28 or later)                                                                          | Every command run as the agent user is `runuser -u <user> -- env -i -C` | The failing command's own error, as `container <container>: ... failed (exit N)`; there is no separate version probe        |
| `git`, and a checkout of a GitHub repository (an `origin` remote on github.com) at the declared `checkout` path                                   | `pingbus suggest-handle`, run as the agent user in the checkout         | `suggest-handle` refuses with its own message; a checkout that is not the declared `repo` is refused naming both            |
| `systemd-detect-virt`, reporting an `lxc` container                                                                                               | `pingbus suggest-handle`                                                | `suggest-handle says --type=<type>, not lxc: the agent does not run in an LXC container` (or "needs systemd-detect-virt")   |
| Claude Code (`claude`) on the agent user's `PATH`                                                                                                 | `agent-bus-claude`, at session start; the play does not check           | `agent-bus-claude: claude is not on PATH`                                                                                   |
| No Element profile in the agent user's home                                                                                                       | `agent-bus-claude` and `pingbus config check`                           | `this user has an Element profile (...); an agent must never run as a user that holds a human's Matrix session`             |
| The kit directory `/usr/local/share/agent-bus` is root-owned and not group- or world-writable, if it already exists                               | The kit sync                                                            | `<path> is not owned by uid 0 or is writable by others` (a kit copied by hand must go before the play installs it)          |
| `/usr/local/bin/pingbus` and `/usr/local/bin/agent-bus-claude` are absent, or already links to the kit                                            | The kit sync                                                            | `<link> exists and is not a link to <kit>/<name>; README.lxc step 1 links it`                                               |

Notes on the table:

- The play reaches into the container with `lxc-attach -n <container> --clear-env`, as root
  on the desktop, so the container must be one the desktop's `lxc-*` tools can see. It does
  not use SSH, and it needs no network route from the desktop into the container.
- The agent user needs no password and no sudo. It must be a real login-style account with a
  home directory; the bundle goes to `~/.config/pingbus/<team>/`.
- gh's login may be a stored login or `GH_TOKEN`/`GITHUB_TOKEN` in
  `~/.config/pingbus/env`. The play reads that file (when it exists) before it checks gh.
- Python is the only language runtime the member kit needs. The play installs the kit as
  files and two links in `/usr/local/bin`; it installs no packages.

## The desktop side

1. **Declare the team** in `agent_bus_teams` in the desktop's `localhost.yml`, with
   `allow_from` listing the subnet of the LXC bridge the container is on. Find it on the
   desktop:

   ```bash
   ip -4 addr show <bridge> | cat
   ```

   The `inet` line gives the address and prefix, for example `192.0.2.1/24`; the subnet to
   list is its network, `192.0.2.0/24`. If the list is missing the subnet, the play stops
   and names the one to add, so you can also let it tell you. Then apply the team:

   ```bash
   ccy-teams apply <team>
   ```

   The full team file is described in [agent-bus.md](agent-bus.md); `localhost.yml.dist`
   shows an example entry.

2. **Declare the member** in `agent_bus_lxc_members` in the same file:

   ```yaml
   agent_bus_lxc_members:
     - team: <team>
       container: <container>
       user: <agent-user>
       host: <role>
       checkout: /home/<agent-user>/<repo>
   ```

   `host` is the install's role, which becomes the handle's `<host>` part. It is a role name
   (for example `worker`), never a machine's hostname: handles appear in public forge text.
   The optional keys are `seat`, `role`, `human_text`, `repo` and `state`; the comments in
   `localhost.yml.dist` describe each.

3. **Run the play**, or let `ccy-teams apply <team>` run it for that team:

   ```bash
   ./playbooks/imports/play-agent-bus.yml
   ```

   (From the desktop's own checkout; a container that works in that checkout hands it to the
   host through `CLAUDE/Plan/meta-deploy.bash`.) The play installs the kit in the container,
   joins the member, writes `~/.config/pingbus/env` with `PINGBUS_TEAMS=<team>` and
   `HOOKS_DAEMON_HOSTNAME=<role>`, and runs `pingbus config check` as the agent user. A run
   with nothing new reports no change.

**The handle stays fixed.** The member's handle is `<repo>.<seat>+<host>.lxc`. The seat
defaults to the container's name (lower case, letters and digits, at most 12, a letter
first); set `seat` when the name does not give one. A member that has joined is left alone on
later runs, so its token does not change; a lost bundle is replaced with a new token for the
same handle. `seat` and `host` are part of the handle: to change either, set the entry
`state: absent`, run the play, then declare it again.

**Leaving.** `state: absent` parks the member's handle in the team, deletes the bundle from
the container and takes the team out of `PINGBUS_TEAMS`. A container that is already gone
needs `repo: <repo>` on its entry so the play knows which handle to park. The play never
removes the kit or the container.

## Starting sessions

The play does not start a session. Today the way to start one as a member is
`agent-bus-claude`, which the play links into `/usr/local/bin`. As the agent user:

```bash
agent-bus-claude            # the arguments are passed on to claude
```

What it does, in order (the script is `files/opt/claude-yolo/optional/agent-bus/agent-bus-claude`):

1. Refuses (exit 78) when the user has an Element profile in its home.

2. Sources the env file (`~/.config/pingbus/env`, or the file `PINGBUS_ENV` names) with every
   assignment exported. It must set `PINGBUS_TEAMS`; `PINGBUS_HOME` defaults to
   `~/.config/pingbus`.

3. Puts the kit directory first on `PATH` (the plugin's hooks run `pingbus`), then runs
   `pingbus config check`, whose refusal ends the launch with its exit code. That check also
   needs gh logged in.

4. Replaces itself with:

   ```bash
   claude --plugin-dir <kit>/plugin/pingbus --settings <kit>/settings.json "$@"
   ```

   where `<kit>` is `/usr/local/share/agent-bus/kit`. The plugin supplies the
   `SessionStart`, `UserPromptSubmit` and `Stop` hooks (`pingbus hook session-start`,
   `prompt`, `stop`) and the pingbus skill. The settings file holds one setting,
   `"crossSessionInbound": "accept"`, without which a bypass-permissions session holds the
   bus's wake notice instead of starting a turn.

**If the provisioner has its own launcher** that starts Claude Code under the hooks-daemon
supervisor, there are two ways to combine it with the bus.

- **Make `agent-bus-claude` the `claude` the launcher runs.** It takes the launcher's
  arguments, adds the two flags above and `exec`s the real `claude`, so the supervised
  process is the same process. This works if the launcher can be told which command is
  `claude`. The environment the launcher gives it is what the env file is read on top of.
- **Do what it does yourself**, in the launcher before `claude` starts: export the env
  file's assignments (`PINGBUS_TEAMS`, `HOOKS_DAEMON_HOSTNAME`, and `PINGBUS_HOME` if it is
  not the default), put `/usr/local/share/agent-bus/kit` first on `PATH`, run
  `pingbus config check` and stop if it fails, and pass `--plugin-dir /usr/local/share/agent-bus/kit/plugin/pingbus` and the setting
  `"crossSessionInbound": "accept"` (the kit's `settings.json`, or merged into the
  launcher's own settings) to `claude`. These are the only things `agent-bus-claude`
  adds. Whether `claude` accepts the launcher's own `--settings` beside a second one is
  Claude Code's rule, not this repository's; merge the setting into one file if it does not.

Either way the supervisor is not involved in the bus: the pingbus plugin's hooks and the
watcher they start deliver messages to the session. An agent driven by a script
(`claude -p` in a loop) needs neither flag: its driver runs `pingbus wait` between turns.

**Delivery by the supervisor is designed, not built.** [Plan
00194](../CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/PLAN.md) describes a
hooks-daemon supervisor plugin, `ccy-agent-bus`, that would put a team human's message into a
running session at once, framed as the human's, and an agent's message framed as a
colleague's note. Nothing of it is built, and it does not ship in the member kit today: when
it is, it ships in the kit too (a task of that plan), so a supervised member outside ccy
gets the same delivery. Until then, the plugin's hooks are the whole delivery in a
container.

## Verifying

As the agent user in the container (for example `lxc-attach -n <container> -- runuser -u <agent-user> -- ...` on the desktop, with the env file's variables set):

```bash
set -a; . ~/.config/pingbus/env; set +a
pingbus config check
pingbus status
```

- `pingbus config check` prints one `OK<TAB><team><TAB><handle><TAB>human_text=<true|false>`
  line per active team and `FORGE<TAB>gh`. A refusal exits 78 with the reason on stderr.
- `pingbus status` prints, per team, a `TEAM` line (handle, `trust=trusted`, `wake=...`,
  counts of pending items) and a `MEMBER` line for each member of the team. `trust=untrusted`
  with an `UNTRUSTED` line gives the reason.
- Start a session with `agent-bus-claude` (or your launcher), then post a message in the
  team's Element room that mentions the member's handle. The session should receive it as the
  human's request when it next takes a turn (or wakes, for an idle session).

If the play, not the member, is the problem, run it again: it is safe to repeat, and it
reports what it changed with `CHANGED` lines.
