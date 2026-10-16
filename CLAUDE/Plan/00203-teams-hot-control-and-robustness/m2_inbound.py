#!/usr/bin/env python3
"""M2 acceptance (b)'s inbound probe (SPEC-hot-control.md H34, Decision 1). RUN IN THE SESSION'S
CONTAINER, through m2-check.bash: `podman exec <c> python3 -I <this> inbound /proc /etc/claude-code
/tmp/pingbus-home/settings.json`.

Claude Code 2.1.301 binds its inbox socket (`cc-socks/<pid>.sock`) in every session, so the socket
proves nothing. What decides whether a message on it becomes a turn is `crossSessionInbound`, read
once at start from the first of the managed, `--settings` and user settings that sets it (the
project and local settings may only tighten it). So this proves, from what the session was
actually started with:

- exactly one claude runs, and it was started with `--settings` naming the session's settings;
- those settings say `crossSessionInbound: refuse`, which outranks the user's settings;
- no managed settings file (`managed-settings.json` or a drop-in) sets it to anything looser.

Server-managed settings are not visible from here; a ccy session signs in with a personal token.
Prints what it saw, one line each, on stdout; a failure on stderr, exit 1; usage 64.
"""

from __future__ import annotations

import json
import pathlib
import sys
from collections.abc import Sequence

EXIT_OK, EXIT_FAIL, EXIT_USAGE = 0, 1, 64
CLAUDE_NAMES = ("claude", "claude.exe")
BYPASS_FLAGS = ("--dangerously-skip-permissions",)
WANT = "refuse"


class Failed(Exception):
    """The check does not hold; the message says what was seen."""


def _claudes(proc: pathlib.Path) -> list[list[str]]:
    found = []
    for entry in sorted(proc.iterdir()):
        if not entry.name.isdigit():
            continue
        try:
            raw = (entry / "cmdline").read_bytes()
        except OSError:
            continue
        argv = [part.decode("utf-8", "replace") for part in raw.split(b"\0")[:-1]]
        if argv and pathlib.PurePath(argv[0]).name in CLAUDE_NAMES and "--settings" in argv:
            found.append(argv)
    return found


def _inbound_of(path: pathlib.Path) -> object:
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise Failed(f"{path} is not a JSON object")
    return data.get("crossSessionInbound")


def _managed_files(managed: pathlib.Path) -> list[pathlib.Path]:
    files = [managed / "managed-settings.json"]
    drop_in = managed / "managed-settings.d"
    if drop_in.is_dir():
        files.extend(sorted(drop_in.glob("*.json")))
    return [path for path in files if path.exists()]


def inbound(proc: pathlib.Path, managed: pathlib.Path, settings: pathlib.Path) -> list[str]:
    claudes = _claudes(proc)
    if len(claudes) != 1:
        raise Failed(f"want exactly one claude started with --settings, found {claudes!r}")
    argv = claudes[0]
    at = argv.index("--settings") + 1
    given = argv[at] if at < len(argv) else ""
    if given != str(settings):
        raise Failed(f"claude was started with --settings {given!r}, not the session's {settings}")
    value = _inbound_of(settings)
    if value != WANT:
        raise Failed(f"{settings} sets crossSessionInbound to {value!r}, not {WANT!r}")
    seen = [f"claude was started with --settings {settings}, which sets crossSessionInbound: {WANT}"]
    for path in _managed_files(managed):
        policy = _inbound_of(path)
        if policy not in (None, WANT):
            raise Failed(f"the managed settings {path} set crossSessionInbound to {policy!r}, which outranks the session's")
        seen.append(f"the managed settings {path} leave it ({policy!r})")
    if len(seen) == 1:
        seen.append(f"no managed settings under {managed}, so nothing outranks it")
    mode = "bypasses permissions" if any(flag in argv for flag in BYPASS_FLAGS) else "prompts for permissions"
    seen.append(f"the session {mode}; with {WANT}, a message on its inbox socket is dropped, never a turn")
    return seen


def main(argv: Sequence[str]) -> int:
    if len(argv) != 4 or argv[0] != "inbound":
        print(__doc__, file=sys.stderr)
        return EXIT_USAGE
    try:
        lines = inbound(pathlib.Path(argv[1]), pathlib.Path(argv[2]), pathlib.Path(argv[3]))
    except (Failed, OSError, ValueError) as exc:
        print(f"m2_inbound: {exc}", file=sys.stderr)
        return EXIT_FAIL
    for line in lines:
        print(line)
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
