"""Pure version-selection logic for pyenv-managed CPython installs.

No side effects, no I/O, no third-party deps — every function takes plain text
(the output of `pyenv install --list` / `pyenv versions --bare`) and returns
data. This is what makes the behaviour unit-testable; install_pyenv_versions.py
is the thin wrapper that actually shells out to pyenv.

Why this exists at all: the selection logic used to live in an Ansible `shell:`
block, but Ansible 2.19's free-form argument parser is not bash-aware and chokes
on non-trivial scripts ("failed at splitting arguments..."). Moving the logic to
a tested Python helper sidesteps that whole class of breakage.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

# A bare, stable CPython version like "3.14.1". Deliberately rejects pre-releases
# (3.14.0a1/rc1), free-threaded builds (3.13.0t), and alternative interpreters
# (pypy*, *conda*, jython*) — none of those match this exact N.N.N shape.
_STABLE_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)$")

Version = tuple[int, int, int]
Minor = tuple[int, int]


@dataclass(frozen=True)
class InstallPlan:
    """The resolved work for one provisioning run.

    to_install: full version strings to `pyenv install -s`, newest-minor first.
    stale_kept: minor lines ("3.9") installed but outside the newest-N window —
                kept (never auto-removed) but worth flagging to the user.
    top_minors: the newest-N minor lines this run targets, for logging.
    """

    to_install: list[str]
    stale_kept: list[str]
    top_minors: list[str]


def parse_cpython_versions(listing: str) -> list[Version]:
    """Extract stable CPython versions from `pyenv install --list`, sorted ascending."""
    found: set[Version] = set()
    for raw in listing.splitlines():
        match = _STABLE_RE.match(raw.strip())
        if match:
            found.add((int(match.group(1)), int(match.group(2)), int(match.group(3))))
    return sorted(found)


def parse_installed_minors(bare: str) -> list[Minor]:
    """Extract the unique, sorted minor lines from `pyenv versions --bare`."""
    minors: set[Minor] = set()
    for raw in bare.splitlines():
        match = _STABLE_RE.match(raw.strip())
        if match:
            minors.add((int(match.group(1)), int(match.group(2))))
    return sorted(minors)


def _minor_of(version: Version) -> Minor:
    return version[0], version[1]


def newest_minors(versions: list[Version], count: int) -> list[Minor]:
    """The newest `count` distinct minor lines present in `versions`."""
    minors = sorted({_minor_of(v) for v in versions}, reverse=True)
    return minors[:count]


def latest_patch(versions: list[Version], minor: Minor) -> Version | None:
    """Highest patch release for `minor`, or None if that minor is unavailable."""
    candidates = [v for v in versions if _minor_of(v) == minor]
    return max(candidates) if candidates else None


def fmt(version: Version) -> str:
    return f"{version[0]}.{version[1]}.{version[2]}"


def fmt_minor(minor: Minor) -> str:
    return f"{minor[0]}.{minor[1]}"


def build_install_plan(listing: str, installed_bare: str, minor_count: int) -> InstallPlan:
    """Decide which CPython patches to install/upgrade and which installs are stale.

    Targets the newest `minor_count` minor lines plus every already-installed
    minor (so existing versions get patched even when they fall outside the
    window). Installed minors outside the window are reported as stale_kept but
    never removed.
    """
    if minor_count < 1:
        raise ValueError(f"minor_count must be >= 1, got {minor_count}")
    stable = parse_cpython_versions(listing)
    if not stable:
        raise ValueError("no stable CPython versions found in pyenv listing")

    top = newest_minors(stable, minor_count)
    installed = parse_installed_minors(installed_bare)

    # Union, newest-window first then any extra installed minors, order preserved.
    targets: list[Minor] = list(top)
    for minor in installed:
        if minor not in targets:
            targets.append(minor)

    to_install: list[str] = []
    for minor in targets:
        patch = latest_patch(stable, minor)
        if patch is not None:
            to_install.append(fmt(patch))

    stale_kept = [fmt_minor(m) for m in installed if m not in top]

    return InstallPlan(
        to_install=to_install,
        stale_kept=stale_kept,
        top_minors=[fmt_minor(m) for m in top],
    )
