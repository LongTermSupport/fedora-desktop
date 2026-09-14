"""Pure logic for `org.gnome.shell enabled-extensions` as declared state.

Plan 00110's desktop acceptance run found a fresh install ending with every
extension this repo deploys loaded and compiled but none of them enabled:
`gnome-extensions enable` asks the *running shell* to enable a UUID, and on a
fresh install the shell has not scanned the new directory yet, so the request is
lost. The fix is to declare the list instead of requesting the action — write
every deployed UUID into the gsettings key the shell reads at session start.

This module is the pure half: it parses the GVariant string `gsettings get`
prints, merges the deployed UUIDs into it without removing anything the user
added, formats the result back, and discovers which UUIDs are actually deployed.
It performs no I/O beyond reading extension metadata from a directory it is
handed. The side-effecting half is `apply_enabled_extensions.py`.

Merging is additive on purpose: a user's own enabled extensions are not ours to
revoke, so the play is idempotent and never surprises anyone by turning
something off.
"""

from __future__ import annotations

import dataclasses
import json
import os
from collections.abc import Iterable, Sequence

# `gsettings get` prints an empty list with its type annotation, and only accepts
# the same form back — a bare `[]` is ambiguous and it refuses it.
EMPTY_LIST_LITERAL = "@as []"
_TYPE_PREFIX = "@as "


@dataclasses.dataclass(frozen=True)
class MergeResult:
    """The merged list, whether it differs from the current one, and what was added."""

    values: list[str]
    changed: bool
    added: list[str]


def parse_string_list(text: str) -> list[str]:
    """Parse the GVariant `as` literal `gsettings get` prints into a list of strings.

    Accepts `@as []`, `[]` and `['a', 'b']`, with single-quoted elements that may
    escape a quote or a backslash. Anything else raises ValueError rather than
    returning a short list — a silently misparsed setting would be written back
    with the user's extensions dropped.
    """
    stripped = text.strip()
    if stripped.startswith(_TYPE_PREFIX):
        stripped = stripped[len(_TYPE_PREFIX) :].strip()
    if not stripped.startswith("[") or not stripped.endswith("]"):
        raise ValueError(f"not a GVariant string list: {text!r}")

    body = stripped[1:-1].strip()
    if not body:
        return []

    values: list[str] = []
    index = 0
    length = len(body)
    while True:
        while index < length and body[index].isspace():
            index += 1
        if index >= length or body[index] != "'":
            raise ValueError(f"expected a quoted element at offset {index} in {text!r}")
        index += 1

        buffer: list[str] = []
        while True:
            if index >= length:
                raise ValueError(f"unterminated string in {text!r}")
            char = body[index]
            if char == "\\":
                if index + 1 >= length:
                    raise ValueError(f"trailing escape in {text!r}")
                buffer.append(body[index + 1])
                index += 2
                continue
            if char == "'":
                index += 1
                break
            buffer.append(char)
            index += 1
        values.append("".join(buffer))

        while index < length and body[index].isspace():
            index += 1
        if index >= length:
            return values
        if body[index] != ",":
            raise ValueError(f"expected ',' at offset {index} in {text!r}")
        index += 1


def format_string_list(values: Sequence[str]) -> str:
    """Render a list of strings as the GVariant `as` literal `gsettings set` accepts."""
    if not values:
        return EMPTY_LIST_LITERAL
    escaped = (value.replace("\\", "\\\\").replace("'", "\\'") for value in values)
    return "[" + ", ".join(f"'{value}'" for value in escaped) + "]"


def merge(current: Iterable[str], deployed: Iterable[str]) -> MergeResult:
    """Add every deployed UUID to the current list, keeping order and removing nothing.

    Duplicates in either input collapse to their first occurrence; a current list
    that held duplicates therefore reports `changed`, because writing the
    collapsed form back is a repair.
    """
    current_list = list(current)
    values: list[str] = []
    seen: set[str] = set()
    for uuid in current_list:
        if uuid not in seen:
            seen.add(uuid)
            values.append(uuid)

    added: list[str] = []
    for uuid in deployed:
        if uuid not in seen:
            seen.add(uuid)
            values.append(uuid)
            added.append(uuid)

    return MergeResult(values=values, changed=values != current_list, added=added)


def discover_deployed_uuids(extensions_dir: str) -> list[str]:
    """The UUIDs of every extension deployed under `extensions_dir`, sorted.

    The UUID comes from each `metadata.json`, not from the directory name, and a
    disagreement between the two is an error: GNOME Shell refuses to load such an
    extension, so skipping it would hand the caller a short list that then passes
    as "everything deployed is enabled".
    """
    if not os.path.isdir(extensions_dir):
        return []

    uuids: list[str] = []
    for name in sorted(os.listdir(extensions_dir)):
        directory = os.path.join(extensions_dir, name)
        if not os.path.isdir(directory):
            continue
        metadata_path = os.path.join(directory, "metadata.json")
        if not os.path.isfile(metadata_path):
            continue

        with open(metadata_path, encoding="utf-8") as handle:
            try:
                metadata = json.load(handle)
            except json.JSONDecodeError as error:
                raise ValueError(f"{metadata_path} is not valid JSON: {error}") from error

        uuid = metadata.get("uuid") if isinstance(metadata, dict) else None
        if not isinstance(uuid, str) or not uuid:
            raise ValueError(
                f"{metadata_path} declares no 'uuid'; GNOME Shell cannot load it"
            )
        if uuid != name:
            raise ValueError(
                f"{metadata_path} declares uuid {uuid!r} but sits in directory "
                f"{name!r}; GNOME Shell requires them to match and will not load it"
            )
        # The executor reports this list on a comma-separated marker line the play
        # splits. No real UUID holds a comma; one that did would silently become two
        # UUIDs there, so it is refused here where the message can name the file.
        if "," in uuid:
            raise ValueError(
                f"{metadata_path} declares uuid {uuid!r}, which contains a comma; "
                "that cannot be reported to the play unambiguously"
            )
        uuids.append(uuid)
    return uuids


def missing_required(deployed: Iterable[str], required: Iterable[str]) -> list[str]:
    """Required UUIDs absent from `deployed`, in the order they were required."""
    present = set(deployed)
    return [uuid for uuid in required if uuid not in present]
