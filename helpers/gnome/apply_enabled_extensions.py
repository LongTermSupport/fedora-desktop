#!/usr/bin/env python3
"""Declare every deployed GNOME extension in `org.gnome.shell enabled-extensions`.

The side-effecting half of `enabled_extensions.py`. It discovers the UUIDs
deployed under the user's extensions directory, merges them into the current
gsettings value without removing anything, writes the result back, and re-reads
it to prove the write took.

Why this exists rather than `gnome-extensions enable`: that command asks the
*running shell* to enable a UUID, and on a fresh install the shell has not
scanned the new directory yet, so the request is silently lost — Plan 00110's
desktop acceptance run found eight extensions installed, compiled, loaded and
none enabled. The gsettings key is read by the shell at session start and
watched live, so declaring it works whether or not a shell has loaded the
extension yet.

Invoked from the play as a module from the repo root:

    python3 -m helpers.gnome.apply_enabled_extensions \\
        --extensions-dir ~/.local/share/gnome-shell/extensions \\
        --require workspace-names-overview@fedora-desktop

Marker lines on stdout (the payload the play keys `changed_when` on):

    GNOME-EXT-DEPLOYED <uuid>,<uuid>,...
    GNOME-EXT-ENABLED-CHANGED added=<uuid>,<uuid>,...
    GNOME-EXT-ENABLED-UNCHANGED
    GNOME-EXT-FAIL <reason>

Everything else goes to stderr.
"""

from __future__ import annotations

import argparse
import dataclasses
import os
import subprocess
import sys
from collections.abc import Mapping

from helpers.gnome import enabled_extensions

DEFAULT_SCHEMA = "org.gnome.shell"
DEFAULT_KEY = "enabled-extensions"


@dataclasses.dataclass(frozen=True)
class SessionBus:
    """How to reach a D-Bus session so dconf accepts the write."""

    prefix: list[str]
    address: str | None
    source: str


def resolve_session_bus(environ: Mapping[str, str], runtime_dir: str) -> SessionBus:
    """Pick the bus to write through, preferring the user's live one.

    A live session's bus means the running shell sees the change immediately. With
    no bus at all — `run.bash` from a TTY — `dbus-run-session` gives dconf a
    throwaway one and the value still lands in the user's database, so the play
    needs no `failed_when: false` for the no-session case.

    The socket is tested for access, not mere existence: `sudo -u` can leave a
    stale XDG_RUNTIME_DIR pointing at another user's `0700` runtime directory, and
    connecting to that would fail where falling back succeeds.
    """
    address = environ.get("DBUS_SESSION_BUS_ADDRESS", "").strip()
    if address:
        return SessionBus(prefix=[], address=address, source="environment")

    socket = os.path.join(runtime_dir, "bus")
    if os.access(socket, os.R_OK | os.W_OK):
        return SessionBus(prefix=[], address=f"unix:path={socket}", source="runtime-socket")

    return SessionBus(prefix=["dbus-run-session", "--"], address=None, source="dbus-run-session")


def _gsettings(bus: SessionBus, *args: str) -> str:
    env = dict(os.environ)
    if bus.address:
        env["DBUS_SESSION_BUS_ADDRESS"] = bus.address
    result = subprocess.run(
        [*bus.prefix, "gsettings", *args],
        text=True,
        capture_output=True,
        env=env,
        check=True,
    )
    return result.stdout


def _fail(reason: str, detail: str) -> int:
    print(f"GNOME-EXT-FAIL {reason}")
    print(detail, file=sys.stderr)
    return 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--extensions-dir",
        default=os.path.expanduser("~/.local/share/gnome-shell/extensions"),
        help="Base extensions directory (default: the user's local extensions dir).",
    )
    parser.add_argument(
        "--require",
        action="append",
        default=[],
        metavar="UUID",
        help="A UUID that MUST be deployed; repeatable. Absence fails before any write.",
    )
    parser.add_argument("--schema", default=DEFAULT_SCHEMA, help="GSettings schema.")
    parser.add_argument("--key", default=DEFAULT_KEY, help="GSettings key holding the list.")
    args = parser.parse_args(argv)

    try:
        deployed = enabled_extensions.discover_deployed_uuids(args.extensions_dir)
    except ValueError as error:
        return _fail("bad-extension-metadata", str(error))

    if not deployed:
        return _fail(
            "nothing-deployed",
            f"no extensions found under {args.extensions_dir} — the install steps "
            "above this task did not deploy anything, so there is nothing to enable.",
        )

    missing = enabled_extensions.missing_required(deployed, args.require)
    if missing:
        return _fail(
            "required-uuid-not-deployed",
            f"required but not deployed under {args.extensions_dir}: "
            f"{', '.join(missing)}. Deployed: {', '.join(deployed)}.",
        )

    print(f"GNOME-EXT-DEPLOYED {','.join(deployed)}")

    runtime_dir = os.environ.get("XDG_RUNTIME_DIR") or f"/run/user/{os.getuid()}"
    bus = resolve_session_bus(os.environ, runtime_dir)
    print(f"reaching dconf via {bus.source}", file=sys.stderr)

    raw = _gsettings(bus, "get", args.schema, args.key)
    try:
        current = enabled_extensions.parse_string_list(raw)
    except ValueError as error:
        return _fail(
            "unreadable-current-value",
            f"cannot parse {args.schema} {args.key}: {error}. Refusing to overwrite a "
            "value that could not be read, which would drop the user's own extensions.",
        )

    merged = enabled_extensions.merge(current, deployed)
    if not merged.changed:
        print("GNOME-EXT-ENABLED-UNCHANGED")
        return 0

    _gsettings(bus, "set", args.schema, args.key, enabled_extensions.format_string_list(merged.values))

    # dconf can accept a write and keep the old value (locked or read-only
    # database). Read it back, or this task reports ok while the session comes up
    # with nothing enabled — exactly the defect it was written to remove.
    readback_raw = _gsettings(bus, "get", args.schema, args.key)
    try:
        readback = enabled_extensions.parse_string_list(readback_raw)
    except ValueError as error:
        return _fail("unreadable-written-value", f"cannot parse the value just written: {error}")

    not_written = enabled_extensions.missing_required(readback, deployed)
    if not_written:
        return _fail(
            "write-did-not-take",
            f"{args.schema} {args.key} still omits {', '.join(not_written)} after the "
            f"write (via {bus.source}). The dconf database may be locked or read-only.",
        )

    print(f"GNOME-EXT-ENABLED-CHANGED added={','.join(merged.added)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
