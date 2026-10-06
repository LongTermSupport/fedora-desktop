#!/usr/bin/env python3
"""Plan 00161 unit U16: is the bus address free for agentbus0? (deploy.bash's first leg)

  syntax ADDRESS   exit 64 unless ADDRESS is a concrete, canonical IP literal: the rule
                   agent-bus-install applies to --bus-address (no wildcard, loopback,
                   link-local or multicast address). Runs nothing.
  free ADDRESS     exit 1, naming each, when ADDRESS is already assigned to an interface
                   other than agentbus0, or when a route other than a default route covers
                   it in any routing table; exit 0 when it is free. agentbus0's own address
                   and routes are a previous run's, not a conflict.

`ip -j route get` names the device the kernel would use, not the route it matched, so it
cannot tell a default route from a narrower one through the same gateway; the covering
routes come from `ip -j route show table all match ADDRESS` instead, which lists every
route whose prefix contains the address, in every table (policy routing included).

Read-only. Everything is written to stderr; there is no stdout payload.
"""

from __future__ import annotations

import ipaddress
import json
import subprocess
import sys
from typing import Any

BUS_IFACE = "agentbus0"
EXIT_CONFLICT = 1
EXIT_USAGE = 64
REMEDY = "choose a free address and pass it as --bus-address=<ip>"


def say(message: str) -> None:
    print(message, file=sys.stderr, flush=True)


def valid_address(text: str) -> bool:
    try:
        address = ipaddress.ip_address(text)
    except ValueError:
        return False
    bad = address.is_unspecified or address.is_loopback or address.is_link_local or address.is_multicast
    return str(address) == text and not bad


def _describe_route(route: dict[str, Any]) -> str:
    words = [route.get("type", "unicast"), route.get("dst", "?")]
    if route.get("gateway"):
        words.append(f"via {route['gateway']}")
    words.append(f"dev {route['dev']}" if route.get("dev") else "(no device)")
    if route.get("table"):
        words.append(f"table {route['table']}")
    return " ".join(words)


def conflicts(address: str, links: list[dict[str, Any]], routes: list[dict[str, Any]]) -> list[str]:
    """Why ADDRESS cannot be agentbus0's, from `ip -j addr` and the covering routes."""
    reasons = []
    holders = sorted({
        link.get("ifname", "?")
        for link in links
        if link.get("ifname") != BUS_IFACE
        and any(info.get("local") == address for info in link.get("addr_info", []))
    })
    if holders:
        reasons.append(f"{address} is already assigned to {', '.join(holders)}")
    for route in routes:
        if route.get("dst") == "default" or route.get("dev") == BUS_IFACE:
            continue
        reasons.append(f"{address} is covered by the existing route {_describe_route(route)}")
    return reasons


def ip_json(argv: list[str]) -> list[dict[str, Any]]:
    done = subprocess.run(["ip", "-j", *argv], capture_output=True, text=True, check=True)
    text = done.stdout.strip()
    return json.loads(text) if text else []


def main(argv: list[str]) -> int:
    if len(argv) != 2 or argv[0] not in ("syntax", "free"):
        say("usage: deploy_check.py syntax|free ADDRESS")
        return EXIT_USAGE
    command, address = argv
    if not valid_address(address):
        say(f"--bus-address={address!r} is not a concrete, canonical IP address "
            "(no wildcard, loopback, link-local or multicast); pass --bus-address=<ip>")
        return EXIT_USAGE
    if command == "syntax":
        return 0
    reasons = conflicts(address, ip_json(["addr", "show"]),
                        ip_json(["route", "show", "table", "all", "match", address]))
    if reasons:
        for reason in reasons:
            say(f"[FAIL] {reason}")
        say(f"[FAIL] {address} cannot be the bus address here; nothing was changed: {REMEDY}")
        return EXIT_CONFLICT
    say(f"{address} is free for {BUS_IFACE}: on no other interface, under no route but the default")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
