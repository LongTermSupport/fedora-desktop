"""Tests for helpers/dconf/staleness.py (Plan 00132).

The module decides whether `dconf update` needs to run at all. That decision is
load-bearing for desktop STABILITY, not merely for tidiness: running the compile
rewrites every database under /etc/dconf/db -- including `local` and `site`,
which a logged-in user's profile stacks -- and each rewrite broadcasts a
settings-changed notification into every live session. One such broadcast
segfaulted a running GNOME Shell inside libgnome-desktop's clock handler and
took the desktop down with it.

So a needless compile is not a no-op. These tests pin the two directions:
a genuinely stale database MUST be found (or the play cannot self-heal), and an
up-to-date one MUST NOT be recompiled (or every routine run rolls the dice).
"""

import os
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from helpers.dconf import staleness


def _touch(path: Path, *, mtime: float, content: str = "x") -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)
    os.utime(path, (mtime, mtime))


def _mkdir(path: Path, *, mtime: float) -> None:
    path.mkdir(parents=True, exist_ok=True)
    os.utime(path, (mtime, mtime))


class StaleDatabasesFound(unittest.TestCase):
    """The self-heal direction: anything out of date must be reported."""

    def test_a_missing_database_is_stale(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "10-power", mtime=1000)
            self.assertEqual(staleness.stale_databases(root), ["gdm"])

    def test_a_database_older_than_its_drop_in_is_stale(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "10-power", mtime=2000)
            _touch(root / "gdm", mtime=1000)
            self.assertEqual(staleness.stale_databases(root), ["gdm"])

    def test_the_newest_drop_in_decides_not_the_first_one_seen(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "00-old", mtime=1000)
            _touch(root / "gdm.d" / "10-power", mtime=3000)
            _touch(root / "gdm", mtime=2000)
            self.assertEqual(staleness.stale_databases(root), ["gdm"])

    def test_every_stale_database_is_reported_not_just_the_first(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "10-power", mtime=2000)
            _touch(root / "local.d" / "00-site", mtime=2000)
            _touch(root / "gdm", mtime=1000)
            _touch(root / "local", mtime=1000)
            self.assertEqual(staleness.stale_databases(root), ["gdm", "local"])

    def test_the_result_is_sorted_so_the_report_is_stable(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in ("site", "gdm", "local"):
                _touch(root / f"{name}.d" / "10-x", mtime=2000)
            self.assertEqual(staleness.stale_databases(root), ["gdm", "local", "site"])


class UpToDateDatabasesLeftAlone(unittest.TestCase):
    """The blast-radius direction: a needless compile must not be requested."""

    def test_a_database_newer_than_its_drop_ins_is_not_stale(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "10-power", mtime=1000)
            _touch(root / "gdm", mtime=2000)
            self.assertEqual(staleness.stale_databases(root), [])

    def test_equal_mtimes_are_not_stale(self):
        # dconf update can finish inside the same second it read the drop-in, so
        # treating equality as stale would make the gate fire for ever on a
        # database that is in fact current -- the compile-every-run behaviour
        # this module exists to stop.
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "10-power", mtime=1000)
            _touch(root / "gdm", mtime=1000)
            self.assertEqual(staleness.stale_databases(root), [])

    def test_an_empty_drop_in_directory_with_a_database_is_not_stale(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _mkdir(root / "local.d", mtime=2000)
            _touch(root / "local", mtime=1000)
            self.assertEqual(staleness.stale_databases(root), [])

    def test_an_empty_drop_in_directory_with_no_database_is_not_stale(self):
        # Nothing to compile means nothing to broadcast. Reporting this as stale
        # would request a compile on a host that has never needed one.
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _mkdir(root / "local.d", mtime=1000)
            self.assertEqual(staleness.stale_databases(root), [])

    def test_a_database_with_no_drop_in_directory_is_not_stale(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "distro", mtime=1000)
            self.assertEqual(staleness.stale_databases(root), [])

    def test_a_missing_root_is_not_stale(self):
        with TemporaryDirectory() as tmp:
            self.assertEqual(staleness.stale_databases(Path(tmp) / "absent"), [])


class DropInDirectoriesReadRecursively(unittest.TestCase):
    def test_a_nested_file_counts_towards_the_newest_mtime(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "sub" / "20-more", mtime=3000)
            _touch(root / "gdm.d" / "10-power", mtime=1000)
            _touch(root / "gdm", mtime=2000)
            self.assertEqual(staleness.stale_databases(root), ["gdm"])

    def test_a_directory_named_without_the_suffix_is_ignored(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "notadropin" / "10-x", mtime=3000)
            self.assertEqual(staleness.stale_databases(root), [])


class RenderReportsForAnsible(unittest.TestCase):
    """The play reads stdout; the exit code must not encode the verdict.

    A non-zero exit for "stale" would make the probe indistinguishable from the
    probe itself failing, and under this project's fail-fast rules the play would
    have to suppress the failure to read it -- trading a clear signal for a
    banned annotation.
    """

    def test_stdout_names_the_stale_databases(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            _touch(root / "gdm.d" / "10-power", mtime=2000)
            _touch(root / "gdm", mtime=1000)
            out = staleness.render(staleness.stale_databases(root))
            self.assertEqual(out, "DCONF-STALE: gdm")

    def test_a_clean_host_says_so_explicitly(self):
        # Not an empty string: silence is indistinguishable from a probe that
        # produced no output because it never ran.
        self.assertEqual(staleness.render([]), "DCONF-STALE: none")

    def test_several_stale_databases_are_all_named(self):
        self.assertEqual(staleness.render(["gdm", "local"]), "DCONF-STALE: gdm local")


if __name__ == "__main__":
    unittest.main()
