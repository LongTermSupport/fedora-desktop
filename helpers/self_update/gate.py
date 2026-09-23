"""The signed-tip trust gate's decisions (Plan 00137 Task 1.2, decision D3).

The server runs whatever this gate lets through as root, so push access to the branch
must not be enough. The owner's signature is the credential: the cycle deploys the
newest commit on the branch that carries a good signature from the pinned key, and that
signature vouches for everything between the deployed commit and it. The agents write
most commits and never hold the key, so their unsigned commits wait above it until the
owner signs a commit on top.

What each signature state means, as git 2.39 reports it for SSH signatures (measured,
not assumed — the probe is recorded in the plan's subagent report):

- `G` with the pinned principal: TRUSTED. The only state that can move the checkout.
- `N` (unsigned), `U` (a good signature from a key that is not pinned), `X`/`Y`
  (expired): UNTRUSTED. Not the owner, but not an attack either, so they are passed
  over and the walk continues.
- `B` (the bytes do not match the signature), `R` (revoked), `E` (the check itself
  could not run), or any letter this code does not know: REFUSE. Someone altered a
  signed commit, or the gate cannot tell — either way nothing is deployed.

Only SSH signatures are ever verified. A PGP or X.509 signature is UNTRUSTED without
being checked, because checking it would execute whatever `gpg.program` the
repository's config names.
"""

from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass

TRUSTED = "trusted"
UNTRUSTED = "untrusted"
REFUSE = "refuse"

KIND_NONE = "none"
KIND_SSH = "ssh"
KIND_OTHER = "other"

_UNTRUSTED_STATUSES = frozenset({"N", "U", "X", "Y"})


def signature_kind(raw_commit: str) -> str:
    """Which signature a raw commit object carries, read from its headers alone.

    Headers end at the first blank line. Anything after it is the message, so a message
    that quotes a signature block is not a signed commit.
    """
    headers = raw_commit.split("\n\n", 1)[0]
    for line in headers.split("\n"):
        if line.startswith("gpgsig ") or line.startswith("gpgsig-sha256 "):
            if line.split(" ", 1)[1].startswith("-----BEGIN SSH SIGNATURE-----"):
                return KIND_SSH
            return KIND_OTHER
    return KIND_NONE


def judge(status: str, signer: str, expected_principal: str) -> str:
    """TRUSTED, UNTRUSTED or REFUSE for one commit's `%G?` status and `%GS` signer."""
    if status == "G":
        if expected_principal and signer == expected_principal:
            return TRUSTED
        return UNTRUSTED
    if status in _UNTRUSTED_STATUSES:
        return UNTRUSTED
    return REFUSE


@dataclass(frozen=True)
class Choice:
    """The commit to deploy (or None), or the commit that refused the cycle."""

    target: str | None
    refused: str | None


def choose_target(candidates: Iterable[tuple[str, str]]) -> Choice:
    """The newest TRUSTED commit, walking `(sha, verdict)` pairs newest first.

    A REFUSE above the trusted commit refuses the whole cycle. One below it does not:
    the owner signed a descendant of it, and that signature covers the range. The walk
    stops at the first TRUSTED commit, so nothing older is examined.
    """
    for sha, verdict in candidates:
        if verdict == REFUSE:
            return Choice(target=None, refused=sha)
        if verdict == TRUSTED:
            return Choice(target=sha, refused=None)
    return Choice(target=None, refused=None)


_PIN_RE = re.compile(r"""^fedora_version:\s*["']?(\d+)["']?\s*$""", re.MULTILINE)
_VERSION_ID_RE = re.compile(r"""^VERSION_ID=["']?(\d+)""", re.MULTILINE)


def pinned_fedora_version(vars_text: str) -> int:
    """`fedora_version` from `vars/fedora-version.yml`, the pin the preflight play checks."""
    match = _PIN_RE.search(vars_text)
    if match is None:
        raise ValueError("vars/fedora-version.yml has no fedora_version line")
    return int(match.group(1))


def running_fedora_version(os_release_text: str) -> int:
    """The major version from os-release: what `distribution_major_version` reads."""
    match = _VERSION_ID_RE.search(os_release_text)
    if match is None:
        raise ValueError("os-release has no numeric VERSION_ID")
    return int(match.group(1))
