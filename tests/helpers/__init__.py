"""The helper tests' package. Importing any helper test module runs this first, and this removes
the agent-bus session's environment (PINGBUS_*, AGENT_BUS_*, CCY_SESSION_NAME) from the process,
so a test run by hand with `python3 -m unittest tests.helpers.<module>` is as hermetic as one run
by scripts/qa-helper-tests.bash. Why, and the list: helpers/qa_environment/session_env.py.

This is the one regular package among the helper trees; every directory below it stays a
namespace package with no __init__.py (helpers/CLAUDE.md).
"""

import os
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))

from helpers.qa_environment import session_env

session_env.scrub(os.environ)
