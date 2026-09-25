"""Which commit-signing keys GitHub still needs, and on which account.

Every account in github_accounts has its own signing key, registered on that account, so
a commit it signs shows as Verified there. The machine key signs everything else, the
repos whose remote is plain github.com, which ~/.ssh/id reaches; it belongs on whichever
account holds ~/.ssh/id as an authentication key.

Pure: no I/O. Keys are compared by their base64 body, which is what GitHub lists.
"""

from __future__ import annotations

import json
import re
from typing import NamedTuple

_KEY_TYPES = ("ssh-", "ecdsa-", "sk-")
_ALIAS = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]*$")


class Wanted(NamedTuple):
    login: str
    name: str
    blob: str


def parse_accounts(text: str) -> dict[str, str]:
    """github_accounts from its JSON form: a non-empty map of alias to GitHub login."""
    try:
        accounts = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ValueError(f"not JSON: {exc}") from exc
    if not isinstance(accounts, dict) or not accounts:
        raise ValueError("must be a non-empty map of alias to GitHub login")
    for alias, login in accounts.items():
        if not _ALIAS.match(alias) or not isinstance(login, str) or not login:
            raise ValueError(f"{alias!r}: {login!r} is not an alias and a GitHub login")
    return accounts


def account_key_name(alias: str) -> str:
    """The file name of an account's signing key in ~/.ssh."""
    return f"github_{alias}_signing"


def key_blob(line: str) -> str:
    """The base64 body of one OpenSSH public key line. ValueError when it is not one."""
    fields = line.split()
    if len(fields) < 2 or not fields[0].startswith(_KEY_TYPES):
        raise ValueError(f"not an OpenSSH public key line: {line.strip()[:40]!r}")
    return fields[1]


def blobs(listing: str) -> set[str]:
    """The key bodies in a listing of public key lines, one per line."""
    return {key_blob(line) for line in listing.splitlines() if line.strip()}


def owner_of_login_key(login_blob: str, auth_blobs: dict[str, set[str]]) -> str:
    """The one account that holds the login key as an authentication key."""
    owners = sorted(login for login, held in auth_blobs.items() if login_blob in held)
    if not owners:
        raise ValueError(
            f"none of {', '.join(sorted(auth_blobs))} holds the login key as an authentication key"
        )
    if len(owners) > 1:
        raise ValueError(
            f"the login key is an authentication key on more than one account: {', '.join(owners)}"
        )
    return owners[0]


def missing_registrations(
    wanted: list[Wanted], signing_blobs: dict[str, set[str]]
) -> list[Wanted]:
    """The wanted keys not yet a signing key on their own account, in the order given."""
    return [w for w in wanted if w.blob not in signing_blobs.get(w.login, set())]
