"""The retired-plays map: which play absorbed each play that has been removed.

A play the ledger has seen and HEAD no longer has is GONE, and GONE on its own has
no remedy — there is nothing to re-run, so the finding would stand for ever. When a
play is merged into another, `retired-plays.json` beside this file records the
successor. `freshness.retire` then turns the advice into "run the successor", and
drops the finding once the successor has run SUCCESSFULLY at a commit without the old
play.

**Remove the old play and merge its tasks into the successor in ONE commit.** "The old
play is absent at that commit" is the only evidence the successor carried its tasks,
so a commit that deletes the play before the tasks arrive would let a run of the
unfinished successor retire it.

**Refused, never read around.** The map changes what a health check says, so a map
that is malformed, names a play still at HEAD, or points at a successor that does not
exist is an error the check reports, not an entry it skips. A skipped entry would
leave a real finding in place with nothing saying why.

The file is JSON (stdlib-parseable, unlike YAML) and a flat object:
`{"<removed play>": "<successor play>"}`, both repo-relative exactly as the ledger
records `play`.
"""

from __future__ import annotations

import json
import os
import posixpath
from collections.abc import Callable
from typing import Any

#: Repo-relative, so the map read is the one in the checkout being judged.
MAP_PATH = "helpers/play_ledger/retired-plays.json"


def _refuse_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"retired-plays map names {key!r} twice")
        result[key] = value
    return result


def _require_play_path(value: Any, role: str) -> str:
    if not isinstance(value, str) or not value:
        raise ValueError(f"retired-plays map has a {role} that is not a non-empty string: {value!r}")
    if value.startswith("/") or posixpath.normpath(value) != value or value.split("/")[0] == "..":
        raise ValueError(
            f"retired-plays map {role} {value!r} is not a normalised repo-relative path, "
            "so it can never match a play the ledger records"
        )
    return value


def parse(text: str) -> dict[str, str]:
    """The map in `text`. Raises ValueError on anything that is not a clean map."""
    try:
        data = json.loads(text, object_pairs_hook=_refuse_duplicates)
    except json.JSONDecodeError as error:
        raise ValueError(f"retired-plays map is not valid JSON: {error}") from error
    if not isinstance(data, dict):
        raise ValueError(f"retired-plays map must be a JSON object, got {type(data).__name__}")
    mapping: dict[str, str] = {}
    for gone, successor in data.items():
        mapping[_require_play_path(gone, "removed play")] = _require_play_path(successor, "successor")
        if gone == successor:
            raise ValueError(f"retired-plays map names {gone!r} as its own successor")
    for gone, successor in mapping.items():
        if successor in mapping:
            raise ValueError(
                f"retired-plays map points {gone!r} at {successor!r}, which is itself "
                f"retired; point it at {mapping[successor]!r} instead"
            )
    return mapping


def load(repo_root: str) -> dict[str, str]:
    """The checkout's map. A missing file raises: it is tracked, so absence is a fault."""
    with open(os.path.join(repo_root, MAP_PATH), encoding="utf-8") as handle:
        return parse(handle.read())


def validate(mapping: dict[str, str], *, exists_at_head: Callable[[str], bool]) -> None:
    """Refuse a map that disagrees with HEAD, naming the entry at fault."""
    for gone, successor in mapping.items():
        if exists_at_head(gone):
            raise ValueError(
                f"retired-plays map lists {gone!r} as removed, but it still exists at "
                "HEAD; remove the entry or the play"
            )
        if not exists_at_head(successor):
            raise ValueError(
                f"retired-plays map names {successor!r} as the successor of {gone!r}, "
                "but it does not exist at HEAD"
            )
