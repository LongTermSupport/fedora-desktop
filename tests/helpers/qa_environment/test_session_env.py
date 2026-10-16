"""Tests for helpers/qa_environment/session_env.py and the helper tests' package scrub.

Run from the repo root:

    python3 -m unittest tests.helpers.qa_environment.test_session_env

An agent-bus session hands every process it starts PINGBUS_*, AGENT_BUS_* and CCY_SESSION_NAME.
A helper test that inherited them tested the session it ran in instead of the fixture it built:
`seat exec` refused PINGBUS_MEMBERSHIP beside the test's own PINGBUS_SEATS and exited 78. These
pin the list, the scrub, and that importing any helper test module removes them first.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.qa_environment import session_env

BOGUS = {
    "PINGBUS_HOME": "/nonexistent/bogus",
    "PINGBUS_TEAMS": "bogus-team",
    "PINGBUS_MEMBERSHIP": "/nonexistent/bogus",
    "PINGBUS_SEATS": "bogus@bogus",
    "PINGBUS_SOMETHING_NEW": "x",
    "AGENT_BUS_CLI": "/nonexistent/bogus",
    "CCY_SESSION_NAME": "bogus-session",
}
KEPT = {"PATH": "/usr/bin:/bin", "PINGBUS": "x", "XPINGBUS_HOME": "x", "CCY_SESSION_NAMES": "x",
        "CCY_LIFECYCLE_LAUNCH_ID": "x", "AGENT_BUSY": "x"}


class SessionNamesTest(unittest.TestCase):
    def test_every_pingbus_and_agent_bus_variable_and_the_session_name_are_named(self):
        self.assertEqual(session_env.session_names({**BOGUS, **KEPT}), sorted(BOGUS))

    def test_a_name_that_only_resembles_one_is_not(self):
        self.assertEqual(session_env.session_names(KEPT), [])


class ScrubTest(unittest.TestCase):
    def test_the_session_variables_are_removed_in_place_and_the_rest_kept(self):
        environ = {**BOGUS, **KEPT}
        self.assertEqual(session_env.scrub(environ), sorted(BOGUS))
        self.assertEqual(environ, KEPT)

    def test_a_clean_environment_is_left_alone(self):
        environ = dict(KEPT)
        self.assertEqual(session_env.scrub(environ), [])
        self.assertEqual(environ, KEPT)


class PackageScrubTest(unittest.TestCase):
    """The scrub that makes `python3 -m unittest tests.helpers.<module>` hermetic: importing any
    module under tests.helpers runs the package's __init__ first."""

    def seen_after_import(self, module: str) -> list[str]:
        probe = (f"import json, os, {module}; "
                 "print(json.dumps(sorted(k for k in os.environ if k in "
                 f"{sorted(BOGUS)!r})))")
        result = subprocess.run([sys.executable, "-c", probe], cwd=REPO_ROOT, env={**os.environ, **BOGUS},
                                capture_output=True, text=True, check=True)
        return json.loads(result.stdout)

    def test_importing_the_helper_tests_package_removes_the_session_variables(self):
        self.assertEqual(self.seen_after_import("tests.helpers"), [])

    def test_a_test_module_never_sees_them_even_at_its_own_import(self):
        # This module is under tests.helpers too: the package ran before its first line.
        self.assertEqual(self.seen_after_import("tests.helpers.qa_environment.test_session_env"), [])

    def test_this_process_has_none_of_them(self):
        self.assertEqual(session_env.session_names(os.environ), [])


if __name__ == "__main__":
    unittest.main()
