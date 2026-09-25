"""The GitHub OAuth scope rules, in one place.

`vars/github-required-scopes.yml` says WHICH scopes every gh token must carry. This module
says what a token's granted scopes satisfy, and it is the only copy of that: run.bash,
scripts/gh-account-setup.bash and play-github-cli-multi.yml all ask the CLI beside it
rather than keep a table of their own. Three hand-kept copies of the hierarchy were how a
new scope came to be understood by one caller and not the others.

Pure: no I/O. The scopes file is parsed from its text, so this needs no YAML library; the
format is held to the one shape the file uses, and anything else is an error rather than
a scope quietly dropped from every check.
"""

from __future__ import annotations

import re

LIST_KEY = "github_required_scopes"

# GitHub's hierarchy: a granted scope on the left satisfies every scope on its right.
# admin:X > write:X > read:X for these families; `user` and `project` cover their children.
_ADMIN_WRITE_READ = ("org", "public_key", "repo_hook", "gpg_key", "ssh_signing_key")
_IMPLIES: dict[str, set[str]] = {}
for _family in _ADMIN_WRITE_READ:
    _IMPLIES[f"admin:{_family}"] = {f"write:{_family}", f"read:{_family}"}
    _IMPLIES[f"write:{_family}"] = {f"read:{_family}"}
_IMPLIES["user"] = {"read:user", "user:email", "user:follow"}
_IMPLIES["project"] = {"read:project"}

_ITEM = re.compile(r"^\s+-\s+(?P<scope>[a-z_:]+)\s*$")
_HEADER = re.compile(r"^x-oauth-scopes:(?P<value>.*)$", re.IGNORECASE)


def _strip_comment(line: str) -> str:
    return line.split("#", 1)[0].rstrip()


def load_required(text: str) -> list[str]:
    """The required scopes from the text of vars/github-required-scopes.yml.

    Raises ValueError when the list is absent, empty, repeats a scope, or holds a line
    that is not a plain `  - scope` item.
    """
    found = False
    required: list[str] = []
    for raw in text.splitlines():
        line = _strip_comment(raw)
        if not line.strip() or line.strip() == "---":
            continue
        if not found:
            if line == f"{LIST_KEY}:":
                found = True
            elif line.startswith(f"{LIST_KEY}:"):
                raise ValueError(f"{LIST_KEY} must be a block list of '  - scope' lines, got: {raw!r}")
            continue
        if not line[0].isspace():
            break
        match = _ITEM.match(line)
        if match is None:
            raise ValueError(f"not a '  - scope' line under {LIST_KEY}: {raw!r}")
        scope = match.group("scope")
        if scope in required:
            raise ValueError(f"{scope} is listed twice under {LIST_KEY}")
        required.append(scope)
    if not found:
        raise ValueError(f"no {LIST_KEY} list found")
    if not required:
        raise ValueError(f"{LIST_KEY} is empty")
    return required


def parse_granted_list(text: str) -> set[str]:
    """The scopes in a comma-separated list, as `gh auth status --json hosts` reports them."""
    return {s.strip() for s in text.replace("\n", ",").split(",") if s.strip()}


def parse_granted_response(text: str) -> set[str]:
    """The scopes a token carries, read from the output of `gh api -i user`.

    The X-OAuth-Scopes header is matched at the start of a line, so
    Access-Control-Expose-Headers, whose value names that header, is not mistaken for it.
    No such header means no classic OAuth scopes were granted.
    """
    for line in text.splitlines():
        match = _HEADER.match(line.strip("\r"))
        if match:
            return parse_granted_list(match.group("value"))
    return set()


def _satisfies(granted: str) -> set[str]:
    return {granted} | _IMPLIES.get(granted, set())


def missing(required: list[str], granted: set[str]) -> list[str]:
    """The required scopes the granted set does not satisfy, in the required order."""
    covered: set[str] = set()
    for scope in granted:
        covered |= _satisfies(scope)
    return [scope for scope in required if scope not in covered]
