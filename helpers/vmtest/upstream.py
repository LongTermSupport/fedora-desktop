"""Pure parsing of the Fedora upstream freshness signals (Plan 00110, DESIGN.md §4).

No side effects, no I/O, no network, no third-party deps — every function here
takes the TEXT of an upstream document and returns data. probe_upstream.py is
the thin executor that actually fetches. That split is what makes the policy
testable offline, and it is why the unit tests need no fixtures on disk.

Two values come out of here, and they are NOT interchangeable (§4.3):

    artefact_identity  — "is the base built from the currently published media?"
                         Drives `reinstall`.
    package_revision   — "are the packages layered on top of it current?"
                         Drives `refresh`. This is parse_repomd_revision().

Pooling them would let a package update trigger a full reinstall, or a media
change be papered over by a TTL. They are kept apart deliberately.
"""

from __future__ import annotations

import configparser
import hashlib
import json
import posixpath
import re
from collections.abc import Mapping
from dataclasses import dataclass
from urllib.parse import urlsplit

# Bump when the canonicalisation below changes shape. A stored identity carries
# no schema of its own, so without this a change to what gets hashed would read
# as "the media changed" — a spurious reinstall that looks exactly like a real
# one. With it, the mismatch is still a reinstall, but the reason is legible.
IDENTITY_SCHEMA = "vmtest-artefact-identity-v1"

# The kinds of base, per §3.5. `fast` is imported from a published qcow2 and
# never runs Anaconda, so it has no install tree; `full` is an Anaconda install
# and always has one. The mapping is enforced, not assumed — see _validate().
KIND_FAST = "fast"
KIND_FULL = "full"
_KINDS_WITH_TREE = frozenset({KIND_FULL})
_KINDS = frozenset({KIND_FAST, KIND_FULL})

_SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
_COMPOSE_ID_RE = re.compile(r"^Fedora-[A-Za-z0-9][A-Za-z0-9._-]*$")
_REVISION_RE = re.compile(r"<revision>\s*([^<]*?)\s*</revision>")

# The compose label as it appears in a published filename: a version and a
# respin, e.g. the "44-1.7" in Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2 and
# in Fedora-Everything-netinst-x86_64-44-1.7.iso. The arch sits on either side of
# it depending on the artefact, so the label is matched on its own shape rather
# than by counting hyphen-separated fields.
_COMPOSE_LABEL_RE = re.compile(r"-(\d+-\d+\.\d+)(?:\.|$)")


class UpstreamParseError(ValueError):
    """An upstream document was absent, truncated, or not the document we expect.

    Everything here raises rather than returning a sentinel. §4.4 makes an
    unreadable artefact identity block the lab outright, and that only works if
    "could not read it" is impossible to confuse with a value.
    """


class _PathPreservingParser(configparser.ConfigParser):
    """A ConfigParser that does not fold option-name case.

    `.treeinfo`'s `[checksums]` option names are artefact PATHS. The default
    `optionxform` lowercases them, silently rewriting the very strings the media
    identity is built from.
    """

    def optionxform(self, optionstr: str) -> str:
        return optionstr


@dataclass(frozen=True)
class ArtefactRef:
    """One published artefact a base was built from: its filename and sha256."""

    name: str
    sha256: str


@dataclass(frozen=True)
class ReleaseArtefact:
    """One entry from releases.json, keyed elsewhere by `filename`.

    `version`, `arch`, `variant` and `subvariant` are releases.json's own
    structured fields, and they are how a base's artefact is selected: the
    filename embeds a compose label (`44-1.7`) that is not known until the
    document is read, so selecting by name would mean guessing the label.
    """

    filename: str
    link: str
    sha256: str
    size: int
    version: str
    arch: str
    variant: str
    subvariant: str


@dataclass(frozen=True)
class ArtefactSelector:
    """How a base names the published artefact it is built from (§4.3).

    Matched against `ReleaseArtefact.variant`/`subvariant` plus a filename
    prefix and suffix, for the branch's Fedora version and architecture. The
    prefix is needed because siblings can share every structured field:
    measured live, the Server DVD and the Server netinst are both
    `variant=Server subvariant=Server` `.iso`. Exactly one entry must match.
    """

    variant: str
    subvariant: str
    prefix: str
    suffix: str


@dataclass(frozen=True)
class TreeInfo:
    """The parts of a `.treeinfo` this design depends on.

    `checksums` maps a tree-relative path to a bare sha256 hex digest, with the
    `sha256:` algorithm prefix already validated and stripped.
    """

    checksums: Mapping[str, str]
    build_timestamp: int


@dataclass(frozen=True)
class BaseFingerprint:
    """Everything that makes a base's media identity what it is.

    Bodhi release state is deliberately NOT a field. §4.3's formula listed it as
    an identity input while §4.4 says "Bodhi leaving `current` warns; it does not
    rebuild" — and since a differing identity IS the reinstall trigger, including
    it would have made Bodhi a reinstall trigger and contradicted §4.4 outright.
    Nothing upstream changes when a release is archived: the media is
    byte-identical, so the rebuild would burn a full base build to produce the
    same bytes. State is carried alongside as a warning signal instead.

    `base_name` and `base_kind` are inputs beyond §4.3's formula, so that a
    record copied between bases fails identity comparison rather than silently
    certifying the wrong one — §3.5's anti-substitution property, bound in data.
    """

    base_name: str
    base_kind: str
    compose_label: str
    artefacts: tuple[ArtefactRef, ...]
    treeinfo_checksums: Mapping[str, str] | None
    recipe_digest: str


def parse_compose_id(text: str) -> str:
    """Read a COMPOSE_ID document, e.g. `Fedora-44-20260422.1`.

    A COMPOSE_ID is one label on one line. Anything else — an empty body, an
    error page, a directory index — is an error, because taking the first line
    of such a document would launder it into a plausible-looking label.
    """
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    if not lines:
        raise UpstreamParseError("COMPOSE_ID is empty")
    if len(lines) > 1:
        raise UpstreamParseError(
            f"COMPOSE_ID has {len(lines)} non-blank lines; expected exactly one label"
        )
    label = lines[0]
    if not _COMPOSE_ID_RE.match(label):
        raise UpstreamParseError(f"COMPOSE_ID is not a compose label: {label[:80]!r}")
    return label


def parse_treeinfo(text: str) -> TreeInfo:
    """Parse a `.treeinfo` into its `[checksums]` map and `[tree] build_timestamp`.

    `images/install.img` is the Anaconda stage-2 runtime and the others are the
    installer kernel and boot images, so this section IS the answer to "did the
    installer change" — no proxy, no inference (§4.1 S2).
    """
    # `=` only: the values are `sha256:<hex>`, and configparser's default
    # delimiters include `:`, which would split a value rather than the pair on
    # any line where a colon came first.
    parser = _PathPreservingParser(delimiters=("=",))
    try:
        parser.read_string(text)
    except configparser.Error as exc:
        raise UpstreamParseError(f"treeinfo is not parseable INI: {exc}") from exc

    if not parser.has_section("checksums"):
        raise UpstreamParseError("treeinfo has no [checksums] section")

    checksums: dict[str, str] = {}
    for path, raw in parser.items("checksums"):
        algorithm, separator, digest = raw.partition(":")
        if not separator:
            raise UpstreamParseError(f"treeinfo checksum for {path} has no algorithm: {raw!r}")
        if algorithm != "sha256":
            raise UpstreamParseError(
                f"treeinfo checksum for {path} uses {algorithm!r}; this design compares sha256 only"
            )
        if not _SHA256_RE.match(digest):
            raise UpstreamParseError(f"treeinfo checksum for {path} is not a sha256 digest")
        checksums[path] = digest

    # An empty [checksums] section parses cleanly and hashes to a stable value,
    # which would make the identity vacuous for every Anaconda base — a check
    # that cannot fail. It is an error.
    if not checksums:
        raise UpstreamParseError("treeinfo [checksums] section is empty")

    if not parser.has_option("tree", "build_timestamp"):
        raise UpstreamParseError("treeinfo has no [tree] build_timestamp")
    raw_timestamp = parser.get("tree", "build_timestamp").strip()
    try:
        build_timestamp = int(raw_timestamp)
    except ValueError as exc:
        raise UpstreamParseError(
            f"treeinfo build_timestamp is not an integer: {raw_timestamp!r}"
        ) from exc

    return TreeInfo(checksums=checksums, build_timestamp=build_timestamp)


def parse_releases_json(text: str) -> dict[str, ReleaseArtefact]:
    """Index `https://fedoraproject.org/releases.json` by artefact filename.

    The document is a flat JSON array; every entry carries `link`, `sha256` and
    `size` (verified live: 378 entries, no gaps, no duplicate filenames).
    """
    try:
        document = json.loads(text)
    except json.JSONDecodeError as exc:
        raise UpstreamParseError(f"releases.json is not valid JSON: {exc}") from exc
    if not isinstance(document, list):
        raise UpstreamParseError(
            f"releases.json is a {type(document).__name__}, expected a list of artefacts"
        )
    if not document:
        raise UpstreamParseError("releases.json is empty")

    index: dict[str, ReleaseArtefact] = {}
    for position, entry in enumerate(document):
        if not isinstance(entry, dict):
            raise UpstreamParseError(f"releases.json entry {position} is not an object")
        link = entry.get("link")
        if not link:
            raise UpstreamParseError(f"releases.json entry {position} has no link")
        filename = posixpath.basename(urlsplit(link).path)
        if not filename:
            raise UpstreamParseError(
                f"releases.json entry {position} link has no filename: {link!r}"
            )
        raw_size = entry.get("size")
        if raw_size is None:
            raise UpstreamParseError(f"releases.json entry for {filename} has no size")
        try:
            size = int(raw_size)
        except (TypeError, ValueError) as exc:
            raise UpstreamParseError(
                f"releases.json entry for {filename} has a non-numeric size: {raw_size!r}"
            ) from exc
        structured: dict[str, str] = {}
        for field in ("version", "arch", "variant", "subvariant"):
            value = entry.get(field)
            if not isinstance(value, str) or not value:
                raise UpstreamParseError(
                    f"releases.json entry for {filename} has no {field}; cannot be selected"
                )
            structured[field] = value
        artefact = ReleaseArtefact(
            filename=filename,
            link=link,
            sha256=str(entry.get("sha256") or ""),
            size=size,
            **structured,
        )
        existing = index.get(filename)
        # A repeated filename is fine while it names the same bytes. Two
        # different hashes for one name means the document cannot say what the
        # artefact is, and picking either would be a guess.
        if existing is not None and existing.sha256 != artefact.sha256:
            raise UpstreamParseError(
                f"releases.json lists {filename} twice with different sha256 digests"
            )
        index[filename] = artefact
    return index


def find_artefact(index: Mapping[str, ReleaseArtefact], filename: str) -> ReleaseArtefact:
    """Look an artefact up by exact filename, or raise.

    Never returns None: a base whose media cannot be located upstream is the
    §4.5 fail-fast case, not a value to carry forward.
    """
    artefact = index.get(filename)
    if artefact is None:
        raise UpstreamParseError(f"releases.json does not list {filename}")
    if not _SHA256_RE.match(artefact.sha256):
        raise UpstreamParseError(f"releases.json entry for {filename} has no usable sha256")
    return artefact


def select_artefact(
    index: Mapping[str, ReleaseArtefact],
    fedora_version: int,
    arch: str,
    selector: ArtefactSelector,
) -> ReleaseArtefact:
    """The one artefact matching a base's selector for this version and arch.

    Zero matches is the §4.5 fail-fast case. More than one — two respins
    published side by side — is an error too: choosing between them by label
    order would be a guess, and the selector should be narrowed instead.
    """
    for field in ("variant", "subvariant", "prefix", "suffix"):
        if not getattr(selector, field):
            raise UpstreamParseError(f"artefact selector has an empty {field}")
    version = str(fedora_version)
    matches = [
        artefact
        for artefact in index.values()
        if artefact.version == version
        and artefact.arch == arch
        and artefact.variant == selector.variant
        and artefact.subvariant == selector.subvariant
        and artefact.filename.startswith(selector.prefix)
        and artefact.filename.endswith(selector.suffix)
    ]
    description = (
        f"version {version} {arch} variant={selector.variant} "
        f"subvariant={selector.subvariant} prefix={selector.prefix} suffix={selector.suffix}"
    )
    if not matches:
        raise UpstreamParseError(f"releases.json lists no artefact for {description}")
    if len(matches) > 1:
        names = ", ".join(sorted(m.filename for m in matches))
        raise UpstreamParseError(
            f"releases.json lists {len(matches)} artefacts for {description}: {names}; "
            "narrow the selector"
        )
    return find_artefact(index, matches[0].filename)


def compose_label_from_link(link: str) -> str:
    """Extract the compose label (e.g. `44-1.7`) from a releases.json `link`.

    §4.3: the label is read from this structured field and never scraped out of
    an Apache-generated directory listing, which is not a contract.
    """
    if not link:
        raise UpstreamParseError("cannot read a compose label from an empty link")
    filename = posixpath.basename(urlsplit(link).path)
    match = _COMPOSE_LABEL_RE.search(filename)
    if match is None:
        raise UpstreamParseError(f"no compose label in artefact filename: {filename!r}")
    return match.group(1)


def parse_bodhi_releases(text: str) -> dict[str, str]:
    """Map Bodhi release name (`F44`, `F44F`, `EPEL-10.0`) to its state."""
    try:
        document = json.loads(text)
    except json.JSONDecodeError as exc:
        raise UpstreamParseError(f"bodhi releases is not valid JSON: {exc}") from exc
    if not isinstance(document, dict):
        raise UpstreamParseError(
            f"bodhi releases is a {type(document).__name__}, expected an object"
        )

    # Measured live: 85 releases against rows_per_page=100. That is one page
    # today and becomes two without warning, at which point a single fetch
    # returns a PARTIAL list indistinguishable from a complete one — and a
    # missing F<version> would read as "unknown release" rather than "we only
    # looked at half of them". An absent `pages` cannot be assumed to mean one.
    raw_pages = document.get("pages")
    raw_page = document.get("page")
    if raw_pages is None or raw_page is None:
        raise UpstreamParseError(
            "bodhi releases has no pagination fields; cannot prove it is whole"
        )
    try:
        pages = int(raw_pages)
        page = int(raw_page)
    except (TypeError, ValueError) as exc:
        raise UpstreamParseError("bodhi releases pagination fields are not integers") from exc
    if pages != 1 or page != 1:
        raise UpstreamParseError(
            f"bodhi releases is paginated (page {page} of {pages}); this is a partial list. "
            "Raise rows_per_page or paginate the fetch — do not read one page as the whole set."
        )

    releases = document.get("releases")
    if not isinstance(releases, list):
        raise UpstreamParseError("bodhi releases document has no releases list")
    if not releases:
        raise UpstreamParseError("bodhi releases list is empty")

    states: dict[str, str] = {}
    for position, entry in enumerate(releases):
        if not isinstance(entry, dict):
            raise UpstreamParseError(f"bodhi release {position} is not an object")
        name = entry.get("name")
        state = entry.get("state")
        if not name:
            raise UpstreamParseError(f"bodhi release {position} has no name")
        if not state:
            raise UpstreamParseError(f"bodhi release {name!r} has no state")
        states[str(name)] = str(state)
    return states


def bodhi_state_for(states: Mapping[str, str], fedora_version: int) -> str:
    """The Bodhi state for a Fedora release, by EXACT name.

    Bodhi ships `F44` and `F44F` (Flatpak) side by side, and their states can
    diverge, so this must never prefix-match.
    """
    name = f"F{fedora_version}"
    state = states.get(name)
    if state is None:
        raise UpstreamParseError(f"bodhi does not list a release named {name}")
    return state


def parse_repomd_revision(text: str) -> int:
    """Read `<revision>` out of a `repomd.xml`. This is `package_revision` (§4.3).

    Matched with a regex rather than an XML parser on purpose: the stdlib's
    `xml.etree.ElementTree` is documented as not secure against maliciously
    constructed data, `defusedxml` is third-party and helpers here are
    stdlib-only, and exactly one well-defined element is needed. Requiring a
    single match is what keeps the shortcut honest — a document where the
    element repeats is rejected rather than guessed at.
    """
    matches = _REVISION_RE.findall(text)
    if not matches:
        raise UpstreamParseError("repomd.xml has no <revision> element")
    if len(matches) > 1:
        raise UpstreamParseError(
            f"repomd.xml has {len(matches)} <revision> elements; expected exactly one"
        )
    raw = matches[0]
    try:
        return int(raw)
    except ValueError as exc:
        raise UpstreamParseError(f"repomd.xml revision is not an integer: {raw!r}") from exc


def _validate(fingerprint: BaseFingerprint) -> None:
    if fingerprint.base_kind not in _KINDS:
        raise UpstreamParseError(
            f"unknown base kind {fingerprint.base_kind!r}; expected one of {sorted(_KINDS)}"
        )
    if not fingerprint.base_name:
        raise UpstreamParseError("base_name is required")
    if not fingerprint.compose_label:
        raise UpstreamParseError("compose_label is required")
    if not fingerprint.artefacts:
        raise UpstreamParseError(
            f"{fingerprint.base_name} has no artefacts; an identity over nothing cannot differ"
        )
    for artefact in fingerprint.artefacts:
        if not artefact.name:
            raise UpstreamParseError(f"{fingerprint.base_name} has an artefact with no name")
        if not _SHA256_RE.match(artefact.sha256):
            raise UpstreamParseError(
                f"{fingerprint.base_name} artefact {artefact.name} has a malformed sha256"
            )
    if not _SHA256_RE.match(fingerprint.recipe_digest):
        raise UpstreamParseError(f"{fingerprint.base_name} recipe_digest is not a sha256 digest")

    # The kind decides whether an install tree exists at all, so a mismatch here
    # means the caller assembled the wrong fingerprint. Hashing a tree a `fast`
    # base was not built from would bind its identity to media it never touched;
    # omitting the tree for a `full` base would drop the installer hashes that
    # are the entire point of the reinstall trigger.
    has_tree = fingerprint.base_kind in _KINDS_WITH_TREE
    if has_tree and not fingerprint.treeinfo_checksums:
        raise UpstreamParseError(
            f"{fingerprint.base_name} is kind {fingerprint.base_kind!r} and needs treeinfo checksums"
        )
    if not has_tree and fingerprint.treeinfo_checksums:
        raise UpstreamParseError(
            f"{fingerprint.base_name} is kind {fingerprint.base_kind!r} and has no install tree, "
            "but treeinfo checksums were supplied"
        )


def artefact_identity(fingerprint: BaseFingerprint) -> str:
    """The sha256 that answers "is this base built from the published media?" (§4.3).

    Canonicalised through sorted-key JSON so the digest depends on the VALUES and
    not on dict or tuple ordering — a base whose artefacts were listed in a
    different order must not read as a media change.
    """
    _validate(fingerprint)
    payload = {
        "schema": IDENTITY_SCHEMA,
        "base_name": fingerprint.base_name,
        "base_kind": fingerprint.base_kind,
        "compose_label": fingerprint.compose_label,
        "artefacts": [
            {"name": artefact.name, "sha256": artefact.sha256}
            for artefact in sorted(fingerprint.artefacts, key=lambda a: a.name)
        ],
        "treeinfo_checksums": (
            dict(sorted(fingerprint.treeinfo_checksums.items()))
            if fingerprint.treeinfo_checksums
            else None
        ),
        "recipe_digest": fingerprint.recipe_digest,
    }
    blob = json.dumps(payload, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    return hashlib.sha256(blob.encode("utf-8")).hexdigest()
