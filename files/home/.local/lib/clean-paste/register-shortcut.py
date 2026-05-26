#!/usr/bin/env python3
"""Reconcile a GNOME custom keyboard shortcut for the clean-paste helper.

Reads parameters from environment variables, inspects the current GNOME
custom-keybindings list, and either reuses an existing slot whose command
already points at our binary or allocates the first unused customN slot.
Then it sets name/command/binding on that slot and adds it to the list
(idempotent — only prints CHANGED when something actually changes).

Environment variables (all required, set by the playbook):
    CP_BINARY   absolute path of the clean-paste helper script
    CP_NAME     display name for the shortcut
    CP_BINDING  GNOME accelerator string, e.g. <Primary><Alt>v
    CP_SCHEMA   org.gnome.settings-daemon.plugins.media-keys
    CP_BASE     /org/gnome/settings-daemon/plugins/media-keys/custom-keybindings

Deployed by: playbooks/imports/optional/common/play-clean-paste.yml
"""
from __future__ import annotations

import ast
import os
import subprocess
import sys


def _required_env(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        print(f"register-shortcut: missing required env var {name}", file=sys.stderr)
        sys.exit(2)
    return value


def gsettings_get(schema: str, key: str) -> str:
    return subprocess.check_output(
        ["gsettings", "get", schema, key], text=True,
    ).strip()


def gsettings_set(schema: str, key: str, value: str) -> None:
    subprocess.check_call(["gsettings", "set", schema, key, value])


def parse_path_list(raw: str) -> list[str]:
    raw = raw.strip()
    if raw.startswith("@as"):
        raw = raw[len("@as"):].strip()
    if not raw:
        return []
    try:
        parsed = ast.literal_eval(raw)
    except (SyntaxError, ValueError):
        return []
    if not isinstance(parsed, (list, tuple)):
        return []
    return [str(p) for p in parsed]


def parse_string(raw: str) -> str:
    raw = raw.strip()
    try:
        parsed = ast.literal_eval(raw)
    except (SyntaxError, ValueError):
        return ""
    return str(parsed) if parsed is not None else ""


def slot_schema(slot_path: str) -> str:
    return f"org.gnome.settings-daemon.plugins.media-keys.custom-keybinding:{slot_path}"


def normalise(path: str) -> str:
    return path if path.endswith("/") else path + "/"


def main() -> int:
    binary = _required_env("CP_BINARY")
    name = _required_env("CP_NAME")
    binding = _required_env("CP_BINDING")
    schema = _required_env("CP_SCHEMA")
    base = _required_env("CP_BASE").rstrip("/")

    list_raw = gsettings_get(schema, "custom-keybindings")
    existing_paths = [normalise(p) for p in parse_path_list(list_raw)]

    # 1. Reuse an existing slot if its command already invokes our binary.
    target_slot: str | None = None
    for path in existing_paths:
        try:
            current_cmd = parse_string(gsettings_get(slot_schema(path), "command"))
        except subprocess.CalledProcessError:
            current_cmd = ""
        if current_cmd == binary:
            target_slot = path
            break

    # 2. Otherwise pick the first /customN/ that is not already in the list.
    if target_slot is None:
        for i in range(100):
            candidate = f"{base}/custom{i}/"
            if candidate not in existing_paths:
                target_slot = candidate
                break
        else:
            print("register-shortcut: no free custom keybinding slot in 0..99", file=sys.stderr)
            return 3

    changed = False
    desired = {"name": name, "command": binary, "binding": binding}
    target_schema = slot_schema(target_slot)
    for key, want in desired.items():
        try:
            have = parse_string(gsettings_get(target_schema, key))
        except subprocess.CalledProcessError:
            have = ""
        if have != want:
            gsettings_set(target_schema, key, want)
            changed = True

    if target_slot not in existing_paths:
        new_list = existing_paths + [target_slot]
        gvariant_literal = "[" + ", ".join("'" + p + "'" for p in new_list) + "]"
        gsettings_set(schema, "custom-keybindings", gvariant_literal)
        changed = True

    state = "CHANGED" if changed else "UNCHANGED"
    print(f"{state} slot={target_slot}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
