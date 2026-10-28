"""The bridge spool's retention sweep (Plan 00110, §9 T6.4).

    python3 -m helpers.vmtest.retention sweep-spool --checkout DIR --log FILE [--keep-spool N]
        VMTEST-EVICTED quarantine|responses <name>   one per eviction, also appended to --log
        VMTEST-SPOOL-SWEEP-DONE evicted=<n>
        exit 0, or 2 when a spool directory was refused (nothing evicted there)

It bounds `quarantine/` and `responses/` on the shared mount (the former is
sandbox-writable and would otherwise be a trivial disk fill) to their newest
--keep-spool files, through the pinned spool descriptors of spool.py, so a
symlinked directory is refused rather than swept. Every eviction is written to
the off-mount log first, so retention is never silent deletion.

This is the spool's half only, Python because spool.py is shared with the bridge
helpers. The lab's own retention (the disk and RAM floors, the run and build
sweep, the overlay prune plan) is `vmtest`'s, in src-go/internal/vmtest, which
runs this after its own sweep.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import stat
import sys
import time
from dataclasses import dataclass

from helpers.vmtest import spool

DEFAULT_KEEP_SPOOL = 200


@dataclass(frozen=True)
class Entry:
    name: str
    mtime: float


def plan_sweep(entries: list[Entry], *, keep: int) -> list[Entry]:
    """Evict the entries that are not among the newest `keep`."""
    return sorted(entries, key=lambda e: e.mtime, reverse=True)[keep:]


class Log:
    def __init__(self, path: pathlib.Path) -> None:
        self.path = path

    def record(self, event: str, kind: str, name: str, detail: str = "") -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(f"{int(time.time())} {event} {kind} {name} {detail}".rstrip() + "\n")


def sweep_spool_dir(root_fd: int, name: str, keep: int, log: Log) -> int:
    """Bound one host-facing spool directory to its newest `keep` regular files, by pinned fd."""
    dir_fd = spool.open_subdir(root_fd, name)
    try:
        listing_fd = os.open(".", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=dir_fd)
        try:
            names = os.listdir(listing_fd)
        finally:
            os.close(listing_fd)
        entries = []
        for entry_name in names:
            info = os.stat(entry_name, dir_fd=dir_fd, follow_symlinks=False)
            if stat.S_ISREG(info.st_mode):
                entries.append(Entry(name=entry_name, mtime=info.st_mtime))
        evicted = 0
        for entry in plan_sweep(entries, keep=keep):
            log.record("evicted", name, entry.name, "beyond the spool retention cap")
            os.unlink(entry.name, dir_fd=dir_fd)
            print(f"VMTEST-EVICTED {name} {entry.name}")
            evicted += 1
        return evicted
    finally:
        os.close(dir_fd)


def sweep_spool(checkout: str, keep: int, log: Log) -> int:
    bridge = pathlib.Path(checkout) / "untracked" / "vmtest-bridge"
    if not bridge.is_dir():
        return 0
    root_fd = spool.open_root(checkout)
    try:
        return sweep_spool_dir(root_fd, "quarantine", keep, log) + sweep_spool_dir(root_fd, "responses", keep, log)
    finally:
        os.close(root_fd)


def cmd_sweep_spool(args: argparse.Namespace) -> int:
    log = Log(pathlib.Path(args.log))
    try:
        evicted = sweep_spool(args.checkout, args.keep_spool, log)
    except spool.SpoolRefusal as exc:
        log.record("refused", "spool", "-", str(exc))
        print(f"REFUSED: {exc}", file=sys.stderr)
        return 2
    print(f"VMTEST-SPOOL-SWEEP-DONE evicted={evicted}")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    sweep = sub.add_parser("sweep-spool", help="bound the bridge spool; every eviction is logged")
    sweep.add_argument("--checkout", required=True, help="the checkout whose spool to bound; absent is fine")
    sweep.add_argument("--log", required=True, help="the off-mount retention log")
    sweep.add_argument("--keep-spool", type=int, default=DEFAULT_KEEP_SPOOL)
    sweep.set_defaults(func=cmd_sweep_spool)
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
