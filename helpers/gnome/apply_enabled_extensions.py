#!/usr/bin/env python3
"""Declare the play's extensions in `org.gnome.shell enabled-extensions`.

The side-effecting half of `enabled_extensions.py`. It confirms every UUID the
play DECLARES is on disk, merges them into the current gsettings value without
removing anything, writes the result back, and re-reads it to prove the write
took.

Why this exists rather than `gnome-extensions enable`: that command asks the
*running shell* to enable a UUID, and on a fresh install the shell has not
scanned the new directory yet, so the request is silently lost — Plan 00110's
desktop acceptance run found eight extensions installed, compiled, loaded and
none enabled. The gsettings key is read by the shell at session start and
watched live, so declaring it works whether or not a shell has loaded the
extension yet.

**The set is declared, never discovered.** `~/.local/share/gnome-shell/extensions`
is also where the user's own extensions live: enumerating it would re-enable what
they deliberately disabled and hand the play's verify loop extensions this repo
never installed, while still missing a partial install. The play passes `--uuid`
per extension it deploys and this confirms each against the search path.

Invoked from the play as a module from the repo root:

    python3 -m helpers.gnome.apply_enabled_extensions \\
        --extensions-dir ~/.local/share/gnome-shell/extensions \\
        --extensions-dir /usr/share/gnome-shell/extensions \\
        --uuid blur-my-shell@aunetx --uuid ...

Marker lines on stdout (the payload the play keys `changed_when` on):

    GNOME-EXT-DEPLOYED <uuid>,<uuid>,...
    GNOME-EXT-ENABLED-CHANGED added=<uuid>,<uuid>,...
    GNOME-EXT-ENABLED-UNCHANGED
    GNOME-EXT-FAIL <reason>

Everything else goes to stderr.
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys

from helpers.gnome import enabled_extensions, session_bus

DEFAULT_SCHEMA = "org.gnome.shell"
DEFAULT_KEY = "enabled-extensions"
DEFAULT_DISABLE_KEY = "disable-user-extensions"


def _gsettings(bus: session_bus.SessionBus, *args: str) -> str:
    env = session_bus.env_for(bus, os.environ)
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
        action="append",
        required=True,
        metavar="DIR",
        help="Directory to search for a declared UUID; repeatable, searched in order.",
    )
    parser.add_argument(
        "--uuid",
        action="append",
        required=True,
        metavar="UUID",
        help="A UUID the play deploys; repeatable. This IS the declared set.",
    )
    parser.add_argument("--schema", default=DEFAULT_SCHEMA, help="GSettings schema.")
    parser.add_argument("--key", default=DEFAULT_KEY, help="GSettings key holding the list.")
    parser.add_argument(
        "--disable-key",
        default=DEFAULT_DISABLE_KEY,
        help="The master switch that defeats every user extension when true.",
    )
    args = parser.parse_args(argv)

    try:
        resolution = enabled_extensions.resolve_declared(args.extensions_dir, args.uuid)
    except ValueError as error:
        return _fail("bad-extension-metadata", str(error))

    if resolution.missing:
        return _fail(
            "declared-extension-not-deployed",
            f"declared but not found under {', '.join(args.extensions_dir)}: "
            f"{', '.join(resolution.missing)}. The install step above did not deploy "
            "them; enabling what did arrive would report a partial install as success.",
        )

    deployed = resolution.found
    print(f"GNOME-EXT-DEPLOYED {','.join(deployed)}")

    bus = session_bus.current()
    print(f"reaching dconf via {bus.source}", file=sys.stderr)

    # This key defeats every user extension whatever the list says, so writing the
    # list while it is true would report success over a session with nothing
    # enabled — Plan 00110's finding one key over. It is false on a fresh install;
    # if someone set it, that is a deliberate act to undo deliberately.
    disabled = _gsettings(bus, "get", args.schema, args.disable_key).strip()
    if disabled == "true":
        return _fail(
            "user-extensions-disabled",
            f"{args.schema} {args.disable_key} is true, so GNOME will run none of "
            "these whatever the enabled list holds. Set it false "
            f"(`gsettings set {args.schema} {args.disable_key} false`) and re-run.",
        )

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

    _gsettings(
        bus, "set", args.schema, args.key, enabled_extensions.format_string_list(merged.values)
    )

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
