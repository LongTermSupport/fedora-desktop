"""Upgrade UniFi device firmware through the local controller, one device at a time.

Lists the devices the controller marks upgradable. `--check` only reports. `--apply`
upgrades them in order, access points first and switches last (a switch reboot takes
down whatever it carries), and waits after each until the device is back, connected and
on the target version before starting the next, so at most one device is ever down. A
device that does not come back within `--timeout` stops the run: nothing further is
upgraded. A device the controller does not see as connected is refused before anything
is sent, since its upgrade could not be watched.

Marker lines on stdout, for the play's changed_when:
  FIRMWARE-UPGRADE <device> <from> -> <to>   (planned)
  FIRMWARE-PENDING <n>                        (check: n devices would upgrade)
  FIRMWARE-STARTED <device>                   (apply: upgrade sent)
  FIRMWARE-UPGRADED <device> <version>        (apply: back, on the target)
  FIRMWARE-TIMEOUT <device>                   (apply: not back in time; run stopped)
  FIRMWARE-CHANGED                            (apply: every planned device upgraded)
  FIRMWARE-CURRENT                            (nothing to upgrade)

Usage: python3 -m helpers.unifi.upgrade_firmware (--check | --apply)
           [--order NAME,NAME,...] [--timeout SECONDS] [--interval SECONDS]
           [--url URL] [--user NAME]
"""

import argparse
import os
import sys
import time
from dataclasses import dataclass

from helpers.unifi.client import UnifiClient

DEFAULT_URL = "https://localhost:8443"
DEFAULT_USER = "unifi-admin"
CONNECTED = 1
# Access points first: they reboot alone. Switches last: a switch reboot also drops
# everything wired through it, including access points it powers.
_KIND_ORDER = {"uap": 0, "usw": 1}


@dataclass(frozen=True)
class Upgrade:
    name: str
    mac: str
    current: str
    target: str


def plan_upgrades(devices, order=()):
    """The devices to upgrade, in the order to upgrade them.

    Devices named in `order` come first, in that order (put the least-used first, so a
    bad release shows there and stops the run); the rest follow, access points before
    switches, each kind by name.
    """
    known = {dev.get("name") or dev["mac"] for dev in devices}
    for name in order:
        if name not in known:
            raise ValueError(f"order names '{name}', which the controller does not list")
    rank = {name: position for position, name in enumerate(order)}
    planned = []
    for dev in devices:
        if not dev.get("upgradable"):
            continue
        name = dev.get("name") or dev["mac"]
        if dev.get("state") != CONNECTED:
            raise ValueError(f"{name} is not connected (state {dev.get('state')}); "
                             "its upgrade could not be watched")
        key = (rank.get(name, len(order)), _KIND_ORDER.get(dev.get("type"), 2), name)
        planned.append((key, Upgrade(name, dev["mac"], dev.get("version"),
                                     dev.get("upgrade_to_firmware"))))
    return [upgrade for _, upgrade in sorted(planned, key=lambda p: p[0])]


def upgrade_done(dev, target):
    """True once the device is connected, on the target, with nothing newer offered."""
    return (dev.get("state") == CONNECTED and dev.get("version") == target
            and not dev.get("upgradable"))


def _wait_for(client, upgrade, timeout, interval, clock):
    deadline = clock.time() + timeout
    while clock.time() < deadline:
        clock.sleep(interval)
        for dev in client.get("stat/device"):
            if dev["mac"] == upgrade.mac and upgrade_done(dev, upgrade.target):
                return True
    return False


def main(argv, stdout, stderr, environ, client_factory=UnifiClient, clock=time):
    parser = argparse.ArgumentParser(prog="upgrade_firmware")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true", help="report upgrades only")
    mode.add_argument("--apply", action="store_true", help="upgrade, one device at a time")
    parser.add_argument("--timeout", type=float, default=900,
                        help="seconds to wait for each device to come back")
    parser.add_argument("--interval", type=float, default=15, help="seconds between polls")
    parser.add_argument("--order", default="",
                        help="comma-separated device names to upgrade first, in order")
    parser.add_argument("--url", default=DEFAULT_URL)
    parser.add_argument("--user", default=DEFAULT_USER)
    args = parser.parse_args(argv)

    password = environ.get("UNIFI_ADMIN_PASSWORD", "")
    if not password:
        print("UNIFI_ADMIN_PASSWORD is not set; the play passes the vaulted password in it",
              file=stderr)
        return 2

    client = client_factory(args.url, args.user, password)
    client.login()
    try:
        order = [name.strip() for name in args.order.split(",") if name.strip()]
        upgrades = plan_upgrades(client.get("stat/device"), order)
    except ValueError as error:
        print(f"refusing to upgrade: {error}", file=stderr)
        return 1

    if not upgrades:
        print("FIRMWARE-CURRENT", file=stdout)
        return 0
    for upgrade in upgrades:
        print(f"FIRMWARE-UPGRADE {upgrade.name} {upgrade.current} -> {upgrade.target}",
              file=stdout)
    if args.check:
        print(f"FIRMWARE-PENDING {len(upgrades)}", file=stdout)
        return 0

    for upgrade in upgrades:
        client.post("cmd/devmgr", {"cmd": "upgrade", "mac": upgrade.mac})
        print(f"FIRMWARE-STARTED {upgrade.name}", file=stdout, flush=True)
        if not _wait_for(client, upgrade, args.timeout, args.interval, clock):
            print(f"FIRMWARE-TIMEOUT {upgrade.name}", file=stdout)
            print(f"{upgrade.name} was not back on {upgrade.target} within "
                  f"{args.timeout:.0f} s; stopping before the next device", file=stderr)
            return 1
        print(f"FIRMWARE-UPGRADED {upgrade.name} {upgrade.target}", file=stdout, flush=True)
    print("FIRMWARE-CHANGED", file=stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:], sys.stdout, sys.stderr, os.environ))
