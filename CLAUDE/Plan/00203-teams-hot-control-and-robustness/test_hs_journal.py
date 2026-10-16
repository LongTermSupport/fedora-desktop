"""Tests for hs_journal.py, the homeserver stop analysis behind triage-hs.bash.

Run from anywhere:
    python3 CLAUDE/Plan/00203-teams-hot-control-and-robustness/test_hs_journal.py
"""

from __future__ import annotations

import importlib.util
import json
import os
import pathlib
import sys
import tempfile
import time
import unittest

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("hs_journal", _HERE / "hs_journal.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'hs_journal.py'}")
hj = importlib.util.module_from_spec(_SPEC)
sys.modules["hs_journal"] = hj
_SPEC.loader.exec_module(hj)

HS = "agent-bus-hs@team-a.service"
HEALTH = "agent-bus-hs-health@team-a.service"
BACKUP = "agent-bus-backup@team-a.service"
T0 = 1_760_000_000.0


def pid1(at: float, unit: str, msg: str, boot: str = "b2") -> hj.Entry:
    """A line systemd (PID 1) logged about <unit>."""
    return hj.Entry(ts=T0 + at, boot=boot, unit=unit, origin="systemd", msg=msg)


def own(
    at: float, unit: str, msg: str, origin: str = "tuwunel", boot: str = "b2"
) -> hj.Entry:
    """A line <unit>'s own process logged."""
    return hj.Entry(ts=T0 + at, boot=boot, unit=unit, origin=origin, msg=msg)


def other(at: float, origin: str, msg: str, boot: str = "b2") -> hj.Entry:
    """A line from outside the unit (sudo, ansible, the kernel)."""
    return hj.Entry(ts=T0 + at, boot=boot, unit="", origin=origin, msg=msg)


BOOTS = [
    hj.Boot("b1", T0 - 10_000, T0 - 5_000),
    hj.Boot("b2", T0 - 4_000, T0 + 100_000),
]


def analyse(
    entries: list[hj.Entry], windows: list[hj.Window] | None = None
) -> list[hj.Stop]:
    return hj.stops_of(HS, sorted(entries, key=lambda e: e.ts), BOOTS, windows or [])


class Unrequested(unittest.TestCase):
    def test_watchdog_abort_is_watchdog(self) -> None:
        stops = analyse(
            [
                pid1(
                    0,
                    HS,
                    "Started agent-bus-hs@team-a.service - Agent team bus homeserver for team team-a.",
                ),
                own(50, HS, "WARN something slow"),
                pid1(
                    100,
                    HS,
                    "agent-bus-hs@team-a.service: Watchdog timeout (limit 30s)!",
                ),
                pid1(
                    100,
                    HS,
                    "agent-bus-hs@team-a.service: Killing process 1 (tuwunel) with signal SIGABRT.",
                ),
                pid1(
                    101,
                    HS,
                    "agent-bus-hs@team-a.service: Main process exited, code=dumped, status=6/ABRT",
                ),
                pid1(
                    101,
                    HS,
                    "agent-bus-hs@team-a.service: Failed with result 'watchdog'.",
                ),
                pid1(
                    106,
                    HS,
                    "agent-bus-hs@team-a.service: Scheduled restart job, restart counter is at 1.",
                ),
                pid1(
                    107,
                    HS,
                    "Started agent-bus-hs@team-a.service - Agent team bus homeserver for team team-a.",
                ),
            ]
        )
        self.assertEqual(len(stops), 1)
        self.assertEqual(stops[0].category, "watchdog")
        self.assertFalse(stops[0].requested)
        self.assertEqual(stops[0].nrestarts, 1)
        self.assertEqual(stops[0].status, "6/ABRT")
        self.assertIn("WARN something slow", "\n".join(stops[0].context))

    def test_oom_kill_beats_signal(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: A process of this unit has been killed by the OOM killer.",
                ),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: Main process exited, code=killed, status=9/KILL",
                ),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: Failed with result 'oom-kill'.",
                ),
            ]
        )
        self.assertEqual([s.category for s in stops], ["oom-kill"])

    def test_nonzero_exit_is_exit_code(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: Main process exited, code=exited, status=1/FAILURE",
                ),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: Failed with result 'exit-code'.",
                ),
            ]
        )
        self.assertEqual([s.category for s in stops], ["exit-code"])
        self.assertTrue(hj.CATEGORY_KIND["exit-code"] == "not deliberate")

    def test_failed_start_after_a_stop_is_its_own_stop(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                pid1(10, HS, "Stopping agent-bus-hs@team-a.service..."),
                pid1(11, HS, "Stopped agent-bus-hs@team-a.service."),
                pid1(5_000, HS, "Starting agent-bus-hs@team-a.service..."),
                pid1(
                    5_001,
                    HS,
                    "agent-bus-hs@team-a.service: Main process exited, code=exited, status=1/FAILURE",
                ),
                pid1(
                    5_001,
                    HS,
                    "agent-bus-hs@team-a.service: Failed with result 'exit-code'.",
                ),
            ]
        )
        self.assertEqual(
            [s.category for s in stops], ["requested, requester not found", "exit-code"]
        )
        self.assertTrue(stops[1].during_start)

    def test_systemd_oomd_kill_is_oom(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: systemd-oomd killed 1 process(es) in this unit.",
                ),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: Main process exited, code=killed, status=9/KILL",
                ),
            ]
        )
        self.assertEqual([s.category for s in stops], ["oom-kill"])

    def test_kernel_oom_line_lands_in_context(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                other(9, "kernel", "Out of memory: Killed process 4242 (tuwunel)"),
                pid1(
                    10,
                    HS,
                    "agent-bus-hs@team-a.service: Main process exited, code=killed, status=9/KILL",
                ),
            ]
        )
        self.assertIn("Out of memory", "\n".join(stops[0].context))


class Requested(unittest.TestCase):
    def test_health_restart(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                own(
                    500,
                    HEALTH,
                    "agent-bus health: agent-bus-hs@team-a.service: http://127.0.0.1:1/ missed 3 probes "
                    "10 s apart; restarting agent-bus-hs@team-a.service",
                    origin="python3",
                ),
                pid1(
                    501,
                    HS,
                    "Stopping agent-bus-hs@team-a.service - Agent team bus homeserver for team team-a...",
                ),
                pid1(
                    502,
                    HS,
                    "Stopped agent-bus-hs@team-a.service - Agent team bus homeserver for team team-a.",
                ),
            ]
        )
        self.assertEqual([s.category for s in stops], ["health-restart"])
        self.assertEqual(hj.CATEGORY_KIND["health-restart"], "not deliberate")

    def test_ansible_invocation_is_a_play(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                other(
                    400,
                    "ansible-ansible.legacy.command",
                    "Invoked with argv=['/usr/local/sbin/agent-bus-install', 'team', '--file', 'x'] chdir=None",
                ),
                pid1(420, HS, "Stopping agent-bus-hs@team-a.service..."),
            ]
        )
        self.assertEqual(stops[0].category, "deploy/play")
        self.assertIn("agent-bus-install", stops[0].actor)
        self.assertEqual(hj.CATEGORY_KIND["deploy/play"], "deliberate")

    def test_plan_run_window_names_the_run(self) -> None:
        win = hj.Window(
            "00203-teams-hot-control-and-robustness/acceptance/20261016-022144",
            T0 + 300,
            T0 + 900,
        )
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                other(
                    400,
                    "sudo",
                    "someone : PWD=/x ; USER=root ; COMMAND=/usr/local/sbin/agent-bus-install remove team-a",
                ),
                pid1(420, HS, "Stopping agent-bus-hs@team-a.service..."),
            ],
            [win],
        )
        self.assertEqual(stops[0].category, "acceptance")
        self.assertIn("acceptance/20261016-022144", stops[0].window)

    def test_stop_at_the_end_of_an_earlier_boot_is_a_reboot(self) -> None:
        stops = analyse(
            [
                pid1(-9_000, HS, "Started agent-bus-hs@team-a.service.", boot="b1"),
                pid1(-5_100, HS, "Stopping agent-bus-hs@team-a.service...", boot="b1"),
                pid1(-5_099, HS, "Stopped agent-bus-hs@team-a.service.", boot="b1"),
            ]
        )
        self.assertEqual([s.category for s in stops], ["reboot/shutdown"])

    def test_a_sudo_journalctl_is_no_actor(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                other(
                    400,
                    "sudo",
                    "someone : COMMAND=/usr/bin/journalctl -o json -u agent-bus-hs@*",
                ),
                pid1(420, HS, "Stopping agent-bus-hs@team-a.service..."),
            ]
        )
        self.assertEqual(stops[0].category, "requested, requester not found")

    def test_no_actor_found(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                pid1(5_000, HS, "Stopping agent-bus-hs@team-a.service..."),
            ]
        )
        self.assertEqual(
            [s.category for s in stops], ["requested, requester not found"]
        )
        self.assertEqual(hj.CATEGORY_KIND["requested, requester not found"], "unknown")


class BootEnds(unittest.TestCase):
    def test_boot_ending_with_the_unit_running_is_recorded(self) -> None:
        stops = analyse(
            [
                pid1(-9_000, HS, "Started agent-bus-hs@team-a.service.", boot="b1"),
                pid1(0, HS, "Started agent-bus-hs@team-a.service.", boot="b2"),
            ]
        )
        self.assertEqual([s.category for s in stops], ["boot ended, unit running"])
        self.assertEqual(stops[0].at, T0 - 5_000)

    def test_unit_running_in_the_current_boot_is_no_stop(self) -> None:
        self.assertEqual(
            analyse([pid1(0, HS, "Started agent-bus-hs@team-a.service.")]), []
        )


class Resources(unittest.TestCase):
    def test_memory_peak_parsed(self) -> None:
        stops = analyse(
            [
                pid1(0, HS, "Started agent-bus-hs@team-a.service."),
                pid1(10, HS, "Stopping agent-bus-hs@team-a.service..."),
                pid1(
                    11,
                    HS,
                    "agent-bus-hs@team-a.service: Consumed 1min 2.3s CPU time, 512.5M memory peak, "
                    "0B memory swap peak.",
                ),
            ]
        )
        self.assertIn("512.5M memory peak", stops[0].consumed)
        self.assertEqual(hj.size_bytes("512.5M"), int(512.5 * 1024 * 1024))


class Redaction(unittest.TestCase):
    def test_tokens_are_substituted(self) -> None:
        line = hj.redact(
            "GET /_matrix/client/v3/sync?access_token=syt_abc_DEF123 Authorization: Bearer syt_xyz_987"
        )
        self.assertNotIn("syt_abc", line)
        self.assertNotIn("syt_xyz", line)
        self.assertIn("access_token=<redacted>", line)


class Loading(unittest.TestCase):
    def test_journal_json_lines_load_and_dedupe(self) -> None:
        rec = {
            "__CURSOR": "c1",
            "__REALTIME_TIMESTAMP": str(int(T0 * 1_000_000)),
            "_BOOT_ID": "b2",
            "_PID": "1",
            "SYSLOG_IDENTIFIER": "systemd",
            "UNIT": HS,
            "MESSAGE": "Started agent-bus-hs@team-a.service.",
        }
        coredump = {
            "__CURSOR": "c2",
            "__REALTIME_TIMESTAMP": str(int(T0 * 1_000_000) + 5),
            "_BOOT_ID": "b2",
            "_PID": "77",
            "SYSLOG_IDENTIFIER": "systemd-coredump",
            "COREDUMP_UNIT": HS,
            "MESSAGE": [80, 114, 111, 99],
        }
        with tempfile.TemporaryDirectory() as tmp:
            one = pathlib.Path(tmp, "a.json")
            two = pathlib.Path(tmp, "b.json")
            one.write_text(json.dumps(rec) + "\n" + json.dumps(coredump) + "\n")
            two.write_text(json.dumps(rec) + "\n")
            entries = hj.load_entries([one, two])
        self.assertEqual(len(entries), 2)
        self.assertEqual(entries[0].origin, "systemd")
        self.assertEqual(entries[0].unit, HS)
        self.assertEqual(entries[1].unit, HS)
        self.assertEqual(entries[1].msg, "Proc")

    def test_plan_run_windows(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = pathlib.Path(tmp, "00203-x", "acceptance", "20261016-022144")
            run.mkdir(parents=True)
            log = run / "acceptance.log"
            log.write_text("x")
            start = time.mktime(time.strptime("20261016-022144", "%Y%m%d-%H%M%S"))
            os.utime(log, (start + 600, start + 600))
            meta = pathlib.Path(tmp, "_meta-deploy", "20261016-084726")
            meta.mkdir(parents=True)
            for name, offset in (
                ("play-claude-yolo.yml.log", 60),
                ("00203-x-acceptance.log", 120),
            ):
                (meta / name).write_text("x")
                mstart = time.mktime(time.strptime("20261016-084726", "%Y%m%d-%H%M%S"))
                os.utime(meta / name, (mstart + offset, mstart + offset))
            windows = hj.plan_run_windows(pathlib.Path(tmp))
        labels = {w.label: (w.start, w.end) for w in windows}
        self.assertEqual(
            labels["00203-x/acceptance/20261016-022144"], (start, start + 600)
        )
        self.assertIn("_meta-deploy/20261016-084726/00203-x-acceptance.log", labels)
        played = labels["_meta-deploy/20261016-084726/play-claude-yolo.yml.log"]
        accepted = labels["_meta-deploy/20261016-084726/00203-x-acceptance.log"]
        self.assertEqual(played[1], accepted[0])


if __name__ == "__main__":
    unittest.main()
