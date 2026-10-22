#!/usr/bin/env python3
"""Plan 00161 unit U16: is the bus address free for agentbus0? (deploy.bash's first leg)

  syntax ADDRESS   exit 64 unless ADDRESS is a concrete, canonical IP literal: the rule
                   agent-bus-install applies to --bus-address (no wildcard, loopback,
                   link-local or multicast address). Runs nothing.
  free ADDRESS     exit 1, naming each, when ADDRESS is already assigned to an interface
                   other than agentbus0, or when a route other than a default route covers
                   it in any routing table; exit 0 when it is free. agentbus0's own address
                   and routes are a previous run's, not a conflict. The interface's name
                   is read from this checkout's agent-bus-install (exit 2 if it cannot be).

`ip -j route get` names the device the kernel would use, not the route it matched, so it
cannot tell a default route from a narrower one through the same gateway; the covering
routes come from `ip -j route show table all match ADDRESS` instead, which lists every
route whose prefix contains the address, in every table (policy routing included).

Read-only. Everything is written to stderr; there is no stdout payload.
"""

from __future__ import annotations

import functools
import ipaddress
import json
import pathlib
import re
import subprocess
import sys
from typing import Any

#: The installer that creates the bus interface, in this checkout; its name is read from it.
INSTALLER = pathlib.PurePosixPath("files/usr/local/sbin/agent-bus-install")
_IFACE_RE = re.compile(r"^readonly BUS_IFACE=([A-Za-z0-9_.-]+)$", re.MULTILINE)
EXIT_CONFLICT = 1
EXIT_UNREADABLE = 2
EXIT_USAGE = 64
REMEDY = "choose a free address and pass it as --bus-address=<ip>"


def say(message: str) -> None:
    print(message, file=sys.stderr, flush=True)


def bus_iface_in(source: str) -> str:
    """The interface agent-bus-install's `readonly BUS_IFACE=` names, from its source text."""
    found = _IFACE_RE.findall(source)
    if len(found) != 1:
        raise ValueError(f"want exactly one `readonly BUS_IFACE=<name>` in {INSTALLER}, found {found!r}")
    return found[0]


@functools.cache
def bus_iface() -> str:
    """The bus interface this checkout's agent-bus-install creates (the checkout's top is the
    first directory above this file holding ansible.cfg)."""
    here = pathlib.Path(__file__).resolve().parent
    for candidate in (here, *here.parents):
        if (candidate / "ansible.cfg").exists():
            return bus_iface_in((candidate / INSTALLER).read_text(encoding="utf-8"))
        if (candidate / ".git").exists():
            break
    raise ValueError(f"no ansible.cfg above {here}: not inside a fedora-desktop checkout")


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
    reasons, iface = [], bus_iface()
    holders = sorted({
        link.get("ifname", "?")
        for link in links
        if link.get("ifname") != iface
        and any(info.get("local") == address for info in link.get("addr_info", []))
    })
    if holders:
        reasons.append(f"{address} is already assigned to {', '.join(holders)}")
    for route in routes:
        if route.get("dst") == "default" or route.get("dev") == iface:
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
    try:
        bus_iface()
    except (ValueError, OSError) as error:
        say(f"[FAIL] the bus interface's name cannot be read: {error}")
        return EXIT_UNREADABLE
    reasons = conflicts(address, ip_json(["addr", "show"]),
                        ip_json(["route", "show", "table", "all", "match", address]))
    if reasons:
        for reason in reasons:
            say(f"[FAIL] {reason}")
        say(f"[FAIL] {address} cannot be the bus address here; nothing was changed: {REMEDY}")
        return EXIT_CONFLICT
    say(f"{address} is free for {bus_iface()}: on no other interface, under no route but the default")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
