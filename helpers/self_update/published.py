"""The self-update result the user side may read (Plan 00137 Task 4.3).

The cycle's state directory is root-only 0700: `deployed` and `owed-verify` decide what
the next root run does, so nothing outside root may read them as inputs or see them
change. The host-health report runs as the user and has to know how the last cycle
went. This is the one copy between the two, written by the cycle and never read back
by it.

It lives in its own directory, `root:<user's group>` mode 2750, which
`play-self-update.yml` creates when self-update is enabled and removes when it is not.
The setgid bit gives each file the user's group without a chown, and the file is 0640:
the user can read it and nobody but root can change it. So the directory's existence is
also how the report knows self-update is enabled here.

The contract is CLAUDE/Plan/00137-unattended-server-self-update/DESIGN-cycle.md.
"""

from __future__ import annotations

import os
import tempfile

#: Where the play creates the directory. A constant, because the reader (the user's
#: report) and the writer (root's cycle) must agree on it without sharing a config.
DIRECTORY = "/var/lib/fedora-desktop/self-update-status"
FILE_NAME = "result"

#: The cycle's result keys, then the boot a post-boot check is owed by ("" for none).
#: `alert` names each sink that did not accept this result's alert ("" when all did).
KEYS = ("at", "phase", "outcome", "old", "new", "plays", "detail", "alert", "owed_boot")
#: Keys a record written by an older cycle lacks, and what they mean when absent. A cycle
#: from before alerts existed sent nothing, so nothing it sent can have failed.
_ABSENT_MEANS = {"alert": ""}

#: A cycle that finished with nothing wrong.
OK_OUTCOMES = frozenset({"nothing", "deployed"})
#: Written before the countdown; the reboot, then the post-boot check, follow it.
IN_PROGRESS_OUTCOMES = frozenset({"rebooting"})
#: A cycle that stopped short. Each is alerted by the cycle and reported by the reader.
FAILED_OUTCOMES = frozenset({
    "refused", "config-invalid", "play-failed", "unwarnable", "cancelled",
    "reboot-failed", "verify-failed",
})

_MODE = 0o640


def path(directory: str) -> str:
    return os.path.join(directory, FILE_NAME)


def write(directory: str, record: dict[str, str], *, owed_boot: str) -> None:
    """Replace the published record atomically, so a reader never sees half of one."""
    values = {**{key: record.get(key, "") for key in KEYS[:-1]}, "owed_boot": owed_boot}
    for key, value in values.items():
        if "\n" in value:
            raise ValueError(f"the published {key} cannot be written as one line")
    text = "".join(f"{key}={value}\n" for key, value in values.items())
    descriptor, temporary = tempfile.mkstemp(dir=directory, prefix=".result-", suffix=".tmp")
    try:
        os.fchmod(descriptor, _MODE)
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(text)
        os.replace(temporary, path(directory))
    except BaseException:
        if os.path.exists(temporary):
            os.unlink(temporary)
        raise


def read(directory: str) -> dict[str, str] | None:
    """The published record, None when there is none. Raises ValueError for a malformed
    one and OSError for one that cannot be opened: the reader reports both."""
    try:
        with open(path(directory), encoding="utf-8") as handle:
            text = handle.read()
    except FileNotFoundError:
        return None
    values: dict[str, str] = {}
    for line in text.splitlines():
        key, sep, value = line.partition("=")
        if not sep or key not in KEYS:
            raise ValueError("it holds a line that is not one of its keys as key=value")
        values[key] = value
    for key, meaning in _ABSENT_MEANS.items():
        values.setdefault(key, meaning)
    missing = [key for key in KEYS if key not in values]
    if missing:
        raise ValueError(f"it has no {', '.join(missing)}")
    return values
