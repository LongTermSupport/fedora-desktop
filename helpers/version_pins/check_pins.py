"""Repo pin versus what is installed on this host (Plan 00109, Task 2.2).

The third drift axis, and the one the failure this plan exists for happened on:
`check-pinned-versions.bash` compares the pin against *upstream latest* and said
"up to date"; `qa-deployed-drift.bash` compares repo scripts against their deployed
copies and said "in sync". Neither asked whether the host has what the repo says it
should.

Three rules, each a way this could have been a check that cannot fail:

1. **A tracked pin whose version cannot be resolved is a finding.** That is what
   `compare.UNDETERMINED` is for, and the reason `parse_version` raises rather than
   returning a sentinel — two sentinels compare equal, which would report MATCH.
2. **An untracked pin is silent.** The decision not to compare it is recorded in
   `vars/version-pins.yml` with a reason, and counted by
   `scripts/qa-version-pins.bash` on every run. Repeating it at every login is how
   a report gets ignored.
3. **Nothing escapes as an exception.** This runs at the end of a login, so an
   uncaught error takes the whole surface down, and a user who sees nothing cannot
   tell that from a healthy host.

Run it: `python3 -m helpers.version_pins.check_pins` (host-side; needs `dkms`)

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from collections.abc import Callable
from typing import TextIO

from helpers.host_health import probe_results
from helpers.version_pins import compare, manifest

#: Nothing the user must act on, and nothing printed.
EXIT_OK = 0
#: A pin drifted, or could not be checked. Both are the user's business.
EXIT_FINDINGS = 1

_TIMEOUT_SECONDS = 20


class ResolutionError(RuntimeError):
    """A value could not be read. Always becomes a finding, never a pass."""


def pinned_value(playbook_text: str, var: str) -> str:
    """The pinned version as the playbook declares it.

    Raises on a var that is not there: returning None would let a renamed var read
    as an absent install, which is the wrong finding for the wrong reason.
    """
    pattern = re.compile(rf"^\s*{re.escape(var)}\s*:(.*)$", re.MULTILINE)
    match = pattern.search(playbook_text)
    if match is None:
        raise ResolutionError(f"no var {var!r} in the playbook")
    return match.group(1).strip().strip("\"'").strip()


def installed_from_dkms(dkms_text: str, module: str) -> str | None:
    """The version of `module` DKMS has here, or None if it has none.

    None is ABSENT — a real, reportable state, and the incident's own worst case.
    Unreadable output raises instead, because a line this cannot parse might be the
    module in question.

    The highest version wins when several are listed: a module built for several
    kernels appears once per build, and the question here is what this host has.
    Numerically, not lexically — as strings `1.14.16` sorts before `1.14.9`.
    """
    try:
        entries = probe_results.parse_dkms(dkms_text)
    except ValueError as error:
        raise ResolutionError(f"cannot read dkms status: {error}") from error

    versions = [entry.version for entry in entries if entry.module == module]
    if not versions:
        return None

    def sort_key(version: str) -> tuple[int, ...]:
        try:
            return compare.parse_version(version)
        except ValueError:
            return ()

    return max(versions, key=sort_key)


def _run(argv: list[str]) -> str:
    try:
        completed = subprocess.run(
            argv, capture_output=True, text=True, check=False, timeout=_TIMEOUT_SECONDS
        )
    except FileNotFoundError as error:
        raise ResolutionError(f"{argv[0]}: command not found") from error
    except subprocess.TimeoutExpired as error:
        raise ResolutionError(f"{argv[0]}: no answer after {_TIMEOUT_SECONDS}s") from error
    if completed.returncode != 0:
        detail = " ".join(completed.stderr.split()) or f"exit status {completed.returncode}"
        raise ResolutionError(f"{argv[0]}: {detail}")
    return completed.stdout


def check(
    *,
    pins: list[manifest.Pin],
    playbook_text: Callable[[str], str],
    dkms_status: Callable[[], str],
    rpm_version: Callable[[str], str | None] | None = None,
    command_version: Callable[[str], str | None] | None = None,
) -> list[str]:
    """One finding per pin that is not a clean MATCH. The probes are seams.

    `dkms_status` is called lazily and at most once, so a host with no DKMS modules
    and no tracked DKMS pin never pays for it — and, more to the point, never gets a
    finding about a probe it had no reason to run.
    """
    findings: list[str] = []
    dkms_cache: list[str] = []

    def dkms() -> str:
        if not dkms_cache:
            dkms_cache.append(dkms_status())
        return dkms_cache[0]

    for pin in pins:
        if not pin.is_tracked:
            continue
        try:
            pinned = pinned_value(playbook_text(pin.playbook), pin.var)
            kind = pin.installed.kind
            if kind == manifest.DKMS:
                installed = installed_from_dkms(dkms(), pin.installed.name)
            elif kind == manifest.RPM:
                resolver = rpm_version or _rpm_version
                installed = resolver(pin.installed.name)
            else:
                resolver = command_version or _command_version
                installed = resolver(pin.installed.name)
        except Exception as error:
            # Deliberately broad: the alternative is an exception escaping into a
            # login-time surface, where the user sees nothing at all and nothing
            # distinguishes that from a clean host.
            findings.append(f"{pin.var}: could not be checked — {error}")
            continue

        verdict = compare.classify(pinned=pinned, installed=installed)
        if not verdict.is_clean:
            findings.append(f"{pin.var} ({verdict.state}): {verdict.detail}")
    return findings


def _rpm_version(package: str) -> str | None:
    """The installed rpm's version, or None if the package is not installed."""
    try:
        return _run(["rpm", "-q", "--queryformat", "%{VERSION}", package]).strip() or None
    except ResolutionError as error:
        if "is not installed" in str(error):
            return None
        raise


def _command_version(command: str) -> str | None:
    return _run([command, "--version"]).strip() or None


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/version_pins/check_pins.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(argv: list[str] | None = None, *, stdout: TextIO | None = None) -> int:
    parser = argparse.ArgumentParser(description="Report repo-pin vs installed drift.")
    parser.add_argument("--repo-root", default=_repo_root_default())
    arguments = parser.parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    root = arguments.repo_root

    def read(relative: str) -> str:
        try:
            with open(os.path.join(root, relative), encoding="utf-8") as handle:
                return handle.read()
        except OSError as error:
            raise ResolutionError(str(error)) from error

    manifest_path = os.path.join(root, "vars", "version-pins.yml")
    # The YAML conversion lives outside the stdlib-only helpers, exactly as
    # scripts/qa-version-pins.bash does it.
    try:
        decoded = json.loads(
            _run([
                sys.executable, "-c",
                "import json,sys,yaml; json.dump(yaml.safe_load(open(sys.argv[1])), sys.stdout)",
                manifest_path,
            ])
        )
        pins = manifest.parse(decoded, require_installed=True)
    except (ResolutionError, ValueError, manifest.ManifestError) as error:
        out.write(f"the version-pin manifest could not be read: {error}\n")
        return EXIT_FINDINGS

    findings = check(pins=pins, playbook_text=read, dkms_status=lambda: _run(["dkms", "status"]))
    if not findings:
        return EXIT_OK
    for finding in findings:
        out.write(f"{finding}\n")
    return EXIT_FINDINGS


if __name__ == "__main__":
    raise SystemExit(main())
