"""Unit tests for the ccy-lifecycle supervisor plugin (worker half).

Run from the repo root:

    python3 -m unittest tests.helpers.ccy_lifecycle.test_plugin

The plugin ships in the ccy image at /opt/claude-yolo/supervisor-plugins/ and is loaded by
the hooks-daemon supervisor's worker. Its contract is the supervisor's plugin API: a
module-level PLUGIN_API major and create_worker_half(api), whose half has on_start() and
on_idle(tick).

Most tests drive the half against a minimal fake of the api object, which implements only
the members the plugin uses, with the shapes the supervisor documents (Notify kinds are the
strings "restart-soon" and "deadline-reached"). The fake can drift from the real api, so
SupervisorHarnessContractTests also loads the plugin through the supervisor's own
PluginTestHarness: the real loader, vetting, API-major check and budgeted hook calls. That
class needs a supervisor that ships the harness (see _locate_supervisor) and SKIPS, with the
reason written to stderr, when there is none.
"""

from __future__ import annotations

import dataclasses
import hashlib
import importlib.util
import json
import os
import pathlib
import shutil
import stat
import sys
import tempfile
import time
import unittest
from types import ModuleType
from typing import Any
from unittest import mock

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
PLUGIN_PATH = REPO_ROOT / "files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py"

LAUNCH_A = "launch-1700000000-111"
LAUNCH_B = "launch-1700009999-222"
SESSION = "sess-test"
OTHER_SESSION = "sess-other"
T0 = 1_790_000_000.0
MINUTE = 60.0


def load_plugin_module() -> ModuleType:
    """Import the plugin file by path, as a fresh module per call (each test is isolated)."""
    spec = importlib.util.spec_from_file_location("ccy_lifecycle_under_test", PLUGIN_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {PLUGIN_PATH}")
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

    def session_id(self) -> str | None:
        return SESSION

    def status(self, text: str, level: str = "info", ttl: float = 5.0) -> None:
        return None

    def audit(self, message: str) -> None:
        self.audit_lines.append(message)


def env(**overrides: object) -> dict[str, str]:
    base = {"CCY_LIFECYCLE_LAUNCH_ID": LAUNCH_A}
    base.update({k: str(v) for k, v in overrides.items() if v is not None})
    return base


def session_key(session_id: str) -> str:
    return hashlib.sha256(session_id.encode("utf-8")).hexdigest()[:32]


class PluginTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.module = load_plugin_module()
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.state_dir = pathlib.Path(self._tmp.name) / "state"
        self.state_dir.mkdir(mode=0o700)
        self.api = FakeApi(self.state_dir)

    def half(self, environ: dict[str, str], now: float = T0) -> Any:
        half = self.module.create_worker_half(self.api, environ=environ, clock=lambda: now)
        half.on_start()
        return half

    def state_path(self, launch: str = LAUNCH_A) -> pathlib.Path:
        return self.state_dir / f"lifecycle-{launch}.json"

    def state(self, launch: str = LAUNCH_A) -> Any:
        return json.loads(self.state_path(launch).read_text(encoding="utf-8"))

    def deadline_marker(self, deadline: int, session_id: str = SESSION) -> pathlib.Path:
        return self.state_dir / f"deadline-{deadline}-{session_key(session_id)}.json"


class ContractTests(PluginTestCase):
    def test_declares_api_major_one_and_matching_name(self) -> None:
        self.assertEqual(self.module.PLUGIN_API, 1)
        half = self.module.create_worker_half(self.api, environ=env(), clock=lambda: T0)
        self.assertEqual(half.name, "ccy-lifecycle")
        self.assertIsInstance(half.version, str)

    def test_factory_works_with_the_signature_the_supervisor_uses(self) -> None:
        # The supervisor calls create_worker_half(api) with no other argument.
        half = self.module.create_worker_half(self.api)
        self.assertTrue(callable(half.on_start))
        self.assertTrue(callable(half.on_idle))

    def test_module_is_stdlib_only(self) -> None:
        source = PLUGIN_PATH.read_text(encoding="utf-8")
        for line in source.splitlines():
            stripped = line.strip()
            if stripped.startswith(("import ", "from ")):
                top = stripped.split()[1].split(".")[0]
                self.assertIn(
                    top,
                    {
                        "__future__",
                        "collections",
                        "dataclasses",
                        "hashlib",
                        "json",
                        "math",
                        "os",
                        "pathlib",
                        "re",
                        "stat",
                        "time",
                        "typing",
                    },
                    f"unexpected import: {stripped}",
                )

    def test_on_idle_before_on_start_does_nothing(self) -> None:
        # The one path where no configuration exists yet: it must be "off", not an error.
        half = self.module.create_worker_half(
            self.api, environ=env(CCY_LIFECYCLE_MAX_AGE_SECONDS=7200), clock=lambda: T0
        )
        self.assertIsNone(half.on_idle(FakeTick(T0 + 10_000_000)))
        self.assertEqual(list(self.state_dir.iterdir()), [])

    def test_a_failed_restart_of_the_half_leaves_it_off_not_half_configured(self) -> None:
        good = env(CCY_LIFECYCLE_MAX_AGE_SECONDS=7200)
        half = self.half(good)
        self.assertIsNotNone(half.on_idle(FakeTick(T0 + 7200)))
        half._environ = env(CCY_LIFECYCLE_MAX_AGE_SECONDS="junk")
        with self.assertRaises(ValueError):
            half.on_start()
        self.assertIsNone(half.on_idle(FakeTick(T0 + 7300)))


class ConfigTests(PluginTestCase):
    def test_nothing_configured_is_off_and_writes_no_state(self) -> None:
        half = self.half(env())
        self.assertIsNone(half.on_idle(FakeTick(T0 + 10_000_000)))
        self.assertEqual(list(self.state_dir.iterdir()), [])

    def test_empty_values_count_as_absent(self) -> None:
        half = self.half(
            env(
                CCY_LIFECYCLE_MAX_AGE_SECONDS="",
                CCY_LIFECYCLE_DEADLINE_EPOCH="",
                CCY_LIFECYCLE_WARN_MINUTES="",
            )
        )
        self.assertIsNone(half.on_idle(FakeTick(T0 + 10_000_000)))

    def test_invalid_values_fail_loudly_at_start(self) -> None:
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

    def test_a_feature_without_a_launch_id_is_refused(self) -> None:
        half = self.module.create_worker_half(
            self.api,
            environ={"CCY_LIFECYCLE_MAX_AGE_SECONDS": "7200"},
            clock=lambda: T0,
        )
        with self.assertRaises(ValueError):
            half.on_start()

    def test_launch_id_shape_is_validated(self) -> None:
        for launch_id in ("../x", "a b", "x" * 200, "ab", "launch/../../etc"):
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

    def environ(self, **extra: object) -> dict[str, str]:
        return env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE, **extra)

    def test_quiet_before_the_warning_window(self) -> None:
        half = self.half(self.environ())
        self.assertIsNone(half.on_idle(FakeTick(T0 + 60)))
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE - 1)))

    def test_warns_once_ten_minutes_before_max_age(self) -> None:
        half = self.half(self.environ())
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE))
        self.assertEqual(result, FakeNotify("restart-soon", 10))
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE - 5 * MINUTE)))

    def test_exits_for_restart_at_max_age(self) -> None:
        half = self.half(self.environ())
        half.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE))
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE))
        self.assertIsInstance(result, FakeExitForRestart)
        self.assertTrue(result.reason.isprintable())
        self.assertLess(len(result.reason), 120)

    def test_a_session_busy_through_the_window_restarts_at_its_first_idle_after_max_age(
        self,
    ) -> None:
        half = self.half(self.environ())
        # The supervisor only asks at idle. The first ask arrives long after max age.
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE + 5 * 3600))
        self.assertIsInstance(result, FakeExitForRestart)

    def test_a_late_first_ask_inside_the_window_clamps_the_minutes(self) -> None:
        half = self.half(self.environ())
        result = half.on_idle(FakeTick(T0 + self.MAX_AGE - 3 * MINUTE - 30))
        self.assertEqual(result, FakeNotify("restart-soon", 4))

    def test_custom_warning_lead(self) -> None:
        half = self.half(self.environ(CCY_LIFECYCLE_WARN_MINUTES=25))
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE - 26 * MINUTE)))
        self.assertEqual(
            half.on_idle(FakeTick(T0 + self.MAX_AGE - 25 * MINUTE)),
            FakeNotify("restart-soon", 25),
        )

    def test_repeated_exit_requests_are_idempotent(self) -> None:
        half = self.half(self.environ())
        first = half.on_idle(FakeTick(T0 + self.MAX_AGE))
        second = half.on_idle(FakeTick(T0 + self.MAX_AGE + 30))
        self.assertEqual(first, second)
        # ...and the audit trail records the request once, not on every idle.
        requested = [line for line in self.api.audit_lines if "restart requested" in line]
        self.assertEqual(len(requested), 1)


class PersistenceTests(PluginTestCase):
    MAX_AGE = 2 * 3600

    def environ(self, launch: str = LAUNCH_A) -> dict[str, str]:
        return env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE, CCY_LIFECYCLE_LAUNCH_ID=launch)

    def test_start_time_is_persisted_per_launch_with_private_mode(self) -> None:
        self.half(self.environ())
        self.assertEqual(self.state()["started_at"], T0)
        self.assertEqual(self.state()["launch_id"], LAUNCH_A)
        mode = stat.S_IMODE(os.stat(self.state_path()).st_mode)
        self.assertEqual(mode, 0o600)

    def test_a_worker_hot_reload_does_not_reset_the_clock(self) -> None:
        self.half(self.environ(), now=T0)
        reloaded = self.half(self.environ(), now=T0 + 3000)
        self.assertEqual(self.state()["started_at"], T0)
        self.assertIsInstance(reloaded.on_idle(FakeTick(T0 + self.MAX_AGE)), FakeExitForRestart)

    def test_a_hot_reload_does_not_repeat_the_warning(self) -> None:
        first = self.half(self.environ(), now=T0)
        warn_at = T0 + self.MAX_AGE - 10 * MINUTE
        self.assertEqual(first.on_idle(FakeTick(warn_at)), FakeNotify("restart-soon", 10))
        reloaded = self.half(self.environ(), now=warn_at + 5)
        self.assertIsNone(reloaded.on_idle(FakeTick(warn_at + 10)))

    def test_a_new_container_starts_a_fresh_clock_and_warning(self) -> None:
        old = self.half(self.environ(LAUNCH_A), now=T0)
        old.on_idle(FakeTick(T0 + self.MAX_AGE - 10 * MINUTE))
        later = T0 + self.MAX_AGE + 100
        fresh = self.half(self.environ(LAUNCH_B), now=later)
        self.assertEqual(self.state(LAUNCH_B)["started_at"], later)
        self.assertIsNone(fresh.on_idle(FakeTick(later + 60)))
        self.assertEqual(
            fresh.on_idle(FakeTick(later + self.MAX_AGE - 10 * MINUTE)),
            FakeNotify("restart-soon", 10),
        )

    def test_unreadable_state_is_an_error_not_a_silent_reset(self) -> None:
        self.state_path().write_text("{not json", encoding="utf-8")
        half = self.module.create_worker_half(self.api, environ=self.environ(), clock=lambda: T0)
        with self.assertRaises(ValueError):
            half.on_start()

    def test_state_with_wrong_types_is_an_error(self) -> None:
        self.state_path().write_text(
            json.dumps({"launch_id": LAUNCH_A, "started_at": "yesterday"}), encoding="utf-8"
        )
        half = self.module.create_worker_half(self.api, environ=self.environ(), clock=lambda: T0)
        with self.assertRaises(ValueError):
            half.on_start()

    def test_a_state_file_naming_another_launch_is_an_error(self) -> None:
        # The file name is the key; contents that disagree with it mean something wrote the
        # wrong file, which must not be adopted as this container's clock.
        self.state_path(LAUNCH_A).write_text(
            json.dumps({"launch_id": LAUNCH_B, "started_at": T0}), encoding="utf-8"
        )
        half = self.module.create_worker_half(self.api, environ=self.environ(), clock=lambda: T0)
        with self.assertRaises(ValueError):
            half.on_start()


class ConcurrentSessionTests(PluginTestCase):
    """Several containers of one project share the supervisor's per-project state_dir."""

    MAX_AGE = 2 * 3600

    def environ(self, launch: str, **extra: object) -> dict[str, str]:
        return env(
            CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE, CCY_LIFECYCLE_LAUNCH_ID=launch, **extra
        )

    def test_two_sessions_keep_their_own_clocks_across_interleaved_reloads(self) -> None:
        self.half(self.environ(LAUNCH_A), now=T0)
        self.half(self.environ(LAUNCH_B), now=T0 + 1000)
        # A's worker hot-reloads after B started: A must keep its own start, not adopt B's
        # launch as "a new container" and restart its clock.
        a_reloaded = self.half(self.environ(LAUNCH_A), now=T0 + 2000)
        b_reloaded = self.half(self.environ(LAUNCH_B), now=T0 + 3000)

        self.assertEqual(self.state(LAUNCH_A)["started_at"], T0)
        self.assertEqual(self.state(LAUNCH_B)["started_at"], T0 + 1000)
        self.assertIsInstance(a_reloaded.on_idle(FakeTick(T0 + self.MAX_AGE)), FakeExitForRestart)
        self.assertIsNone(b_reloaded.on_idle(FakeTick(T0 + self.MAX_AGE)))
        self.assertIsInstance(
            b_reloaded.on_idle(FakeTick(T0 + 1000 + self.MAX_AGE)), FakeExitForRestart
        )

    def test_one_sessions_warning_does_not_silence_the_other(self) -> None:
        a = self.half(self.environ(LAUNCH_A), now=T0)
        self.half(self.environ(LAUNCH_B), now=T0)
        warn_at = T0 + self.MAX_AGE - 10 * MINUTE
        self.assertEqual(a.on_idle(FakeTick(warn_at)), FakeNotify("restart-soon", 10))
        b_reloaded = self.half(self.environ(LAUNCH_B), now=warn_at)
        self.assertEqual(b_reloaded.on_idle(FakeTick(warn_at)), FakeNotify("restart-soon", 10))

    def test_two_sessions_with_the_same_deadline_are_each_told(self) -> None:
        deadline = int(T0) + 3600
        a = self.half(env(CCY_LIFECYCLE_DEADLINE_EPOCH=deadline, CCY_LIFECYCLE_LAUNCH_ID=LAUNCH_A))
        b = self.half(env(CCY_LIFECYCLE_DEADLINE_EPOCH=deadline, CCY_LIFECYCLE_LAUNCH_ID=LAUNCH_B))
        self.assertEqual(
            a.on_idle(FakeTick(deadline, session_id=SESSION)), FakeNotify("deadline-reached")
        )
        self.assertEqual(
            b.on_idle(FakeTick(deadline + 5, session_id=OTHER_SESSION)),
            FakeNotify("deadline-reached"),
        )


class PruneTests(PluginTestCase):
    MAX_AGE = 2 * 3600

    def age(self, path: pathlib.Path, seconds: float) -> None:
        stamp = T0 - seconds
        os.utime(path, (stamp, stamp))

    def write(self, name: str, content: str = "{}") -> pathlib.Path:
        path = self.state_dir / name
        path.write_text(content, encoding="utf-8")
        return path

    def test_stale_files_of_ended_launches_and_old_deadlines_are_removed(self) -> None:
        window = self.module.PRUNE_AFTER_SECONDS
        stale_launch = self.write(f"lifecycle-{LAUNCH_B}.json")
        stale_marker = self.write(f"deadline-{int(T0) - 99}-{session_key(OTHER_SESSION)}.json")
        stale_tmp = self.write(f"lifecycle-{LAUNCH_B}.json.tmp.4242")
        for path in (stale_launch, stale_marker, stale_tmp):
            self.age(path, window + 1)

        self.half(env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE))

        for path in (stale_launch, stale_marker, stale_tmp):
            self.assertFalse(path.exists(), path.name)
        self.assertTrue(self.state_path(LAUNCH_A).exists())

    def test_a_concurrent_sessions_recent_files_are_kept(self) -> None:
        window = self.module.PRUNE_AFTER_SECONDS
        live_launch = self.write(f"lifecycle-{LAUNCH_B}.json")
        live_marker = self.write(f"deadline-{int(T0)}-{session_key(OTHER_SESSION)}.json")
        for path in (live_launch, live_marker):
            self.age(path, window - 60)

        self.half(env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE))

        self.assertTrue(live_launch.exists())
        self.assertTrue(live_marker.exists())

    def test_files_the_plugin_does_not_own_are_never_touched(self) -> None:
        window = self.module.PRUNE_AFTER_SECONDS
        foreign = [self.write("notes.txt"), self.write("lifecycle.json"), self.write("other.json")]
        for path in foreign:
            self.age(path, window * 10)

        self.half(env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE))

        for path in foreign:
            self.assertTrue(path.exists(), path.name)

    def test_the_window_outlasts_the_longest_session_the_plugin_can_restart(self) -> None:
        self.assertGreater(self.module.PRUNE_AFTER_SECONDS, self.module.MAX_AGE_MAX_SECONDS)


class DeadlineTests(PluginTestCase):
    DEADLINE = int(T0) + 2 * 3600

    def environ(self, launch: str = LAUNCH_A, **extra: object) -> dict[str, str]:
        return env(
            CCY_LIFECYCLE_DEADLINE_EPOCH=self.DEADLINE,
            CCY_LIFECYCLE_LAUNCH_ID=launch,
            **extra,
        )

    def test_quiet_until_the_deadline_then_notifies_once(self) -> None:
        half = self.half(self.environ())
        self.assertIsNone(half.on_idle(FakeTick(self.DEADLINE - 1)))
        self.assertEqual(half.on_idle(FakeTick(self.DEADLINE)), FakeNotify("deadline-reached"))
        self.assertIsNone(half.on_idle(FakeTick(self.DEADLINE + 60)))

    def test_the_deadline_never_exits(self) -> None:
        half = self.half(self.environ())
        for offset in (0, 60, 3600, 86_400):
            result = half.on_idle(FakeTick(self.DEADLINE + offset))
            self.assertNotIsInstance(result, FakeExitForRestart)

    def test_the_notice_is_not_repeated_after_a_hot_reload(self) -> None:
        half = self.half(self.environ(), now=T0)
        half.on_idle(FakeTick(self.DEADLINE))
        reloaded = self.half(self.environ(), now=self.DEADLINE + 10)
        self.assertIsNone(reloaded.on_idle(FakeTick(self.DEADLINE + 20)))

    def test_the_notice_is_not_repeated_after_a_restart_into_a_new_container(self) -> None:
        half = self.half(self.environ(LAUNCH_A), now=T0)
        half.on_idle(FakeTick(self.DEADLINE))
        self.assertTrue(self.deadline_marker(self.DEADLINE).exists())
        relaunched = self.half(self.environ(LAUNCH_B), now=self.DEADLINE + 120)
        self.assertIsNone(relaunched.on_idle(FakeTick(self.DEADLINE + 180)))

    def test_a_relaunch_after_the_notice_does_not_restart_for_max_age(self) -> None:
        max_age = 2 * 3600
        environ_a = self.environ(LAUNCH_A, CCY_LIFECYCLE_MAX_AGE_SECONDS=max_age)
        half = self.half(environ_a, now=T0)
        self.assertEqual(half.on_idle(FakeTick(self.DEADLINE)), FakeNotify("deadline-reached"))
        later = self.DEADLINE + 60
        environ_b = self.environ(LAUNCH_B, CCY_LIFECYCLE_MAX_AGE_SECONDS=max_age)
        relaunched = self.half(environ_b, now=later)
        self.assertIsNone(relaunched.on_idle(FakeTick(later + 1)))
        self.assertIsNone(relaunched.on_idle(FakeTick(later + max_age + 60)))

    def test_with_no_single_session_id_the_notice_is_kept_per_container(self) -> None:
        half = self.half(self.environ(), now=T0)
        self.assertEqual(
            half.on_idle(FakeTick(self.DEADLINE, session_id=None)), FakeNotify("deadline-reached")
        )
        self.assertEqual(
            sorted(p.name for p in self.state_dir.iterdir()), [self.state_path().name]
        )
        reloaded = self.half(self.environ(), now=self.DEADLINE + 10)
        self.assertIsNone(reloaded.on_idle(FakeTick(self.DEADLINE + 20, session_id=None)))

    def test_a_different_deadline_notifies_again(self) -> None:
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

    def test_the_session_marker_is_private(self) -> None:
        half = self.half(self.environ())
        half.on_idle(FakeTick(self.DEADLINE))
        mode = stat.S_IMODE(os.stat(self.deadline_marker(self.DEADLINE)).st_mode)
        self.assertEqual(mode, 0o600)
        self.assertNotIn(SESSION, self.deadline_marker(self.DEADLINE).read_text("utf-8"))


class CombinedTests(PluginTestCase):
    MAX_AGE = 3600 * 2

    def test_no_restart_once_the_deadline_has_been_announced(self) -> None:
        deadline = int(T0) + 3600
        environ = env(
            CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE,
            CCY_LIFECYCLE_DEADLINE_EPOCH=deadline,
        )
        half = self.half(environ)
        self.assertEqual(half.on_idle(FakeTick(deadline)), FakeNotify("deadline-reached"))
        # Max age is later than the deadline; a session told to wrap up is not restarted.
        self.assertIsNone(half.on_idle(FakeTick(T0 + self.MAX_AGE + 10)))

    def test_restart_still_happens_when_the_deadline_is_far_off(self) -> None:
        environ = env(
            CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE,
            CCY_LIFECYCLE_DEADLINE_EPOCH=int(T0) + 10 * 86_400,
        )
        half = self.half(environ)
        self.assertIsInstance(half.on_idle(FakeTick(T0 + self.MAX_AGE)), FakeExitForRestart)


# -- the real supervisor's PluginTestHarness ------------------------------------------------

SUPERVISOR_PATH_ENV = "CCY_SUPERVISOR_PATH"
SUPERVISOR_IN_REPO = REPO_ROOT / ".claude/ccy/claude-supervise.py"
HARNESS_MARKER = "class PluginTestHarness"
PLUGIN_NAME = "ccy-lifecycle"
HARNESS_SESSION = "sess-contract"


def _locate_supervisor() -> tuple[pathlib.Path | None, str]:
    """Return (supervisor path, "") or (None, why there is none to test against).

    CCY_SUPERVISOR_PATH names a supervisor explicitly; a name that does not exist is a
    mistake by whoever set it and FAILS rather than skips. Otherwise the supervisor the
    hooks daemon deploys into this repo is used, if it is new enough to ship the harness.
    """
    explicit = os.environ.get(SUPERVISOR_PATH_ENV, "")
    if explicit:
        path = pathlib.Path(explicit)
        if not path.is_file():
            raise AssertionError(f"{SUPERVISOR_PATH_ENV} names no file: {explicit}")
        if HARNESS_MARKER not in path.read_text(encoding="utf-8"):
            raise AssertionError(f"{SUPERVISOR_PATH_ENV} names a supervisor without the harness")
        return path, ""
    relative = SUPERVISOR_IN_REPO.relative_to(REPO_ROOT)
    if not SUPERVISOR_IN_REPO.is_file():
        return None, f"no supervisor at {relative}; set {SUPERVISOR_PATH_ENV} to run it"
    if HARNESS_MARKER not in SUPERVISOR_IN_REPO.read_text(encoding="utf-8"):
        return None, (
            f"the supervisor at {relative} predates PluginTestHarness (plugin API); "
            f"set {SUPERVISOR_PATH_ENV} to a newer claude-supervise.py to run it"
        )
    return SUPERVISOR_IN_REPO, ""


def _load_supervisor(path: pathlib.Path) -> ModuleType:
    # Registered in sys.modules before exec, as the script's dataclasses require.
    name = "ccy_supervisor_for_contract_test"
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load the supervisor at {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    try:
        spec.loader.exec_module(module)
    except BaseException:
        sys.modules.pop(name, None)
        raise
    return module


class SupervisorHarnessContractTests(unittest.TestCase):
    """The plugin through the supervisor's own loader, not the fake above."""

    supervise: ModuleType

    MAX_AGE = 2 * 3600

    @classmethod
    def setUpClass(cls) -> None:
        path, why = _locate_supervisor()
        if path is None:
            reason = f"supervisor contract tests SKIPPED: {why}"
            print(reason, file=sys.stderr)
            raise unittest.SkipTest(reason)
        cls.supervise = _load_supervisor(path)

    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = pathlib.Path(tmp.name)
        # The loader refuses a plugin file or directory that is group- or world-writable or
        # owned by anyone but this uid or root. The image installs it root-owned and 0644;
        # a private copy owned by the test's own uid is the equivalent the loader accepts.
        plugin_dir = root / "plugins"
        plugin_dir.mkdir(mode=0o700)
        self.plugin = plugin_dir / PLUGIN_PATH.name
        shutil.copyfile(PLUGIN_PATH, self.plugin)
        self.plugin.chmod(0o600)
        self.work_dir = root / "work"

    def harness(self, environ: dict[str, str]) -> Any:
        patcher = mock.patch.dict(os.environ, environ)
        patcher.start()
        self.addCleanup(patcher.stop)
        harness = self.supervise.PluginTestHarness(
            PLUGIN_NAME, self.plugin, work_dir=self.work_dir, session_ids=(HARNESS_SESSION,)
        )
        harness.start()
        return harness

    def started_at(self, harness: Any, launch: str = LAUNCH_A) -> float:
        state_file = pathlib.Path(harness.state_dir) / f"lifecycle-{launch}.json"
        return float(json.loads(state_file.read_text(encoding="utf-8"))["started_at"])

    def test_the_real_loader_accepts_the_plugin(self) -> None:
        harness = self.harness(env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE))
        self.assertEqual(harness.half.name, PLUGIN_NAME)
        started = f"plugin {PLUGIN_NAME}: started:"
        self.assertTrue(any(line.startswith(started) for line in harness.audit_lines()))

    def test_max_age_warns_then_exits_through_the_real_api(self) -> None:
        harness = self.harness(env(CCY_LIFECYCLE_MAX_AGE_SECONDS=self.MAX_AGE))
        start = self.started_at(harness)
        self.assertIsNone(harness.idle(now=start + 60))
        self.assertEqual(harness.notifications, ())
        self.assertIsNone(harness.idle(now=start + self.MAX_AGE - 10 * MINUTE))
        self.assertEqual(
            harness.notifications,
            (self.supervise.PluginNotification(PLUGIN_NAME, "restart-soon", 10),),
        )
        request = harness.idle(now=start + self.MAX_AGE)
        self.assertIsInstance(request, self.supervise.PluginExitRequest)
        self.assertEqual(request.plugin, PLUGIN_NAME)

    def test_deadline_notice_through_the_real_api_and_a_reload(self) -> None:
        deadline = int(time.time()) + 3600
        environ = env(CCY_LIFECYCLE_DEADLINE_EPOCH=deadline)
        harness = self.harness(environ)
        self.assertIsNone(harness.idle(now=deadline))
        self.assertEqual(
            harness.notifications,
            (self.supervise.PluginNotification(PLUGIN_NAME, "deadline-reached", None),),
        )
        # A second harness over the same work_dir is a worker hot reload: same state_dir.
        reloaded = self.harness(environ)
        self.assertIsNone(reloaded.idle(now=deadline + 60))
        self.assertEqual(reloaded.notifications, ())

    def test_a_bad_value_disables_the_plugin_loudly(self) -> None:
        with self.assertRaises(self.supervise.PluginHarnessError):
            self.harness(env(CCY_LIFECYCLE_MAX_AGE_SECONDS="junk"))


if __name__ == "__main__":
    unittest.main()
