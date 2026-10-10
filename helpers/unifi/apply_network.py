"""Check or apply desired access-point radio state on the local UniFi controller.

Reads the desired state as JSON on stdin (the play passes `network.yml` through
`to_json`), the admin password from UNIFI_ADMIN_PASSWORD, and talks to the controller's
API. `--check` only reports; `--apply` first adopts each device listed under `adopt`
that is awaiting adoption (the controller then provisions it: a switch may restart its
ports and with them any PoE-powered devices), then sends each changed device's radio
table, which makes the controller re-provision that access point (its radios restart).

Marker lines on stdout, for the play's changed_when/failed_when:
  UNIFI-ADOPT <device> <mac>   (a listed device is awaiting adoption)
  UNIFI-ADOPTED <device>       (apply: adoption sent)
  UNIFI-DIFF <device> <band> <field>: <current> -> <desired>
  UNIFI-PENDING <n>      (check: n devices would change)
  UNIFI-APPLIED <device> (apply: one device sent)
  UNIFI-CHANGED          (apply: at least one device sent)
  UNIFI-NO-CHANGES       (nothing differs)

Usage: python3 -m helpers.unifi.apply_network (--check | --apply)
           [--url URL] [--user NAME] < desired.json
"""

import argparse
import json
import os
import sys

from helpers.unifi.client import UnifiClient
from helpers.unifi.desired import DesiredStateError, plan_adoptions, plan_changes

DEFAULT_URL = "https://localhost:8443"
DEFAULT_USER = "unifi-admin"


def main(argv, stdin, stdout, stderr, environ, client_factory=UnifiClient):
    parser = argparse.ArgumentParser(prog="apply_network")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true", help="report differences only")
    mode.add_argument("--apply", action="store_true", help="send the differences")
    parser.add_argument("--url", default=DEFAULT_URL)
    parser.add_argument("--user", default=DEFAULT_USER)
    args = parser.parse_args(argv)

    password = environ.get("UNIFI_ADMIN_PASSWORD", "")
    if not password:
        print("UNIFI_ADMIN_PASSWORD is not set; the play passes the vaulted password in it",
              file=stderr)
        return 2

    desired = json.load(stdin)
    client = client_factory(args.url, args.user, password)
    client.login()
    devices = client.get("stat/device")
    try:
        adoptions = plan_adoptions(devices, desired)
        changes = plan_changes(devices, desired)
    except DesiredStateError as error:
        print(f"desired state does not fit the controller's devices: {error}", file=stderr)
        return 1

    if not adoptions and not changes:
        print("UNIFI-NO-CHANGES", file=stdout)
        return 0
    for name, mac in adoptions:
        print(f"UNIFI-ADOPT {name} {mac}", file=stdout)
    for change in changes:
        for band, field, current, wanted in change.diffs:
            print(f"UNIFI-DIFF {change.device_name} {band} {field}: {current} -> {wanted}",
                  file=stdout)
    if args.check:
        print(f"UNIFI-PENDING {len(adoptions) + len(changes)}", file=stdout)
        return 0
    for name, mac in adoptions:
        client.post("cmd/devmgr", {"cmd": "adopt", "mac": mac})
        print(f"UNIFI-ADOPTED {name}", file=stdout)
    for change in changes:
        client.put(f"rest/device/{change.device_id}", {"radio_table": change.radio_table})
        print(f"UNIFI-APPLIED {change.device_name}", file=stdout)
    print("UNIFI-CHANGED", file=stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:], sys.stdin, sys.stdout, sys.stderr, os.environ))
