"""Is an imported RPM signing key still able to verify what a repo now ships?

Issue #45, on a host upgraded from Fedora 41. Google's Linux signing key is ONE
primary key carrying a growing set of signing SUBKEYS, and the current Chrome
package is signed by a subkey added long after that key was first published.

Both `rpm` and `dnf` decide a key is "present" by its **primary** id. So a key
imported years ago is never refreshed: the primary matches, the import is skipped,
and the newer subkey never arrives. dnf5 then reports both halves of a
contradiction in one message, and both halves are true:

    Public key "…linux_signing_key.pub" is already present, not importing.
    OpenPGP check … has failed: Public key is not installed.

`rpm_key: state=present` cannot fix that — it is the module whose "already
present" check is the problem. The stale key has to be REMOVED and re-imported,
and doing that unconditionally would report `changed` on every run for ever. So
"is the installed key still sufficient?" has to be answered first, and answering
it is this module's whole job. The play does the removing.

**Three answers, not two.** `refresh` (our key is installed and is missing
something the published one has), `import` (it is not installed), and `none`. The
first two have different remedies — you cannot remove a key that is not there — so
collapsing them would fail the play on a clean host.

**The installed key is found by ID, never by description.** The play acts on a
`refresh` with `rpm -e`, so the set this module hands it has to be incapable of
naming a key nobody asked about. Everything here is anchored on the id of the
PUBLISHED key: the listing is filtered to packages rpm named after that id, and a
key sitting at that id whose primary turns out to be something else is reported as
`import` — our key is absent — rather than as a stale key to make room by deleting.

`gpg` is shelled out to rather than reimplemented: parsing OpenPGP packets in the
standard library to answer one question would be a great deal of code with its own
bugs, and `gpg` is already present wherever `rpm` is. Every subprocess call takes
an injected runner, so the decisions are tested without a real keyring.

Called by `playbooks/imports/play-browsers.yml`; design notes in
`CLAUDE/Plan/00124-chrome-install-gpg-failure-on-upgraded-host/PLAN.md`.
"""

from __future__ import annotations

import argparse
import subprocess
import sys
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Any, TextIO

Runner = Callable[..., "subprocess.CompletedProcess[str]"]

#: Marker lines the play parses. Stdout is the payload; diagnostics go to stderr.
#: `ACTION` is the instruction and `ENVELOPE` is what happens to be installed — a
#: play reading the envelope lines as the instruction would remove a working key.
ACTION_MARKER = "RPM-KEY-ACTION"
ENVELOPE_MARKER = "RPM-KEY-ENVELOPE"
REASON_MARKER = "RPM-KEY-REASON"

#: rpm's listing format — the package envelope alone. The summary is deliberately
#: not asked for: it is a description, and a description is not an identity.
_LISTING_FORMAT = "%{name}-%{version}-%{release}\n"

#: How many hex digits rpm uses to name a `gpg-pubkey` package.
_SHORT_ID_LENGTH = 8

#: gpg validity codes for a key that cannot sign anything now — expired, revoked,
#: invalid, disabled. Its absence from an installed copy explains no install failure.
_UNUSABLE_VALIDITY = frozenset({"e", "r", "i", "d"})


@dataclass(frozen=True)
class Verdict:
    """What the play should do about this key.

    `stale` is the erase list, and it is part of the verdict rather than something the
    caller assembles alongside it. Those were separate once, and the erase list then
    held every package found at the key's id while only the first had been identity-
    checked — so a package nobody had examined reached a destructive command.
    """

    refresh: bool
    import_missing: bool
    reason: str
    stale: tuple[str, ...] = ()

    @property
    def action(self) -> str:
        if self.refresh:
            return "refresh"
        if self.import_missing:
            return "import"
        return "none"


@dataclass(frozen=True)
class InstalledKey:
    """One `gpg-pubkey` package found at the published key's id, and what it holds."""

    envelope: str
    primary: str | None
    subkeys: frozenset[str]


def key_ids(colons: str) -> dict[str, Any]:
    """The FIRST certificate's primary id and its usable signing subkeys.

    From `gpg --show-keys --with-colons`, whose records are `type:validity:len:algo:
    keyid:created:expires:…:capabilities:` — fields 1, 2, 5 and 12 are read.

    **One certificate, not the whole file.** A key file can hold several, and a vendor
    rotating a primary ships exactly that: a transitional bundle. Reading the primary
    from the first certificate while accumulating subkeys from all of them attributes
    the second key's subkeys to the first, which no installed copy can ever hold — so
    every run reports a refresh, erases and re-imports, for ever. That is the precise
    churn this module exists to prevent, so parsing stops at the second `pub`.

    **Only subkeys that could sign this package.** Expired and revoked ones are
    skipped, as are subkeys with no signing capability. Five of Google's eight are
    expired; a host missing only those is not a host that cannot verify what the repo
    ships, and refreshing for them would be churn justified by a reason that is false.

    An absent primary is reported as `None` rather than as an empty string: gpg
    printing nothing means the key could not be read, and a key that could not be
    read is not a key with no subkeys. Those are different facts, and the caller
    refuses on the first.
    """
    primary: str | None = None
    subs: set[str] = set()
    for line in colons.splitlines():
        fields = line.split(":")
        if len(fields) < 12:
            continue
        if fields[0] == "pub":
            if primary is not None:
                break
            primary = fields[4]
        elif fields[0] == "sub" and primary is not None:
            if fields[1] in _UNUSABLE_VALIDITY or "s" not in fields[11]:
                continue
            subs.add(fields[4])
    return {"primary": primary, "subkeys": subs}


def short_id(primary: str) -> str:
    """rpm's name for a key: the last 8 hex digits of gpg's long id, lowercased.

    Raises on anything shorter. A truncated id would build a removal target that
    matches packages nobody meant to name, and guessing is the one thing this
    module must not do with an argument that ends up in `rpm -e`.
    """
    if len(primary) < _SHORT_ID_LENGTH:
        raise ValueError(
            f"{primary!r} is too short to be a key id, so no rpm package name can be "
            "derived from it"
        )
    return primary[-_SHORT_ID_LENGTH:].lower()


def read_key(path: str, *, run: Runner = subprocess.run) -> dict[str, Any]:
    """Parse the published armoured key at `path` into `key_ids` form."""
    result = run(
        ["gpg", "--show-keys", "--with-colons", path],
        check=True,
        capture_output=True,
        text=True,
    )
    return key_ids(result.stdout)


def read_armour(armour: str, *, run: Runner = subprocess.run) -> dict[str, Any]:
    """Parse armour held in memory.

    The installed key only ever exists as rpm's `%{description}`, so there is no
    file on disk to hand gpg.
    """
    result = run(
        ["gpg", "--show-keys", "--with-colons", "-"],
        check=True,
        capture_output=True,
        text=True,
        input=armour,
    )
    return key_ids(result.stdout)


def installed_envelopes(key_id: str, *, run: Runner = subprocess.run) -> list[str]:
    """Every installed `gpg-pubkey` package rpm named after `key_id`.

    The match is anchored on the trailing separator, so `d38b4796` does not also
    claim `d38b4796ab`: these names become `rpm -e` arguments, and a prefix match
    would hand the play a package it was never asked about.

    EVERY match, not the first: an upgraded host accumulates stale duplicates, and
    leaving one behind would leave the same unusable key in the keyring after the
    refresh. An empty list means rpm answered and nothing matched — a normal state
    the caller handles by importing. A FAILING `rpm` is left to raise: "rpm could
    not tell me" and "there is no such key" are different facts, and reporting the
    first as the second would re-import a key on every run for ever.
    """
    listing = run(
        ["rpm", "-qa", "gpg-pubkey", "--qf", _LISTING_FORMAT],
        check=True,
        capture_output=True,
        text=True,
    )
    prefix = f"gpg-pubkey-{key_id.lower()}-"
    return [line.strip() for line in listing.stdout.splitlines() if line.lower().startswith(prefix)]


def key_armour(envelope: str, *, run: Runner = subprocess.run) -> str:
    """The armoured public key rpm stores as one `gpg-pubkey` package's description."""
    result = run(
        ["rpm", "-q", envelope, "--qf", "%{description}"],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout


def needs_refresh(
    *, installed: Sequence[InstalledKey], published: dict[str, Any]
) -> Verdict:
    """Decide what to do, given EVERY package found at the published key's id.

    It takes the whole list rather than one key because the erase set is its output,
    and identity-checking one package while erasing all of them puts an unexamined
    package under a destructive command. Each is matched against the published primary
    here, and only those that match can ever appear in `stale`.

    Raises when the PUBLISHED key could not be read. That is not a state to hold an
    opinion from: it is not evidence the installed key is fine, and answering
    "nothing to do" there would be a confident claim sourced from a probe that
    failed — leaving a host unable to install anything, with a green run to show
    for it.
    """
    if not published["primary"]:
        raise ValueError(
            "the published key could not be read, so whether the installed key is "
            "still sufficient cannot be determined; refusing to answer"
        )

    # Short ids are 8 hex digits and are not unique, so a package sitting at this id
    # with a different primary belongs to somebody else. It is not ours to remove, and
    # it is not evidence ours is present either.
    ours = [key for key in installed if key.primary == published["primary"]]
    strangers = [key for key in installed if key.primary != published["primary"]]

    if not ours:
        if strangers:
            found = ", ".join(f"{k.envelope} ({k.primary})" for k in strangers)
            return Verdict(
                refresh=False,
                import_missing=True,
                reason=(
                    f"no key with the published primary {published['primary']} is "
                    f"installed; what sits at this id is {found}, which is not ours "
                    "to remove"
                ),
            )
        return Verdict(
            refresh=False,
            import_missing=True,
            reason="no matching key is installed; it needs importing, not refreshing",
        )

    # Only the deficient copies. A current one alongside them is not a problem, and
    # erasing it would be churn carried out with a destructive verb.
    deficient = [key for key in ours if published["subkeys"] - key.subkeys]
    if deficient:
        missing = sorted({sub for key in deficient for sub in published["subkeys"] - key.subkeys})
        return Verdict(
            refresh=True,
            import_missing=False,
            reason=(
                f"the installed key is missing signing subkey(s) {', '.join(missing)}, "
                "so it cannot verify what the repo now ships"
            ),
            stale=tuple(key.envelope for key in deficient),
        )

    # An installed key holding MORE than the published file is not a problem:
    # nothing needed for verification is absent. That happens mid-rotation, and
    # treating it as drift would remove a working key to install a smaller one.
    return Verdict(refresh=False, import_missing=False, reason="the installed key is current")


def report(*, verdict: Verdict, stdout: TextIO) -> None:
    """Write the marker lines the play parses.

    The envelope lines come from the verdict's own erase set, which is empty on every
    action but `refresh`. Nothing here re-derives which packages to name — that
    decision belongs to `needs_refresh`, which is the only thing that checked their
    identity.
    """
    stdout.write(f"{ACTION_MARKER} {verdict.action}\n")
    for envelope in verdict.stale:
        stdout.write(f"{ENVELOPE_MARKER} {envelope}\n")
    stdout.write(f"{REASON_MARKER} {verdict.reason}\n")


def main(
    argv: list[str] | None = None,
    *,
    run: Runner = subprocess.run,
    stdout: TextIO | None = None,
) -> int:
    parser = argparse.ArgumentParser(description="Decide whether an RPM signing key is stale.")
    parser.add_argument("--published", required=True, help="path to the published armoured key")
    arguments = parser.parse_args(argv)

    published = read_key(arguments.published, run=run)
    if not published["primary"]:
        raise ValueError(
            f"no OpenPGP key could be read from {arguments.published}, so whether the "
            "installed key is still sufficient cannot be determined"
        )

    # EVERY envelope is read, not just the first. The verdict's erase set becomes an
    # argument to `rpm --erase`, so a package that was never opened must not be able
    # to reach it — and an upgraded host holding two packages at one id is the
    # expected state, not an exotic one.
    installed = []
    for envelope in installed_envelopes(short_id(published["primary"]), run=run):
        ids = read_armour(key_armour(envelope, run=run), run=run)
        installed.append(
            InstalledKey(
                envelope=envelope,
                primary=ids["primary"],
                subkeys=frozenset(ids["subkeys"]),
            )
        )

    verdict = needs_refresh(installed=installed, published=published)
    report(verdict=verdict, stdout=stdout or sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
