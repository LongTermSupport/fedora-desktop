"""The lab's subnets: checked, then rendered into lab.env (Plan 00246 D1).

A multi-network lab scenario (the X30 run) puts each of its links on a subnet the host's
owner chooses: the host_vars mapping `vm_test_lab_networks` of a short name to an IPv4 CIDR,
with no default. The lab play hands it here on stdin with the controller's routes
(`ip -j -4 route show table all`) as {"networks": {...}, "routes": [...]}, and writes what this
prints into lab.env, one `VMTEST_NET_<NAME>=<cidr>` line per network, which `vmtest
lab-network <name>` reads back.

Refused, naming the variable: a name that is not 1 to 16 of a-z and 0-9 starting with a
letter; a value that is not a canonical IPv4 CIDR with a prefix from /8 to /27 (a scenario
puts guests at host numbers up to 20); two networks that overlap; a network that overlaps any
route of the controller but the default one. The last matters because passt copies the host's
own address and prefix onto each guest's first NIC, so a lab subnet over the host's LAN would
look routed by that NIC. A route this cannot read is refused, never skipped.
"""

from __future__ import annotations

import ipaddress
import json
import re
import sys

VARIABLE = "vm_test_lab_networks"
_NAME_RE = re.compile(r"^[a-z][a-z0-9]{0,15}$")
_MIN_PREFIX = 8
_MAX_PREFIX = 27


class LabNetworksError(ValueError):
    """The declared subnets cannot be given to the lab."""


def _network(name: object, value: object) -> ipaddress.IPv4Network:
    if not isinstance(name, str) or not _NAME_RE.match(name):
        raise LabNetworksError(f"{VARIABLE}: the name {name!r} is not 1 to 16 of a-z and 0-9 starting with a letter")
    if not isinstance(value, str) or "/" not in value:
        raise LabNetworksError(f"{VARIABLE}: {name} is {value!r}, not a CIDR such as 192.0.2.0/24")
    try:
        net = ipaddress.ip_network(value, strict=True)
    except ValueError as exc:
        raise LabNetworksError(f"{VARIABLE}: {name} {value} is not a canonical CIDR (host bits clear): {exc}") from exc
    if not isinstance(net, ipaddress.IPv4Network):
        raise LabNetworksError(f"{VARIABLE}: {name} {value} is not IPv4")
    if not _MIN_PREFIX <= net.prefixlen <= _MAX_PREFIX:
        raise LabNetworksError(f"{VARIABLE}: {name} {value} has a prefix outside /{_MIN_PREFIX} to /{_MAX_PREFIX}")
    return net


def _route(entry: object) -> ipaddress.IPv4Network | None:
    """The network a route covers, or None for the default route."""
    if not isinstance(entry, dict) or not isinstance(entry.get("dst"), str):
        raise LabNetworksError(f"a route of the controller has no destination: {entry!r}")
    dst = entry["dst"]
    if dst == "default":
        return None
    try:
        net = ipaddress.ip_network(dst, strict=False)
    except ValueError as exc:
        raise LabNetworksError(f"a route of the controller has a destination that is not an address: {dst!r}") from exc
    if not isinstance(net, ipaddress.IPv4Network):
        raise LabNetworksError(f"a route of the controller is not IPv4: {dst!r}")
    return net


def check(networks: object, routes: object) -> dict[str, ipaddress.IPv4Network]:
    """The declared networks, each checked against the others and the controller's routes."""
    if not isinstance(networks, dict):
        raise LabNetworksError(f"{VARIABLE} must be a mapping of a name to a CIDR")
    if not isinstance(routes, list):
        raise LabNetworksError("the controller's routes must be a list")
    parsed = {name: _network(name, value) for name, value in networks.items()}
    names = sorted(parsed)
    for i, a in enumerate(names):
        for b in names[i + 1 :]:
            if parsed[a].overlaps(parsed[b]):
                raise LabNetworksError(f"{VARIABLE}: {a} {parsed[a]} overlaps {b} {parsed[b]}")
    covered = [net for net in (_route(r) for r in routes) if net is not None]
    for name in names:
        for net in covered:
            if parsed[name].overlaps(net):
                raise LabNetworksError(f"{VARIABLE}: {name} {parsed[name]} overlaps the controller's route {net}; choose a subnet no route of the host covers")
    return {name: parsed[name] for name in names}


def env_text(networks: dict[str, ipaddress.IPv4Network]) -> str:
    """The lab.env lines, one per network in name order."""
    return "".join(f"VMTEST_NET_{name.upper()}={net}\n" for name, net in sorted(networks.items()))


def main(argv: list[str] | None = None) -> int:
    del argv
    try:
        payload = json.loads(sys.stdin.read())
        if not isinstance(payload, dict) or sorted(payload) != ["networks", "routes"]:
            raise LabNetworksError('the input must be {"networks": {...}, "routes": [...]}')
        text = env_text(check(payload["networks"], payload["routes"]))
    except (LabNetworksError, json.JSONDecodeError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
