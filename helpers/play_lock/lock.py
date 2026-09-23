"""The one lock every play run on a host takes, so two can never overlap (Plan 00137 T1.4).

Three things run plays on a host, and before this nothing stopped them running at once:
a human running a play by path (`run.bash` single-play mode, which every play's shebang
enters), the panel's `fedora-desktop-health --run-play` (which runs the play through that
same shebang), and Plan 00137's unattended cycle. Two Ansible runs writing the same units,
files and packages is a race nobody would diagnose from the output.

**WHERE.** `fedora-desktop-plays.lock` in the user's runtime directory
(`$XDG_RUNTIME_DIR`, else `/run/user/<uid>`). Not in a checkout: a human's checkout and the
cycle's deploy-only clone are different directories running plays against the same host.
Not a new system path: `/run/user/<uid>` already exists for any user with a session or
linger, so nothing has to be deployed before the lock works. That matters because the
first thing a lock-taking `run.bash` would ever run is the play that deploys it.

**ROOT IS A CALLER TOO.** The cycle's orchestrator is root and runs the plays as the user,
so it opens this file in a directory the USER controls. So nothing here follows a link:
the lock file is opened `O_NOFOLLOW`, created only `O_CREAT|O_EXCL` (which refuses an
existing path, dangling links included), must be a regular file owned by the user, and
the runtime directory itself must be a real directory owned by the user and not writable
by anyone else. A file root creates is handed to the user with `fchown`.

**HOLDING IT.** A flock(2) held for the life of the run. `run.bash` takes it with flock(1)
on a descriptor it keeps open, so its children inherit the hold. A holder can DELEGATE: it
exports `FEDORA_DESKTOP_PLAY_LOCK_FD=<fd>` and lets the child inherit that descriptor.
flock belongs to the open file description, so the child's descriptor already holds the
lock, and `held` proves it (same inode as the lock path, and a non-blocking re-lock on
that description succeeds) before the child treats it as its own. This is how the
orchestrator keeps the lock across the whole cycle while `run.bash` runs each play inside
it, instead of the two deadlocking.

The lock is cooperative. Anyone can still run `ansible-playbook` directly; what it
prevents is the three routes above colliding by accident.

Run it: `python3 -m helpers.play_lock.lock {path|held} [--user NAME]`
"""

from __future__ import annotations

import argparse
import errno
import fcntl
import os
import pwd
import stat
import sys
import time
from collections.abc import Callable, Mapping
from typing import TextIO

LOCK_NAME = "fedora-desktop-plays.lock"
#: The environment variable a holder sets to delegate its descriptor to a child.
FD_ENV = "FEDORA_DESKTOP_PLAY_LOCK_FD"

EXIT_OK = 0
EXIT_NOT_HELD = 1
EXIT_USAGE = 2
#: The lock could not be opened safely, or a delegation was bogus. Not "busy".
EXIT_ERROR = 3
#: Another run holds the lock. EX_TEMPFAIL, so a caller can tell "try later" from "broken".
EXIT_BUSY = 75

_NOTE_MAX = 4096


class LockError(Exception):
    """The lock cannot be used safely. The message says why and is meant for a human."""


def lock_path(runtime_dir: str) -> str:
    return os.path.join(runtime_dir, LOCK_NAME)


def runtime_dir_for(uid: int, *, env: Mapping[str, str], euid: int) -> str:
    """The runtime directory whose lock serialises `uid`'s play runs.

    The caller's own `XDG_RUNTIME_DIR` only when the caller IS that user: root serving a
    user has root's variable, and locking there would serialise nothing.
    """
    if euid == uid and env.get("XDG_RUNTIME_DIR"):
        return env["XDG_RUNTIME_DIR"]
    return f"/run/user/{uid}"


def _check_runtime_dir(runtime_dir: str, uid: int) -> None:
    try:
        st = os.lstat(runtime_dir)
    except FileNotFoundError:
        raise LockError(
            f"the runtime directory {runtime_dir} does not exist, so there is nowhere to "
            "take the play lock; it exists for any user with a login session or linger "
            "(play-systemd-user-tweaks.yml enables linger)"
        ) from None
    if stat.S_ISLNK(st.st_mode) or not stat.S_ISDIR(st.st_mode):
        raise LockError(f"the runtime directory {runtime_dir} is not a real directory")
    if st.st_uid != uid:
        raise LockError(f"the runtime directory {runtime_dir} is owned by uid {st.st_uid}, not {uid}")
    if st.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        raise LockError(f"the runtime directory {runtime_dir} is writable by others")


def open_lock(runtime_dir: str, uid: int, gid: int) -> int:
    """Open (creating if needed) the lock file for `uid`, refusing anything unsafe.

    Returns a read-write descriptor with close-on-exec set; a holder that delegates
    clears that flag on purpose.
    """
    _check_runtime_dir(runtime_dir, uid)
    path = lock_path(runtime_dir)
    flags = os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC
    for _attempt in range(2):
        try:
            fd = os.open(path, flags)
        except FileNotFoundError:
            try:
                fd = os.open(path, flags | os.O_CREAT | os.O_EXCL, 0o600)
            except FileExistsError:
                # Someone created it between the two opens; the next pass opens theirs.
                continue
            if os.geteuid() != uid:
                os.fchown(fd, uid, gid)
        except OSError as error:
            # O_NOFOLLOW on a symlink is ELOOP; O_EXCL on a dangling one is EEXIST, which
            # the loop above sees as FileExistsError and retries into the ELOOP here.
            if error.errno == errno.ELOOP or _is_symlink(path):
                raise LockError(f"{path} is a symlink, and the play lock never follows one") from None
            raise LockError(f"{path} cannot be opened: {error.strerror}") from None
        st = os.fstat(fd)
        if not stat.S_ISREG(st.st_mode) or st.st_uid != uid:
            os.close(fd)
            raise LockError(f"{path} is not a regular file owned by uid {uid}")
        return fd
    raise LockError(f"{path} kept changing while it was being opened")


def _is_symlink(path: str) -> bool:
    try:
        return stat.S_ISLNK(os.lstat(path).st_mode)
    except FileNotFoundError:
        return False


def acquire(
    fd: int,
    *,
    wait_seconds: float = 0,
    sleep: Callable[[float], None] = time.sleep,
    clock: Callable[[], float] = time.monotonic,
) -> bool:
    """Take the lock on `fd`. Non-blocking unless `wait_seconds` gives a bound."""
    deadline = clock() + wait_seconds
    while True:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return True
        except BlockingIOError:
            remaining = deadline - clock()
            if remaining <= 0:
                return False
            sleep(min(1.0, remaining))


def write_note(fd: int, what: str) -> None:
    """Record who holds the lock, for the message a refused caller prints.

    Only ever written by the holder, so a refused caller reading it sees the current one.
    A note left by an earlier holder is harmless: it is only shown while the lock is held.
    """
    stamp = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    os.ftruncate(fd, 0)
    os.pwrite(fd, f"pid={os.getpid()} what={what} since={stamp}\n".encode(), 0)


def read_note(fd: int) -> str:
    return os.pread(fd, _NOTE_MAX, 0).decode("utf-8", errors="replace").strip()


def held(env: Mapping[str, str], runtime_dir: str) -> tuple[bool, str]:
    """Whether this process inherited a descriptor that holds the play lock.

    `(False, "")` when nothing was delegated. `(False, reason)` when a delegation was
    claimed and is not real, which a caller must treat as an error: continuing without
    the lock would be the overlap this module exists to prevent.
    """
    value = env.get(FD_ENV, "")
    if not value:
        return False, ""
    if not value.isdigit():
        return False, f"{FD_ENV}={value!r} is not a file descriptor number"
    fd = int(value)
    try:
        fd_stat = os.fstat(fd)
    except OSError as error:
        return False, f"{FD_ENV}={fd} is not an open descriptor ({error.strerror})"
    try:
        path_stat = os.lstat(lock_path(runtime_dir))
    except FileNotFoundError:
        return False, f"{FD_ENV}={fd} was set, but {lock_path(runtime_dir)} does not exist"
    if (fd_stat.st_dev, fd_stat.st_ino) != (path_stat.st_dev, path_stat.st_ino):
        return False, f"{FD_ENV}={fd} is not the play lock {lock_path(runtime_dir)}"
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        return False, f"{FD_ENV}={fd} is the play lock file but does not hold the lock"
    return True, ""


def _resolve_user(name: str | None, euid: int) -> tuple[int, int]:
    if name is None:
        if euid == 0:
            raise LockError("running as root: pass --user NAME for the user whose play runs this serialises")
        return euid, os.getegid()
    try:
        entry = pwd.getpwnam(name)
    except KeyError:
        raise LockError(f"no such user {name!r}") from None
    return entry.pw_uid, entry.pw_gid


def main(
    argv: list[str] | None = None,
    *,
    env: Mapping[str, str] | None = None,
    stdout: TextIO | None = None,
    stderr: TextIO | None = None,
    euid: int | None = None,
) -> int:
    out: TextIO = stdout if stdout is not None else sys.stdout
    err: TextIO = stderr if stderr is not None else sys.stderr
    environ = os.environ if env is None else env
    effective = os.geteuid() if euid is None else euid

    parser = argparse.ArgumentParser(description="The shared play-run lock (Plan 00137 T1.4).")
    parser.add_argument("command", choices=("path", "held"))
    parser.add_argument("--user", help="the user whose play runs to serialise (required as root)")
    try:
        args = parser.parse_args(argv)
    except SystemExit as exit_request:
        return EXIT_USAGE if exit_request.code else EXIT_OK

    try:
        uid, gid = _resolve_user(args.user, effective)
        runtime_dir = runtime_dir_for(uid, env=environ, euid=effective)
        if args.command == "path":
            os.close(open_lock(runtime_dir, uid, gid))
            out.write(lock_path(runtime_dir) + "\n")
            return EXIT_OK
        ok, why = held(environ, runtime_dir)
    except LockError as error:
        err.write(f"play-lock: {error}\n")
        return EXIT_ERROR
    if ok:
        return EXIT_OK
    if why:
        err.write(f"play-lock: {why}\n")
        return EXIT_ERROR
    return EXIT_NOT_HELD


if __name__ == "__main__":
    sys.exit(main())
