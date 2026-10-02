"""The server's allowed-signers list: every key of the owner's it trusts (Plan 00137 Task 4.8).

Run: `python3 -m helpers.self_update.signers < input.json`, where the input is
`{"principal": P, "keys": [...] or null, "key": "..." or null}`: the host vars
`self_update_signing_principal`, `self_update_signing_public_keys` and the older single
`self_update_signing_public_key`, with an undeclared one as null.

The owner's commits carry more than one key's signature: the desktop's `~/.ssh/id` signs
on the host, and a ccy session signs with the GitHub account key it was started with
(Plan 00139 D5). So the server trusts a list. The effective list is the declared list,
then the single key when a server still declares it, each key once (a key is its type and
blob; the comment is a label). At least one is required, and each must be one public key
line. Anything else is refused, never skipped.

stdout is the allowed-signers file, one `<principal> <key>` line per key, and nothing
else. A refusal exits EXIT_INVALID with the reason on stderr and nothing on stdout.
"""

from __future__ import annotations

import json
import re
import sys
from typing import TextIO

EXIT_OK = 0
EXIT_INVALID = 2

LIST_VAR = "self_update_signing_public_keys"
SINGLE_VAR = "self_update_signing_public_key"
PRINCIPAL_VAR = "self_update_signing_principal"

_PUBLIC_KEY = re.compile(
    r"(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp[0-9]+|sk-ssh-ed25519@openssh\.com) [A-Za-z0-9+/=]+( [^\n]*)?"
)
_PRINCIPAL = re.compile(r"[^\s]+")


class InvalidSigners(ValueError):
    """The declared keys cannot make an allowed-signers file; the message is the remedy."""


def _checked(value: object, name: str) -> str:
    if not isinstance(value, str):
        raise InvalidSigners(f"{name} must be one public key line (a string), not {type(value).__name__}")
    key = value.strip()
    if not _PUBLIC_KEY.fullmatch(key):
        raise InvalidSigners(
            f"{name} is not one SSH public key line (`<type> <base64> [comment]`, the contents of a .pub file)"
        )
    return key


def effective_keys(listed: object, single: object) -> list[str]:
    """The keys to trust: `listed`, then `single`, each once, validated."""
    candidates: list[tuple[object, str]] = []
    if listed is not None:
        if not isinstance(listed, list):
            raise InvalidSigners(f"{LIST_VAR} must be a list of public key lines, one per key")
        candidates += [(value, f"{LIST_VAR}[{index}]") for index, value in enumerate(listed)]
    if single is not None and single != "":
        candidates.append((single, SINGLE_VAR))
    if listed is None and not candidates:
        raise InvalidSigners(
            f"declare {LIST_VAR}: the .pub line of every key that signs the owner's commits "
            "(the desktop's ~/.ssh/id.pub and each GitHub account key ccy sessions push with)"
        )
    keys: list[str] = []
    seen: set[str] = set()
    for value, name in candidates:
        key = _checked(value, name)
        identity = " ".join(key.split()[:2])
        if identity not in seen:
            seen.add(identity)
            keys.append(key)
    if not keys:
        raise InvalidSigners(f"{LIST_VAR} must name at least one key")
    return keys


def render(principal: str, keys: list[str]) -> str:
    """The allowed-signers file: one `<principal> <key>` line per key."""
    if not _PRINCIPAL.fullmatch(principal):
        raise InvalidSigners(f"{PRINCIPAL_VAR} must be one word with no whitespace (the signer's email)")
    return "".join(f"{principal} {key}\n" for key in keys)


def main(*, stdin: TextIO = sys.stdin, stdout: TextIO = sys.stdout, stderr: TextIO = sys.stderr) -> int:
    try:
        try:
            given = json.loads(stdin.read())
        except json.JSONDecodeError as error:
            raise InvalidSigners(f"the input is not JSON: {error}") from error
        if not isinstance(given, dict) or not {"principal", "keys", "key"} <= given.keys():
            raise InvalidSigners('the input must be a JSON object with "principal", "keys" and "key"')
        principal = given["principal"]
        if not isinstance(principal, str):
            raise InvalidSigners(f"{PRINCIPAL_VAR} must be a string")
        content = render(principal, effective_keys(given["keys"], given["key"]))
    except InvalidSigners as error:
        stderr.write(f"self-update signers: refused: {error}\n")
        return EXIT_INVALID
    stdout.write(content)
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
