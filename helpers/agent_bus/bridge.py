"""The LXC bridge's subnet, which every present team admits without being told.

This repository sets LXC up (play-lxc-install-config.yml: lxc-net, the stock bridge
`lxcbr0`), so a team's effective `allow_from` is the declared list plus that bridge's
subnet. play-agent-bus.yml calls this module once, with the bridge's Ansible facts, and
renders every team file from its output: the installer, and through it the firewall rule,
the unit drop-in and the homeserver config, all see the one list computed here.

    python3 -m helpers.agent_bus.bridge --bridge lxcbr0 --facts '<json|null>' --teams '<json>'

prints `{"note": "<one line>", "teams": [...]}`. `null` facts mean the bridge does not exist
(LXC is not installed on this host): the note says so and no team changes. A bridge with no
IPv4 address means lxc-net is broken, and exits 1 with the reason on stderr.

Stdlib only; no file or network I/O.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import sys
from collections.abc import Mapping, Sequence


class BridgeError(ValueError):
    """The bridge exists but its subnet cannot be read."""


def subnet(bridge: str, facts: Mapping | None) -> str | None:
    """The bridge's IPv4 network as a canonical CIDR; None when the bridge does not exist."""
    if facts is None:
        return None
    ipv4 = facts.get("ipv4") if isinstance(facts, Mapping) else None
    if not isinstance(ipv4, Mapping) or not ipv4.get("address"):
        raise BridgeError(f"bridge {bridge} exists but has no IPv4 address: lxc-net is broken; "
                          f"check `systemctl status lxc-net` and play-lxc-install-config.yml")
    if ipv4.get("network") and ipv4.get("prefix"):
        spec = f"{ipv4['network']}/{ipv4['prefix']}"
    elif ipv4.get("netmask"):
        spec = f"{ipv4['address']}/{ipv4['netmask']}"
    else:
        raise BridgeError(f"bridge {bridge}: its IPv4 facts carry neither a prefix nor a netmask")
    try:
        return str(ipaddress.ip_network(spec, strict=False))
    except ValueError as exc:
        raise BridgeError(f"bridge {bridge}: {spec!r} is not a network: {exc}") from None


def effective(declared: Sequence[str] | None, bridge_cidr: str | None) -> list[str]:
    """Declared `allow_from` then the bridge subnet, once each."""
    out = list(dict.fromkeys(declared or []))
    if bridge_cidr and bridge_cidr not in out:
        out.append(bridge_cidr)
    return out


def apply(bridge: str, facts: Mapping | None, teams: Sequence[Mapping]) -> dict:
    """Every team with its effective allow_from, and the one-line note for the play."""
    cidr = subnet(bridge, facts)
    if cidr is None:
        note = f"no LXC bridge {bridge} on this host (LXC not installed): teams admit only their declared allow_from"
    else:
        note = f"LXC bridge {bridge} subnet {cidr} is admitted by every present team"
    return {"note": note, "teams": [{**t, "allow_from": effective(t.get("allow_from"), cidr)} for t in teams]}


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="bridge", description=__doc__.split("\n\n")[0])
    parser.add_argument("--bridge", required=True)
    parser.add_argument("--facts", required=True, help="the bridge's Ansible facts as JSON, or null")
    parser.add_argument("--teams", required=True, help="JSON list of team entries")
    args = parser.parse_args(argv)
    try:
        result = apply(args.bridge, json.loads(args.facts), json.loads(args.teams))
    except (BridgeError, json.JSONDecodeError) as exc:
        print(f"bridge: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    sys.exit(main())
