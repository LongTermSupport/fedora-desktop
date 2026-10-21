"""Take a pingbus v1 lock through v1's own code, for the Go lock proofs (locks_peer_test.go).

argv: <checkout> <command> <args...>. The first line out is `pid <n>`. A lock taken is said
`held` and kept until a line on stdin: `release` (or the end of input) lets it go as v1 does,
`die` ends the process still holding it, so what the file says is what a dead holder leaves.
"""

import importlib
import os
import pathlib
import sys

sys.path.insert(0, sys.argv[1])
inbox = importlib.import_module("helpers.pingbus.inbox")
inject = importlib.import_module("helpers.pingbus.inject")
membership = importlib.import_module("helpers.pingbus.membership")
relay = importlib.import_module("helpers.pingbus.relay")
seat = importlib.import_module("helpers.pingbus.seat")


def say(*words: str) -> None:
    print(*words, flush=True)


def hold() -> None:
    if sys.stdin.readline().strip() == "die":
        os._exit(0)


def no_sleep(_seconds: float) -> None:
    """v1's three tries without the 50 ms between them: the Go side holds or it does not."""


def path_of(role: str, args: list[str]) -> pathlib.Path:
    where = pathlib.Path(args[0])
    if role.startswith("sync-"):
        return where / inbox.LOCK_FILE
    if role == "seat":
        return where / seat.LOCK_FILE
    if role == "keeper":
        return where / seat.KEEPER_LOCK
    if role == "watch":
        marks = inject.Marks(where, args[1])
        marks.ensure_dir()
        return marks.watch_lock_path()
    raise SystemExit(f"v1locks: no lock role {role}")


def take(role: str, args: list[str]) -> None:
    try:
        if role.startswith("sync-"):
            state = inbox.TeamState(pathlib.Path(args[0]))
            lock = inbox.acquire_lock(state, role.removeprefix("sync-"), sleep=no_sleep)
        elif role == "seat":
            lock = inbox.acquire_lock_at(
                path_of(role, args),
                inbox.SEAT_KIND,
                claimed_ms=int(args[1]),
                sleep=no_sleep,
            )
        elif role == "keeper":
            lock = inbox.acquire_lock_at(
                path_of(role, args), inbox.KEEPER_KIND, sleep=no_sleep
            )
        else:
            lock = inbox.acquire_lock_at(path_of(role, args), "watch", sleep=no_sleep)
    except inbox.Busy as busy:
        say("busy", busy.holder)
        return
    say("held:" + lock.previous)
    hold()
    lock.release()
    say("released")


def probe(role: str, args: list[str]) -> None:
    holder = inbox.probe_lock_at(path_of(role, args))
    say("free" if holder is None else "held " + holder)


def outbox(state_dir: str) -> None:
    say("waiting")
    with inbox.TeamState(pathlib.Path(state_dir)).outbox():
        say("held")
        hold()
    say("released")


def delivery(home: str, block: bool) -> None:
    if block:
        say("waiting")
    try:
        with relay.Spool(pathlib.Path(home)).exclusive(block=block):
            say("held")
            hold()
    except BlockingIOError:
        say("busy")
        return
    say("released")


def told(home: str) -> None:
    with membership.telling(pathlib.Path(home)) as mine:
        if not mine:
            say("busy")
            return
        say("held")
        hold()
    say("released")


def fds(path: str) -> None:
    """Whether this process, started by the Go side, has any descriptor open on path."""
    found = any(
        os.readlink(f"/proc/self/fd/{fd}") == path
        for fd in os.listdir("/proc/self/fd")
        if os.path.islink(f"/proc/self/fd/{fd}")
    )
    say("inherited" if found else "none")


def main() -> None:
    say("pid", str(os.getpid()))
    command, args = sys.argv[2], sys.argv[3:]
    if command == "path":
        take(args[0], args[1:])
    elif command == "probe":
        probe(args[0], args[1:])
    elif command == "claim":
        claim = inbox.read_claim_at(pathlib.Path(args[0]) / seat.LOCK_FILE)
        say("none" if claim is None else str(claim))
    elif command == "outbox":
        outbox(args[0])
    elif command in ("delivery", "try-delivery"):
        delivery(args[0], command == "delivery")
    elif command == "told":
        told(args[0])
    elif command == "fds":
        fds(args[0])
    elif command == "keep":
        # Keep the descriptors the Go side handed down open, touching none, until told.
        say("kept")
        sys.stdin.readline()
    else:
        raise SystemExit(f"v1locks: no command {command}")


main()
