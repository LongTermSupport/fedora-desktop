"""The CPU sampler must sleep ONCE per scan, not once per process.

This is not a performance nicety. The sampler slept `interval_s` for every
candidate PID in turn, so a scan configured to run every 2 minutes actually took
about 11.8 minutes on a real host (measured from the journal: 12:13 -> 12:25 ->
12:36 -> 12:48 -> 13:00 -> 13:12).

That silently disabled automatic crash-loop containment. Containment differences
restart counts across a rolling 600-second window and needs two samples inside
it; with ticks ~708 seconds apart the previous sample was always trimmed, every
container held exactly one sample, and the rate was therefore never measurable.
The teeth were present, tested, deployed — and structurally unable to fire.

So the shared-sleep property below is a CORRECTNESS test for containment, and
the assertion that matters is the count of sleeps, not the wall-clock time.
"""

import os
import unittest
import unittest.mock
from pathlib import Path
from tempfile import TemporaryDirectory

from helpers.containerwatch import cli

# A /proc/<pid>/stat line: field 14 (utime) and 15 (stime) carry CPU ticks, 22 is
# starttime. Only the fields the delta uses need to be plausible.
def _stat(pid: int, *, utime: int, stime: int, starttime: int = 1000) -> str:
    fields = ["0"] * 52
    fields[0] = str(pid)
    fields[1] = "(proc)"
    fields[2] = "S"
    fields[13] = str(utime)
    fields[14] = str(stime)
    fields[21] = str(starttime)
    return " ".join(fields)


class SharedSleepTests(unittest.TestCase):
    def _proc_root(self, tmp: str, pids: list[int]) -> str:
        for pid in pids:
            d = Path(tmp) / str(pid)
            d.mkdir(parents=True, exist_ok=True)
            (d / "stat").write_text(_stat(pid, utime=0, stime=0))
        return tmp

    def test_sampling_many_pids_sleeps_exactly_once(self):
        with TemporaryDirectory() as tmp:
            root = self._proc_root(tmp, [101, 102, 103, 104, 105])
            sleeps = []
            with unittest.mock.patch.object(cli.time, "sleep", lambda s: sleeps.append(s)):
                sampler = cli.make_cpu_sampler(root, 1.0, 100)
                for pid in (101, 102, 103, 104, 105):
                    sampler(pid)
            self.assertEqual(
                len(sleeps), 1,
                f"one sleep per scan, not per pid — got {len(sleeps)}",
            )

    def test_the_single_sleep_is_the_configured_interval(self):
        with TemporaryDirectory() as tmp:
            root = self._proc_root(tmp, [201])
            sleeps = []
            with unittest.mock.patch.object(cli.time, "sleep", lambda s: sleeps.append(s)):
                cli.make_cpu_sampler(root, 2.5, 100)(201)
            self.assertEqual(sleeps, [2.5])

    def test_a_vanished_pid_yields_none_rather_than_raising(self):
        with TemporaryDirectory() as tmp:
            root = self._proc_root(tmp, [301])
            with unittest.mock.patch.object(cli.time, "sleep", lambda _s: None):
                sampler = cli.make_cpu_sampler(root, 1.0, 100)
                self.assertIsNone(sampler(999999))

    def test_a_pid_that_disappears_mid_scan_yields_none(self):
        with TemporaryDirectory() as tmp:
            root = self._proc_root(tmp, [401, 402])

            def remove_one(_s):
                # 402 exits during the sampling interval.
                os.remove(Path(root) / "402" / "stat")
                os.rmdir(Path(root) / "402")

            with unittest.mock.patch.object(cli.time, "sleep", remove_one):
                sampler = cli.make_cpu_sampler(root, 1.0, 100)
                self.assertIsNone(sampler(402))
                self.assertIsNotNone(sampler(401))

    def test_cpu_use_is_measured_not_merely_returned_as_zero(self):
        # A sampler that always returns 0 would satisfy every test above, and
        # would silently disable the CPU half of the watchdog.
        with TemporaryDirectory() as tmp:
            root = self._proc_root(tmp, [501])

            def burn(_s):
                (Path(root) / "501" / "stat").write_text(
                    _stat(501, utime=50, stime=50)
                )

            with unittest.mock.patch.object(cli.time, "sleep", burn):
                # 100 ticks over 1s at 100 ticks/s == 100% of one core.
                pct = cli.make_cpu_sampler(root, 1.0, 100)(501)
            self.assertAlmostEqual(pct, 100.0, places=1)

    def test_every_pid_is_measured_over_the_same_interval(self):
        # Shared snapshots mean the figures are comparable between processes,
        # which per-pid sleeps never guaranteed.
        with TemporaryDirectory() as tmp:
            root = self._proc_root(tmp, [601, 602])

            def burn(_s):
                (Path(root) / "601" / "stat").write_text(_stat(601, utime=25, stime=25))
                (Path(root) / "602" / "stat").write_text(_stat(602, utime=25, stime=25))

            with unittest.mock.patch.object(cli.time, "sleep", burn):
                sampler = cli.make_cpu_sampler(root, 1.0, 100)
                self.assertAlmostEqual(sampler(601), sampler(602), places=6)


if __name__ == "__main__":
    unittest.main()
