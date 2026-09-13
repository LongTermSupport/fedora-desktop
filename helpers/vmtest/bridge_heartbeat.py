"""The timer-driven bridge liveness writer (Plan 00110, DESIGN.md §6.5).

    python3 -m helpers.vmtest.bridge_heartbeat --checkout DIR --slug SLUG --state-dir DIR

Every interval the timer runs this once. It asks systemd for the state of the
bridge's path and service units, reads the in-flight lock, and writes the
heartbeat document into `diagnostics/` through a pinned directory fd (a
planted symlink at the heartbeat's fixed name is replaced, not followed). It
REPORTS a failed unit with the remedy a human must run; it never repairs one.

A refused spool (a symlinked directory) is logged off the mount and exits 2
with nothing written; a `systemctl` failure exits 1 with nothing written.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import subprocess
import sys
import time

from helpers.vmtest import spool, verdict

HEARTBEAT_FILE = "bridge-heartbeat.json"


def unit_state(systemctl: str, unit: str) -> dict:
    result = subprocess.run(
        [systemctl, "--user", "show", "-p", "ActiveState", "-p", "Result", unit],
        capture_output=True,
        text=True,
        check=True,
    )
    fields = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
    return {"active_state": fields.get("ActiveState", "unknown"), "result": fields.get("Result", "unknown")}


def in_flight(state_dir: pathlib.Path) -> str | None:
    try:
        return (state_dir / "in-flight").read_text(encoding="utf-8").strip() or None
    except FileNotFoundError:
        return None


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--checkout", required=True)
    parser.add_argument("--slug", required=True)
    parser.add_argument("--state-dir", required=True)
    parser.add_argument("--systemctl", default="/usr/bin/systemctl")
    parser.add_argument("--now", type=int, default=None)
    args = parser.parse_args(argv)
    now = args.now if args.now is not None else int(time.time())
    state_dir = pathlib.Path(args.state_dir)

    try:
        path_unit = unit_state(args.systemctl, f"vmtest-bridge@{args.slug}.path")
        service_unit = unit_state(args.systemctl, f"vmtest-bridge@{args.slug}.service")
    except subprocess.CalledProcessError as exc:
        print(f"ERROR: systemctl show failed (exit {exc.returncode}): {exc.stderr.strip()}", file=sys.stderr)
        return 1
    document = verdict.heartbeat(now=now, path_unit=path_unit, service_unit=service_unit, in_flight=in_flight(state_dir), slug=args.slug)

    try:
        root_fd = spool.open_root(args.checkout)
        try:
            diagnostics_fd = spool.open_subdir(root_fd, "diagnostics")
        finally:
            os.close(root_fd)
        try:
            spool.write_file(diagnostics_fd, HEARTBEAT_FILE, (verdict.json.dumps(document, indent=2, sort_keys=True) + "\n").encode())
        finally:
            os.close(diagnostics_fd)
    except spool.SpoolRefusal as exc:
        state_dir.mkdir(parents=True, exist_ok=True)
        with (state_dir / "service.log").open("a", encoding="utf-8") as handle:
            handle.write(f"{now} refused heartbeat {exc}\n")
        print(f"REFUSED: {exc}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
