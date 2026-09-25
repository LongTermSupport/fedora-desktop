#!/usr/bin/env python3
"""What every gh token must carry, and what a token is missing.

The one executor behind every scope check: run.bash, scripts/gh-account-setup.bash and
play-github-cli-multi.yml call it, and none of them keeps its own list or hierarchy.

    python3 -m helpers.github_scopes.cli required
        the required scopes, comma-joined, for `gh auth login --scopes`.
    gh api -i user | python3 -m helpers.github_scopes.cli missing [--marker]
        the scopes that token lacks, comma-joined, for ONE `gh auth refresh --scopes`
        (nothing when none). --marker prints OK or MISSING:<scopes> instead.
    python3 -m helpers.github_scopes.cli audit --user LOGIN [--user LOGIN ...]
        one line per account: SCOPES-OK, SCOPES-MISSING <scopes>,
        SCOPES-NOT-AUTHENTICATED or SCOPES-UNREADABLE <why>. Each account is read with
        its own token, so the active gh account is never switched.

Run from the repository root, so the package imports. Diagnostics go to stderr; stdout is
only the answer.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import subprocess
import sys

from helpers.github_scopes import scopes

DEFAULT_SCOPES_FILE = pathlib.Path(__file__).resolve().parents[2] / "vars" / "github-required-scopes.yml"
HOST = "github.com"


def _required(path: pathlib.Path) -> list[str]:
    return scopes.load_required(path.read_text(encoding="utf-8"))


def _audit_one(login: str, required: list[str]) -> str:
    token = subprocess.run(
        ["gh", "auth", "token", "--hostname", HOST, "--user", login],
        capture_output=True,
        text=True,
        check=False,
    )
    if token.returncode != 0 or not token.stdout.strip():
        return f"SCOPES-NOT-AUTHENTICATED {login}"
    response = subprocess.run(
        ["gh", "api", "-i", "user"],
        capture_output=True,
        text=True,
        check=False,
        env={**os.environ, "GH_TOKEN": token.stdout.strip()},
    )
    if response.returncode != 0:
        why = " ".join((response.stderr or response.stdout).split()) or f"gh api exit {response.returncode}"
        return f"SCOPES-UNREADABLE {login} {why}"
    lacking = scopes.missing(required, scopes.parse_granted_response(response.stdout))
    if lacking:
        return f"SCOPES-MISSING {login} {','.join(lacking)}"
    return f"SCOPES-OK {login}"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python3 -m helpers.github_scopes.cli")
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("required", "missing", "audit"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--scopes-file", type=pathlib.Path, default=DEFAULT_SCOPES_FILE)
        if name == "missing":
            cmd.add_argument("--marker", action="store_true")
        if name == "audit":
            cmd.add_argument("--user", action="append", required=True)
    args = parser.parse_args(argv)

    try:
        required = _required(args.scopes_file)
    except (OSError, ValueError) as exc:
        print(f"github_scopes: cannot read the required scopes from {args.scopes_file}: {exc}", file=sys.stderr)
        return 1

    if args.command == "required":
        print(",".join(required))
    elif args.command == "missing":
        lacking = scopes.missing(required, scopes.parse_granted_response(sys.stdin.read()))
        if args.marker:
            print(f"MISSING:{','.join(lacking)}" if lacking else "OK")
        elif lacking:
            print(",".join(lacking))
    else:
        for login in args.user:
            print(_audit_one(login, required))
    return 0


if __name__ == "__main__":
    sys.exit(main())
