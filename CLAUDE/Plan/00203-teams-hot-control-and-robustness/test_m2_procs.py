"""Tests for m2_procs.py, the bus-process probe behind M1 (b) and M2 (c) (e).

Run from anywhere:
    python3 CLAUDE/Plan/00203-teams-hot-control-and-robustness/test_m2_procs.py
"""

from __future__ import annotations

import importlib.util
import os
import pathlib
import sys
import tempfile
import unittest

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("m2_procs", _HERE / "m2_procs.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'm2_procs.py'}")
mp = importlib.util.module_from_spec(_SPEC)
sys.modules["m2_procs"] = mp
_SPEC.loader.exec_module(mp)

KIT = "/opt/claude-yolo/optional/agent-bus"
#: The plain session's process list on the host at F44 51dce29f (Plan 00203 M2 (c)): claude
#: names the plugin directory .../plugin/pingbus in its argv, and the keeper is the only bus
#: process. The old filter, any argv containing "pingbus", counted claude as a second one.
CLAUDE = ["claude", "--plugin-dir", f"{KIT}/plugin/pingbus", "--settings", "/tmp/pingbus-home/settings.json",
          "--dangerously-skip-permissions", "-p", "Plan 00203 M2 acceptance: an idle session; nothing to do.",
          "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--model", "haiku"]
KEEPER = ["/usr/bin/python3", f"{KIT}/pingbus", "seat", "keep", "--launch", "--parent", "2",
          "--root", "/workspace/.claude/ccy/pingbus/seats", "--home", "/tmp/pingbus-home"]


class ProcTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self._tmp.name)
        self.proc = self.root / "proc"
        self.proc.mkdir()
        self.locks = self.root / "locks"
        self.locks.mkdir()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def fresh(self) -> None:
        """An empty /proc again, within one test."""
        self.tearDown()
        self.setUp()

    def process(self, pid: str, argv: list[str], *locks: str) -> None:
        base = self.proc / pid
        (base / "fd").mkdir(parents=True)
        (base / "cmdline").write_bytes(b"\0".join(a.encode() for a in argv) + b"\0")
        (base / "fd" / "0").symlink_to("/dev/null")
        for n, lock in enumerate(locks, start=3):
            target = self.locks / pid / lock
            target.parent.mkdir(exist_ok=True)
            target.touch()
            (base / "fd" / str(n)).symlink_to(target)

    def host_list(self, *, keeper_locks: tuple[str, ...] = ("keeper.lock",)) -> None:
        """The process list the host saw, plus the container's first process."""
        self.process("1", ["catatonit", "--", "/entrypoint.sh", "claude"])
        self.process("2", CLAUDE)
        self.process("40", KEEPER, *keeper_locks)

    def test_the_host_list_has_one_bus_process_the_keeper(self) -> None:
        self.host_list()
        found = mp.bus_processes(mp.processes(self.proc))
        self.assertEqual([p.pid for p in found], [40])
        self.assertTrue(mp.is_launch_keeper(found[0].argv))

    def test_m2_c_passes_on_the_host_list(self) -> None:
        self.host_list()
        lines = mp.at_most_keeper(mp.processes(self.proc))
        self.assertEqual(len(lines), 1, lines)
        self.assertIn("seat keep", lines[0])

    def test_m1_b_passes_on_the_host_list(self) -> None:
        self.host_list()
        mp.keeper_only(mp.processes(self.proc))

    def test_claude_naming_the_plugin_is_not_a_bus_process(self) -> None:
        for argv in (CLAUDE, ["claude", "--plugin-dir", f"{KIT}/plugin/pingbus", "seat", "keep"],
                     ["/usr/local/bin/claude", "-p", "run pingbus watch", "--plugin-dir", f"{KIT}/plugin/pingbus"]):
            with self.subTest(argv=argv):
                self.assertIsNone(mp.program(argv))

    def test_bus_programs_by_their_entry_point(self) -> None:
        for argv, want in ((KEEPER, "pingbus"),
                           (["python3", "/usr/local/bin/pingbus", "watch", "--child"], "pingbus"),
                           (["python3", "-I", "/usr/local/bin/agent-bus", "seat", "take"], "agent-bus"),
                           (["/usr/local/bin/pingbus", "wait"], "pingbus"),
                           (["python3", "/workspace/CLAUDE/Plan/x/m2_procs.py", "bus"], None),
                           (["python3"], None),
                           (["sh", "-c", "pingbus watch"], None)):
            with self.subTest(argv=argv):
                self.assertEqual(mp.program(argv), want)

    def test_m2_c_passes_with_no_bus_process(self) -> None:
        self.process("2", CLAUDE)
        self.assertEqual(mp.at_most_keeper(mp.processes(self.proc)), [])

    def test_m2_c_fails_on_a_second_bus_process_or_a_non_keeper(self) -> None:
        self.host_list()
        self.process("50", ["python3", "/usr/local/bin/pingbus", "watch", "--supervise"])
        with self.assertRaises(mp.Failed):
            mp.at_most_keeper(mp.processes(self.proc))
        self.fresh()
        self.process("50", ["python3", "/usr/local/bin/pingbus", "watch"])
        with self.assertRaises(mp.Failed):
            mp.at_most_keeper(mp.processes(self.proc))

    def test_m1_b_fails_without_a_keeper_or_its_lock(self) -> None:
        self.process("2", CLAUDE)
        with self.assertRaises(mp.Failed):
            mp.keeper_only(mp.processes(self.proc))
        self.fresh()
        self.host_list(keeper_locks=())
        with self.assertRaises(mp.Failed):
            mp.keeper_only(mp.processes(self.proc))

    def test_m1_b_fails_when_another_process_holds_a_lock(self) -> None:
        self.host_list()
        self.process("60", ["sleep", "1000"], "seat.lock")
        with self.assertRaises(mp.Failed):
            mp.keeper_only(mp.processes(self.proc))

    def test_m1_b_the_keeper_may_hold_seat_locks(self) -> None:
        self.host_list(keeper_locks=("keeper.lock", "seat.lock"))
        mp.keeper_only(mp.processes(self.proc))

    def test_m2_e_none(self) -> None:
        self.process("1", ["catatonit", "--", "/entrypoint.sh", "claude"])
        self.process("2", ["claude", "--dangerously-skip-permissions", "-p", "about pingbus"])
        mp.no_bus(mp.processes(self.proc))
        self.process("40", KEEPER)
        with self.assertRaises(mp.Failed):
            mp.no_bus(mp.processes(self.proc))

    def test_a_vanished_process_is_skipped(self) -> None:
        self.host_list()
        (self.proc / "77").mkdir()
        self.assertEqual([p.pid for p in mp.processes(self.proc)], [1, 2, 40])

    def test_main(self) -> None:
        self.host_list()
        self.assertEqual(mp.main(["keeper-only", str(self.proc)]), 0)
        self.assertEqual(mp.main(["at-most-keeper", str(self.proc)]), 0)
        self.assertEqual(mp.main(["none", str(self.proc)]), 1)
        self.assertEqual(mp.main(["nope"]), 64)
        self.assertEqual(mp.main(["none"]), 64)


if __name__ == "__main__":
    os.umask(0o077)
    unittest.main()
