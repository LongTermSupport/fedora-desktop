"""Disk floor, guest-RAM ceiling and the retention sweep (Plan 00110, §9 T6.4, §10).

Two executors and the pure decisions behind them:

    python3 -m helpers.vmtest.retention floor --path DIR --base-size BYTES --step run|rebuild --guest-mib N
        VMTEST-DISK ok|short free=<bytes> need=<bytes>
        VMTEST-RAM ok|over guest_mib=<n> host_mib=<n>
        exit 0 when both hold, 1 otherwise — the caller REFUSES; nothing is cleaned up

    python3 -m helpers.vmtest.retention sweep --lab-dir DIR --checkout DIR --log FILE \\
        [--keep-runs N] [--keep-spool N]
        VMTEST-EVICTED <kind> <name>        one per eviction, also appended to --log
        VMTEST-SWEEP-DONE evicted=<n>
        exit 0, or 2 when a spool directory was refused (nothing evicted there)

Retention keeps the newest --keep-runs runs that PASSED and every run that did
not (their overlays are the diagnosis); drops a leftover build directory of a
fast base (rebuilt in minutes) but keeps a desktop one (hours, per §3.5); and
bounds `quarantine/` and `responses/` on the shared mount — the former is
sandbox-writable and would otherwise be a trivial disk fill — through the
pinned spool descriptors of spool.py, so a symlinked directory is refused
rather than swept. Every eviction is written to the off-mount log first, so
retention is never silent deletion.
"""

from __future__ import annotations

import argparse
import json
import os
import pathlib
import shutil
import stat
import sys
import time
from dataclasses import dataclass

from helpers.vmtest import spool

MARGIN_BYTES = 2 * 1024**3  # headroom below which a run is refused even if the arithmetic fits
RAM_CEILING_FRACTION = 0.75
DEFAULT_KEEP_RUNS = 10
DEFAULT_KEEP_SPOOL = 200
DESKTOP_PREFIX = "desktop-"


@dataclass(frozen=True)
class Verdict:
    ok: bool
    reason: str


@dataclass(frozen=True)
class Entry:
    name: str
    mtime: float
    keep_always: bool


# ── pure decisions ────────────────────────────────────────────────────────────────────────


def needed_for_run(base_size: int) -> int:
    """An overlay can grow to the base's size in the worst case."""
    return base_size + MARGIN_BYTES


def needed_for_rebuild(base_size: int) -> int:
    """Old base and new base coexist until the rename (§3.3)."""
    return 2 * base_size + MARGIN_BYTES


def assess_disk(*, free_bytes: int, needed_bytes: int) -> Verdict:
    if free_bytes >= needed_bytes:
        return Verdict(True, f"{free_bytes} bytes free, {needed_bytes} needed")
    return Verdict(False, f"only {free_bytes} bytes free, {needed_bytes} needed; free space or lower the guest sizing")


def assess_ram(*, guest_mib: int, host_mib: int) -> Verdict:
    ceiling = int(host_mib * RAM_CEILING_FRACTION)
    if guest_mib <= ceiling:
        return Verdict(True, f"guest {guest_mib} MiB within the {ceiling} MiB ceiling of a {host_mib} MiB host")
    return Verdict(False, f"guest {guest_mib} MiB exceeds the {ceiling} MiB ceiling of a {host_mib} MiB host")


def plan_sweep(entries: list[Entry], *, keep: int) -> list[Entry]:
    """Evict the entries that are neither protected nor among the newest `keep` unprotected ones."""
    candidates = sorted((e for e in entries if not e.keep_always), key=lambda e: e.mtime, reverse=True)
    return candidates[keep:]


# ── host facts ────────────────────────────────────────────────────────────────────────────


def host_mib() -> int:
    with open("/proc/meminfo", encoding="utf-8") as handle:
        for line in handle:
            if line.startswith("MemTotal:"):
                return int(line.split()[1]) // 1024
    raise RuntimeError("/proc/meminfo has no MemTotal line")


def free_bytes(path: str) -> int:
    usage = os.statvfs(path)
    return usage.f_bavail * usage.f_frsize


# ── the sweep ─────────────────────────────────────────────────────────────────────────────


class Log:
    def __init__(self, path: pathlib.Path) -> None:
        self.path = path

    def record(self, event: str, kind: str, name: str, detail: str = "") -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(f"{int(time.time())} {event} {kind} {name} {detail}".rstrip() + "\n")


def run_passed(run_dir: pathlib.Path) -> bool:
    """Only a judged `pass` is evictable; an unjudged or non-passing run is kept for diagnosis."""
    try:
        document = json.loads((run_dir / "response.json").read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    return isinstance(document, dict) and document.get("state") == "finished" and document.get("verdict") == "pass"


def sweep_runs(lab_dir: pathlib.Path, keep: int, log: Log) -> int:
    runs = lab_dir / "runs"
    if not runs.is_dir():
        return 0
    entries = []
    for child in runs.iterdir():
        if child.is_dir() and not child.is_symlink():
            entries.append(Entry(name=child.name, mtime=child.stat().st_mtime, keep_always=not run_passed(child)))
    evicted = 0
    for entry in plan_sweep(entries, keep=keep):
        log.record("evicted", "run", entry.name, "passing run beyond the retention cap")
        shutil.rmtree(runs / entry.name)
        print(f"VMTEST-EVICTED run {entry.name}")
        evicted += 1
    return evicted


def sweep_builds(lab_dir: pathlib.Path, log: Log) -> int:
    """A leftover `<base>.build` directory is a failed build; fast bases are cheap to redo."""
    bases = lab_dir / "bases"
    if not bases.is_dir():
        return 0
    evicted = 0
    for child in sorted(bases.iterdir()):
        if child.is_dir() and not child.is_symlink() and child.name.endswith(".build"):
            if child.name.startswith(DESKTOP_PREFIX):
                log.record("kept", "build", child.name, "failed desktop build kept for diagnosis")
                continue
            log.record("evicted", "build", child.name, "failed fast-base build directory")
            shutil.rmtree(child)
            print(f"VMTEST-EVICTED build {child.name}")
            evicted += 1
    return evicted


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
                entries.append(Entry(name=entry_name, mtime=info.st_mtime, keep_always=False))
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


# ── executors ─────────────────────────────────────────────────────────────────────────────


def cmd_floor(args: argparse.Namespace) -> int:
    need = needed_for_run(args.base_size) if args.step == "run" else needed_for_rebuild(args.base_size)
    disk = assess_disk(free_bytes=free_bytes(args.path), needed_bytes=need)
    ram = assess_ram(guest_mib=args.guest_mib, host_mib=host_mib())
    print(f"VMTEST-DISK {'ok' if disk.ok else 'short'} free={free_bytes(args.path)} need={need}")
    print(f"VMTEST-RAM {'ok' if ram.ok else 'over'} guest_mib={args.guest_mib} host_mib={host_mib()}")
    for verdict in (disk, ram):
        if not verdict.ok:
            print(f"ERROR: {verdict.reason}", file=sys.stderr)
    return 0 if disk.ok and ram.ok else 1


def cmd_sweep(args: argparse.Namespace) -> int:
    log = Log(pathlib.Path(args.log))
    lab_dir = pathlib.Path(args.lab_dir)
    evicted = sweep_runs(lab_dir, args.keep_runs, log) + sweep_builds(lab_dir, log)
    rc = 0
    try:
        evicted += sweep_spool(args.checkout, args.keep_spool, log)
    except spool.SpoolRefusal as exc:
        log.record("refused", "spool", "-", str(exc))
        print(f"REFUSED: {exc}", file=sys.stderr)
        rc = 2
    print(f"VMTEST-SWEEP-DONE evicted={evicted}")
    return rc


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    floor = sub.add_parser("floor", help="refuse when the disk or the host RAM cannot carry the next step")
    floor.add_argument("--path", required=True, help="a path on the filesystem the lab writes to")
    floor.add_argument("--base-size", type=int, required=True, help="bytes of the base the step works on")
    floor.add_argument("--step", choices=("run", "rebuild"), required=True)
    floor.add_argument("--guest-mib", type=int, required=True)
    floor.set_defaults(func=cmd_floor)
    sweep = sub.add_parser("sweep", help="apply the retention policy; every eviction is logged")
    sweep.add_argument("--lab-dir", required=True)
    sweep.add_argument("--checkout", required=True, help="the checkout whose spool to bound; absent is fine")
    sweep.add_argument("--log", required=True, help="the off-mount retention log")
    sweep.add_argument("--keep-runs", type=int, default=DEFAULT_KEEP_RUNS)
    sweep.add_argument("--keep-spool", type=int, default=DEFAULT_KEEP_SPOOL)
    sweep.set_defaults(func=cmd_sweep)
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
