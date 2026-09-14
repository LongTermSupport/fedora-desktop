"""Validate the declared upstream version-pin manifest (Plan 00109, Task 2.2).

The manifest was a heredoc inside `scripts/check-pinned-versions.bash`, so a second
consumer could only copy it. It is `vars/version-pins.yml` now, and both read that.

Pure and stdlib-only: this takes the JSON that `scripts/qa-version-pins.bash`
converts the YAML into, because helpers cannot read YAML. The gate script owns the
conversion and the on-disk checks (does the playbook exist, does it still declare
the var); everything here is about the manifest's own shape.

Rows reach the bash consumer **pipe-delimited, one per line**, which is the reason
several of the rules below exist: a `|` or a newline inside a field does not fail,
it silently produces a different row. They are rejected rather than escaped, so the
manifest can never mean something other than it reads.

Read it: `python3 -m helpers.version_pins.manifest < manifest.json`
"""

from __future__ import annotations

import json
import re
import sys
from collections.abc import Mapping
from typing import NamedTuple, TextIO

#: Printed on success, followed by one pipe-delimited row per pin.
OK_MARKER = "VERSION-PINS-OK"
#: Printed on rejection. Deliberately shares no prefix with OK_MARKER — a gate
#: that greps for one must not match the other.
INVALID_MARKER = "VERSION-PINS-INVALID"

_TOP_KEY = "version_pins"
_REQUIRED = ("playbook", "var")
_OPTIONAL = ("github", "tag_prefix", "note")
_KNOWN = frozenset(_REQUIRED + _OPTIONAL)

#: `owner/repo` and nothing else. A URL or a bare name would be handed to
#: `gh api repos/<value>/...` and fail as a query error, which reads as "upstream
#: is unreachable" rather than "this manifest row is wrong".
_GITHUB_RE = re.compile(r"^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$")

#: The row encoding's delimiters.
_FORBIDDEN = ("|", "\n", "\r")


class ManifestError(ValueError):
    """The manifest cannot be trusted. Never softened into a warning."""


class Pin(NamedTuple):
    playbook: str
    var: str
    #: Empty for a pin with no automated source — reported as MANUAL.
    github: str
    #: Tag prefix beyond a leading `v` to strip before comparing.
    tag_prefix: str
    #: Free text shown for a MANUAL row.
    note: str


def _field(row: Mapping, key: str, *, where: str) -> str:
    value = row.get(key, "")
    if value is None:
        value = ""
    if not isinstance(value, str):
        raise ManifestError(f"{where}: {key} must be a string, got {type(value).__name__}")
    for character in _FORBIDDEN:
        if character in value:
            raise ManifestError(
                f"{where}: {key} contains {character!r}, which is a row delimiter — "
                "the consumer would read a different row than this one"
            )
    return value.strip()


def parse(document: Mapping) -> list[Pin]:
    """Every declared pin, validated. Raises rather than dropping a bad row.

    Dropping one would shrink the population a review gate reports on, and a pin
    nobody looks at reads exactly like a pin that is up to date.
    """
    if not isinstance(document, Mapping):
        raise ManifestError(f"the manifest must be a mapping, got {type(document).__name__}")
    rows = document.get(_TOP_KEY)
    if rows is None:
        raise ManifestError(f"no {_TOP_KEY}: key")
    if not isinstance(rows, list):
        raise ManifestError(f"{_TOP_KEY} must be a list, got {type(rows).__name__}")
    if not rows:
        # An empty manifest would validate, report nothing and exit 0 — a gate
        # that passes having checked nothing, which is this plan's whole subject.
        raise ManifestError(f"{_TOP_KEY} is empty; a manifest with no pins checks nothing")

    pins: list[Pin] = []
    seen: set[tuple[str, str]] = set()
    for index, row in enumerate(rows):
        where = f"{_TOP_KEY}[{index}]"
        if not isinstance(row, Mapping):
            raise ManifestError(f"{where}: each pin must be a mapping, got {type(row).__name__}")

        unknown = sorted(set(row) - _KNOWN)
        if unknown:
            # A typo'd key would otherwise be ignored, and `githug:` would quietly
            # demote an automated pin to a manual one that nobody queries.
            raise ManifestError(f"{where}: unknown key(s) {unknown}; known keys are {sorted(_KNOWN)}")

        pin = Pin(
            playbook=_field(row, "playbook", where=where),
            var=_field(row, "var", where=where),
            github=_field(row, "github", where=where),
            tag_prefix=_field(row, "tag_prefix", where=where),
            note=_field(row, "note", where=where),
        )
        for key in _REQUIRED:
            if not getattr(pin, key):
                raise ManifestError(f"{where}: {key} is required and must not be empty")
        if pin.github and not _GITHUB_RE.match(pin.github):
            raise ManifestError(f"{where}: github must be owner/repo, got {pin.github!r}")
        if not pin.github and not pin.note:
            # It would print "MANUAL:" with nothing after it: a review item a human
            # has been given no way to action.
            raise ManifestError(
                f"{where}: a pin with no github: needs a note: saying how to check it by hand"
            )

        key = (pin.playbook, pin.var)
        if key in seen:
            raise ManifestError(f"{where}: {pin.playbook} {pin.var} is declared twice")
        seen.add(key)
        pins.append(pin)
    return pins


def to_row(pin: Pin) -> str:
    """One pin in the five-field pipe encoding the bash consumer reads."""
    return "|".join((pin.playbook, pin.var, pin.github, pin.tag_prefix, pin.note))


def main(stdin: TextIO | None = None, stdout: TextIO | None = None) -> int:
    source = stdin if stdin is not None else sys.stdin
    out = stdout if stdout is not None else sys.stdout
    try:
        pins = parse(json.load(source))
    except (ManifestError, json.JSONDecodeError) as error:
        out.write(f"{INVALID_MARKER} {error}\n")
        return 1
    out.write(f"{OK_MARKER} {len(pins)} pin(s)\n")
    for pin in pins:
        out.write(f"{to_row(pin)}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
