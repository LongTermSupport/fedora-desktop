"""Unit tests for helpers/containerwatch/cli.py — the thin side-effecting executor.

Run from the repo root (stdlib unittest only):

    python3 -m unittest tests.helpers.containerwatch.test_cli

These exercise the wiring the pure core can't: threshold/config resolution
(env > config > default), btime parsing, the atomic report round-trip, the
engine name-resolver and gdbus-emit argv (subprocess mocked, never spawned), the
engine-presence probe, and the --inject report builder. No real /proc, no
containers, no session bus.
"""

from __future__ import annotations

import json
import os
import pathlib
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.containerwatch import cli, core


class TestThresholds(unittest.TestCase):
    def test_defaults(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            age, cpu = cli.resolve_thresholds(config={})
        self.assertEqual(age, cli.DEFAULT_AGE_S)
        self.assertEqual(cpu, cli.DEFAULT_CPU_PCT)

    def test_config_overrides_default(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            age, cpu = cli.resolve_thresholds(config={"age_s": 60, "cpu_pct": 25})
        self.assertEqual(age, 60)
        self.assertEqual(cpu, 25)

    def test_env_overrides_config(self):
        with mock.patch.dict(os.environ, {"CW_AGE_S": "1", "CW_CPU_PCT": "5"}, clear=True):
            age, cpu = cli.resolve_thresholds(config={"age_s": 60, "cpu_pct": 25})
        self.assertEqual(age, 1)
        self.assertEqual(cpu, 5)


class TestConfig(unittest.TestCase):
    def test_missing_config_returns_empty(self):
        self.assertEqual(cli.load_config("/no/such/config.json"), {})

    def test_loads_allowlist(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "config.json")
            with open(p, "w", encoding="utf-8") as fh:
                json.dump({"allowlist": [{"container_name": "ci"}]}, fh)
            cfg = cli.load_config(p)
        self.assertEqual(cfg["allowlist"], [{"container_name": "ci"}])


class TestBtime(unittest.TestCase):
    def test_read_btime(self):
        with tempfile.TemporaryDirectory() as proc:
            with open(os.path.join(proc, "stat"), "w", encoding="utf-8") as fh:
                fh.write("cpu  1 2 3\nbtime 1700000000\nprocesses 99\n")
            self.assertEqual(cli.read_btime(proc), 1700000000)


class TestReportRoundTrip(unittest.TestCase):
    def test_build_and_write_atomic(self):
        attr = core.Attribution(engine="docker", rootless=False, owner_uid=0, container_token="abcdef")
        finding = core.make_finding(
            host_pid=10, container_pid=3, attr=attr, argv0="x", cmd="x",
            age_s=1000, cpu_pct=90, rss_kb=100, container_name="web",
            exec_hint="docker exec -it web ps",
        )
        report = cli.build_report([finding], host_cores=22, age_threshold=900, cpu_threshold=50, generated_at=123)
        self.assertEqual(report["schema"], core.SCHEMA_VERSION)
        self.assertEqual(report["generated_at"], 123)
        self.assertEqual(report["host_cores"], 22)
        self.assertEqual(report["thresholds"], {"age_s": 900, "cpu_pct": 50})
        self.assertEqual(len(report["findings"]), 1)

        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "sub", "report.json")
            cli.write_report_atomic(path, report)
            with open(path, encoding="utf-8") as fh:
                back = json.load(fh)
            self.assertEqual(back, report)
            # No leftover temp file beside the target.
            leftovers = [f for f in os.listdir(os.path.dirname(path)) if f != "report.json"]
            self.assertEqual(leftovers, [])


class TestNameResolver(unittest.TestCase):
    def test_podman_rootless_inspect(self):
        attr = core.Attribution(engine="podman", rootless=True, owner_uid=1000, container_token="deadbeef")
        with mock.patch.object(cli.subprocess, "run") as run:
            run.return_value = mock.Mock(stdout="proj_a_yolo\n", returncode=0)
            name = cli.resolve_name(attr)
        argv = run.call_args.args[0]
        self.assertEqual(argv[0], "podman")
        self.assertIn("inspect", argv)
        self.assertEqual(name, "proj_a_yolo")

    def test_resolution_failure_falls_back_to_short_token(self):
        attr = core.Attribution(engine="docker", rootless=False, owner_uid=0, container_token="abcdef0123456789")
        with mock.patch.object(cli.subprocess, "run", side_effect=cli.subprocess.CalledProcessError(1, "docker")):
            name = cli.resolve_name(attr)
        # Fall back to the short id, never crash the scan.
        self.assertEqual(name, "abcdef012345")

    def test_unknown_engine_uses_token_or_marker(self):
        attr = core.Attribution(engine="unknown", rootless=False, owner_uid=0, container_token="")
        # Must not shell out for an unknown engine.
        with mock.patch.object(cli.subprocess, "run") as run:
            name = cli.resolve_name(attr)
        run.assert_not_called()
        self.assertIn("unknown", name)


class TestEmitSignal(unittest.TestCase):
    def test_gdbus_argv(self):
        with mock.patch.object(cli.subprocess, "run") as run:
            run.return_value = mock.Mock(returncode=0)
            cli.emit_signal(count=3, report_path="/run/user/1000/container-watch/report.json")
        argv = run.call_args.args[0]
        self.assertEqual(argv[0], "gdbus")
        self.assertIn("emit", argv)
        self.assertIn(cli.DBUS_PATH, argv)
        self.assertIn(f"{cli.DBUS_INTERFACE}.FindingsChanged", argv)
        # Count is carried as a signal argument.
        self.assertIn("3", argv)

    def test_emit_failure_is_nonfatal(self):
        # No session bus (e.g. timer fired headless): emit fails but the report is
        # already written, so the scan must not abort.
        with mock.patch.object(cli.subprocess, "run", side_effect=cli.subprocess.CalledProcessError(1, "gdbus")):
            cli.emit_signal(count=1, report_path="/x")  # must not raise


class TestEngineProbe(unittest.TestCase):
    def test_podman_available(self):
        with mock.patch.object(cli.shutil, "which", side_effect=lambda b: "/usr/bin/podman" if b == "podman" else None):
            self.assertTrue(cli.engine_available("podman"))
            self.assertFalse(cli.engine_available("docker"))


class TestInject(unittest.TestCase):
    def test_inject_finding_file_builds_report(self):
        finding = {"host_pid": 1, "container_name": "x", "engine": "podman", "cmd": "y", "exec_hint": "z"}
        with tempfile.TemporaryDirectory() as d:
            fp = os.path.join(d, "f.json")
            with open(fp, "w", encoding="utf-8") as fh:
                json.dump(finding, fh)
            findings = cli.load_injected(fp)
        self.assertEqual(findings, [finding])

    def test_inject_empty_yields_no_findings(self):
        self.assertEqual(cli.load_injected("empty"), [])


class TestRender(unittest.TestCase):
    def _report(self):
        return {
            "schema": 1, "generated_at": 123, "host_cores": 22,
            "thresholds": {"age_s": 900, "cpu_pct": 50},
            "findings": [{
                "host_pid": 2124472, "container_pid": 42, "engine": "podman",
                "container_name": "proj_a_yolo", "argv0": "ugrep",
                "cmd": "ugrep -rl x /", "age_s": 6916, "cpu_pct": 1116,
                "exec_hint": "podman exec -it proj_a_yolo ps -o pid,%cpu,args -p 42",
            }],
        }

    def test_status_line_counts(self):
        self.assertIn("1", cli.render_status(self._report()))
        self.assertIn("0", cli.render_status({"findings": []}))

    def test_list_table_has_container_and_cmd(self):
        out = cli.render_list(self._report())
        self.assertIn("proj_a_yolo", out)
        self.assertIn("ugrep", out)

    def test_explain_shows_exec_hint(self):
        out = cli.render_explain(self._report(), host_pid=2124472)
        self.assertIn("podman exec -it proj_a_yolo", out)

    def test_explain_unknown_pid(self):
        out = cli.render_explain(self._report(), host_pid=999999)
        self.assertIn("999999", out)


if __name__ == "__main__":
    unittest.main()
