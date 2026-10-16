"""The environment an agent-bus session hands its processes, which no helper test may inherit.

A ccy session on the agent bus (Plan 00203 M2 on) carries PINGBUS_HOME, PINGBUS_TEAMS and
PINGBUS_MEMBERSHIP, and the launcher and bus add more under the same prefixes. A helper test
that passes `os.environ` on to a subprocess then tests the session it happens to run in instead
of the fixture it built: `seat exec` refuses PINGBUS_MEMBERSHIP beside the test's own
PINGBUS_SEATS and exits 78, so the suite is red in every bus session and green everywhere else.

Two places remove these, both through `scrub`: `tests/helpers/__init__.py`, which runs before any
helper test module (so `python3 -m unittest tests.helpers.<module>` is hermetic too), and the
runner `unittest_counts.py`, which also refuses a run that leaves one behind. A test that needs
one sets it itself, in the `env=` it passes or with `mock.patch.dict`.
"""

from __future__ import annotations

from collections.abc import Mapping, MutableMapping

SESSION_PREFIXES = ("PINGBUS_", "AGENT_BUS_")
SESSION_NAMES = frozenset({"CCY_SESSION_NAME"})


def session_names(environ: Mapping[str, str]) -> list[str]:
    """The session variables present in `environ`, sorted."""
    return sorted(name for name in environ if name.startswith(SESSION_PREFIXES) or name in SESSION_NAMES)


def scrub(environ: MutableMapping[str, str]) -> list[str]:
    """Remove the session variables from `environ` in place; return the names removed."""
    names = session_names(environ)
    for name in names:
        del environ[name]
    return names
