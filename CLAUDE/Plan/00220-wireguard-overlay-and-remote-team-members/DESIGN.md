# Plan 00220 design: a WireGuard overlay and team members on other machines

Placeholders, as in Plan 00161: `<team>`, `<port>`, `<bus_ip>` (the host's address on
`agentbus0`), `<wg_ip_A>` (the hub's overlay address), `<wg_ip_B>` (a peer's overlay address).
Example values come from [ExampleValues.md](../../ExampleValues.md): the overlay subnet is
written `192.0.2.0/24` (the real one is an RFC 1918 `/24` chosen in untracked host_vars, D-C3),
a LAN `198.51.100.0/24`, the home public address `203.0.113.10`. Laptop A is this desktop (hub
and homeserver host); laptop B is the second laptop (spoke).

The research this rests on is the coordinator's cross-machine teams report (untracked). Every
repository fact it gave that this design builds on was re-checked against the tree; the
citations below are to the tree.

## 1. Why the bus cannot reach another machine today

Three layers refuse a source on another machine, and this plan opens each one only for named
overlay peers:

- **Bind.** The homeserver listens on `127.0.0.1` and the team file's `listen` list.
  `ccy-teams create` declares `listen: [<bus_ip>]`, a `/32` on the NetworkManager dummy
  interface `agentbus0` (`ensure_bus_interface` in `files/usr/local/sbin/agent-bus-install`).
  Nothing routes to it from another machine.
- **Unit IP filter.** `IPAddressDeny=any` and `IPAddressAllow=` loopback, the listen addresses
  and `allow_from` (`render_dropin` in `helpers/agent_bus/render.py`).
- **firewalld.** One rich rule per `allow_from` CIDR, `source address=<cidr> port=<port> tcp accept`, in the zone of the interface the kernel routes that CIDR through, or the default zone
  if that interface has no zone (`zone_of_cidr`, `rich_rule` in the installer).

A ccy session on laptop B is refused separately, in code: `ccy --teams` requires
`agent-bus-hs@<team>.service` to be active on the local host (`_check_team`,
`helpers/agent_bus/checkout.py`). That is D42/D46 and Phase 3's subject, not this design's v1
phases.

## 2. The overlay (Phase 1)

### 2.1 Shape

- **One overlay per machine.** Each machine carries one WireGuard interface, whatever teams it
  serves. Per-team separation is already a property of each team's homeserver (its own port,
  accounts, `allow_from`, rich rules and unit filter), so a shared overlay with per-peer `/32`s
  in each team's `allow_from` maps each peer to exactly the teams it may reach (D-C2).
- **Hub and spoke, this desktop the hub.** Laptop A listens on one UDP port. Each spoke dials
  the hub. Spokes never route to each other: a spoke's only peer is the hub, with
  `AllowedIPs = <wg_ip_A>/32` (not the subnet). The hub does **not** forward between peers: no
  `ip_forward` change, no masquerade, no policy between overlay and any other zone. A spoke
  reaches the hub's homeservers and nothing else.
- **No full tunnel.** No peer ever has `0.0.0.0/0` or `::/0` in `AllowedIPs`; the validator
  refuses it. The overlay carries overlay traffic only; a spoke's internet traffic never goes
  through home.
- **IPv4 only** for now (YAGNI); the bus team files take IPv6 literals, and an IPv6 overlay is a
  later declaration change, not a redesign.

### 2.2 The declaration (untracked host_vars, vaulted secrets)

In `environment/localhost/host_vars/localhost.yml` on each machine, with a commented
placeholder in `localhost.yml.dist`:

```yaml
wireguard_overlay:
  role: hub                       # hub | spoke
  address: 192.0.2.1/24           # this machine's overlay address and the overlay prefix
  listen_port: 51820              # hub only: the one UDP port the router forwards
  private_key: "{{ vault_wireguard_overlay_private_key }}"
  peers:
    - name: laptop-b              # a label, never a real hostname
      public_key: "<base64 public key>"
      preshared_key: "{{ vault_wireguard_overlay_psk_laptop_b }}"
      address: 192.0.2.2/32       # hub: the peer's one address
    # on a spoke, exactly one peer, the hub:
    # - name: hub
    #   public_key: "<hub public key>"
    #   preshared_key: "{{ vault_wireguard_overlay_psk_laptop_b }}"
    #   address: 192.0.2.1/32
    #   endpoint: 203.0.113.10:51820   # the hub's public address, or its LAN address at home
    #   persistent_keepalive: 25
```

- `interface` and `zone` are not declared; both default to `wg-overlay` (D-C5) and are play
  variables, so one overlay per machine is the only shape.
- **Private keys and preshared keys are vault-encrypted** variables (`./vault.bash set vault_wireguard_overlay_private_key`, CLAUDE/SecurityRules.md), never in clear in any file, any
  task output or any process argument list (section 2.4). Public keys and overlay addresses are
  not secrets but are install-specific, so they live only in the untracked host_vars too. The
  repository holds no key, no peer name and no address.
- A per-peer preshared key is a coordinator default (D-C4): it is cheap and adds a symmetric
  layer that a later break of Curve25519 does not remove. The same PSK is vaulted on both ends.
- Key generation is an owner step on each machine, documented with the exact commands
  (`wg genkey | tee /dev/stderr | wg pubkey` is **not** the documented form, since it prints the
  private key; the doc gives `umask 077; wg genkey > <file>` in `untracked/` then `vault.bash set`
  from it and `shred` after). The research preferred keys generated on each host and never
  leaving it; the owner's direction for this plan is the vault, and a host's own key in its own
  vault never leaves that host either (D-C4).

### 2.3 Validation and rendering: Go, tests first

The declaration is validated and the NetworkManager keyfile rendered by a small Go command,
`wg-overlay` in `src-go/` (`check` and `render`), so every rule below is a unit test written
before the code (RED, then GREEN). An Ansible template plus `assert` tasks was considered and
rejected: those rules could only be exercised by running Ansible, which the container never
does, and the project's tooling rule puts new logic in Go.

`check` refuses, with the field named:

- a role other than `hub`/`spoke`; a hub without `listen_port`, a spoke with one; a spoke with
  other than exactly one peer, or a spoke peer without `endpoint`; a hub peer with `endpoint`
  (roaming peers dial in);
- a key that is not 32 bytes of standard base64 (44 characters); the same public key twice; a
  peer whose public key is this machine's own (derived from the private key);
- a peer `address` that is not a `/32` inside `address`'s prefix, two peers with one address, a
  peer address equal to this machine's;
- `AllowedIPs` wider than the peer's `/32` (a full-tunnel or subnet route is never rendered);
- an overlay prefix that overlaps any route the host already has outside the overlay interface
  (the play passes `ip -j route` and `ip -j addr` JSON on stdin): the LAN, `lxcbr0`, libvirt,
  podman and docker networks, and `agentbus0`'s `<bus_ip>`;
- an interface name over 15 bytes or a zone name over firewalld's limit (both are play
  variables, checked anyway);
- a `listen_port` outside 1024-65535, and `persistent_keepalive` outside 0-65535.

`render` reads the declaration (private and preshared keys included) on **stdin**, never argv,
and writes the keyfile `/etc/NetworkManager/system-connections/wg-overlay.nmconnection` itself,
mode 0600 root, by temp file and rename: `connection.type=wireguard`,
`connection.interface-name=wg-overlay`, `connection.zone=wg-overlay`,
`connection.autoconnect=true`, `wireguard.listen-port` (hub), `wireguard.peer-routes=true`,
`ipv4.method=manual` with the overlay address, `ipv6.method=disabled`, one `[wireguard-peer.<key>]`
section per peer. It prints only a `CHANGED`/unchanged marker; no key reaches stdout, stderr or a
diagnostic, and a golden test proves no key byte appears in any output stream, including the error
paths.

### 2.4 The play

The overlay is an extension of `play-vpn.yml`, not a new play: that play already owns
WireGuard (it installs `wireguard-tools`), it is general-scope and headless-safe, and
`playbook-main.yml` already orders it before `play-agent-bus.yml` "so a team may listen on a
WireGuard address". The overlay tasks are skipped as a whole when `wireguard_overlay` is not
defined (a machine with no overlay changes nothing). The qa-reviewer judges this placement at the
plan's end.

Order, each step failing the play on error:

1. Build `wg-overlay` (the existing `build-go-command.yml` import).
2. `wg-overlay check`, the declaration and the live `ip -j` JSON on stdin (`no_log: true`).
3. The firewalld zone `wg-overlay`, permanent and runtime: target `default` (rejects what no rule
   accepts), no services, no ports, no masquerade, no forward, `icmp-block-inversion` off with
   echo-request allowed (so reachability is testable), bound **by interface only**. The play
   asserts afterwards that the zone has no `sources` (a source binding would pull overlay-sourced
   packets from any interface into it) and no services or ports beyond what the bus's rich rules
   add.
4. On the hub only: the UDP `listen_port` added to the zone of the uplink interface (the
   interface of the default route), permanent and runtime.
5. Anti-spoof: in every zone other than `wg-overlay`, a rich rule dropping packets whose source is
   inside the overlay prefix. A packet claiming an overlay address must arrive on `wg-overlay`,
   where WireGuard has authenticated it against the peer's key and `AllowedIPs`. Without this,
   the host's weak-host routing accepts a LAN packet addressed to `<wg_ip_A>`, Fedora's loose
   `rp_filter` passes a spoofed overlay source, and the Workstation zone's open high ports admit
   it to the firewall; only the unit filter would then stand, and it trusts that same source.
6. `wg-overlay render` (stdin, `no_log: true`), then `nmcli connection reload` and
   `nmcli connection up wg-overlay` when the keyfile changed.
7. Read back: `wg show wg-overlay` lists the declared peers' public keys and the listen port, the
   interface carries the declared address, and `firewall-cmd --get-zone-of-interface=wg-overlay`
   is `wg-overlay`. Any mismatch fails the play.

Removal: `wireguard_overlay: {state: absent}` brings the connection down, deletes the keyfile and
the zone and the anti-spoof rules. The play refuses (fails fast, naming the team) while any
present `agent_bus_teams` entry still lists the overlay address in `listen`, since the homeserver
would then fail to bind (section 2.6).

### 2.5 The homeserver on the overlay

No bus code changes. The owner edits the team's `agent_bus_teams` entry in host_vars (ccy-teams
shows `listen` as fixed, `src-go/internal/teams/settings/fields.go`) and runs the play:

- `listen: [<bus_ip>, <wg_ip_A>]`: the overlay address is a **second** listen address. It is never
  `agent_bus_address`: the installer puts that address on `agentbus0`, and `interface_of`
  refuses a listen address carried by two interfaces. Existing bundles keep `<bus_ip>` and stay
  valid; a remote member's bundle is made with `--address=<wg_ip_A>`.
- `allow_from: [<wg_ip_B>/32, ...]`, one `/32` per remote peer that belongs in this team, never
  the overlay subnet (D-C2). `zone_of_cidr` routes `<wg_ip_B>` through `wg-overlay` (the peer
  route NetworkManager adds), so the bus's rich rule lands in the overlay zone.

The create wizard's "Bus address" step suggests "such as its WireGuard address"
(`askAddress` in `src-go/internal/teams/create/wizard.go`). Following it would collide with the
overlay interface. Phase 1 corrects the text: the bus address is a private address for
`agentbus0`; a WireGuard address is added as a second listen address in host_vars.

### 2.6 Availability: the homeserver depends on the overlay interface

The drop-in has `After=`/`Wants=` on each listen interface's device and
`StartLimitIntervalSec=0` (`render_dropin`; Plan 00161 DESIGN.md 3.3). If `wg-overlay` is absent,
Tuwunel cannot bind `<wg_ip_A>` and the **whole** homeserver, local members included, stays down
until it appears. A WireGuard interface is up as soon as NetworkManager activates it, whether or
not any peer is reachable, so with `autoconnect=true` this costs nothing in practice. Two
consequences are designed in: the removal refusal in 2.4, and a host acceptance step that reboots
the hub and finds the homeserver active with no peer up.

### 2.7 The router forward and the endpoint at home

The owner forwards one UDP port on the router (`<public>:51820/udp` to the hub's LAN address).
This is outside IaC and is documented in `docs/` as an owner step with what to verify.

A spoke has one `endpoint`. At home, the public address works only if the router loops NAT back
to the LAN (hairpin NAT), which varies by router. The documented default: the spoke's endpoint is
the hub's LAN address for the home phases, and the public address for the outside test (a
host_vars change and a play run on laptop B). Whether the router does hairpin NAT is an owner fact
recorded at the outside test (Q4).

## 3. A `host` member on laptop B (Phase 2)

Laptop B is a Fedora machine built from this repository: `play-vpn.yml` (spoke declaration) and
`play-agent-bus.yml` with no team (`agent-bus-install software` only; the homeserver binary stays
unused). Its agent user has `gh` logged in to github.com (README.host).

On laptop A, against a throwaway team that the acceptance script creates and removes in an
`EXIT` trap (and removes at start if a failed run left it), with a fixed test name and
`listen: [<bus_ip>, <wg_ip_A>]`, `allow_from: [<wg_ip_B>/32]`:

```bash
sudo agent-bus add-member <team> --repo=<repo> --host=<role> --type=host --role=worker \
  --address=<wg_ip_A> --out=$HOME/bundle-<team>
```

The bundle (`member.json`, `token`, `README`) goes to laptop B over the overlay (`scp` to the
agent user's `~/.config/pingbus/<team>/`, mode 0600), never by mail, chat or a synced folder. B
follows `README.host`: env file, `pingbus config check`, start with `agent-bus-claude`. The bundle's
`base_url` is `http://<wg_ip_A>:<port>`, which pingbus accepts as a listed IP literal
(`helpers/pingbus/config.py`).

Acceptance (the throwaway team, a local ccy seat on A as the other party):

- B sends a ping and free text to A's seat; A receives both. A sends to B; B receives.
- Laptop B sleeps (`systemctl suspend`). On A, `send`, `recv`, `wait`, the board and seat takes
  for other members complete within their usual bounds and show B offline, nothing waits on B.
  v1's rule is that offline members are shown and never block; v2 makes it binding (OF1).
- B wakes; its watcher reconnects and catches up the message sent while it slept.
- Clock skew is noted, not tested: v1 takes a ping's deadline from the sender's clock, so a
  drifting laptop can see early or late timeouts (v2 fixes this with server timestamps). B runs
  chronyd (Fedora default); the acceptance records both clocks' offsets.

## 4. Remote ccy seats (Phase 3, v2 only)

v1 code for remote ccy seats is **not** built (owner, 2026-10-23, with the research): v1 is to be
retired, and v2 already has the slot and the gate this needs. Phase 3 is tracked here and built
in Plan 00209.

| Need                                                                             | Owned by                                                                                                                                                                                                                                                                                   |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| A team reached at a declared URL that is not the bus address                     | Plan 00209: the per-team `url` field (DESIGN-v2-C-host.md, team declaration; "when a team is reached through something other than the bus address")                                                                                                                                        |
| An owner approval for changes to `listen`, `allow_from`, `url`                   | Plan 00209: C46 / DC18                                                                                                                                                                                                                                                                     |
| A seat take on a host with no local homeserver                                   | Plan 00209: v2 seat paths are one-host today ("not on this host" stays `needs: reissue`, DESIGN-v2-C-host.md); the change is the design note below                                                                                                                                         |
| Delivering the seat's credential from the homeserver host to the remote launcher | one of: Plan 00205 (ssh to the homeserver host to run the member add; its Q6 asks whether to wait for WireGuard), or Plan 00197 (the broker, approval-gated; today it listens only on bridge interfaces a team admits, so it would need to admit the overlay interface). Owner decision Q7 |
| Offline members never block, a sleeping laptop is offline                        | Plan 00209: binding rule, OF1 (already designed)                                                                                                                                                                                                                                           |
| Ping deadlines immune to member clock skew                                       | Plan 00209: server timestamps (already designed)                                                                                                                                                                                                                                           |

### 4.1 Design note for seat 3 (Plan 00209), to be delivered by the coordinator

> Plan 00220 opens cross-machine teams over a WireGuard overlay (hub on the homeserver host,
> per-peer `/32` in `allow_from`, the overlay address a second `listen` address, never
> `teamchat_address`). Its v1 phases need no bus code. Remote ccy seats are v2-only, and need from
> v2: (1) a seat take on a host where the team's homeserver is not local, keyed on the team's
> approved `url` instead of a local unit; (2) a credential path from the homeserver host to that
> launcher: ssh (Plan 00205) or the broker (Plan 00197, admitting the overlay interface), the
> owner to choose; (3) fail fast, naming the URL, when the homeserver is unreachable at seat
> creation, while a launch into an existing seat starts and catches up; (4) `url` validation that
> refuses a non-overlay, non-loopback, non-bridge address unless the owner approves it, since
> there is no TLS. Nothing here asks v2 to change its network model or add TLS.

## 5. Security

- **No TLS on the bus, so the bus never listens on a non-overlay interface.** Every member token,
  every human password and session token, and every room message travel in clear over HTTP. On a
  LAN or Wi-Fi anyone who can watch the traffic (any client that knows the WPA2 passphrase, a
  coworking network's operator) can replay a token and instruct every agent in the team. A team's
  `listen` therefore holds only loopback, `<bus_ip>`, host-only bridges and the overlay address;
  never a LAN, Wi-Fi or public address. The installer refuses wildcards already; Phase 1 adds a
  check to `play-agent-bus.yml` that fails when a present team lists an address carried by the
  uplink interface or any interface in a zone other than the overlay, `agentbus0`'s or a bridge's.
- **The overlay is the authentication.** WireGuard binds each peer key to its `AllowedIPs`
  (`/32`), so a source address on `wg-overlay` names a peer. That is what makes `allow_from`
  meaningful across machines. The anti-spoof rules (2.4 step 5) keep the same source from being
  claimed on any other interface.
- **WireGuard is silent.** It answers no packet that does not authenticate, so a scan of the
  forwarded port sees nothing, and an unknown key gets no handshake.
- **Revocation.** A lost or stolen laptop: delete its peer from the hub's host_vars, run the play
  (its key no longer completes a handshake), remove its `/32` from every team's `allow_from`, and
  park its member handles on each team. Documented as one runbook in `docs/`.
- **Root on the hub can instruct every member** (Plan 00161 D46's trust, unchanged).
  `--no-human-text` narrows what a remote member acts on.
- **DMZ.** A router DMZ host receives every unsolicited inbound port, so its own firewall is its
  only barrier. Recommended: **no DMZ; a single UDP forward to a non-DMZ host** (this desktop or a
  later hub). One forwarded port means a mistake in the host firewall (a service added to the
  default zone, the Workstation zone's open high ports) is not reachable from the internet. A DMZ
  host is acceptable only if it is default-deny on the uplink: the uplink zone's target `DROP`,
  no services, only the WireGuard UDP port open, sshd reachable only from the overlay, every bus
  address on the overlay or loopback, and a play task that fails when anything else is open in
  the uplink zone. That variant conflicts with `play-lxc-install-config.yml`'s guard that keeps
  SSH open in the default zone, and so is designed in its own plan if the owner chooses it; this
  plan does not build it.
- **Machines the owner does not own** (another person's laptop in a team) are a separate trust
  domain: a separate overlay interface and subnet with no routing to this one, and its own
  per-team `allow_from`. Not built here (Q6).

## 6. Decisions

### Owner decisions (open; the plan's defaults hold until decided)

| ID  | Question                                                                   | Recommended                                                                                                                             |
| --- | -------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Q1  | The public endpoint and router forward: forward one UDP port to the hub?   | Yes, for the outside test only (Phase 1 at home needs none); the default port 51820.                                                    |
| Q2  | A dedicated always-on hub, or this desktop?                                | This desktop for the test. A dedicated hub makes every ccy session remote, which needs Phase 3 (v2), so it waits for v2.                |
| Q3  | A DMZ host?                                                                | No: a single UDP forward to a non-DMZ host (section 5). A DMZ hub only in its own plan, with the default-deny uplink.                   |
| Q4  | Does the router do hairpin NAT (the public endpoint from inside the home)? | An owner fact, checked at the outside test; the LAN endpoint at home is the default either way.                                         |
| Q5  | Any third-party control plane (Tailscale, ZeroTier, a hosted NetBird)?     | No: plain WireGuard has no third party, no vendor repository and the best IaC fit. Revisit only if a guest network blocks outbound UDP. |
| Q6  | Teams with machines the owner does not own?                                | Not now; when it arises, a separate overlay and subnet per trust domain (section 5).                                                    |
| Q7  | Phase 3's credential route: ssh (Plan 00205) or the broker (Plan 00197)?   | Decided with Plan 00209; no default here.                                                                                               |

### Coordinator defaults (decided; may be overturned)

| ID   | Decision                                                                                                                                                                                                                            |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D-C1 | Plain WireGuard through NetworkManager (a keyfile), no `wg-quick`, no third-party mesh.                                                                                                                                             |
| D-C2 | One overlay per machine, hub and spoke; per-peer `/32` in each team's `allow_from`, never the subnet.                                                                                                                               |
| D-C3 | Addressing: one IPv4 `/24` from RFC 1918, chosen in untracked host_vars to overlap no existing network on any member (the validator checks the local host); the hub takes `.1`, peers from `.2`. Docs and tests use `192.0.2.0/24`. |
| D-C4 | Keys: each machine's private key and each per-peer preshared key are vault-encrypted host_vars variables on the machine that uses them; public keys in clear in untracked host_vars; nothing in the repository.                     |
| D-C5 | Names: interface `wg-overlay`, NetworkManager connection `wg-overlay`, firewalld zone `wg-overlay`, variable `wireguard_overlay`, Go command `wg-overlay`.                                                                          |
| D-C6 | No forwarding on the hub; spokes reach the hub only.                                                                                                                                                                                |
| D-C7 | The overlay lives in `play-vpn.yml` (an extension, not a new play).                                                                                                                                                                 |
