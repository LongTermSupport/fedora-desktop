"""Unit tests for helpers/agent_bus/health.py: the homeserver health check and `agent-bus health`.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_health

Plan 00203 gaps 4 and 10. `agent-bus-hs-health@<team>.timer` runs `health check` as root every
minute: it probes the client API of a homeserver that has been up past its start grace and
restarts one that misses every probe of a run, notes the restarts systemd made itself, and
judges the backups. It writes what it found to a record the stamps' group reads, which the
`ccy-teams` board and `agent-bus health status` show. The host's side is a fake; the record and
the backup directories are real files in a temporary directory.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import sys
import tempfile
import unittest
from collections.abc import Sequence

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import cli, health, render

TEAM = "team-a"
HS = "agent-bus-hs@team-a.service"
BACKUP = "agent-bus-backup@team-a.service"
TIMER = "agent-bus-backup@team-a.timer"
NOW = 1_791_000_000.0
HOUR = 3600.0


#: What systemd's D-Bus API gives every unit (org.freedesktop.systemd1.Unit) and each type
#: (.Service, .Timer) of the properties the check reads; `systemctl show` prints no other.
UNIT_PROPS = frozenset({"ActiveState", "SubState", "InvocationID", "ActiveEnterTimestampMonotonic"})
TYPE_PROPS = {"service": frozenset({"Result", "NRestarts"}),
              "timer": frozenset({"Result", "LastTriggerUSec", "NextElapseUSecRealtime"})}


def unit(**props: str) -> dict[str, str]:
    base = {"ActiveState": "active", "SubState": "running", "Result": "success",
            "InvocationID": "inv-1", "ActiveEnterTimestampMonotonic": str(int(1_000 * 1e6)),
            "NRestarts": "0", "LastTriggerUSec": "n/a", "NextElapseUSecRealtime": "n/a"}
    return {**base, **props}


class FakeHost:
    """systemctl show, the client API probe, restart, the clocks and the log."""

    def __init__(self, root: pathlib.Path) -> None:
        self.units: dict[str, dict[str, str] | list[dict[str, str]]] = {
            HS: unit(), BACKUP: unit(ActiveState="inactive", SubState="dead"),
            TIMER: unit(SubState="waiting", LastTriggerUSec=f"@{int(NOW - 2 * HOUR)}",
                        NextElapseUSecRealtime=f"@{int(NOW + 20 * HOUR)}"),
        }
        #: Each probe's answer in turn, the last kept.
        self.answers = [True]
        self.probes: list[tuple[str, float]] = []
        self.restarted: list[str] = []
        self.restart_fails = False
        self.slept: list[float] = []
        self.now = NOW
        self.mono = 2_000.0
        self.said: list[str] = []
        self.shown: list[tuple[str, tuple[str, ...]]] = []
        self.state_root = root / "state"
        self.install_root = root / "install"
        self.health_dir = root / "applied" / "health"
        self.health_dir.parent.mkdir()
        (self.state_root / TEAM).mkdir(parents=True)
        (self.state_root / TEAM / "team.json").write_text(json.dumps({"team": TEAM, "port": 8448}), encoding="utf-8")

    def show(self, name: str, props: Sequence[str]) -> dict[str, str]:
        """As real `systemctl show --property=...` does: a property the unit's type does not have
        is left out of the output, not refused, and the real parser judges what came back."""
        self.shown.append((name, tuple(props)))
        found = self.units[name]
        if isinstance(found, list):
            found = found.pop(0) if len(found) > 1 else found[0]
        has = UNIT_PROPS | TYPE_PROPS[name.rsplit(".", 1)[1]]
        out = "".join(f"{key}={found[key]}\n" for key in props if key in has)
        return health.parse_show(name, props, out)

    def get(self, url: str, timeout: float) -> bool:
        self.probes.append((url, timeout))
        return self.answers.pop(0) if len(self.answers) > 1 else self.answers[0]

    def restart(self, name: str) -> None:
        if self.restart_fails:
            raise health.HealthError(f"systemctl restart {name} failed: refused")
        self.restarted.append(name)

    def sleep(self, seconds: float) -> None:
        self.slept.append(seconds)
        self.now += seconds
        self.mono += seconds

    def backup(self, number: int, age: float) -> None:
        meta = self.state_root / TEAM / "backups" / "meta"
        meta.mkdir(parents=True, exist_ok=True)
        (meta / str(number)).write_text("", encoding="utf-8")
        tars = self.install_root / TEAM / "backups"
        tars.mkdir(parents=True, exist_ok=True)
        tar = tars / f"agent-bus-state-{number}.tar"
        tar.write_bytes(b"")
        os.utime(tar, (self.now - age, self.now - age))

    def system(self) -> health.HealthSystem:
        return health.HealthSystem(
            show=self.show, get=self.get, restart=self.restart, sleep=self.sleep,
            now=lambda: self.now, monotonic=lambda: self.mono, say=self.said.append,
            state_root=self.state_root, install_root=self.install_root, health_dir=self.health_dir,
            group_id=lambda: os.getgid(), teams_here=lambda: [TEAM])

    def record(self) -> dict:
        return json.loads((self.health_dir / f"{TEAM}.json").read_text(encoding="utf-8"))


class HealthTestCase(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.host = FakeHost(pathlib.Path(tmp.name))
        self.host.backup(3, 2 * HOUR)

    def check(self) -> None:
        health.check(TEAM, self.host.system())

    def problems(self) -> list[str]:
        return [problem["what"] for problem in self.host.record()["problems"]]


class UnitPropertiesTest(HealthTestCase):
    """The host's first run: every unit was asked for every property, and systemctl leaves a
    timer's properties out of a .service's answer, so each check failed with exit 78."""

    def test_a_property_systemctl_left_out_is_refused_by_name(self):
        out = "ActiveState=active\nSubState=running\n"
        with self.assertRaisesRegex(health.HealthError,
                                    r"^systemctl show agent-bus-hs@team-a.service gave no LastTriggerUSec, "
                                    r"NextElapseUSecRealtime$"):
            health.parse_show(HS, ("ActiveState", "SubState", "LastTriggerUSec", "NextElapseUSecRealtime"), out)

    def test_each_unit_is_asked_only_what_its_type_has(self):
        self.check()
        for name, props in self.host.shown:
            with self.subTest(unit=name):
                self.assertLessEqual(set(props), UNIT_PROPS | TYPE_PROPS[name.rsplit(".", 1)[1]])
        self.assertEqual({name for name, _ in self.host.shown}, {HS, BACKUP, TIMER})

    def test_a_healthy_team_checks_clean_and_reports_healthy(self):
        self.check()
        lines, problems = health.status([TEAM], self.host.system())
        self.assertEqual(problems, 0, lines)


class LivenessTest(HealthTestCase):
    def test_a_healthy_homeserver_is_left_alone(self):
        self.check()
        self.assertEqual(self.host.restarted, [])
        self.assertEqual(self.host.probes, [("http://127.0.0.1:8448/_matrix/client/versions", health.PROBE_TIMEOUT)])
        self.assertEqual(self.problems(), [])

    def test_a_homeserver_that_misses_every_probe_is_restarted(self):
        """Gap 4: a Tuwunel that still pings systemd's watchdog but whose client API has stopped
        answering (a stalled handler, a database stall) is restarted after three misses."""
        self.host.answers = [False]
        self.check()
        self.assertEqual(len(self.host.probes), health.PROBE_TRIES)
        self.assertEqual(self.host.slept, [health.PROBE_GAP] * (health.PROBE_TRIES - 1))
        self.assertEqual(self.host.restarted, [HS])
        self.assertTrue(any("restarting" in line and HS in line for line in self.host.said), self.host.said)
        [problem] = self.host.record()["problems"]
        self.assertIn("restarted by the health check", problem["what"])
        self.assertIn(f"journalctl -u {HS}", problem["fix"])

    def test_one_answer_in_the_run_is_enough(self):
        self.host.answers = [False, False, True]
        self.check()
        self.assertEqual(self.host.restarted, [])

    def test_a_homeserver_inside_its_start_grace_is_not_probed(self):
        self.host.mono = 1_000.0 + health.START_GRACE - 1
        self.host.answers = [False]
        self.check()
        self.assertEqual(self.host.probes, [])
        self.assertEqual(self.host.restarted, [])

    def test_a_homeserver_restarted_during_the_run_is_not_restarted_again(self):
        self.host.answers = [False]
        self.host.units[HS] = [unit(), unit(InvocationID="inv-2")]
        self.check()
        self.assertEqual(self.host.restarted, [])

    def test_a_stopped_or_starting_homeserver_is_not_probed(self):
        for state, sub in (("inactive", "dead"), ("activating", "auto-restart"), ("failed", "failed")):
            with self.subTest(state=state):
                self.host.units[HS] = unit(ActiveState=state, SubState=sub)
                self.host.answers = [False]
                self.check()
                self.assertEqual(self.host.probes, [])
                self.assertEqual(self.host.restarted, [])

    def test_a_restart_that_fails_fails_the_check(self):
        self.host.answers = [False]
        self.host.restart_fails = True
        with self.assertRaisesRegex(health.HealthError, "systemctl restart"):
            self.check()

    def test_restarts_systemd_made_are_noted_once_and_kept_a_day(self):
        """A watchdog abort or a crash is restarted by systemd itself (NRestarts rises); the
        board says so for a day."""
        self.check()
        self.host.units[HS] = unit(NRestarts="2", Result="watchdog", InvocationID="inv-3")
        self.check()
        [problem] = self.problems()
        self.assertIn("systemd restarted the homeserver 2 times", problem)
        self.assertIn("watchdog", problem)
        self.host.now += 2 * HOUR
        self.check()
        self.assertEqual(len(self.problems()), 1, "noted once, not on every run")
        self.host.now += health.EVENT_KEEP
        self.host.backup(4, 0)
        self.check()
        self.assertEqual(self.problems(), [])

    def test_a_lower_restart_count_is_a_manual_restart(self):
        self.host.units[HS] = unit(NRestarts="2", Result="watchdog")
        self.check()
        self.host.units[HS] = unit(NRestarts="0")
        self.host.now += health.EVENT_KEEP + 1
        self.host.backup(5, 0)
        self.check()
        self.assertEqual(self.problems(), [])


class BackupTest(HealthTestCase):
    def test_a_recent_backup_is_recorded(self):
        self.check()
        record = self.host.record()
        self.assertEqual(record["backup"], {"id": 3, "time": int(NOW - 2 * HOUR)})
        self.assertEqual(record["problems"], [])

    def test_the_newest_backup_needs_its_state_tar(self):
        meta = self.host.state_root / TEAM / "backups" / "meta"
        (meta / "9").write_text("", encoding="utf-8")
        (meta / "notes").write_text("", encoding="utf-8")
        self.check()
        self.assertEqual(self.host.record()["backup"]["id"], 3)

    def test_a_stale_backup_is_a_problem(self):
        """Gap 10: the newest backup older than the daily timer allows is shown, with what to do."""
        self.host.backup(4, health.BACKUP_MAX_AGE + HOUR)
        (self.host.install_root / TEAM / "backups" / "agent-bus-state-3.tar").unlink()
        self.check()
        [problem] = self.host.record()["problems"]
        self.assertIn("the newest backup is 1 d 13 h old", problem["what"])
        self.assertIn(f"sudo systemctl start {BACKUP}", problem["fix"])

    def test_a_failed_backup_is_a_problem(self):
        self.host.units[BACKUP] = unit(ActiveState="failed", SubState="failed", Result="exit-code")
        self.check()
        [problem] = self.host.record()["problems"]
        self.assertIn("the last backup failed (exit-code)", problem["what"])
        self.assertIn(f"journalctl -u {BACKUP}", problem["fix"])

    def test_no_backup_after_the_timer_ran_is_a_problem(self):
        (self.host.install_root / TEAM / "backups" / "agent-bus-state-3.tar").unlink()
        self.check()
        self.assertIn("no backup exists, though the daily backup has run", self.problems()[0])

    def test_no_backup_before_the_timer_first_ran_is_not(self):
        (self.host.install_root / TEAM / "backups" / "agent-bus-state-3.tar").unlink()
        self.host.units[TIMER] = unit(SubState="waiting", NextElapseUSecRealtime=f"@{int(NOW + HOUR)}")
        self.check()
        self.assertEqual(self.problems(), [])
        self.assertEqual(self.host.record()["backup_due"], int(NOW + HOUR))

    def test_a_stopped_backup_timer_is_a_problem(self):
        self.host.units[TIMER] = unit(ActiveState="inactive", SubState="dead")
        self.check()
        self.assertIn(f"the daily backup timer {TIMER} is inactive", self.problems()[0])


class RecordTest(HealthTestCase):
    def test_the_record_is_group_readable_only_and_expires(self):
        self.check()
        path = self.host.health_dir / f"{TEAM}.json"
        self.assertEqual(path.stat().st_mode & 0o777, 0o640)
        self.assertEqual(self.host.health_dir.stat().st_mode & 0o777, 0o750)
        record = self.host.record()
        self.assertEqual(record["checked"], int(NOW))
        self.assertEqual(record["expires"], int(NOW) + health.RECORD_LIFETIME)

    def test_a_missing_team_file_fails_the_check(self):
        (self.host.state_root / TEAM / "team.json").unlink()
        with self.assertRaisesRegex(health.HealthError, "team.json"):
            self.check()


class StatusTest(HealthTestCase):
    def status(self, *teams: str) -> tuple[int, str, str]:
        out, err = io.BytesIO(), io.StringIO()
        code = cli.main(["health", "status", *teams], stdout=out, stderr=err, health_system=self.host.system())
        return code, out.getvalue().decode(), err.getvalue()

    def test_a_healthy_team(self):
        self.check()
        code, out, err = self.status()
        self.assertEqual(code, 0, err)
        self.assertIn("team-a: homeserver active (running)", out)
        self.assertIn("newest backup #3", out)
        self.assertNotIn("PROBLEM", out)

    def test_problems_are_named_with_what_to_do(self):
        self.host.units[BACKUP] = unit(ActiveState="failed", SubState="failed", Result="exit-code")
        self.check()
        code, out, _ = self.status(TEAM)
        self.assertEqual(code, health.EXIT_PROBLEM)
        self.assertIn("team-a: PROBLEM the last backup failed (exit-code)", out)
        self.assertIn(f"team-a:   do: journalctl -u {BACKUP}", out)

    def test_a_homeserver_down_now_is_a_problem(self):
        self.check()
        self.host.units[HS] = unit(ActiveState="failed", SubState="failed")
        code, out, _ = self.status()
        self.assertEqual(code, health.EXIT_PROBLEM)
        self.assertIn("PROBLEM the homeserver is failed (failed)", out)

    def test_a_check_that_has_not_run_lately_is_a_problem(self):
        self.check()
        self.host.now += health.RECORD_LIFETIME + 60
        code, out, _ = self.status()
        self.assertEqual(code, health.EXIT_PROBLEM)
        self.assertIn("PROBLEM the health check last ran 6 min ago", out)
        self.assertIn("agent-bus-hs-health@team-a.timer", out)

    def test_a_check_that_never_ran_is_a_problem(self):
        code, out, _ = self.status()
        self.assertEqual(code, health.EXIT_PROBLEM)
        self.assertIn("PROBLEM the health check has never run", out)

    def test_the_sign_in_limits_and_what_to_do(self):
        """Gap 10: Tuwunel 1.9.4 keeps its per-account sign-in buckets in memory and logs a
        refusal only at debug level, compiled out of the release build, so a lockout cannot be
        read; the status says the limits and that the wait clears by itself."""
        self.check()
        _, out, _ = self.status()
        self.assertIn("3 wrong passwords, then one every 5 min", out)
        self.assertIn("5 sign-ins, then one every 6 min", out)
        self.assertIn("Too many login attempts", out)

    def test_an_unknown_team_is_refused(self):
        code, _, err = self.status("team-z")
        self.assertEqual(code, cli.EXIT_CONFIG)
        self.assertIn("team-z", err)


class LoginLimitsTest(unittest.TestCase):
    def test_the_words_follow_the_rendered_limits(self):
        text = health.login_limits_text()
        failed, account = render.LOGIN_LIMITS["failed"], render.LOGIN_LIMITS["account"]
        self.assertIn(f"{failed['burst_count']} wrong passwords", text)
        self.assertIn(f"{account['burst_count']} sign-ins", text)


if __name__ == "__main__":
    unittest.main()
