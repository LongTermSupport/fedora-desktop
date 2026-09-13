"""Symlink-safe, read-once I/O for the bridge spool (Plan 00110, DESIGN.md §6.3).

The spool is `<checkout>/untracked/vmtest-bridge/`, inside the bind-mounted
checkout. The sandbox can restructure it at will — replace any directory with a
symlink to `~/.ssh`, `~/.config`, the policy directory — and the host writes
into it as the host user, so ownership is no control (rootless podman maps
container root onto the host uid). Every host operation here therefore:

- reaches the spool root by a COMPONENT-WISE walk with `O_NOFOLLOW` at every
  step, refusing on a symlink or a non-directory anywhere on the way, and
  confirming `S_ISDIR` by `fstat` rather than inferring it;
- pins directory file descriptors once and does every later operation with
  `dir_fd=` (`openat`/`renameat`/`mkdirat` semantics), never a path string;
- opens request files `O_NOFOLLOW`, checks `S_ISREG` and a size cap BEFORE
  reading (a FIFO would block for ever, an oversized file would exhaust), and
  reads exactly once — `parse_request` judges the buffer, never the file;
- writes by `O_CREAT|O_EXCL|O_NOFOLLOW` to a temporary name then `rename`
  within the same pinned directory, which replaces a planted symlink rather
  than following it.

Two outcomes, kept distinct on purpose: `SpoolRefusal` is an attempt on the
host account (a symlinked directory) or a programming error — the caller logs
it OFF the mount and exits non-zero, writing nothing into the spool.
`SpoolMalformed` and `RequestRejected` are recoverable bad input, answered by
quarantine or a `rejected` response. `openat2(RESOLVE_NO_SYMLINKS)` has no
stdlib binding, and helpers are stdlib-only, so the walk is written out.
"""

from __future__ import annotations

import errno
import json
import os
import re
import stat
from dataclasses import dataclass

SPOOL_DIRS = ("tmp", "requests", "processing", "responses", "archive", "quarantine", "diagnostics")
# `tmp/` is the sandbox's staging area; the host never opens it.
HOST_DIRS = frozenset(SPOOL_DIRS) - {"tmp"}
ROOT_COMPONENTS = ("untracked", "vmtest-bridge")

REQUEST_NAME_RE = re.compile(r"^([0-9]{8}T[0-9]{6}Z)-([a-z-]+)-([0-9a-f]{16})\.json$")
ARGUMENT_RE = re.compile(r"^[a-z][a-z0-9_-]*$")
RUN_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
REQUEST_SIZE_CAP = 16 * 1024

# §6.2: five verbs; the denylist is checked before the verb set, fails closed.
VERBS = frozenset({"list-scenarios", "lab-status", "run-scenario", "refresh-base", "abort-run"})
VERBS_WITH_ARGUMENT = frozenset({"run-scenario", "refresh-base"})
DENY_LIST = frozenset({"exec", "shell", "sh", "bash", "run", "eval", "system", "ansible", "ansible-playbook"})

_DIR_FLAGS = os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW
_REFUSE_ERRNOS = frozenset({errno.ELOOP, errno.ENOTDIR, errno.ENOENT, errno.EACCES})


class SpoolRefusal(RuntimeError):
    """A spool directory is not what it must be; nothing is written and the caller exits non-zero."""


class SpoolMalformed(ValueError):
    """A request entry that is not a plain, bounded regular file; quarantine it."""


class RequestRejected(ValueError):
    """A request that fails validation; answer it with a `rejected` response."""

    def __init__(self, code: str, reason: str) -> None:
        super().__init__(f"{code}: {reason}")
        self.code = code
        self.reason = reason


@dataclass(frozen=True)
class Request:
    name: str
    timestamp: str
    verb: str
    argument: str | None
    nonce: str


@dataclass(frozen=True)
class Listing:
    valid: list[str]
    malformed: list[str]


def _confirm_directory(fd: int, what: str) -> int:
    if not stat.S_ISDIR(os.fstat(fd).st_mode):
        os.close(fd)
        raise SpoolRefusal(f"{what} is not a directory")
    return fd


def _open_dir_component(dir_fd: int | None, component: str, what: str) -> int:
    try:
        if dir_fd is None:
            fd = os.open(component, _DIR_FLAGS)
        else:
            fd = os.open(component, _DIR_FLAGS, dir_fd=dir_fd)
    except OSError as exc:
        if exc.errno in _REFUSE_ERRNOS:
            raise SpoolRefusal(f"{what}: refused ({os.strerror(exc.errno)}); a symlink or a non-directory is not a spool") from exc
        raise
    return _confirm_directory(fd, what)


def _safe_name(name: str, what: str) -> str:
    if not name or name in (".", "..") or "/" in name or "\0" in name:
        raise SpoolRefusal(f"{what}: {name!r} is not a plain file name")
    return name


def open_root(checkout: str) -> int:
    """Pin `<checkout>/untracked/vmtest-bridge` by a component-wise O_NOFOLLOW walk."""
    if not os.path.isabs(checkout):
        raise SpoolRefusal(f"checkout must be an absolute path, got {checkout!r}")
    fd = _open_dir_component(None, checkout, checkout)
    try:
        for component in ROOT_COMPONENTS:
            parent = fd
            fd = _open_dir_component(parent, component, f"{checkout}/…/{component}")
            os.close(parent)
    except SpoolRefusal:
        os.close(fd)
        raise
    return fd


def open_subdir(root_fd: int, name: str) -> int:
    """Pin one host-facing spool directory relative to the pinned root."""
    if name not in HOST_DIRS:
        raise SpoolRefusal(f"{name!r} is not a host-facing spool directory")
    return _open_dir_component(root_fd, name, f"spool/{name}")


def list_requests(requests_fd: int) -> Listing:
    valid: list[str] = []
    malformed: list[str] = []
    # An O_PATH fd cannot be read; reopen the SAME directory (".", relative to
    # the pinned fd, so no path is resolved) for the listing.
    listing_fd = os.open(".", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=requests_fd)
    try:
        entries = sorted(os.listdir(listing_fd))
    finally:
        os.close(listing_fd)
    for entry in entries:
        if REQUEST_NAME_RE.match(entry):
            valid.append(entry)
        else:
            malformed.append(entry)
    return Listing(valid=valid, malformed=malformed)


def read_request(requests_fd: int, name: str) -> bytes:
    """Open O_NOFOLLOW, check S_ISREG and the size cap, then read exactly once."""
    _safe_name(name, "request")
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=requests_fd)
    except OSError as exc:
        if exc.errno == errno.ELOOP:
            raise SpoolMalformed(f"{name} is a symlink") from exc
        raise SpoolMalformed(f"{name}: {os.strerror(exc.errno)}") from exc
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode):
            raise SpoolMalformed(f"{name} is not a regular file")
        if info.st_size > REQUEST_SIZE_CAP:
            raise SpoolMalformed(f"{name} is {info.st_size} bytes; the cap is {REQUEST_SIZE_CAP}")
        body = os.read(fd, REQUEST_SIZE_CAP + 1)
        if len(body) > REQUEST_SIZE_CAP:
            raise SpoolMalformed(f"{name} grew past the cap while being read")
        return body
    finally:
        os.close(fd)


def parse_request(name: str, body: bytes) -> Request:
    """Judge the buffer (never the file) in §6.4 order: filename, denylist, verb, argument."""
    match = REQUEST_NAME_RE.match(name)
    if not match:
        raise RequestRejected("bad-filename", f"{name!r} does not match the request name grammar")
    timestamp, filename_verb, filename_nonce = match.groups()
    if filename_verb in DENY_LIST:
        raise RequestRejected("denylisted-verb", f"{filename_verb!r} is on the hardcoded deny list")

    try:
        document = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise RequestRejected("bad-body", f"request body is not JSON: {exc}") from exc
    if not isinstance(document, dict) or set(document) != {"verb", "argument", "nonce"}:
        raise RequestRejected("bad-body", "request body must be an object with exactly verb, argument and nonce")
    body_verb = document["verb"]
    if body_verb in DENY_LIST:
        raise RequestRejected("denylisted-verb", f"{body_verb!r} is on the hardcoded deny list")
    if body_verb not in VERBS:
        raise RequestRejected("unknown-verb", f"{body_verb!r} is not a bridge verb")
    if body_verb != filename_verb:
        raise RequestRejected("verb-mismatch", f"filename says {filename_verb!r}, body says {body_verb!r}")
    if document["nonce"] != filename_nonce:
        raise RequestRejected("nonce-mismatch", "filename nonce and body nonce differ")

    argument = document["argument"]
    if body_verb in VERBS_WITH_ARGUMENT:
        if not isinstance(argument, str) or not ARGUMENT_RE.match(argument):
            raise RequestRejected("bad-argument", f"{body_verb} needs an argument matching {ARGUMENT_RE.pattern}")
    elif argument is not None:
        raise RequestRejected("bad-argument", f"{body_verb} takes no argument")
    return Request(name=name, timestamp=timestamp, verb=body_verb, argument=argument, nonce=filename_nonce)


def claim(requests_fd: int, name: str, processing_fd: int) -> None:
    """Atomically move a request out of requests/ so no second activation can take it."""
    _safe_name(name, "request")
    os.rename(name, name, src_dir_fd=requests_fd, dst_dir_fd=processing_fd)


def quarantine(requests_fd: int, name: str, quarantine_fd: int) -> None:
    _safe_name(name, "request")
    os.rename(name, name, src_dir_fd=requests_fd, dst_dir_fd=quarantine_fd)


def write_file(dir_fd: int, name: str, data: bytes) -> None:
    """Create-exclusive to a temp name in the pinned directory, then rename over `name`.

    rename() replaces whatever `name` is — including a planted symlink — and
    never follows it, so the bytes can only land in this directory.
    """
    _safe_name(name, "file")
    temp = f".{name}.{os.getpid()}.tmp"
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644, dir_fd=dir_fd)
    try:
        view = memoryview(data)
        while view:
            written = os.write(fd, view)
            view = view[written:]
        os.fsync(fd)
    finally:
        os.close(fd)
    os.rename(temp, name, src_dir_fd=dir_fd, dst_dir_fd=dir_fd)


def create_archive_dir(archive_fd: int, run_id: str) -> int:
    """mkdirat a fresh run directory and return its pinned fd; an existing entry is refused."""
    if not RUN_ID_RE.match(run_id):
        raise SpoolRefusal(f"run id {run_id!r} is not a plain name")
    try:
        os.mkdir(run_id, 0o755, dir_fd=archive_fd)
    except FileExistsError as exc:
        raise SpoolRefusal(f"archive/{run_id} already exists; a run id is used once") from exc
    return _open_dir_component(archive_fd, run_id, f"archive/{run_id}")
