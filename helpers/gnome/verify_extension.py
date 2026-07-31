#!/usr/bin/env python3
"""Verify a deployed GNOME Shell extension is healthy, Wayland-reload-aware.

Thin side-effecting wrapper around the pure classifier in extension_state.py.
It gathers three facts and lets the classifier decide:

  1. on-disk metadata `shell-version` (what the deployed extension claims),
  2. the running GNOME Shell major version (`gnome-shell --version`),
  3. the live State from `gnome-extensions info <uuid>`.

Why a helper and not a shell block: the decision is non-trivial (distinguish a
genuine version mismatch / load ERROR — which must fail the play — from the
benign "fixed on disk, pending a Wayland logout" case — which must not), and
this repo's rule is that complex logic goes in a tested helper, not inline YAML
(see helpers/CLAUDE.md). Invoked from the play as a module from the repo root:

    python3 -m helpers.gnome.verify_extension --uuid <uuid>
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys

from helpers.gnome import extension_state


def _dbus_env() -> dict[str, str]:
    env = dict(os.environ)
    env.setdefault(
        "DBUS_SESSION_BUS_ADDRESS", f"unix:path=/run/user/{os.getuid()}/bus"
    )
    return env


def _metadata_shell_versions(extensions_dir: str, uuid: str) -> list[str]:
    path = os.path.join(extensions_dir, uuid, "metadata.json")
    if not os.path.isfile(path):
        sys.exit(
            f"ERROR: {path} not found — the extension is not deployed. Run the "
            "'Deploy Custom Extension' play step before verifying."
        )
    with open(path, encoding="utf-8") as handle:
        data = json.load(handle)
    versions = data.get("shell-version", [])
    # metadata may list integers or strings; normalise to strings for comparison.
    return [str(v) for v in versions]


def _shell_major(env: dict[str, str]) -> str | None:
    """Running GNOME major (e.g. "50"), or None if gnome-shell is unavailable."""
    # check=False is deliberate and the returncode is checked on the next line:
    # "gnome-shell is not installed / no session" is a RESULT this function
    # reports as None, not an error to raise. This is the probe-then-check
    # pattern (CLAUDE.md), not error hiding — nothing is swallowed.
    result = subprocess.run(
        ["gnome-shell", "--version"], text=True, capture_output=True, env=env, check=False
    )
    if result.returncode != 0 or not result.stdout.strip():
        return None
    # "GNOME Shell 50.1" -> "50"
    token = result.stdout.strip().split()[-1]
    return token.split(".")[0] or None


def _live_state(uuid: str, env: dict[str, str]) -> tuple[bool, str | None]:
    """Return (session_available, state). gnome-extensions exits non-zero with no session."""
    # check=False is deliberate — see _shell_major above. A non-zero exit here
    # means "no live session", which is exactly what this function returns as
    # session_available=False; the returncode is checked on the next line.
    result = subprocess.run(
        ["gnome-extensions", "info", uuid], text=True, capture_output=True, env=env, check=False
    )
    if result.returncode != 0:
        return False, None
    state: str | None = None
    for line in result.stdout.splitlines():
        stripped = line.strip()
        if stripped.startswith("State:"):
            state = stripped.split(":", 1)[1].strip()
            break
    return True, state


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--uuid", required=True, help="Extension UUID to verify.")
    parser.add_argument(
        "--extensions-dir",
        default=os.path.expanduser("~/.local/share/gnome-shell/extensions"),
        help="Base extensions directory (default: the user's local extensions dir).",
    )
    args = parser.parse_args(argv)

    env = _dbus_env()
    metadata_versions = _metadata_shell_versions(args.extensions_dir, args.uuid)
    session_available, live_state = _live_state(args.uuid, env)
    shell_major = _shell_major(env) if session_available else None

    verdict = extension_state.classify(
        uuid=args.uuid,
        session_available=session_available,
        live_state=live_state,
        shell_major=shell_major,
        metadata_shell_versions=metadata_versions,
    )

    prefix = "EXT-FAIL" if verdict.is_failure else "EXT-OK"
    print(f"{prefix} [{verdict.verdict.value}] {verdict.message}")
    return verdict.exit_code


if __name__ == "__main__":
    raise SystemExit(main())
