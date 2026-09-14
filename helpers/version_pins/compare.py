"""Repo pin versus what is actually installed on this host (Plan 00109, Task 2.2).

The third drift axis. `check-pinned-versions.bash` compares the repo pin against
*upstream latest*; `qa-deployed-drift.bash` compares repo scripts against their
*deployed copies*. Neither asks whether the host has what the repo says it should,
and that is the axis a DKMS module drifted on — pinned a minor version ahead of
what was installed, every gate green, and the displays it drives dark after a
reboot.

Pure logic: the caller resolves what is installed (rpm, a binary's `--version`,
`dkms status`) and hands both strings in.

Design note that matters more than it looks: **nothing here has a "cannot tell"
that passes.** An unparseable version is `UNDETERMINED` and is a finding, because
the whole point of this axis is that a silent pass on it is what caused the
incident.
"""

from __future__ import annotations

import re
from typing import NamedTuple

#: Installed matches the pin. The only clean state.
MATCH = "match"
#: Installed is older than the pin — the incident's shape.
BEHIND = "behind"
#: Installed is newer than the pin. The repo can no longer reproduce this host.
AHEAD = "ahead"
#: The pin names something this host does not have at all.
ABSENT = "absent"
#: A version could not be read. A finding, never a pass.
UNDETERMINED = "undetermined"

# A dotted numeric run anywhere in the string. This tolerates the shapes the
# resolvers actually produce — `v1.15.0`, `1.14.16-1.fc44`, `evdi/1.14.16` —
# without each caller having to normalise first.
_VERSION_RE = re.compile(r"(\d+(?:\.\d+)*)")


class Verdict(NamedTuple):
    state: str
    detail: str

    @property
    def is_clean(self) -> bool:
        return self.state == MATCH


class Pin(NamedTuple):
    name: str
    pinned: str
    #: None when nothing is installed.
    installed: str | None


class Finding(NamedTuple):
    name: str
    verdict: Verdict


def parse_version(text: str) -> tuple[int, ...]:
    """A version string as a CANONICAL comparable tuple.

    Canonical, not literal: trailing zero components are dropped, so `1.15.0` and
    `1.15` parse identically and therefore compare as the same version. Keeping
    them would make `(1, 15) < (1, 15, 0)` — Python orders a shorter tuple first —
    and the check would report a host "behind" its pin over a formatting
    difference.

    Raises rather than returning a sentinel: a sentinel would compare equal to
    another sentinel, so two unreadable versions would report MATCH — a silent
    pass on exactly the axis this module exists to watch.
    """
    match = _VERSION_RE.search(text.strip())
    if match is None:
        raise ValueError(f"no version number found in {text!r}")
    parts = tuple(int(part) for part in match.group(1).split("."))
    # Trailing zeros carry no ordering information, and dropping them is what
    # makes "1.15" and "1.15.0" the same version rather than adjacent ones.
    while len(parts) > 1 and parts[-1] == 0:
        parts = parts[:-1]
    return parts


def classify(*, pinned: str, installed: str | None) -> Verdict:
    """How this host's installed version stands against the repo's pin."""
    if installed is None:
        return Verdict(ABSENT, f"pinned {pinned}, nothing installed")

    try:
        pinned_parts = parse_version(pinned)
    except ValueError as error:
        return Verdict(UNDETERMINED, f"pin is unreadable: {error}")
    try:
        installed_parts = parse_version(installed)
    except ValueError as error:
        return Verdict(UNDETERMINED, f"installed version is unreadable: {error}")

    detail = f"pinned {pinned}, installed {installed}"
    if installed_parts == pinned_parts:
        return Verdict(MATCH, detail)
    if installed_parts < pinned_parts:
        return Verdict(BEHIND, detail)
    return Verdict(AHEAD, detail)


def findings(pins: list[Pin]) -> list[Finding]:
    """Every pin that is not a clean MATCH, sorted by name.

    Sorted so two runs of the report diff cleanly rather than by input order.
    """
    results = [
        Finding(pin.name, classify(pinned=pin.pinned, installed=pin.installed))
        for pin in pins
    ]
    return sorted(
        (finding for finding in results if not finding.verdict.is_clean),
        key=lambda finding: finding.name,
    )
