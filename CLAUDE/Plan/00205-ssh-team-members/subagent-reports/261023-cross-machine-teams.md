# Cross-machine agent teams on one home network: what exists, what is missing

Research only, nothing edited. Placeholders: laptop A (this desktop, today's homeserver host),
laptop B (the second machine), `<team>`, `<port>`, `<bus_ip>`, `<wg_ip_A>`, `<wg_ip_B>`.
Facts from the repo are cited by path. Facts about third-party products come from general
knowledge, not the repo, and are marked **(unverified here)** where I am not certain.

## 1. What works today with zero changes

**Nothing on laptop B can reach a team on laptop A.** Three independent layers refuse it:

- **Bind.** Tuwunel listens on `127.0.0.1` plus the team file's `listen` list. `ccy-teams create`
  writes `listen: [<agent_bus_address>]` (`helpers/agent_bus/declare.py` header). That address
  is a `/32` on the NetworkManager **dummy** interface `agentbus0`
  (`files/usr/local/sbin/agent-bus-install`, `ensure_bus_interface`). Nothing advertises a route
  to it, so laptop B has no route to `<bus_ip>`.
- **Unit IP filter.** `IPAddressDeny=any` with `IPAddressAllow=` set to loopback, the listen
  addresses and `allow_from` only (DESIGN.md 3.3, rendered by `helpers/agent_bus/render.py`).
  `allow_from` defaults to empty, plus the live `lxcbr0` subnet that `play-agent-bus.yml` adds.
  A LAN source is dropped by the unit even if firewalld lets it in.
- **firewalld.** One rich rule per `allow_from` CIDR, in the zone of the interface the kernel
  routes that CIDR through, or the default zone if that interface has none (`zone_of_cidr`,
  `rich_rule` in the installer). With no LAN CIDR declared, no LAN rule exists. The design notes
  that Fedora Workstation's default zone already opens a high port range. That is why the unit
  filter is the layer that actually holds.
- **ccy launch.** `ccy --teams` runs `agent-bus seat check` and `seat take` on laptop B, and
  `_check_team` (`helpers/agent_bus/checkout.py`) requires `agent-bus-hs@<team>.service` to be
  active **on the local host**. Otherwise it refuses: "there is no team `<team>` on this host".
  This is D42/D46 in code. `take` also reads the local `agentbus0` address and runs a local
  `sudo agent-bus add-member`. A bundle carried by hand into
  `.claude/ccy/pingbus/seats/<team>/<seat>/` does not get past the check, because the team
  check runs first.

**What already works with config only, once a network path exists:** a **`host`-type member**
(a dedicated agent user started with `agent-bus-claude`, not a ccy session), and likewise a
`vm`-type member, on another machine. This is documented in `docs/agent-bus.md` ("Joining a
team", "A member on another machine reaches the homeserver over WireGuard"), `README.host` and
`README.vm`. The steps:

1. Add `<wg_ip_A>` to the team's `listen`.
2. Add `<wg_ip_B>/32` to the team's `allow_from`.
3. On A, run `sudo agent-bus add-member <team> ... --type=host --address=<wg_ip_A> --out=<dir>`.
   `_member_address` in `helpers/agent_bus/admin.py` requires the address to be one of the
   listen addresses, and the bundle's `base_url` is then `http://<wg_ip_A>:<port>`, with that
   address as its `plain_http_hosts` entry.
4. Copy the bundle to `~<agent-user>/.config/pingbus/<team>/` on B, write the env file, run
   `pingbus config check`, then start with `agent-bus-claude`.

pingbus itself makes no "same host" check: `helpers/pingbus/config.py` only requires `http://`
to go to a listed IP literal.

Caveats found in the code:

- **Changing `listen`.** ccy-teams "Edit settings" shows `listen` as fixed
  (`src-go/internal/teams/settings/fields.go`). Adding the WireGuard address therefore means
  editing the `agent_bus_teams` entry in host_vars and running `play-agent-bus.yml`. I found no
  installer rule that refuses a new listen address. Existing bundles keep pointing at
  `<bus_ip>`, so they stay valid.
- **The wizard's advice clashes with the installer.** The create wizard's "Bus address" step
  suggests "such as its WireGuard address" (`src-go/internal/teams/create/wizard.go`
  `askAddress`). But the installer puts `agent_bus_address` on `agentbus0` as a `/32`, and
  `interface_of` refuses a listen address that sits on more than one interface. If you give it
  the address that `wg0` also carries, the result is either a duplicate address or a refusal.
  Recommendation: keep `<bus_ip>` on `agentbus0` and add the WireGuard address as a second
  `listen` entry. The wizard text should be corrected; that is a small v1 docs/UX fix.
- **gh on B.** B's agent user must have `gh` logged in to github.com (`README.host`).

## 2. The smallest secure path to a real test

| Piece                                                                   | State                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| A WireGuard link A to B as IaC                                          | **New.** `play-vpn.yml` only installs `wireguard-tools` and says configs "must be imported manually" with `nmcli`. There is no peer, key or interface play. `playbook-main.yml` orders `play-vpn.yml` before `play-agent-bus.yml` "so a team may listen on a WireGuard address", so the ordering is designed.                                                                                                                                                                                                                  |
| Homeserver listening on `<wg_ip_A>`, `allow_from` set to `<wg_ip_B>/32` | **Exists** (installer and team file). Edit host_vars by hand, then run the play.                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| A firewalld zone for `wg0`                                              | **Partly exists.** The installer puts the rich rule in whatever zone `wg0` is in. An NM WireGuard connection with no `connection.zone` lands in the default zone, so a dedicated zone is **new** (part of the WireGuard play).                                                                                                                                                                                                                                                                                                 |
| Unit start-ordering on `wg0`                                            | **Exists** (DESIGN.md: `After=`/`Wants=` on each listen interface's device; `StartLimitIntervalSec=0`).                                                                                                                                                                                                                                                                                                                                                                                                                        |
| B's member bundle, `host` type, carried by hand                         | **Exists and is documented** (README.host/README.vm). It is the test that works today.                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| B's **ccy** seat                                                        | **Refused by the code** (D42, `_check_team`). Every route to it is new or only designed: ssh to A to run `add-member --out=-` (Plan 00205 covers ssh to *members*, not a ccy launcher on another host, and is "owner decisions open, nothing built"; its Q6 asks whether to wait for WireGuard); the 00197 broker (Status: Not Started; designed for LXC bridge clients, "listens only on the bridge interfaces a team admits"); or a hand-carried bundle (needs the D42 check relaxed to "the team is reachable at its URL"). |
| Acceptance                                                              | **Designed, not built.** U24 in DESIGN.md is a later phase: a Fedora test VM as the other host, over WireGuard, SKIPPED-NEEDS-OWNER without a VM.                                                                                                                                                                                                                                                                                                                                                                              |

## 3. Must laptop B be built from this repository?

- **For a ccy session: yes.** ccy (`play-claude-yolo.yml`), the `agent-bus` wrapper (whose
  absence `ccy --teams` refuses, `lib/agent-bus-seats.bash`) and the kit (`play-agent-bus.yml`
  via `agent-bus-install software`) all come from the playbooks. On top of that, the seat code
  must change (section 2).
- **For a `host`/`vm` member: not the whole desktop build.** The "From another project" route in
  `docs/agent-bus.md` covers it: any Fedora desktop or server can clone the repo at a pinned
  commit and run `agent-bus-install software` (no `--bus-address` needed). That installs pingbus,
  the plugin, `agent-bus-claude`, and the homeserver binary, which stays unused. B then needs
  gh logged in and Claude Code.
- **If B is not Fedora:** the installer uses dnf, nmcli and firewalld and the docs say "Fedora".
  Nothing in the repo supports a non-Fedora member. Copying the kit by hand might work but is
  unproven and outside IaC. I would not recommend it.

## 4. Security position: no TLS

The bus is plain HTTP by design (D6; DESIGN.md 3.3; `docs/agent-bus.md` "There is no TLS ...
Use WireGuard"). v2 keeps this (`DESIGN-v2-C-host.md` 3.2: "No TLS, as in v1 (R3)").

**On plain Wi-Fi or a wired LAN, these travel in clear:**

- every member's Matrix access token, sent on every request;
- each human's password at Element login, plus their session token after it;
- every message in the team room (rooms are unencrypted), so all pings, free text, handles and
  repository references.

Anyone who can see the traffic can replay a token and post as that agent or human, and so
instruct every agent in the team (DESIGN.md 9). On home WPA2-PSK Wi-Fi, that includes any client
that knows the passphrase and captures the handshake **(general knowledge)**. It also includes
the access point's operator, or anyone else on a coworking network.

**Over WireGuard** the path is encrypted and authenticated per peer key, which is the case the
design treats as private. What remains is the documented D46 trust: whoever is root on the
homeserver host can instruct every member, and `--no-human-text` narrows that.

## 5. Recommended plan outline

**Phase 0, owner decision.** Lift D46 for a test now (D46 is the owner's own "later phase"
rule), or wait.

**Phase 1, new play: the WireGuard overlay.** Build it now, independent of the bus version.

- Declared in host_vars: `wireguard_overlay` with this host's overlay address, listen port, and
  a peer list (name placeholder, public key, overlay `/32`, optional endpoint).
- The private key comes from the vault or is generated on the host and never leaves it
  (**coordinator default**: generate on host, publish only the public key).
- An NM WireGuard connection with `connection.zone=<overlay zone>`. That zone is a dedicated
  firewalld zone with target default (reject) and no services, so only the bus's rich rules
  open anything in it (**coordinator default**).
- On the hub, a forwarded UDP port only if a spoke is ever off-LAN.

**Phase 2, config only.** Add `listen += <wg_ip_A>` and `allow_from += <wg_ip_B>/32`, and run
the play. Fix the wizard wording from section 1.

**Phase 3, the first real test.** A `host` member on B by the README.host route, hand-carried
bundle, `--address=<wg_ip_A>`. Build U24 as an acceptance against a vmtest guest acting as the
other host.

**Phase 4, ccy seats on another host.** This is new code: the launcher must accept a team
whose homeserver is reached at its URL, not run locally, and must get a seat bundle from the
homeserver host. **Owner decision**: ssh to the homeserver host (the 00205 pattern) or the 00197
broker over the overlay (approval-gated pull; the trust surface is a network service). Then
decide offline/unreachable behaviour, per section 6(c).

**v1 or v2?** Phases 1 to 3 need no bus code and work on v1 now. Build Phase 4 **on v2 only**:

- v1 is to be retired;
- v2 is still one-host in its seat paths (C 3.7: "not on this host" answers stay `needs:`);
- v2 already adds a declared per-team `url` field "when a team is reached through something
  other than the bus address" (C 3.9), keeps the same listen/`allow_from`/firewalld/no-TLS
  model, and adds an owner password approval for changes to `listen`, `allow_from` and `url`
  (C46/DC18).

v2 changes neither binding, TLS nor the network model; it gives cross-machine a slot (`url`)
and a gate (approval). Building Phase 4 on v1 would be done twice.

## 6. The widened question

### (a) Hub-and-spoke with this desktop as the WireGuard endpoint

This fits D46's later-phase design as written: "a team ... runs its homeserver beside that
member, reached by the others over WireGuard".

- Desktop = homeserver and WireGuard hub; laptop = spoke.
- **Bind:** keep `agentbus0` (`<bus_ip>`) for local ccy, LXC and VM guests, and add `<wg_ip_A>`
  as a second `listen` address. Never put the WireGuard address in `agent_bus_address`
  (section 1).
- **firewalld:** the rich rule lands in `wg0`'s zone. Assign `wg0` a dedicated zone rather than
  inheriting the default Workstation zone, which opens high ports. The unit filter holds
  either way.
- **Limit:** a hub that is a laptop leaves home, which breaks the "home stays running" case
  below.

### (b) Overlay options for roaming (laptop at a coworking space, home machine running)

| Option                                                     | Packaging (Fedora)                                                                                                                             | Third party sees                                                                                                                                                                                      | NAT, no home ports?                                                                                                                                                                                                  | IaC fit                                                                                              | Bus bind                                                                                                                                               |
| ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Plain WireGuard + router port-forward (static IP, no DDNS) | `wireguard-tools` in Fedora (already installed by `play-vpn.yml`); kernel module in-tree                                                       | nobody: keys are yours; the ISP and the coworking network see UDP to the home IP                                                                                                                      | needs one forwarded UDP port at home. The laptop dials out, so the coworking NAT is fine unless that network blocks outbound UDP **(possible on some guest networks; unverified)**, and plain WG has no TCP fallback | best: NM connection and keys via vault/host_vars, the pattern already used here                      | `wg0` address only, dedicated zone                                                                                                                     |
| Tailscale                                                  | vendor repo (pkgs.tailscale.com), not in Fedora's repos **(unverified here)**                                                                  | the hosted control plane sees node public keys, names, IPs, ACLs and connection metadata; traffic is end-to-end WireGuard; DERP relays carry encrypted packets when there is no direct path           | yes: NAT traversal plus DERP over HTTPS, no home ports                                                                                                                                                               | moderate: vendor repo plus an auth key (a secret in the vault) and an account owned outside the repo | `tailscale0` address; its zone must be set; Tailscale also manipulates its own firewall/nft rules **(unverified how it coexists with firewalld here)** |
| Headscale (self-hosted Tailscale control)                  | not in Fedora repos as far as I know (upstream binary/container) **(unverified)**; clients still use the Tailscale client from the vendor repo | nobody else, if you also run DERP yourself; the default client config may still point at Tailscale's DERP map unless overridden **(unverified)**                                                      | needs a publicly reachable control server (a TCP port forwarded at home; static IP helps) with TLS, which means a certificate and public DNS name, the conflict DESIGN.md notes for U26                              | heavy: a server to run, back up, and keep updated                                                    | as Tailscale                                                                                                                                           |
| NetBird (self-hostable)                                    | vendor repo **(unverified)**                                                                                                                   | its cloud if hosted; nobody if fully self-hosted (management, signal, relay/TURN)                                                                                                                     | yes with its relays; self-hosting needs public ports and TLS                                                                                                                                                         | heavy when self-hosted (several services)                                                            | `wt0`-style interface **(name unverified)**, dedicated zone                                                                                            |
| Nebula                                                     | I believe Fedora packages `nebula` **(unverified)**                                                                                            | nobody: your own CA and certs                                                                                                                                                                         | the lighthouse needs a public UDP port, so home with port-forward; hole-punching between peers; relays optional                                                                                                      | good: certificates from a CA key in the vault; peers declared                                        | `nebula1` address, dedicated zone; Nebula has its own firewall layer too                                                                               |
| ZeroTier                                                   | vendor repo **(unverified)**                                                                                                                   | hosted controller and root servers see membership and metadata; traffic is encrypted; a self-hosted controller is possible but roots are still theirs unless you run your own **(unverified detail)** | yes, no home ports                                                                                                                                                                                                   | moderate (vendor repo, network join, approval in their controller)                                   | `zt*` interface, dedicated zone                                                                                                                        |

With a static public IP and a port-forward available, **plain WireGuard has no third party, no
vendor repo and the best IaC fit**. Its one real risk is a guest network that blocks outbound
UDP. Tailscale is the fallback if that bites, at the cost of a third-party control plane.

### (c) What the bus must handle once members roam

- **Offline members never block.** In v1 the rule is that offline members are shown and never
  block. v2 keeps it as a **binding** rule: `DESIGN-v2-FRAME.md` "Offline members never block
  (binding)", and `SPEC-n3.md` OF1: no verb, phase or probe fails, refuses or waits because
  another member is offline. A sleeping laptop is an offline member. v2 presence is driven by
  the link's long-poll, and server timers mark a member `unavailable` and then `offline`
  (A 6.6).
- **Home homeserver down or unreachable from the laptop.**
  - v1: the watcher waits, retries and catches up (`docs/agent-bus.md`). The goodbye notice is
    best effort, about 4 s inside an 8 s SessionEnd budget, and its line says "not sent".
  - v2: per-team retry, the team is marked down after 15 s, and other teams are unaffected
    (B 2.x failure table). `wait` reconnects across a link loss.
  - A ccy launch on the laptop cannot create a seat while the home homeserver is unreachable.
    Phase 4 must define this; I suggest fail fast with the reason (**coordinator default**),
    since a launch with an existing seat can still start and catch up.
- **Clock and latency.**
  - v2 computes ping deadlines from server `origin_server_ts` on both sides (A 7.x, R42). It
    names v1's defect: v1 took the deadline from the sender's local clock, so a skewed member
    clock moved deadlines. Under v1 a roaming laptop with clock drift can see early or late
    TIMEOUTs.
  - WAN latency of tens of ms is small against the bounded waits: seat take 60 s (launcher) and
    180 s (at boot), and the 15 s down mark.
  - The goodbye's ~4 s budget is the tightest, and a slow link can cost the goodbye; it is not
    a correctness issue.

### (d) Recommended default

**Owner decisions:**

- lifting D46 now;
- a public endpoint at home (one forwarded UDP port);
- whether any third-party control plane is acceptable (Tailscale or ZeroTier hosted), and
  whether to pay for it;
- which machine is the always-on hub;
- a DMZ host or not;
- for Phase 4, ssh or the broker for remote seats.

**Coordinator defaults:**

- plain WireGuard;
- keys generated on each host, with only public keys in host_vars;
- a dedicated firewalld zone for the overlay;
- the bus bound to the overlay address plus `agentbus0`, never a LAN or Wi-Fi address;
- `allow_from` set to per-peer `/32`s, not the overlay subnet;
- Phase 4 built on v2;
- fail fast on an unreachable homeserver at seat creation.

## 7. Owner facts: static IP, 2 Gbit/s symmetric, port-forwarding, a DMZ host being considered

### (1) One overlay per machine, or one WireGuard interface per team

The bus design favours **one overlay per machine** (hub-and-spoke, one forwarded UDP port).
Per-team isolation is already a per-team property of the homeserver, not of the network:

- each team has its own homeserver, port, accounts and tokens;
- each has its own `allow_from`, applied as its own firewalld rich rules (source `/32` to that
  team's port only) and as its own unit `IPAddressAllow=`.

So over a shared overlay, listing **specific peer `/32`s** per team maps each peer to exactly
the team homeservers it may reach. Matrix authentication (closed registration, a token per
member) sits on top.

On the hub, WireGuard `AllowedIPs` pins each peer key to its `/32`, so a peer cannot spoof
another's overlay address **(general WireGuard property)**. That is what makes a source-address
`allow_from` meaningful.

`allow_from` is **not** a boundary between teams *on one host* (v2 K16: local ccy containers
reach every team as host-local traffic). Across the overlay it is per-peer.

**Per-team tunnels are worth it** only when teams are in different trust domains, for example a
team that includes another person's machine. Then a separate interface or subnet, with no
routing between overlays, keeps their peers from even reaching your other hosts. That is an
owner decision when it arises; it is not needed for one owner's own machines.

### (2) Where the homeservers live

**On the desktop as now:**

- the ccy and ccy-teams code works unchanged (D42 requires the homeserver to be local);
- but the team goes dark when the desktop sleeps or leaves home.

**On an always-on home hub:**

- this is supported by "Any Fedora desktop or server can be a homeserver host" (the
  `docs/agent-bus.md` "From another project" route) and by D46's placement convention;
- spokes talk only to the hub, so no peer-to-peer forwarding is needed;
- **but** every ccy session, the desktop's included, then becomes a remote member, and today's
  code refuses all of them (D42);
- `ccy-teams create` and `apply` also run plays on the local host.

So hub placement depends on Phase 4 and belongs with v2. **Owner decision:** whether to buy or
dedicate an always-on hub. **Coordinator default:** start with the desktop as the hub for the
test, and move to a hub when v2's remote seats exist.

### (3) DMZ advice

A router DMZ host receives every unsolicited inbound port, so its own firewall is the only
barrier.

**DMZ hub:**

- safe only if it is default-deny: firewalld default zone with target DROP/reject on the
  uplink, only the WireGuard UDP port open, sshd not on the uplink;
- homeservers bound to the overlay address and loopback only (the installer already refuses
  wildcards; never list the LAN or public address);
- the unit `IPAddressDeny=any` remains as a second layer.

**Single UDP port-forward to a non-DMZ host:**

- exposes exactly one port, so a mistake in the host firewall (a service added to the default
  zone, the Workstation zone's high-port range) is not reachable from the internet.

**Recommendation: a port-forward, not DMZ.** It gives the same function with one less
single-layer failure mode. **Owner decision**, but a strong default.

Note: Fedora Workstation's default zone opens a high port range (DESIGN.md 3.3). On a DMZ
host that would be public. On a server-profile host the default zone differs
**(unverified here)**.

### (4) What a play here would need

Common to both options:

- a new play (or a major extension of `play-vpn.yml`; the qa-reviewer would judge which);
- `scope` general and headless-safe;
- the private key generated on each host into a root-only file, or held in the vault per host
  (**coordinator default**: generate on host, register the public key in host_vars; the repo
  holds no keys and no peer names);
- host_vars declares: overlay address and prefix, listen port (hub), peers (public key,
  `/32`, endpoint for spokes = the home static IP via a vaulted or untracked variable,
  `PersistentKeepalive` for roaming spokes);
- an NM WireGuard connection with `connection.zone=<overlay zone>`;
- a dedicated firewalld zone (target reject, no services; the bus adds its rich rules there);
- ordered before `play-agent-bus.yml` (already the slot in `playbook-main.yml`).

Port-forward variant:

- the hub's uplink zone gets one `--add-port=<udp>/udp`;
- the router forward is outside IaC: an owner step, documented.

DMZ variant, in addition:

- the uplink zone set to target DROP and stripped of services;
- an assertion task that fails the play if any other port is open in the uplink zone (fail
  fast);
- sshd restricted to the overlay zone. Mind the existing SSH-lockout guard in
  `play-lxc-install-config.yml`, which ensures SSH in the default zone; that would conflict and
  needs an explicit design.

The bus team entries then add `listen: [<bus_ip>, <wg_ip_hub>]` and `allow_from: [<peer>/32, ...]`. Under v2, those changes need the owner's `team approve` password (C46).
