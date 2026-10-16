"""Tests for m2_inbound.py, M2 acceptance (b)'s inbound probe (SPEC-hot-control.md H34).

Run from anywhere:
    python3 CLAUDE/Plan/00203-teams-hot-control-and-robustness/test_m2_inbound.py
"""

from __future__ import annotations

import importlib.util
import json
import pathlib
import sys
import tempfile
import unittest

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("m2_inbound", _HERE / "m2_inbound.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'm2_inbound.py'}")
mi = importlib.util.module_from_spec(_SPEC)
sys.modules["m2_inbound"] = mi
_SPEC.loader.exec_module(mi)

BYPASS = "--dangerously-skip-permissions"


class InboundTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self._tmp.name)
        self.proc = self.root / "proc"
        self.proc.mkdir()
        self.managed = self.root / "etc-claude-code"
        self.settings = self.root / "home" / "settings.json"
        self.settings.parent.mkdir()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def process(self, pid: str, argv: list[str]) -> None:
        base = self.proc / pid
        base.mkdir()
        (base / "cmdline").write_bytes(b"\0".join(a.encode() for a in argv) + b"\0")

    def session(self, inbound: str | None, *extra: str) -> None:
        data: dict[str, object] = {"statusLine": {"type": "command", "command": "x"}}
        if inbound is not None:
            data["crossSessionInbound"] = inbound
        self.settings.write_text(json.dumps(data), encoding="utf-8")
        self.process("1", ["catatonit", "--", "/entrypoint.sh", "claude"])
        self.process("2", ["claude", BYPASS, "--plugin-dir", "/kit", "--settings", str(self.settings), *extra])

    def verdict(self) -> list[str]:
        return mi.inbound(self.proc, self.managed, self.settings)

    def test_a_refusing_launch_passes_and_says_why(self) -> None:
        self.session("refuse")
        lines = self.verdict()
        self.assertTrue(any("refuse" in line for line in lines), lines)
        self.assertTrue(any("bypass" in line for line in lines), lines)

    def test_no_setting_or_accept_fails(self) -> None:
        for value in (None, "accept", "hold"):
            with self.subTest(value=value):
                self.setUp()
                self.session(value)
                with self.assertRaises(mi.Failed):
                    self.verdict()

    def test_launched_with_other_settings_fails(self) -> None:
        self.session("refuse")
        other = self.root / "other.json"
        other.write_text(json.dumps({"crossSessionInbound": "refuse"}), encoding="utf-8")
        (self.proc / "2" / "cmdline").write_bytes(b"claude\0--settings\0" + str(other).encode() + b"\0")
        with self.assertRaises(mi.Failed):
            self.verdict()

    def test_managed_settings_that_accept_fail(self) -> None:
        self.session("refuse")
        self.managed.mkdir()
        (self.managed / "managed-settings.json").write_text(json.dumps({"crossSessionInbound": "accept"}), encoding="utf-8")
        with self.assertRaises(mi.Failed):
            self.verdict()

    def test_managed_drop_in_that_accepts_fails(self) -> None:
        self.session("refuse")
        (self.managed / "managed-settings.d").mkdir(parents=True)
        (self.managed / "managed-settings.d" / "10-x.json").write_text(
            json.dumps({"crossSessionInbound": "accept"}), encoding="utf-8")
        with self.assertRaises(mi.Failed):
            self.verdict()

    def test_managed_settings_silent_on_inbound_pass(self) -> None:
        self.session("refuse")
        self.managed.mkdir()
        (self.managed / "managed-settings.json").write_text(json.dumps({"model": "x"}), encoding="utf-8")
        self.verdict()

    def test_no_claude_or_two_fail(self) -> None:
        self.settings.write_text(json.dumps({"crossSessionInbound": "refuse"}), encoding="utf-8")
        with self.assertRaises(mi.Failed):
            self.verdict()
        self.process("2", ["claude", "--settings", str(self.settings)])
        self.process("3", ["/usr/local/bin/claude", "--settings", str(self.settings)])
        with self.assertRaises(mi.Failed):
            self.verdict()

    def test_main(self) -> None:
        self.session("refuse")
        self.assertEqual(mi.main(["inbound", str(self.proc), str(self.managed), str(self.settings)]), 0)
        self.session_fails_main()

    def session_fails_main(self) -> None:
        self.settings.write_text(json.dumps({}), encoding="utf-8")
        self.assertEqual(mi.main(["inbound", str(self.proc), str(self.managed), str(self.settings)]), 1)
        self.assertEqual(mi.main(["nope"]), 64)


if __name__ == "__main__":
    unittest.main()
