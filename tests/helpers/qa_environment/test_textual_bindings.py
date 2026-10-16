"""Unit tests for helpers/qa_environment/textual_bindings.py, the Python half of the rule
project_hidden_bindings: a Textual key binding whose action no visible control offers.

Run from the repo root:

    python3 -m unittest tests.helpers.qa_environment.test_textual_bindings
"""

from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import tempfile
import textwrap
import unittest

from helpers.qa_environment import textual_bindings

REPO = pathlib.Path(__file__).resolve().parents[3]


def judge(source: str) -> list[tuple[int, str]]:
    found = textual_bindings.judge_source(textwrap.dedent(source), "app.py")
    return [(f.line, f.message) for f in found]


class KeyOnlyActions(unittest.TestCase):
    def test_a_binding_with_no_button_is_a_finding(self):
        found = judge(
            """
            from textual.app import App
            from textual.binding import Binding

            class A(App):
                BINDINGS = [Binding("d", "download", "Download")]
            """
        )
        self.assertEqual(len(found), 1)
        self.assertEqual(found[0][0], 6)
        self.assertIn("d -> download", found[0][1])

    def test_a_tuple_binding_and_a_bind_call_are_judged(self):
        found = judge(
            """
            from textual.app import App

            class A(App):
                BINDINGS = [("r", "remove", "Remove")]

                def on_mount(self):
                    self.bind("x", "erase")
            """
        )
        self.assertEqual(
            [m.split(":")[0] for _, m in found],
            ["Textual binding r -> remove", "Textual binding x -> erase"],
        )

    def test_a_binding_paired_with_a_button_action_is_clean(self):
        self.assertEqual(
            judge(
                """
                from textual.app import App
                from textual.binding import Binding
                from textual.widgets import Button

                class A(App):
                    BINDINGS = [Binding("d", "download", "Download"), ("y", "screen.confirm", "Yes")]

                    def compose(self):
                        yield Button("Download (d)", action="app.download")
                        yield Button("Yes (y)", action="screen.confirm")
                """
            ),
            [],
        )

    def test_navigation_needs_no_button(self):
        self.assertEqual(
            judge(
                """
                from textual.app import App

                class A(App):
                    BINDINGS = [("/", "focus_filter", "Filter"), ("j", "cursor_down", ""), ("pgdn", "scroll_down", "")]
                """
            ),
            [],
        )

    def test_what_cannot_be_read_is_a_finding(self):
        found = judge(
            """
            from textual.app import App
            from textual.binding import Binding

            ACTION = "download"

            class A(App):
                BINDINGS = [Binding("d", ACTION), Binding(key="q"), "oops"]

            class B(App):
                BINDINGS = make_bindings()
            """
        )
        self.assertEqual(len(found), 4)
        self.assertTrue(all("cannot be read" in m for _, m in found))

    def test_a_button_built_at_run_time_pairs_nothing(self):
        found = judge(
            """
            from textual.app import App
            from textual.widgets import Button

            class A(App):
                BINDINGS = [("d", "download", "Download")]

                def compose(self):
                    yield Button("Download", action=self.which)
            """
        )
        self.assertEqual(len(found), 1)

    def test_a_file_that_does_not_import_textual_is_not_judged(self):
        self.assertEqual(judge('BINDINGS = [("d", "download", "Download")]\n'), [])


class Command(unittest.TestCase):
    def run_tool(self, *paths: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, "-m", "helpers.qa_environment.textual_bindings", *paths],
            cwd=REPO,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_the_report_is_json_with_every_file_counted(self):
        with tempfile.TemporaryDirectory() as tmp:
            bad = pathlib.Path(tmp, "bad.py")
            bad.write_text(
                "from textual.app import App\nclass A(App):\n    BINDINGS = [('d', 'download')]\n"
            )
            plain = pathlib.Path(tmp, "plain.py")
            plain.write_text("x = 1\n")
            res = self.run_tool(str(bad), str(plain))
        self.assertEqual(res.returncode, 0, res.stderr)
        report = json.loads(res.stdout)
        self.assertEqual(report["files"], 2)
        self.assertEqual(
            [(f["path"], f["line"]) for f in report["findings"]], [(str(bad), 3)]
        )

    def test_an_unparseable_file_is_exit_2(self):
        with tempfile.TemporaryDirectory() as tmp:
            broken = pathlib.Path(tmp, "broken.py")
            broken.write_text("def (:\n")
            res = self.run_tool(str(broken))
        self.assertEqual(res.returncode, 2)
        self.assertIn("broken.py", res.stderr)

    def test_no_file_is_exit_2(self):
        self.assertEqual(self.run_tool().returncode, 2)


if __name__ == "__main__":
    unittest.main()
