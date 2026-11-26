# Plan 00270 design: ccy's "already contained" mode for LXC seats (issue #441)

Design only. The build is Phase 5 of [PLAN.md](PLAN.md), after the ccy repository exists.

## The shape

Today ccy is a podman launcher: it starts a rootless container, and everything that makes a
session safe and long-lived (the network policy, the supervisor, the session record) is
wired around that container. LXC seats (`docs/agent-bus-lxc-members.md`) skip ccy and
reach teamchat by their own install path, so they lose verbs and guarantees.

In contained mode the LXC container is the sandbox. ccy runs inside it, starts no podman, and
runs Claude Code directly in the container with the same entrypoint logic, supervisor and
session record a podman session has. Seen from ccy-teams, an LXC seat is a ccy session that
lives in another place. It has the same verbs and the same records, reached through
`lxc-attach` instead of `podman exec`.

The launcher gains one seam: **where the session runs** (`podman` or `contained`). Everything
above it (supervisor, registry, restore, conversation, teams) is shared code and takes no
branch on it.

## 1. What ccy gives beyond the container, and how each piece holds in an LXC

| Piece                                                          | Podman today                                                                         | Contained mode in an LXC                                                                                                                                                                                                                                                                                                                                                                                      |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Network policy: egress allow-list, refused ranges              | enforced on the host, outside the container (`ccy-network-policy`, podman's network) | **enforced on the host**, on the container's veth, by an nftables table the host-side LXC play writes (approved by the owner, PLAN.md Q7). Never by nftables inside: root in the container could rewrite that. At start, ccy probes from inside: a connect to a refused-range address must fail and an allow-listed host must answer. If the refused connect succeeds, ccy refuses to start, with the reason. |
| Hooks daemon and its guards                                    | per project, from the checkout, with the image's Python                              | the same: the daemon is per project and installs from the checkout. The tool inventory (below) holds its runtime.                                                                                                                                                                                                                                                                                             |
| Supervisor: goals, compacts, watchdog, failsafe crons          | ccy's supervisor plugins in the session's tmux                                       | the same plugins in the same tmux inside the LXC. A user systemd unit in the container (`ccy-session@<name>`) holds the tmux server, as the podman container does today.                                                                                                                                                                                                                                      |
| Teams and seat verbs (`ccy-teams seats …`, take-over, restart) | ccy-teams on the host addresses sessions through podman                              | ccy-teams addresses the seat through `lxc-attach -n <container> -- ccy-sessions <verb>`. That is one verb surface with two transports. `teams/lxcsession` already holds the LXC side and becomes the second transport, not a special case.                                                                                                                                                                    |
| Session record and restore across reboot                       | the registry on the host; ccy's restore unit brings sessions back                    | the registry lives **inside** the container (it is the container's session). Boot order: `lxc@<name>.service` (Plan 00266 C.2) starts the container, then the container's own `ccy-restore.service` reads its registry and resumes each session by conversation id (#431). The host does not restore inside a container. It checks that the seat came back and reports it if not.                             |
| Image tool inventory against the LXC template                  | the image's Dockerfile installs it                                                   | one inventory manifest in the ccy repository, read by the Dockerfile build **and** by `ccy doctor --contained`. The LXC is made by another repository's provisioner, so ccy cannot install into it. ccy states what it needs, and a missing tool makes contained mode refuse at start, naming the tool (fail fast).                                                                                           |

## 2. "Already contained" is proved, not assumed

Yolo mode (`--dangerously-skip-permissions`) must never switch on for a plain host. Contained
mode switches on only when **all** of these hold, checked by ccy at every start:

1. **The kernel says container.** `/run/systemd/container` reads `lxc`. Systemd in the
   container writes that file from its environment, and only PID 1's environment counts.
   This is checked alongside, not instead of, the next two.
2. **The namespaces say container.** `/proc/self/uid_map` is not the identity map (an
   unprivileged LXC maps root to a high host uid), and `/proc/1/cgroup` sits under the LXC
   payload cgroup. A plain host has the identity map.
3. **The host vouches, read-only.** The host-side LXC play bind-mounts a host file read-only
   into the container at a fixed path (for example `/run/ccy/contained`, holding the
   container's name). The mount comes from the host's mount namespace, so the kernel marks it
   locked: an unprivileged container cannot remount it writable or unmount it. ccy checks the
   file's content against the container's own name, **and** checks in `/proc/self/mountinfo`
   that it is a read-only bind mount whose source is outside the container's root.

No environment variable or flag turns contained mode on, and none can skip a check. On a
plain host, checks 1 and 2 fail and there is no locked mount, so ccy does what it does today:
it starts podman.

**When the proof fails**, ccy refuses, gives the reason and starts nothing. It prints which
check failed and what it read, for example:
`ccy: not starting in contained mode: /proc/self/uid_map is the identity map (this looks like a plain host, not an LXC container)`. It does not fall back to a yolo session without the
proof. A failed proof inside an LXC is a provisioning fault to fix in the play.

## 3. Migration of the existing LXC seats

One seat is running and five more are declared. Nothing moves until Phase 5's build is green
and released.

1. **A throwaway seat first.** The owner declares a new test seat in contained mode and runs
   `meta-deploy.bash`. It is accepted when the proof passes, the network probe passes, every
   seat verb works through `lxc-attach`, and a reboot brings it back by conversation id.
2. **The five declared, not-running seats next.** They have no conversation to lose. Each
   flips a per-seat declaration (`mode: contained`) and deploys through `meta-deploy.bash`.
3. **The running seat last.** Before the switch, its conversation id and seat identity
   (name, credential, home) are read from the current records and written into the
   container's ccy registry. #431 makes sure a restore never starts fresh while the checkout
   records the conversation under another name. The switch then resumes that exact
   conversation.

**Rollback** is per seat: the old install path stays in place until the owner accepts the
seat. Setting `mode` back and running `meta-deploy.bash` restores the old path. The
container's conversation is untouched either way, because both paths read the same
transcript.

**What the owner runs:** only `./CLAUDE/Plan/meta-deploy.bash`, with each step queued in
`meta-deploy.list` by the commit that needs it, and the reboot for step 1's restore check.

## 4. What changes for podman-based ccy users

- **Install source.** ccy comes from a pinned ccy release instead of this checkout. The plays
  install that release. The commands and paths stay the same.
- **Version.** `CCY_VERSION` becomes the ccy repository's release tag. The bump rule
  (`qa-ccy-version-bump`) moves with it, and this repository's `docs/ccy-changelog.md`
  becomes a pointer.
- **ccy-teams no longer reads this checkout** (`main.RootDir`). Adding a seat runs the
  provisioner command ccy is configured with. On this host, the play configures it to run
  this repository's agent-bus play, so the verb behaves the same.
- Nothing is lost: every verb, the supervisor, restore and the network policy work as they
  do today. Phase 4 of PLAN.md checks this with the acceptance list before the old path
  is removed.
