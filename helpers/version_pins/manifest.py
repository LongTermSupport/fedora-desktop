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
_OPTIONAL = ("github", "tag_prefix", "note", "installed")
_KNOWN = frozenset(_REQUIRED + _OPTIONAL)

#: Resolve the installed version from `dkms status`, by module name.
DKMS = "dkms"
#: Resolve it from the rpm database, by package name.
RPM = "rpm"
#: Run `<name> --version` and take the first version-shaped run out of it.
COMMAND = "command"
#: Nobody compares this pin against the host, and `why` records the decision.
#: Not an omission: a pin with no `installed:` block at all is rejected by the
#: host-side consumer, so "nobody decided" is not a reachable state.
UNTRACKED = "untracked"
_RESOLVER_KINDS = (DKMS, RPM, COMMAND)
_INSTALLED_KINDS = frozenset((*_RESOLVER_KINDS, UNTRACKED))
_INSTALLED_KEYS = frozenset(("kind", "name", "why"))

#: `owner/repo` and nothing else. A URL or a bare name would be handed to
#: `gh api repos/<value>/...` and fail as a query error, which reads as "upstream
#: is unreachable" rather than "this manifest row is wrong".
_GITHUB_RE = re.compile(r"^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$")

#: The row encoding's delimiters.
_FORBIDDEN = ("|", "\n", "\r")


class ManifestError(ValueError):
    """The manifest cannot be trusted. Never softened into a warning."""


class Installed(NamedTuple):
    kind: str
    #: The module, package or command to ask. Empty only for UNTRACKED.
    name: str
    #: Why this pin is not compared against the host. Required for UNTRACKED.
    why: str


class Pin(NamedTuple):
    playbook: str
    var: str
    #: Empty for a pin with no automated source — reported as MANUAL.
    github: str
    #: Tag prefix beyond a leading `v` to strip before comparing.
    tag_prefix: str
    #: Free text shown for a MANUAL row.
    note: str
    #: How to resolve what is installed here. None when undeclared, which only the
    #: upstream-drift consumer tolerates.
    installed: Installed | None = None

    @property
    def is_tracked(self) -> bool:
        """Whether this pin's install state is actually compared against the host."""
        return self.installed is not None and self.installed.kind in _RESOLVER_KINDS


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


def _installed(row: Mapping, *, where: str, required: bool) -> Installed | None:
    """The `installed:` block, or None when undeclared and that is allowed.

    Resolving what is installed cannot be generic, and guessing is worse than
    omitting: the `displaylink_version` pin is a release tag while the installed
    rpm's own version tracks evdi, so a plausible rpm resolver would report a
    permanent false finding. Hence the explicit UNTRACKED kind — a recorded
    decision, distinct from nobody having decided.
    """
    block = row.get("installed")
    if block is None:
        if required:
            raise ManifestError(
                f"{where}: no installed: block. Declare a resolver, or "
                f"`installed: {{kind: {UNTRACKED}, why: ...}}` — silence is not a decision"
            )
        return None
    if not isinstance(block, Mapping):
        raise ManifestError(f"{where}: installed must be a mapping, got {type(block).__name__}")

    unknown = sorted(set(block) - _INSTALLED_KEYS)
    if unknown:
        raise ManifestError(f"{where}: unknown installed key(s) {unknown}")

    kind = _field(block, "kind", where=f"{where}.installed")
    if kind not in _INSTALLED_KINDS:
        # Never defaulted to UNTRACKED: that would turn a typo into a decision
        # nobody made, and silently stop comparing a pin somebody meant to track.
        raise ManifestError(
            f"{where}: installed.kind must be one of {sorted(_INSTALLED_KINDS)}, got {kind!r}"
        )
    name = _field(block, "name", where=f"{where}.installed")
    why = _field(block, "why", where=f"{where}.installed")
    if kind == UNTRACKED and not why:
        raise ManifestError(f"{where}: installed.kind={UNTRACKED} needs a why: recording the decision")
    if kind != UNTRACKED and not name:
        raise ManifestError(f"{where}: installed.kind={kind} needs a name: to ask for")
    return Installed(kind=kind, name=name, why=why)


def parse(document: Mapping, *, require_installed: bool = False) -> list[Pin]:
    """Every declared pin, validated. Raises rather than dropping a bad row.

    Dropping one would shrink the population a review gate reports on, and a pin
    nobody looks at reads exactly like a pin that is up to date.

    `require_installed` is set by the host-side consumer, which cannot act on a pin
    whose install state nobody has decided about.
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
            installed=_installed(row, where=where, required=require_installed),
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
        # `installed` is required here, not just by the host consumer: this is the
        # only place every row is read on every qa-all run, so it is where a pin
        # nobody has decided about gets caught.
        pins = parse(json.load(source), require_installed=True)
    except (ManifestError, json.JSONDecodeError) as error:
        out.write(f"{INVALID_MARKER} {error}\n")
        return 1
    tracked = sum(pin.is_tracked for pin in pins)
    # The untracked count is printed, not hidden, so the gap stares back on every
    # run rather than reading as an absence of findings.
    out.write(
        f"{OK_MARKER} {len(pins)} pin(s), {tracked} with install state tracked, "
        f"{len(pins) - tracked} declared untracked\n"
    )
    for pin in pins:
        out.write(f"{to_row(pin)}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
