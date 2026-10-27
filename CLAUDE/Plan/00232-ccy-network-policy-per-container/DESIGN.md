# Plan 00232 design: a network policy per ccy container

**State**: direction accepted by the owner, Q1 to Q4 answered (section 8). The first host triage
run is read into section 9, the rerun into section 10, which settles the points sections 3 and 6
left open and the build's details. Where section 10 differs from an earlier section, section 10
holds.

## 1. What is asked

Issue #114, with the owner's decision on ask 3 and the owner's added constraint:

- **A1** a declared policy per checkout, read on the host before the container starts and
  enforced outside the container, so nothing inside can lift it;
- **A2** the policy cannot be edited from inside the container it binds (issue #83's classes:
  mounted read-only, or never mounted);
- **A3** the default for a checkout with no policy is "block privates": the internet stays
  open; the private and LAN ranges (10/8, 172.16/12, 192.168/16, 100.64/10, 169.254/16,
  fe80::/10, fc00::/7) and the host itself are refused, except what ccy itself needs. A
  checkout opts hosts or ranges back in with an allow-list. A refused connection must be
  understandable to the agent: fail at once, and the session is told its limits at start;
- **A4** a probe: from inside, a refused route fails and an allowed one works;
- **HARD REQUIREMENT (owner): do not break agent teams.** Under the default policy the agent
  bus, ccy-teams, Element and seats on other machines keep working. Every path the team stack
  needs is either always allowed or derived automatically from the team configuration, never
  something the owner lists by hand. The acceptance gate proves a pingbus message is sent and
  received both ways by sessions under the default policy.

## 2. Facts this design rests on

Numbered in [PLAN.md](PLAN.md) (F1 to F16). In short:

- ccy picks `--network podman`, the shared rootless bridge, when nothing else is chosen (F1);
  `--no-network` gives pasta (F2). `ccy --connect` attaches a running session to more networks
  (F3).
- A ccy container gets podman's default capability set: no `NET_ADMIN`, no `SYS_ADMIN`, not
  privileged, no `--cap-add` anywhere in the launcher (F4). So the container cannot change the
  firewall or the routes of its own network namespace.
- The container's nameservers include pasta's link-local DNS forwarder and the host's own
  resolvers, which can sit in a private range (F5). `host.containers.internal` is a link-local
  address that pasta maps to the host (F6). The host's `/etc/hosts` entries are copied into every
  container, including names on the LXC bridge (F7).
- The agent bus: a container reaches the homeserver at `http://<bus_ip>:<port>`, where
  `<bus_ip>` is on the host's dummy interface `agentbus0` and is reached through pasta. The
  launcher reads it live from `agentbus0`. `agent-bus-install` refuses only loopback,
  link-local, multicast and unspecified addresses, so `<bus_ip>` can be in a blocked range
  (F8). A session can join a team after it starts (F9). pingbus also calls `api.github.com`
  (F10). LXC, docker and VM members, Element (a host Flatpak) and seats on other machines reach
  the homeserver themselves; a ccy container never connects to them (F11). A later remote team
  would give a WireGuard `base_url` in the seat bundle (F11).
- The startup system prompt (`--append-system-prompt`) is only passed when the launch has no
  prompt and no arguments, so headless and prompted sessions get none today (F12).
- `ccy.env` and `ccy.env.local` are sourced inside the container only; `ccy.env.local` is bound
  read-only over the writable workspace (F13). The host-side ccy state under
  `~/.claude-tokens/ccy/` is never mounted (F14).

## 3. Where the rule can be enforced

| #   | Where                                                                                                                  | Who can lift it                                                                                                    | Per container          | Survives `--connect`           | Root on the host | Verdict                                                           |
| --- | ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ | ---------------------- | ------------------------------ | ---------------- | ----------------------------------------------------------------- |
| E1  | nft table in the **container's own network namespace**, applied from the host between `podman init` and `podman start` | only a holder of `NET_ADMIN` in that namespace: the host user via `podman unshare nsenter`; not the container (F4) | yes, by construction   | yes (rules key on destination) | no               | **recommended** (entry confirmed, F18; PENDING TRIAGE A4, A5, A6) |
| E2  | nft forward chain in the **shared rootless network namespace**, keyed by container address                             | the host user                                                                                                      | only by address        | needs re-keying                | no               | rejected                                                          |
| E3  | podman network options (`--internal`, `isolate`)                                                                       | n/a                                                                                                                | per network            | n/a                            | no               | rejected: cannot filter by destination                            |
| E4  | pasta options (`--no-map-gw`, `--map-host-loopback`, outbound interface)                                               | the host user                                                                                                      | no: one pasta per user | n/a                            | no               | rejected as the mechanism; possible extra                         |
| E5  | host nftables output chain matched on the pasta process                                                                | root                                                                                                               | no: pasta is shared    | n/a                            | yes              | rejected                                                          |
| E6  | an OCI hook (`createRuntime`) applying E1's table, told the policy by an annotation                                    | as E1                                                                                                              | yes                    | yes                            | no               | the alternative to E1's launch sequence                           |

Why not E2: the rootless namespace is torn down when the last container stops and made again
by the next, so a table put there vanishes and the next container can start before it is put
back (an open window); container addresses are reused, so a stale rule binds the wrong
container; and it does nothing for pasta containers. Why not E3 or E5: E3 has no
destination filter (an internal network loses the internet, which Claude needs), and E5 needs
root and cannot tell one container's traffic from another's.

**E1's launch sequence.** `podman run --rm` becomes `podman create --rm`, `podman init`, the
host applies the table with `podman unshare nsenter -t <pid> -n nft -f -` and reads it back,
then `podman start -ai`. Any failure stops the launch before the container's process runs, so a
session never starts unfiltered (fail fast). Triage A5 measures this exact sequence with `--rm`.

**E1 against E6.** E6 keeps `podman run` and so the launcher's shape, but adds a user-wide
hooks directory that every podman container of the user consults, and an annotation format to
version. E1 is local to ccy's launcher. Recommendation: **E1**; E6 only if A5 shows `podman start -ai` cannot carry ccy's terminal and exit status (status 75, the restart request, must
still reach the launcher).

**What keeps it unliftable.** The container never holds `NET_ADMIN`. The launcher already adds
no capability; Phase 3 adds a guard that refuses a launch whose effective capabilities include
`NET_ADMIN` or `SYS_ADMIN`, or a privileged one, so a later flag cannot quietly undo this. The
acceptance gate checks the live session's `CapEff` (check `caps`).

**Docker engine.** ccy can run on rootful Docker; E1 there needs root. Decided (Q3): the
launch runs unprotected, with a printed warning and a context that says it has no policy.

## 4. The default policy

**Refused**: the seven ranges above; every global address of the host itself, read from
`ip -j addr` at launch (so a host with a public address is covered too); and so
`host.containers.internal`, which is link-local.

What "the host itself" is from inside, measured (F24 to F26): a session reaches the host
through `host.containers.internal` and through the addresses of the host's other interfaces
(the LXC bridge, docker bridges, VPN and WireGuard clients, `agentbus0`), every one inside a
default range. The outbound interface's own addresses are not the host: pasta copies them into
the rootless namespace, where netavark's published-port DNAT answers, so they lead to other
containers' published ports. A private one is refused by its range; a public one is why the
host's global addresses stay in the block list. Private subnets the host routes over a VPN are
refused by the ranges too; a checkout that needs one opts it in.

**Always allowed, derived on the host at launch; never listed by hand:**

| What                                   | Derived from                                                                                          | Why                                                                                |
| -------------------------------------- | ----------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| loopback inside the container          | the rule set (`oifname lo accept`)                                                                    | the session's own servers                                                          |
| the container's nameservers, port 53   | its `resolv.conf`, read on the host after `podman init` (`ResolvConfPath`); re-read on `--connect`    | DNS (F5, F20); pasta's forwarder is link-local, a project network's is its gateway |
| the agent bus address, **all ports**   | `ip -j addr show dev agentbus0`, as the launcher already reads it                                     | every team on this host, including one joined after launch (F8, F9)                |
| every team homeserver the seats name   | the `base_url` host of each seat bundle of the launch's teams                                         | a team on another machine (WireGuard, later) (F11)                                 |
| the subnets of joined project networks | `podman network inspect` of the `--network` choice at launch                                          | the compose stack the session was asked to join                                    |
| a network joined later                 | `ccy --connect`, which runs on the host, adds the subnet to the live table; `--disconnect` removes it | `--connect` keeps working (F3); triage A6 measures it                              |

Everything else, the public internet, stays open, so the package mirrors, the forge, the
Claude API and the other APIs an agent uses need no list (ask 3's "what every container always
needs" is the table above).

**Not allowed by default, deliberately:** the other sessions on the shared `podman` bridge.
This gives Plan 00080 its isolation (sessions cannot reach each other) without a network per
session: the default policy refuses the bridge's own subnet. Plan 00080 is closed as
superseded (decided, Q4).

**Agent teams, path by path** (the hard requirement):

| Path                                   | Under the default policy                                                                                                 |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| session to its homeserver on this host | allowed: `<bus_ip>` on every port, derived from `agentbus0`                                                              |
| session to a team joined mid-session   | allowed: same address; a remote team's host is added to the live table by `ccy-teams` on the host when the seat is added |
| session to `api.github.com` (pingbus)  | public, allowed                                                                                                          |
| ccy-teams, membership and token files  | bind mounts, not network; unaffected                                                                                     |
| Element                                | a host Flatpak; not under the policy                                                                                     |
| LXC, docker, VM and remote members     | they reach the homeserver themselves; the policy binds only ccy containers                                               |
| homeserver to the session              | nothing inbound is needed (pingbus polls)                                                                                |

## 5. Where the policy lives (asks A1 and A2)

The owner suggested a ccy env setting. `ccy.env` cannot hold it: it is tracked, so the agent the
policy binds can edit it, and it is sourced inside the container, never read on the host.
`ccy.env.local` is read-only inside, but is a file in the checkout, and **a container can
replace it** (triage A10, F29): the write is refused, but the container renames the parent
directory and writes a new file at the path, which the host then reads. So no file the host
reads may live in the checkout, bound read-only or not. This bears on issue #83 in general: a
read-only bind of a file protects that inode, not the path.

**Decided by A10: host-side, never mounted** (issue #83's second class):
`~/.config/claude-yolo/network/<project-key>.env`, keyed as ccy keys its other per-project host
state, holding ccy env setting lines that the launcher **parses** (it never sources a file on the
host):

```
# one entry per line: an address, a prefix or a host name (resolved on the host at launch),
# optionally :port
CCY_NETWORK_ALLOW=192.0.2.10,198.51.100.0/24,build.example.com:8443
```

and, for one launch, the same `CCY_NETWORK_ALLOW` exported in the host shell. Two keywords
(decided, section 8): `lxc` opens the LXC dev-site addresses found on the host, and `off`
means no policy for the checkout.

## 6. A refusal the agent understands

**Verdict, PENDING TRIAGE A3.** Triage measures drop, ICMP admin-prohibited and TCP reset with
curl, nc and ssh. The criterion: the attempt fails in well under the 2 s the acceptance gate
allows, with a message that does not read as "the service is down". The expectation is
admin-prohibited (curl and ssh report "No route to host", which is distinct from "Connection
refused"), but the choice is made on the measured messages.

**Context, in every case.** The launcher puts a block headed `CCY NETWORK POLICY` into the
system prompt of **every** session, headless and prompted ones included, outside the
startup-info branch (F12): what is refused, the allow-list in force (with its source), and how
the owner widens it (the host file, on the host, then a relaunch). A read-only copy of the same
text is mounted at `/run/ccy/network-policy`, beside the membership mount, so the agent can
re-read it after its context is compacted, and the prompt says so. When `ccy --connect` or a
team join widens the live table, the file is rewritten from the host.

## 7. Implementation shape (Phase 3, after review)

- **In Go**, per the bash line in the sand: the launcher is on the bash ratchet and cannot grow.
  A `ccylaunch` subcommand computes the policy (pure, unit-tested: the ranges, the derived
  allows, the parse of the host file), renders the nft table, and applies and verifies it
  through an injected runner (`podman unshare nsenter`), tested through fakes. The bash
  launcher calls it between `podman init` and `podman start`, and `ccy --connect` and
  `ccy-teams` call it to widen a live table. The inputs overlap by nature (the host's own
  addresses lie inside the ranges, the bus address comes from `agentbus0` and from every seat
  bundle), and nft refuses overlapping elements in an interval set (F21). So the package
  normalises each set (dedupe, drop an entry another entry covers) and the sets also carry
  `auto-merge`. Because `auto-merge` joins adjacent prefixes, a later `delete element` of one
  of them can fail: widening and narrowing therefore recompute the allow set and replace it
  whole (`flush set` and `add element` in one `nft -f` transaction), and verification checks
  each computed prefix is covered by the read-back table rather than comparing text. The
  ranges are built from octets
  (`netip.AddrFrom4`), which also keeps the public-repo scanner, which refuses dotted private
  addresses, quiet without an exception.
- nft must be on the host: declared in `play-claude-yolo.yml`, never installed by hand.
- `CCY_VERSION` minor bump; `docs/ccy.md` (networking and security); the changelog.
- `deploy.bash` runs the play, launches two throwaway headless seated sessions in a test team,
  and `acceptance-policy.bash` (renamed `acceptance.bash` then) judges them.

## 8. Owner decisions (2026-10-27)

The owner accepted the design direction and answered the four questions:

- **Q1 the LXC dev sites: an `lxc` keyword.** A checkout's policy may say `lxc` in its
  allow-list (`CCY_NETWORK_ALLOW=lxc,...`), which opens exactly the LXC dev-site addresses the
  launcher finds on the host at launch. They stay refused unless a checkout asks.
- **Q2 an opt-out: yes.** `CCY_NETWORK_ALLOW=off` means no policy for that checkout. The
  session's `CCY NETWORK POLICY` context then says it has open reach.
- **Q3 Docker: run unprotected and warn.** Under the Docker engine the launch goes ahead with
  no policy (the owner chose this over refusing). The launcher prints a warning, and the
  session's context says it has no network policy. This replaces section 3's proposal to refuse.
- **Q4 Plan 00080: closed as superseded by this plan.** The default policy refuses the shared
  bridge's subnet, which is the isolation 00080 set out to give.

## 9. What the first host triage run answered

Facts F17 to F30 in [PLAN.md](PLAN.md). Per question the triage was built for:

- **A3, what each verdict looks like inside: not answered.** nft refused the table at every
  apply (F21) and the harness carried on, so all four modes measured the same unfiltered
  container (F22). The one refusal seen is the baseline: curl's "Couldn't connect to server"
  and ssh's "Connection refused", from the outbound-interface address that pasta answers
  locally (F24). Section 6's verdict choice stays PENDING TRIAGE (the rerun).
- **A4, can the container lift the rules: the capability fact holds, the attempt is to be
  rerun.** No session is privileged or has a `--cap-add`, and none holds `NET_ADMIN` or
  `SYS_ADMIN` (F19), so the kernel refuses it any change to its namespace's table or routes.
  The run's lift attempts proved nothing (the image has no nft or ip). The rerun runs the
  host's nft and ip in the container's namespace with exactly the container's bounding set,
  beside a control that adds `net_admin`.
- **A10, the read-only bound file: replaceable** (F29). Section 5's host-side location is
  required, not just recommended.
- **P5 and P6, what a session must keep reaching**, all covered by section 4's derived allows:
  the bus at the `agentbus0` address, a /32 in 10/8, which is also the only host the seat
  bundles name (F27); the nameservers in its own `resolv.conf`, including pasta's link-local
  forwarder and a resolver on the LAN, or on a project network that network's gateway (F20);
  and the subnet of each network it joined, the shared bridge or a project network (F20).
- **E1's mechanics so far**: the host user enters a held container's namespace only through
  `podman unshare` (F18), and `podman start -ai` keeps `--rm` and stdin (F28). Applying a table,
  its survival across `podman network connect`, exit status 75 and pasta wait on the rerun.

Changes this made to the design: overlapping inputs are normalised and the sets replaced whole
(section 7); "the host itself" is described as measured (section 4); the nameservers are re-read
on `--connect` (section 4); the triage and the gate probe the host through
`host.containers.internal` and an address off the outbound interface, never the outbound one.
No new owner question: the design is settled except for what the rerun measures.

## 10. What the rerun answered, and the build's settled points

Facts F31 to F40 in [PLAN.md](PLAN.md); H1 to H7 are confirmed.

- **The verdict is ICMP admin-prohibited** (F33). Drop leaves every attempt to its timeout (5 s
  in the probes, minutes for a default client). A TCP reset is as fast but says "Connection
  refused", which reads as a service that is down. Admin-prohibited fails in milliseconds, and
  nc and ssh say "No route to host". curl says "Couldn't connect to server" either way, so the
  `CCY NETWORK POLICY` context names the signature: a private or host address failing at once
  with "No route to host" or curl exit 7 is the policy, not the service.
- **E1 holds as designed** (F32, F35 to F37): applied from the host between `init` and `start`,
  unliftable with the container's capabilities, kept across `podman network connect`, and
  `start -ai` carries exit status 75, so E6 is not needed.
- **The seat bundles are not a source of allows** (F39). They live in the checkout the
  container writes, the class A10 proved replaceable (section 5). The bus address comes from
  `agentbus0` on the host alone, and that is every seat bundle's host today (F27), so no team
  path changes. A homeserver on another machine will need a host-side source of its address
  (the installer's records) when one exists; until then the owner opens it in the host file.
  This replaces the seat-bundle row of section 4's table.
- **The nameservers are read once**, after `init` and before `start`, while only podman has
  written the container's `resolv.conf`; a later refresh keeps them, because a running
  container can rewrite its own `resolv.conf`. A project network joined later needs no re-read:
  its resolver is its gateway, inside the subnet the refresh opens. This replaces section 4's
  "re-read on `--connect`".
- **Joined subnets are opened only for project networks.** The default bridge `podman` stays
  refused (Q4: sessions do not reach each other); a network ccy joins by `--network`, compose or
  `--connect` is opened whole.
- **Unavailable is not failed.** The owner's rule: where enforcement is unavailable the session
  runs unprotected and is warned loudly. Unavailable is the Docker engine (Q3), a rootful
  podman, or no `nft` on the host (an IaC gap: the warning names the play). There the launch
  goes ahead, a warning box goes to stderr, and the context says the session has no network
  policy. A failure of the mechanism where it is available (the apply, the read-back, the
  capability guard) stops the launch and removes the container: that is a fault, and starting
  behind it would be the fail-open this plan rules out.
- **The shape is a command of its own**, `ccy-network-policy` (`internal/netpolicy`), installed
  as `ccy-token-usable` is. The launcher's container call goes through `ccy-network-policy launch`, which turns `run` into create, init, apply, verify, start, adds the read-only
  `/run/ccy/network-policy` mount and the context to the agent's command; `ccy --connect` and
  `--disconnect` run `ccy-network-policy refresh` after the connect, which replaces the allow
  set of every live session whole. The bash launcher stays within its ratchet.
- **A `host:port` entry is a rule of its own** (`ip daddr X tcp dport P accept`, and the same
  for UDP), not a concatenated set, a form the triage never applied. The allow set keeps the
  shape the triage applied.
- **The host file** is `~/.config/claude-yolo/network/<checkout-name>-<16 hex>.env`, the hex the
  start of the md5 of the checkout's path (as the restart history keys a checkout);
  `ccy-network-policy where` prints it, and the context names it. Entries from it and from a
  host-shell `CCY_NETWORK_ALLOW` add up; `off` in either turns the policy off.
- **Known exposure, accepted with section 4:** the bus address is a host address opened on every
  port, so a host service bound to every address is reachable through it. The homeserver ports
  are recorded only in root-readable installer state today; narrowing the allow to them needs a
  user-readable record first.
