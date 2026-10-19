"""The LXC member record's file (`members.py`, `<applied>/lxc/<team>.json`), which root writes
beside the applied stamps for ccy-teams to read without sudo.

It is kept as the stamps and the health check's record (`helpers/agent_bus/health.py`) are: the
directory 0750 and each file 0640, root's and the stamps' group's, so the desktop user reads it
and no other user learns a team's name. It holds no secret. health.py keeps its own writer:
helpers/agent_bus is baked into the ccy image, and sharing this one would cost an image bump.
"""

from __future__ import annotations

import os
import pathlib

DIR_MODE = 0o750
FILE_MODE = 0o640


class RecordError(Exception):
    """The record's directory cannot be used; the message says why."""


def read(path: pathlib.Path) -> str | None:
    """The record's text, or None when there is none."""
    try:
        return path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return None


def write(directory: pathlib.Path, name: str, text: str | None, gid: int) -> bool:
    """`directory`/`name` made to hold `text` (replaced atomically), or removed when `text` is
    None. Returns whether anything changed. The directory's parent, the applied stamps'
    directory, must already be there: `agent-bus-install team` makes it."""
    path = directory / name
    if text is None:
        if not os.path.lexists(path):
            return False
        path.unlink()
        return True
    if not directory.parent.is_dir():
        raise RecordError(f"{directory.parent} is missing: agent-bus-install team makes it (ccy-teams apply)")
    directory.mkdir(mode=DIR_MODE, exist_ok=True)
    if directory.is_symlink() or not directory.is_dir():
        raise RecordError(f"{directory} is not a directory")
    os.chown(directory, -1, gid)
    os.chmod(directory, DIR_MODE)
    if read(path) == text and path.stat().st_gid == gid and (path.stat().st_mode & 0o7777) == FILE_MODE:
        return False
    tmp = directory / f".{name}.new"
    tmp.unlink(missing_ok=True)
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, FILE_MODE)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        os.fchown(handle.fileno(), -1, gid)
        os.fchmod(handle.fileno(), FILE_MODE)
        handle.write(text)
    os.replace(tmp, path)
    return True
