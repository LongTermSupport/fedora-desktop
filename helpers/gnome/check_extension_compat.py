#!/usr/bin/env python3
"""Static CI gate: every extension's metadata.json covers this branch's GNOME Shell.

Thin side-effecting wrapper around the pure classifier in fedora_compat.py. It
gathers two file facts and lets the classifier decide, per extension:

  1. the branch's Fedora version (vars/fedora-version.yml), and
  2. each extension's on-disk metadata `shell-version` list
     (extensions/<uuid>/metadata.json).

Unlike verify_extension.py — which inspects a *running* GNOME session and so only
works on the deployed desktop — this check is purely static, so it runs in CI on
the repo source before anything is deployed. It is the gate that would have caught
"metadata stops at GNOME 49 while the F44 branch ships GNOME 50" at PR time.

Why a helper and not a shell block: the decision is non-trivial and this repo's
rule is that complex logic lives in a tested helper, not inline YAML/CI (see
helpers/CLAUDE.md). Invoked from the repo root:

    python3 -m helpers.gnome.check_extension_compat
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys

from helpers.gnome import fedora_compat

# Matches `fedora_version: 44` (optionally quoted), ignoring leading whitespace.
_FEDORA_VERSION_RE = re.compile(r"""^\s*fedora_version:\s*["']?(\d+)["']?\s*$""")


def read_fedora_version(path: str) -> int:
    """Parse the integer `fedora_version` from vars/fedora-version.yml.

    Stdlib-only (no PyYAML — helpers import only the standard library). The file
    is a flat key/value list, so a line scan is sufficient and robust.
    """
    if not os.path.isfile(path):
        sys.exit(
            f"ERROR: {path} not found — cannot determine the branch's Fedora "
            "version. This file is the source of truth for the target release."
        )
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            match = _FEDORA_VERSION_RE.match(line)
            if match:
                return int(match.group(1))
    sys.exit(f"ERROR: no `fedora_version:` key found in {path}.")


def discover_extensions(extensions_dir: str) -> list:
    """Every immediate subdirectory of extensions_dir that holds a metadata.json."""
    if not os.path.isdir(extensions_dir):
        sys.exit(f"ERROR: extensions directory {extensions_dir} not found.")
    found = []
    for entry in sorted(os.scandir(extensions_dir), key=lambda e: e.name):
        if entry.is_dir() and os.path.isfile(
            os.path.join(entry.path, "metadata.json")
        ):
            found.append(entry)
    if not found:
        sys.exit(
            f"ERROR: no extensions with metadata.json found under {extensions_dir}."
        )
    return found


def _metadata(path: str) -> dict:
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def main(argv: list | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--fedora-version-file",
        default="vars/fedora-version.yml",
        help="Path to the Fedora version file (default: vars/fedora-version.yml).",
    )
    parser.add_argument(
        "--extensions-dir",
        default="extensions",
        help="Directory holding <uuid>/metadata.json extension sources "
        "(default: extensions).",
    )
    args = parser.parse_args(argv)

    fedora_version = read_fedora_version(args.fedora_version_file)
    extensions = discover_extensions(args.extensions_dir)

    failures = 0
    for entry in extensions:
        meta = _metadata(os.path.join(entry.path, "metadata.json"))
        uuid = meta.get("uuid", entry.name)
        verdict = fedora_compat.classify(
            uuid=uuid,
            fedora_version=fedora_version,
            metadata_shell_versions=meta.get("shell-version", []),
        )
        prefix = "COMPAT-FAIL" if verdict.is_failure else "COMPAT-OK"
        print(f"{prefix} {verdict.message}")
        if verdict.is_failure:
            failures += 1

    if failures:
        print(
            f"\n{failures} extension(s) do not declare support for the GNOME Shell "
            f"that Fedora {fedora_version} ships. Fix the metadata.json shell-version "
            "list(s) above."
        )
        return 1
    print(
        f"\nAll {len(extensions)} extension(s) cover the GNOME Shell that "
        f"Fedora {fedora_version} ships."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
