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
| The team admits that source address (the LXC bridge `lxcbr0` always is; another bridge needs `allow_from`)                                        | The team file                                                           | `team <team>'s allow_from (...) does not cover LXC container ... add "<cidr>" ...`                                          |
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

1. **Declare the team** in `agent_bus_teams` in the desktop's `localhost.yml` (or with
   `ccy-teams create`). Nothing in it is needed for LXC: the team play reads the subnet of
   the LXC bridge this repository sets up (`lxcbr0`) from the live host and admits it for every team, so `allow_from` stays for other
   machines reached over WireGuard. Then apply the team:

   ```bash
   ccy-teams apply <team>
   ```

   The full team file is described in [agent-bus.md](agent-bus.md); `localhost.yml.dist`
   shows an example entry.

2. **Add the member from ccy-teams.** This is the way to do it: no file is edited by hand.
   In `ccy-teams`, Enter on any row of the team and choose *Add an LXC container's agent to
   `<team>`* (it has no shortcut key), or run:

   ```bash
   ccy-teams lxc-add <team>
   ```

   It asks for sudo (LXC is rootful), lists the host's running containers and asks for the
   container, the agent user (the container's ordinary users are suggested; a user already
   in another team is fine), the checkout inside it and the role (`worker`; `coordinator`
   only while the team has none). Every question shows the team, the step, and the container
   and user once chosen. The checkout is typed on an editable line, with Tab completing a
   directory name from the container's tree as a shell does (the one directory a name starts,
   or what several share, with those listed), and a hint line under it saying so. The git
   checkouts with a GitHub remote that a bounded search found are suggested under the line,
   and it starts with the first: the search looks in the user's home and in `/var/www`, `/srv`
   and `/opt` (for example a site under `/var/www/vhosts/<site>/`), up to four directories
   deep, ccy projects (those with a `.claude/ccy` directory) first; dependency trees such as
   `node_modules` and `vendor` are skipped, a root that is missing is passed over, one with a
   symbolic link on its way is named and not followed, and a search stopped by its time or
   count limit says it may have missed some. Every line wraps to the terminal, never cut off.
   The arrows put a suggestion in the line; Enter takes the line, Esc cancels the run. Every listing
   is read from the host's side. A container on LXC's `dir` or `btrfs` store is read
   as the directory its rootfs is; one on `overlay`, `zfs`, `lvm`, `loop`, `rbd` or `nbd`
   through the running container's own root (`/proc/<init pid>/root`); any other store is
   refused, naming it. It never asks for the handle's repo, host or seat: they are derived from the
   checkout, as for a ccy seat (below). Each answer is checked as it is
   given by the rules the play applies, and asked again when refused (a duplicate, a second
   coordinator, a relative checkout, a path that is not a git checkout). It then shows a
   summary with the derived handle, asks once, writes the
   `agent_bus_lxc_members` entry (the file as it was is kept beside it as
   `localhost.yml.backup.<time>`) and applies the team, which is step 3. It ends by saying
   what is left inside the container: `gh auth login` as the agent user if gh is not logged
   in (the play stops naming it when it is not; fix that, then `ccy-teams apply <team>`), and
   `agent-bus-claude` (the kit's launcher) as that user in the checkout. It looks, read only,
   for a Claude session of that user already running in the container: one started before
   the add has not joined, and is named with when it started, to be ended and started again.
   It also prints the check to run there, which reads the team's room live as the member:
   `set -a; . ~/.config/pingbus/env; set +a; pingbus config check && pingbus history --limit 1`
   (a login shell has not read the env file, which only `agent-bus-claude` reads, so
   `pingbus` alone refuses, saying this). The container and user must already exist; this
   never makes either.

   **Or declare the member by hand** in `agent_bus_lxc_members` in the same file, which is
   what the command writes:

   ```yaml
   agent_bus_lxc_members:
     - team: <team>
       container: <container>
       user: <agent-user>
       checkout: /home/<agent-user>/<repo>
   ```

   Those four keys are all an entry needs. The handle is derived (see **The handle** below).
   The optional keys are `host` and `seat` (overrides), `role`, `human_text`, `repo`,
   `state` and `service`; the comments in `localhost.yml.dist` describe each.

   **`service: true` keeps the session running by itself.** The play installs
   `agent-bus-claude-<agent-user>.service` inside the container: it runs `agent-bus-claude`
   as the agent user in `checkout`, under tmux, and systemd restarts it after a crash, a
   container restart or a host reboot. The play refuses the entry, before changing anything,
   unless the container starts at host boot (`lxc.start.auto = 1` in its config), runs
   systemd as its init and has tmux. To watch or type into the session:
   `sudo lxc-attach -n <container> -- runuser -u <agent-user> -- tmux -L agent-bus-claude attach`.
   Leave it out for a member someone starts by hand; turning it off, or the user leaving its
   last team, stops and removes the unit.

3. **Run the play**, or let `ccy-teams apply <team>` run it for that team:

   ```bash
   ./playbooks/imports/play-agent-bus.yml
   ```

   (From the desktop's own checkout; a container that works in that checkout hands it to the
   host through `CLAUDE/Plan/meta-deploy.bash`.) The play installs the kit in the container,
   joins the member, writes `~/.config/pingbus/env` with `PINGBUS_TEAMS=<team>` and
   `HOOKS_DAEMON_HOSTNAME=<role>`, and runs `pingbus config check` as the agent user. A run
   with nothing new reports no change.

**The handle** is `<repo>.<seat>+<host>.lxc`, derived from the repository as ccy derives a
podman seat's ([agent-bus.md](agent-bus.md)), reading the checkout in the container:

- `<repo>` is the last component of the checkout's `origin` (`.git` removed), else the
  directory's name;
- `<host>` is the `HOOKS_DAEMON_HOSTNAME` that the checkout's `.claude/ccy/ccy.env.local`
  assigns, else the literal `local`; never the machine's hostname, because handles appear in
  public forge text;
- `<seat>` is the lowest number that no active member of the same `<repo>+<host>.lxc` holds in
  the team; a parked member's number is offered again, so it returns. An active numbered
  member that no entry holds is not skipped: see **A lost bundle**, below.

The host reads `<repo>` and `<host>` and the play mints the handle from them. The container's
own `pingbus suggest-handle` is only held to the same answer: if it names another repo or
host, the play refuses and mints nothing.

The checkout is read from the host's side, as root, through the container's LXC config (its
rootfs and the bind mounts the config declares). A `dir` or `btrfs` rootfs is a directory on
the host, so a stopped container is read too; the other stores (`overlay`, `overlayfs`, `zfs`,
`lvm`, `loop`, `rbd`, `nbd`) are mounted only inside the running container, so they are read
through its init process's root (`/proc/<pid>/root`, the pid from `lxc-info`), which needs it
running; any other store is refused, naming it. Nothing is run there: `.git/config` and `ccy.env.local` are read as plain files, no
git is started (a repository config can name programs to run), every path component is opened
without following a link and a link anywhere is refused, and each file is capped at 64 KiB. A
container with an `lxc.idmap` (unprivileged) is refused.
Git section names are read without regard to case (`[Remote "origin"]`), and a url may be
quoted as a whole. `git` would apply a `url.<base>.insteadOf` rewrite to the origin, which
can change the repository's name; this does not apply it, so a checkout whose `.git/config`
has one is refused (remove the rewrite, or give the entry `repo`, `host` and `seat`). A rewrite
in the agent's own `~/.gitconfig` cannot be seen from the host; it shows as a disagreement with
the container's `pingbus suggest-handle`, which is refused too. An `optional` bind mount whose
source is absent is skipped, as LXC skips it; any other missing source is refused by name.

**The handle stays fixed.** A member that has joined is left alone on later runs, so its token
does not change; a lost bundle is replaced with a new token for the same handle. An entry that
leaves `seat` out keeps the seat its bundle in the container names, so a run again never
renames it.

**Entries written before host and seat were derived** keep their handles, and nothing is renamed
silently. An entry that gives `host` and `seat` is used as it is, and the checkout is not read.
An entry that gives `host` but not `seat` keeps the seat its bundle names; with no bundle, the
seat the container's name gave before (lower case, letters and digits, at most 12, a letter
first) when a member of that handle is already in the team; otherwise it is derived like a new
one. `host` and `seat` stay valid as overrides. They are part of the handle: to change either,
set the entry `state: absent`, run the play, then declare it again.

**A lost bundle.** If a member's bundle is gone while it is still active, the play does not
give the container a new handle: that would leave the old one active with a token nobody
holds. It refuses, naming the handle, whenever an active numbered member of the same
`<repo>+<host>.lxc` is held by no entry (by an explicit `seat`, or by a bundle in another
declared container). To take that handle back, set the entry `state: absent`, run the play
(which parks it), then declare it present again: it returns with a new token. To leave it
alone, give the entry another `seat`. An explicit `seat` equal to that handle is refused the
same way. A member joined by hand looks the same as a lost bundle, so it is refused the same way.

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
   `SessionStart`, `UserPromptSubmit`, `Stop` and `SessionEnd` hooks (`pingbus hook session-start`, `prompt`, `stop`, `session-end`) and the pingbus skill. `SessionEnd`
   posts the goodbye ([agent-bus.md](agent-bus.md)). The settings file holds one setting,
   `"crossSessionInbound": "accept"`, without which a bypass-permissions session holds the
   bus's deliveries instead of starting a turn (the watcher then reports them missing).

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
