"""Unit tests for the ccy-lifecycle supervisor plugin (worker half).

Run from the repo root:

    python3 -m unittest tests.helpers.ccy_lifecycle.test_plugin

The plugin ships in the ccy image at /opt/claude-yolo/supervisor-plugins/ and is loaded by
the hooks-daemon supervisor's worker. Its contract is the supervisor's plugin API: a
module-level PLUGIN_API major and create_worker_half(api), whose half has on_start() and
on_idle(tick). The supervisor itself is not importable from this repo, so a minimal fake of
the api object stands in; it implements only the members the plugin uses, with the shapes
the supervisor documents (Notify kinds are the strings "restart-soon" and "deadline-reached").
"""

from __future__ import annotations

import dataclasses
import importlib.util
import json
import os
import pathlib
import stat
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
PLUGIN_PATH = REPO_ROOT / "files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py"

LAUNCH_A = "launch-1700000000-111"
LAUNCH_B = "launch-1700009999-222"
SESSION = "sess-test"
T0 = 1_790_000_000.0
MINUTE = 60.0


def load_plugin_module():
    spec = importlib.util.spec_from_file_location("ccy_lifecycle_under_test", PLUGIN_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


@dataclasses.dataclass(frozen=True)
class FakeExitForRestart:
    reason: str


@dataclasses.dataclass(frozen=True)
class FakeNotify:
    kind: str
    minutes: int | None = None


@dataclasses.dataclass(frozen=True)
class FakeTick:
    now: float
    session_id: str | None = SESSION


class FakeApi:
    api_version = (1, 0)
    ExitForRestart = FakeExitForRestart
    Notify = FakeNotify
    RESTART_SOON = "restart-soon"
    DEADLINE_REACHED = "deadline-reached"

    def __init__(self, state_dir: pathlib.Path) -> None:
        self.state_dir = state_dir
        self.audit_lines: list[str] = []

    def session_id(self):
        return SESSION

    def status(self, text, level="info", ttl=5.0):
        return None

    def audit(self, message):
        self.audit_lines.append(message)


def env(**overrides):
    base = {"CCY_LIFECYCLE_LAUNCH_ID": LAUNCH_A}
    base.update({k: str(v) for k, v in overrides.items() if v is not None})
    return base


class PluginTestCase(unittest.TestCase):
    def setUp(self):
        self.module = load_plugin_module()
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.state_dir = pathlib.Path(self._tmp.name) / "state"
        self.state_dir.mkdir(mode=0o700)
        self.api = FakeApi(self.state_dir)

    def half(self, environ, now=T0):
        half = self.module.create_worker_half(self.api, environ=environ, clock=lambda: now)
        half.on_start()
        return half

    def state(self):
        return json.loads((self.state_dir / "lifecycle.json").read_text(encoding="utf-8"))


class ContractTests(PluginTestCase):
    def test_declares_api_major_one_and_matching_name(self):
        self.assertEqual(self.module.PLUGIN_API, 1)
        half = self.module.create_worker_half(self.api, environ=env(), clock=lambda: T0)
        self.assertEqual(half.name, "ccy-lifecycle")
        self.assertIsInstance(half.version, str)

    def test_factory_works_with_the_signature_the_supervisor_uses(self):
        # The supervisor calls create_worker_half(api) with no other argument.
        half = self.module.create_worker_half(self.api)
        self.assertTrue(callable(half.on_start))
        self.assertTrue(callable(half.on_idle))

    def test_module_is_stdlib_only(self):
        source = PLUGIN_PATH.read_text(encoding="utf-8")
        for line in source.splitlines():
            stripped = line.strip()
            if stripped.startswith(("import ", "from ")):
                top = stripped.split()[1].split(".")[0]
                self.assertIn(
                    top,
                    {"__future__", "json", "math", "os", "re", "time", "pathlib", "typing"},
                    f"unexpected import: {stripped}",
                )


class ConfigTests(PluginTestCase):
    def test_nothing_configured_is_off_and_writes_no_state(self):
        half = self.half(env())
        self.assertIsNone(half.on_idle(FakeTick(T0 + 10_000_000)))
        self.assertFalse((self.state_dir / "lifecycle.json").exists())

    def test_empty_values_count_as_absent(self):
        half = self.half(
            env(
                CCY_LIFECYCLE_MAX_AGE_SECONDS="",
                CCY_LIFECYCLE_DEADLINE_EPOCH="",
                CCY_LIFECYCLE_WARN_MINUTES="",
            )
        )
        self.assertIsNone(half.on_idle(FakeTick(T0 + 10_000_000)))

    def test_invalid_values_fail_loudly_at_start(self):
        bad = [
            {"CCY_LIFECYCLE_MAX_AGE_SECONDS": "abc"},
            {"CCY_LIFECYCLE_MAX_AGE_SECONDS": "-5"},
            {"CCY_LIFECYCLE_MAX_AGE_SECONDS": "1.5"},
            {"CCY_LIFECYCLE_MAX_AGE_SECONDS": "٣٠٠"},  # arabic-indic digits
            {"CCY_LIFECYCLE_MAX_AGE_SECONDS": "60"},  # below the 30 minute floor
            {"CCY_LIFECYCLE_MAX_AGE_SECONDS": str(31 * 86400)},  # above the 30 day cap
            {"CCY_LIFECYCLE_DEADLINE_EPOCH": "tomorrow"},
            {"CCY_LIFECYCLE_DEADLINE_EPOCH": "0"},
            {"CCY_LIFECYCLE_WARN_MINUTES": "0"},
            {"CCY_LIFECYCLE_WARN_MINUTES": "241"},
            {"CCY_LIFECYCLE_MAX_AGE_SECONDS": "1800", "CCY_LIFECYCLE_WARN_MINUTES": "30"},
        ]
        for extra in bad:
            with self.subTest(extra=extra):
                api = FakeApi(self.state_dir)
                half = self.module.create_worker_half(api, environ=env(**extra), clock=lambda: T0)
                with self.assertRaises(ValueError):
                    half.on_start()

    def test_a_feature_without_a_launch_id_is_refused(self):
        half = self.module.create_worker_half(
            self.api,
            environ={"CCY_LIFECYCLE_MAX_AGE_SECONDS": "7200"},
            clock=lambda: T0,
        )
        with self.assertRaises(ValueError):
            half.on_start()

    def test_launch_id_shape_is_validated(self):
        for launch_id in ("../x", "a b", "x" * 200, "ab"):
            with self.subTest(launch_id=launch_id):
                half = self.module.create_worker_half(
                    FakeApi(self.state_dir),
                    environ={
                        "CCY_LIFECYCLE_MAX_AGE_SECONDS": "7200",
                        "CCY_LIFECYCLE_LAUNCH_ID": launch_id,
                    },
                    clock=lambda: T0,
                )
                with self.assertRaises(ValueError):
                    half.on_start()


class MaxAgeTests(PluginTestCase):
    MAX_AGE = 3 * 3600

    def environ(self, **extra):
        return env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE, **extra)

    def test_quiet_before_the_warning_window(self):
        half = self.half(self.environ())
        self.assertIsNone(half.on_idle(FakeTick(T0 + 60)))
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE - 1)))

    def test_warns_once_ten_minutes_before_max_age(self):
        half = self.half(self.environ())
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE))
        self.assertEqual(result, FakeNotify("restart-soon", 10))
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE - 5 * MINUTE)))

    def test_exits_for_restart_at_max_age(self):
        half = self.half(self.environ())
        half.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE))
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE))
        self.assertIsInstance(result, FakeExitForRestart)
        self.assertTrue(result.reason.isprintable())
        self.assertLess(len(result.reason), 120)

    def test_a_session_busy_through_the_window_restarts_at_its_first_idle_after_max_age(self):
        half = self.half(self.environ())
        # The supervisor only asks at idle. The first ask arrives long after max age.
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE + 5 * 3600))
        self.assertIsInstance(result, FakeExitForRestart)

    def test_a_late_first_ask_inside_the_window_clamps_the_minutes(self):
        half = self.half(self.environ())
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE - 3 * MINUTE - 30))
        self.assertEqual(result, FakeNotify("restart-soon", 4))

    def test_custom_warning_lead(self):
        half = self.half(self.environ(CCY_LIFECYCLE_WARN_MINUTES=25))
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE - 26 * MINUTE)))
        self.assertEqual(
            half.on_idle(FakeTick(T0 + self.MAX_AGE - 25 * MINUTE)),
            FakeNotify("restart-soon", 25),
        )

    def test_repeated_exit_requests_are_idempotent(self):
        half = self.half(self.environ())
        first = half.on_idle(FakeTick(T0 + self.MAX_AGE))
        second = half.on_idle(FakeTick(T0 + self.MAX_AGE + 30))
        self.assertEqual(first, second)
        # ...and the audit trail records the request once, not on every idle.
        requested = [line for line in self.api.audit_lines if "restart requested" in line]
        self.assertEqual(len(requested), 1)


class PersistenceTests(PluginTestCase):
    MAX_AGE = 2 * 3600

    def environ(self, launch=LAUNCH_A):
        return env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE, CCY_LIFECYCLE_LAUNCH_ID=launch)

    def test_start_time_is_persisted_with_private_mode(self):
        self.half(self.environ())
        self.assertEqual(self.state()["started_at"], T0)
        mode = stat.S_IMODE(os.stat(self.state_dir / "lifecycle.json").st_mode)
        self.assertEqual(mode, 0o600)

    def test_a_worker_hot_reload_does_not_reset_the_clock(self):
        self.half(self.environ(), now=T0)
        reloaded = self.half(self.environ(), now=T0 + 3000)
        self.assertEqual(self.state()["started_at"], T0)
        self.assertIsInstance(reloaded.on_idle(FakeTick(T0 + self.MAX_AGE)), FakeExitForRestart)

    def test_a_hot_reload_does_not_repeat_the_warning(self):
        first = self.half(self.environ(), now=T0)
        warn_at = T0 + self.MAX_AGE - 10 * MINUTE
        self.assertEqual(first.on_idle(FakeTick(warn_at)), FakeNotify("restart-soon", 10))
        reloaded = self.half(self.environ(), now=warn_at + 5)
        self.assertIsNone(reloaded.on_idle(FakeTick(warn_at + 10)))

    def test_a_new_container_starts_a_fresh_clock_and_warning(self):
        old = self.half(self.environ(LAUNCH_A), now=T0)
        old.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE))
        later = T0 + self.MAX_AGE + 100
        fresh = self.half(self.environ(LAUNCH_B), now=later)
        self.assertEqual(self.state()["started_at"], later)
        self.assertIsNone(fresh.on_idle(FakeTick(later + 60)))
        self.assertEqual(
            fresh.on_idle(FakeTick(later + self.MAX_AGE - 10 * MINUTE)),
            FakeNotify("restart-soon", 10),
        )

    def test_unreadable_state_is_an_error_not_a_silent_reset(self):
        (self.state_dir / "lifecycle.json").write_text("{not json", encoding="utf-8")
        half = self.module.create_worker_half(self.api, environ=self.environ(), clock=lambda: T0)
        with self.assertRaises(ValueError):
            half.on_start()

    def test_state_with_wrong_types_is_an_error(self):
        (self.state_dir / "lifecycle.json").write_text(
            json.dumps({"launch_id": LAUNCH_A, "started_at": "yesterday"}), encoding="utf-8"
        )
        half = self.module.create_worker_half(self.api, environ=self.environ(), clock=lambda: T0)
        with self.assertRaises(ValueError):
            half.on_start()


class DeadlineTests(PluginTestCase):
    DEADLINE = int(T0) + 2 * 3600

    def environ(self, launch=LAUNCH_A, **extra):
        return env(
            CCY_LIFECYCLE_DEADLINE_EPOCH=self.DEADLINE,
            CCY_LIFECYCLE_LAUNCH_ID=launch,
            **extra,
        )

    def test_quiet_until_the_deadline_then_notifies_once(self):
        half = self.half(self.environ())
        self.assertIsNone(half.on_idle(FakeTick(self.DEADLINE - 1)))
        self.assertEqual(half.on_idle(FakeTick(self.DEADLINE)), FakeNotify("deadline-reached"))
        self.assertIsNone(half.on_idle(FakeTick(self.DEADLINE + 60)))

    def test_the_deadline_never_exits(self):
        half = self.half(self.environ())
        for offset in (0, 60, 3600, 86_400):
            result = half.on_idle(FakeTick(self.DEADLINE + offset))
            self.assertNotIsInstance(result, FakeExitForRestart)

    def test_the_notice_is_not_repeated_after_a_hot_reload(self):
        half = self.half(self.environ(), now=T0)
        half.on_idle(FakeTick(self.DEADLINE))
        reloaded = self.half(self.environ(), now=self.DEADLINE + 10)
        self.assertIsNone(reloaded.on_idle(FakeTick(self.DEADLINE + 20)))

    def test_the_notice_is_not_repeated_after_a_restart_into_a_new_container(self):
        half = self.half(self.environ(LAUNCH_A), now=T0)
        half.on_idle(FakeTick(self.DEADLINE))
        relaunched = self.half(self.environ(LAUNCH_B), now=self.DEADLINE + 120)
        self.assertIsNone(relaunched.on_idle(FakeTick(self.DEADLINE + 180)))

    def test_a_different_deadline_notifies_again(self):
        half = self.half(self.environ(), now=T0)
        half.on_idle(FakeTick(self.DEADLINE))
        new_deadline = self.DEADLINE + 3600
        later = self.module.create_worker_half(
            self.api,
            environ=env(CCY_LIFECYCLE_DEADLINE_EPOCH=new_deadline),
            clock=lambda: float(self.DEADLINE + 10),
        )
        later.on_start()
        self.assertIsNone(later.on_idle(FakeTick(new_deadline - 1)))
        self.assertEqual(later.on_idle(FakeTick(new_deadline)), FakeNotify("deadline-reached"))


class CombinedTests(PluginTestCase):
    MAX_AGE = 3600 * 2

    def test_no_restart_once_the_deadline_has_been_announced(self):
        deadline = int(T0) + 3600
        environ = env(
            CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE,
            CCY_LIFECYCLE_DEADLINE_EPOCH=deadline,
        )
        half = self.half(environ)
        self.assertEqual(half.on_idle(FakeTick(deadline)), FakeNotify("deadline-reached"))
        # Max age is later than the deadline; a session told to wrap up is not restarted.
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE + 10)))

    def test_restart_still_happens_when_the_deadline_is_far_off(self):
        environ = env(
            CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE,
            CCY_LIFECYCLE_DEADLINE_EPOCH=int(T0) + 10 * 86_400,
        )
        half = self.half(environ)
        self.assertIsInstance(half.on_idle(FakeTick(T0 + self.MAX_AGE)), FakeExitForRestart)


if __name__ == "__main__":
    unittest.main()
