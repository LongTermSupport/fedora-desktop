#!/usr/bin/env bash
# Plan 00198 acceptance.bash: the per-user GNOME Text Editor override is deployed and in force.
# Read-only; run on the host as the desktop user (through meta-deploy.bash, after
# playbooks/imports/play-gsettings.yml). It cannot prove the workspace behaviour, which is the
# owner's check in Task 2.2: open files from Nautilus on two workspaces.
set -euo pipefail

override="${XDG_DATA_HOME:-${HOME}/.local/share}/applications/org.gnome.TextEditor.desktop"

fail() {
    printf '[FAIL] %s\n' "$*" >&2
    exit 1
}

[[ -f "${override}" ]] || fail "override missing: ${override}"
grep -qx 'DBusActivatable=false' "${override}" || fail "override is still D-Bus activated"
grep -qx 'Exec=gnome-text-editor --new-window %U' "${override}" || fail "override Exec has no --new-window"

default_text="$(gio mime text/plain)"
grep -q 'Default application for.*org.gnome.TextEditor.desktop' <<<"${default_text}" ||
    fail "GNOME Text Editor is not the default for text/plain"

printf '[PASS] GNOME Text Editor override in force: a new window per open, no D-Bus activation\n'
