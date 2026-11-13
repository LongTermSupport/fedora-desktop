"""The one list of the Matrix desktop clients this repository installs (issue #251).

`matrix_clients.json`, beside this module and packaged with it, names each client: its Flatpak
app id (its data is `~/.var/app/<app id>`), any native install's folders (`parent/name`, and
`parent/name-<profile>` when it has profiles), and whether it keeps its session in the keyring.
Every guard that keeps an agent away from a human's Matrix session reads it; the Go twin,
src-go/internal/matrixclients, reads an embedded copy by the same rules, and its tests hold the
guards that cannot read the list at run time equal to it.

The consent record (config.ELEMENT_ACCEPTED_FILE) is read here: one line `clients[ <app
id>...]` names the clients the owner accepted a cc host seat beside; a record without that
line was written before the record named its clients, and counts for `legacy_consent` alone.
"""

from __future__ import annotations

import dataclasses
import importlib.resources
import json
import os
import pathlib
import re
from collections.abc import Iterable, Sequence

#: The most a consent record may hold.
CONSENT_MAX_BYTES = 4096
FLATPAK_DATA = ".var/app/"
_APP_ID = re.compile(r"[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_-]*){2,}")
_LIST_KEYS = {"clients": list, "legacy_consent": list}
_CLIENT_KEYS = {"name": str, "app_id": str, "keyring": bool, "native": list}
_NATIVE_KEYS = {"parent": str, "name": str, "profiles": bool}


class ListError(Exception):
    """The list is not one every reader takes the same way."""


class ConsentError(Exception):
    """The consent record is not one pingbus can read."""


class PresenceError(Exception):
    """A folder that could hold a profile cannot be listed."""


@dataclasses.dataclass(frozen=True)
class Native:
    parent: str
    name: str
    profiles: bool

    def matches(self, entry: str) -> bool:
        """Whether an entry of the parent is this folder or one of its profiles."""
        return entry == self.name or (self.profiles and entry.startswith(f"{self.name}-"))


@dataclasses.dataclass(frozen=True)
class Client:
    name: str
    app_id: str
    keyring: bool
    native: tuple[Native, ...]

    @property
    def flatpak_dir(self) -> str:
        """The client's Flatpak data folder, relative to a home."""
        return FLATPAK_DATA + self.app_id


@dataclasses.dataclass(frozen=True)
class Found:
    """A client's profile found under a home."""

    client: Client
    path: pathlib.Path


@dataclasses.dataclass(frozen=True)
class MatrixClients:
    clients: tuple[Client, ...]
    legacy_consent: tuple[str, ...]

    def app_ids(self) -> list[str]:
        return [c.app_id for c in self.clients]

    def client(self, name: str) -> Client:
        """The listed client called `name`."""
        for c in self.clients:
            if c.name == name:
                return c
        raise ListError(f"the Matrix client list names no {name}")

    def profile_paths(self) -> tuple[str, ...]:
        """Each Flatpak's data folder and each native parent/name, in list order."""
        paths: list[str] = []
        for c in self.clients:
            paths.append(c.flatpak_dir)
            paths.extend(f"{n.parent}/{n.name}" for n in c.native)
        return tuple(paths)

    def profile_prefixes(self) -> tuple[str, ...]:
        """The native parent/name- whose entries are named profiles."""
        return tuple(f"{n.parent}/{n.name}-" for c in self.clients for n in c.native if n.profiles)

    def names(self, ids: Iterable[str]) -> str:
        """The listed clients among `ids` in words, in list order."""
        wanted = set(ids)
        return words([c.name for c in self.clients if c.app_id in wanted])

    def present(self, home: pathlib.Path) -> list[Found]:
        """Every listed client's profile under `home`, in list order: its Flatpak data folder
        (a dangling link counts: something put it there), then each native folder and its named
        profiles. A parent folder that cannot be listed could hide one, so it is an error."""
        found: list[Found] = []
        for c in self.clients:
            flatpak = home / c.flatpak_dir
            if os.path.lexists(flatpak):
                found.append(Found(c, flatpak))
            for n in c.native:
                parent = home / n.parent
                if not parent.is_dir():
                    continue
                try:
                    entries = sorted(entry.name for entry in os.scandir(parent))
                except OSError as exc:
                    raise PresenceError(
                        f"{parent} cannot be listed to look for a {c.name} profile ({exc.strerror})"
                    ) from None
                found.extend(Found(c, parent / entry) for entry in entries if n.matches(entry))
        return found

    def parse_consent(self, data: bytes) -> list[str]:
        """The app ids a consent record accepts: those its one clients line names, or, for a
        record without one, the legacy_consent clients."""
        if len(data) > CONSENT_MAX_BYTES:
            raise ConsentError(f"the consent record is larger than {CONSENT_MAX_BYTES} bytes")
        lines = [raw for raw in data.split(b"\n") if raw.startswith(b"clients")]
        if not lines:
            return list(self.legacy_consent)
        if len(lines) > 1:
            raise ConsentError("the consent record has more than one clients line")
        line = lines[0]
        if line != b"clients" and not line.startswith(b"clients "):
            raise ConsentError(
                'the consent record\'s clients line is not "clients" and its app ids, one space apart'
            )
        ids: list[str] = []
        for word in line.split(b" ")[1:]:
            text = word.decode("ascii", errors="replace")
            if not _APP_ID.fullmatch(text):
                raise ConsentError(f"the consent record's clients line holds {text!r}, which is not an app id")
            if text in ids:
                raise ConsentError(f"the consent record names {text} twice")
            ids.append(text)
        return ids


def words(names: Sequence[str]) -> str:
    """Names joined as a sentence does: "A", "A and B", "A, B and C"."""
    if len(names) < 2:
        return "".join(names)
    return ", ".join(names[:-1]) + " and " + names[-1]


def uncovered(found: Iterable[Found], accepted: Iterable[str]) -> list[Client]:
    """The clients found whose app id `accepted` does not name, each once, in order."""
    allowed = set(accepted)
    out: list[Client] = []
    for f in found:
        if f.client.app_id not in allowed and f.client not in out:
            out.append(f.client)
    return out


def load() -> MatrixClients:
    """The list packaged beside this module (in the repository, or in the pingbus zipapp)."""
    data = importlib.resources.files("helpers.pingbus").joinpath("matrix_clients.json").read_bytes()
    return parse(data)


def parse(data: bytes) -> MatrixClients:
    """Read a list, refusing unknown keys, wrong types, trailing data and any entry the
    readers could take differently."""
    try:
        raw = json.loads(data)
    except (ValueError, UnicodeDecodeError) as exc:
        raise ListError(f"the Matrix client list: {exc}") from None
    top = _object(raw, _LIST_KEYS, "the list")
    clients: list[Client] = []
    for i, entry in enumerate(top.get("clients", [])):
        # A missing key is its zero value, as Go's decoder leaves it.
        fields = _object(entry, _CLIENT_KEYS, f"client {i + 1}")
        native = []
        for item in fields.get("native", []):
            n = _object(item, _NATIVE_KEYS, f"client {i + 1}'s native folder")
            native.append(Native(n.get("parent", ""), n.get("name", ""), n.get("profiles", False)))
        clients.append(Client(fields.get("name", ""), fields.get("app_id", ""), fields.get("keyring", False), tuple(native)))
    legacy = top.get("legacy_consent", [])
    if not all(isinstance(item, str) for item in legacy):
        raise ListError("the Matrix client list: legacy_consent holds a value that is not an app id")
    listed = MatrixClients(tuple(clients), tuple(legacy))
    _validate(listed)
    return listed


def _object(value: object, keys: dict[str, type], what: str) -> dict:
    if not isinstance(value, dict):
        raise ListError(f"the Matrix client list: {what} is not an object")
    for key, item in value.items():
        if key not in keys:
            raise ListError(f"the Matrix client list: {what} has an unknown key {key!r}")
        if not isinstance(item, keys[key]):
            raise ListError(f"the Matrix client list: {what}'s {key} is not a {keys[key].__name__}")
    return value


def _validate(listed: MatrixClients) -> None:
    if not listed.clients:
        raise ListError("the Matrix client list names no client")
    seen: set[str] = set()
    for i, c in enumerate(listed.clients):
        if not c.name:
            raise ListError(f"the Matrix client list: client {i + 1} has no name")
        if not _APP_ID.fullmatch(c.app_id):
            raise ListError(f"the Matrix client list: {c.name}: {c.app_id!r} is not a Flatpak app id")
        if c.app_id in seen:
            raise ListError(f"the Matrix client list: {c.app_id} is listed twice")
        for n in c.native:
            if not _relative_dir(n.parent) or n.name in ("", ".", "..") or any(ch in n.name for ch in "/*"):
                raise ListError(
                    f"the Matrix client list: {c.name}: a native folder must be a relative parent and a plain name"
                )
        seen.add(c.app_id)
    for app_id in listed.legacy_consent:
        if app_id not in seen:
            raise ListError(f"the Matrix client list: legacy_consent names {app_id}, which is not listed")


def _relative_dir(path: str) -> bool:
    if not path or path.startswith("/") or "*" in path:
        return False
    return all(part not in ("", ".", "..") for part in path.split("/"))
