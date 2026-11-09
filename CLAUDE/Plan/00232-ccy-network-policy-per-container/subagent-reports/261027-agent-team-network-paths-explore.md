# Agent-team network paths from a ccy container (Explore sub-agent, 2026-10-27)

Read-only research for Plan 00232's hard requirement (do not break agent teams). The facts it
supports are F8 to F11 in PLAN.md; claims marked UNVERIFIED were not checked by the plan agent.

Short answer: a ccy container in a team makes only two kinds of network connection, and the address of each is known on the host before the container starts. One is plain HTTP to the team's homeserver on the host's bus address. The other is HTTPS to api.github.com. Nothing in the agent-team stack makes a ccy container reach an LXC container, another machine on the LAN, or the host's main address. One catch: the bus address may well sit inside a range you plan to block, so it needs an explicit exception.

## 1. ccy container to the team homeserver (Tuwunel, the Matrix server)

- **Destination:** the bus address `<bus_ip>`. This is a /32 (or /128) on the NetworkManager dummy interface `agentbus0` on the host. The container reaches it through pasta, rootless podman's networking, which connects from the host's own namespace. Protocol is plain `http://<bus_ip>:<team port>`, with no TLS.
  - Evidence: `files/usr/local/sbin/agent-bus-install:40` (`BUS_IFACE=agentbus0`) and `:119-122`.
  - `CLAUDE/Plan/00161-agent-team-bus-matrix/DESIGN.md:232-235` and `:665-674`. Probe H1 reached `<bus_ip>` on the default network, a named project network and `--no-network` alike, with the source seen as `<bus_ip>`. The host's primary address was refused from all three. `host.containers.internal` is deliberately not used.
  - `files/opt/claude-yolo/optional/agent-bus/README.podman:55-56`.
- **How the address is configured:**
  - Host var `agent_bus_address` in `environment/localhost/host_vars/localhost.yml` (example at `localhost.yml.dist:88-93`), passed as `--bus-address` at `playbooks/imports/play-agent-bus.yml:198`.
  - Per-team `port`, `listen` and `allow_from` live in `agent_bus_teams` (`localhost.yml.dist:101-123`). The team file schema is in `docs/agent-bus.md:319-334`.
  - The seat bundle's `member.json` carries `base_url = http://<addr>:<port>` and `plain_http_hosts=[addr]`, where `addr` must be one of the team's `listen` addresses (`helpers/agent_bus/admin.py:1018-1033` and `:1046-1060`).
  - For a ccy seat, that address is read live from `agentbus0` with `ip -j addr show dev agentbus0` (`helpers/agent_bus/checkout.py:56`, `:134-145`, `:494-505`, `:878`).
  - The bundle is stored at `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/` (`docs/agent-bus.md:440`).
  - The client refuses any `http://` host that is not an IP literal in `plain_http_hosts` (`helpers/pingbus/config.py:581-619`).
- **Known before start?** Yes. `agent-bus seat check`/`seat take` run on the host before `podman run` (`files/var/local/claude-yolo/lib/agent-bus-seats.bash:48,85`).
- **Caveat 1, hot control (Plan 00203):** a running session can be put into a team later. It carries the bus kit even when it is on no team, and its membership dir is `$XDG_RUNTIME_DIR/ccy/membership/<name>/`, mounted read-only at `/run/ccy/membership` (`files/var/local/claude-yolo/lib/agent-bus-membership.bash:8-16`, `:67`). So the allowed ports cannot be derived only from launch-time `--teams`.
  - Safe rule: allow `<bus_ip>` on every port in `agent_bus_teams`, or simply all ports to `<bus_ip>`.
  - This applies to every session launched without `--no-bus`, not only `--teams` sessions.
- **Caveat 2:** `valid_bus_address` refuses only unspecified, loopback, link-local and multicast addresses (`agent-bus-install:194-207`). An RFC 1918 or CGNAT bus address is allowed, and likely in practice. The policy must exempt `<bus_ip>` before the private-range drops. Tests use 192.0.2.10 (TEST-NET), but the real value is site-specific (UNVERIFIED).
- **Caveat 3:** I did not see the main play derive any address other than the `agentbus0` one. A team can `listen` on other concrete addresses, such as a WireGuard address (`docs/agent-bus.md:329-330`), but ccy seats always get the `agentbus0` address (UNVERIFIED that nothing else is ever used).

## 2. ccy container to the GitHub API (public internet)

- pingbus checks every ping reference over HTTPS 443 to `https://api.github.com`, using gh's login (`helpers/pingbus/forge.py:47`, `:139-158`). Any other `forge_api` is refused (`:142-144`).
- gh is logged in inside the container at start (`docs/agent-bus.md:400-409`). This traffic is already public, so the policy does not affect it.

## 3. Other members (LXC, docker, VM, other machines)

- **LXC agents** reach the host homeserver themselves, routed via `lxcbr0`. The play adds lxcbr0's subnet to every team's `allow_from` (`docs/agent-bus.md:442`, `:471-476`; `playbooks/imports/play-agent-bus.yml:21-22`, `:306`; `helpers/agent_bus/bridge.py`). Docker and libvirt-bridge guests need their subnet in `allow_from` (`docs/agent-bus.md:443-444`). All traffic between members goes through the homeserver; a ccy container never connects to an LXC address. Rooms and verbs are the protocol in `docs/agent-bus-protocol.md`. No peer-to-peer path was found, but the protocol spec was not read in full (UNVERIFIED).
- **Where the homeserver runs:** on one host per team, under the `agent-bus-hs@<team>.service` systemd unit (Tuwunel, `files/etc/systemd/system/agent-bus-hs@.service:24`). It is always on loopback plus the `listen` addresses (`docs/agent-bus.md:329`). It can run on a remote server via `agent-bus-install` from another project's IaC (`docs/agent-bus.md:365-376`). Placement rules: `docs/agent-bus.md:336-348`.
- **Seats on other machines** reach the homeserver over WireGuard, with `--address` set to the homeserver's WireGuard address (`docs/agent-bus.md:446-448`). The WireGuard overlay plan (00220) is "Not Started", and SSH members (00205) have "nothing built".
  - A ccy session on this host today never reaches another machine for the bus. A ccy podman seat requires the homeserver on the same machine (`README.podman:55`).
  - In the future, ccy would reach a remote team's WireGuard address "the same way" through pasta (`DESIGN.md:688-690`). That address lives in the team file and the bundle `base_url`. WireGuard addresses are often in 10/8 or 100.64/10.

## 4. Element

Element is the Desktop Flatpak on the host, run as the user, not in ccy (`playbooks/imports/optional/common/play-agent-bus-element.yml:3-11`, `:82-88`). Its profile's `base_url` comes from `agent_bus_element_teams` (`helpers/agent_bus/element.py:18`, `:53-77`, `:139`). Phones would use `http://<address>:<port>` over WireGuard (`docs/agent-bus.md:859-860`). Nothing in the ccy container talks to Element.

## 5. Inbound to the container

None. Waking a session is local inside the container (an inbox socket or spool), and `pingbus watch` pulls from the homeserver (`helpers/pingbus/relay.py:7`, `:128-133`). When a session dies, the host's own pingbus announces the disconnect (`agent-bus-membership.bash:18-20`). Nothing needs to reach the container.

## 6. Other host services

- **ssh-agent:** a unix socket bind-mounted at `/run/ccy/ssh-agent`, through a filter socket (`files/var/local/claude-yolo/lib/ssh-handling.bash:1177`, `:1442-1443`). Not network.
- **Membership:** a read-only bind mount (above). Not network.
- **hooks-daemon / supervisor:** runs inside the container as the claude wrapper (`entrypoint.sh:704`). No host port was found.
- **`ssh.github.com:443` fallback** (`entrypoint.sh:222`): public.
- **Project compose networks** (`--network`, `lib/network-management.bash`): podman bridge subnets, often in 10/8 or 172.16/12. Not part of the bus, but blocking them would break `--network` service access (DESIGN.md section 4 allows the joined networks' subnets).
- **No proxy, ollama, MCP-on-host or package-cache references** turned up in `claude-yolo`, `entrypoint.sh` or `ssh-handling.bash` (UNVERIFIED for `lib/common.bash` and the Go launcher).

## Policy implication

Always allow `<bus_ip>`: read it from `agentbus0` (or `agent_bus_address`) at launch, on all ports or on the ports of every team in `agent_bus_teams`. Exempt it before the private-range refusals.

Pasta delivers this traffic from the host namespace, so "block the host itself" must not cover the `agentbus0` address, even though it is a host address. Blocking host loopback or the primary address does not affect the bus. Later, add each remote team's `base_url` host from the bundles or team files.
