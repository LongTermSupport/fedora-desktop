#!/usr/bin/env python3
"""The bus-process probe behind M1 (b) and M2 (c) (e) (SPEC-hot-control.md H33, H34). RUN IN
THE SESSION'S CONTAINER: `podman exec <c> python3 -I <this> <check> /proc`.

A bus process is a program, not a word in an argv: claude itself names the plugin directory
`.../plugin/pingbus` in its argv, and a prompt may say "pingbus". So a process is a bus process
when its program is the pingbus or agent-bus entry point: argv[0], or argv[1] when argv[0] is a
python interpreter (the kernel runs a script's shebang that way), by basename. A pingbus run
from a source tree (`python3 -c ...`) is out of scope: a ccy container runs the zipapp.

  keeper-only     (M1 b) exactly one bus process, `pingbus seat keep --launch --parent ...`,
                  holding keeper.lock; no other process holds keeper.lock or seat.lock
  at-most-keeper  (M2 c) no bus process, or one: `pingbus seat keep`
  none            (M2 e) no bus process

Prints "<pid> <locks held, or -> <argv>" for each process it judged on stdout; a failure on
stderr, exit 1; usage 64.
"""

from __future__ import annotations

import dataclasses
import os
import pathlib
import sys
from collections.abc import Sequence

EXIT_OK, EXIT_FAIL, EXIT_USAGE = 0, 1, 64
BUS_PROGRAMS = ("pingbus", "agent-bus")
LOCK_NAMES = ("keeper.lock", "seat.lock")


class Failed(Exception):
    """The check does not hold; the message says what was seen."""


@dataclasses.dataclass(frozen=True)
class Process:
    pid: int
    argv: tuple[str, ...]
    locks: tuple[str, ...]

    def line(self) -> str:
        return f"{self.pid} {','.join(self.locks) or '-'} {' '.join(self.argv)}"


def _program_words(argv: Sequence[str]) -> list[str]:
    """The argv from the program on: past a python interpreter and its options."""
    words = list(argv)
    if words and os.path.basename(words[0]).startswith("python"):
        words = words[1:]
        while words and words[0].startswith("-"):
            words = words[1:]
    return words


def program(argv: Sequence[str]) -> str | None:
    """The bus program a process runs, or None for any other process."""
    words = _program_words(argv)
    name = os.path.basename(words[0]) if words else ""
    return name if name in BUS_PROGRAMS else None


def _subcommand(argv: Sequence[str]) -> list[str]:
    """The words after the program."""
    return _program_words(argv)[1:]


def is_keeper(argv: Sequence[str]) -> bool:
    return program(argv) == "pingbus" and _subcommand(argv)[:2] == ["seat", "keep"]


def is_launch_keeper(argv: Sequence[str]) -> bool:
    words = _subcommand(argv)
    return is_keeper(argv) and "--launch" in words and "--parent" in words


def _locks(fd_dir: pathlib.Path) -> tuple[str, ...]:
    found = set()
    try:
        entries = list(fd_dir.iterdir())
    except OSError:
        return ()
    for entry in entries:
        try:
            name = os.path.basename(os.readlink(entry))
        except OSError:
            continue
        if name in LOCK_NAMES:
            found.add(name)
    return tuple(sorted(found))


def processes(proc: pathlib.Path) -> list[Process]:
    """Every process with an argv (a kernel thread has none; a vanished one is skipped)."""
    found = []
    for entry in proc.iterdir():
        if not entry.name.isdigit():
            continue
        try:
            raw = (entry / "cmdline").read_bytes()
        except OSError:
            continue
        argv = tuple(part.decode("utf-8", "replace") for part in raw.split(b"\0")[:-1])
        if argv:
            found.append(Process(int(entry.name), argv, _locks(entry / "fd")))
    return sorted(found, key=lambda p: p.pid)


def bus_processes(procs: Sequence[Process]) -> list[Process]:
    return [p for p in procs if program(p.argv) is not None]


def _seen(procs: Sequence[Process]) -> str:
    return "; ".join(p.line() for p in procs) or "none"


def at_most_keeper(procs: Sequence[Process]) -> list[str]:
    bus = bus_processes(procs)
    if len(bus) > 1 or (bus and not is_keeper(bus[0].argv)):
        raise Failed(f"the session runs bus processes other than one keeper: {_seen(bus)}")
    return [p.line() for p in bus]


def keeper_only(procs: Sequence[Process]) -> list[str]:
    bus = bus_processes(procs)
    if len(bus) != 1 or not is_launch_keeper(bus[0].argv):
        raise Failed(f"want one bus process, `pingbus seat keep --launch --parent ...`, found: {_seen(bus)}")
    keeper = bus[0]
    if "keeper.lock" not in keeper.locks:
        raise Failed(f"the keeper does not hold keeper.lock: {keeper.line()}")
    others = [p for p in procs if p.locks and p.pid != keeper.pid]
    if others:
        raise Failed(f"processes other than the keeper hold a keeper or seat lock: {_seen(others)}")
    return [p.line() for p in procs]


def no_bus(procs: Sequence[Process]) -> list[str]:
    bus = bus_processes(procs)
    if bus:
        raise Failed(f"the session runs bus processes: {_seen(bus)}")
    return []


CHECKS = {"keeper-only": keeper_only, "at-most-keeper": at_most_keeper, "none": no_bus}


def main(argv: Sequence[str]) -> int:
    if len(argv) != 2 or argv[0] not in CHECKS:
        print(__doc__, file=sys.stderr)
        return EXIT_USAGE
    try:
        lines = CHECKS[argv[0]](processes(pathlib.Path(argv[1])))
    except (Failed, OSError) as exc:
        print(f"m2_procs: {exc}", file=sys.stderr)
        return EXIT_FAIL
    for line in lines:
        print(line)
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
