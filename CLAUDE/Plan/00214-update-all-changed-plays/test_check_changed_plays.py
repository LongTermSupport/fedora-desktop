"""Tests for check-changed-plays.bash `report` (Plan 00214), over a stand-in checkout.

The checker runs this checkout's helpers by module path (PYTHONPATH=<repo root>), so a
temporary root holding small stand-ins for the four modules it calls is the checker's whole
world: run.bash --changed's helper (`changed_plays`, whose RUN lines say a play is to run), the
collection (`login_report`, exiting with the status each case sets, and defining
EXIT_FINDINGS as the real one does), `login_message`'s words and command, and
fedora-desktop-health under a temporary HOME.

The case it exists for: login_report exits EXIT_FINDINGS (3) when it has findings, and a play
to run is one, so with a play to run 3 is the expected result and 0 is wrong; with none,
either is a result. Any other status is the collection failing.

Run from anywhere:
    python3 CLAUDE/Plan/00214-update-all-changed-plays/test_check_changed_plays.py
"""

from __future__ import annotations

import os
import pathlib
import subprocess
import tempfile
import textwrap
import unittest

CHECKER = pathlib.Path(__file__).resolve().parent / "check-changed-plays.bash"

STUBS = {
    "helpers/__init__.py": "",
    "helpers/play_ledger/__init__.py": "",
    "helpers/play_ledger/freshness.py": 'STALE = "stale"\n',
    "helpers/play_ledger/changed_plays.py": """\
        import os, pathlib
        print(pathlib.Path(os.environ["FAKE"], "changed.out").read_text(), end="")
        """,
    "helpers/host_health/__init__.py": "",
    "helpers/host_health/login_report.py": """\
        import os, pathlib, sys
        EXIT_FINDINGS = 3
        if __name__ == "__main__":
            sys.exit(int(pathlib.Path(os.environ["FAKE"], "report.rc").read_text()))
        """,
    "helpers/host_health/login_message.py": """\
        _RUNS_CHANGED = "the changed plays"
        def run_changed_command(_document, checkout):
            return f"{checkout}/run.bash --changed"
        """,
}


class ReportCheckTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self._tmp.name)
        self.root = self.dir / "root"
        self.fake = self.dir / "fake"
        self.run_dir = self.dir / "run"
        self.home = self.dir / "home"
        for directory in (self.fake, self.run_dir, self.home / ".local/bin"):
            directory.mkdir(parents=True)
        for name, text in STUBS.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(textwrap.dedent(text), encoding="utf-8")
        health = self.home / ".local/bin/fedora-desktop-health"
        health.write_text(f'#!/bin/bash\ncat -- "{self.fake}/health.out"\n', encoding="utf-8")
        health.chmod(0o755)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def report(self, rc: int, *, plays: bool) -> tuple[int, str]:
        """The `report` check, with a play to run or none, after a collection exiting rc."""
        (self.fake / "report.rc").write_text(f"{rc}\n", encoding="utf-8")
        (self.fake / "changed.out").write_text("RUN play-a.yml\n" if plays else "", encoding="utf-8")
        printed = f"  To run the changed plays: {self.root}/run.bash --changed\n" if plays else "all current\n"
        (self.fake / "health.out").write_text(printed, encoding="utf-8")
        env = {**os.environ, "HOME": str(self.home), "FAKE": str(self.fake)}
        done = subprocess.run(["bash", str(CHECKER), "report", str(self.root), str(self.run_dir)],
                              capture_output=True, text=True, env=env, timeout=60, check=False)
        return done.returncode, done.stdout + done.stderr

    def not_judged(self) -> bool:
        return (self.run_dir / "not-judged").exists()

    def test_findings_with_a_play_to_run_is_the_expected_result(self) -> None:
        status, out = self.report(3, plays=True)
        self.assertEqual(status, 0, out)
        self.assertFalse(self.not_judged())

    def test_a_clean_collection_with_a_play_to_run_fails(self) -> None:
        status, out = self.report(0, plays=True)
        self.assertEqual(status, 1, out)
        self.assertIn("exited 0", out)

    def test_with_no_play_to_run_clean_or_findings_is_not_judged(self) -> None:
        for rc in (0, 3):
            with self.subTest(rc=rc):
                (self.run_dir / "not-judged").unlink(missing_ok=True)
                status, out = self.report(rc, plays=False)
                self.assertEqual(status, 0, out)
                self.assertTrue(self.not_judged())

    def test_any_other_status_is_the_collection_failing(self) -> None:
        for rc in (1, 2, 4):
            for plays in (True, False):
                with self.subTest(rc=rc, plays=plays):
                    status, out = self.report(rc, plays=plays)
                    self.assertEqual(status, 1, out)
                    self.assertIn(f"the collection exited {rc}", out)


if __name__ == "__main__":
    unittest.main()
