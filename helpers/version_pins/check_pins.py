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

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-version-pins.md
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
    """Run a probe and return its stdout, raising `ResolutionError` on any failure.

    `LC_ALL=C` because every caller parses this output, and a resolver that reads a
    translated message is a resolver that works on the developer's machine. It is set
    here rather than per-caller so no future probe has to remember.
    """
    try:
        completed = subprocess.run(
            argv,
            capture_output=True,
            text=True,
            check=False,
            timeout=_TIMEOUT_SECONDS,
            env={**os.environ, "LC_ALL": "C"},
        )
    except FileNotFoundError as error:
        raise ResolutionError(f"{argv[0]}: command not found") from error
    except subprocess.TimeoutExpired as error:
        raise ResolutionError(f"{argv[0]}: no answer after {_TIMEOUT_SECONDS}s") from error
    if completed.returncode != 0:
        # stdout as well as stderr. A failing tool does not necessarily report on
        # stderr — `rpm -q` prints "package X is not installed" to STDOUT and exits
        # non-zero — so an error built from stderr alone was empty for the one case
        # `_rpm_version` has to recognise, leaving its ABSENT branch unreachable and
        # an uninstalled package reported as "could not be checked".
        detail = (
            " ".join(f"{completed.stderr} {completed.stdout}".split())
            or f"exit status {completed.returncode}"
        )
        raise ResolutionError(f"{argv[0]}: {detail}")
    return completed.stdout


def check(
    *,
    pins: list[manifest.Pin],
    playbook_text: Callable[[str], str],
    dkms_status: Callable[[], str],
    rpm_version: Callable[[str], str | None] | None = None,
    command_version: Callable[[str], str | None] | None = None,
    ran_plays: set[str] | None = None,
    registry: probe_results.DkmsRegistry | None = None,
) -> list[probe_results.Finding]:
    """One finding per pin that is not a clean MATCH. The probes are seams.

    `dkms_status` is called lazily and at most once, so a host with no DKMS modules
    and no tracked DKMS pin never pays for it — and, more to the point, never gets a
    finding about a probe it had no reason to run.

    **Zero coverage is itself a finding.** A pin declared `untracked` with a reason is a
    decision somebody wrote down, so it stays silent and the QA gate prints the split.
    But if *nothing* is tracked, this check compares no pins, returns no findings, and
    is indistinguishable from a host whose every version matches — a whole drift axis
    gone quiet, on the axis the incident happened on. Partial coverage is a decision;
    zero coverage is a check that cannot fail, and it says so with the number.

    `ran_plays` and `dkms_registered` are the two things this host knows about itself.
    Neither narrows the population — a pin the ledger has never seen is still compared,
    because "installed 1.14.16 against a pinned 1.15.0" is drift whatever the ledger
    says. They act on one verdict and one resolver respectively; see the comments below.
    """
    findings: list[probe_results.Finding] = []
    dkms_cache: list[str] = []

    tracked = sum(1 for pin in pins if pin.is_tracked)
    if pins and tracked == 0:
        findings.append(
            probe_results.unchecked(
                f"the installed-vs-pinned check compared 0 of {len(pins)} declared pins, "
                "so nothing on this host was held against the repo's versions"
            )
        )

    def ran_here(pin: manifest.Pin) -> bool:
        """Has this host a ledger record for the play that installs this pin's software?

        `None` — the ledger could not be read — answers True: an open question must not
        buy silence, and the ledger's own brokenness is `ledger_presence`'s finding.
        """
        return ran_plays is None or pin.playbook in ran_plays

    def dkms() -> str:
        if not dkms_cache:
            dkms_cache.append(dkms_status())
        return dkms_cache[0]

    for pin in pins:
        if not pin.is_tracked:
            continue
        # NO DKMS SUBSYSTEM ON THIS HOST, so a DKMS-resolved pin is not answerable here
        # and that is an answer, not a failure to get one. `dkms` is installed by two
        # optional desktop-hardware plays, so on a stock server this is the whole reason
        # the check spoke at every login: `dkms()` raises "command not found" and every
        # DKMS pin became "could not be checked", for ever.
        #
        # `present is False` — no state directory at all — and NOT merely an empty
        # module list. The `dkms` rpm owns that directory, so a DisplayLink host whose
        # module has been removed has the directory and an empty registry, which is the
        # very state this axis exists to report; skipping on "no modules" would silence
        # it. `None` (could not read it) falls through and still reports.
        if pin.installed.kind == manifest.DKMS and registry is not None \
                and registry.present is False:
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
            # distinguishes that from a clean host. `unchecked`, because a pin whose
            # installed version could not be resolved says nothing about this host —
            # reading it as a fault names a problem nobody has established.
            findings.append(
                probe_results.unchecked(f"{pin.var}: could not be checked — {error}")
            )
            continue

        verdict = compare.classify(pinned=pinned, installed=installed)
        # ABSENT — "pinned X, nothing installed" — is the ONE verdict the ledger
        # disambiguates, and only it. On a host that ran the play, software that has
        # since vanished is a fault. On a host that never ran it, absence is exactly
        # what is expected, and reporting it is a permanent line nobody can act on.
        #
        # Scoped to ABSENT, never to the population. Filtering every pin by the ledger
        # silences the drift this whole plan exists to catch: with no backfill
        # (Task 1.3) a host has no `play-displaylink.yml` record until that play next
        # runs, so `evdi_version (behind)` would go unreported on every desktop.
        # BEHIND, AHEAD and UNDETERMINED all mean the software IS here and was
        # compared, so no ledger state can make them uninteresting. Task 1.3's rule was
        # derived for the freshness axis, where "never run here" is the whole question;
        # on this axis it is not. See DESIGN-server-route.md §5.
        if verdict.state == compare.ABSENT and not ran_here(pin):
            continue
        if not verdict.is_clean:
            findings.append(probe_results.broken(f"{pin.var} ({verdict.state}): {verdict.detail}"))
    return findings


def declared_pins(root: str) -> list[manifest.Pin]:
    """The pin manifest, parsed. The single route both consumers take.

    The YAML conversion lives outside the stdlib-only helpers, exactly as
    `scripts/qa-version-pins.bash` does it — and it goes through `_run`, so a failure
    carries the interpreter's own stderr. A second copy of this subprocess without
    that reported `returned non-zero exit status 1` and threw away the
    `ModuleNotFoundError` that said which module was missing: "could not run" with
    nothing after it tells the user precisely nothing.
    """
    document = _run([
        sys.executable, "-c",
        "import json,sys,yaml; json.dump(yaml.safe_load(open(sys.argv[1])), sys.stdout)",
        os.path.join(root, "vars", "version-pins.yml"),
    ])
    return manifest.parse(json.loads(document), require_installed=True)


def _rpm_version(package: str) -> str | None:
    """The installed rpm's version, or None if the package is not installed."""
    try:
        return _run(["rpm", "-q", "--queryformat", "%{VERSION}", package]).strip() or None
    except ResolutionError as error:
        if "is not installed" in str(error):
            return None
        raise


def _command_version(command: str) -> str | None:
    """The command's reported version, or None when the command is not installed.

    The `None` branch is the counterpart of `_rpm_version`'s, and it matters for the
    same reason: without it a missing command raises, `check` catches it broadly, and
    the pin becomes a permanent *unchecked* finding on every host that never ran the
    play that installs it — the original noise, arriving through the one resolver the
    ABSENT scoping cannot reach, because it never gets as far as `classify`.
    """
    try:
        return _run([command, "--version"]).strip() or None
    except ResolutionError as error:
        if "command not found" in str(error):
            return None
        raise


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

    try:
        pins = declared_pins(root)
    except (ResolutionError, ValueError, manifest.ManifestError) as error:
        out.write(f"the version-pin manifest could not be read: {error}\n")
        return EXIT_FINDINGS

    findings = check(pins=pins, playbook_text=read, dkms_status=lambda: _run(["dkms", "status"]))
    if not findings:
        return EXIT_OK
    for finding in findings:
        out.write(f"{finding.text}\n")
    return EXIT_FINDINGS


if __name__ == "__main__":
    raise SystemExit(main())
