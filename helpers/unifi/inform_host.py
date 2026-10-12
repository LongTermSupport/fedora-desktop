"""Pin the address UniFi devices inform to: the controller's hostname plus its override.

Without the override the controller tells each device to inform to the host's current
address, which on a laptop changes with the connection and strands every device in
re-adoption. `--apply` sets `super_identity.hostname` to `--host` and turns on
`super_mgmt.override_inform_host`; devices take the new address at their next
provisioning. `--check` only reports.

Marker lines on stdout: INFORM-HOST-OK, INFORM-HOST-PENDING (check),
INFORM-HOST-CHANGED (apply).

Usage: python3 -m helpers.unifi.inform_host (--check | --apply) --host ADDRESS
           [--url URL] [--user NAME]
"""

import argparse
import os
import sys

from helpers.unifi.client import UnifiClient

DEFAULT_URL = "https://localhost:8443"
DEFAULT_USER = "unifi-admin"


def _setting(data, key):
    for item in data:
        if item.get("key") == key:
            return item
    raise ValueError(f"the controller has no '{key}' setting")


def plan(data, host):
    """The (path, body) PUTs that pin informs to `host`; empty when already pinned."""
    identity = _setting(data, "super_identity")
    mgmt = _setting(data, "super_mgmt")
    puts = []
    if identity.get("hostname") != host:
        puts.append((f"rest/setting/super_identity/{identity['_id']}", {"hostname": host}))
    if mgmt.get("override_inform_host") is not True:
        puts.append((f"rest/setting/super_mgmt/{mgmt['_id']}", {"override_inform_host": True}))
    return puts


def main(argv, stdout, stderr, environ, client_factory=UnifiClient):
    parser = argparse.ArgumentParser(prog="inform_host")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--apply", action="store_true")
    parser.add_argument("--host", required=True, help="the address devices inform to")
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
        puts = plan(client.get("rest/setting"), args.host)
    except ValueError as error:
        print(f"cannot pin the inform host: {error}", file=stderr)
        return 1
    if not puts:
        print("INFORM-HOST-OK", file=stdout)
        return 0
    if args.check:
        print("INFORM-HOST-PENDING", file=stdout)
        return 0
    for path, body in puts:
        client.put(path, body)
    print("INFORM-HOST-CHANGED", file=stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:], sys.stdout, sys.stderr, os.environ))
