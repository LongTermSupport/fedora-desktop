"""Unit tests for helpers/agent_bus/timer_state.py: the timer state of teams no longer here.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_timer_state

Plan 00203. `agent-bus-backup@<team>.timer` is Persistent=true, so systemd keeps its last run in
/var/lib/systemd/timers/stamp-<unit>. A team removed before agent-bus-install cleared that
stamp left it behind, and a new team of the same name inherited a backup run it never made.
The play clears the stamp of every agent-bus timer whose team has no applied stamp.
"""

from __future__ import annotations

import io
import pathlib
import sys
import tempfile
import unittest
from collections.abc import Sequence

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import timer_state


class TimerStateTest(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = pathlib.Path(tmp.name)
        self.stamps = root / "timers"
        self.applied = root / "applied"
        self.stamps.mkdir()
        self.applied.mkdir()
        self.cleaned: list[str] = []

    def stamp(self, unit: str) -> None:
        (self.stamps / f"stamp-{unit}").write_text("", encoding="utf-8")

    def clean(self, unit: str) -> None:
        self.cleaned.append(unit)
        (self.stamps / f"stamp-{unit}").unlink()

    def run_main(self, argv: Sequence[str] = ()) -> str:
        out = io.StringIO()
        timer_state.main(["--stamp-dir", str(self.stamps), "--applied-dir", str(self.applied), *argv],
                         clean=self.clean, out=out)
        return out.getvalue()

    def test_the_stamp_of_a_team_no_longer_here_is_cleared(self):
        self.stamp("agent-bus-backup@gone.timer")
        out = self.run_main()
        self.assertEqual(self.cleaned, ["agent-bus-backup@gone.timer"])
        self.assertEqual(out, "CHANGED\tcleared the last-run time of agent-bus-backup@gone.timer (team gone is not here)\n")

    def test_an_installed_team_and_other_timers_are_left_alone(self):
        self.stamp("agent-bus-backup@alpha.timer")
        (self.applied / "alpha.json").write_text("{}\n", encoding="utf-8")
        self.stamp("fedora-desktop-self-update.timer")
        self.stamp("abrt-prune-stale.timer")
        self.assertEqual(self.run_main(), "")
        self.assertEqual(self.cleaned, [])

    def test_a_second_run_changes_nothing(self):
        self.stamp("agent-bus-backup@gone.timer")
        self.run_main()
        self.assertEqual(self.run_main(), "")
        self.assertEqual(self.cleaned, ["agent-bus-backup@gone.timer"])

    def test_a_host_with_no_timer_state_has_nothing_to_do(self):
        self.stamps.rmdir()
        self.assertEqual(self.run_main(), "")

    def test_a_name_that_is_no_team_name_is_left_alone(self):
        self.stamp("agent-bus-backup@Not_A_Team.timer")
        self.assertEqual(self.run_main(), "")

    def test_a_failed_clean_stops_the_run(self):
        self.stamp("agent-bus-backup@gone.timer")

        def refuse(unit: str) -> None:
            raise timer_state.TimerStateError(f"systemctl clean --what=state {unit} failed (exit 1): busy")

        with self.assertRaisesRegex(timer_state.TimerStateError, "busy"):
            timer_state.main(["--stamp-dir", str(self.stamps), "--applied-dir", str(self.applied)],
                             clean=refuse, out=io.StringIO())


if __name__ == "__main__":
    unittest.main()
