# Proposal: team members reached over plain SSH

Research for [Plan 00205](PLAN.md). Nothing here is built. Names are placeholders:
`<team>`, `<agent-user>`, `<admin-user>`, `<dest>` (an ssh destination: `user@host` or an
ssh-config alias), `container-A`, `<repo>`, `<role>`.

## 1. The question, and the short answer

ccy-teams joins an agent user inside an LXC container to a team by reaching into the container
with `lxc-attach` as root on the host (`helpers/agent_bus_lxc/members.py`, driven by
`play-agent-bus.yml`; `ccy-teams lxc-add` writes the entry). The owner already reaches the same
containers with plain `ssh` as the right user. Could a member be reached over ssh instead, and
if so, does the tooling need to know how the member is virtualised (LXC, VM, another physical
host)?

**Short answer: no, the transport does not need to know.** Every step members.py runs inside the
container is "run this command as root" or "run this command as the agent user, with this
stdin". ssh does both for any machine. Three things in today's flow are tied to LXC, and none of
them is needed for an ssh member:

| LXC-specific today                                                       | Why it exists                                                                                  | For an ssh member                                                                                                                                               |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Reading the checkout from the host's side through the container's rootfs | So root on the host never executes anything the container controls; it also works when stopped | Read it inside the member **as the agent user**: no privilege is crossed, so the reason goes away. Working while stopped is lost (section 4.2)                  |
| `lxc-ls`/`lxc-info` state and `lxc.start.auto` for `service: true`       | A clear "stopped" message; a supervised session must come back after a host reboot             | "Unreachable" replaces "stopped". Whether the machine itself boots is outside ssh's view: a stated requirement, checked only where an adapter can (section 4.5) |
| The source address must be on a bridge **of this host**                  | A local container is routed through `lxcbr0`, which every team admits                          | Keep only the part that matters: the member's source address towards the bus address is inside the team's `allow_from` (section 4.4)                            |

The handle's `<type>` (`lxc`, `vm`, `host`, `docker`) is not the transport either. It is part of
the handle (`<repo>.<seat>+<host>.<type>`, `helpers/pingbus/protocol.py` `HANDLE_TYPES`), and the
member already reports it about itself: `pingbus suggest-handle` runs `systemd-detect-virt`
inside the member. members.py today holds that answer to `lxc` and refuses anything else. An ssh
member would take the member's own answer, record it at the first join, and refuse a change
later, because a changed type is a changed handle. No new `ssh` type: the type says where the
agent runs, never how the desktop reached it.

## 2. What the LXC flow does today, step by step

From members.py (`run_present`, `_join`, `LxcGuest`) and `src-go/internal/lxcfreeze/lister.go`:

| Step                                 | How (LXC)                                                                                                   | Runs as                   | Over ssh                                                                                       |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------- | ------------------------- | ---------------------------------------------------------------------------------------------- |
| List containers (lxc-add)            | `sudo lxc-ls -1`, `--running`                                                                               | host root                 | Not needed: the owner names `<dest>` (suggest `Host` aliases from the owner's `~/.ssh/config`) |
| List users (lxc-add)                 | `lxc-attach ... getent passwd`                                                                              | container root            | Not needed: the ssh login *is* the user                                                        |
| Exists / running                     | `lxc-ls`, `lxc-info -sH`                                                                                    | host root                 | `ssh -o BatchMode=yes <dest> true`: reachable or not                                           |
| Home                                 | `getent passwd <user>`                                                                                      | container root            | `$HOME` of the login (or `getent passwd "$(id -un)"`)                                          |
| gh logged in                         | `runuser -u <user> -- env -i ... sh -c GH_CHECK_SH`                                                         | agent user                | the same `sh -c GH_CHECK_SH`, as the login                                                     |
| Route check                          | `SOURCE_PY` in the container, then the host's `ip -j addr` and `/sys/class/net/<dev>/bridge`                | container root, host      | `SOURCE_PY` in the member; only the `allow_from` test stays                                    |
| Read the checkout (derive repo/host) | `python3 -I -c ROOT_READ_PY probe <lxcpath> <container> ...` **on the host**, through the LXC config/rootfs | host root                 | a reader run in the member as the agent user (section 4.2)                                     |
| Kit sync                             | `KIT_SYNC_PY` with the kit tar on stdin                                                                     | container root            | the same, as root or `sudo -n` (section 4.6), or a per-user kit                                |
| `suggest-handle` agreement           | `pingbus suggest-handle` in the checkout                                                                    | agent user                | the same; `--type` is now recorded, not required to be `lxc`                                   |
| Bundle: observe, remove, place       | `OBSERVE_PY`, `REMOVE_PY`, `PLACE_PY` (bundle tar on stdin)                                                 | agent user                | the same, stdin over the ssh channel (section 4.3)                                             |
| Mint / park / set-role               | `agent-bus add-member --out=-`, `park-member`, `set-role`                                                   | host root                 | unchanged: the homeserver host's side                                                          |
| Env file, `pingbus config check`     | `WRITE_ENV_PY`, `CONFIG_CHECK_SH`                                                                           | agent user                | the same                                                                                       |
| `service: true`                      | `lxc.start.auto`, `SERVICE_NEEDS_SH`, `UNIT_WRITE_PY`, `systemctl`                                          | host root, container root | system unit with root in the member, or a user unit with linger (section 4.5)                  |

`members.py` already separates the transport: `Host.guest` returns an object with `home`,
`root(argv, what, stdin)` and `agent(user, home, cwd, argv, what, stdin, env)`, and `LxcGuest`
is the only implementation. Every script is passed as `python3 -I -c SCRIPT ARGS`. An `SshGuest`
with the same three methods covers all of the "container root" and "agent user" rows. The
host-root rows change for an ssh member, and are listed above.

## 3. Does an ssh member need to know how it is virtualised?

No. Following each LXC-specific dependency in turn:

- **Transport.** ssh reaches an LXC container, a VM, a docker container running sshd, or another
  physical machine the same way. The destination may be an ssh-config alias, so ProxyJump, a
  non-standard port and per-host keys stay in the owner's ssh config and need no new settings.
- **Handle type.** Reported by the member (`systemd-detect-virt`), recorded at the first join
  and checked on later runs.
- **Network admission.** `allow_from` is about the source address the homeserver sees, which
  the member computes itself (`SOURCE_PY`). The "bridge of this host" refinement only helps a
  local container's error message. A member that reaches the bus over passt (user-mode
  networking) shows the host's own address, which every team allows (`README.vm`).
- **Starting at boot.** The one place knowing the virtualisation would add a check: for a local
  LXC container `lxc.start.auto`, for a libvirt guest `virsh dominfo` autostart, for a physical
  host nothing. This is an optional adapter keyed on the type the member reports. It is not a
  transport concern (section 4.5).

What *is* lost by not knowing: reading a stopped member, and telling "stopped" from "sshd down"
from "network down". Section 6 lists these.

## 4. Impact, area by area

### 4.1 members.py and the ccy-teams LXC flow

- **The entry.** Today's entry is keyed by `container` (an LXC name, `_CONTAINER_RE`) and `user`.
  An ssh entry is keyed by `ssh: <dest>` instead of `container`, and `user` becomes the login
  user (checked against `id -un` on the first call, so an alias that logs in as someone else is
  refused). The rules between entries (`_check_between`) carry over with `<dest>` in place of
  the container: one entry per destination, user and team; one host per destination and user
  (one env file); one destination per team, host and seat.
- **One executor, two transports.** `run_present` and `_absent_one` stay. `Host` gains a
  per-entry way in: `LxcGuest` for `container:` entries, `SshGuest` for `ssh:` entries. The
  LXC-only probes (`container_state`, `autostarts`, `read_checkout`, `is_bridge`) move behind
  the transport, with the ssh versions described here.
- **Python on the member.** Unchanged: every script runs as `python3 -I -c`, and the kit needs
  3.11 or later in the member anyway.
- **ccy-teams.** `lxc-add` becomes one of two paths: "Add an agent reached over ssh" asks for
  `<dest>` (offering `Host` aliases from the owner's ssh config), the checkout (suggested by the
  reader's `checkouts` mode, run in the member) and the role. No container list, no users list,
  and **no sudo for reading**, because the ssh probes run as the owner. `lxcfreeze.Lister` stays
  for LXC. The apply step still runs the play (section 4.6 says who runs ssh then).
- **The play.** `play-agent-bus.yml` calls members.py twice (absent before teams change, present
  after). An ssh member list would be passed the same way. One ordering matter: `lxcbr0`'s
  subnet is added to `allow_from` automatically, but a remote member's subnet (WireGuard) is
  declared, never inferred.

### 4.2 The Checkout step: what replaces reading the rootfs as root

Today `read_checkout` runs `derive.ROOT_READ_PY probe` on the host as root. It parses the
container's LXC config for `lxc.rootfs.path` and `lxc.mount.entry` binds, then reads
`/etc/passwd`, `<checkout>/.git/config` and `<checkout>/.claude/ccy/ccy.env.local` as plain
files: no git, no links followed, every size capped. `parse_probe` and `identity_of` judge the
result. ccy-teams uses the same script through `sudo -n python3 -I -c`
(`lxcfreeze.Lister.ReadCheckout`).

Why it was built that way (Plan 00177 Task 9.7, the owner's safety rules): **root on the host
must not execute anything the container controls.** A repository's config can name programs
to run (`core.fsmonitor`, hooks). It also works while the container is stopped. The *content*
it reads is the container's either way. The host-side read never made the repo name more
trustworthy, only safer to obtain.

Over ssh, the read runs **as the agent user, inside the member**. Nothing crosses a privilege
boundary, so the reason for the host-side read is gone. The options:

| Option                                                                                                                                                                                                                   | For                                                                                                                            | Against                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------- |
| **C1.** The reader's file-reading half, without the LXC config parse: given a checkout path, print the same probe JSON (`path`, `uid`, `git_config`, `env_uid`, `env_local`), run with `python3 -I -c` as the agent user | Same JSON, so `parse_probe`, `identity_of` and `derive_seat` are reused unchanged; still runs no git; one rule for LXC and ssh | A second entry point to keep beside the rootfs one (one template, two prologues)                           |
| **C2.** Trust `pingbus suggest-handle` alone (runs git as the agent user)                                                                                                                                                | No new code; git applies `insteadOf` correctly, which C1 refuses                                                               | The answer is computed by code in the member's kit, which the member can change; loses the agreement check |
| **C3.** Require `repo`, `host` and `seat` on every ssh entry                                                                                                                                                             | Nothing read at all                                                                                                            | Back to hand-written handles, which Plan 00177 Task 9.7 removed at the owner's request                     |

**Recommendation: C1, with C2 kept as the agreement check**, exactly as today: the reader
derives, `suggest-handle` must agree, and a disagreement is refused. The `checkouts` mode
(suggestions for ccy-teams) moves into the member the same way.

What is lost: deriving a handle for a member that is down. The LXC path can read a stopped
container. An unreachable ssh member cannot be read, so its first join waits until it is up.
Later runs need no read: an entry whose bundle names its handle keeps it.

### 4.3 Seat token delivery into the member

Today: `agent-bus add-member ... --out=-` prints the bundle (`member.json`, `token`, `README`)
as a tar on stdout, members.py holds it in memory, and pipes it into
`lxc-attach ... runuser -u <agent-user> -- env -i ... python3 -I -c PLACE_PY` on stdin. The
token never touches the host's disk, never appears in an argv and never passes through Ansible
variables.

Over ssh the shape is the same: the tar goes on the stdin of `ssh <dest> -- env -i ... python3 -I -c PLACE_PY <base> <team>`. The ssh channel encrypts it, and `PLACE_PY` writes the
files `O_EXCL`, 0600, in a 0700 directory as the login user. What to hold to:

- stdin only, never an argv or an environment variable (a remote `ps` would show an argv);
- ssh run with `-o LogLevel=ERROR` and `-T` (no tty: a tty would echo stdin);
- a remote login shell's startup files can print. bash runs `~/.bashrc` even for a
  non-interactive ssh command. `PLACE_PY` reads only its own stdin, but any parsed stdout
  (`OBSERVE_PY`'s JSON, `SOURCE_PY`) must tolerate or refuse noise. Refusing, naming the
  startup file, is the fail-fast choice.

Plan 00197 (seat broker) is the pull alternative: the member asks the broker for a seat, the
owner approves, and the member collects its token with a secret only it holds. That flow does
not depend on the transport at all, and works for any member that can reach the broker. If
00197 lands, ssh no longer carries tokens. It still installs the kit, writes the env file and
the unit, and verifies (section 5, O4).

### 4.4 The keeper and pingbus in the member

**Neither uses lxc-attach, now or after this change.** lxc-attach is only the desktop's way in
to *provision* the member. At run time:

- `agent-bus-claude` reads `~/.config/pingbus/env`, runs `pingbus config check` and execs
  `claude` with the pingbus plugin. The plugin's hooks start the supervised watcher, and the
  watcher talks HTTP to the team's homeserver at the bundle's `base_url`.
- The seat keeper (`pingbus seat keep`, Plan 00203 M1-M3) holds the seat locks of a ccy
  session's seats and follows its membership directory. It is part of the ccy seat path. An
  `agent-bus-claude` member uses its bundle directly and has no keeper today. If hot control is
  later extended to non-ccy members, its control channel is the member's own membership file.
  Over ssh that is one more `WRITE_ENV_PY`-style write as the agent user. Nothing about it is
  LXC-specific.

What ssh *does* change is the network question, which is separate from the transport. The
member must reach the bus address:

- same host: the LXC bridge, a libvirt bridge (subnet in `allow_from`), or passt (seen as the
  host itself);
- another machine: the team's WireGuard address, with `allow_from` listing the peer
  (`README.vm`, `docs/agent-bus.md`). Plan 00161 D46 kept cross-machine teams for a later
  phase, so an ssh member on another physical host depends on that phase.

Carrying the bus over an ssh tunnel (`ssh -R`) is **not recommended**: the member's bus
liveness would hang on one ssh session from the desktop, which is the opposite of the 00203
"recovers by itself" goal.

### 4.5 `service: true` units in the member

Today members.py refuses `service: true` unless `lxc.start.auto = 1`, systemd is the init
(`/run/systemd/system`) and tmux is installed. It then writes
`/etc/systemd/system/agent-bus-claude-<user>.service` (`render_member_unit`) as container root,
enables it and starts it.

Over ssh, two shapes:

| Shape                                                                                                                                            | Needs                                                                                          | Notes                                                                                                                                                                           |
| ------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **S1.** The same system unit, written as root in the member                                                                                      | root or passwordless `sudo -n` for an admin login (section 4.6)                                | Identical unit text; `systemctl` as root                                                                                                                                        |
| **S2.** A **user** unit, `~/.config/systemd/user/agent-bus-claude.service`, `WantedBy=default.target`, with lingering enabled for the agent user | Only the agent user's login, plus `loginctl enable-linger <agent-user>` once (root, or polkit) | No root at run time; `systemctl --user` as the agent. Linger is checked (`loginctl show-user -p Linger`) and refused when off, naming the one root command the provisioner runs |

The boot question: "does the member itself start when the desktop boots" cannot be answered
over ssh in general. For a local LXC container it is `lxc.start.auto`; for a libvirt guest,
autostart; for a physical host, not the desktop's business. Proposal: make it a **stated
requirement** of `service: true` for ssh members, and keep the check as an optional adapter
when the member reports `lxc` and the desktop's own `lxc-info` sees a container mapped to it.
The mapping is the one place that would need the virtualisation, and it only adds a check.

### 4.6 Auth: keys, agent forwarding, host-key trust, least privilege

**Who runs ssh.** members.py runs as root under `become` in the play. Root's ssh has no key for
the members and does not read the owner's `~/.ssh/config`. Three options:

| Option                                                                                                                                                                          | For                                                                                                                                                                             | Against                                                                                                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **A1.** ssh runs **as the owner**: members.py calls `runuser -u <owner> -- ssh ...` with the owner's `SSH_AUTH_SOCK` passed explicitly (read on the controller before `become`) | No new credential anywhere; uses exactly the access the owner already has; the owner's ssh config (aliases, ProxyJump) applies; ccy-teams' probes run as the owner with no sudo | Needs an unlocked agent (or an unencrypted key) when the play runs: a headless run fails fast naming `ssh-add`, as headless ccy launches already do (Plan 00161 D38) |
| **A2.** A **dedicated provisioning key** on the desktop (root-owned, 0600), its public half in each member's `authorized_keys` with `restrict,from="<desktop-addr>"`            | Works unattended; revocable per member; separate from the owner's identity                                                                                                      | A new standing credential that opens every member; someone must install the public half in each member (bootstrapping is the provisioner's or the owner's job)       |
| **A3.** `ssh root@<dest>`                                                                                                                                                       | One login does root and user steps                                                                                                                                              | `PermitRootLogin` on every member; root is the widest possible grant; against least privilege                                                                        |

**Agent forwarding into the member: never.** `ForwardAgent=no` on every call, set explicitly so
an owner's `Host *` default cannot turn it on. The member runs an autonomous agent; a
forwarded socket would let it sign as the owner while the connection lasts. That is the risk
Plan 00116 accepts deliberately for ccy's `--ssh-agent`, and it has no place in provisioning.
Every call also sets `ClearAllForwardings=yes`, `ForwardX11=no`, `RequestTTY=no` and
`BatchMode=yes` (never prompt in a play). One `ControlMaster` connection per member per run
keeps the dozen calls per member cheap. Its socket goes under a 0700 run directory and is
closed at exit.

**Host-key trust.** `StrictHostKeyChecking=yes` always; never `accept-new` inside the play. The
first sighting happens in ccy-teams on a terminal: it shows the member's host-key fingerprint
(`ssh-keyscan` plus `ssh-keygen -lf`) and asks once. On a yes, it records the key in a known
hosts file of its own (`UserKnownHostsFile=<state>/agent-bus-members.known_hosts`, owner-owned),
so the play never depends on whatever the owner's `known_hosts` holds. A changed key stops the
play, naming the member. For an LXC container whose key is not yet pinned, the fingerprint can
be cross-checked from the host's side (the container's `/etc/ssh/ssh_host_*_key.pub` through
the rootfs reader), the one place LXC knowledge helps trust.

**Least privilege.** Two identities, used only where needed:

1. **The agent user**: gh check, checkout read, `suggest-handle`, bundle observe/place/remove,
   env file, `config check`, and with S2 the user unit. The whole join needs nothing else.
2. **Root in the member**: only the kit in `/usr/local/share/agent-bus` with its two links,
   and S1's system unit. Either root through an `<admin-user>` login with `sudo -n` limited to
   those commands, or not at all, with a **per-user kit** in `~/.local/share/agent-bus/kit`
   linked from `~/.local/bin`.

A per-user kit means the agent user can modify its own pingbus and plugin. Today
`KIT_SYNC_PY` refuses a kit that is not root-owned and not writable by others, so a
prompt-injected agent cannot rewrite the code that filters its own inbound (`human_text: false`)
or that posts its goodbye. Whether that defence in depth is worth requiring root in every member
is an owner decision (open question 3). The recommendation is to keep the root-owned kit as the
default, through an admin login. The provisioner may also pre-install the kit, in which case the
ssh flow only verifies it and needs no root.

### 4.7 lxc-attach versus ssh

| lxc-attach gives, ssh does not                                                        | ssh gives, lxc-attach does not                                                                                                                |
| ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Works while the member's sshd is down or misconfigured, or not installed at all       | Any machine: LXC, VM, docker with sshd, another physical host, a cloud server                                                                 |
| Root in the container with no credential in the container at all                      | No host root needed: the owner's own login, or a scoped key; the desktop's root is not part of the path                                       |
| No network dependency: no route from the desktop into the container needed            | No rootful-container assumption: unprivileged LXC (`lxc.idmap`) and non-`dir:` rootfs (the `btrfs:` refusal below) are irrelevant             |
| Reading a **stopped** container's tree from the host (derive a handle, find a bundle) | The member's own view: its git, its passwd, its bind mounts, with no LXC config parsing                                                       |
| A clear state: `lxc-info` says RUNNING, STOPPED or FROZEN                             | Standard, audited auth (keys, host keys, `authorized_keys` options), logged by the member's own sshd                                          |
| The container's boot setting (`lxc.start.auto`) is readable                           | Works the same from the owner's terminal, from ccy-teams and from the play                                                                    |
| A host-side cross-check of the container's ssh host key (useful to pin it)            | A member on another machine can be provisioned at all, rather than joined by hand (README.lxc's by-hand route moves a token between machines) |

## 5. Options

| Option                                                         | Summary                                                                                                                     | For                                                                                                                        | Against                                                                                                          |
| -------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| **O1.** Status quo                                             | LXC by lxc-attach; everything else by hand from the kit READMEs                                                             | Nothing to build                                                                                                           | VMs and other machines get no IaC; the by-hand route moves a token by copy                                       |
| **O2.** A second transport in the same executor                | `ssh:` entries beside `container:` entries; one executor, `LxcGuest` and `SshGuest`                                         | Small and incremental; reuses every in-member script and the derive rule; LXC keeps its stopped-read and no-sshd strengths | Two paths to test; the entry list must say which                                                                 |
| **O3.** ssh only, retire lxc-attach                            | Every member, local LXC included, is an ssh destination                                                                     | One path; the rootful/rootfs restrictions vanish                                                                           | Every container needs sshd and a key; loses the stopped read, the no-credential root and the no-network property |
| **O4.** Pull (Plan 00197 broker) plus a provisioning transport | Tokens are pulled by the member after the owner's approval; ssh (or lxc-attach) only installs the kit and unit and verifies | Token delivery becomes transport-free and virtualisation-free; nothing writes `localhost.yml` per member                   | 00197 is Not Started; a network-reachable service is a bigger trust surface than a play run by the owner         |

**Recommendation: O2 now, shaped so that O4 can take over token delivery later.**

1. Add `ssh:` member entries (generic: any destination) beside the LXC entries. Same steps,
   through an `SshGuest` that runs `ssh <hardened options> <dest> -- env -i PATH=... HOME=... python3 -I -c ...` with arguments quoted for the remote shell.
2. Do not ask the virtualisation. The handle's type is the member's own `suggest-handle`
   answer, recorded at the first join and held.
3. The checkout is read inside the member as the agent user (C1), with `suggest-handle` as the
   agreement check.
4. Auth A1 by default: ssh as the owner, with the owner's agent and ssh config, agent forwarding
   off, host keys pinned in ccy-teams' own known hosts file at add time. A2 (a dedicated key)
   only for a host that runs the play unattended.
5. Least privilege: every join step as the agent user. Root in the member only for the root-owned
   kit and an S1 unit, through an admin login with `sudo -n`. S2 (user unit plus linger) is
   offered where the owner accepts it.
6. Keep LXC on lxc-attach (not O3): it covers sshd-down and stopped containers, and needs no
   credential in the container.

## 6. Open questions for the owner

1. **Entry shape.** A new `agent_bus_ssh_members` list, or one generalised `agent_bus_members`
   with `via: lxc|ssh` (the LXC list migrated)? The second is one list to read in ccy-teams, at
   the cost of a migration.
2. **Who runs ssh in the play.** A1 (your agent, so the play fails fast without `ssh-add`), A2 (a
   dedicated provisioning key, unattended), or both, chosen per entry?
3. **Root in the member.** Is the root-owned kit (defence in depth against an agent rewriting
   its own pingbus) worth requiring an admin login with `sudo -n` on every ssh member? Or is a
   per-user kit acceptable, or a kit pre-installed by the provisioner?
4. **`service: true` over ssh.** S1 (system unit, root), S2 (user unit plus linger), or both?
   And is "the member starts at boot" a stated requirement only, with the LXC/libvirt adapter
   checks as extras?
5. **An unreachable member declared absent.** Today an absent LXC entry needs the container
   running (or gone, with `repo:`). For an unreachable ssh member: refuse (fail fast), or park the
   handle on the homeserver (the token is then dead) and report the bundle left on the member?
6. **Members on other machines.** These depend on cross-machine teams over WireGuard (Plan 00161
   D46, a later phase). Should this plan wait for it, or cover only same-host VMs and containers
   first?
7. **Relation to Plan 00197.** Build O2's token push now, or wait for the broker and let ssh
   only install and verify?
8. **Handle type for an ssh member.** Take the member's own `systemd-detect-virt` answer (any of
   `lxc`, `vm`, `host`, `docker`), or let the entry pin it (`type:`) and hold the member to it,
   as `repo:` is held today?

## 7. Context noted in passing (not fixed here)

The host-side rootfs reader refuses an `lxc.rootfs.path` that is not a plain directory:
`helpers/agent_bus_lxc/derive.py` lines 131-134 strip a `dir:` prefix and die on anything not
starting with `/`, so a `btrfs:<path>` rootfs is refused. Another agent is fixing that. It
illustrates the class of LXC-specific knowledge the host-side read needs and an in-member read
does not.
