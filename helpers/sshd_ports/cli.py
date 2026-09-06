#!/usr/bin/env python3
"""Executor: print every TCP port sshd is configured to listen on, one per line.

Thin side-effecting wrapper around the pure parser in core.py. Runs `sshd -T`,
which is an OFFLINE parse of the effective configuration by the sshd binary
itself — it resolves `Include` directives, drop-ins and repeated directives the
way the daemon will, without querying (or needing) a running daemon. It reads
the config files, so it must run as root.

Invoked as a module from the repo root:

    python3 -m helpers.sshd_ports.cli

Output is the payload — bare port numbers on stdout, nothing else — so a caller
can loop over the lines directly.
"""

from __future__ import annotations

import argparse
import subprocess

from helpers.sshd_ports import core

DEFAULT_SSHD_PATH = "/usr/sbin/sshd"


def collect(sshd_path: str = DEFAULT_SSHD_PATH) -> list[str]:
    """Ports from `<sshd_path> -T`.

    `check=True`: an sshd that cannot parse its own configuration is a hard
    stop. Returning an empty list instead would permit no ports at all and read
    as "this machine has no SSH", which is the failure this whole path exists
    to prevent.
    """
    result = subprocess.run(
        [sshd_path, "-T"],
        capture_output=True,
        text=True,
        check=True,
    )
    return core.parse_ports(result.stdout)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--sshd-path",
        default=DEFAULT_SSHD_PATH,
        help=f"Path to the sshd binary (default: {DEFAULT_SSHD_PATH}).",
    )
    args = parser.parse_args(argv)
    for port in collect(args.sshd_path):
        print(port)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
