"""Usage errors carry the program prefix once, and a stray text for a stdin command says so."""

import io
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli


def run(*argv: str) -> tuple[int, str]:
    err = io.StringIO()
    code = cli.main(list(argv), environ={}, stdout=io.StringIO(), stderr=err, stdin=io.BytesIO(b""))
    return code, err.getvalue()


class UsageErrorPrefix(unittest.TestCase):
    """Every usage error goes through the one Parser: the prefix appears once."""

    CASES = (
        ("--bogus",),
        ("nosuchcommand",),
        ("say", "--bogus"),
        ("say", "--to"),
        ("wait", "--timeout", "x"),
        ("history", "--room", "nope"),
        ("recv", "stray"),
        ("inbox", "stray"),
    )

    def test_prefix_once(self) -> None:
        for argv in self.CASES:
            with self.subTest(argv=argv):
                code, err = run(*argv)
                self.assertEqual(code, cli.EXIT_USAGE, err)
                self.assertTrue(err.startswith("pingbus: "), err)
                self.assertFalse(err.removeprefix("pingbus: ").startswith("pingbus: "), err)


class StdinCommandStrayText(unittest.TestCase):
    def test_say_names_stdin_and_shows_the_form(self) -> None:
        code, err = run("--team", "T", "say", "--to", "X", "some text")
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertEqual(
            err,
            "pingbus: say reads the message from stdin: "
            "printf '%s\\n' '...' | pingbus --team T say --to NAME\n",
        )

    def test_other_stdin_commands(self) -> None:
        for argv in (("report", "stray"), ("ask-human", "stray"), ("thread", "Q1", "stray")):
            command = argv[0]
            with self.subTest(command=command):
                code, err = run("--team", "T", *argv)
                self.assertEqual(code, cli.EXIT_USAGE, err)
                self.assertIn(f"pingbus: {command} reads the message from stdin", err)
                self.assertIn(f"| pingbus --team T {command}", err)
                self.assertNotIn("unrecognized", err)


if __name__ == "__main__":
    unittest.main()
