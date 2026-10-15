"""The LXC bridge's subnet, which every present team admits without being told.

This repository sets LXC up (play-lxc-install-config.yml: lxc-net, the stock bridge
`lxcbr0`), so a team's effective allow_from is the declared list plus that bridge's
subnet. The subnet is read live (`ip -j -4 addr show dev lxcbr0`) by one function,
`live_subnet`, and combined with the declared list by one function, `effective`. Both
callers use them: play-agent-bus.yml renders every team file from `apply`'s output (the
installer, and through it the firewall rule, the unit drop-in and the homeserver config,
all see that list, and its config digest is stamped), and `declare.py status` digests the
same effective list, so a stamp reads back as current.

    python3 -m helpers.agent_bus.bridge --teams '<json>'

prints `{"note": "<one line>", "teams": [...]}`. No such bridge (LXC is not installed) is
said in the note and no team changes. A bridge with no IPv4 address, or any other failure
of `ip`, exits 1 with the reason on stderr.

Stdlib only; no file or network I/O.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import subprocess
import sys
from collections.abc import Callable, Mapping, Sequence

#: The bridge play-lxc-install-config.yml sets up (lxc-net's stock one).
BRIDGE = "lxcbr0"

#: Runs an argument list, returns (exit code, stdout, stderr); tests inject a fake.
Runner = Callable[[Sequence[str]], tuple[int, str, str]]


class BridgeError(ValueError):
    """The bridge exists but its subnet cannot be read."""


def _run(argv: Sequence[str]) -> tuple[int, str, str]:
    done = subprocess.run(list(argv), capture_output=True, text=True, check=False)
    return done.returncode, done.stdout, done.stderr


def live_subnet(run: Runner = _run) -> str | None:
    """The bridge's IPv4 network as a canonical CIDR; None when the bridge does not exist."""
    argv = ["ip", "-j", "-4", "addr", "show", "dev", BRIDGE]
    try:
        code, out, err = run(argv)
    except OSError as exc:
        raise BridgeError(f"cannot run ip to read bridge {BRIDGE}: {exc}") from None
    if code != 0:
        if "does not exist" in err:
            return None
        raise BridgeError(f"`{' '.join(argv)}` failed ({code}): {err.strip()}")
    try:
        inet = [a for link in json.loads(out) for a in link.get("addr_info", []) if a.get("family") == "inet"]
    except (ValueError, AttributeError, TypeError):
        raise BridgeError(f"bridge {BRIDGE}: ip printed output that is not its JSON address list") from None
    if not inet:
        raise BridgeError(f"bridge {BRIDGE} exists but has no IPv4 address: lxc-net is broken; "
                          f"check `systemctl status lxc-net` and play-lxc-install-config.yml")
    spec = f"{inet[0].get('local')}/{inet[0].get('prefixlen')}"
    try:
        return str(ipaddress.ip_network(spec, strict=False))
    except ValueError as exc:
        raise BridgeError(f"bridge {BRIDGE}: {spec!r} is not a network: {exc}") from None


def effective(declared: Sequence[str] | None, bridge_cidr: str | None) -> list[str]:
    """Declared `allow_from` then the bridge subnet, once each."""
    out = list(dict.fromkeys(declared or []))
    if bridge_cidr and bridge_cidr not in out:
        out.append(bridge_cidr)
    return out


def apply(cidr: str | None, teams: Sequence[Mapping]) -> dict:
    """Every team with its effective allow_from, and the one-line note for the play."""
    if cidr is None:
        note = f"no LXC bridge {BRIDGE} on this host (LXC not installed): teams admit only their declared allow_from"
    else:
        note = f"LXC bridge {BRIDGE} subnet {cidr} is admitted by every present team"
    return {"note": note, "teams": [{**t, "allow_from": effective(t.get("allow_from"), cidr)} for t in teams]}


def main(argv: Sequence[str] | None = None, run: Runner = _run) -> int:
    parser = argparse.ArgumentParser(prog="bridge", description=__doc__.split("\n\n")[0])
    parser.add_argument("--teams", required=True, help="JSON list of team entries")
    args = parser.parse_args(argv)
    try:
        result = apply(live_subnet(run), json.loads(args.teams))
    except (BridgeError, json.JSONDecodeError) as exc:
        print(f"bridge: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    sys.exit(main())
