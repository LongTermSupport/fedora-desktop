"""Decide whether `dconf update` actually needs to run (Plan 00132).

RUNNING THE COMPILE IS NOT FREE, AND NOT LOCAL
----------------------------------------------
`dconf update` recompiles EVERY database under /etc/dconf/db, not only the one
whose drop-in directory changed. Rewriting those files makes dconf notify every
live session, and a logged-in user's profile stacks `local` and `site` -- so a
change confined to `gdm.d` still delivers a settings-changed signal into that
user's GNOME Shell.

That is how this module came to exist. A play edited a gdm drop-in, ran the
compile, and the running GNOME Shell segfaulted handling the resulting
notification, inside libgnome-desktop's clock handler:

    #0  update_clock ()                  libgnome-desktop-4.so.2
    #5  g_settings_real_change_event ()  libgio-2.0.so.0
    #10 settings_backend_path_changed () libgio-2.0.so.0

The user lost their whole desktop -- every Wayland client dies with the
compositor. The crash itself is upstream and not ours to fix; how OFTEN we hand
it the opportunity is entirely ours.

WHY NOT JUST GATE ON THE COPY TASK
----------------------------------
`when: <copy task> is changed` looks equivalent and is not. It ties the compile
to whether ANSIBLE wrote the file, so a binary database that goes stale by any
other route -- deleted, truncated, restored from a backup, written by a package
-- is never rebuilt, and no subsequent run can repair it. The play would have no
route back to a correct state.

Comparing mtimes asks the question that actually matters: is the compiled
database older than the drop-ins it was compiled from? That self-heals, and it
stays silent on the routine run where nothing changed.

The module is PURE filesystem inspection: it decides, and the caller acts.
"""

from __future__ import annotations

import sys
from pathlib import Path

DROP_IN_SUFFIX = ".d"
DEFAULT_ROOT = Path("/etc/dconf/db")

_MARKER = "DCONF-STALE:"
_NONE = "none"


def _newest_mtime(directory: Path) -> float | None:
    """Newest mtime among the files under `directory`, or None if it has none.

    Directories are deliberately excluded: a directory's own mtime changes when a
    file inside it is created or removed, which is already reflected by that
    file's mtime -- and on a directory that has since been emptied it would
    report a change with nothing left to compile.
    """
    newest: float | None = None
    for path in directory.rglob("*"):
        if not path.is_file():
            continue
        mtime = path.stat().st_mtime
        if newest is None or mtime > newest:
            newest = mtime
    return newest


def stale_databases(root: Path = DEFAULT_ROOT) -> list[str]:
    """Names of the databases under `root` whose compiled form is out of date.

    Sorted, so a report reads the same twice and a diff of two runs means
    something. Empty means: running the compile would change nothing and notify
    every session for no reason.
    """
    if not root.is_dir():
        return []

    stale: list[str] = []
    for drop_in in root.iterdir():
        if not drop_in.is_dir() or not drop_in.name.endswith(DROP_IN_SUFFIX):
            continue

        newest = _newest_mtime(drop_in)
        if newest is None:
            # An empty drop-in directory compiles to nothing, so there is no
            # state for the database to be behind.
            continue

        name = drop_in.name[: -len(DROP_IN_SUFFIX)]
        compiled = root / name
        if not compiled.exists():
            stale.append(name)
            continue

        # Strictly greater: `dconf update` frequently finishes within the same
        # second it read the drop-in, and treating equality as stale would make
        # this gate fire on every run for ever -- restoring exactly the
        # compile-every-time behaviour it exists to prevent.
        if newest > compiled.stat().st_mtime:
            stale.append(name)

    return sorted(stale)


def render(stale: list[str]) -> str:
    """One parseable line for the playbook to read.

    A clean host says `none` rather than printing nothing: an empty string is
    indistinguishable from a probe that produced no output because it never ran,
    and the play would read that silence as "nothing to do".
    """
    return f"{_MARKER} {' '.join(stale) if stale else _NONE}"


def main(argv: list[str] | None = None) -> int:
    """Print the verdict on stdout. The exit code reports the PROBE, not the verdict.

    A non-zero exit for "stale" would be indistinguishable from the probe itself
    failing, and the play would have to carry `failed_when: false` to read it --
    trading a clear signal for an annotation this project bans.
    """
    argv = sys.argv[1:] if argv is None else argv
    root = Path(argv[0]) if argv else DEFAULT_ROOT
    print(render(stale_databases(root)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
