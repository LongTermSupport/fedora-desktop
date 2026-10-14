"""Deployed Go commands must match the checkout they would be rebuilt from.

Every command under src-go/cmd is built by tasks/build-go-command.yml with the digest of its
source (helpers/agent_bus/installer_digest.py) baked in, and prints it for `--version`. This
check runs each installed command's `--version` and compares the answer with the digest of
the checkout, so a command a play has not rebuilt since a source change, including one that
runs as root from a timer, is a QA finding rather than a surprise. A command is checked only
when it is installed, so a host that never ran a play is not nagged about it.

The table, scripts/go-binary-installs.tsv, holds `command<TAB>installed path<TAB>play` per row
(`~/` is the login user's home). A new deployed command takes a row there and in
scripts/go-binary-suites.tsv; the tests of this module fail while either is missing.

Exit 0: every installed command matches (or none is installed); 1: drift; 2: the check could
not run (a bad table, a command the source does not have). Findings go to stderr.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import subprocess
import sys
from collections.abc import Sequence
from typing import TextIO

from helpers.agent_bus import installer_digest

TABLE = "scripts/go-binary-installs.tsv"
EXIT_OK, EXIT_DRIFT, EXIT_ERROR = 0, 1, 2
ANSWER_TIMEOUT_SECONDS = 30
COLUMNS = 3


class CheckError(Exception):
    """The check cannot run; the message names why."""


def read_table(path: pathlib.Path) -> dict[str, tuple[str, str]]:
    """command -> (installed path, the play that deploys it)."""
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise CheckError(f"cannot read {path}: {exc.strerror}") from None
    rows: dict[str, tuple[str, str]] = {}
    for number, line in enumerate(text.splitlines(), start=1):
        if not line or line.startswith("#"):
            continue
        fields = line.split("\t")
        if len(fields) != COLUMNS or not all(fields):
            raise CheckError(f"{path.name} line {number}: want command<TAB>installed path<TAB>play, got {line!r}")
        rows[fields[0]] = (fields[1], fields[2])
    return rows


def installed_digest(program: str) -> str | None:
    """What the installed command prints for --version, None when it cannot answer."""
    try:
        done = subprocess.run(
            [program, "--version"], capture_output=True, text=True, check=False,
            timeout=ANSWER_TIMEOUT_SECONDS, stdin=subprocess.DEVNULL)
    except (OSError, subprocess.TimeoutExpired):
        return None
    if done.returncode != 0:
        return None
    return done.stdout.strip()


def _expand(installed: str, home: str) -> str:
    return home.rstrip("/") + "/" + installed[2:] if installed.startswith("~/") else installed


def main(argv: Sequence[str] | None = None, *, stdout: TextIO | None = None,
         stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    parser = argparse.ArgumentParser(prog="go_drift.check", description=__doc__.split("\n\n")[0])
    parser.add_argument("--source", required=True, help="the fedora-desktop checkout")
    parser.add_argument("--home", default=os.path.expanduser("~"), help="what ~/ in the table means")
    args = parser.parse_args(argv)
    source = pathlib.Path(args.source)
    try:
        rows = read_table(source / TABLE)
        expected = {command: installer_digest.source_digest(source, command=command) for command in rows}
    except (CheckError, installer_digest.DigestError) as exc:
        err.write(f"go_drift: {exc}\n")
        return EXIT_ERROR
    checked = missing = 0
    drifted: list[str] = []
    for command, (installed, play) in rows.items():
        program = _expand(installed, args.home)
        if not os.path.isfile(program):
            missing += 1
            continue
        checked += 1
        answer = installed_digest(program)
        if answer == expected[command]:
            continue
        why = ("does not report a source digest (built before it did, or not runnable)" if answer is None
               else f"reports {answer[:12]}, the checkout is {expected[command][:12]}")
        drifted.append(f"  {command}: {why}\n    installed: {program}\n    rebuild it with:\n"
                       f"    ansible-playbook {play}\n")
    if drifted:
        err.write("✗ deployed-drift: Go commands built from older source than the checkout\n\n")
        err.write("\n".join(drifted))
        err.write(f"✗ deployed-drift: {len(drifted)} of {checked} deployed Go command(s) differ from the checkout\n")
        return EXIT_DRIFT
    out.write(f"✓ deployed-drift: {checked} deployed Go command(s) match the checkout; "
              f"{missing} not installed on this host\n")
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
