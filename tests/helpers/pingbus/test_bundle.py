"""Unit tests for helpers/pingbus/bundle.py, the reproducible zipapp builder.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_bundle

Plan 00161 U13: a rebuild is byte-identical, every module of the bus packages is in the
archive (including modules added after the bundler was written), and the archives run
`version`. Every archive is built in a temporary directory.
"""

from __future__ import annotations

import contextlib
import io
import os
import pathlib
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
import zipfile

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.pingbus import bundle, cli, protocol

AGENT_BUS_CLI = REPO_ROOT / "helpers" / "agent_bus" / "cli.py"


def bus_modules(source: pathlib.Path) -> set[str]:
    """Every module file of the bus packages, found independently of the bundler."""
    found = set()
    for package in ("pingbus", "agent_bus"):
        for path in (source / "helpers" / package).rglob("*.py"):
            if "__pycache__" not in path.parts:
                found.add(path.relative_to(source).as_posix())
    return found


def copy_source(dest: pathlib.Path) -> pathlib.Path:
    """The bus packages of this checkout, copied so a test can add files to them."""
    for package in ("pingbus", "agent_bus"):
        shutil.copytree(REPO_ROOT / "helpers" / package, dest / "helpers" / package,
                        ignore=shutil.ignore_patterns("__pycache__"))
    return dest


def run_archive(archive: pathlib.Path, *args: str) -> subprocess.CompletedProcess[str]:
    env = {key: value for key, value in os.environ.items() if not key.startswith("PYTHON")}
    with tempfile.TemporaryDirectory() as cwd:
        return subprocess.run([sys.executable, str(archive), *args], cwd=cwd, env=env,
                              capture_output=True, text=True, check=False, timeout=60)


class BuildTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = pathlib.Path(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_rebuild_is_byte_identical(self) -> None:
        first = bundle.build("pingbus", REPO_ROOT)
        second = bundle.build("pingbus", REPO_ROOT)
        self.assertEqual(first, second)

    def test_source_mtimes_and_modes_do_not_change_the_bytes(self) -> None:
        source = copy_source(self.tmp / "src")
        before = bundle.build("pingbus", source)
        for path in (source / "helpers").rglob("*.py"):
            os.utime(path, (1_900_000_000, 1_900_000_000))
            path.chmod(0o600)
        self.assertEqual(before, bundle.build("pingbus", source))

    def test_every_module_is_included(self) -> None:
        names = set(zipfile.ZipFile(io.BytesIO(bundle.build("pingbus", REPO_ROOT))).namelist())
        expected = bus_modules(REPO_ROOT) - {"helpers/pingbus/bundle.py"}
        self.assertTrue(expected)
        self.assertLessEqual(expected, names)
        generated = {"__main__.py", "helpers/__init__.py", "helpers/pingbus/__init__.py",
                     "helpers/agent_bus/__init__.py"}
        self.assertEqual(names - expected, generated)

    def test_the_bundler_is_not_shipped(self) -> None:
        for app in ("pingbus", "agent-bus"):
            source = copy_source(self.tmp / app)
            (source / "helpers" / "agent_bus" / "cli.py").write_text("def main():\n    return 0\n")
            names = zipfile.ZipFile(io.BytesIO(bundle.build(app, source))).namelist()
            self.assertNotIn("helpers/pingbus/bundle.py", names)

    def test_a_symlinked_module_fails(self) -> None:
        source = copy_source(self.tmp / "src")
        outside = self.tmp / "outside.py"
        outside.write_text("Z = 3\n")
        (source / "helpers" / "pingbus" / "linked.py").symlink_to(outside)
        with self.assertRaisesRegex(bundle.BundleError, r"helpers/pingbus/linked\.py"):
            bundle.build("pingbus", source)

    def test_a_module_added_later_is_picked_up(self) -> None:
        source = copy_source(self.tmp / "src")
        (source / "helpers" / "pingbus" / "later_unit.py").write_text("X = 1\n")
        (source / "helpers" / "agent_bus" / "sub").mkdir()
        (source / "helpers" / "agent_bus" / "sub" / "deep.py").write_text("Y = 2\n")
        names = zipfile.ZipFile(io.BytesIO(bundle.build("pingbus", source))).namelist()
        self.assertIn("helpers/pingbus/later_unit.py", names)
        self.assertIn("helpers/agent_bus/sub/deep.py", names)

    def test_caches_and_non_modules_are_left_out(self) -> None:
        source = copy_source(self.tmp / "src")
        cache = source / "helpers" / "pingbus" / "__pycache__"
        cache.mkdir()
        (cache / "protocol.cpython-311.pyc").write_bytes(b"\0")
        (cache / "stray.py").write_text("")
        (source / "helpers" / "pingbus" / "notes.txt").write_text("x")
        (source / "helpers" / "pingbus" / ".protocol.py.swp").write_text("x")
        names = zipfile.ZipFile(io.BytesIO(bundle.build("pingbus", source))).namelist()
        self.assertFalse([name for name in names if "__pycache__" in name or not name.endswith(".py")])

    def test_entries_are_normalised(self) -> None:
        data = bundle.build("pingbus", REPO_ROOT)
        self.assertTrue(data.startswith(bundle.SHEBANG))
        archive = zipfile.ZipFile(io.BytesIO(data))
        names = archive.namelist()
        self.assertEqual(names, sorted(names))
        for info in archive.infolist():
            self.assertEqual(info.date_time, bundle.ZIP_EPOCH)
            self.assertEqual(info.external_attr, bundle.ENTRY_MODE << 16)
            self.assertEqual(info.compress_type, zipfile.ZIP_STORED)

    def test_main_module_calls_the_entry_point(self) -> None:
        archive = zipfile.ZipFile(io.BytesIO(bundle.build("pingbus", REPO_ROOT)))
        main = archive.read("__main__.py").decode()
        self.assertIn("from helpers.pingbus.cli import main", main)

    def test_missing_entry_module_fails(self) -> None:
        source = copy_source(self.tmp / "src")
        (source / "helpers" / "pingbus" / "cli.py").unlink()
        with self.assertRaisesRegex(bundle.BundleError, r"helpers/pingbus/cli\.py"):
            bundle.build("pingbus", source)

    def test_missing_package_fails(self) -> None:
        source = copy_source(self.tmp / "src")
        shutil.rmtree(source / "helpers" / "agent_bus")
        with self.assertRaisesRegex(bundle.BundleError, r"helpers/agent_bus"):
            bundle.build("pingbus", source)

    def test_unknown_app_fails(self) -> None:
        with self.assertRaises(bundle.BundleError):
            bundle.build("nope", REPO_ROOT)


class RunTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = pathlib.Path(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_pingbus_archive_runs_version(self) -> None:
        archive = self.tmp / "pingbus.pyz"
        archive.write_bytes(bundle.build("pingbus", REPO_ROOT))
        done = run_archive(archive, "version")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual(done.stdout, f"pingbus {cli.TOOL_VERSION} protocol {protocol.PROTOCOL_VERSION}\n")
        self.assertEqual(done.stderr, "")

    def test_archive_packages_take_no_modules_from_a_helpers_tree_elsewhere(self) -> None:
        """The archive's packages are regular packages: a `helpers` namespace portion
        later on the path cannot contribute modules to them."""
        shadow = self.tmp / "shadow"
        (shadow / "helpers" / "pingbus").mkdir(parents=True)
        (shadow / "helpers" / "pingbus" / "only_shadow.py").write_text("")
        archive = self.tmp / "pingbus.pyz"
        archive.write_bytes(bundle.build("pingbus", REPO_ROOT))
        probe = ("import sys\n"
                 f"sys.path[:0] = [{str(archive)!r}, {str(shadow)!r}]\n"
                 "try:\n"
                 "    import helpers.pingbus.only_shadow\n"
                 "except ModuleNotFoundError:\n"
                 "    print('isolated')\n"
                 "else:\n"
                 "    print('leaked')\n")
        with tempfile.TemporaryDirectory() as cwd:
            done = subprocess.run([sys.executable, "-I", "-c", probe], cwd=cwd,
                                  capture_output=True, text=True, check=False, timeout=60)
        self.assertEqual((done.returncode, done.stdout), (0, "isolated\n"), done.stderr)

    def test_agent_bus_archive_runs_its_entry_point(self) -> None:
        """Against a stand-in `helpers/agent_bus/cli.py` exposing `main()`, the interface
        U15 implements; the real one is covered below once it exists."""
        source = copy_source(self.tmp / "src")
        (source / "helpers" / "agent_bus" / "cli.py").write_text(
            "import sys\n\n\ndef main():\n    sys.stdout.write('agent-bus stand-in\\n')\n    return 0\n")
        archive = self.tmp / "agent-bus.pyz"
        archive.write_bytes(bundle.build("agent-bus", source))
        done = run_archive(archive, "version")
        self.assertEqual((done.returncode, done.stdout), (0, "agent-bus stand-in\n"), done.stderr)

    @unittest.skipUnless(AGENT_BUS_CLI.exists(), "helpers/agent_bus/cli.py is U15's")
    def test_real_agent_bus_archive_runs_version(self) -> None:
        archive = self.tmp / "agent-bus.pyz"
        archive.write_bytes(bundle.build("agent-bus", REPO_ROOT))
        done = run_archive(archive, "version")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertTrue(done.stdout.startswith("agent-bus "))


class MainTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = pathlib.Path(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def call(self, *argv: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = bundle.main(list(argv), stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def test_writes_executable_archive_then_reports_unchanged(self) -> None:
        target = self.tmp / "out" / "pingbus.pyz"
        (self.tmp / "out").mkdir()
        code, out, err = self.call("--source", str(REPO_ROOT), "--out", str(target), "pingbus")
        self.assertEqual((code, out, err), (0, f"{bundle.MARK_CHANGED} {target}\n", ""))
        self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o755)
        self.assertEqual(target.read_bytes(), bundle.build("pingbus", REPO_ROOT))
        inode = target.stat().st_ino
        code, out, err = self.call("--source", str(REPO_ROOT), "--out", str(target), "pingbus")
        self.assertEqual((code, out, err), (0, f"{bundle.MARK_UNCHANGED} {target}\n", ""))
        self.assertEqual(target.stat().st_ino, inode)

    def test_replaces_a_different_archive_and_fixes_its_mode(self) -> None:
        target = self.tmp / "pingbus.pyz"
        target.write_bytes(b"old")
        target.chmod(0o600)
        code, out, _ = self.call("--source", str(REPO_ROOT), "--out", str(target), "pingbus")
        self.assertEqual((code, out), (0, f"{bundle.MARK_CHANGED} {target}\n"))
        self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o755)
        self.assertEqual(sorted(p.name for p in self.tmp.iterdir()), ["pingbus.pyz"])

    def test_same_bytes_wrong_mode_is_a_change(self) -> None:
        target = self.tmp / "pingbus.pyz"
        target.write_bytes(bundle.build("pingbus", REPO_ROOT))
        target.chmod(0o644)
        _, out, _ = self.call("--source", str(REPO_ROOT), "--out", str(target), "pingbus")
        self.assertEqual(out, f"{bundle.MARK_CHANGED} {target}\n")
        self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o755)

    def test_build_error_exits_nonzero_on_stderr(self) -> None:
        target = self.tmp / "pingbus.pyz"
        code, out, err = self.call("--source", str(self.tmp), "--out", str(target), "pingbus")
        self.assertEqual((code, out), (1, ""))
        self.assertIn("helpers/pingbus", err)
        self.assertFalse(target.exists())

    def test_missing_output_directory_fails(self) -> None:
        target = self.tmp / "absent" / "pingbus.pyz"
        code, out, err = self.call("--source", str(REPO_ROOT), "--out", str(target), "pingbus")
        self.assertEqual((code, out), (1, ""))
        self.assertIn(str(target.parent), err)

    def test_unknown_app_is_a_usage_error(self) -> None:
        target = self.tmp / "x.pyz"
        with self.assertRaises(SystemExit) as raised, contextlib.redirect_stderr(io.StringIO()):
            bundle.main(["--source", str(REPO_ROOT), "--out", str(target), "nope"],
                        stdout=io.StringIO(), stderr=io.StringIO())
        self.assertEqual(raised.exception.code, 2)

    def test_runs_as_a_module(self) -> None:
        target = self.tmp / "pingbus.pyz"
        done = subprocess.run([sys.executable, "-m", "helpers.pingbus.bundle", "--source", str(REPO_ROOT),
                               "--out", str(target), "pingbus"], cwd=REPO_ROOT,
                              capture_output=True, text=True, check=False, timeout=60)
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual(done.stdout, f"{bundle.MARK_CHANGED} {target}\n")


if __name__ == "__main__":
    unittest.main()
