"""The base.json record: one base's fingerprint and provenance (Plan 00110, DESIGN.md §3.2).

Pure. The base builder assembles a record with `build_record` and writes what
`render_record` returns; every run reads it back through `parse_record`, which
re-validates every field and recomputes the artefact identity from the record's
own fields — a record whose stored identity disagrees with its contents is not
a base, it is a copy of one, and refusing it is §3.5's anti-substitution
property applied to the file on disk.

What the record carries, and which decision each field feeds:

    artefact_identity, compose_label, artefacts, treeinfo_checksums,
    recipe_digest                          -> reinstall (§4.3, §4.4)
    last_upgraded_revision (GUEST-seen),
    last_upgraded_mirror, refresh_state    -> refresh, and its completeness (§4.4a)
    installed_at, last_upgraded_at         -> the TTL backstops (§4.4)
    base_sha256, base_size, base_mtime     -> "is the disk the one we built?" (§7:
                                              size+mtime per run, sha256 on demand)
"""

from __future__ import annotations

import json
import re
from collections.abc import Mapping
from dataclasses import asdict, dataclass

from helpers.vmtest import upstream

SCHEMA = 1

PROFILES = frozenset({"server", "desktop"})
KINDS = frozenset({upstream.KIND_FAST, upstream.KIND_FULL})

REFRESH_COMPLETE = "complete"
REFRESH_INCOMPLETE = "incomplete"
REFRESH_DEGRADED = "degraded"
REFRESH_STATES = frozenset({REFRESH_COMPLETE, REFRESH_INCOMPLETE, REFRESH_DEGRADED})

_SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
_COMPOSE_ID_RE = re.compile(r"^Fedora-[A-Za-z0-9][A-Za-z0-9._-]*$")
_COMPOSE_LABEL_RE = re.compile(r"^\d+-\d+\.\d+$")
_HTTP_URL_RE = re.compile(r"^https?://\S+$")


class BaseRecordError(ValueError):
    """The record is not a coherent description of a base; no run may use it."""


@dataclass(frozen=True)
class BaseRecord:
    schema: int
    fedora_version: int
    profile: str
    kind: str
    name: str
    compose_id: str
    compose_label: str
    artefacts: tuple[dict[str, str], ...]
    treeinfo_checksums: dict[str, str] | None
    recipe_digest: str
    artefact_identity: str
    installed_at: int
    last_upgraded_at: int
    last_upgraded_revision: int
    last_upgraded_mirror: str
    refresh_state: str
    base_sha256: str
    base_size: int
    base_mtime: int


def _is_int(value: object) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def _positive_int(value: object, field: str) -> int:
    if not _is_int(value) or value <= 0:
        raise BaseRecordError(f"{field} must be a positive integer, got {value!r}")
    return value


def _sha256(value: object, field: str) -> str:
    if not isinstance(value, str) or not _SHA256_RE.match(value):
        raise BaseRecordError(f"{field} must be a sha256 hex digest, got {str(value)[:80]!r}")
    return value


def _artefacts(value: object) -> tuple[dict[str, str], ...]:
    if not isinstance(value, (list, tuple)) or not value:
        raise BaseRecordError("artefacts must be a non-empty list")
    result = []
    for position, entry in enumerate(value):
        if not isinstance(entry, Mapping) or set(entry) != {"name", "sha256"}:
            raise BaseRecordError(f"artefacts[{position}] must have exactly name and sha256")
        name = entry["name"]
        if not isinstance(name, str) or not name:
            raise BaseRecordError(f"artefacts[{position}].name must be a non-empty string")
        result.append({"name": name, "sha256": _sha256(entry["sha256"], f"artefacts[{position}].sha256")})
    return tuple(result)


def _treeinfo(value: object, kind: str) -> dict[str, str] | None:
    if kind == upstream.KIND_FAST:
        if value is not None:
            raise BaseRecordError("a fast base has no install tree, but treeinfo_checksums is set")
        return None
    if not isinstance(value, Mapping) or not value:
        raise BaseRecordError("a full base needs a non-empty treeinfo_checksums mapping")
    checksums = {}
    for path, digest in value.items():
        if not isinstance(path, str) or not path:
            raise BaseRecordError("treeinfo_checksums has an empty path")
        checksums[path] = _sha256(digest, f"treeinfo_checksums[{path}]")
    return dict(sorted(checksums.items()))


def fingerprint_of(record: BaseRecord) -> upstream.BaseFingerprint:
    """The fingerprint the record's identity was computed from."""
    return upstream.BaseFingerprint(
        base_name=record.name,
        base_kind=record.kind,
        compose_label=record.compose_label,
        artefacts=tuple(upstream.ArtefactRef(a["name"], a["sha256"]) for a in record.artefacts),
        treeinfo_checksums=record.treeinfo_checksums,
        recipe_digest=record.recipe_digest,
    )


def _validated(fields: dict) -> BaseRecord:
    """Build a record from raw fields, checking every one; identity is derived, never trusted."""
    schema = fields.get("schema", SCHEMA)
    if schema != SCHEMA:
        raise BaseRecordError(f"base.json schema {schema!r} is not the supported schema {SCHEMA}")

    fedora_version = _positive_int(fields.get("fedora_version"), "fedora_version")
    profile = fields.get("profile")
    if profile not in PROFILES:
        raise BaseRecordError(f"profile {profile!r} is not one of {sorted(PROFILES)}")
    kind = fields.get("kind")
    if kind not in KINDS:
        raise BaseRecordError(f"kind {kind!r} is not one of {sorted(KINDS)}")
    name = fields.get("name")
    if not isinstance(name, str) or not name.endswith(f"-{fedora_version}") or len(name) <= len(f"-{fedora_version}"):
        raise BaseRecordError(
            f"name {name!r} must end in -{fedora_version}; a name that does not match the "
            "record's Fedora version is a record copied from another base"
        )
    compose_id = fields.get("compose_id")
    if not isinstance(compose_id, str) or not _COMPOSE_ID_RE.match(compose_id):
        raise BaseRecordError(f"compose_id {compose_id!r} is not a compose label")
    compose_label = fields.get("compose_label")
    if not isinstance(compose_label, str) or not _COMPOSE_LABEL_RE.match(compose_label):
        raise BaseRecordError(f"compose_label {compose_label!r} is not of the form <version>-<respin>")

    artefacts = _artefacts(fields.get("artefacts"))
    treeinfo_checksums = _treeinfo(fields.get("treeinfo_checksums"), kind)
    recipe_digest = _sha256(fields.get("recipe_digest"), "recipe_digest")

    installed_at = _positive_int(fields.get("installed_at"), "installed_at")
    last_upgraded_at = _positive_int(fields.get("last_upgraded_at"), "last_upgraded_at")
    if last_upgraded_at < installed_at:
        raise BaseRecordError(
            f"last_upgraded_at ({last_upgraded_at}) is before installed_at ({installed_at})"
        )
    last_upgraded_revision = _positive_int(fields.get("last_upgraded_revision"), "last_upgraded_revision")
    mirror = fields.get("last_upgraded_mirror")
    if not isinstance(mirror, str) or not _HTTP_URL_RE.match(mirror):
        raise BaseRecordError(f"last_upgraded_mirror must be an http(s) URL, got {mirror!r}")
    refresh_state = fields.get("refresh_state")
    if refresh_state not in REFRESH_STATES:
        raise BaseRecordError(f"refresh_state {refresh_state!r} is not one of {sorted(REFRESH_STATES)}")

    base_sha256 = _sha256(fields.get("base_sha256"), "base_sha256")
    base_size = _positive_int(fields.get("base_size"), "base_size")
    base_mtime = _positive_int(fields.get("base_mtime"), "base_mtime")

    partial = BaseRecord(
        schema=SCHEMA,
        fedora_version=fedora_version,
        profile=profile,
        kind=kind,
        name=name,
        compose_id=compose_id,
        compose_label=compose_label,
        artefacts=artefacts,
        treeinfo_checksums=treeinfo_checksums,
        recipe_digest=recipe_digest,
        artefact_identity="",
        installed_at=installed_at,
        last_upgraded_at=last_upgraded_at,
        last_upgraded_revision=last_upgraded_revision,
        last_upgraded_mirror=mirror,
        refresh_state=refresh_state,
        base_sha256=base_sha256,
        base_size=base_size,
        base_mtime=base_mtime,
    )
    try:
        identity = upstream.artefact_identity(fingerprint_of(partial))
    except upstream.UpstreamParseError as exc:
        raise BaseRecordError(f"cannot compute artefact_identity: {exc}") from exc

    stored = fields.get("artefact_identity")
    if stored is not None and stored != identity:
        raise BaseRecordError(
            "artefact_identity in the record does not match its own fields; the record was "
            "edited or copied and cannot certify this base"
        )
    return BaseRecord(**{**asdict(partial), "artefacts": artefacts, "artefact_identity": identity})


def build_record(**fields) -> BaseRecord:
    """Assemble a new record from the builder's facts; the identity is computed here."""
    if "artefact_identity" in fields or "schema" in fields:
        raise BaseRecordError("artefact_identity and schema are derived; do not supply them")
    return _validated(fields)


def render_record(record: BaseRecord) -> str:
    """The on-disk form: sorted keys, newline-terminated, artefacts as a list."""
    document = asdict(record)
    document["artefacts"] = list(record.artefacts)
    return json.dumps(document, indent=2, sort_keys=True) + "\n"


def parse_record(text: str) -> BaseRecord:
    """Read base.json, re-validating every field and the stored identity."""
    try:
        document = json.loads(text)
    except json.JSONDecodeError as exc:
        raise BaseRecordError(f"base.json is not valid JSON: {exc}") from exc
    if not isinstance(document, Mapping):
        raise BaseRecordError(f"base.json must be an object, got {type(document).__name__}")
    expected = {f.name for f in BaseRecord.__dataclass_fields__.values()}
    missing = sorted(expected - set(document))
    unknown = sorted(set(document) - expected)
    if missing:
        raise BaseRecordError(f"base.json is missing {', '.join(missing)}")
    if unknown:
        raise BaseRecordError(f"base.json has unknown key(s) {', '.join(unknown)}")
    return _validated(dict(document))
