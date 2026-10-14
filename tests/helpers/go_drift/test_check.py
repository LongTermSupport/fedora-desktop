"""The deployed-Go-command drift check: an installed command must report the digest of the
checkout it would be rebuilt from.

The commands here are stand-in executables (a shell script that prints a chosen line for
`--version`), because the check's subject is the answer, not Go.
"""

import io
import os
import pathlib
import stat
import tempfile
import unittest

from helpers.agent_bus import installer_digest
from helpers.go_drift import check

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
MODULE = "example.test/mod"
PLAY = "playbooks/imports/play-x.yml"


def write(path: pathlib.Path, text: str, *, mode: int = 0o644) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    path.chmod(mode)


class CheckTest(unittest.TestCase):
    def setUp(self) -> None:
        self.root = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.bin = self.root / "bin"
        write(self.root / "src-go/go.mod", f"module {MODULE}\n\ngo 1.26\n")
        for name in ("alpha", "beta"):
            write(self.root / f"src-go/cmd/{name}/main.go", f"package main\n\n// {name}\n")
        self.table = self.root / "scripts/go-binary-installs.tsv"

    def digest(self, command: str) -> str:
        return installer_digest.source_digest(self.root, command=command)

    def install(self, command: str, answer: str, *, exit_code: int = 0) -> pathlib.Path:
        path = self.bin / command
        write(path, f"#!/bin/sh\n[ \"$1\" = --version ] || exit 9\necho '{answer}'\nexit {exit_code}\n", mode=0o755)
        return path

    def rows(self, *commands: str) -> None:
        lines = "".join(f"{c}\t{self.bin / c}\t{PLAY}\n" for c in commands)
        write(self.table, "# header\n\n" + lines)

    def run_check(self) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = check.main(["--source", str(self.root)], stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def test_installed_commands_that_report_the_checkouts_digest_pass(self) -> None:
        self.rows("alpha", "beta")
        self.install("alpha", self.digest("alpha"))
        self.install("beta", self.digest("beta"))
        code, out, err = self.run_check()
        self.assertEqual((code, err), (0, ""))
        self.assertIn("2 deployed Go command(s) match the checkout", out)

    def test_a_command_built_from_older_source_is_drift_and_names_the_play(self) -> None:
        self.rows("alpha", "beta")
        self.install("alpha", self.digest("alpha"))
        self.install("beta", "0" * 64)
        code, out, err = self.run_check()
        self.assertEqual(code, 1)
        self.assertIn("beta", err)
        self.assertIn(f"ansible-playbook {PLAY}", err)
        self.assertNotIn("alpha", err)
        self.assertIn("1 of 2", err)
        self.assertEqual(out, "")

    def test_a_source_change_moves_the_expected_digest(self) -> None:
        self.rows("alpha")
        self.install("alpha", self.digest("alpha"))
        write(self.root / "src-go/cmd/alpha/main.go", "package main\n\n// changed\n")
        self.assertEqual(self.run_check()[0], 1)

    def test_a_deployed_command_that_cannot_report_a_digest_is_drift(self) -> None:
        self.rows("alpha")
        self.install("alpha", "usage: unknown option --version", exit_code=2)
        code, _, err = self.run_check()
        self.assertEqual(code, 1)
        self.assertIn("alpha", err)
        self.assertIn("does not report a source digest", err)

    def test_a_command_that_is_not_installed_is_counted_not_checked(self) -> None:
        self.rows("alpha", "beta")
        self.install("alpha", self.digest("alpha"))
        code, out, _ = self.run_check()
        self.assertEqual(code, 0)
        self.assertIn("1 deployed Go command(s) match the checkout", out)
        self.assertIn("1 not installed on this host", out)

    def test_a_home_relative_path_is_expanded(self) -> None:
        home = self.root / "home"
        write(self.table, f"alpha\t~/.local/bin/alpha\t{PLAY}\n")
        path = home / ".local/bin/alpha"
        write(path, f"#!/bin/sh\necho {self.digest('alpha')}\n", mode=0o755)
        out, err = io.StringIO(), io.StringIO()
        code = check.main(["--source", str(self.root), "--home", str(home)], stdout=out, stderr=err)
        self.assertEqual((code, err.getvalue()), (0, ""))

    def test_a_row_for_a_command_the_source_does_not_have_is_an_error(self) -> None:
        write(self.table, f"ghost\t{self.bin / 'ghost'}\t{PLAY}\n")
        self.install("ghost", "x")
        code, _, err = self.run_check()
        self.assertEqual(code, 2)
        self.assertIn("cmd/ghost", err)

    def test_a_malformed_row_is_an_error(self) -> None:
        write(self.table, "alpha only-two-columns\n")
        code, _, err = self.run_check()
        self.assertEqual(code, 2)
        self.assertIn("go-binary-installs.tsv", err)

    def test_a_missing_table_is_an_error(self) -> None:
        code, _, err = self.run_check()
        self.assertEqual(code, 2)
        self.assertIn("go-binary-installs.tsv", err)

    def test_a_file_that_is_not_executable_is_drift_not_a_crash(self) -> None:
        self.rows("alpha")
        path = self.install("alpha", self.digest("alpha"))
        path.chmod(path.stat().st_mode & ~(stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH))
        code, _, err = self.run_check()
        self.assertEqual(code, 1)
        self.assertIn("alpha", err)


class ThisCheckoutTest(unittest.TestCase):
    """The table and the sources agree: nothing deployed is left out, nothing named is missing."""

    def table(self) -> dict[str, tuple[str, str]]:
        return check.read_table(REPO_ROOT / check.TABLE)

    def test_every_command_of_the_module_has_a_row(self) -> None:
        commands = sorted(p.name for p in (REPO_ROOT / "src-go/cmd").iterdir() if p.is_dir())
        self.assertEqual(sorted(self.table()), commands)

    def test_every_rows_play_exists_and_builds_that_command(self) -> None:
        for command, (_, play) in self.table().items():
            text = (REPO_ROOT / play).read_text(encoding="utf-8")
            tool = "freeze_tool" if command in ("podfreeze", "lxcfreeze") else "go_command_name"
            if tool == "freeze_tool":
                self.assertIn(f"freeze_tool: {command}", text, command)
            else:
                self.assertIn(f"go_command_name: {command}", text, command)

    def test_every_commands_main_answers_version_with_a_source_digest(self) -> None:
        for command in self.table():
            main = (REPO_ROOT / f"src-go/cmd/{command}/main.go").read_text(encoding="utf-8")
            self.assertIn("var SourceDigest string", main, command)

    def test_the_real_table_parses_to_absolute_or_home_paths(self) -> None:
        for command, (path, _) in self.table().items():
            self.assertTrue(path.startswith(("/", "~/")), command)
        self.assertTrue(os.path.isdir(REPO_ROOT / "src-go/cmd"))


if __name__ == "__main__":
    unittest.main()
