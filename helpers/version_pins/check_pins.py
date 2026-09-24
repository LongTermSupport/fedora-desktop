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
from typing import NamedTuple, TextIO

from helpers.host_health import probe_results
from helpers.version_pins import compare, manifest

#: Nothing the user must act on, and nothing printed.
EXIT_OK = 0
#: A pin drifted, or could not be checked. Both are the user's business.
EXIT_FINDINGS = 1

_TIMEOUT_SECONDS = 20


class ResolutionError(RuntimeError):
    """A value could not be read. Always becomes a finding, never a pass.

    Carries the probe's exit status and both streams, so a caller deciding what the
    failure MEANT discriminates on structure rather than on a substring of a merged
    blob — see `NotInstalled` for what that conflation costs.
    """

    def __init__(
        self,
        message: str,
        *,
        returncode: int | None = None,
        stdout: str = "",
        stderr: str = "",
    ) -> None:
        super().__init__(message)
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr


class NotInstalled(ResolutionError):
    """The command could not be resolved on **this process's PATH**, per the OS.

    A distinct type because "the OS refused to exec it" and "the binary ran, failed, and
    its output happened to contain *command not found*" are different facts, and a
    resolver matching on the message string cannot tell them apart. That matters more
    than it looks: an unresolvable install becomes `ABSENT`, which renders as *"pinned
    1.15.0, nothing installed"* — a confident claim about the host, made from a probe
    that broke.

    **The name is wider than the evidence, and the gap is real.** `FileNotFoundError`
    from `exec` says the name did not resolve on the PATH this process happens to have;
    it does not say the software is absent from the machine. The consumer here is a
    systemd `--user` unit, whose PATH is narrower than the login shell an operator would
    test in, so an installed tool can reach `ABSENT` by that route. `shutil.which` does
    **not** close it — measured: it consults the same PATH and returns None for the same
    input — and nothing cheap distinguishes the two, so the honest move is to claim only
    what was established and leave the rest to the caller.

    `probe.run_probe` answers the identical question with `ProbeOutcome.missing`, under
    the same limit.

    A subclass, so every `except ResolutionError` still catches it — the type is there
    to let a caller be MORE specific, never to let one escape.
    """


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
        # The OS said the binary is not there. That is the one absence this can
        # establish structurally, so it gets its own type rather than a phrase a
        # caller has to recognise.
        raise NotInstalled(f"{argv[0]}: command not found") from error
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
        raise ResolutionError(
            f"{argv[0]}: {detail}",
            returncode=completed.returncode,
            stdout=completed.stdout,
            stderr=completed.stderr,
        )
    return completed.stdout


class Coverage(NamedTuple):
    """What this host actually compared, in this host's numbers.

    Carried ALWAYS, including on a clean run. The coverage *finding* below fires only
    when something is wrong, which left a consumer with nothing to assert on when
    everything was right — so this plan's own acceptance gate asked the rendered
    document whether it contained the phrase `compared 0 of ` and called the absence of
    that phrase a real population. Two reachable states have no such phrase and no
    comparisons: a pin whose probe RAISED (the error finding suppresses the guard) and
    PARTIAL coverage (whose wording is `compared 1 of 2`). A grep for a sentence is not
    an assertion about a population, and this is the axis the incident happened on.
    """

    #: Every pin in the manifest, tracked or not.
    declared: int
    #: Tracked pins that APPLY to this host: the population it is held to.
    tracked: int
    #: Pins this host actually compared. The number that can differ per host.
    compared: int
    #: Tracked DKMS-resolved pins left out of `tracked`, because this host has no DKMS
    #: subsystem. Counted so the statement can say so, never folded into `compared`.
    not_applicable: int

    @property
    def is_complete(self) -> bool:
        """Whether this host held everything the repo tracks against the repo.

        `tracked == 0` is NOT complete. `0 of 0` satisfies `compared == tracked` while
        describing a host that compared nothing at all — a vacuous pass, which is the
        shape this whole plan exists to remove.
        """
        return self.tracked > 0 and self.compared == self.tracked

    @property
    def nothing_applies(self) -> bool:
        """The repo tracks pins, and none of them applies to this host."""
        return self.tracked == 0 and self.not_applicable > 0

    def sentence(self) -> str:
        """The numbers, in a form a consumer parses and a human reads.

        Both numbers are always present when anything applies. A consumer keys on them,
        so a reworded sentence that drops one breaks the gate — the test asserts this
        format for exactly that reason. When nothing applies it says so in words: `0 of
        0` is what a manifest tracking nothing looks like, and `N of N` is a comparison
        this host did not make.
        """
        if self.nothing_applies:
            return (f"no tracked pin applies on this host: {self.not_applicable} "
                    "DKMS-resolved, and this host has no DKMS subsystem")
        text = f"compared {self.compared} of {self.tracked} tracked pins"
        if self.not_applicable:
            text += (f"; {self.not_applicable} DKMS-resolved do not apply, as this host "
                     "has no DKMS subsystem")
        return text


class PinCheck(NamedTuple):
    """The findings and what they were drawn from — never one without the other."""

    findings: list[probe_results.Finding]
    coverage: Coverage


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
    """The findings alone, for callers that do not state coverage.

    A thin wrapper over `check_with_coverage` rather than a second walk of the pins:
    two implementations of this loop is how the duplicate-walk defects in this repo
    start, and a test pins the two entry points to the same answer.
    """
    return check_with_coverage(
        pins=pins, playbook_text=playbook_text, dkms_status=dkms_status,
        rpm_version=rpm_version, command_version=command_version,
        ran_plays=ran_plays, registry=registry,
    ).findings


def check_with_coverage(
    *,
    pins: list[manifest.Pin],
    playbook_text: Callable[[str], str],
    dkms_status: Callable[[], str],
    rpm_version: Callable[[str], str | None] | None = None,
    command_version: Callable[[str], str | None] | None = None,
    ran_plays: set[str] | None = None,
    registry: probe_results.DkmsRegistry | None = None,
) -> PinCheck:
    """One finding per pin that is not a clean MATCH, plus what was compared.

    `dkms_status` is called lazily and at most once, so a host with no DKMS modules
    and no tracked DKMS pin never pays for it — and, more to the point, never gets a
    finding about a probe it had no reason to run.

    **Zero coverage is itself a finding, and it is counted on this host.** A pin declared
    `untracked` with a reason is a decision somebody wrote down, so it stays silent and
    the QA gate prints the split. But if *nothing was compared*, this check returns no
    findings and is indistinguishable from a host whose every version matches — a whole
    drift axis gone quiet, on the axis the incident happened on. Partial coverage is a
    decision; zero coverage is a check that cannot fail, and it says so with the number.

    The count is taken after the loop rather than from the manifest, because those are
    two different numbers: the manifest's tracked count is the repo's intent, and a host
    can skip every one of them. See the comment at the guard.

    `ran_plays` and `registry` are the two things this host knows about itself.
    Neither narrows the population — a pin the ledger has never seen is still compared,
    because "installed 1.14.16 against a pinned 1.15.0" is drift whatever the ledger
    says. They act on one verdict and one resolver respectively; see the comments below.
    """
    findings: list[probe_results.Finding] = []
    dkms_cache: list[str] = []
    dkms_error: list[ResolutionError] = []

    manifest_tracked = sum(1 for pin in pins if pin.is_tracked)
    compared = 0
    not_applicable = 0

    def ran_here(pin: manifest.Pin) -> bool:
        """Has this host a ledger record for the play that installs this pin's software?

        `None` — the ledger could not be read — answers True: an open question must not
        buy silence, and the ledger's own brokenness is `ledger_presence`'s finding.
        """
        return ran_plays is None or pin.playbook in ran_plays

    def dkms() -> str:
        # The failure is remembered too, so the subsystem test below and the resolver
        # after it see one answer from one call.
        if dkms_error:
            raise dkms_error[0]
        if not dkms_cache:
            try:
                dkms_cache.append(dkms_status())
            except ResolutionError as error:
                dkms_error.append(error)
                raise
        return dkms_cache[0]

    def no_dkms_subsystem() -> bool:
        """The health probe's own test (DESIGN-server-route.md §5), and both halves.

        No state directory, so no registered module tree — `present is False`, and NOT
        an empty module list: the `dkms` rpm owns that directory, so a DisplayLink host
        whose module was removed has it, and that is the very state this axis reports.
        AND the OS could not find the command. Any other failure of the command
        establishes nothing, so it answers False and the resolver reports it.
        """
        if registry is None or registry.present is not False:
            return False
        try:
            dkms()
        except NotInstalled:
            return True
        except ResolutionError:
            return False
        return False

    for pin in pins:
        if not pin.is_tracked:
            continue
        try:
            # NOT APPLICABLE, the owner's decision. `dkms` is installed by two optional
            # desktop-hardware plays, so a stock server has no DKMS subsystem and a
            # DKMS-resolved pin describes nothing on it. It leaves the population this
            # host is held to, which is what keeps a clean server login silent.
            if pin.installed.kind == manifest.DKMS and no_dkms_subsystem():
                not_applicable += 1
                continue
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
        compared += 1
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

    # COVERAGE, COUNTED AFTER THE LOOP — because the manifest's number and this host's
    # number are different facts. A pin that does not apply leaves the population, so
    # `tracked` is what THIS host is held to, and a server whose every tracked pin is
    # DKMS-resolved is held to nothing and says so in its coverage statement.
    # `not findings` is the defect's own condition, not a convenience: a pin whose probe
    # raised already carries a line naming the error, so the axis is visibly unavailable
    # and a second sentence about coverage would only repeat it at every login. The
    # guard is for the case where the check returned NOTHING having compared too little.
    coverage = Coverage(declared=len(pins), tracked=manifest_tracked - not_applicable,
                        compared=compared, not_applicable=not_applicable)
    gap = coverage_gap(coverage)
    if pins and not findings and gap is not None:
        findings.append(probe_results.unchecked(gap))
    return PinCheck(findings=findings, coverage=coverage)


def coverage_gap(coverage: Coverage) -> str | None:
    """The coverage finding a host owes, or None when it owes none.

    None when everything that applies was compared, and when nothing applies at all:
    the owner's decision, so a clean server login is silent. Otherwise zero or PARTIAL
    coverage is stated in this host's numbers — a host that compared 1 of 2 returns one
    clean answer and no statement about the other, which renders `ok` exactly like one
    that compared both.

    "The repo tracks nothing" and "this host compared less than applies" are different
    facts and call for different actions — one is a decision to revisit in
    `vars/version-pins.yml`, the other a property of the machine.
    """
    if coverage.is_complete or coverage.nothing_applies:
        return None
    if coverage.tracked == 0:
        return (f"the installed-vs-pinned check compared 0 of {coverage.declared} "
                "declared pins, so nothing on this host was held against the repo's "
                "versions")
    return (f"the installed-vs-pinned check compared {coverage.compared} of "
            f"{coverage.tracked} tracked pins on this host")


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
    """The installed rpm's version, or None if the package is not installed.

    `rpm -q` exits non-zero and prints *"package X is not installed"* on **stdout** for
    an absent package, so absence has to be recognised from output — there is no
    FileNotFoundError to catch, since `rpm` itself is present.

    Narrowed to the stream rpm actually uses and to the package this call asked about.
    Matching a phrase anywhere in a merged stdout+stderr blob would let any tool that
    failed for another reason and echoed those words resolve to `ABSENT`, which renders
    as the confident claim *"pinned X, nothing installed"*.

    A fully structural answer exists — `rpm -q --quiet` exits 0/1 and prints nothing —
    at the cost of a second subprocess on the login path for a distinction no host in
    this repo currently exercises. Recorded rather than taken.
    """
    try:
        return _run(["rpm", "-q", "--queryformat", "%{VERSION}", package]).strip() or None
    except ResolutionError as error:
        if error.returncode and f"package {package} is not installed" in error.stdout:
            return None
        raise


def _command_version(command: str) -> str | None:
    """The command's reported version, or None when the command is not installed.

    The `None` branch is the counterpart of `_rpm_version`'s, and it matters for the
    same reason: without it a missing command raises, `check` catches it broadly, and
    the pin becomes a permanent *unchecked* finding on every host that never ran the
    play that installs it — the original noise, arriving through the one resolver the
    ABSENT scoping cannot reach, because it never gets as far as `classify`.

    Caught **by type**. Matching "command not found" in the message would also swallow a
    wrapper script that exists, ran, exited non-zero and printed that phrase about
    something inside itself — reporting a broken tool as an absent one, and rendering it
    as "pinned X, nothing installed".
    """
    try:
        return _run([command, "--version"]).strip() or None
    except NotInstalled:
        return None


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
