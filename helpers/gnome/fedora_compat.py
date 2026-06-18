"""Pure logic: does an extension's metadata cover the GNOME Shell this branch ships?

Static, session-free counterpart to extension_state.py. extension_state.py
decides a *runtime* verdict from a live `gnome-extensions info` State and so needs
a GNOME session; this module decides a *static* verdict from two file facts only —
the branch's Fedora version (vars/fedora-version.yml) and an extension's
metadata.json `shell-version` list — so it runs in CI with no session, no D-Bus.

The check it encodes: the Fedora release this branch targets ships a specific
GNOME Shell major; the extension's metadata must declare support for that major,
or GNOME flags it "out of date" and refuses to load. That is the exact drift this
gate exists to catch before it reaches a user's desktop.

Why an explicit Fedora→GNOME table and not arithmetic: recent releases happen to
follow Fedora N → GNOME N+6 (F40→46 … F44→50), but that offset is a numbering
coincidence GNOME does not guarantee. An explicit table fails fast on an unknown
Fedora version, which forces a human to confirm the GNOME major the moment a new
F<N> branch is cut — exactly when it must be verified — instead of trusting
arithmetic that may silently drift. Add the new release here when branching.
"""

from __future__ import annotations

import enum
from dataclasses import dataclass

# Fedora release → the GNOME Shell major it ships. Extend when cutting a new
# F<N> branch (confirm the GNOME major against the Fedora release notes first).
FEDORA_TO_GNOME_MAJOR: dict[int, int] = {
    40: 46,
    41: 47,
    42: 48,
    43: 49,
    44: 50,
}


class Verdict(enum.Enum):
    OK = "ok"
    FAIL_UNCOVERED = "fail_uncovered"
    FAIL_UNKNOWN_FEDORA = "fail_unknown_fedora"


@dataclass(frozen=True)
class Classification:
    verdict: Verdict
    message: str

    @property
    def is_failure(self) -> bool:
        return self.verdict in (Verdict.FAIL_UNCOVERED, Verdict.FAIL_UNKNOWN_FEDORA)

    @property
    def exit_code(self) -> int:
        return 1 if self.is_failure else 0


def gnome_major_for_fedora(fedora_version: int) -> int | None:
    """GNOME Shell major for a Fedora release, or None if the release is unmapped."""
    return FEDORA_TO_GNOME_MAJOR.get(fedora_version)


def classify(
    *,
    uuid: str,
    fedora_version: int,
    metadata_shell_versions: list,
) -> Classification:
    """Decide whether one extension's metadata covers this branch's GNOME Shell."""
    gnome_major = gnome_major_for_fedora(fedora_version)

    if gnome_major is None:
        known = ", ".join(str(v) for v in sorted(FEDORA_TO_GNOME_MAJOR))
        return Classification(
            Verdict.FAIL_UNKNOWN_FEDORA,
            f"{uuid}: Fedora {fedora_version} is not in the Fedora→GNOME map "
            f"(known: {known}). Add its GNOME Shell major to "
            "FEDORA_TO_GNOME_MAJOR in helpers/gnome/fedora_compat.py.",
        )

    # metadata.json may list ints or strings; normalise both sides to strings.
    declared = {str(v) for v in metadata_shell_versions}
    if str(gnome_major) in declared:
        return Classification(
            Verdict.OK,
            f"{uuid}: Fedora {fedora_version} ships GNOME Shell {gnome_major}, "
            f"which metadata.json shell-version {metadata_shell_versions} covers.",
        )

    return Classification(
        Verdict.FAIL_UNCOVERED,
        f"{uuid}: Fedora {fedora_version} ships GNOME Shell {gnome_major}, but "
        f"metadata.json shell-version {metadata_shell_versions} does NOT list it. "
        f'Add "{gnome_major}" to the extension\'s metadata.json shell-version.',
    )
