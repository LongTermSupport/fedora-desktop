# Research: a ccy seat on a remote, less-trusted VM

The question: can a ccy container on a remote Fedora VM in a datacentre (reached from the laptop
over WireGuard with an SSH jump) hold a seat on a team whose homeserver runs on the laptop? Read
only; citations are `path:line` at F44 `b83158ca8`. Placeholders as in Plan 00220: `<bus_ip>`
(the laptop's bus address), `<port>`, `<wg_ip_A>` (the homeserver host's overlay address),
`<wg_ip_B>` (the VM's overlay address). Example addresses are from
[ExampleValues.md](../../ExampleValues.md).

## Answers

1. **Where it runs.** One Tuwunel homeserver per team, as `agent-bus-hs@<team>.service` on the
   laptop. It listens on `127.0.0.1` plus the team's `listen` list, on the team's own declared
   `port`. That list is by default `<bus_ip>`, a `/32` that `agent-bus-install software --bus-address`
   puts on the NetworkManager dummy interface `agentbus0`. It is not an address on a podman
   network. A ccy container reaches it through pasta. The address comes from the host_vars key
   `agent_bus_address` and the port from each `agent_bus_teams` entry; `play-agent-bus.yml`
   applies both. A session finds the homeserver from its seat bundle's `base_url`
   (`http://<bus_ip>:<port>`). The launcher writes that URL from the live `agentbus0` address
   when it issues the seat.
2. **Remote seat support: none today.** There is no federation: it is off by design. Nothing is
   exposed off the machine, and there is no tunnel or relay. Three network layers refuse another
   machine: the bind address, the unit's IP filter and firewalld. In code, `ccy --teams` refuses
   any team whose homeserver unit is not active on the local host, and issues seats only by a
   local `sudo agent-bus add-member`. Plan 00220 designs the network part: a WireGuard overlay, a
   second `listen` address and a per-peer `/32` in `allow_from`. It also names a `host`-type
   member on another machine for v1. The owner decided that remote **ccy** seats are v2 only, in
   Plan 00209. Plan 00209 Task 0.6 found that no v2 choice blocks this. Its four design changes
   are still open, unchecked.
3. **Smallest change, and what tracks it.** The network part is Plan 00220 Phase 1 with no bus
   code. The VM goes in as its own overlay peer, and the team gets `listen: [<bus_ip>, <wg_ip_A>]`
   and `allow_from: [<wg_ip_B>/32]`. The seat part is Plan 00220 §4.1's four v2 items, delivered
   through Plan 00209:
   - a seat take keyed on the team's approved `url`, not on a local unit;
   - a credential route, either ssh (Plan 00205) or the broker (Plan 00197);
   - a fast failure that names the URL when the homeserver cannot be reached;
   - `url` validation that refuses any address that is not on the overlay unless the owner
     approves it.

   In the meantime, Plan 00220 Phase 2's `host` member works on v1, but it is not a ccy seat. The
   tracking is Plan 00220 (Not Started: Tasks 3.1 and 3.2) and Plan 00209 Task 0.6's open
   subtask. Issue #59 is only the umbrella; no GitHub issue tracks a remote seat. One gap is not
   designed anywhere. Plan 00220 makes the homeserver host the WireGuard hub, which listens on UDP
   behind the owner's router. The scenario here has a laptop that roams behind NAT and a VM with
   a public address. The repo does not say who dials whom in that case, and it does not cover a
   VM that must not share the laptop's estate tunnel (see section 4).
4. **Security posture required.** The homeserver stays on the laptop and never moves to the VM.
   The VM reaches one TCP port, `<wg_ip_A>:<port>`, over a dedicated WireGuard peer that has only
   its own `/32` and no forwarding. It gets one token per seat, valid only on that team's
   homeserver. It holds no vault, no admin token, backup, registration secret or human password,
   no bundle of another team or seat, and no owner credential. It is a `worker`, never the
   coordinator, in that team's room only. Its handle carries a distinct site label. If the VM is
   compromised, the response is: `park-member` (which revokes the token), take the `/32` out of
   `allow_from`, and delete the WireGuard peer. The blast radius is that team's room (plaintext
   history included) and pings into repositories the team trusts. That second part is bounded by
   the VM's forge credential, so the VM must not be able to push to those repositories.

## Evidence

### 1. Where the homeserver runs and how a session finds it

- The play is a thin caller of the installer. `software --source ... --bus-address` runs on
  every run, and `team --team-file` runs per present team: `playbooks/imports/play-agent-bus.yml:13-22`.
  The argv is at `:194-198`. The play is ordered after `play-vpn.yml`, "so a team may listen on a
  WireGuard address": `playbooks/playbook-main.yml:46-48`.
- The installer's `BUS_IFACE=agentbus0` is at `files/usr/local/sbin/agent-bus-install:40`. The
  `--bus-address` text, "IP/32 ... on a NetworkManager dummy interface agentbus0", is at
  `:119-122`. Its validation, a concrete canonical IP and never a wildcard or loopback, is at
  `:194-211`. The homeserver binds `127.0.0.1` plus `listen`, asserted at `:698-699`.
- The declarations are `agent_bus_address` (`environment/localhost/host_vars/localhost.yml.dist:88-93`)
  and per-team `port` and `listen` (`:101-112`). Teams live only in the untracked host_vars:
  `docs/agent-bus.md:342-363`.
- On the session side, the launcher reads `agentbus0`'s one address (`helpers/agent_bus/checkout.py:494-505`).
  It issues a missing seat with `add-member ... --address=<that>` (`:247-252`). The bundle's
  `base_url` must be `http://` to an IP literal listed in `plain_http_hosts`
  (`helpers/pingbus/config.py:581-611`). The Go port is `src-go/internal/bus/seat/baseurl.go:12-41`.
- Reach from a container is through pasta to the `agentbus0` address, read live
  (`CLAUDE/Plan/00232-ccy-network-policy-per-container/DESIGN.md:42-49`). Every seat bundle's
  `base_url` host is that address (`.../00232-.../PLAN.md:80`, F27).

### 2. What exists for a remote seat

- **Federation** is off, with one server and no other origin
  (`CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-A-protocol.md:571-576`;
  `DESIGN-v2-C-host.md:51`, C1). This is not a route to a remote seat.
- **Exposure** is refused at three layers: bind, unit `IPAddressAllow=`, and firewalld rich rules
  per `allow_from` (`CLAUDE/Plan/00220-wireguard-overlay-and-remote-team-members/DESIGN.md:14-26`).
  The docs say "There is no TLS" and "Use WireGuard" (`docs/agent-bus.md:379-384`). The installer
  refuses only wildcards; choosing the network is the team's decision (`:358-359`, `:383`).
- **The code refuses a remote ccy seat.** `_check_team` requires `agent-bus-hs@<team>.service`
  to be active on this host (`helpers/agent_bus/checkout.py:701-716`). Plan 00220 says the same:
  "That is D42/D46 and Phase 3's subject" (`.../00220-.../DESIGN.md:28-31`).
- **Tunnel (designed, not built).** Plan 00220 specifies a WireGuard overlay in `play-vpn.yml`:
  - hub and spoke, with a per-peer `/32` and no forwarding or full tunnel (DESIGN.md:35-50);
  - vaulted keys (`:52-91`);
  - a guard policy that drops overlay sources and destinations on any other interface (`:165-199`);
  - the homeserver on the overlay as a second `listen` address (`:221-232`).

  It is Not Started (`.../00220-.../PLAN.md:3`). v1 gets no remote ccy seat code (PLAN.md:29-31).
  Docs already describe a `host` or other-machine member over WireGuard (`docs/agent-bus.md:372-373`,
  `:478-480`).
- **Plan 00209.** The owner's direction 6 is a homeserver "locally ... on another server ...
  somewhere in the cloud", with agents on several machines over "some kind of vpn or mesh"
  (`PLAN.md:40-46`). Task 0.6's verdict is that no irreversible choice blocks this
  (`PLAN.md:182-187`; `subagent-reports/261030-task-0.6-remote-homeserver.md:9-15`). Its four
  changes are open (`PLAN.md:188-192`):
  - team ids unique across homeserver hosts;
  - a `site` per machine;
  - plain HTTP only inside a mesh tunnel, with an optional `tls` field reserved in bundle v1;
  - SSH as the remote issuing channel.

  v2's per-team `url` is "when a team is reached through something other than the bus address"
  (`DESIGN-v2-C-host.md:1438`, `:1486-1487`). It is approval-gated (`:56`, C46/DC18). A seat
  whose pool record "is not on this host" stays `needs: reissue` (`:2190`). v2 still states no
  TLS and "never routed off the machine" (`:402-405`).
- **Relay or broker.** Plan 00197's broker listens only on bridge interfaces. It would need to
  admit the overlay (`.../00220-.../DESIGN.md:363`).
- **Plan 00205.** ssh members are research only, with owner decisions open (`PLAN.md:3`,
  `:55-57`). Members on other physical hosts depend on WireGuard (`:38-40`;
  `PROPOSAL-ssh-members.md:313`).
- **Plan 00232.** The container network policy allows the bus address on every port, from
  `agentbus0` only. Seat bundles are not a source of allows, because the container can write
  them. For a homeserver on another machine, "until then the owner opens it in the host file"
  (`.../00232-.../DESIGN.md:113-114`, `:264-269`). A ccy container on the VM therefore needs
  `<wg_ip_A>` opened in its own host's policy file.
- **The pingbus plugin** (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/`) has no
  remote logic. It uses whatever bundle the seat has.

### 3. Smallest change and trackers

- The network needs no bus code: Plan 00220 Tasks 1.1-1.8, with the team entry edited as in
  DESIGN.md:226-232. The bundle is made with `--address=<wg_ip_A>` (`:335-339`).
- The seat follows Plan 00220 §4.1 items (1)-(4) (DESIGN.md:367-378) and its needs table
  (`:358-365`), built in Plan 00209. Q7, ssh or broker, is decided with Plan 00209 (`:436`).
- Until then, there is Plan 00220 Phase 2's `host` member (PLAN.md:100-108). It is not a ccy
  container.
- `gh issue list --search agent-bus` shows #59 (the umbrella), #101, #103, #104, #111, #115,
  #116 and #122. None of them covers remote seats.
- **Gaps for this scenario that no plan covers:**
  - Plan 00220 puts the hub on the homeserver host, with a router forward (DESIGN.md:41-45,
    `:251-254`). A laptop that roams behind NAT, with a VM that has a public address, needs
    either the VM as the listening WireGuard end with the laptop dialling out (persistent
    keepalive), or a forward to the laptop.
  - Plan 00220 also fixes "one overlay per machine" (D-C2, `:444`). Yet it calls a separate
    trust domain "a separate overlay interface and subnet with no routing to this one" (`:420-422`,
    Q6). A less-trusted VM fits Q6, not the shared overlay, and it must not reuse the existing
    estate tunnel behind the SSH jump.

  Both need an owner decision before Plan 00220 Task 1.1.

### 4. Security posture to require

| Area | Requirement | Grounds |
| --- | --- | --- |
| Where the homeserver lives | On the laptop, never on the VM. Root on the homeserver host can instruct every member. | `docs/agent-bus.md:812-816`; A 6.1 (`DESIGN-v2-A-protocol.md:573-576`) |
| Reach | `<wg_ip_A>:<port>` TCP only, over a dedicated WireGuard peer that has only the VM's `/32` and has no forwarding. The VM's `/32` goes in the one team's `allow_from`, never a subnet. The guard drops overlay addresses that arrive on any other interface. It is not the estate's existing tunnel or jump host. The VM's ccy policy opens only `<wg_ip_A>` (in examples, `192.0.2.1`), plus the forge and the Claude API. | `.../00220-.../DESIGN.md:41-48`, `:165-194`, `:230-232`, `:398-402`; `.../00232-.../DESIGN.md:264-269` |
| Transport | Plain HTTP only inside the tunnel, never across a subnet router or exit node. The WireGuard key is generated on the VM and never leaves it. | Task 0.6 report:53-59; `.../00220-.../DESIGN.md:86-91` |
| Authentication | One member account and token per seat, issued by `add-member` on the laptop and carried over the tunnel (never by mail, chat or a synced folder). One homeserver per team, so the token works on no other team. Between teams, the token is the boundary. | `docs/agent-bus.md:448-457`; `.../00220-.../DESIGN.md:335-337`; `DESIGN-v2-C-host.md:398-401` |
| Never on the VM | The repository's vault or its password, and the laptop's host_vars. The team's admin token, registration secret or backups (each is the root of trust). Human passwords and Element sessions. Other teams' or other seats' bundles (sibling seats in one checkout can read each other's tokens). The owner's forge token, SSH agent (no agent forwarding through the jump) and Claude credentials. | `docs/agent-bus.md:841-847`, `:925-928`; A 6.1 |
| Forge credential | Its own credential, read-only on the team's trusted repositories. A ping can only point at a committed file on a trusted branch, so push rights on those repositories would let a compromised VM plant the very content it then pings about. | `docs/agent-bus.md:319-330`, `:821-822` |
| Trust and role | `worker`, never coordinator (no `set-role`). A member of that team's room only. v2 gates `listen`, `allow_from`, `url` and any new member behind the owner's password approval. | `docs/agent-bus.md:334-337`, `:670-673`; `DESIGN-v2-C-host.md:56` |
| Labelling | The homeserver authenticates `sender`, so each of its messages carries its handle `<repo>.<seat>+<site>.<type>`. Give the VM a `site` that marks it remote; Task 0.6 change 2 says each machine has its own site. Today no member-level "less trusted" flag exists, only the role. Members that should ignore its free text are created with `--no-human-text`. | `docs/agent-bus.md:332-334`, `:818-827`; Task 0.6 report:50-53 |
| If compromised | First, `sudo agent-bus park-member <team> <handle>` (revokes the token at once), or `remove-member`. Then take its `/32` out of `allow_from` and apply. Then delete its WireGuard peer and run the play. The blast radius: posting and pinging as that worker in that one team, and reading the room's plaintext history (encryption is off). It cannot reach other teams, other laptop ports or the estate. Keep secrets out of the room. | `docs/agent-bus.md:677-684`; `.../00220-.../DESIGN.md:405-407`; `DESIGN-v2-C-host.md:51` |
