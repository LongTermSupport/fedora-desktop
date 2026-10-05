"""The signed-tip trust gate's decisions (Plan 00137 Task 1.2, decision D3).

The server runs whatever this gate lets through as root, so push access to the branch
must not be enough. A signature from one of the owner's listed keys is the credential:
the cycle deploys the newest commit on the branch that carries a good signature from a
listed key, and that signature vouches for everything between the deployed commit and
it. The owner's own ccy sessions sign with the session's account key (Plan 00139 D4,
D5), so a listed account key lets their commits through; a commit signed by any other
key, or unsigned, waits above it until a listed key signs a commit on top.

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


_RELEASE_RE = re.compile(r"([0-9]+)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")


def parse_release_tag(name: str, major: int) -> tuple[int, int, int] | None:
    """`(major, minor, patch)` when `name` is exactly a release tag of `major`, else None.

    Plain decimal with no leading zeros, nothing before or after: `44.1.0-rc1`, `44.01.0`
    and `v44.1.0` are the owner's drafts or typos, not releases.
    """
    match = _RELEASE_RE.fullmatch(name)
    if match is None or match.group(1) != str(major):
        return None
    return int(match.group(1)), int(match.group(2)), int(match.group(3))


@dataclass(frozen=True)
class ReleaseChoice:
    """The newest release tag (or None), and the names that were passed over as not releases."""

    tag: str | None
    ignored: tuple[str, ...]


def choose_release(names: Iterable[str], major: int) -> ReleaseChoice:
    """The highest release tag of `major` by NUMERIC version (`44.10.0` beats `44.9.0`)."""
    best: tuple[tuple[int, int, int], str] | None = None
    ignored: list[str] = []
    for name in names:
        version = parse_release_tag(name, major)
        if version is None:
            ignored.append(name)
        elif best is None or version > best[0]:
            best = (version, name)
    return ReleaseChoice(tag=best[1] if best else None, ignored=tuple(ignored))


_BRANCH_MAJOR_RE = re.compile(r"F([1-9][0-9]*)")
_GOOD_TAG_RE = re.compile(r'^Good "git" signature for (\S+) with ', re.MULTILINE)


def branch_major(branch: str) -> int | None:
    """The Fedora major a release branch carries (`F44` gives 44), or None for any other name."""
    match = _BRANCH_MAJOR_RE.fullmatch(branch)
    return int(match.group(1)) if match else None


def tag_signer(verify_output: str) -> str | None:
    """The principal in `git verify-tag --raw`'s good-signature line, or None when it names none."""
    match = _GOOD_TAG_RE.search(verify_output)
    return match.group(1) if match else None


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
